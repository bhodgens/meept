// Package tui provides the terminal user interface for meept.
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SlashAutocomplete is a type-ahead autocomplete popup for slash commands.
// It appears when the user types "/" at the start of the input.
type SlashAutocomplete struct {
	visible    bool
	commands   []string // All available commands
	filtered   []string // Commands matching current filter
	selected   int      // Currently selected index
	filter     string   // Current filter text (what user typed after /)
	maxHeight  int      // Maximum visible items before scrolling
	styles     *Styles
}

// NewSlashAutocomplete creates a new autocomplete component.
func NewSlashAutocomplete(styles *Styles) *SlashAutocomplete {
	commands := BuiltinCommands()
	// Also add skill commands if needed (for now just builtins)

	return &SlashAutocomplete{
		visible:   false,
		commands:  commands,
		filtered:  commands,
		selected:  0,
		filter:    "",
		maxHeight: 8,
		styles:    styles,
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

// HandleKeyResult indicates how a key was handled by the autocomplete.
type HandleKeyResult int

const (
	HandleKeyPassThrough HandleKeyResult = iota // Key should be passed through to input
	HandleKeyNavigated                          // Key was navigation (up/down/etc.)
	HandleKeyInsert                             // Key should insert selected command
)

// HandleKey processes key input for navigation and selection.
// Returns HandleKeyInsert (with non-nil cmd) to insert the selected command,
// HandleKeyNavigated to consume the key as navigation,
// or HandleKeyPassThrough to let the key pass through to normal input.
func (s *SlashAutocomplete) HandleKey(key string) (HandleKeyResult, tea.Cmd) {
	if !s.visible {
		return HandleKeyPassThrough, nil
	}

	switch key {
	case "up", "ctrl+k":
		if s.selected > 0 {
			s.selected--
		}
		return HandleKeyNavigated, nil
	case "down", "ctrl+j", "tab":
		if s.selected < len(s.filtered)-1 {
			s.selected++
		}
		return HandleKeyNavigated, nil
	case "enter":
		// Insert selected command and return it for execution
		if len(s.filtered) > 0 {
			cmd := s.GetSelectedCommandWithSlash()
			return HandleKeyInsert, func() tea.Msg {
				return SlashAutocompleteMsg{Command: cmd}
			}
		}
		return HandleKeyPassThrough, nil
	case "esc":
		s.Hide()
		return HandleKeyPassThrough, nil
	}

	// Any other key - pass through to input (autocomplete will be hidden by caller)
	return HandleKeyPassThrough, nil
}

// SlashAutocompleteMsg carries a command from the autocomplete popup.
type SlashAutocompleteMsg struct {
	Command string // Full command with leading slash (e.g., "/help")
}

// View renders the autocomplete popup.
// Returns empty string if not visible.
func (s *SlashAutocomplete) View() string {
	if !s.visible || len(s.filtered) == 0 {
		return ""
	}

	// Calculate which items to show (support scrolling if many results)
	startIdx := 0
	endIdx := len(s.filtered)
	if len(s.filtered) > s.maxHeight {
		// Keep selected item visible
		if s.selected >= s.maxHeight {
			startIdx = s.selected - s.maxHeight + 1
		}
		endIdx = startIdx + s.maxHeight
	}

	var b strings.Builder

	// Box style
	boxStyle := s.styles.ModalBox.Width(40)

	// Header
	b.WriteString(s.styles.ModalTitle.Render("commands"))
	b.WriteString("\n")
	b.WriteString(s.styles.Muted.Render(strings.Repeat("─", 36)))
	b.WriteString("\n")

	// Items
	for i := startIdx; i < endIdx && i < len(s.filtered); i++ {
		cmd := s.filtered[i]

		style := s.styles.ModalItem
		if i == s.selected {
			style = s.styles.ModalItemSelected
		}

		// Show match indicator
		matchMarker := " "
		if i == s.selected {
			matchMarker = "▸"
		}

		// Highlight the matched portion
		var label string
		if strings.HasPrefix(cmd, s.filter) && s.filter != "" {
			matchedLen := len(s.filter)
			if matchedLen > len(cmd) {
				matchedLen = len(cmd)
			}
			label = s.styles.HelpKey.Render(cmd[:matchedLen]) + cmd[matchedLen:]
		} else {
			label = cmd
		}

		line := matchMarker + " " + label
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	// Footer hint
	if len(s.filtered) > 1 {
		b.WriteString("\n")
		hint := "↑/↓ navigate · enter select · esc cancel"
		if len(s.filtered) > s.maxHeight {
			hint += " · " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("scrolling")
		}
		b.WriteString(s.styles.Muted.Render(hint))
	}

	return boxStyle.Render(b.String())
}

// UpdateCommands refreshes the command list (e.g., after skills are installed).
func (s *SlashAutocomplete) UpdateCommands(commands []string) {
	s.commands = commands
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
