package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ConfirmationModel is a bubbletea model for the destructive-tool confirmation
// modal. It receives the phase-1 response map from ConfirmationResponse and
// renders a modal box per the epistemic spec.
//
// Keys: y confirms, n/esc cancels, v toggles detail view.
type ConfirmationModel struct {
	response    map[string]any
	confirmed   bool
	cancelled   bool
	showDetail  bool
	width       int
}

// NewConfirmationModel constructs a ConfirmationModel from a phase-1 response map.
func NewConfirmationModel(response map[string]any) ConfirmationModel {
	return ConfirmationModel{
		response:   response,
		width:      60,
		showDetail: false,
	}
}

// Init returns the initial command (nil for this modal).
func (m ConfirmationModel) Init() tea.Cmd {
	return nil
}

// Update handles keypresses.
func (m ConfirmationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.confirmed = true
			return m, tea.Quit
		case "n", "N", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "v", "V":
			m.showDetail = !m.showDetail
			return m, nil
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	}
	return m, nil
}

// View renders the confirmation modal. Returns tea.View per bubbletea v2.
func (m ConfirmationModel) View() tea.View {
	action := asStringField(m.response, "action")
	summary := asStringField(m.response, "summary")
	reversible, _ := m.response["reversible"].(bool)

	reversibility := "no"
	if reversible {
		reversibility = "yes"
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(0, 1).
		Width(m.width)

	var b strings.Builder

	// Header line
	b.WriteString(fmt.Sprintf("  %s — confirm action\n\n", action))
	b.WriteString(fmt.Sprintf("  %s\n\n", summary))

	// Detail previews (if present in the response)
	details, _ := m.response["details"].(map[string]any)
	if old := asStringField(details, "old_preview"); old != "" {
		b.WriteString(fmt.Sprintf("  OLD: %s\n", truncateForDisplay(old, m.width-8)))
	}
	if newText := asStringField(details, "new_preview"); newText != "" {
		b.WriteString(fmt.Sprintf("  NEW: %s\n", truncateForDisplay(newText, m.width-8)))
	}
	if edges := asStringField(details, "affected_edges"); edges != "" {
		b.WriteString(fmt.Sprintf("  %s edges will be redirected.\n", edges))
	}

	b.WriteString(fmt.Sprintf("  reversible: %s\n\n", reversibility))

	if m.showDetail {
		b.WriteString("  --- full details ---\n")
		for k, v := range m.response {
			if k == "details" {
				continue
			}
			b.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
		}
		for k, v := range details {
				b.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
			}
		b.WriteString("\n")
	}

	// Footer with keybindings — all lowercase per CLAUDE.md
	b.WriteString("  [y] confirm    [n] cancel    [v] view full details")

	return tea.NewView(borderStyle.Render(b.String()))
}

// IsConfirmed reports whether the user confirmed the action.
func (m ConfirmationModel) IsConfirmed() bool {
	return m.confirmed
}

// IsCancelled reports whether the user cancelled the action.
func (m ConfirmationModel) IsCancelled() bool {
	return m.cancelled
}

// asStringField extracts a string field from a map, returning "" if missing or wrong type.
func asStringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", val)
	}
}

// truncateForDisplay caps a string at maxLen characters, appending "..." if truncated.
func truncateForDisplay(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
