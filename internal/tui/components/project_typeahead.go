package components

import (
	"strings"

	teacmd "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/caimlas/meept/internal/tui"
)

// ProjectTypeaheadModel is the typeahead state for project path selection.
type ProjectTypeaheadModel struct {
	textInput     textinput.Model
	recents       []string
	filtered      []string
	selected      string
	selectedIndex int
	callback      func(path string) teacmd.Cmd
	width         int
}

// NewProjectTypeahead creates a new typeahead component.
func NewProjectTypeahead(callback func(path string) teacmd.Cmd) *ProjectTypeaheadModel {
	ti := textinput.New()
	ti.Placeholder = "enter project path..."
	ti.Focus()
	ti.SetWidth(60)

	return &ProjectTypeaheadModel{
		textInput: ti,
		callback:  callback,
		width:     60,
	}
}

// WithWidth sets the max display width and resizes the text input.
func (m *ProjectTypeaheadModel) WithWidth(w int) {
	m.width = w
	m.textInput.SetWidth(w)
}

// Init initializes the typeahead.
func (m *ProjectTypeaheadModel) Init() teacmd.Cmd {
	return textinput.Blink
}

// Update handles messages.
func (m *ProjectTypeaheadModel) Update(msg teacmd.Msg) teacmd.Cmd {
	switch msg := msg.(type) {
	case teacmd.KeyPressMsg:
		switch msg.String() {
		case "enter":
			if m.selected != "" {
				return m.callback(m.selected)
			}
			// If nothing selected, submit the typed value
			val := m.textInput.Value()
			if val != "" {
				return m.callback(val)
			}
			return nil
		case "esc":
			// Escape closes the typeahead — return nil cmd
			return nil
		case "up":
			if len(m.filtered) > 0 {
				m.selectedIndex--
				if m.selectedIndex < 0 {
					m.selectedIndex = len(m.filtered) - 1
				}
				m.selected = m.filtered[m.selectedIndex]
				return nil
			}
		case "down":
			if len(m.filtered) > 0 {
				m.selectedIndex++
				if m.selectedIndex >= len(m.filtered) {
					m.selectedIndex = 0
				}
				m.selected = m.filtered[m.selectedIndex]
				return nil
			}
		}
	}

	var cmd teacmd.Cmd
	m.textInput, cmd = m.textInput.Update(msg)

	// Re-filter recents on text change
	prefix := m.textInput.Value()
	m.filtered = make([]string, 0, len(m.recents))
	for _, r := range m.recents {
		if prefix == "" || strings.Contains(r, prefix) {
			m.filtered = append(m.filtered, r)
		}
	}

	// Reset selection when filter changes
	if len(m.filtered) == 0 {
		m.selected = ""
		m.selectedIndex = 0
	} else if m.selected != "" {
		idx := -1
		for i, item := range m.filtered {
			if item == m.selected {
				idx = i
				break
			}
		}
		if idx == -1 {
			m.selected = m.filtered[0]
			m.selectedIndex = 0
		} else {
			m.selectedIndex = idx
		}
	}

	return cmd
}

// View renders the typeahead.
func (m *ProjectTypeaheadModel) View() string {
	var b strings.Builder
	b.WriteString(m.textInput.View())
	if len(m.filtered) > 0 {
		b.WriteString("\n")
	}
	for i, item := range m.filtered {
		if i >= 5 {
			break
		}
		if i == m.selectedIndex {
			indicatorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F97316")).
				Bold(true)
			b.WriteString(indicatorStyle.Render("> "))
		} else {
			b.WriteString("  ")
		}
		b.WriteString(item)
		b.WriteString("\n")
	}
	return b.String()
}

// SetRecents updates the recents list and re-filters visible options.
func (m *ProjectTypeaheadModel) SetRecents(recents []string) {
	m.recents = recents
	prefix := m.textInput.Value()
	m.filtered = make([]string, 0, len(recents))
	for _, r := range recents {
		if prefix == "" || strings.Contains(r, prefix) {
			m.filtered = append(m.filtered, r)
		}
	}
	if len(m.filtered) == 0 {
		m.selected = ""
		m.selectedIndex = 0
	} else {
		m.selectedIndex = 0
		m.selected = m.filtered[0]
	}
}
