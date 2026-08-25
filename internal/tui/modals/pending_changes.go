// Package modals provides TUI modal dialogs.
package modals

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// PendingChange is a staged file change awaiting human review. It mirrors
// the pending-change list representation used by the change review API:
// a diff preview, never full file contents.
type PendingChange struct {
	ID        string `json:"id"`
	FilePath  string `json:"file_path"`
	Diff      string `json:"diff"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// PendingChangeAPI is the small surface the pending changes modal needs,
// defined here so the modal has no concrete dependency on the daemon-side
// registry or RPC client. The TUI wires an RPC-backed implementation.
type PendingChangeAPI interface {
	ListPendingChanges(sessionID string) ([]PendingChange, error)
	AcceptPendingChange(id string) error
	RejectPendingChange(id string) error
}

// PendingChangesListMsg carries a refreshed pending-changes list.
type PendingChangesListMsg struct {
	Changes []PendingChange
	Err     error
}

// PendingChangeActionResultMsg carries the outcome of an accept or reject.
type PendingChangeActionResultMsg struct {
	ID     string
	Action string // "accept" or "reject"
	Err    error
}

// PendingChangesCountMsg carries the number of pending changes for the
// status bar indicator.
type PendingChangesCountMsg struct {
	Count int
}

// PendingChangesModal lists the staged pending changes of the current
// session and lets the user review each diff and accept or reject it.
// Navigation: j/k move, v toggles the full diff view, a accepts, r
// rejects, esc closes (or leaves diff view first). All UI strings are
// lowercase.
type PendingChangesModal struct {
	api       PendingChangeAPI
	sessionID string

	changes   []PendingChange
	selected  int
	visible   bool
	diffMode  bool
	scrollOff int

	width      int
	maxVisible int
}

// NewPendingChangesModal creates a modal backed by api. api may be nil
// (e.g. daemon not connected); list fetches then report an error message
// instead of panicking.
func NewPendingChangesModal(api PendingChangeAPI) *PendingChangesModal {
	return &PendingChangesModal{
		api:        api,
		width:      90,
		maxVisible: 15,
	}
}

// Show makes the modal visible for sessionID and returns a command that
// fetches the session's pending changes.
func (m *PendingChangesModal) Show(sessionID string) tea.Cmd {
	m.sessionID = sessionID
	m.visible = true
	m.selected = 0
	m.diffMode = false
	m.scrollOff = 0
	return m.fetchList()
}

// Refresh re-fetches the pending changes for the session shown last.
func (m *PendingChangesModal) Refresh() tea.Cmd {
	return m.fetchList()
}

// FetchCount returns a command that reports how many changes are pending
// for sessionID (0 on any error) so the status bar can show an indicator.
func (m *PendingChangesModal) FetchCount(sessionID string) tea.Cmd {
	api := m.api
	return func() tea.Msg {
		if api == nil {
			return PendingChangesCountMsg{Count: 0}
		}
		changes, err := api.ListPendingChanges(sessionID)
		if err != nil {
			return PendingChangesCountMsg{Count: 0}
		}
		return PendingChangesCountMsg{Count: len(changes)}
	}
}

func (m *PendingChangesModal) fetchList() tea.Cmd {
	api := m.api
	sessionID := m.sessionID
	return func() tea.Msg {
		if api == nil {
			return PendingChangesListMsg{Err: fmt.Errorf("pending changes api unavailable")}
		}
		changes, err := api.ListPendingChanges(sessionID)
		return PendingChangesListMsg{Changes: changes, Err: err}
	}
}

// SetChanges installs a fetched list and clamps the selection.
func (m *PendingChangesModal) SetChanges(changes []PendingChange) {
	m.changes = changes
	if m.selected >= len(changes) {
		m.selected = len(changes) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.diffMode && len(changes) == 0 {
		m.diffMode = false
	}
}

// Hide closes the modal.
func (m *PendingChangesModal) Hide() {
	m.visible = false
}

// IsVisible reports whether the modal is open.
func (m *PendingChangesModal) IsVisible() bool {
	return m.visible
}

// SelectedIndex exposes the current selection for tests.
func (m *PendingChangesModal) SelectedIndex() int {
	return m.selected
}

// InDiffMode reports whether the full diff view is active, for tests.
func (m *PendingChangesModal) InDiffMode() bool {
	return m.diffMode
}

// HandleKey processes a key press. Accept/reject run through the injected
// PendingChangeAPI inside the returned command; navigation and view
// toggles are applied immediately.
func (m *PendingChangesModal) HandleKey(key string) tea.Cmd {
	if m.diffMode {
		switch key {
		case "v":
			m.diffMode = false
		case "j", "down":
			m.scrollOff++
		case "k", "up":
			if m.scrollOff > 0 {
				m.scrollOff--
			}
		case "esc", "q":
			// First esc leaves the diff view; a second one closes.
			m.diffMode = false
		}
		return nil
	}

	switch key {
	case "j", "down":
		if m.selected < len(m.changes)-1 {
			m.selected++
		}
	case "k", "up":
		if m.selected > 0 {
			m.selected--
		}
	case "v":
		if len(m.changes) > 0 {
			m.diffMode = true
			m.scrollOff = 0
		}
	case "a":
		return m.resolveSelected("accept")
	case "r":
		return m.resolveSelected("reject")
	case "esc", "q":
		m.visible = false
	}
	return nil
}

func (m *PendingChangesModal) resolveSelected(action string) tea.Cmd {
	if len(m.changes) == 0 || m.selected < 0 || m.selected >= len(m.changes) {
		return nil
	}
	change := m.changes[m.selected]
	api := m.api
	if api == nil {
		return func() tea.Msg {
			return PendingChangeActionResultMsg{
				ID:     change.ID,
				Action: action,
				Err:    fmt.Errorf("pending changes api unavailable"),
			}
		}
	}
	return func() tea.Msg {
		var err error
		switch action {
		case "accept":
			err = api.AcceptPendingChange(change.ID)
		case "reject":
			err = api.RejectPendingChange(change.ID)
		default:
			err = fmt.Errorf("unknown action %q", action)
		}
		return PendingChangeActionResultMsg{ID: change.ID, Action: action, Err: err}
	}
}

// View renders the modal centered on screen.
func (m *PendingChangesModal) View(screenW, screenH int) string {
	if !m.visible {
		return ""
	}

	var b strings.Builder

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(m.width)

	title := lipgloss.NewStyle().Bold(true).Width(m.width - 4).Render("pending changes")
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width-4))
	b.WriteString("\n")

	if m.diffMode {
		m.renderDiff(&b)
	} else if len(m.changes) == 0 {
		b.WriteString(lipgloss.NewStyle().Faint(true).Render("no pending changes for this session"))
		b.WriteString("\n")
	} else {
		m.renderList(&b)
	}

	// Footer hints
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width-4))
	b.WriteString("\n")
	var hints string
	if m.diffMode {
		hints = "[j/k] scroll  [v] list  [esc] back"
	} else {
		hints = "[j/k] navigate  [v] view diff  [a] accept  [r] reject  [esc] close"
	}
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(hints))

	content := box.Render(b.String())
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

func (m *PendingChangesModal) renderList(b *strings.Builder) {
	start := 0
	if m.selected >= m.maxVisible {
		start = m.selected - m.maxVisible + 1
	}
	if start > len(m.changes)-m.maxVisible {
		start = max(0, len(m.changes)-m.maxVisible)
	}
	end := min(start+m.maxVisible, len(m.changes))

	for i := start; i < end; i++ {
		change := m.changes[i]
		pointer := "  "
		line := lipgloss.NewStyle()
		if i == m.selected {
			pointer = "▸ "
			line = line.Reverse(true)
		}

		name := change.FilePath
		nameCol := m.width - 30
		if len(name) > nameCol {
			name = "..." + name[len(name)-nameCol+3:]
		}
		id := change.ID
		if len(id) > 20 {
			id = id[:20]
		}
		b.WriteString(line.Render(fmt.Sprintf("%s%-*s %s", pointer, nameCol, name, id)))
		b.WriteString("\n")
	}

	if len(m.changes) > m.maxVisible {
		b.WriteString(lipgloss.NewStyle().Faint(true).
			Render(fmt.Sprintf("  showing %d-%d of %d", start+1, end, len(m.changes))))
		b.WriteString("\n")
	}
}

func (m *PendingChangesModal) renderDiff(b *strings.Builder) {
	change := m.changes[m.selected]
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(change.FilePath))
	b.WriteString("\n")

	lines := strings.Split(strings.TrimRight(change.Diff, "\n"), "\n")
	viewH := m.maxVisible
	if m.scrollOff > max(0, len(lines)-viewH) {
		m.scrollOff = max(0, len(lines)-viewH)
	}
	for i := m.scrollOff; i < len(lines) && i < m.scrollOff+viewH; i++ {
		line := lines[i]
		if len(line) > m.width-6 {
			line = line[:m.width-6]
		}
		switch {
		case strings.HasPrefix(line, "+"):
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(line))
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	if len(lines) > viewH {
		b.WriteString(lipgloss.NewStyle().Faint(true).
			Render(fmt.Sprintf("  lines %d-%d of %d", m.scrollOff+1, min(m.scrollOff+viewH, len(lines)), len(lines))))
		b.WriteString("\n")
	}
}
