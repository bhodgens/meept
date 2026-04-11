// Package lite provides the lightweight terminal UI for meept-lite.
package lite

import (
	"sort"
	"strings"
)

// Autocompleter handles tab completion for slash commands in meept-lite.
// It supports completing built-in commands and available skills when input
// starts with '/'. On repeated Tab presses, it cycles through matching completions.
type Autocompleter struct {
	commands []string // available command names (without leading /)
	skills   []string // available skill names (without leading /)
	matches  []string // current matches (full completions including /)
	matchIdx int      // current match index
	prefix   string   // the prefix being completed (including /)
}

// NewAutocompleter creates a new Autocompleter instance.
func NewAutocompleter() *Autocompleter {
	return &Autocompleter{
		commands: []string{},
		skills:   []string{},
		matches:  []string{},
		matchIdx: 0,
		prefix:   "",
	}
}

// SetCommands sets the list of available built-in commands.
// Commands should be provided without the leading '/' prefix.
func (a *Autocompleter) SetCommands(cmds []string) {
	a.commands = make([]string, len(cmds))
	copy(a.commands, cmds)
	sort.Strings(a.commands)
}

// SetSkills sets the list of available skills.
// Skills should be provided without the leading '/' prefix.
func (a *Autocompleter) SetSkills(skills []string) {
	a.skills = make([]string, len(skills))
	copy(a.skills, skills)
	sort.Strings(a.skills)
}

// Complete takes current input and returns completed text.
// If multiple matches exist, cycles through them on repeated calls with the same prefix.
// Returns (completed text, true) if completion occurred, (original, false) if not.
//
// Behavior:
//   - If input doesn't start with '/', returns (input, false)
//   - If input is just '/', returns (input, false)
//   - If input is '/he' and Tab pressed, completes to '/help'
//   - If multiple matches (e.g., '/s' matches '/session', '/skills'), cycles through
//   - Completion state resets when input prefix changes
func (a *Autocompleter) Complete(input string) (string, bool) {
	// Must start with / for slash command completion
	if !strings.HasPrefix(input, "/") {
		a.Reset()
		return input, false
	}

	// Just '/' - no completion possible
	if len(input) == 1 {
		a.Reset()
		return input, false
	}

	// Extract the command prefix (everything after /)
	prefix := strings.ToLower(input[1:])

	// Check if this is a continuation of the same completion session
	if a.prefix == input && len(a.matches) > 0 {
		// Same prefix, cycle to next match
		a.matchIdx = (a.matchIdx + 1) % len(a.matches)
		return a.matches[a.matchIdx], true
	}

	// New prefix - find all matching completions
	a.prefix = input
	a.matches = a.findMatches(prefix)
	a.matchIdx = 0

	if len(a.matches) == 0 {
		return input, false
	}

	return a.matches[a.matchIdx], true
}

// Reset clears the current completion state.
// Should be called when the user modifies input (other than via Tab completion).
func (a *Autocompleter) Reset() {
	a.matches = nil
	a.matchIdx = 0
	a.prefix = ""
}

// findMatches returns all commands and skills that start with the given prefix.
// Results are returned with the leading '/' prefix, sorted alphabetically
// with commands appearing before skills.
func (a *Autocompleter) findMatches(prefix string) []string {
	var matches []string

	// Match against commands first (they take priority)
	for _, cmd := range a.commands {
		if strings.HasPrefix(strings.ToLower(cmd), prefix) {
			matches = append(matches, "/"+cmd)
		}
	}

	// Then match against skills
	for _, skill := range a.skills {
		if strings.HasPrefix(strings.ToLower(skill), prefix) {
			matches = append(matches, "/"+skill)
		}
	}

	return matches
}

// GetMatches returns the current list of matches.
// Useful for displaying completion suggestions to the user.
func (a *Autocompleter) GetMatches() []string {
	result := make([]string, len(a.matches))
	copy(result, a.matches)
	return result
}

// GetMatchCount returns the number of current matches.
func (a *Autocompleter) GetMatchCount() int {
	return len(a.matches)
}

// GetCurrentMatchIndex returns the index of the currently selected match.
// Returns -1 if there are no matches.
func (a *Autocompleter) GetCurrentMatchIndex() int {
	if len(a.matches) == 0 {
		return -1
	}
	return a.matchIdx
}

// HasMatches returns true if there are any completion matches.
func (a *Autocompleter) HasMatches() bool {
	return len(a.matches) > 0
}
