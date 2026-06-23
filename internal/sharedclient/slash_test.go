package sharedclient

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseSlash(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd *SlashCommand
		wantNil bool
	}{
		{"simple help", "/help", &SlashCommand{Name: "help", Args: nil}, false},
		{"usage command", "/usage", &SlashCommand{Name: "usage", Args: nil}, false},
		{"session with args", "/session list", &SlashCommand{Name: "session", Args: []string{"list"}}, false},
		{"multi-arg command", "/amend task1 name new-name", &SlashCommand{Name: "amend", Args: []string{"task1", "name", "new-name"}}, false},
		{"not a command", "hello", nil, true},
		{"just slash", "/", nil, true},
		{"empty string", "", nil, true},
		{"whitespace only", "   ", nil, true},
		{"command with hyphen", "/my-command", &SlashCommand{Name: "my-command", Args: nil}, false},
		{"command with underscore", "/my_command", &SlashCommand{Name: "my_command", Args: nil}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSlash(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseSlash(%q) = %v, want nil", tt.input, got)
				}
				return
			}
			if got == nil {
				t.Errorf("ParseSlash(%q) = nil, want %v", tt.input, tt.wantCmd)
				return
			}
			if got.Name != tt.wantCmd.Name {
				t.Errorf("ParseSlash(%q).Name = %q, want %q", tt.input, got.Name, tt.wantCmd.Name)
			}
			if len(got.Args) != len(tt.wantCmd.Args) {
				t.Errorf("ParseSlash(%q).Args = %v, want %v", tt.input, got.Args, tt.wantCmd.Args)
			}
			for i, arg := range got.Args {
				if i < len(tt.wantCmd.Args) && arg != tt.wantCmd.Args[i] {
					t.Errorf("ParseSlash(%q).Args[%d] = %q, want %q", tt.input, i, arg, tt.wantCmd.Args[i])
				}
			}
		})
	}
}

func TestIsValidCommandName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"alphanumeric", "help123", true},
		{"with hyphen", "my-command", true},
		{"with underscore", "my_command", true},
		{"mixed case", "MyCoMmAnD", true},
		{"empty", "", false},
		{"with space", "my command", false},
		{"with slash", "my/command", false},
		{"with dot", "my.command", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidCommandName(tt.input)
			if got != tt.want {
				t.Errorf("isValidCommandName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuiltinCommands(t *testing.T) {
	cmds := BuiltinCommands()
	if len(cmds) == 0 {
		t.Fatal("BuiltinCommands() returned empty slice")
	}

	// Check for expected commands
	expected := []string{"help", "clear", "session", "tasks", "cancel", "amend", "interrupt"}
	for _, exp := range expected {
		found := false
		for _, cmd := range cmds {
			if cmd == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("BuiltinCommands() missing expected command: %s", exp)
		}
	}

	// Verify sorted order
	for i := 1; i < len(cmds); i++ {
		if cmds[i] < cmds[i-1] {
			t.Errorf("BuiltinCommands() not sorted: %q > %q at positions %d, %d", cmds[i-1], cmds[i], i-1, i)
		}
	}
}

func TestIsBuiltin(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"help is builtin", "help", true},
		{"clear is builtin", "clear", true},
		{"session is builtin", "session", true},
		{"unknown not builtin", "unknown-cmd", false},
		{"empty not builtin", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsBuiltin(tt.cmd); got != tt.want {
				t.Errorf("IsBuiltin(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsSlashCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid slash", "/help", true},
		{"slash with args", "/session list", true},
		{"not slash", "hello", false},
		{"just slash", "/", false},
		{"empty", "", false},
		{"whitespace then slash", "  /help", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSlashCommand(tt.input); got != tt.want {
				t.Errorf("IsSlashCommand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSortStrings(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"unsorted", []string{"charlie", "alpha", "bravo"}, []string{"alpha", "bravo", "charlie"}},
		{"already sorted", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"empty", []string{}, []string{}},
		{"single", []string{"one"}, []string{"one"}},
		{"with numbers", []string{"z2", "a1", "m3"}, []string{"a1", "m3", "z2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slices.Sort(tt.input)
			for i, got := range tt.input {
				if i < len(tt.want) && got != tt.want[i] {
					t.Errorf("slices.Sort() [%d] = %q, want %q", i, got, tt.want[i])
				}
			}
		})
	}
}

// TestDiscoverCustomCommands_WithClaudeCommands verifies that
// DiscoverCustomCommands() picks up files from the ~/.claude/commands/ tier
// (in addition to ~/.meept/commands/) and that project-local commands override
// user-global ones. The HOME environment variable is redirected to a temp dir
// so the test does not touch the real user home.
func TestDiscoverCustomCommands_WithClaudeCommands(t *testing.T) {
	// Redirect HOME to a temp dir so os.UserHomeDir resolves there.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// ~/.claude/commands/claude-only.md — only in the Claude tier.
	claudeDir := filepath.Join(tmpHome, ".claude", "commands")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeOnly := `---
name: claude-only
description: Claude-only command
---
Claude body with $ARGUMENTS
`
	if err := os.WriteFile(filepath.Join(claudeDir, "claude-only.md"), []byte(claudeOnly), 0o644); err != nil {
		t.Fatal(err)
	}

	// ~/.meept/commands/meept-only.md — only in the meept user-global tier.
	meeptDir := filepath.Join(tmpHome, ".meept", "commands")
	if err := os.MkdirAll(meeptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meeptOnly := `---
name: meept-only
description: Meept-only command
---
Meept body
`
	if err := os.WriteFile(filepath.Join(meeptDir, "meept-only.md"), []byte(meeptOnly), 0o644); err != nil {
		t.Fatal(err)
	}

	// ~/.meept/commands/shared.md — should be overridden by the project-local
	// command of the same name below. Also drop a shared.md into the Claude
	// tier to confirm both user tiers can be shadowed by project-local.
	sharedMeept := `---
name: shared
description: meept user-global version
---
meept user body
`
	if err := os.WriteFile(filepath.Join(meeptDir, "shared.md"), []byte(sharedMeept), 0o644); err != nil {
		t.Fatal(err)
	}
	sharedClaude := `---
name: shared-claude
description: claude user-global version
---
claude user body
`
	if err := os.WriteFile(filepath.Join(claudeDir, "shared-claude.md"), []byte(sharedClaude), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restore cache after test so we don't leak state into other tests.
	defer SetCustomCommands(nil)

	cmds := DiscoverCustomCommands()

	// Claude-only command should be discovered.
	c, ok := cmds["claude-only"]
	if !ok {
		t.Fatal("expected 'claude-only' command from ~/.claude/commands/ to be discovered")
	}
	if c.Description != "Claude-only command" {
		t.Errorf("claude-only description = %q, want %q", c.Description, "Claude-only command")
	}
	if c.Template != "Claude body with $ARGUMENTS" {
		t.Errorf("claude-only template = %q, want %q", c.Template, "Claude body with $ARGUMENTS")
	}

	// Meept-only command should be discovered.
	if _, ok := cmds["meept-only"]; !ok {
		t.Error("expected 'meept-only' command from ~/.meept/commands/ to be discovered")
	}

	// Both tiers should contribute commands; verify cache is populated too.
	cacheNames := CustomCommandNames()
	if len(cacheNames) < 3 {
		t.Errorf("expected at least 3 cached commands, got %d (%v)", len(cacheNames), cacheNames)
	}

	// Verify cache lookup works for the Claude-discovered command.
	if !IsCustomCommand("claude-only") {
		t.Error("IsCustomCommand(claude-only) = false, want true (cache should be populated by DiscoverCustomCommands)")
	}
}

// TestRenderTemplate_Arguments verifies $ARGUMENTS and $N positional
// substitution in command templates. This is a pure function test and safe to
// run in parallel.
func TestRenderTemplate_Arguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		args     []string
		want     string
	}{
		{
			name:     "all arguments",
			template: "Do this: $ARGUMENTS",
			args:     []string{"on", "friday", "at", "12pm"},
			want:     "Do this: on friday at 12pm",
		},
		{
			name:     "positional",
			template: "Research $1 and summarize in $2 sentences",
			args:     []string{"quantum computing", "5"},
			want:     "Research quantum computing and summarize in 5 sentences",
		},
		{
			name:     "mixed $1 and $ARGUMENTS",
			template: "Task: $1 - Details: $ARGUMENTS",
			args:     []string{"urgent", "by EOD", "for client"},
			want:     "Task: urgent - Details: urgent by EOD for client",
		},
		{
			name:     "no arguments keeps template verbatim",
			template: "Static template",
			args:     []string{},
			want:     "Static template",
		},
		{
			name:     "$ARGUMENTS with no args collapses to empty",
			template: "deploy to $ARGUMENTS now",
			args:     nil,
			want:     "deploy to  now",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RenderTemplate(tt.template, tt.args)
			if got != tt.want {
				t.Errorf("RenderTemplate(%q, %v) = %q, want %q", tt.template, tt.args, got, tt.want)
			}
		})
	}
}
