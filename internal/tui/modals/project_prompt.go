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
	visible     bool
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

// Show arms the modal for the given session/path and makes it visible.
// Mirrors the sibling-modal Show pattern (SessionRenameModal, ConfirmModal):
// the host App owns visibility through activeModal, the modal keeps its own
// flag so View can short-circuit.
func (m *ProjectPromptModal) Show(sessionID, defaultPath string) {
	m.sessionID = sessionID
	m.defaultPath = defaultPath
	m.selected = 0
	m.visible = true
}

// Hide hides the modal.
func (m *ProjectPromptModal) Hide() {
	m.visible = false
}

// IsVisible reports whether the modal is showing.
func (m *ProjectPromptModal) IsVisible() bool {
	return m.visible
}

// Update handles key events.
func (m *ProjectPromptModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.visible = false
			return m, func() tea.Msg {
				return ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: true}
			}
		case "n", "N":
			m.visible = false
			return m, func() tea.Msg {
				return ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: false}
			}
		case "p", "P":
			m.visible = false
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
				m.visible = false
				return m, func() tea.Msg {
					return ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: true}
				}
			case 1:
				m.visible = false
				return m, func() tea.Msg {
					return ProjectConfirmedMsg{SessionID: m.sessionID, Accepted: false}
				}
			case 2:
				m.visible = false
				return m, func() tea.Msg {
					return ProjectPickRequestedMsg{SessionID: m.sessionID}
				}
			}
		case "esc":
			// Dismiss, never quit: as an embedded App modal, esc closes the
			// dialog (session stays unbound, matching the GUI's skip path),
			// it must not tear down the whole program.
			m.visible = false
			return m, func() tea.Msg {
				return ProjectDismissedMsg{SessionID: m.sessionID}
			}
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

// ProjectDismissedMsg signals the user dismissed the prompt (esc): the
// session stays unbound and execution proceeds without a project, matching
// the GUI dialog's skip path.
type ProjectDismissedMsg struct {
	SessionID string
}

// SessionDefaultPath returns the default path the prompt was armed with (the
// detected CWD or the "(no project detected)" placeholder). The App reads it
// when the user accepts, to bind via the path-based project.set — the same
// payload the GUI's accept path sends.
func (m *ProjectPromptModal) SessionDefaultPath() string {
	return m.defaultPath
}

// OpenProjectPromptMsg asks the App to open the project-binding prompt for a
// session. Emitted on session switch when the session carries no project
// context (TUI/GUI parity with SessionProjectChecker.checkAndPrompt), or via
// the manual ctrl+g binding.
type OpenProjectPromptMsg struct {
	SessionID   string
	DefaultPath string
}
