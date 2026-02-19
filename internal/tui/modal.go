package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/types"
)

// ModalType represents the type of modal currently displayed.
type ModalType int

const (
	ModalNone ModalType = iota
	ModalCommandPalette
	ModalSessionPicker
	ModalNewSession
)

// ModalItem represents an item in a modal menu.
type ModalItem struct {
	Key         string // Keyboard shortcut (e.g., "1", "s")
	Label       string // Display text
	Description string // Optional description
	Disabled    bool   // Whether the item is disabled
}

// Modal is a centered popup modal component.
type Modal struct {
	title    string
	items    []ModalItem
	selected int
	visible  bool
	width    int
	styles   *Styles
}

// NewModal creates a new modal.
func NewModal(title string, styles *Styles) *Modal {
	return &Modal{
		title:    title,
		items:    []ModalItem{},
		selected: 0,
		visible:  false,
		width:    50,
		styles:   styles,
	}
}

// SetItems sets the modal items.
func (m *Modal) SetItems(items []ModalItem) {
	m.items = items
	m.selected = 0
}

// Show makes the modal visible.
func (m *Modal) Show() {
	m.visible = true
	m.selected = 0
}

// Hide hides the modal.
func (m *Modal) Hide() {
	m.visible = false
}

// IsVisible returns whether the modal is visible.
func (m *Modal) IsVisible() bool {
	return m.visible
}

// View renders the modal centered on screen.
func (m *Modal) View(screenW, screenH int) string {
	if !m.visible {
		return ""
	}

	content := m.renderContent()
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

func (m *Modal) renderContent() string {
	var b strings.Builder

	// Modal box style
	boxStyle := m.styles.ModalBox.Width(m.width)

	// Title
	titleStyle := m.styles.ModalTitle.Width(m.width - 4)
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")

	// Separator
	b.WriteString(m.styles.Muted.Render(strings.Repeat("─", m.width-4)))
	b.WriteString("\n")

	// Items
	for i, item := range m.items {
		style := m.styles.ModalItem
		if i == m.selected {
			style = m.styles.ModalItemSelected
		}
		if item.Disabled {
			style = m.styles.Muted
		}

		keyStyle := m.styles.HelpKey
		if i == m.selected {
			keyStyle = keyStyle.Background(lipgloss.Color("#374151"))
		}

		line := fmt.Sprintf("[%s]  %s", item.Key, item.Label)
		if item.Description != "" {
			descStyle := m.styles.Muted
			if i == m.selected {
				descStyle = descStyle.Background(lipgloss.Color("#374151"))
			}
			line += descStyle.Render(" - " + item.Description)
		}

		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	// Footer hint
	b.WriteString("\n")
	hintStyle := m.styles.Muted.Align(lipgloss.Center).Width(m.width - 4)
	b.WriteString(hintStyle.Render("Press key or Esc to cancel"))

	return boxStyle.Render(b.String())
}

// HandleKey processes a key press and returns the selected item key or empty string.
func (m *Modal) HandleKey(key string) string {
	// Check for direct key match
	for _, item := range m.items {
		if !item.Disabled && item.Key == key {
			m.Hide()
			return item.Key
		}
	}

	// Navigation keys
	switch key {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(m.items)-1 {
			m.selected++
		}
	case "enter":
		if m.selected >= 0 && m.selected < len(m.items) && !m.items[m.selected].Disabled {
			m.Hide()
			return m.items[m.selected].Key
		}
	case "esc", "q":
		m.Hide()
		return ""
	}

	return ""
}

// CommandPaletteModal creates a command palette modal with standard items.
func CommandPaletteModal(styles *Styles, config *ClientConfig) *Modal {
	m := NewModal("Command Palette", styles)
	keys := config.Keybindings.CommandPalette

	m.SetItems([]ModalItem{
		{Key: keys.ViewChat, Label: "Chat", Description: "Switch to chat view"},
		{Key: keys.ViewTasks, Label: "Tasks", Description: "Switch to tasks view"},
		{Key: keys.ViewQueue, Label: "Queue", Description: "Switch to queue view"},
		{Key: keys.ViewMemory, Label: "Memory", Description: "Switch to memory view"},
		{Key: keys.Sidebar, Label: "Toggle Sidebar", Description: "Show/hide sidebar"},
		{Key: keys.Sessions, Label: "Sessions...", Description: "Manage sessions"},
	})

	return m
}

// SessionPickerModal is a modal for selecting and managing sessions.
type SessionPickerModal struct {
	*Modal
	sessions     []types.Session
	inputMode    bool        // true when entering new session name
	inputBuffer  string      // buffer for new session name
	rpc          *RPCClient
	clientConfig *ClientConfig
}

// NewSessionPickerModal creates a new session picker modal.
func NewSessionPickerModal(styles *Styles, rpc *RPCClient, config *ClientConfig) *SessionPickerModal {
	m := NewModal("Sessions", styles)
	m.width = 55

	return &SessionPickerModal{
		Modal:        m,
		sessions:     []types.Session{},
		rpc:          rpc,
		clientConfig: config,
	}
}

// RefreshSessions fetches the session list from the daemon.
func (s *SessionPickerModal) RefreshSessions() tea.Cmd {
	return func() tea.Msg {
		if s.rpc == nil || !s.rpc.IsConnected() {
			return SessionListMsg{Sessions: nil, Err: fmt.Errorf("not connected")}
		}

		resp, err := s.rpc.ListSessions()
		if err != nil {
			return SessionListMsg{Sessions: nil, Err: err}
		}

		return SessionListMsg{Sessions: resp.Sessions, Err: nil}
	}
}

// SessionListMsg carries the session list response.
type SessionListMsg struct {
	Sessions []types.Session
	Err      error
}

// SetSessions updates the session list and rebuilds items.
func (s *SessionPickerModal) SetSessions(sessions []types.Session) {
	s.sessions = sessions
	s.rebuildItems()
}

func (s *SessionPickerModal) rebuildItems() {
	items := make([]ModalItem, 0, len(s.sessions)+1)

	for i, sess := range s.sessions {
		// Parse last activity for relative time display
		lastActivity := sess.LastActivity
		if t, err := time.Parse(time.RFC3339, sess.LastActivity); err == nil {
			lastActivity = formatRelativeTime(t)
		}

		key := fmt.Sprintf("%d", i+1)
		if i >= 9 {
			key = "" // No shortcut for sessions beyond 9
		}

		label := sess.Description
		if label == "" {
			label = sess.Name
		}
		if len(sess.AttachedClients) > 0 {
			label += fmt.Sprintf(" (%d attached)", len(sess.AttachedClients))
		}

		items = append(items, ModalItem{
			Key:         key,
			Label:       label,
			Description: lastActivity,
		})
	}

	s.SetItems(items)
}

// View renders the session picker modal.
func (s *SessionPickerModal) View(screenW, screenH int) string {
	if !s.visible {
		return ""
	}

	var b strings.Builder

	// Modal box style
	boxStyle := s.styles.ModalBox.Width(s.width)

	// Title
	titleStyle := s.styles.ModalTitle.Width(s.width - 4)
	b.WriteString(titleStyle.Render(s.title))
	b.WriteString("\n")

	// Separator
	b.WriteString(s.styles.Muted.Render(strings.Repeat("─", s.width-4)))
	b.WriteString("\n")

	if s.inputMode {
		// New session input mode
		b.WriteString(s.styles.Paragraph.Render("Enter session name:"))
		b.WriteString("\n")
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorAccent).
			Padding(0, 1).
			Width(s.width - 8)
		input := s.inputBuffer
		if input == "" {
			input = s.clientConfig.Session.DefaultName
		}
		b.WriteString(inputStyle.Render(input + "█"))
		b.WriteString("\n\n")
		b.WriteString(s.styles.Muted.Render("Enter to create, Esc to cancel"))
	} else if len(s.sessions) == 0 {
		// No sessions
		b.WriteString(s.styles.Muted.Render("No sessions found"))
		b.WriteString("\n\n")
		b.WriteString(s.styles.HelpKey.Render("[n]"))
		b.WriteString(s.styles.HelpValue.Render(" Create new session"))
	} else {
		// Session list
		for i, sess := range s.sessions {
			style := s.styles.ModalItem
			if i == s.selected {
				style = s.styles.ModalItemSelected
			}

			// Pointer for selected item
			pointer := "  "
			if i == s.selected {
				pointer = "▸ "
			}

			// Parse last activity for relative time
			lastActivity := sess.LastActivity
			if t, err := time.Parse(time.RFC3339, sess.LastActivity); err == nil {
				lastActivity = formatRelativeTime(t)
			}

			// Prefer description over name for display
			name := sess.Description
			if name == "" {
				name = sess.Name
			}
			maxNameLen := s.width - 20
			if len(name) > maxNameLen {
				name = name[:maxNameLen-3] + "..."
			}

			line := fmt.Sprintf("%s%-*s %s", pointer, maxNameLen, name, s.styles.Muted.Render(lastActivity))
			b.WriteString(style.Render(line))
			b.WriteString("\n")
		}

		// Footer with actions
		b.WriteString("\n")
		b.WriteString(s.styles.Muted.Render(strings.Repeat("─", s.width-4)))
		b.WriteString("\n")
		actions := []string{
			s.styles.HelpKey.Render("[Enter]") + s.styles.HelpValue.Render(" Switch"),
			s.styles.HelpKey.Render("[n]") + s.styles.HelpValue.Render(" New"),
			s.styles.HelpKey.Render("[d]") + s.styles.HelpValue.Render(" Delete"),
			s.styles.HelpKey.Render("[Esc]") + s.styles.HelpValue.Render(" Cancel"),
		}
		b.WriteString(strings.Join(actions, "  "))
	}

	content := boxStyle.Render(b.String())
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

// SessionSwitchMsg indicates a session switch request.
type SessionSwitchMsg struct {
	Session *types.Session
}

// SessionCreateMsg indicates a new session creation request.
type SessionCreateMsg struct {
	Name string
}

// SessionDeleteMsg indicates a session deletion request.
type SessionDeleteMsg struct {
	SessionID string
}

// HandleKey processes key input for the session picker.
// Returns a tea.Cmd if an action should be performed.
func (s *SessionPickerModal) HandleKey(key string) tea.Cmd {
	if s.inputMode {
		return s.handleInputKey(key)
	}

	switch key {
	case "up", "k":
		if s.selected > 0 {
			s.selected--
		}
	case "down", "j":
		if s.selected < len(s.sessions)-1 {
			s.selected++
		}
	case "enter":
		if s.selected >= 0 && s.selected < len(s.sessions) {
			sess := s.sessions[s.selected]
			s.Hide()
			return func() tea.Msg {
				return SessionSwitchMsg{Session: &sess}
			}
		}
	case "n":
		s.inputMode = true
		s.inputBuffer = ""
	case "d":
		if s.selected >= 0 && s.selected < len(s.sessions) {
			sess := s.sessions[s.selected]
			s.Hide()
			return func() tea.Msg {
				return SessionDeleteMsg{SessionID: sess.ID}
			}
		}
	case "esc", "q":
		s.Hide()
	default:
		// Check for numeric shortcuts
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			idx := int(key[0] - '1')
			if idx < len(s.sessions) {
				sess := s.sessions[idx]
				s.Hide()
				return func() tea.Msg {
					return SessionSwitchMsg{Session: &sess}
				}
			}
		}
	}

	return nil
}

func (s *SessionPickerModal) handleInputKey(key string) tea.Cmd {
	switch key {
	case "enter":
		name := s.inputBuffer
		if name == "" {
			name = s.clientConfig.Session.DefaultName
		}
		s.inputMode = false
		s.inputBuffer = ""
		s.Hide()
		return func() tea.Msg {
			return SessionCreateMsg{Name: name}
		}
	case "esc":
		s.inputMode = false
		s.inputBuffer = ""
	case "backspace":
		if len(s.inputBuffer) > 0 {
			s.inputBuffer = s.inputBuffer[:len(s.inputBuffer)-1]
		}
	default:
		// Append printable characters
		if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
			s.inputBuffer += key
		}
	}
	return nil
}

// formatRelativeTime formats a time as relative to now (e.g., "2h ago", "1d ago").
func formatRelativeTime(t time.Time) string {
	diff := time.Since(t)

	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		mins := int(diff.Minutes())
		return fmt.Sprintf("%dm ago", mins)
	}
	if diff < 24*time.Hour {
		hours := int(diff.Hours())
		return fmt.Sprintf("%dh ago", hours)
	}
	if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	}
	if diff < 30*24*time.Hour {
		weeks := int(diff.Hours() / 24 / 7)
		return fmt.Sprintf("%dw ago", weeks)
	}

	return t.Format("Jan 2")
}
