package builtin

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/skills/lifecycle"
	"github.com/caimlas/meept/internal/tools"
)

// skillNameRE matches meept skill names: lowercase kebab-case, starting with
// a lowercase letter or digit, up to 63 characters. The restriction doubles
// as a path-safety gate: no separators, dots, or traversal sequences can
// appear in a name that reaches filesystem joins.
var skillNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// maxDescriptionLength caps the description field length.
const maxDescriptionLength = 200

// schemaPropBody is the skill markdown body parameter name.
const schemaPropBody = "body"

// SkillCreateTool creates new SKILL.md files in the user skills tier through
// the lifecycle Writer. The agent supplies name, description, tags, and a
// markdown body; the tool assembles frontmatter + body, validates against the
// skills parser's rules, and delegates the write to the Writer (tier
// resolution, versioning, and SHA dedup remain the Writer's job).
//
// The writer may be nil at construction time: the daemon builds tools before
// skills are initialized. Execute returns a clear error if the writer is
// still nil at call time.
type SkillCreateTool struct {
	tools.ToolDefaults
	writer   *lifecycle.Writer
	registry *skills.Registry
	logger   *slog.Logger
}

// NewSkillCreateTool creates a new skill creation tool. The writer may be
// nil at construction time (daemon ordering builds tools before skills);
// Execute fails with a clear error if it is still nil at call time.
func NewSkillCreateTool(writer *lifecycle.Writer, logger *slog.Logger) *SkillCreateTool {
	if logger == nil {
		logger = slog.Default()
	}
	return &SkillCreateTool{
		writer: writer,
		logger: logger,
	}
}

// Name returns the unique identifier for this tool.
func (t *SkillCreateTool) Name() string { return "skills_create" }

// Description returns a human-readable description of what the tool does.
func (t *SkillCreateTool) Description() string {
	return "Create a new skill (SKILL.md) in the user skills tier. " +
		"Supply a kebab-case name, a one-line description (max 200 chars), " +
		"optional tags, and the markdown instruction body. The skill becomes " +
		"discoverable immediately."
}

// Parameters returns the JSON Schema parameters for this tool.
func (t *SkillCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropName: {
				Type:        schemaTypeString,
				Description: "Unique skill name: lowercase kebab-case (^[a-z0-9][a-z0-9-]*$), max 63 chars.",
			},
			schemaPropDescription: {
				Type:        schemaTypeString,
				Description: "One-line description of what the skill does (max 200 chars).",
			},
			schemaPropBody: {
				Type:        schemaTypeString,
				Description: "Markdown instruction body for the skill.",
			},
			schemaPropTags: {
				Type:        schemaTypeArray,
				Description: "Optional categorization tags.",
				Items:       &llm.ParameterProperty{Type: schemaTypeString},
			},
		},
		Required: []string{schemaPropName, schemaPropDescription, schemaPropBody},
	}
}

// validateSkillName enforces the kebab-case name rule shared with discovery.
// It also blocks path traversal: the regex admits only [a-z0-9-], so no
// separators or dots can reach filepath joins.
func validateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if len(name) > 63 {
		return fmt.Errorf("skill name too long (%d chars, max 63)", len(name))
	}
	if !skillNameRE.MatchString(name) {
		return fmt.Errorf("invalid skill name %q: must be lowercase kebab-case matching ^[a-z0-9][a-z0-9-]*$", name)
	}
	return nil
}

// Execute runs the skill creation tool.
func (t *SkillCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// ctx is part of the tools.Tool contract; this tool performs no
	// context-sensitive I/O of its own (the Writer handles cancellation).
	name, _ := args[schemaPropName].(string)
	description, _ := args[schemaPropDescription].(string)
	body, _ := args[schemaPropBody].(string)

	if err := validateSkillName(name); err != nil {
		return nil, err
	}
	if err := validateSkillDescription(description); err != nil {
		return nil, err
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("body is required (non-empty markdown instructions)")
	}

	tags := extractTags(args)

	content := assembleSkillContent(name, description, tags, body)

	// Ensure the assembled content is acceptable to the skills parser before
	// writing. This is a cheap internal sanity gate; assembly is deterministic
	// so failure here indicates a tool bug, surfaced honestly.
	if _, err := skills.ParseSkillText(content); err != nil {
		return nil, fmt.Errorf("assembled skill failed validation: %w", err)
	}

	if t.writer == nil {
		return nil, fmt.Errorf("skills writer unavailable")
	}

	if err := t.writer.WriteSkill(name, content); err != nil {
		return nil, fmt.Errorf("write skill %q: %w", name, err)
	}

	path := t.writer.SkillPath(name)
	output := fmt.Sprintf("created skill %q at %s", name, path)

	// Shadow detection runs after a successful write: the registry (when
	// attached) reflects skills from every tier, so a hit means the new
	// user-tier skill outranks an existing lower-priority copy. Shadowing is
	// the discovery mechanism — creation succeeds, with a stated warning.
	if note := t.shadowNote(name); note != "" {
		output += "\n" + note
	}

	return map[string]any{
		"message": output,
		"name":    name,
		"path":    path,
	}, nil
}

// shadowNote returns a human-readable shadow warning when the registry knows
// an existing skill with the same name, or "" when it does not. Nil-safe.
func (t *SkillCreateTool) shadowNote(name string) string {
	if t.registry == nil {
		return ""
	}
	existing := t.registry.Get(name)
	if existing == nil {
		return ""
	}
	source := existing.Source
	if source == "" {
		source = existing.SourceOrigin
	}
	if source == "" {
		source = "unknown"
	}
	return fmt.Sprintf("warning: new skill %q shadows existing skill %q (source: %s, priority: %d)",
		name, existing.Name, source, existing.Priority)
}

// SetRegistry injects a skills registry for shadow detection. Nil guard per
// CLAUDE.md setter rule: a nil argument is ignored.
func (t *SkillCreateTool) SetRegistry(r *skills.Registry) {
	if r != nil {
		t.registry = r
	}
}

// validateSkillDescription enforces the description length cap.
func validateSkillDescription(description string) error {
	if strings.TrimSpace(description) == "" {
		return fmt.Errorf("description is required")
	}
	if len(description) > maxDescriptionLength {
		return fmt.Errorf("description too long (%d chars, max %d)", len(description), maxDescriptionLength)
	}
	return nil
}

// extractTags pulls the optional tags array from tool args, tolerating
// []string and []any forms.
func extractTags(args map[string]any) []string {
	raw, ok := args[schemaPropTags]
	if !ok || raw == nil {
		return nil
	}
	switch tags := raw.(type) {
	case []string:
		return tags
	case []any:
		out := make([]string, 0, len(tags))
		for _, v := range tags {
			s, ok := v.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}

// assembleSkillContent builds SKILL.md content: YAML frontmatter (name,
// description, tags only when non-empty) followed by the body. Description is
// emitted as a single YAML-safe line.
func assembleSkillContent(name, description string, tags []string, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "description: %s\n", yamlSafeLine(description))
	if len(tags) > 0 {
		b.WriteString("tags:\n")
		for _, tag := range tags {
			fmt.Fprintf(&b, "  - %s\n", yamlSafeLine(tag))
		}
	}
	b.WriteString("---\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return b.String()
}

// yamlSafeLine renders s as a single-line YAML scalar, quoting when the value
// contains characters YAML would otherwise interpret (colons, hashes,
// leading specials, braces).
func yamlSafeLine(s string) string {
	trimmed := strings.TrimSpace(s)
	collapsed := strings.Join(strings.Fields(trimmed), " ")
	needsQuote := collapsed == "" ||
		strings.ContainsAny(collapsed, ":#{}[]&*!|>'\"%@`,") ||
		strings.HasPrefix(collapsed, "-")
	if needsQuote {
		return fmt.Sprintf("%q", strings.ReplaceAll(collapsed, `"`, `\"`))
	}
	return collapsed
}
