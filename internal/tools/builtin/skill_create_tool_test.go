package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

func TestSkillCreate_NameAndSchema(t *testing.T) {
	tool := NewSkillCreateTool(nil, nil)
	if got := tool.Name(); got != "skills_create" {
		t.Errorf("Name() = %q, want %q", got, "skills_create")
	}
	if tool.Description() == "" {
		t.Error("Description() is empty")
	}

	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters type = %q, want %q", params.Type, "object")
	}

	required := map[string]bool{}
	for _, r := range params.Required {
		required[r] = true
	}
	for _, want := range []string{"name", "description", "body"} {
		if !required[want] {
			t.Errorf("parameter %q missing from Required", want)
		}
	}
	if required["tags"] {
		t.Error("tags must be optional")
	}

	wantTypes := map[string]string{
		"name":        "string",
		"description": "string",
		"body":        "string",
		"tags":        "array",
	}
	for prop, wantType := range wantTypes {
		p, ok := params.Properties[prop]
		if !ok {
			t.Errorf("property %q missing from schema", prop)
			continue
		}
		if p.Type != wantType {
			t.Errorf("property %q type = %q, want %q", prop, p.Type, wantType)
		}
	}
}

func TestSkillCreate_ValidateName(t *testing.T) {
	tests := []struct {
		label   string
		input   string
		wantErr bool
	}{
		{"kebab ok", "code-review", false},
		{"digits ok", "skill2", false},
		{"leading digit ok", "2fast", false},
		{"space and caps reject", "Code Review", true},
		{"empty reject", "", true},
		{"leading hyphen reject", "-x", true},
		{"underscore reject", "a_b", true},
		{"64 chars reject", strings.Repeat("a", 64), true},
		{"63 chars ok", strings.Repeat("a", 63), false},
		{"path traversal reject", "../evil", true},
		{"dot reject", "a.b", true},
		{"slash reject", "a/b", true},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			err := validateSkillName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSkillName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestSkillCreate_AssembleContent(t *testing.T) {
	content := assembleSkillContent("learn-from-video", "one line", []string{"media", "learning"}, "# Learn\n\nsteps...")
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("content should start with frontmatter, got prefix %q", content[:10])
	}
	for _, want := range []string{"name: learn-from-video", "description:", "tags:"} {
		if !strings.Contains(content, want) {
			t.Errorf("content missing %q:\n%s", want, content)
		}
	}
	if !strings.Contains(content, "# Learn\n\nsteps...") {
		t.Errorf("body not present verbatim:\n%s", content)
	}
	// The body must come after the closing frontmatter marker.
	if idx := strings.Index(content, "# Learn"); idx < strings.LastIndex(content, "---") {
		t.Errorf("body must follow frontmatter:\n%s", content)
	}
}

func TestSkillCreate_AssembleContent_NoTags(t *testing.T) {
	content := assembleSkillContent("plain-skill", "desc here", nil, "body text")
	if strings.Contains(content, "tags:") {
		t.Errorf("tags block should be omitted when empty:\n%s", content)
	}
}

func TestSkillCreate_AssembleContent_QuotedDescription(t *testing.T) {
	content := assembleSkillContent("tricky", "use: colons and # hashes", nil, "body")
	if !strings.Contains(content, `description: "use: colons and # hashes"`) {
		t.Errorf("description with YAML specials should be quoted:\n%s", content)
	}
}

func TestSkillCreate_AssembledContent_ParsesWithSkillsParser(t *testing.T) {
	content := assembleSkillContent(
		"learn-from-video",
		"Learns from video transcripts",
		[]string{"media", "learning"},
		"# Learn\n\nstep one\n",
	)
	skill, err := skills.ParseSkillText(content)
	if err != nil {
		t.Fatalf("ParseSkillText failed: %v", err)
	}
	if skill.Name != "learn-from-video" {
		t.Errorf("parsed name = %q, want %q", skill.Name, "learn-from-video")
	}
	if skill.Description != "Learns from video transcripts" {
		t.Errorf("parsed description = %q, want %q", skill.Description, "Learns from video transcripts")
	}
	if len(skill.Tags) != 2 || skill.Tags[0] != "media" || skill.Tags[1] != "learning" {
		t.Errorf("parsed tags = %v, want [media learning]", skill.Tags)
	}
	if !strings.Contains(skill.Body, "step one") {
		t.Errorf("parsed body missing content: %q", skill.Body)
	}
}

// newTestWriter returns a lifecycle.Writer rooted at a fresh temp dir, the
// minimum setup (read writer_test.go): NewWriter accepts a nil logger and
// resolves new-skill paths to <dir>/<name>/SKILL.md via its default resolver.
func newTestWriter(t *testing.T) (*lifecycle.Writer, string) {
	t.Helper()
	dir := t.TempDir()
	return lifecycle.NewWriter(dir, nil), dir
}

func TestSkillCreate_Execute_WritesViaLifecycleWriter(t *testing.T) {
	writer, dir := newTestWriter(t)
	tool := NewSkillCreateTool(writer, nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name":        "learn-from-video",
		"description": "Learns from video transcripts",
		"body":        "# Learn\n\nsteps...",
		"tags":        []any{"media", "learning"},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	wantPath := filepath.Join(dir, "learn-from-video", "SKILL.md")
	msg, _ := result.(map[string]any)["message"].(string)
	if !strings.Contains(msg, wantPath) {
		t.Errorf("output %q should mention created path %q", msg, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("created skill file missing: %v", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\nname: learn-from-video\n") {
		t.Errorf("written content has unexpected prefix: %q", content[:40])
	}
	if !strings.Contains(content, "# Learn") {
		t.Errorf("written content missing body: %q", content)
	}
}

func TestSkillCreate_Execute_DuplicateContentRejected(t *testing.T) {
	writer, _ := newTestWriter(t)
	tool := NewSkillCreateTool(writer, nil)

	// Seed a skill whose on-disk content is byte-identical to what the tool
	// will assemble for "dup-target". Content embeds the skill name, so two
	// different-named creates can only collide when the seeded content is
	// built from the same assembly — exactly the case the writer's SHA dedup
	// rejects (identical content under a different name).
	seed := assembleSkillContent("dup-target", "shared description", []string{"tag"}, "shared body")
	if err := writer.WriteSkill("other-skill", seed); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	_, err := tool.Execute(context.Background(), map[string]any{
		"name":        "dup-target",
		"description": "shared description",
		"body":        "shared body",
		"tags":        []any{"tag"},
	})
	if err == nil {
		t.Fatal("duplicate-content create should fail (writer SHA dedup)")
	}
	if !strings.Contains(err.Error(), "other-skill") {
		t.Errorf("duplicate error should mention the existing skill, got: %v", err)
	}
}

func TestSkillCreate_Execute_InvalidArgsRejected(t *testing.T) {
	writer, _ := newTestWriter(t)
	tool := NewSkillCreateTool(writer, nil)

	tests := []struct {
		label string
		args  map[string]any
	}{
		{"missing name", map[string]any{"description": "d", "body": "b"}},
		{"bad name", map[string]any{"name": "Bad Name", "description": "d", "body": "b"}},
		{"missing description", map[string]any{"name": "ok-name", "body": "b"}},
		{"long description", map[string]any{"name": "ok-name", "description": strings.Repeat("x", 201), "body": "b"}},
		{"empty body", map[string]any{"name": "ok-name", "description": "d", "body": "   "}},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if _, err := tool.Execute(context.Background(), tt.args); err == nil {
				t.Errorf("expected validation error for %v", tt.args)
			}
		})
	}
}

func TestSkillCreate_Execute_NilWriter(t *testing.T) {
	tool := NewSkillCreateTool(nil, nil) // nil writer tolerated at construction
	_, err := tool.Execute(context.Background(), map[string]any{
		"name":        "some-skill",
		"description": "d",
		"body":        "b",
	})
	if err == nil {
		t.Fatal("expected error for nil writer")
	}
	if !strings.Contains(err.Error(), "skills writer unavailable") {
		t.Errorf("error should be the documented nil-writer message, got: %v", err)
	}
}

func TestSkillCreate_Execute_ShadowWarnsButSucceeds(t *testing.T) {
	writer, dir := newTestWriter(t)

	// Minimal registry construction per registry_test.go: NewRegistry + Register.
	registry := skills.NewRegistry()
	registry.Register(&skills.Skill{
		Name:     "existing-skill",
		Priority: skills.PrioritySystem,
		Source:   "system",
	})

	tool := NewSkillCreateTool(writer, nil)
	tool.SetRegistry(registry)

	result, err := tool.Execute(context.Background(), map[string]any{
		"name":        "existing-skill",
		"description": "a shadowing copy",
		"body":        "shadow body",
	})
	if err != nil {
		t.Fatalf("shadowing create should succeed, got error: %v", err)
	}

	msg, _ := result.(map[string]any)["message"].(string)
	if !strings.Contains(msg, "shadows") {
		t.Errorf("success output must state the shadow, got: %q", msg)
	}
	if !strings.Contains(msg, "system") {
		t.Errorf("shadow note should name the tier/source, got: %q", msg)
	}

	// The file still lands on disk.
	if _, err := os.Stat(filepath.Join(dir, "existing-skill", "SKILL.md")); err != nil {
		t.Errorf("shadowing skill should still be written: %v", err)
	}
}

func TestSkillCreate_SetRegistry_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetRegistry panicked on nil: %v", r)
		}
	}()
	tool := &SkillCreateTool{}
	tool.SetRegistry(nil)
}
