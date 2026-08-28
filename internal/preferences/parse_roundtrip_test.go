package preferences

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseInstructionFile_BareYAML is the round-trip regression test for
// the Save→disk→Discovery loop. Save() writes bare YAML (no frontmatter
// markers) and docs/concepts/instructions.md documents that format; the
// parser previously only understood frontmatter files, so every scan of a
// Save()-written file minted a fresh ID and dropped all fields (list showed
// a new ghost row per call; show by the listed ID failed).
func TestParseInstructionFile_BareYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "run-tests.yaml")
	content := `id: instr_roundtrip1
name: run tests
trigger: 'manual:'
action: shell_execute
action_args:
    command: go test ./...
enabled: true
scope: global
priority: normal
created_at: 2026-08-28T22:26:38.094611Z
updated_at: 2026-08-28T22:26:38.094611Z
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	instr, err := parseInstructionFile(path, 1)
	if err != nil {
		t.Fatalf("parseInstructionFile: %v", err)
	}
	if instr.ID != "instr_roundtrip1" {
		t.Errorf("ID = %q, want stable frontmatter-style id instr_roundtrip1 (got a freshly minted id?)", instr.ID)
	}
	if instr.Trigger != "manual:" {
		t.Errorf("Trigger = %q, want %q", instr.Trigger, "manual:")
	}
	if instr.Action != "shell_execute" {
		t.Errorf("Action = %q, want shell_execute", instr.Action)
	}
	if cmd := instr.ActionArgs["command"]; cmd != "go test ./..." {
		t.Errorf("ActionArgs[command] = %v, want 'go test ./...'", cmd)
	}
	if !instr.Enabled || instr.Scope != "global" || instr.Priority != "normal" {
		t.Errorf("scalar fields lost: enabled=%v scope=%q priority=%q", instr.Enabled, instr.Scope, instr.Priority)
	}
}

// TestParseInstructionFile_FrontmatterStillWorks pins the markdown-with-
// frontmatter format so the bare-YAML fix doesn't regress it.
func TestParseInstructionFile_FrontmatterStillWorks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "greet.md")
	content := "---\nid: instr_fm1\nname: greet\ntrigger: intent:say hi\naction: agent_trigger\nenabled: true\nscope: project\n---\n\nSay hi back politely.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	instr, err := parseInstructionFile(path, 0)
	if err != nil {
		t.Fatalf("parseInstructionFile: %v", err)
	}
	if instr.ID != "instr_fm1" {
		t.Errorf("ID = %q, want instr_fm1", instr.ID)
	}
	if instr.Trigger != "intent:say hi" || instr.Action != "agent_trigger" {
		t.Errorf("fields lost: trigger=%q action=%q", instr.Trigger, instr.Action)
	}
	if instr.Body != "Say hi back politely." {
		t.Errorf("Body = %q, want frontmatter-stripped markdown", instr.Body)
	}
}
