// Package tui provides the terminal user interface for meept.
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/sharedclient"
)

// SlashAutocomplete is a type-ahead autocomplete popup for slash commands.
// It appears when the user types "/" at the start of the input.
// Uses sharedclient.SlashAutocomplete for data management.
type SlashAutocomplete struct {
	data    *sharedclient.SlashAutocomplete // Shared data layer
	visible bool
	styles  *Styles
}

// NewSlashAutocomplete creates a new autocomplete component.
func NewSlashAutocomplete(styles *Styles) *SlashAutocomplete {
	return &SlashAutocomplete{
		data:    sharedclient.NewSlashAutocomplete(),
		visible: false,
		styles:  styles,
	}
}

// Show makes the autocomplete visible with the given filter.
func (s *SlashAutocomplete) Show(filter string) {
	s.visible = true
	s.data.Show(filter)
}

// Hide hides the autocomplete.
func (s *SlashAutocomplete) Hide() {
	s.visible = false
	s.data.Hide()
}

// IsVisible returns whether the autocomplete is visible.
func (s *SlashAutocomplete) IsVisible() bool {
	return s.visible
}

// SetFilter updates the filter and recomputes matching commands.
func (s *SlashAutocomplete) SetFilter(filter string) {
	s.data.SetFilter(filter)
}

// GetSelectedCommand returns the currently selected command name.
func (s *SlashAutocomplete) GetSelectedCommand() string {
	return s.data.GetSelectedCommand()
}

// GetSelectedCommandWithSlash returns the selected command with leading slash.
func (s *SlashAutocomplete) GetSelectedCommandWithSlash() string {
	return s.data.GetSelectedCommandWithSlash()
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
		s.data.Up()
		return HandleKeyNavigated, nil
	case KeyDown, "ctrl+j", KeyTab:
		s.data.Down()
		return HandleKeyNavigated, nil
	case KeyEnter:
		// Insert selected command and return it for execution
		if cmd, ok := s.data.Select(); ok {
			return HandleKeyInsert, func() tea.Msg {
				return SlashAutocompleteMsg{Command: cmd}
			}
		}
		return HandleKeyPassThrough, nil
	case KeyEsc:
		s.Hide()
		return HandleKeyNavigated, nil
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
	if !s.visible {
		return ""
	}

	// Use sharedclient's GetVisibleItems for scrolling logic
	items, _, selectedInItems := s.data.GetVisibleItems()
	if len(items) == 0 {
		return ""
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
	for i, cmd := range items {
		style := s.styles.ModalItem
		if i == selectedInItems {
			style = s.styles.ModalItemSelected
		}

		// Show match indicator
		matchMarker := " "
		if i == selectedInItems {
			matchMarker = "▸"
		}

		// Highlight the matched portion
		filter := s.data.FilterText()
		var label string
		if strings.HasPrefix(cmd, filter) && filter != "" {
			matchedLen := min(len(filter), len(cmd))
			label = s.styles.HelpKey.Render(cmd[:matchedLen]) + cmd[matchedLen:]
		} else {
			label = cmd
		}

		line := matchMarker + " " + label
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	// Footer hint
	if len(items) > 1 {
		b.WriteString("\n")
		hint := "↑/↓ navigate · enter select · esc cancel"
		if s.data.VisibleCount() > s.data.MaxHeight() {
			hint += " · " + lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("scrolling")
		}
		b.WriteString(s.styles.Muted.Render(hint))
	}

	return boxStyle.Render(b.String())
}

// UpdateCommands refreshes the command list (e.g., after skills are installed).
func (s *SlashAutocomplete) UpdateCommands(commands []string) {
	s.data.UpdateCommands(commands)
}

// MergeCommands adds extra commands (template names, skill names) to the
// existing built-in command list. Duplicates are removed. The merged list
// is sorted for consistent autocomplete ordering.
func (s *SlashAutocomplete) MergeCommands(extra []string) {
	s.data.MergeCommands(extra)
}

// GetFilteredCommands returns the currently filtered commands.
func (s *SlashAutocomplete) GetFilteredCommands() []string {
	return s.data.GetFilteredCommands()
}

// GetSelectedIndex returns the currently selected index.
func (s *SlashAutocomplete) GetSelectedIndex() int {
	_, _, selectedInItems := s.data.GetVisibleItems()
	return selectedInItems
}
