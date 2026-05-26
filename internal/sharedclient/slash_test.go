package sharedclient

import (
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
