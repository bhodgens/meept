// Package tui provides the terminal user interface for meept.
// This file re-exports slash command parsing from internal/sharedclient
// to eliminate code duplication.
package tui

import (
	"github.com/caimlas/meept/internal/sharedclient"
)

// SlashCommand represents a parsed slash command.
// Re-exported from sharedclient for backward compatibility.
type SlashCommand = sharedclient.SlashCommand

// ParseSlash parses a slash command from input text.
// Re-exported from sharedclient for backward compatibility.
var ParseSlash = sharedclient.ParseSlash

// BuiltinCommands returns a list of built-in command names for autocomplete.
// Re-exported from sharedclient for backward compatibility.
var BuiltinCommands = sharedclient.BuiltinCommands

// IsBuiltin returns true if the command is a built-in (not a skill).
// Re-exported from sharedclient for backward compatibility.
var IsBuiltin = sharedclient.IsBuiltin

// IsSlashCommand checks if input starts with a slash command.
// Re-exported from sharedclient for backward compatibility.
var IsSlashCommand = sharedclient.IsSlashCommand

// Note: CmdTasks is defined in constants.go for backward compatibility

// sortStrings sorts a slice of strings in place using simple insertion sort.
// This is kept for backward compatibility with slash_autocomplete.go
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
