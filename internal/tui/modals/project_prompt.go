// Package modals provides TUI modal dialogs.
package modals

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// NoProjectPlaceholder is the dialog's default-path label when the session
// carries no detected CWD. It is NOT a directory: accepting it would send a
// project.set RPC with this literal and the daemon's CreateOrResolve would
// mkdir + git-init a directory literally named "(no project detected)" and
// mark it active (rpc/projects.go). The GUI never sends it — its accept path
// is guarded by cwd.isNotEmpty (session_project_provider.dart) — so the TUI
// must not either: Yes is disabled while the placeholder is showing
// (bughunt M1/A6-3, TUI/GUI parity).
const NoProjectPlaceholder = "(no project detected)"

// ProjectPromptModal shows a confirmation dialog for project binding.
type ProjectPromptModal struct {
	sessionID   string
	defaultPath string
	selected    int // 0=Yes, 1=No, 2=Pick
	visible     bool
	styles      ModalStyles
}

// yesEnabled reports whether the detected default path is a real directory
// string the accept path may bind. Empty and the "(no project detected)"
// placeholder both disable Yes (mirrors the GUI's cwd.isNotEmpty guard).
func (m *ProjectPromptModal) yesEnabled() bool {
	return m.defaultPath != "" && m.defaultPath != NoProjectPlaceholder
}

// ModalStyles holds styling for modals.
type ModalStyles struct {
	Selected   string
	Unselected string
}

// NewProjectPromptModal creates a new prompt. When defaultPath is the
// "(no project detected)" placeholder (or empty), Yes starts pre-CLEARED to
// No: with no real cwd there is nothing to accept (M1/A6-3 parity with the
// GUI's cwd.isNotEmpty guard).
func NewProjectPromptModal(sessionID, defaultPath string) *ProjectPromptModal {
	m := &ProjectPromptModal{
		sessionID:   sessionID,
		defaultPath: defaultPath,
		selected:    0,
		styles: ModalStyles{
			Selected:   "[✓]",
			Unselected: "[ ]",
		},
	}
	m.armSelection()
	return m
}

// armSelection resets the cursor for the currently-showing defaultPath:
// Yes when a real path exists, else No.
func (m *ProjectPromptModal) armSelection() {
	if m.yesEnabled() {
		m.selected = 0
		return
	}
	m.selected = 1
}

// minSelectable is the lowest cursor position the arrow keys may reach:
// 0 (Yes) when a real path exists, else 1 (No) — Yes is disabled while the
// placeholder shows, so the cursor must never rest on it.
func (m *ProjectPromptModal) minSelectable() int {
	if m.yesEnabled() {
		return 0
	}
	return 1
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
	m.armSelection()
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

// Update handles key events. While the default path is the "(no project
// detected)" placeholder, Yes is DISABLED: 'y' is ignored and the cursor
// clamps to No/Pick, so Enter can never emit Accepted=true with the
// placeholder — the App would issue project.set with a literal that is not
// a directory (M1/A6-3). Declining (n) and Pick stay available, matching the
// GUI's skip/pick paths.
func (m *ProjectPromptModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			if !m.yesEnabled() {
				// Yes disabled (no real cwd): accept would bind the
				// placeholder path — never send it.
				return m, nil
			}
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
			if m.selected > m.minSelectable() {
				m.selected--
			}
		case "down", "j":
			if m.selected < 2 {
				m.selected++
			}
		case "enter":
			switch m.selected {
			case 0:
				if !m.yesEnabled() {
					// Belt-and-braces: cursor cannot rest here while
					// disabled, never accept the placeholder.
					return m, nil
				}
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

	// Parity guard (M1/A6-3): with no real cwd, Yes is disabled and labelled
	// so — the GUI only renders the accept option when cwd.isNotEmpty.
	yesLabel := "Yes"
	if !m.yesEnabled() {
		yesMark = "[✗]"
		yesLabel = "Yes (unavailable: no cwd)"
	}

	yesRow := fmt.Sprintf("  %s %s", yesMark, yesLabel)
	return tea.NewView(fmt.Sprintf(`
╭─────────────────────────────────────────╮
│  Session has no project bound           │
│                                         │
│  Use current directory for execution?   │
│    %s                                     │
│                                         │
%-41s│
│  %s No (run without project)            │
│  %s Pick project...                     │
│                                         │
│  [y/n/p or ↑/↓ + Enter]                 │
╰─────────────────────────────────────────╯
`, m.defaultPath, yesRow, noMark, pickMark))
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
