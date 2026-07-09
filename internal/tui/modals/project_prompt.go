// Package modals provides TUI modal dialogs.
package modals

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// ProjectPromptModal shows a confirmation dialog for project binding.
type ProjectPromptModal struct {
	sessionID   string
	defaultPath string
	selected    int // 0=Yes, 1=No, 2=Pick
	styles      ModalStyles
}

// ModalStyles holds styling for modals.
type ModalStyles struct {
	Selected   string
	Unselected string
}

// NewProjectPromptModal creates a new prompt.
func NewProjectPromptModal(sessionID, defaultPath string) *ProjectPromptModal {
	return &ProjectPromptModal{
		sessionID:   sessionID,
		defaultPath: defaultPath,
		selected:    0,
		styles: ModalStyles{
			Selected:   "[✓]",
			Unselected: "[ ]",
		},
	}
}

// Init initializes the modal.
func (m *ProjectPromptModal) Init() tea.Cmd {
	return nil
}

// Update handles key events.
func (m *ProjectPromptModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			return m, func() tea.Msg {
				return ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: true}
			}
		case "n", "N":
			return m, func() tea.Msg {
				return ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: false}
			}
		case "p", "P":
			return m, func() tea.Msg {
				return ProjectPickRequestedMsg{SessionID: m.sessionID}
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < 2 {
				m.selected++
			}
		case "enter":
			switch m.selected {
			case 0:
				return m, func() tea.Msg {
					return ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: true}
				}
			case 1:
				return m, func() tea.Msg {
					return ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: false}
				}
			case 2:
				return m, func() tea.Msg {
					return ProjectPickRequestedMsg{SessionID: m.sessionID}
				}
			}
		case "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

// View renders the modal.
func (m *ProjectPromptModal) View() tea.View {
	yesMark := m.styles.Unselected
	noMark := m.styles.Unselected
	pickMark := m.styles.Unselected

	switch m.selected {
	case 0:
		yesMark = m.styles.Selected
	case 1:
		noMark = m.styles.Selected
	case 2:
		pickMark = m.styles.Selected
	}

	return tea.NewView(fmt.Sprintf(`
╭─────────────────────────────────────────╮
│  Session has no project bound           │
│                                         │
│  Use current directory for execution?   │
│    %s                                     │
│                                         │
│  %s Yes                                 │
│  %s No (run without project)            │
│  %s Pick project...                     │
│                                         │
│  [y/n/p or ↑/↓ + Enter]                 │
╰─────────────────────────────────────────╯
`, m.defaultPath, yesMark, noMark, pickMark))
}

// ProjectConfirmedMsg signals user's choice.
type ProjectConfirmedMsg struct {
	SessionID string
	Accepted  bool
}

// ProjectPickRequestedMsg signals user wants to pick project.
type ProjectPickRequestedMsg struct {
	SessionID string
}
