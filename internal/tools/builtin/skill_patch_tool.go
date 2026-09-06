package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/skills/lifecycle"
	"github.com/caimlas/meept/internal/tools"
)

// patchMode selects how skills_patch modifies a skill.
type patchMode string

const (
	patchModeReplace patchMode = "replace"
	patchModeRewrite patchMode = "rewrite"
)

// SkillPatchTool is the agent-facing modification half of skill authoring:
// it edits EXISTING skills through the lifecycle Writer. Two modes, never
// both:
//
//   - replace: a unique-string swap on the body (frontmatter is never
//     touched — use rewrite for structural changes). This keeps old_string
//     uniqueness meaningful and avoids YAML surgery.
//   - rewrite: the full new SKILL.md content, validated through the same
//     parser path skills_create uses.
//
// Skills discovered in the system tier (skills.PrioritySystem) are refused:
// the system tier is immutable from the agent's perspective, and the agent
// is directed to create a user-tier shadow instead.
type SkillPatchTool struct {
	tools.ToolDefaults
	registry *skills.Registry
	writer   *lifecycle.Writer
	logger   *slog.Logger
}

// NewSkillPatchTool creates a new skills_patch tool. The registry and writer
// may be nil (they are injected later in daemon setups); Execute returns an
// error instead of panicking when a nil dependency is actually used.
func NewSkillPatchTool(registry *skills.Registry, writer *lifecycle.Writer, logger *slog.Logger) *SkillPatchTool {
	if logger == nil {
		logger = slog.Default()
	}
	return &SkillPatchTool{
		registry: registry,
		writer:   writer,
		logger:   logger,
	}
}

func (t *SkillPatchTool) Name() string { return "skills_patch" }

func (t *SkillPatchTool) Category() string { return "skills" }

func (t *SkillPatchTool) Description() string {
	return "Modify an existing skill: either patch its body via a unique old_string -> new_string replacement (replace mode), or replace the entire SKILL.md content (rewrite mode). Never edits skills discovered in the system tier; create a user-tier skill with the same name to override one of those."
}

func (t *SkillPatchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"name": {
				Type:        schemaTypeString,
				Description: "Name of the existing skill to modify.",
			},
			"old_string": {
				Type:        schemaTypeString,
				Description: "Replace mode: the exact text to replace. It must occur EXACTLY ONCE in the skill body — include enough surrounding context to make it unique.",
			},
			"new_string": {
				Type:        schemaTypeString,
				Description: "Replace mode: the replacement text. May be empty to delete old_string.",
			},
			"content": {
				Type:        schemaTypeString,
				Description: "Rewrite mode: the complete new SKILL.md including YAML frontmatter (name must match the target skill).",
			},
		},
		Required: []string{"name"},
	}
}

// validatePatchArgs extracts the skill name and infers the mode.
// Mode inference: content != "" -> rewrite; else replace. Supplying both
// content and old_string is an error.
func validatePatchArgs(args map[string]any) (patchMode, error) {
	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("name is required and must be a non-empty string")
	}

	content, _ := args["content"].(string)
	oldString, _ := args["old_string"].(string)

	switch {
	case content != "" && oldString != "":
		return "", fmt.Errorf("set either content (rewrite) or old_string/new_string (replace), not both")
	case content != "":
		return patchModeRewrite, nil
	case oldString != "":
		return patchModeReplace, nil
	default:
		return "", fmt.Errorf("provide old_string (replace mode) or content (rewrite mode)")
	}
}

func (t *SkillPatchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	mode, err := validatePatchArgs(args)
	if err != nil {
		return tools.NewErrorResult(err.Error()), nil
	}

	name, _ := args["name"].(string)

	if t.registry == nil {
		return tools.NewErrorResult("skills registry unavailable"), nil
	}
	if t.writer == nil {
		return tools.NewErrorResult("skills writer unavailable"), nil
	}

	skill := t.registry.Get(name)
	if skill == nil {
		return tools.NewErrorResult(fmt.Sprintf("skill %q not found in the registry; use skills_create to add it", name)), nil
	}

	// System-tier refusal before any read-modify-write work.
	if skill.Priority == skills.PrioritySystem {
		return tools.NewErrorResult(fmt.Sprintf(
			"skill %q lives in the system tier; system tier skills are read-only; create a user-tier skill named %s to override",
			name, name)), nil
	}

	newContent, err := t.buildContent(mode, name, skill, args)
	if err != nil {
		return tools.NewErrorResult(err.Error()), nil
	}

	if err := t.writer.WriteSkill(name, newContent); err != nil {
		return tools.NewErrorResult(fmt.Sprintf("failed to write skill %q: %v", name, err)), nil
	}

	// The Writer has no exported path accessor; the registry skill's own
	// Path is the tier path discovery serves and the resolver-backed Writer
	// writes in place, so it is the written path.
	path := skill.Path

	t.logger.Info("Skill patched", "name", name, "mode", string(mode), "path", path)

	return tools.NewSuccessResult(fmt.Sprintf(
		"patched skill %q (%s mode) at %s", name, mode, path)), nil
}

// buildContent produces the new SKILL.md text for the selected mode.
// No writes happen here — a validation failure leaves the skill file on
// disk untouched.
func (t *SkillPatchTool) buildContent(mode patchMode, name string, skill *skills.Skill, args map[string]any) (string, error) {
	switch mode {
	case patchModeReplace:
		return patchBody(skill, args)
	case patchModeRewrite:
		return validatedRewriteContent(name, args)
	default:
		return "", fmt.Errorf("unknown patch mode %q", mode)
	}
}

// patchBody re-reads the on-disk SKILL.md (skill.Path) and splices the body
// on the second "---" line, so the frontmatter is carried over byte-for-byte
// (the registry Skill.Body excludes frontmatter, so reassembly from parsed
// fields would be lossy).
func patchBody(skill *skills.Skill, args map[string]any) (string, error) {
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)

	disk, err := os.ReadFile(skill.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read skill %q at %s: %w", skill.Name, skill.Path, err)
	}
	frontmatter, body, ok := splitSkillFrontmatter(string(disk))
	if !ok {
		return "", fmt.Errorf("skill %q at %s has no frontmatter; use rewrite mode to repair it", skill.Name, skill.Path)
	}

	if strings.Count(body, oldString) == 0 {
		return "", fmt.Errorf("old_string not found in skill %q body; the body may have changed since you read it", skill.Name)
	}
	if n := strings.Count(body, oldString); n > 1 {
		return "", fmt.Errorf("old_string found %d occurrences in skill %q body; include more context to make it unique", n, skill.Name)
	}

	patched := strings.Replace(body, oldString, newString, 1)
	return frontmatter + patched, nil
}

// validatedRewriteContent checks that the submitted full SKILL.md content
// parses via the shared skills parser (the same path skills_create uses)
// and that its frontmatter name matches the patch target.
func validatedRewriteContent(name string, args map[string]any) (string, error) {
	content, _ := args["content"].(string)

	parsed, err := skills.ParseSkillText(content)
	if err != nil {
		return "", fmt.Errorf("rewrite content is not a valid SKILL.md: %w", err)
	}
	if parsed.Name != name {
		return "", fmt.Errorf("rewrite content name %q does not match target %q", parsed.Name, name)
	}
	return content, nil
}

// splitSkillFrontmatter splits a SKILL.md into its raw frontmatter block
// (including the closing "---" delimiter and the trailing newline) and the
// body after it. ok is false when the text has no frontmatter.
func splitSkillFrontmatter(text string) (frontmatter, body string, ok bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", "", false
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", false
	}
	after := rest[end+len("\n---"):]
	// The body starts after the closing delimiter's newline (or immediately
	// when the file ends on the delimiter line).
	body = strings.TrimPrefix(after, "\n")
	if !strings.HasSuffix(body, "\n") && body != "" {
		body += "\n"
	}
	return text[:0] + "---\n" + rest[:end+len("\n---")] + "\n", body, true
}

// Ensure SkillPatchTool implements the Tool interface.
var _ tools.Tool = (*SkillPatchTool)(nil)
