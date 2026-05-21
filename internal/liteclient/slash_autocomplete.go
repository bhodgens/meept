package liteclient

import (
	"strings"
)

// SlashAutocomplete provides type-ahead autocomplete for slash commands.
type SlashAutocomplete struct {
	visible   bool
	commands  []string // All available commands
	filtered  []string // Commands matching current filter
	selected  int      // Currently selected index
	filter    string   // Current filter text (what user typed after /)
	maxHeight int      // Maximum visible items before scrolling
}

// NewSlashAutocomplete creates a new autocomplete component.
func NewSlashAutocomplete() *SlashAutocomplete {
	commands := BuiltinCommands()

	return &SlashAutocomplete{
		visible:   false,
		commands:  commands,
		filtered:  commands,
		selected:  0,
		filter:    "",
		maxHeight: 8,
	}
}

// Show makes the autocomplete visible with the given filter.
func (s *SlashAutocomplete) Show(filter string) {
	s.visible = true
	s.filter = filter
	s.updateFiltered()
	s.selected = 0
}

// Hide hides the autocomplete.
func (s *SlashAutocomplete) Hide() {
	s.visible = false
}

// IsVisible returns whether the autocomplete is visible.
func (s *SlashAutocomplete) IsVisible() bool {
	return s.visible
}

// SetFilter updates the filter and recomputes matching commands.
func (s *SlashAutocomplete) SetFilter(filter string) {
	s.filter = filter
	s.updateFiltered()
	if len(s.filtered) > 0 && s.selected >= len(s.filtered) {
		s.selected = len(s.filtered) - 1
	}
}

// updateFiltered recomputes the list of commands matching the current filter.
func (s *SlashAutocomplete) updateFiltered() {
	if s.filter == "" {
		s.filtered = make([]string, len(s.commands))
		copy(s.filtered, s.commands)
		return
	}

	s.filtered = s.filtered[:0]
	for _, cmd := range s.commands {
		if strings.HasPrefix(cmd, s.filter) {
			s.filtered = append(s.filtered, cmd)
		}
	}
}

// GetSelectedCommand returns the currently selected command name.
func (s *SlashAutocomplete) GetSelectedCommand() string {
	if s.selected >= 0 && s.selected < len(s.filtered) {
		return s.filtered[s.selected]
	}
	return ""
}

// GetSelectedCommandWithSlash returns the selected command with leading slash.
func (s *SlashAutocomplete) GetSelectedCommandWithSlash() string {
	cmd := s.GetSelectedCommand()
	if cmd != "" {
		return "/" + cmd
	}
	return ""
}

// Up moves selection up.
func (s *SlashAutocomplete) Up() {
	if s.selected > 0 {
		s.selected--
	}
}

// Down moves selection down.
func (s *SlashAutocomplete) Down() {
	if s.selected < len(s.filtered)-1 {
		s.selected++
	}
}

// Select returns the selected command and resets state.
func (s *SlashAutocomplete) Select() (string, bool) {
	if len(s.filtered) > 0 && s.selected >= 0 && s.selected < len(s.filtered) {
		return s.GetSelectedCommandWithSlash(), true
	}
	return "", false
}

// UpdateCommands refreshes the command list (e.g., after skills are installed).
func (s *SlashAutocomplete) UpdateCommands(commands []string) {
	merged := make([]string, 0, len(s.commands)+len(commands))
	seen := make(map[string]struct{})

	for _, cmd := range s.commands {
		if _, ok := seen[cmd]; !ok {
			seen[cmd] = struct{}{}
			merged = append(merged, cmd)
		}
	}

	for _, cmd := range commands {
		if _, ok := seen[cmd]; !ok {
			seen[cmd] = struct{}{}
			merged = append(merged, cmd)
		}
	}

	sortStrings(merged)
	s.commands = merged
	s.updateFiltered()
}

// GetFilteredCommands returns the currently filtered commands.
func (s *SlashAutocomplete) GetFilteredCommands() []string {
	return s.filtered
}

// GetSelectedIndex returns the currently selected index.
func (s *SlashAutocomplete) GetSelectedIndex() int {
	return s.selected
}

// GetVisibleItems returns the items to display (handles scrolling).
func (s *SlashAutocomplete) GetVisibleItems() ([]string, int, int) {
	if len(s.filtered) <= s.maxHeight {
		return s.filtered, 0, len(s.filtered)
	}

	// Keep selected item visible
	startIdx := 0
	if s.selected >= s.maxHeight {
		startIdx = s.selected - s.maxHeight + 1
	}
	endIdx := startIdx + s.maxHeight
	if endIdx > len(s.filtered) {
		endIdx = len(s.filtered)
		startIdx = endIdx - s.maxHeight
	}

	return s.filtered[startIdx:endIdx], startIdx, s.selected - startIdx
}
