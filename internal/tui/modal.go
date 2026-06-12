package tui

import (
	"encoding/json"
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
	ModalBranchPicker
	ModalProjectPicker
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
	case KeyDown, "j":
		if m.selected < len(m.items)-1 {
			m.selected++
		}
	case KeyEnter:
		if m.selected >= 0 && m.selected < len(m.items) && !m.items[m.selected].Disabled {
			m.Hide()
			return m.items[m.selected].Key
		}
	case KeyEsc, "q":
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
		{Key: keys.ViewSessions, Label: "sessions", Description: "switch to sessions view"},
		{Key: keys.ViewTasks, Label: CmdTasks, Description: "switch to tasks view"},
		{Key: keys.ViewQueue, Label: "queue", Description: "switch to queue view"},
		{Key: keys.ViewMemory, Label: "memory", Description: "switch to memory view"},
		{Key: keys.ViewPlans, Label: "plans", Description: "switch to plans view"},
		{Key: keys.Sidebar, Label: "toggle sidebar", Description: "show/hide sidebar"},
		{Key: keys.Sessions, Label: "sessions picker", Description: "quick session switch"},
		{Key: keys.Projects, Label: "projects", Description: "manage projects"},
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
	inputMode    bool   // true when entering new session name
	inputBuffer  string // buffer for new session name
	rpc          *RPCClient
	clientConfig *ClientConfig
	planCounts   map[string]int // session_id -> total plan count
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

// PlanCountsMsg carries plan counts for sessions.
type PlanCountsMsg struct {
	Counts map[string]int // session_id -> total plan count
}

// FetchPlanCounts fetches plan counts for each session via RPC.
func (s *SessionPickerModal) FetchPlanCounts(sessions []types.Session) tea.Cmd {
	return func() tea.Msg {
		if s.rpc == nil || !s.rpc.IsConnected() {
			return PlanCountsMsg{}
		}
		counts := make(map[string]int, len(sessions))
		for _, sess := range sessions {
			result, err := s.rpc.Call("plan.count_by_session", map[string]string{ParamSessionID: sess.ID})
			if err != nil {
				continue // gracefully degrade
			}
			// Result is map[state]count, sum all states
			var stateCounts map[string]int
			if err := json.Unmarshal(result, &stateCounts); err != nil {
				continue
			}
			total := 0
			for _, cnt := range stateCounts {
				total += cnt
			}
			if total > 0 {
				counts[sess.ID] = total
			}
		}
		return PlanCountsMsg{Counts: counts}
	}
}

// SetPlanCounts stores fetched plan counts and rebuilds items.
func (s *SessionPickerModal) SetPlanCounts(counts map[string]int) {
	s.planCounts = counts
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

	switch {
	case s.inputMode:
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
	case len(s.sessions) == 0:
		// No sessions
		b.WriteString(s.styles.Muted.Render("no sessions found"))
		b.WriteString("\n\n")
		b.WriteString(s.styles.HelpKey.Render("[n]"))
		b.WriteString(s.styles.HelpValue.Render(" create new session"))
	default:
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
			// Plan count indicators per session
			if s.planCounts != nil {
				if cnt, ok := s.planCounts[sess.ID]; ok && cnt > 0 {
					descPart += s.styles.PlanStatePlanning.Render(fmt.Sprintf(" (%d plans)", cnt))
				}
			}
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
	case KeyDown, "j":
		if s.selected < len(s.sessions)-1 {
			s.selected++
		}
	case KeyEnter:
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
	case KeyEsc, "q":
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
	case KeyEnter:
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
	case KeyEsc:
		s.inputMode = false
		s.inputBuffer = ""
	case "backspace", "ctrl+h":
		if s.inputBuffer != "" {
			s.inputBuffer = s.inputBuffer[:len(s.inputBuffer)-1]
		}
	case "ctrl+u":
		// Clear the entire input
		s.inputBuffer = ""
	case KeyLeft, KeyRight, "up", KeyDown, KeyTab:
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
			if m.inputBuffer != "" {
				m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
			}
			return nil
		case "ctrl+u":
			// Clear the entire input
			m.inputBuffer = ""
			return nil
		case KeyTab:
			m.selected = 1
			return nil
		case "shift+tab":
			m.selected = 2
			return nil
		case KeyEnter:
			// Submit the current input
			name := m.inputBuffer
			sessionID := m.sessionID
			m.Hide()
			return func() tea.Msg {
				return SessionRenameMsg{SessionID: sessionID, NewName: name}
			}
		case KeyEsc:
			m.Hide()
			return nil
		case KeyLeft, KeyRight, "up", KeyDown:
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
	case KeyTab:
		m.selected = (m.selected + 1) % 3
	case "shift+tab":
		m.selected = (m.selected + 2) % 3
	case KeyLeft:
		if m.selected > 1 {
			m.selected--
		} else if m.selected == 1 {
			m.selected = 0 // Go back to input
		}
	case KeyRight:
		if m.selected < 2 {
			m.selected++
		}
	case "up":
		m.selected = 0 // Go back to input
	case KeyEnter:
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
	case KeyEsc:
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
	case KeyLeft, "h":
		m.selected = 0 // yes
	case KeyRight, "l":
		m.selected = 1 // no
	case KeyTab:
		m.selected = (m.selected + 1) % 2
	case "y":
		m.Hide()
		if m.onConfirm != nil {
			return m.onConfirm()
		}
	case "n", KeyEsc, "q":
		m.Hide()
		if m.onCancel != nil {
			return m.onCancel()
		}
	case KeyEnter:
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
	searchItems := make([]struct {
		Text string
		Data any
	}, 0, len(f.sessions)+len(f.tasks))
	for i := range f.sessions {
		sess := &f.sessions[i]
		display := sess.Name
		if sess.Description != "" {
			display += " - " + sess.Description
		}
		display += " [session]"
		searchItems = append(searchItems, struct {
			Text string
			Data any
		}{Text: display, Data: sess})
	}
	for i := range f.tasks {
		task := &f.tasks[i]
		display := task.Name
		if task.Description != "" {
			display += " - " + task.Description
		}
		display += " [task]"
		searchItems = append(searchItems, struct {
			Text string
			Data any
		}{Text: display, Data: task})
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
	b.WriteString(lipgloss.NewStyle().Width(f.width - 4).Render(
		inputStyle.Render("search: " + f.inputBuffer + "█"),
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
		if f.inputBuffer != "" {
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
	case KeyDown, "j":
		items := f.getFilteredItems()
		if f.selectedIndex < len(items)-1 {
			f.selectedIndex++
		}
	case KeyEnter:
		items := f.getFilteredItems()
		if f.selectedIndex >= 0 && f.selectedIndex < len(items) {
			f.Hide()
			return "select"
		}
	case KeyEsc, "q":
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

// BranchPickerModal is a modal for selecting and navigating to a conversation branch.
type BranchPickerModal struct {
	*Modal
	branches       []BranchInfo
	activeBranchID string // ID of the currently active branch
	selected       int
	rpc            *RPCClient
	styles         *Styles
	width          int
	scrollOffset   int // For scrolling through long branch lists
}

// BranchNavigateMsg indicates a branch navigation request.
type BranchNavigateMsg struct {
	Branch BranchInfo
	Err    error
}

// NewBranchPickerModal creates a new branch picker modal.
func NewBranchPickerModal(styles *Styles, rpc *RPCClient) *BranchPickerModal {
	return &BranchPickerModal{
		Modal:  NewModal("branches", styles),
		styles: styles,
		rpc:    rpc,
		width:  80,
	}
}

// RefreshBranches fetches the branch list from the daemon.
func (b *BranchPickerModal) RefreshBranches(sessionID string) tea.Cmd {
	return func() tea.Msg {
		if b.rpc == nil || !b.rpc.IsConnected() {
			return BranchInfoMsg{Branches: nil, Err: fmt.Errorf("not connected")}
		}

		branches, err := b.rpc.ListBranches(sessionID)
		return BranchInfoMsg{Branches: branches, Err: err}
	}
}

// SetBranches updates the branch list and resets selection.
func (b *BranchPickerModal) SetBranches(branches []BranchInfo) {
	b.branches = branches
	b.selected = 0
	b.scrollOffset = 0

	// Default active branch to the first one if not set
	if b.activeBranchID == "" && len(branches) > 0 {
		b.activeBranchID = branches[0].ID
	}

	// Position selection on the active branch
	for i, br := range branches {
		if br.ID == b.activeBranchID {
			b.selected = i
			break
		}
	}
}

// SetActiveBranchID sets the currently active branch ID for indicator display.
func (b *BranchPickerModal) SetActiveBranchID(id string) {
	b.activeBranchID = id
}

// View renders the branch picker modal.
func (b *BranchPickerModal) View(screenW, screenH int) string {
	if !b.visible {
		return ""
	}

	var sb strings.Builder

	boxStyle := b.styles.ModalBox.Width(b.width)

	// Title
	titleStyle := b.styles.ModalTitle.Width(b.width - 4)
	sb.WriteString(titleStyle.Render("branches"))
	sb.WriteString("\n")

	// Separator
	sb.WriteString(b.styles.Muted.Render(strings.Repeat("─", b.width-4)))
	sb.WriteString("\n")

	if len(b.branches) == 0 {
		sb.WriteString(b.styles.Muted.Render("no branches found"))
		sb.WriteString("\n")
	} else {
		// Column widths
		idColWidth := 18
		msgColWidth := 8
		summaryColWidth := b.width - idColWidth - msgColWidth - 10 // 10 for spacing/indicators

		// Header
		header := fmt.Sprintf("  %-*s  %-*s  %s",
			idColWidth, "branch id",
			msgColWidth, "msgs",
			"summary")
		sb.WriteString(b.styles.Muted.Render(header))
		sb.WriteString("\n")
		sb.WriteString(b.styles.Muted.Render(strings.Repeat("─", b.width-4)))
		sb.WriteString("\n")

		// Calculate visible range for scrolling
		maxVisible := min(len(b.branches), 15) // show up to 15 branches
		if b.scrollOffset > len(b.branches)-maxVisible {
			b.scrollOffset = max(0, len(b.branches)-maxVisible)
		}
		end := min(b.scrollOffset+maxVisible, len(b.branches))

		for i := b.scrollOffset; i < end; i++ {
			br := b.branches[i]
			style := b.styles.ModalItem
			if i == b.selected {
				style = b.styles.ModalItemSelected
			}

			// Pointer for selected item
			pointer := "  "
			if i == b.selected {
				pointer = "▸ "
			}

			// Active branch indicator
			activeIndicator := " "
			if br.ID == b.activeBranchID {
				activeIndicator = "*"
			}

			// Branch ID (truncated)
			branchID := br.ID
			if len(branchID) > idColWidth {
				branchID = branchID[:idColWidth-3] + "..."
			} else {
				branchID = fmt.Sprintf("%-*s", idColWidth, branchID)
			}

			// Message count
			msgCount := fmt.Sprintf("%-*d", msgColWidth, br.MessageCount)

			// Summary (truncated)
			summary := br.Summary
			if summary == "" {
				summary = "(no summary)"
			}
			if len(summary) > summaryColWidth {
				summary = summary[:summaryColWidth-3] + "..."
			}

			activeStyle := b.styles.Muted
			if i == b.selected {
				activeStyle = b.styles.ModalItemSelected
			}
			activeStr := activeStyle.Render(activeIndicator)

			line := fmt.Sprintf("%s%s %s %s  %s", pointer, activeStr, branchID, msgCount, summary)
			sb.WriteString(style.Render(line))
			sb.WriteString("\n")
		}

		// Scroll indicator
		if len(b.branches) > maxVisible {
			sb.WriteString(b.styles.Muted.Render(fmt.Sprintf("  showing %d-%d of %d", b.scrollOffset+1, end, len(b.branches))))
			sb.WriteString("\n")
		}
	}

	// Footer with actions
	sb.WriteString("\n")
	sb.WriteString(b.styles.Muted.Render(strings.Repeat("─", b.width-4)))
	sb.WriteString("\n")
	actions := []string{
		b.styles.HelpKey.Render("[↑/↓]") + b.styles.HelpValue.Render(" navigate"),
		b.styles.HelpKey.Render("[enter]") + b.styles.HelpValue.Render(" switch"),
		b.styles.HelpKey.Render("[esc]") + b.styles.HelpValue.Render(" cancel"),
	}
	sb.WriteString(strings.Join(actions, "  "))

	content := boxStyle.Render(sb.String())
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

// HandleKey processes key input for the branch picker.
// Returns a tea.Cmd if an action should be performed.
func (b *BranchPickerModal) HandleKey(keyStr string) tea.Cmd {
	if len(b.branches) == 0 {
		if keyStr == KeyEsc || keyStr == "q" {
			b.Hide()
		}
		return nil
	}

	maxVisible := min(len(b.branches), 15)

	switch keyStr {
	case "up", "k":
		if b.selected > 0 {
			b.selected--
			if b.selected < b.scrollOffset {
				b.scrollOffset = b.selected
			}
		}
	case KeyDown, "j":
		if b.selected < len(b.branches)-1 {
			b.selected++
			if b.selected >= b.scrollOffset+maxVisible {
				b.scrollOffset = b.selected - maxVisible + 1
			}
		}
	case KeyEnter:
		if b.selected >= 0 && b.selected < len(b.branches) {
			br := b.branches[b.selected]
			b.Hide()
			return func() tea.Msg {
				return BranchNavigateMsg{Branch: br}
			}
		}
	case KeyEsc, "q":
		b.Hide()
	}

	return nil
}

// HandleMouse processes mouse events for the branch picker.
func (b *BranchPickerModal) HandleMouse(msg tea.MouseMsg, screenW, screenH int) tea.Cmd {
	if !b.visible || len(b.branches) == 0 {
		return nil
	}

	click, ok := msg.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft {
		return nil
	}

	mouse := click.Mouse()

	// Calculate modal dimensions and position (centered)
	headerLines := 4 // title + separator + column header + separator
	footerLines := 3 // separator + actions + padding
	modalH := headerLines + min(len(b.branches), 15) + footerLines
	modalX := (screenW - b.width) / 2
	modalY := (screenH - modalH) / 2

	// Check if click is within modal horizontal bounds
	if mouse.X < modalX || mouse.X >= modalX+b.width {
		return nil
	}

	relY := mouse.Y - modalY - headerLines
	maxVisible := min(len(b.branches), 15)

	if relY >= 0 && relY < maxVisible {
		idx := b.scrollOffset + relY
		if idx >= 0 && idx < len(b.branches) {
			br := b.branches[idx]
			b.Hide()
			return func() tea.Msg {
				return BranchNavigateMsg{Branch: br}
			}
		}
	}

	return nil
}

// ============================================================================
// Project Picker Modal
// ============================================================================

// ProjectListMsg carries the project list response for the picker.
type ProjectListMsg struct {
	Projects []types.ProjectInfo
	Err      error
}

// ProjectSelectMsg indicates a project was selected from the picker.
type ProjectSelectMsg struct {
	ProjectID string
}

// ProjectPickerModal is a modal for selecting and managing projects.
type ProjectPickerModal struct {
	*Modal
	projects     []types.ProjectInfo
	rpc          *RPCClient
	styles       *Styles
	scrollOffset int
}

// NewProjectPickerModal creates a new project picker modal.
func NewProjectPickerModal(styles *Styles, rpc *RPCClient) *ProjectPickerModal {
	m := NewModal("projects", styles)
	m.width = 90

	return &ProjectPickerModal{
		Modal:    m,
		projects: []types.ProjectInfo{},
		rpc:      rpc,
		styles:   styles,
	}
}

// RefreshProjects fetches the project list from the daemon.
func (p *ProjectPickerModal) RefreshProjects() tea.Cmd {
	return func() tea.Msg {
		if p.rpc == nil || !p.rpc.IsConnected() {
			return ProjectListMsg{Projects: nil, Err: fmt.Errorf("not connected")}
		}

		resp, err := p.rpc.ListProjects()
		if err != nil {
			return ProjectListMsg{Projects: nil, Err: err}
		}

		return ProjectListMsg{Projects: resp.Projects, Err: nil}
	}
}

// SetProjects updates the project list and resets selection.
func (p *ProjectPickerModal) SetProjects(projects []types.ProjectInfo) {
	p.projects = projects
	p.selected = 0
	p.scrollOffset = 0
}

// View renders the project picker modal.
func (p *ProjectPickerModal) View(screenW, screenH int) string {
	if !p.visible {
		return ""
	}

	var sb strings.Builder

	boxStyle := p.styles.ModalBox.Width(p.width)

	// Title
	titleStyle := p.styles.ModalTitle.Width(p.width - 4)
	sb.WriteString(titleStyle.Render("projects"))
	sb.WriteString("\n")

	// Separator
	sb.WriteString(p.styles.Muted.Render(strings.Repeat("─", p.width-4)))
	sb.WriteString("\n")

	if len(p.projects) == 0 {
		sb.WriteString(p.styles.Muted.Render("no projects registered"))
		sb.WriteString("\n\n")
		sb.WriteString(p.styles.HelpKey.Render("[a]"))
		sb.WriteString(p.styles.HelpValue.Render(" add new project"))
	} else {
		// Column widths
		nameColWidth := 18
		modeColWidth := 6
		branchColWidth := 14
		statusColWidth := 8
		pathColWidth := p.width - nameColWidth - modeColWidth - branchColWidth - statusColWidth - 14

		// Header
		header := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s  %s",
			nameColWidth, "name",
			modeColWidth, "mode",
			branchColWidth, "branch",
			statusColWidth, "status",
			"path")
		sb.WriteString(p.styles.Muted.Render(header))
		sb.WriteString("\n")
		sb.WriteString(p.styles.Muted.Render(strings.Repeat("─", p.width-4)))
		sb.WriteString("\n")

		// Calculate visible range
		maxVisible := min(len(p.projects), 15)
		if p.scrollOffset > len(p.projects)-maxVisible {
			p.scrollOffset = max(0, len(p.projects)-maxVisible)
		}
		end := min(p.scrollOffset+maxVisible, len(p.projects))

		for i := p.scrollOffset; i < end; i++ {
			proj := p.projects[i]
			style := p.styles.ModalItem
			if i == p.selected {
				style = p.styles.ModalItemSelected
			}

			pointer := "  "
			if i == p.selected {
				pointer = "▸ "
			}

			activeIndicator := " "
			if proj.Status == "active" {
				activeIndicator = "*"
			}

			name := proj.Name
			if len(name) > nameColWidth {
				name = name[:nameColWidth-3] + "..."
			} else {
				name = fmt.Sprintf("%-*s", nameColWidth, name)
			}

			mode := fmt.Sprintf("%-*s", modeColWidth, proj.Mode)

			branch := proj.Branch
			if branch == "" {
				branch = "-"
			}
			if len(branch) > branchColWidth {
				branch = branch[:branchColWidth-3] + "..."
			} else {
				branch = fmt.Sprintf("%-*s", branchColWidth, branch)
			}

			status := proj.Status
			if len(status) > statusColWidth {
				status = status[:statusColWidth-3] + "..."
			} else {
				status = fmt.Sprintf("%-*s", statusColWidth, status)
			}

			path := proj.LocalPath
			if len(path) > pathColWidth {
				path = "..." + path[len(path)-pathColWidth+3:]
			}

			activeStyle := p.styles.Muted
			if i == p.selected {
				activeStyle = p.styles.ModalItemSelected
			}
			activeStr := activeStyle.Render(activeIndicator)

			line := fmt.Sprintf("%s%s %s %s %s %s  %s", pointer, activeStr, name, mode, branch, status, path)
			sb.WriteString(style.Render(line))
			sb.WriteString("\n")
		}

		if len(p.projects) > maxVisible {
			sb.WriteString(p.styles.Muted.Render(fmt.Sprintf("  showing %d-%d of %d", p.scrollOffset+1, end, len(p.projects))))
			sb.WriteString("\n")
		}
	}

	// Footer with actions
	sb.WriteString("\n")
	sb.WriteString(p.styles.Muted.Render(strings.Repeat("─", p.width-4)))
	sb.WriteString("\n")
	actions := []string{
		p.styles.HelpKey.Render("[enter]") + p.styles.HelpValue.Render(" select"),
		p.styles.HelpKey.Render("[a]") + p.styles.HelpValue.Render(" add"),
		p.styles.HelpKey.Render("[s]") + p.styles.HelpValue.Render(" sync"),
		p.styles.HelpKey.Render("[d]") + p.styles.HelpValue.Render(" remove"),
		p.styles.HelpKey.Render("[esc]") + p.styles.HelpValue.Render(" cancel"),
	}
	sb.WriteString(strings.Join(actions, "  "))

	content := boxStyle.Render(sb.String())
	return lipgloss.Place(screenW, screenH, lipgloss.Center, lipgloss.Center, content)
}

// HandleKey processes key input for the project picker.
func (p *ProjectPickerModal) HandleKey(keyStr string) tea.Cmd {
	if len(p.projects) == 0 {
		switch keyStr {
		case "a":
			return nil
		case KeyEsc, "q":
			p.Hide()
		}
		return nil
	}

	maxVisible := min(len(p.projects), 15)

	switch keyStr {
	case "up", "k":
		if p.selected > 0 {
			p.selected--
			if p.selected < p.scrollOffset {
				p.scrollOffset = p.selected
			}
		}
	case KeyDown, "j":
		if p.selected < len(p.projects)-1 {
			p.selected++
			if p.selected >= p.scrollOffset+maxVisible {
				p.scrollOffset = p.selected - maxVisible + 1
			}
		}
	case KeyEnter:
		if p.selected >= 0 && p.selected < len(p.projects) {
			proj := p.projects[p.selected]
			p.Hide()
			return func() tea.Msg {
				return ProjectSelectMsg{ProjectID: proj.ID}
			}
		}
	case "a":
		return nil
	case "s":
		if p.selected >= 0 && p.selected < len(p.projects) {
			proj := p.projects[p.selected]
			projID := proj.ID
			projName := proj.Name
			return func() tea.Msg {
				if p.rpc == nil {
					return nil
				}
				err := p.rpc.SyncProject(projID)
				if err != nil {
					return CommandResultMsg{Result: &CommandResult{
						Output:  fmt.Sprintf("failed to sync project %s: %v", projName, err),
						IsError: true,
					}}
				}
				return CommandResultMsg{Result: &CommandResult{
					Output: fmt.Sprintf("synced project: %s", projName),
				}}
			}
		}
	case "d":
		if p.selected >= 0 && p.selected < len(p.projects) {
			proj := p.projects[p.selected]
			projID := proj.ID
			projName := proj.Name
			p.Hide()
			return func() tea.Msg {
				if p.rpc == nil {
					return nil
				}
				err := p.rpc.UnregisterProject(projID)
				if err != nil {
					return CommandResultMsg{Result: &CommandResult{
						Output:  fmt.Sprintf("failed to remove project %s: %v", projName, err),
						IsError: true,
					}}
				}
				return CommandResultMsg{Result: &CommandResult{
					Output: fmt.Sprintf("removed project: %s", projName),
				}}
			}
		}
	case KeyEsc, "q":
		p.Hide()
	}

	return nil
}
