package sharedclient

import (
	"testing"

	"github.com/caimlas/meept/internal/sharedclient"
)

// ============================================================================
// Test ParseSlash basics
// ============================================================================

func TestParseSlash_BasicCommands(t *testing.T) {
	tests := []struct {
		input string
		want  *sharedclient.SlashCommand
	}{
		{"/help", &sharedclient.SlashCommand{Name: "help", Args: nil}},
		{"/new", &sharedclient.SlashCommand{Name: "new", Args: nil}},
		{"/clear", &sharedclient.SlashCommand{Name: "clear", Args: nil}},
		{"/usage", &sharedclient.SlashCommand{Name: "usage", Args: nil}},
		{"/status", &sharedclient.SlashCommand{Name: "status", Args: nil}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sharedclient.ParseSlash(tc.input)
			if got == nil {
				t.Fatalf("ParseSlash(%q) returned nil, want %v", tc.input, tc.want)
			}
			if got.Name != tc.want.Name {
				t.Errorf("ParseSlash(%q).Name = %q, want %q", tc.input, got.Name, tc.want.Name)
			}
		})
	}
}

func TestParseSlash_WithArgs(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs []string
	}{
		{"/session list", "session", []string{"list"}},
		{"/task create fix bug", "task", []string{"create", "fix", "bug"}},
		{"/my-skill arg1 arg2", "my-skill", []string{"arg1", "arg2"}},
		{"/undo", "undo", nil},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sharedclient.ParseSlash(tc.input)
			if got == nil {
				t.Fatalf("ParseSlash(%q) returned nil", tc.input)
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if len(tc.wantArgs) > 0 {
				if len(got.Args) != len(tc.wantArgs) {
					t.Errorf("Args length = %d, want %d", len(got.Args), len(tc.wantArgs))
				}
				for i, want := range tc.wantArgs {
					if got.Args[i] != want {
						t.Errorf("Args[%d] = %q, want %q", i, got.Args[i], want)
					}
				}
			} else {
				if got.Args != nil {
					t.Errorf("Args = %v, want nil", got.Args)
				}
			}
		})
	}
}

func TestParseSlash_NotACommand(t *testing.T) {
	tests := []string{
		"hello",
		"",
		"/",
		"  /",
		"  hello",
		"/help is nice after trim", // ParseSlash trims whitespace, so this is a valid command
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			got := sharedclient.ParseSlash(input)
			if got != nil {
				t.Errorf("ParseSlash(%q) = %v, want nil", input, got)
			}
		})
	}
}

func TestParseSlash_InvalidCommandName(t *testing.T) {
	tests := []struct {
		input    string
		desc     string
		wantName string
	}{
		{"/hello!", "exclamation mark invalid", ""},
		{"/hello world", "space splits name and args, name valid", "hello"},
		{"/hel lo", "first word is name", "hel"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got := sharedclient.ParseSlash(tc.input)
			if tc.wantName == "" {
				if got != nil {
					t.Errorf("ParseSlash(%q) = %v, want nil", tc.input, got)
				}
			} else {
				if got == nil {
					t.Fatalf("ParseSlash(%q) returned nil, want name %q", tc.input, tc.wantName)
				}
				if got.Name != tc.wantName {
					t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
				}
			}
		})
	}
}

func TestParseSlash_WhitespaceHandling(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs []string
	}{
		{"/  help  ", "help", nil},
		{"/  session  list  ", "session", []string{"list"}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sharedclient.ParseSlash(tc.input)
			if got == nil {
				t.Fatalf("ParseSlash(%q) returned nil", tc.input)
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if tc.wantArgs != nil {
				if len(got.Args) != len(tc.wantArgs) {
					t.Fatalf("Args count = %d, want %d", len(got.Args), len(tc.wantArgs))
				}
				for i, w := range tc.wantArgs {
					if got.Args[i] != w {
						t.Errorf("Args[%d] = %q, want %q", i, got.Args[i], w)
					}
				}
			}
		})
	}
}

// ============================================================================
// Test IsSlashCommand
// ============================================================================

func TestIsSlashCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/help", true},
		{"/", false},
		{"hello", false},
		{"/a", true},
		{"/my-1_test", true},
		{" /help", true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sharedclient.IsSlashCommand(tc.input)
			if got != tc.want {
				t.Errorf("IsSlashCommand(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// ============================================================================
// Test BuiltinCommands / IsBuiltin
// ============================================================================

func TestBuiltinCommands(t *testing.T) {
	cmds := sharedclient.BuiltinCommands()
	if len(cmds) == 0 {
		t.Fatal("BuiltinCommands returned empty list")
	}

	// Check known builtins are present
	expectedBuiltin := []string{"help", "new", "clear", "status", "stop", "usage"}
	for _, expected := range expectedBuiltin {
		found := false
		for _, c := range cmds {
			if c == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("builtin command %q not found", expected)
		}
	}
}

func TestIsBuiltin(t *testing.T) {
	if !sharedclient.IsBuiltin("help") {
		t.Error("IsBuiltin(help) = false, want true")
	}
	if !sharedclient.IsBuiltin("new") {
		t.Error("IsBuiltin(new) = false, want true")
	}
	if sharedclient.IsBuiltin("unknown-command") {
		t.Error("IsBuiltin(unknown-command) = true, want false")
	}
}

func TestSlashCommand_NameValidation(t *testing.T) {
	tests := []struct {
		input    string
		wantNil  bool
		wantName string
	}{
		{"/valid-name_1", false, "valid-name_1"},
		{"/ALLCAPS", false, "ALLCAPS"},
		{"/123numeric", false, "123numeric"},
		{"/has space", false, "has"},
		{"/has.dot", true, ""},
		{"/has@symb", true, ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sharedclient.ParseSlash(tc.input)
			if tc.wantNil {
				if got != nil {
					t.Errorf("ParseSlash(%q) = %v, want nil", tc.input, got)
				}
			} else {
				if got == nil {
					t.Fatalf("ParseSlash(%q) returned nil, want name %q", tc.input, tc.wantName)
				}
				if got.Name != tc.wantName {
					t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
				}
			}
		})
	}
}
