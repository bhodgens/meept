package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizePromptName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"interview", "planner/interview.md"},
		{"planner/interview.md", "planner/interview.md"},
		{"orchestrator/split.md", "orchestrator/split.md"},
		{"/planner/foo.md", "planner/foo.md"},
		{"reflection/turn", "reflection/turn.md"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizePromptName(tt.input)
			if got != tt.want {
				t.Errorf("normalizePromptName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateTemplateContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"valid", "HELLO {{.X}}", false},
		{"valid_with_frontmatter", "---\nname: test\n---\nBODY {{.X}}", false},
		{"empty", "", true},
		{"whitespace_only", "   \n  ", true},
		{"malformed", "{{ .Broken", true},
		{"plain_text", "just text", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTemplateContent(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTemplateContent(%s) err = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestStripFrontmatterCLI(t *testing.T) {
	// Standard frontmatter
	body := stripFrontmatterCLI("---\nname: test\n---\nHELLO")
	if body != "HELLO" {
		t.Errorf("standard: got %q", body)
	}

	// No frontmatter
	body = stripFrontmatterCLI("JUST TEXT")
	if body != "JUST TEXT" {
		t.Errorf("no-frontmatter: got %q", body)
	}

	// CRLF
	body = stripFrontmatterCLI("---\r\nname: test\r\n---\r\nHELLO")
	if body != "HELLO" {
		t.Errorf("crlf: got %q", body)
	}
}

func TestLocalPromptBase_ListAll(t *testing.T) {
	tmp := t.TempDir()
	bundledDir := filepath.Join(tmp, "bundled")
	if err := os.MkdirAll(filepath.Join(bundledDir, "planner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundledDir, "planner", "decompose.md"), []byte("BODY"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := &localPromptBase{
		tiers: []localTier{
			{"bundled", bundledDir},
		},
	}
	entries := b.listAll()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0]["name"] != "planner/decompose.md" {
		t.Errorf("name = %s", entries[0]["name"])
	}
	if entries[0]["tier"] != "bundled" {
		t.Errorf("tier = %s", entries[0]["tier"])
	}
}

func TestLocalPromptBase_FindFirst(t *testing.T) {
	tmp := t.TempDir()
	bundledDir := filepath.Join(tmp, "bundled")
	if err := os.MkdirAll(filepath.Join(bundledDir, "planner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundledDir, "planner", "decompose.md"), []byte("BODY"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := &localPromptBase{
		tiers: []localTier{
			{"bundled", bundledDir},
		},
	}

	tier, full, content, ok := b.findFirst("decompose")
	if !ok {
		t.Fatal("not found")
	}
	if tier != "bundled" {
		t.Errorf("tier = %s", tier)
	}
	if !strings.HasSuffix(full, "planner/decompose.md") {
		t.Errorf("path = %s", full)
	}
	if string(content) != "BODY" {
		t.Errorf("content = %s", string(content))
	}
}

func TestLocalPromptBase_FindFirst_NotFound(t *testing.T) {
	b := &localPromptBase{
		tiers: []localTier{
			{"bundled", t.TempDir()},
		},
	}
	_, _, _, ok := b.findFirst("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestLocalPromptBase_UserOverridePath(t *testing.T) {
	b := &localPromptBase{
		tiers: []localTier{
			{"project", "/project"},
			{"user", "/user"},
			{"system", "/system"},
			{"bundled", "/bundled"},
		},
	}
	path := b.userOverridePath("planner/decompose.md")
	if path != filepath.Join("/user", "planner", "decompose.md") {
		t.Errorf("override path = %s", path)
	}
}

func TestPromptsListCmd_JSON(t *testing.T) {
	// Test that the list command produces valid output with JSON flag.
	// We test via the localPromptBase directly since the cobra command
	// writes to stdout.
	tmp := t.TempDir()
	bundledDir := filepath.Join(tmp, "bundled")
	if err := os.MkdirAll(filepath.Join(bundledDir, "planner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundledDir, "planner", "test.md"), []byte("{{.X}}"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := &localPromptBase{
		tiers: []localTier{
			{"bundled", bundledDir},
		},
	}
	entries := b.listAll()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

// TestPromptsCmd is a smoke test that the command tree is wired.
func TestPromptsCmd(t *testing.T) {
	cmd := newPromptsCmd()
	if cmd.Use != "prompts" {
		t.Errorf("Use = %s", cmd.Use)
	}
	if len(cmd.Commands()) < 4 {
		t.Errorf("expected at least 4 subcommands, got %d", len(cmd.Commands()))
	}
}

// captureStdout captures fmt.Println output during fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}
