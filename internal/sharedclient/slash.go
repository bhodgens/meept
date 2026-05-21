package sharedclient

import (
	"strings"
)

// SlashCommand represents a parsed slash command.
type SlashCommand struct {
	// Name is the command name without the leading slash.
	Name string
	// Args contains any arguments after the command name.
	Args []string
}

// builtinCommands is the set of built-in commands that are handled locally.
var builtinCommands = map[string]struct{}{
	"help":      {},
	"new":       {},
	"clear":     {},
	"retry":     {},
	"undo":      {},
	"usage":     {},
	"stop":      {},
	"status":    {},
	"vim":       {},
	"session":   {},
	"task":      {},
	"cancel":    {},
	"amend":     {},
	"interrupt": {},
	"tasks":     {},
}

// CommandTasks is the "tasks" command name, exported for compatibility with tui/constants.go.
const CommandTasks = "tasks"

// ParseSlash parses a slash command from input text.
// Returns nil if the input is not a slash command.
//
// A valid slash command:
//   - Starts with "/"
//   - Has at least one character after the slash
//   - Command name is alphanumeric with hyphens/underscores allowed
//
// Examples:
//
//	"/help" -> SlashCommand{Name: "help", Args: nil}
//	"/usage" -> SlashCommand{Name: "usage", Args: nil}
//	"/session list" -> SlashCommand{Name: "session", Args: []string{"list"}}
//	"/my-skill arg1 arg2" -> SlashCommand{Name: "my-skill", Args: []string{"arg1", "arg2"}}
//	"hello" -> nil (not a command)
//	"/" -> nil (no command name)
func ParseSlash(input string) *SlashCommand {
	input = strings.TrimSpace(input)

	// Must start with /
	if !strings.HasPrefix(input, "/") {
		return nil
	}

	// Remove the leading slash
	rest := input[1:]
	if rest == "" {
		return nil
	}

	// Split into parts by whitespace
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return nil
	}

	// First part is the command name
	name := parts[0]

	// Validate command name: must be non-empty and contain only
	// alphanumeric characters, hyphens, or underscores
	if !isValidCommandName(name) {
		return nil
	}

	// Remaining parts are arguments
	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}

	return &SlashCommand{
		Name: name,
		Args: args,
	}
}

// isValidCommandName checks if a command name is valid.
// Valid names contain only alphanumeric characters, hyphens, and underscores.
func isValidCommandName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !isCommandNameChar(r) {
			return false
		}
	}
	return true
}

// isCommandNameChar returns true if the rune is valid in a command name.
func isCommandNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' ||
		r == '_'
}

// BuiltinCommands returns a list of built-in command names for autocomplete.
func BuiltinCommands() []string {
	commands := make([]string, 0, len(builtinCommands))
	for name := range builtinCommands {
		commands = append(commands, name)
	}
	// Sort for consistent ordering
	sortStrings(commands)
	return commands
}

// IsBuiltin returns true if the command is a built-in (not a skill).
func IsBuiltin(name string) bool {
	_, ok := builtinCommands[name]
	return ok
}

// SortStrings sorts a slice of strings in place using simple insertion sort.
// This avoids importing the sort package for a small slice.
func SortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// sortStrings is an alias for SortStrings, used internally.
var sortStrings = SortStrings

// IsSlashCommand checks if input starts with a slash command.
func IsSlashCommand(input string) bool {
	input = strings.TrimSpace(input)
	return strings.HasPrefix(input, "/") && len(input) > 1
}
