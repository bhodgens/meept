package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/components"
	"github.com/caimlas/meept/internal/tui/types"
)

// ModalType represents the type of modal currently displayed.
type ModalType int

const (
	ModalNone ModalType = iota
	ModalCommandPalette
	ModalSessionPicker
	ModalNewSession
	ModalSessionRename
	ModalConfirm
	ModalFuzzyFinder
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
	b.WriteString(hintStyle.Render("press key or esc to cancel"))

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
	m := NewModal("command palette", styles)
	keys := config.Keybindings.CommandPalette

	m.SetItems([]ModalItem{
		{Key: keys.ViewChat, Label: "chat", Description: "switch to chat view"},
		{Key: keys.ViewTasks, Label: "tasks", Description: "switch to tasks view"},
		{Key: keys.ViewQueue, Label: "queue", Description: "switch to queue view"},
		{Key: keys.ViewMemory, Label: "memory", Description: "switch to memory view"},
		{Key: keys.Sidebar, Label: "toggle sidebar", Description: "show/hide sidebar"},
		{Key: keys.Sessions, Label: "sessions...", Description: "manage sessions"},
		{Key: keys.NewSession, Label: "new session", Description: "create a new session"},
		{Key: keys.RenameSession, Label: "edit description", Description: "edit session description"},
		{Key: "f", Label: "find...", Description: "search sessions and tasks"},
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
	m := NewModal("sessions", styles)
	m.width = 90

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

// FuzzyFinderSessionsMsg carries sessions for the fuzzy finder.
type FuzzyFinderSessionsMsg struct {
	Sessions []types.Session
}

// FuzzyFinderTasksMsg carries tasks for the fuzzy finder.
type FuzzyFinderTasksMsg struct {
	Tasks []types.TaskExtended
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

// SetCurrentSession sets the selected index to match the given session ID.
func (s *SessionPickerModal) SetCurrentSession(sessionID string) {
	for i, sess := range s.sessions {
		if sess.ID == sessionID {
			s.selected = i
			return
		}
	}
}

// HandleMouse processes mouse events for the session picker.
func (s *SessionPickerModal) HandleMouse(msg tea.MouseMsg, screenW, screenH int) tea.Cmd {
	if !s.visible || s.inputMode || len(s.sessions) == 0 {
		return nil
	}

	click, ok := msg.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft {
		return nil
	}

	mouse := click.Mouse()

	// Calculate modal dimensions and position (centered)
	modalH := len(s.sessions) + 7 // sessions + header + footer
	modalX := (screenW - s.width) / 2
	modalY := (screenH - modalH) / 2

	// Check if click is within modal horizontal bounds
	if mouse.X < modalX || mouse.X >= modalX+s.width {
		return nil
	}

	// Header is 3 lines (title + separator + empty), sessions start after
	headerLines := 3
	relY := mouse.Y - modalY - headerLines

	if relY >= 0 && relY < len(s.sessions) {
		sess := s.sessions[relY]
		s.Hide()
		return func() tea.Msg {
			return SessionSwitchMsg{Session: &sess}
		}
	}

	return nil
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
		b.WriteString(s.styles.Paragraph.Render("enter session name:"))
		b.WriteString("\n")
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorAccent).
			Padding(0, 1).
			Width(s.width - 8)
		input := s.inputBuffer
		if input == "" {
			// Show placeholder in muted style
			placeholder := s.styles.Muted.Render(s.clientConfig.Session.DefaultName)
			b.WriteString(inputStyle.Render(placeholder + "█"))
		} else {
			b.WriteString(inputStyle.Render(input + "█"))
		}
		b.WriteString("\n\n")
		hint := "enter to create"
		if s.inputBuffer == "" {
			hint += fmt.Sprintf(" (uses '%s')", s.clientConfig.Session.DefaultName)
		}
		hint += ", esc to cancel"
		b.WriteString(s.styles.Muted.Render(hint))
	} else if len(s.sessions) == 0 {
		// No sessions
		b.WriteString(s.styles.Muted.Render("no sessions found"))
		b.WriteString("\n\n")
		b.WriteString(s.styles.HelpKey.Render("[n]"))
		b.WriteString(s.styles.HelpValue.Render(" create new session"))
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

			// Show both name and description
			// Name on the left (truncated), description in middle (truncated), time at far right
			maxNameLen := 16
			timeColWidth := 18
			maxDescLen := s.width - maxNameLen - timeColWidth - 8 // 8 for spacing/pointer

			name := sess.Name
			if len(name) > maxNameLen {
				name = name[:maxNameLen-3] + "..."
			}

			desc := sess.Description
			if desc == "" {
				desc = "(no description)"
			}
			if len(desc) > maxDescLen {
				desc = desc[:maxDescLen-3] + "..."
			}

			// Format: pointer + name (fixed width) + description + time (right-aligned)
			namePart := fmt.Sprintf("%-*s", maxNameLen, name)
			descPart := s.styles.Muted.Render(fmt.Sprintf("%-*s", maxDescLen, desc))
			timePart := s.styles.Muted.Render(fmt.Sprintf("%*s", timeColWidth, lastActivity))

			line := fmt.Sprintf("%s%s  %s  %s", pointer, namePart, descPart, timePart)
			b.WriteString(style.Render(line))
			b.WriteString("\n")
		}

		// Footer with actions
		b.WriteString("\n")
		b.WriteString(s.styles.Muted.Render(strings.Repeat("─", s.width-4)))
		b.WriteString("\n")
		actions := []string{
			s.styles.HelpKey.Render("[enter]") + s.styles.HelpValue.Render(" switch"),
			s.styles.HelpKey.Render("[n]") + s.styles.HelpValue.Render(" new"),
			s.styles.HelpKey.Render("[r]") + s.styles.HelpValue.Render(" edit"),
			s.styles.HelpKey.Render("[d]") + s.styles.HelpValue.Render(" delete"),
			s.styles.HelpKey.Render("[esc]") + s.styles.HelpValue.Render(" cancel"),
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

// OpenRenameModalMsg indicates the rename modal should be opened.
type OpenRenameModalMsg struct {
	SessionID   string
	CurrentName string
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
	case "r":
		if s.selected >= 0 && s.selected < len(s.sessions) {
			sess := s.sessions[s.selected]
			s.Hide()
			// Return a message to open rename modal with current session info
			return func() tea.Msg {
				currentName := sess.Description
				if currentName == "" {
					currentName = sess.Name
				}
				return OpenRenameModalMsg{SessionID: sess.ID, CurrentName: currentName}
			}
		}
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
	case "backspace", "ctrl+h":
		if len(s.inputBuffer) > 0 {
			s.inputBuffer = s.inputBuffer[:len(s.inputBuffer)-1]
		}
	case "ctrl+u":
		// Clear the entire input
		s.inputBuffer = ""
	case "left", "right", "up", "down", "tab":
		// Ignore navigation keys in input mode
		return nil
	default:
		// Append printable characters
		if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
			s.inputBuffer += key
		}
	}
	return nil
}

// SessionRenameMsg indicates a session rename request.
type SessionRenameMsg struct {
	SessionID string
	NewName   string
}

// SessionRenameModal is a modal for renaming a session.
type SessionRenameModal struct {
	visible     bool
	sessionID   string
	sessionName string
	inputBuffer string
	selected    int // 0 = input, 1 = ok, 2 = cancel
	styles      *Styles
	width       int
}

// NewSessionRenameModal creates a new session rename modal.
func NewSessionRenameModal(styles *Styles) *SessionRenameModal {
	return &SessionRenameModal{
		visible:  false,
		selected: 0,
		styles:   styles,
		width:    50,
	}
}

// Show shows the rename modal for a session.
func (m *SessionRenameModal) Show(sessionID, currentName string) {
	m.visible = true
	m.sessionID = sessionID
	m.sessionName = currentName
	m.inputBuffer = currentName
	m.selected = 0
}

// Hide hides the modal.
func (m *SessionRenameModal) Hide() {
	m.visible = false
}

// IsVisible returns whether the modal is visible.
func (m *SessionRenameModal) IsVisible() bool {
	return m.visible
}

// View renders the session rename modal.
func (m *SessionRenameModal) View(screenW, screenH int) string {
	if !m.visible {
		return ""
	}

	var b strings.Builder

	// Modal box style
	boxStyle := m.styles.ModalBox.Width(m.width)

	// Title
	titleStyle := m.styles.ModalTitle.Width(m.width - 4)
	b.WriteString(titleStyle.Render("edit session description"))
	b.WriteString("\n")

	// Separator
	b.WriteString(m.styles.Muted.Render(strings.Repeat("─", m.width-4)))
	b.WriteString("\n\n")

	// Input field
	b.WriteString(m.styles.Paragraph.Render("description:"))
	b.WriteString("\n")
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorAccent).
		Padding(0, 1).
		Width(m.width - 8)
	if m.selected == 0 {
		inputStyle = inputStyle.BorderForeground(ColorPrimary)
	}
	input := m.inputBuffer + "█"
	b.WriteString(inputStyle.Render(input))
	b.WriteString("\n\n")

	// Buttons
	okStyle := m.styles.ModalItem
	cancelStyle := m.styles.ModalItem
	if m.selected == 1 {
		okStyle = m.styles.ModalItemSelected
	}
	if m.selected == 2 {
		cancelStyle = m.styles.ModalItemSelected
	}

	okBtn := okStyle.Render("  [ ok ]  ")
	cancelBtn := cancelStyle.Render("  [ cancel ]  ")
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, okBtn, "    ", cancelBtn)
	buttonLine := lipgloss.NewStyle().Width(m.width - 4).Align(lipgloss.Center)
	b.WriteString(buttonLine.Render(buttons))
	b.WriteString("\n")

	// Footer hint
	b.WriteString("\n")
	hintStyle := m.styles.Muted.Align(lipgloss.Center).Width(m.width - 4)
	b.WriteString(hintStyle.Render("tab to switch · enter to confirm · esc to cancel"))

	content := boxStyle.Render(b.String())
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

// HandleKey processes key input for the rename modal.
func (m *SessionRenameModal) HandleKey(key string) tea.Cmd {
	// When input field is focused (selected == 0), handle text input keys first
	if m.selected == 0 {
		switch key {
		case "backspace", "ctrl+h":
			if len(m.inputBuffer) > 0 {
				m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
			}
			return nil
		case "ctrl+u":
			// Clear the entire input
			m.inputBuffer = ""
			return nil
		case "tab":
			m.selected = 1
			return nil
		case "shift+tab":
			m.selected = 2
			return nil
		case "enter":
			// Submit the current input
			name := m.inputBuffer
			sessionID := m.sessionID
			m.Hide()
			return func() tea.Msg {
				return SessionRenameMsg{SessionID: sessionID, NewName: name}
			}
		case "esc":
			m.Hide()
			return nil
		case "left", "right", "up", "down":
			// Ignore arrow keys in input mode - don't change selection
			return nil
		default:
			// Append printable characters
			if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
				m.inputBuffer += key
			}
			return nil
		}
	}

	// Button navigation (when not in input field)
	switch key {
	case "tab":
		m.selected = (m.selected + 1) % 3
	case "shift+tab":
		m.selected = (m.selected + 2) % 3
	case "left":
		if m.selected > 1 {
			m.selected--
		} else if m.selected == 1 {
			m.selected = 0 // Go back to input
		}
	case "right":
		if m.selected < 2 {
			m.selected++
		}
	case "up":
		m.selected = 0 // Go back to input
	case "enter":
		switch m.selected {
		case 1:
			// Ok button - submit
			name := m.inputBuffer
			sessionID := m.sessionID
			m.Hide()
			return func() tea.Msg {
				return SessionRenameMsg{SessionID: sessionID, NewName: name}
			}
		case 2:
			// Cancel button
			m.Hide()
		}
	case "esc":
		m.Hide()
	default:
		// If user starts typing while on a button, go back to input and type
		if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
			m.selected = 0
			m.inputBuffer += key
		}
	}
	return nil
}

// ConfirmModal is a modal for yes/no confirmations.
type ConfirmModal struct {
	visible   bool
	title     string
	message   string
	selected  int // 0=yes, 1=no
	styles    *Styles
	width     int
	onConfirm func() tea.Cmd
	onCancel  func() tea.Cmd
}

// NewConfirmModal creates a new confirmation modal.
func NewConfirmModal(styles *Styles) *ConfirmModal {
	return &ConfirmModal{
		visible:  false,
		selected: 1, // Default to "no" for safety
		styles:   styles,
		width:    50,
	}
}

// Show displays the confirm modal with the given title and message.
func (m *ConfirmModal) Show(title, message string, onConfirm, onCancel func() tea.Cmd) {
	m.visible = true
	m.title = title
	m.message = message
	m.selected = 1 // Default to "no"
	m.onConfirm = onConfirm
	m.onCancel = onCancel
}

// Hide hides the modal.
func (m *ConfirmModal) Hide() {
	m.visible = false
}

// IsVisible returns whether the modal is visible.
func (m *ConfirmModal) IsVisible() bool {
	return m.visible
}

// View renders the confirm modal.
func (m *ConfirmModal) View(screenW, screenH int) string {
	if !m.visible {
		return ""
	}

	var b strings.Builder

	// Modal box style
	boxStyle := m.styles.ModalBox.Width(m.width)

	// Title
	titleStyle := m.styles.ModalTitle.Width(m.width - 4)
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")

	// Separator
	b.WriteString(m.styles.Muted.Render(strings.Repeat("─", m.width-4)))
	b.WriteString("\n\n")

	// Message
	b.WriteString(m.styles.Paragraph.Render(m.message))
	b.WriteString("\n\n")

	// Buttons
	yesStyle := m.styles.ModalItem
	noStyle := m.styles.ModalItem
	if m.selected == 0 {
		yesStyle = m.styles.ModalItemSelected
	}
	if m.selected == 1 {
		noStyle = m.styles.ModalItemSelected
	}

	yesBtn := yesStyle.Render("  [ yes ]  ")
	noBtn := noStyle.Render("  [ no ]  ")
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, yesBtn, "    ", noBtn)
	buttonLine := lipgloss.NewStyle().Width(m.width - 4).Align(lipgloss.Center)
	b.WriteString(buttonLine.Render(buttons))
	b.WriteString("\n")

	// Footer hint
	b.WriteString("\n")
	hintStyle := m.styles.Muted.Align(lipgloss.Center).Width(m.width - 4)
	b.WriteString(hintStyle.Render("←/→ to select · enter to confirm · esc to cancel"))

	content := boxStyle.Render(b.String())
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

// HandleKey processes key input for the confirm modal.
func (m *ConfirmModal) HandleKey(key string) tea.Cmd {
	switch key {
	case "left", "h":
		m.selected = 0 // yes
	case "right", "l":
		m.selected = 1 // no
	case "tab":
		m.selected = (m.selected + 1) % 2
	case "y":
		m.Hide()
		if m.onConfirm != nil {
			return m.onConfirm()
		}
	case "n", "esc", "q":
		m.Hide()
		if m.onCancel != nil {
			return m.onCancel()
		}
	case "enter":
		m.Hide()
		if m.selected == 0 && m.onConfirm != nil {
			return m.onConfirm()
		}
		if m.selected == 1 && m.onCancel != nil {
			return m.onCancel()
		}
	}
	return nil
}


// FuzzyFinderModal is a modal for searching sessions and tasks.
type FuzzyFinderModal struct {
	*Modal
	sessions      []types.Session
	tasks         []types.TaskExtended
	inputBuffer   string
	cursorX       int // left pane (0) or right pane (1)
	selectedIndex int
	rpc           *RPCClient
	styles        *Styles
	width         int
	height        int
}

// NewFuzzyFinderModal creates a new fuzzy finder modal.
func NewFuzzyFinderModal(styles *Styles, rpc *RPCClient) *FuzzyFinderModal {
	return &FuzzyFinderModal{
		sessions:    []types.Session{},
		tasks:       []types.TaskExtended{},
		inputBuffer: "",
		cursorX:     0,
		width:       100,
		height:      30,
		rpc:         rpc,
		styles:      styles,
	}
}

// Show makes the fuzzy finder visible and starts fetching data.
func (f *FuzzyFinderModal) Show() {
	f.visible = true
	f.inputBuffer = ""
	f.selectedIndex = 0
	f.cursorX = 0
}

// Hide hides the fuzzy finder.
func (f *FuzzyFinderModal) Hide() {
	f.visible = false
}

// Visible returns whether the fuzzy finder is visible.
func (f *FuzzyFinderModal) Visible() bool {
	return f.visible
}

// FetchSessions returns a cmd to fetch sessions.
func (f *FuzzyFinderModal) FetchSessions() tea.Cmd {
	return func() tea.Msg {
		if f.rpc == nil || !f.rpc.IsConnected() {
			return FuzzyFinderSessionsMsg{Sessions: []types.Session{}}
		}
		resp, err := f.rpc.ListSessions()
		if err != nil {
			return FuzzyFinderSessionsMsg{Sessions: []types.Session{}}
		}
		return FuzzyFinderSessionsMsg{Sessions: resp.Sessions}
	}
}

// FetchTasks returns a cmd to fetch tasks.
func (f *FuzzyFinderModal) FetchTasks() tea.Cmd {
	return func() tea.Msg {
		if f.rpc == nil || !f.rpc.IsConnected() {
			return FuzzyFinderTasksMsg{Tasks: []types.TaskExtended{}}
		}
		resp, err := f.rpc.ListTasksExtended()
		if err != nil {
			return FuzzyFinderTasksMsg{Tasks: []types.TaskExtended{}}
		}
		return FuzzyFinderTasksMsg{Tasks: resp.Tasks}
	}
}

// SetSessions sets the session list.
func (f *FuzzyFinderModal) SetSessions(sessions []types.Session) {
	f.sessions = sessions
}

// SetTasks sets the task list.
func (f *FuzzyFinderModal) SetTasks(tasks []types.TaskExtended) {
	f.tasks = tasks
}

// GetSelectedSession returns the selected session if any.
func (f *FuzzyFinderModal) GetSelectedSession() *types.Session {
	items := f.getFilteredItems()
	if f.selectedIndex >= 0 && f.selectedIndex < len(items) {
		if items[f.selectedIndex].Session != nil {
			return items[f.selectedIndex].Session
		}
	}
	return nil
}

// GetSelectedTask returns the selected task if any.
func (f *FuzzyFinderModal) GetSelectedTask() *types.TaskExtended {
	items := f.getFilteredItems()
	if f.selectedIndex >= 0 && f.selectedIndex < len(items) {
		if items[f.selectedIndex].Task != nil {
			return items[f.selectedIndex].Task
		}
	}
	return nil
}

// fuzzyFinderItem represents a searchable item.
type fuzzyFinderItem struct {
	Session *types.Session
	Task    *types.TaskExtended
	Match   string // display text
}

// getFilteredItems returns items matching the search query using fuzzy matching.
func (f *FuzzyFinderModal) getFilteredItems() []fuzzyFinderItem {
	query := f.inputBuffer

	// Build item list for the fuzzy matcher
	searchItems := make([]struct{ Text string; Data any }, 0, len(f.sessions)+len(f.tasks))
	for i := range f.sessions {
		sess := &f.sessions[i]
		display := sess.Name
		if sess.Description != "" {
			display += " - " + sess.Description
		}
		display += " [session]"
		searchItems = append(searchItems, struct{ Text string; Data any }{Text: display, Data: sess})
	}
	for i := range f.tasks {
		task := &f.tasks[i]
		display := task.Name
		if task.Description != "" {
			display += " - " + task.Description
		}
		display += " [task]"
		searchItems = append(searchItems, struct{ Text string; Data any }{Text: display, Data: task})
	}

	matcher := components.NewFuzzyMatcher(searchItems)
	matches := matcher.Match(query)

	var items []fuzzyFinderItem
	for _, m := range matches {
		switch v := m.Item.(type) {
		case *types.Session:
			items = append(items, fuzzyFinderItem{
				Session: v,
				Match:   strings.TrimSuffix(m.Text, " [session]"),
			})
		case *types.TaskExtended:
			items = append(items, fuzzyFinderItem{
				Task:  v,
				Match: strings.TrimSuffix(m.Text, " [task]"),
			})
		}
	}

	return items
}

// View renders the fuzzy finder modal.
func (f *FuzzyFinderModal) View(screenW, screenH int) string {
	if !f.visible {
		return ""
	}

	var b strings.Builder

	// Modal box style
	boxStyle := f.styles.ModalBox.Width(f.width).Height(f.height)

	// Title
	titleStyle := f.styles.ModalTitle.Width(f.width - 4)
	b.WriteString(titleStyle.Render("find (sessions and tasks)"))
	b.WriteString("\n")

	// Separator
	b.WriteString(f.styles.Muted.Render(strings.Repeat("─", f.width-4)))
	b.WriteString("\n")

	// Search input
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C3AED")).
		Padding(0, 1).
		Width(f.width - 10)
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Width(f.width-4).Render(
		inputStyle.Render("search: "+f.inputBuffer+"█"),
	))
	b.WriteString("\n\n")

	// Results pane
	items := f.getFilteredItems()
	resultsHeight := max(f.height-12, 5)
	resultsStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Width(f.width - 4).
		Height(resultsHeight)

	var resultsContent strings.Builder
	if len(items) == 0 {
		resultsContent.WriteString(f.styles.Muted.Render("no matches found"))
	} else {
		for i, item := range items {
			style := f.styles.ModalItem
			if i == f.selectedIndex {
				style = f.styles.ModalItemSelected
			}
			pointer := "  "
			if i == f.selectedIndex {
				pointer = "▸ "
			}
			label := item.Match
			if len(label) > f.width-10 {
				label = label[:f.width-13] + "..."
			}
			resultsContent.WriteString(style.Render(pointer + label))
			resultsContent.WriteString("\n")
		}
	}
	b.WriteString(resultsStyle.Render(resultsContent.String()))

	// Footer hints
	b.WriteString("\n")
	hints := []string{
		f.styles.HelpKey.Render("[↑/↓]") + f.styles.HelpValue.Render(" navigate"),
		f.styles.HelpKey.Render("[enter]") + f.styles.HelpValue.Render(" select"),
		f.styles.HelpKey.Render("[esc]") + f.styles.HelpValue.Render(" cancel"),
	}
	b.WriteString(f.styles.Muted.Render(strings.Join(hints, "  ")))

	content := boxStyle.Render(b.String())
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

// HandleKey processes key input for the fuzzy finder.
func (f *FuzzyFinderModal) HandleKey(key string) string {
	// Check for search input keys first
	if len(key) == 1 && key[0] >= ' ' && key[0] <= '~' {
		f.inputBuffer += key
		f.selectedIndex = 0 // Reset selection on new search
		return ""
	}

	switch key {
	case "backspace":
		if len(f.inputBuffer) > 0 {
			f.inputBuffer = f.inputBuffer[:len(f.inputBuffer)-1]
			f.selectedIndex = 0
		}
	case "ctrl+u":
		f.inputBuffer = ""
		f.selectedIndex = 0
	case "up", "k":
		if f.selectedIndex > 0 {
			f.selectedIndex--
		}
	case "down", "j":
		items := f.getFilteredItems()
		if f.selectedIndex < len(items)-1 {
			f.selectedIndex++
		}
	case "enter":
		items := f.getFilteredItems()
		if f.selectedIndex >= 0 && f.selectedIndex < len(items) {
			f.Hide()
			return "select"
		}
	case "esc", "q":
		f.Hide()
	}

	return ""
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
