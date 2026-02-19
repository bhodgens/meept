package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/tui/types"
)

// ViewType represents the different views in the TUI.
type ViewType int

const (
	ViewChat ViewType = iota
	ViewStatus
	ViewTasks
	ViewQueue
	ViewMemory
)

// Note: Command mode timeout removed - now using modal that stays open until dismissed

// AppFocus tracks which component has focus.
type AppFocus int

const (
	FocusChat AppFocus = iota
	FocusSidebar
)

// App is the main bubbletea model for the TUI.
type App struct {
	width      int
	height     int
	styles     *Styles
	rpc        *RPCClient
	currentView ViewType

	// Sub-models for each view
	chat   *models.ChatModel
	status *models.StatusModel
	tasks  *models.TasksModel
	queue  *models.QueueModel
	memory *models.MemoryModel

	// Sidebar
	sidebar *SidebarModel

	// Focus management
	appFocus AppFocus

	// Key bindings
	keys KeyMap

	// Client configuration
	clientConfig *ClientConfig

	// Modal state
	activeModal    ModalType
	commandPalette *Modal
	sessionPicker  *SessionPickerModal

	// Current session
	currentSession *types.Session

	// Copy mode - disables mouse capture for native text selection
	copyMode bool

	// Project directory
	projectDir string

	// Status message (for clipboard feedback)
	statusMessage     string
	statusMessageTime time.Time

	// Error state
	err error
}

// KeyMap defines the key bindings.
type KeyMap struct {
	Quit    key.Binding
	Enter   key.Binding
	Escape  key.Binding
	Help    key.Binding
	Command key.Binding // Ctrl+X prefix
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		Help: key.NewBinding(
			key.WithKeys("ctrl+?"),
			key.WithHelp("ctrl+?", "help"),
		),
		Command: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("ctrl+x", "command mode"),
		),
	}
}

// NewApp creates a new TUI application.
func NewApp(socketPath string) *App {
	rpc := NewRPCClient(socketPath)
	styles := DefaultStyles()

	// Load client configuration
	clientConfig, _ := LoadClientConfig()

	// Get current working directory for display
	projectDir, _ := os.Getwd()

	app := &App{
		styles:       styles,
		rpc:          rpc,
		currentView:  ViewChat,
		chat:         models.NewChatModel(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage),
		status:       models.NewStatusModel(rpc),
		tasks:        models.NewTasksModel(rpc),
		queue:        models.NewQueueModel(rpc),
		memory:       models.NewMemoryModel(rpc),
		sidebar:      NewSidebarModel(rpc, styles),
		keys:         DefaultKeyMap(),
		clientConfig: clientConfig,
		projectDir:   projectDir,
		activeModal:  ModalNone,
	}

	// Create modals
	app.commandPalette = CommandPaletteModal(styles, clientConfig)
	app.sessionPicker = NewSessionPickerModal(styles, rpc, clientConfig)

	return app
}

// Init initializes the application.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.connectDaemon,
		a.loadSession,
		tea.EnterAltScreen,
		tea.SetWindowTitle("Meept"),
		// Use EnableMouseAllMotion for better compatibility but let shift bypass for selection
		// Note: Hold Shift while dragging to select text in most terminals
		tea.EnableMouseAllMotion,
	)
}

// loadSession attempts to load or create a session.
func (a *App) loadSession() tea.Msg {
	// Wait for connection first - this will be called after connectDaemon
	if !a.rpc.IsConnected() {
		return nil
	}

	// Try to auto-resume if enabled
	if a.clientConfig.Session.AutoResume {
		session, err := a.rpc.GetMostRecentSession()
		if err == nil && session != nil {
			return SessionLoadedMsg{Session: session, IsNew: false}
		}
	}

	// Create a new session
	session, err := a.rpc.CreateSession(a.clientConfig.Session.DefaultName)
	if err != nil {
		return SessionLoadedMsg{Session: nil, Err: err}
	}

	return SessionLoadedMsg{Session: session, IsNew: true}
}

// SessionLoadedMsg indicates a session was loaded or created.
type SessionLoadedMsg struct {
	Session *types.Session
	IsNew   bool
	Err     error
}


// connectDaemon is a command that connects to the daemon.
func (a *App) connectDaemon() tea.Msg {
	if err := a.rpc.Connect(); err != nil {
		return ConnectErrorMsg{Err: err}
	}
	return ConnectSuccessMsg{}
}

// ConnectSuccessMsg indicates successful daemon connection.
type ConnectSuccessMsg struct{}

// ConnectErrorMsg indicates a connection error.
type ConnectErrorMsg struct {
	Err error
}

// Update handles messages and updates the model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		// Calculate sidebar width (30% of screen when visible, max 40 chars)
		sidebarWidth := 0
		if a.sidebar.IsVisible() {
			sidebarWidth = msg.Width * 30 / 100
			if sidebarWidth > 40 {
				sidebarWidth = 40
			}
			if sidebarWidth < 20 {
				sidebarWidth = 20
			}
		}
		a.sidebar.SetSize(sidebarWidth, msg.Height-4)

		// Update sub-models with remaining width
		mainWidth := msg.Width - sidebarWidth
		a.chat.SetSize(mainWidth, msg.Height-4) // Account for tabs and status bar
		a.status.SetSize(mainWidth, msg.Height-4)
		a.tasks.SetSize(mainWidth, msg.Height-4)
		a.queue.SetSize(mainWidth, msg.Height-4)
		a.memory.SetSize(mainWidth, msg.Height-4)
		return a, nil

	case tea.MouseMsg:
		// In copy mode or modal open, ignore mouse events
		if a.copyMode || a.activeModal != ModalNone {
			return a, nil
		}

		// Handle mouse clicks on tab bar (first row)
		if msg.Type == tea.MouseLeft && msg.Y == 0 {
			// Calculate which tab was clicked based on X position
			// Tab layout: "[1] Chat  [2] Status  [3] Tasks  [4] Queue  [5] Memory"
			// Approximate positions (accounting for padding):
			tabWidths := []int{10, 12, 11, 11, 12} // "[1] Chat", "[2] Status", "[3] Tasks", "[4] Queue", "[5] Memory"
			x := msg.X
			cumWidth := 0
			for i, w := range tabWidths {
				cumWidth += w
				if x < cumWidth {
					a.currentView = ViewType(i)
					return a, a.initCurrentView()
				}
			}
		}
		// Pass mouse events to current view
		var cmd tea.Cmd
		switch a.currentView {
		case ViewChat:
			cmd = a.chat.Update(msg)
		case ViewStatus:
			cmd = a.status.Update(msg)
		case ViewTasks:
			cmd = a.tasks.Update(msg)
		case ViewQueue:
			cmd = a.queue.Update(msg)
		case ViewMemory:
			cmd = a.memory.Update(msg)
		}
		return a, cmd

	case tea.KeyMsg:
		// Ctrl+C always quits
		if key.Matches(msg, a.keys.Quit) {
			a.rpc.Close()
			return a, tea.Quit
		}

		// Handle modal key input first
		if a.activeModal != ModalNone {
			return a.handleModalKey(msg)
		}

		// Check for Ctrl+X to open command palette
		if key.Matches(msg, a.keys.Command) {
			a.activeModal = ModalCommandPalette
			a.commandPalette.Show()
			return a, nil
		}

		// Exit copy mode on any key press
		if a.copyMode {
			a.copyMode = false
			a.statusMessage = ""
			return a, func() tea.Msg {
				enableMouseTracking()
				return nil
			}
		}

		// Not in modal mode - delegate based on focus
		if a.appFocus == FocusSidebar && a.sidebar.IsVisible() {
			cmd := a.sidebar.Update(msg)
			return a, cmd
		}
		// Otherwise fall through to delegate to current view

	case ConnectSuccessMsg:
		a.err = nil
		// Initialize the current view, sidebar, and load session
		return a, tea.Batch(a.initCurrentView(), a.sidebar.Init(), a.loadSession)

	case SessionLoadedMsg:
		if msg.Err != nil {
			a.statusMessage = fmt.Sprintf("Session error: %v", msg.Err)
			a.statusMessageTime = time.Now()
		} else if msg.Session != nil {
			a.currentSession = msg.Session
			a.chat.SetSession(msg.Session)
			if msg.IsNew {
				a.statusMessage = fmt.Sprintf("Created session: %s", msg.Session.Name)
			} else {
				a.statusMessage = fmt.Sprintf("Resumed session: %s", msg.Session.Name)
			}
			a.statusMessageTime = time.Now()
			return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		}
		return a, nil

	case SessionListMsg:
		// Update session picker with session list
		if msg.Err == nil {
			a.sessionPicker.SetSessions(msg.Sessions)
		}
		return a, nil

	case SessionSwitchMsg:
		// Switch to selected session
		if msg.Session != nil {
			a.currentSession = msg.Session
			a.chat.SetSession(msg.Session)
			a.statusMessage = fmt.Sprintf("Switched to: %s", msg.Session.Name)
			a.statusMessageTime = time.Now()
			return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		}
		return a, nil

	case SessionCreateMsg:
		// Create a new session
		return a, a.createSession(msg.Name)

	case SessionDeleteMsg:
		// Delete a session
		return a, a.deleteSession(msg.SessionID)

	case SidebarDataMsg:
		// Delegate to sidebar
		return a, a.sidebar.Update(msg)

	case models.ChatFocusSidebarMsg:
		// Chat wants to move focus to sidebar
		if a.sidebar.IsVisible() {
			a.appFocus = FocusSidebar
			a.sidebar.SetFocused(true)
		} else {
			// No sidebar, cycle back to chat input
			a.chat.SetFocusFromSidebar()
		}
		return a, nil

	case SidebarFocusChatMsg:
		// Sidebar wants to move focus back to chat
		a.appFocus = FocusChat
		a.sidebar.SetFocused(false)
		a.chat.SetFocusFromSidebar()
		return a, nil

	case ConnectErrorMsg:
		a.err = msg.Err
		return a, nil

	case models.CopyToClipboardMsg:
		// Handle clipboard copy request from chat view
		return a, doCopy(msg.Text)

	case CopySuccessMsg:
		// Show success feedback
		preview := msg.Text
		if len(preview) > 30 {
			preview = preview[:30] + "..."
		}
		a.statusMessage = fmt.Sprintf("Copied: %s", preview)
		a.statusMessageTime = time.Now()
		// Clear message after 2 seconds
		return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusMessageClearMsg{}
		})

	case CopyErrorMsg:
		a.statusMessage = fmt.Sprintf("Copy failed: %v", msg.Err)
		a.statusMessageTime = time.Now()
		return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusMessageClearMsg{}
		})

	case StatusMessageClearMsg:
		if time.Since(a.statusMessageTime) >= 2*time.Second {
			a.statusMessage = ""
		}
		return a, nil
	}

	// Delegate to current view
	var cmd tea.Cmd
	switch a.currentView {
	case ViewChat:
		cmd = a.chat.Update(msg)
	case ViewStatus:
		cmd = a.status.Update(msg)
	case ViewTasks:
		cmd = a.tasks.Update(msg)
	case ViewQueue:
		cmd = a.queue.Update(msg)
	case ViewMemory:
		cmd = a.memory.Update(msg)
	}
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
}

// handleModalKey processes key input when a modal is active.
func (a *App) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()

	switch a.activeModal {
	case ModalCommandPalette:
		action := a.commandPalette.HandleKey(keyStr)
		if !a.commandPalette.IsVisible() {
			a.activeModal = ModalNone
		}

		// Handle command palette actions
		keys := a.clientConfig.Keybindings.CommandPalette
		switch action {
		case keys.ViewChat:
			a.currentView = ViewChat
			return a, a.initCurrentView()
		case keys.ViewStatus:
			a.currentView = ViewStatus
			return a, a.initCurrentView()
		case keys.ViewTasks:
			a.currentView = ViewTasks
			return a, a.initCurrentView()
		case keys.ViewQueue:
			a.currentView = ViewQueue
			return a, a.initCurrentView()
		case keys.ViewMemory:
			a.currentView = ViewMemory
			return a, a.initCurrentView()
		case keys.Sidebar:
			a.sidebar.Toggle()
			return a, func() tea.Msg {
				return tea.WindowSizeMsg{Width: a.width, Height: a.height}
			}
		case keys.Sessions:
			a.activeModal = ModalSessionPicker
			a.sessionPicker.Show()
			return a, a.sessionPicker.RefreshSessions()
		case keys.CopyMode:
			a.copyMode = true
			a.statusMessage = "COPY MODE: Select text with mouse, Cmd+C to copy, any key to exit"
			a.statusMessageTime = time.Now()
			return a, func() tea.Msg {
				disableMouseTracking()
				return nil
			}
		}
		return a, nil

	case ModalSessionPicker:
		cmd := a.sessionPicker.HandleKey(keyStr)
		if !a.sessionPicker.IsVisible() {
			a.activeModal = ModalNone
		}
		return a, cmd
	}

	return a, nil
}

// createSession creates a new session via RPC.
func (a *App) createSession(name string) tea.Cmd {
	return func() tea.Msg {
		session, err := a.rpc.CreateSession(name)
		if err != nil {
			return SessionLoadedMsg{Session: nil, Err: err}
		}
		return SessionSwitchMsg{Session: session}
	}
}

// deleteSession deletes a session via RPC.
func (a *App) deleteSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		err := a.rpc.DeleteSession(sessionID)
		if err != nil {
			// Just refresh the list to show current state
		}
		// Refresh session list
		resp, _ := a.rpc.ListSessions()
		if resp != nil {
			return SessionListMsg{Sessions: resp.Sessions}
		}
		return SessionListMsg{Sessions: nil}
	}
}

// initCurrentView returns a command to initialize the current view.
func (a *App) initCurrentView() tea.Cmd {
	switch a.currentView {
	case ViewChat:
		return a.chat.Init()
	case ViewStatus:
		return a.status.Init()
	case ViewTasks:
		return a.tasks.Init()
	case ViewQueue:
		return a.queue.Init()
	case ViewMemory:
		return a.memory.Init()
	}
	return nil
}

// View renders the application.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}

	// Render modal overlay if active
	if a.activeModal != ModalNone {
		return a.renderModalOverlay()
	}

	var b strings.Builder

	// Render tabs
	b.WriteString(a.renderTabs())
	b.WriteString("\n")

	// Render main content area (view + optional sidebar)
	var mainView string
	if a.err != nil {
		mainView = a.renderError()
	} else {
		switch a.currentView {
		case ViewChat:
			mainView = a.chat.View()
		case ViewStatus:
			mainView = a.status.View()
		case ViewTasks:
			mainView = a.tasks.View()
		case ViewQueue:
			mainView = a.queue.View()
		case ViewMemory:
			mainView = a.memory.View()
		}
	}

	// If sidebar is visible, render it alongside the main view
	if a.sidebar.IsVisible() {
		sidebarView := a.sidebar.View()
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, mainView, sidebarView))
	} else {
		b.WriteString(mainView)
	}

	// Render status bar
	b.WriteString("\n")
	b.WriteString(a.renderStatusBar())

	return b.String()
}

// renderModalOverlay renders the active modal centered on a dimmed background.
func (a *App) renderModalOverlay() string {
	switch a.activeModal {
	case ModalCommandPalette:
		return a.commandPalette.View(a.width, a.height)
	case ModalSessionPicker:
		return a.sessionPicker.View(a.width, a.height)
	}
	return ""
}

func (a *App) renderTabs() string {
	tabs := []struct {
		name string
		view ViewType
	}{
		{"Chat", ViewChat},
		{"Status", ViewStatus},
		{"Tasks", ViewTasks},
		{"Queue", ViewQueue},
		{"Memory", ViewMemory},
	}

	var renderedTabs []string
	for i, t := range tabs {
		style := a.styles.Tab
		if t.view == a.currentView {
			style = a.styles.ActiveTab
		}
		label := fmt.Sprintf("[%d] %s", i+1, t.name)
		renderedTabs = append(renderedTabs, style.Render(label))
	}

	tabLine := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	// Add session indicator if we have one
	var sessionIndicator string
	if a.currentSession != nil {
		sessionIndicator = a.styles.Muted.Render(" [" + a.currentSession.Name + "]")
	}

	// Add separator
	separatorWidth := max(0, a.width-lipgloss.Width(tabLine)-lipgloss.Width(sessionIndicator))
	separator := strings.Repeat("-", separatorWidth)

	return lipgloss.NewStyle().
		Width(a.width).
		Render(tabLine + a.styles.Muted.Render(separator) + sessionIndicator)
}

func (a *App) renderStatusBar() string {
	var left string

	// Show status message if present
	if a.statusMessage != "" {
		left = a.styles.StatusRunning.Render(a.statusMessage)
	} else if a.copyMode {
		left = a.styles.CommandModeIndicator.Render(" COPY MODE ") +
			a.styles.HelpValue.Render("Select text with mouse | Press any key to exit")
	} else {
		left = a.styles.HelpKey.Render("Ctrl+X") + a.styles.HelpValue.Render(" menu") + " | " +
			a.styles.HelpKey.Render("Ctrl+C") + a.styles.HelpValue.Render(" quit") + " | " +
			a.styles.Muted.Render("Shift+drag to select")
	}

	// Connection status
	connectionStatus := "disconnected"
	statusStyle := a.styles.StatusStopped
	if a.rpc.IsConnected() {
		connectionStatus = "connected"
		statusStyle = a.styles.StatusRunning
	}

	// Project directory (shortened if necessary)
	projectDisplay := a.projectDir
	maxProjectLen := 30
	if len(projectDisplay) > maxProjectLen {
		projectDisplay = "..." + projectDisplay[len(projectDisplay)-maxProjectLen+3:]
	}

	right := a.styles.Muted.Render(projectDisplay) + " | " + statusStyle.Render(connectionStatus)

	// Calculate spacing
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	spacing := a.width - leftWidth - rightWidth
	if spacing < 0 {
		spacing = 0
	}

	return a.styles.StatusBar.
		Width(a.width).
		Render(left + strings.Repeat(" ", spacing) + right)
}

func (a *App) renderError() string {
	errMsg := fmt.Sprintf("Error: %v", a.err)
	return a.styles.Panel.
		Width(a.width - 4).
		Render(
			a.styles.Error.Render("Connection Error") + "\n\n" +
				a.styles.Paragraph.Render(errMsg) + "\n\n" +
				a.styles.Muted.Render("Make sure the meept daemon is running:\n  meept daemon start"),
		)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// copyToClipboard copies text to the system clipboard using OSC52 and fallback methods.
func copyToClipboard(text string) error {
	// Try OSC52 first (works in most modern terminals)
	// This writes directly to terminal and should work even over SSH
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	osc52 := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)
	fmt.Print(osc52)

	// Also try platform-specific clipboard as backup
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run() // Ignore error, OSC52 might have worked
	case "linux":
		// Try xclip first, then xsel
		if cmd := exec.Command("xclip", "-selection", "clipboard"); cmd != nil {
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		if cmd := exec.Command("xsel", "--clipboard", "--input"); cmd != nil {
			cmd.Stdin = strings.NewReader(text)
			_ = cmd.Run()
		}
	}
	return nil
}

// CopySuccessMsg indicates clipboard copy succeeded.
type CopySuccessMsg struct {
	Text string
}

// CopyErrorMsg indicates clipboard copy failed.
type CopyErrorMsg struct {
	Err error
}

// doCopy is a command that copies text to clipboard.
func doCopy(text string) tea.Cmd {
	return func() tea.Msg {
		if err := copyToClipboard(text); err != nil {
			return CopyErrorMsg{Err: err}
		}
		return CopySuccessMsg{Text: text}
	}
}

// StatusMessageClearMsg clears the status message.
type StatusMessageClearMsg struct{}

// disableMouseTracking sends escape sequences to disable mouse tracking.
// This allows native terminal text selection to work.
func disableMouseTracking() tea.Msg {
	// Send escape sequences to disable various mouse modes:
	// \x1b[?1000l - disable normal tracking
	// \x1b[?1002l - disable button event tracking
	// \x1b[?1003l - disable all motion tracking
	// \x1b[?1006l - disable SGR extended mode
	fmt.Print("\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l")
	return nil
}

// enableMouseTracking re-enables mouse tracking.
func enableMouseTracking() tea.Msg {
	// Re-enable mouse tracking modes
	fmt.Print("\x1b[?1003h\x1b[?1006h")
	return nil
}
