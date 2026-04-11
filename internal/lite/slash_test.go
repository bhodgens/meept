package lite

import (
	"reflect"
	"testing"
)

func TestParseSlash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *SlashCommand
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "no slash prefix",
			input:    "hello",
			expected: nil,
		},
		{
			name:     "just slash",
			input:    "/",
			expected: nil,
		},
		{
			name:     "slash with space only",
			input:    "/ ",
			expected: nil,
		},
		{
			name:  "simple command",
			input: "/help",
			expected: &SlashCommand{
				Name: "help",
				Args: nil,
			},
		},
		{
			name:  "command with whitespace",
			input: "  /help  ",
			expected: &SlashCommand{
				Name: "help",
				Args: nil,
			},
		},
		{
			name:  "command with one arg",
			input: "/model gpt-4",
			expected: &SlashCommand{
				Name: "model",
				Args: []string{"gpt-4"},
			},
		},
		{
			name:  "command with multiple args",
			input: "/session switch main",
			expected: &SlashCommand{
				Name: "session",
				Args: []string{"switch", "main"},
			},
		},
		{
			name:  "command with hyphen",
			input: "/my-skill",
			expected: &SlashCommand{
				Name: "my-skill",
				Args: nil,
			},
		},
		{
			name:  "command with underscore",
			input: "/my_skill",
			expected: &SlashCommand{
				Name: "my_skill",
				Args: nil,
			},
		},
		{
			name:  "command with numbers",
			input: "/skill123",
			expected: &SlashCommand{
				Name: "skill123",
				Args: nil,
			},
		},
		{
			name:  "command uppercase preserved",
			input: "/MySkill",
			expected: &SlashCommand{
				Name: "MySkill",
				Args: nil,
			},
		},
		{
			name:     "invalid command with special char",
			input:    "/skill!test",
			expected: nil,
		},
		{
			name:     "invalid command with space in name",
			input:    "/",
			expected: nil,
		},
		{
			name:  "new command alias",
			input: "/new",
			expected: &SlashCommand{
				Name: "new",
				Args: nil,
			},
		},
		{
			name:  "clear command alias",
			input: "/clear",
			expected: &SlashCommand{
				Name: "clear",
				Args: nil,
			},
		},
		{
			name:  "task with id",
			input: "/task abc123",
			expected: &SlashCommand{
				Name: "task",
				Args: []string{"abc123"},
			},
		},
		{
			name:  "session with name containing spaces requires multiple args",
			input: "/session my session name",
			expected: &SlashCommand{
				Name: "session",
				Args: []string{"my", "session", "name"},
			},
		},
		{
			name:  "usage command",
			input: "/usage",
			expected: &SlashCommand{
				Name: "usage",
				Args: nil,
			},
		},
		{
			name:  "retry command",
			input: "/retry",
			expected: &SlashCommand{
				Name: "retry",
				Args: nil,
			},
		},
		{
			name:  "undo command",
			input: "/undo",
			expected: &SlashCommand{
				Name: "undo",
				Args: nil,
			},
		},
		{
			name:  "skill invocation with args",
			input: "/code-review file.go",
			expected: &SlashCommand{
				Name: "code-review",
				Args: []string{"file.go"},
			},
		},
		{
			name:  "multiple spaces between args",
			input: "/model   gpt-4    turbo",
			expected: &SlashCommand{
				Name: "model",
				Args: []string{"gpt-4", "turbo"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSlash(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ParseSlash(%q) = %+v, want %+v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuiltinCommands(t *testing.T) {
	commands := BuiltinCommands()

	// Check that all expected commands are present
	expected := []string{"help", "new", "clear", "model", "retry", "undo", "usage", "session", "task"}

	if len(commands) != len(expected) {
		t.Errorf("BuiltinCommands() returned %d commands, expected %d", len(commands), len(expected))
	}

	// Check each expected command is present
	commandSet := make(map[string]bool)
	for _, cmd := range commands {
		commandSet[cmd] = true
	}

	for _, exp := range expected {
		if !commandSet[exp] {
			t.Errorf("BuiltinCommands() missing expected command: %s", exp)
		}
	}

	// Check that commands are sorted
	for i := 1; i < len(commands); i++ {
		if commands[i] < commands[i-1] {
			t.Errorf("BuiltinCommands() not sorted: %s comes after %s", commands[i], commands[i-1])
		}
	}
}

func TestIsBuiltin(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"help is builtin", "help", true},
		{"new is builtin", "new", true},
		{"clear is builtin", "clear", true},
		{"model is builtin", "model", true},
		{"retry is builtin", "retry", true},
		{"undo is builtin", "undo", true},
		{"usage is builtin", "usage", true},
		{"session is builtin", "session", true},
		{"task is builtin", "task", true},
		{"custom-skill is not builtin", "custom-skill", false},
		{"empty is not builtin", "", false},
		{"Help uppercase is not builtin", "Help", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsBuiltin(tt.input)
			if result != tt.expected {
				t.Errorf("IsBuiltin(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsValidCommandName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"simple", "help", true},
		{"with hyphen", "my-skill", true},
		{"with underscore", "my_skill", true},
		{"with numbers", "skill123", true},
		{"uppercase", "MySkill", true},
		{"empty", "", false},
		{"with space", "my skill", false},
		{"with dot", "my.skill", false},
		{"with exclamation", "skill!", false},
		{"with at", "skill@test", false},
		{"with slash", "skill/test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidCommandName(tt.input)
			if result != tt.expected {
				t.Errorf("isValidCommandName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSortStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "single",
			input:    []string{"a"},
			expected: []string{"a"},
		},
		{
			name:     "already sorted",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "reverse",
			input:    []string{"c", "b", "a"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "random",
			input:    []string{"help", "clear", "model", "undo"},
			expected: []string{"clear", "help", "model", "undo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying the test case
			input := make([]string, len(tt.input))
			copy(input, tt.input)

			sortStrings(input)

			if !reflect.DeepEqual(input, tt.expected) {
				t.Errorf("sortStrings(%v) = %v, want %v", tt.input, input, tt.expected)
			}
		})
	}
}
