package builtin

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/skills/lifecycle"
	"github.com/caimlas/meept/internal/tools"
)

// writePatchFixtureSkill creates <skillsDir>/<name>/SKILL.md with valid
// frontmatter and the given body, mirroring the discovery_test.go fixture
// pattern.
func writePatchFixtureSkill(t *testing.T, skillsDir, name, body string) string {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	//nolint:gosec // test directory
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create fixture skill dir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: patch fixture skill\n---\n\n" + body + "\n"
	path := filepath.Join(dir, "SKILL.md")
	//nolint:gosec // test file
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture SKILL.md: %v", err)
	}
	return path
}

// newPatchToolFixture builds a Discovery over a temp skills dir, a Registry
// seeded from it, and a real lifecycle.Writer tier-resolved against the same
// discovery — the minimal real-writer fixture per the leaf spec.
func newPatchToolFixture(t *testing.T, skillsDir string) *SkillPatchTool {
	t.Helper()
	discovery := skills.NewDiscovery(skills.WithTiers([]skills.DiscoveryTier{
		{Path: skillsDir, Priority: skills.PriorityUser},
	}))
	found, err := discovery.Discover()
	if err != nil {
		t.Fatalf("discover fixture skills: %v", err)
	}
	registry := skills.NewRegistry()
	registry.RegisterAll(found)
	writer := lifecycle.NewWriter(skillsDir, nil)
	writer.SetTierResolver(discovery)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewSkillPatchTool(registry, writer, logger)
}

func TestSkillPatch_NameAndSchema(t *testing.T) {
	tool := NewSkillPatchTool(skills.NewRegistry(), nil, nil)

	if got := tool.Name(); got != "skills_patch" {
		t.Errorf("Name() = %q, want %q", got, "skills_patch")
	}
	if tool.Description() == "" {
		t.Error("Description() must not be empty")
	}

	params := tool.Parameters()
	if params.Type != schemaTypeObject {
		t.Errorf("Parameters().Type = %q, want %q", params.Type, schemaTypeObject)
	}
	for _, key := range []string{"name", "old_string", "new_string", "content"} {
		if _, ok := params.Properties[key]; !ok {
			t.Errorf("schema missing property %q", key)
		}
	}
	hasName := false
	for _, req := range params.Required {
		if req == "name" {
			hasName = true
		}
	}
	if !hasName {
		t.Errorf("schema Required = %v, want it to include \"name\"", params.Required)
	}
}

func TestSkillPatch_ModeValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		want    patchMode
		wantErr string // empty means no error expected
	}{
		{
			name: "replace via old_string",
			args: map[string]any{"name": "s", "old_string": "step one", "new_string": "step ONE"},
			want: patchModeReplace,
		},
		{
			name: "replace without new_string (deletion to empty)",
			args: map[string]any{"name": "s", "old_string": "step one"},
			want: patchModeReplace,
		},
		{
			name: "rewrite via content",
			args: map[string]any{"name": "s", "content": "---\nname: s\ndescription: d\n---\n\nbody\n"},
			want: patchModeRewrite,
		},
		{
			name:    "both old_string and content",
			args:    map[string]any{"name": "s", "old_string": "a", "content": "b"},
			wantErr: "set either content (rewrite) or old_string/new_string (replace), not both",
		},
		{
			name:    "neither old_string nor content",
			args:    map[string]any{"name": "s", "new_string": "x"},
			wantErr: "provide old_string (replace mode) or content (rewrite mode)",
		},
		{
			name:    "empty args",
			args:    map[string]any{},
			wantErr: "name is required",
		},
		{
			name:    "empty name",
			args:    map[string]any{"name": "", "old_string": "a"},
			wantErr: "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := validatePatchArgs(tt.args)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("validatePatchArgs() err = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("validatePatchArgs() err = %q, want containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validatePatchArgs() unexpected err: %v", err)
			}
			if mode != tt.want {
				t.Errorf("validatePatchArgs() mode = %q, want %q", mode, tt.want)
			}
		})
	}
}

func TestSkillPatch_ReplaceMode(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	skillPath := writePatchFixtureSkill(t, skillsDir, "patch-me", "Do step one then step two.")

	tool := newPatchToolFixture(t, skillsDir)

	res, err := tool.Execute(context.Background(), map[string]any{
		"name":       "patch-me",
		"old_string": "step one",
		"new_string": "step ONE",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok {
		t.Fatalf("Execute result type %T, want *tools.ToolResult", res)
	}
	if !tr.Success {
		t.Fatalf("Execute not successful: %s", tr.Error)
	}
	out, _ := tr.Result.(string)
	if !strings.Contains(out, skillPath) {
		t.Errorf("output %q does not mention written path %q", out, skillPath)
	}

	data, readErr := os.ReadFile(skillPath)
	if readErr != nil {
		t.Fatalf("read patched skill: %v", readErr)
	}
	content := string(data)
	if !strings.Contains(content, "step ONE") {
		t.Errorf("patched file missing \"step ONE\":\n%s", content)
	}
	if !strings.Contains(content, "step two.") {
		t.Errorf("patched file lost surrounding body:\n%s", content)
	}
	if !strings.Contains(content, "name: patch-me") {
		t.Errorf("patched file frontmatter damaged:\n%s", content)
	}
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("patched file missing frontmatter:\n%s", content)
	}
}

func TestSkillPatch_ReplaceMode_OldStringNotFound(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	skillPath := writePatchFixtureSkill(t, skillsDir, "patch-me", "Do step one then step two.")

	tool := newPatchToolFixture(t, skillsDir)

	res, err := tool.Execute(context.Background(), map[string]any{
		"name":       "patch-me",
		"old_string": "missing text",
		"new_string": "x",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if tr.Success {
		t.Fatalf("replace with absent old_string succeeded: %+v", tr)
	}
	if !strings.Contains(tr.Error, "patch-me") || !strings.Contains(tr.Error, "not found") {
		t.Errorf("error %q must name the skill and say \"not found\"", tr.Error)
	}

	data, readErr := os.ReadFile(skillPath)
	if readErr != nil {
		t.Fatalf("read skill after failed patch: %v", readErr)
	}
	if strings.Contains(string(data), "x\n") && !strings.Contains(string(data), "step one") {
		t.Errorf("skill file was modified on failed patch:\n%s", data)
	}
}

func TestSkillPatch_ReplaceMode_OldStringNotUnique(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	skillPath := writePatchFixtureSkill(t, skillsDir, "dupe-me", "step one then step one again.")

	tool := newPatchToolFixture(t, skillsDir)

	res, err := tool.Execute(context.Background(), map[string]any{
		"name":       "dupe-me",
		"old_string": "step one",
		"new_string": "step ONE",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if tr.Success {
		t.Fatalf("replace with non-unique old_string succeeded: %+v", tr)
	}
	if !strings.Contains(tr.Error, "2 occurrences") || !strings.Contains(tr.Error, "include more context") {
		t.Errorf("error %q must report the occurrence count and \"include more context\"", tr.Error)
	}

	data, readErr := os.ReadFile(skillPath)
	if readErr != nil {
		t.Fatalf("read skill after failed patch: %v", readErr)
	}
	if strings.Contains(string(data), "step ONE") {
		t.Errorf("skill file was modified on failed patch:\n%s", data)
	}
}

func TestSkillPatch_RewriteMode(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	writePatchFixtureSkill(t, skillsDir, "rewrite-me", "old body.")

	tool := newPatchToolFixture(t, skillsDir)

	newContent := "---\nname: rewrite-me\ndescription: rewritten fixture\n---\n\nbrand new body with more instructions.\n"
	res, err := tool.Execute(context.Background(), map[string]any{
		"name":    "rewrite-me",
		"content": newContent,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if !tr.Success {
		t.Fatalf("rewrite not successful: %s", tr.Error)
	}
	out, _ := tr.Result.(string)
	if !strings.Contains(out, "rewrite-me") {
		t.Errorf("output %q does not mention the skill name", out)
	}

	data, readErr := os.ReadFile(filepath.Join(skillsDir, "rewrite-me", "SKILL.md"))
	if readErr != nil {
		t.Fatalf("read rewritten skill: %v", readErr)
	}
	if string(data) != newContent {
		t.Errorf("disk content mismatch:\n got: %q\nwant: %q", string(data), newContent)
	}
}

func TestSkillPatch_RewriteMode_InvalidFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	skillPath := writePatchFixtureSkill(t, skillsDir, "rewrite-me", "old body.")

	tool := newPatchToolFixture(t, skillsDir)

	res, err := tool.Execute(context.Background(), map[string]any{
		"name":    "rewrite-me",
		"content": "just some markdown with no frontmatter",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if tr.Success {
		t.Fatalf("rewrite without frontmatter succeeded: %+v", tr)
	}

	data, readErr := os.ReadFile(skillPath)
	if readErr != nil {
		t.Fatalf("read skill after failed rewrite: %v", readErr)
	}
	if !strings.Contains(string(data), "old body.") {
		t.Errorf("skill file was modified on failed rewrite:\n%s", data)
	}
}

func TestSkillPatch_RewriteMode_NameMismatch(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	skillPath := writePatchFixtureSkill(t, skillsDir, "rewrite-me", "old body.")

	tool := newPatchToolFixture(t, skillsDir)

	res, err := tool.Execute(context.Background(), map[string]any{
		"name":    "rewrite-me",
		"content": "---\nname: other-name\ndescription: d\n---\n\nbody\n",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	tr := res.(*tools.ToolResult)
	if tr.Success {
		t.Fatalf("rewrite with mismatched name succeeded: %+v", tr)
	}
	if !strings.Contains(tr.Error, "other-name") || !strings.Contains(tr.Error, "rewrite-me") {
		t.Errorf("error %q must name both the content and target skills", tr.Error)
	}

	data, readErr := os.ReadFile(skillPath)
	if readErr != nil {
		t.Fatalf("read skill after failed rewrite: %v", readErr)
	}
	if !strings.Contains(string(data), "old body.") {
		t.Errorf("skill file was modified on failed rewrite:\n%s", data)
	}
}

func TestSkillPatch_SystemTierRefused(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	skillPath := writePatchFixtureSkill(t, skillsDir, "locked-skill", "immutable body.")

	// Registry entry constructed directly with the system priority, per the
	// leaf's tier-dir fixture note.
	registry := skills.NewRegistry()
	registry.Register(&skills.Skill{
		Name:     "locked-skill",
		Priority: skills.PrioritySystem,
		Path:     skillPath,
		Body:     "immutable body.",
	})
	writer := lifecycle.NewWriter(skillsDir, nil)
	writer.SetTierResolver(skills.NewDiscovery(skills.WithTiers([]skills.DiscoveryTier{
		{Path: skillsDir, Priority: skills.PrioritySystem},
	})))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tool := NewSkillPatchTool(registry, writer, logger)

	for _, args := range []map[string]any{
		{"name": "locked-skill", "old_string": "immutable", "new_string": "mutable"},
		{"name": "locked-skill", "content": "---\nname: locked-skill\ndescription: d\n---\n\nnew\n"},
	} {
		res, err := tool.Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("Execute(%v): %v", args, err)
		}
		tr := res.(*tools.ToolResult)
		if tr.Success {
			t.Fatalf("system-tier patch succeeded: %+v", tr)
		}
		if !strings.Contains(tr.Error, "read-only") || !strings.Contains(tr.Error, "user-tier skill named locked-skill") {
			t.Errorf("error %q must refuse the system tier and suggest a user-tier override", tr.Error)
		}
	}

	data, readErr := os.ReadFile(skillPath)
	if readErr != nil {
		t.Fatalf("read skill after refused patch: %v", readErr)
	}
	if string(data) == "" || !strings.Contains(string(data), "immutable body.") {
		t.Errorf("system-tier skill file was modified:\n%s", data)
	}
}

func TestSkillPatch_NilDependencies(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, "skills")
	writePatchFixtureSkill(t, skillsDir, "patch-me", "body.")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("nil registry", func(t *testing.T) {
		tool := NewSkillPatchTool(nil, lifecycle.NewWriter(skillsDir, nil), logger)
		res, err := tool.Execute(context.Background(), map[string]any{
			"name": "patch-me", "old_string": "body", "new_string": "new body",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		tr := res.(*tools.ToolResult)
		if tr.Success || !strings.Contains(tr.Error, "skills registry unavailable") {
			t.Errorf("nil registry: got %+v, want error \"skills registry unavailable\"", tr)
		}
	})

	t.Run("nil writer", func(t *testing.T) {
		discovery := skills.NewDiscovery(skills.WithTiers([]skills.DiscoveryTier{
			{Path: skillsDir, Priority: skills.PriorityUser},
		}))
		found, err := discovery.Discover()
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		registry := skills.NewRegistry()
		registry.RegisterAll(found)
		tool := NewSkillPatchTool(registry, nil, logger)
		res, err := tool.Execute(context.Background(), map[string]any{
			"name": "patch-me", "old_string": "body", "new_string": "new body",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		tr := res.(*tools.ToolResult)
		if tr.Success || !strings.Contains(tr.Error, "skills writer unavailable") {
			t.Errorf("nil writer: got %+v, want error \"skills writer unavailable\"", tr)
		}
	})

	t.Run("unknown skill", func(t *testing.T) {
		tool := newPatchToolFixture(t, skillsDir)
		res, err := tool.Execute(context.Background(), map[string]any{
			"name": "no-such-skill", "old_string": "body", "new_string": "new body",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		tr := res.(*tools.ToolResult)
		if tr.Success || !strings.Contains(tr.Error, "no-such-skill") {
			t.Errorf("unknown skill: got %+v, want error naming the skill", tr)
		}
	})
}
