package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/tui/types"
	"github.com/caimlas/meept/internal/tui/viz"
)

// ViewType represents the different views in the TUI.
type ViewType int

const (
	ViewChat ViewType = iota
	ViewTasks
	ViewQueue
	ViewMemory
)

// AppFocus tracks which component has focus.
type AppFocus int

const (
	FocusChat AppFocus = iota
	FocusSidebar
)

// App is the main bubbletea model for the TUI.
type App struct {
	width        int
	height       int
	sidebarWidth int // cached for status bar width calculation
	styles       *Styles
	rpc         *RPCClient
	eventRPC    *RPCClient // Separate connection for event polling
	currentView ViewType

	// Sub-models for each view
	chat   *models.ChatModel
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
	sessionRename  *SessionRenameModal
	confirmModal   *ConfirmModal

	// Current session
	currentSession *types.Session

	// Project directory
	projectDir string

	// Status message (for clipboard feedback)
	statusMessage     string
	statusMessageTime time.Time

	// Double-press tracking for Ctrl-C/Ctrl-D
	lastCtrlC      time.Time
	lastCtrlD      time.Time
	doublePressTTL time.Duration

	// Tab flash indicator (shows notification dot on tab)
	tabFlash     map[ViewType]bool
	tabFlashTime time.Time

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
	// Separate RPC client for event stream polling so it doesn't block
	// on the main client's callMu while a Chat call is in-flight
	eventRPC := NewRPCClient(socketPath)
	styles := DefaultStyles()

	// Load client configuration
	clientConfig, _ := LoadClientConfig()

	// Get current working directory for display
	projectDir, _ := os.Getwd()

	// Create input behavior config from client config
	inputConfig := models.InputBehaviorConfig{
		EnterBehavior: clientConfig.Input.EnterBehavior,
		AutoExpand:    clientConfig.Input.AutoExpand,
	}

	app := &App{
		styles:         styles,
		rpc:            rpc,
		eventRPC:       eventRPC,
		currentView:    ViewChat,
		chat:           models.NewChatModelWithConfig(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage, clientConfig.Keybindings.EscapeBehavior, inputConfig, models.ChatConfig{
		AutoCopyOnRelease: clientConfig.Chat.AutoCopyOnRelease,
		ScrollSpeed:       clientConfig.Chat.ScrollSpeed,
	}),
		tasks:          models.NewTasksModel(rpc),
		queue:          models.NewQueueModel(rpc),
		memory:         models.NewMemoryModel(rpc),
		sidebar:        NewSidebarModel(rpc, eventRPC, styles, clientConfig.Rendering.SidebarAnimation),
		keys:           DefaultKeyMap(),
		clientConfig:   clientConfig,
		projectDir:     projectDir,
		activeModal:    ModalNone,
		doublePressTTL: 500 * time.Millisecond,
		tabFlash:       make(map[ViewType]bool),
	}

	// Create modals
	app.commandPalette = CommandPaletteModal(styles, clientConfig)
	app.sessionPicker = NewSessionPickerModal(styles, rpc, clientConfig)
	app.sessionRename = NewSessionRenameModal(styles)

	return app
}

// Init initializes the application.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.connectDaemon,
		a.loadSession,
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
	// Connect the event stream RPC client on its own connection
	if a.eventRPC != nil {
		_ = a.eventRPC.Connect()
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

		// Calculate reserved height for chrome (header + status bar)
		chromeHeight := 1 // status bar
		if a.clientConfig.Rendering.ShowHeader {
			chromeHeight = 2 // header + status bar
		}

		// Calculate sidebar width (30% of screen when visible, max 40 chars)
		a.sidebarWidth = 0
		if a.sidebar.IsVisible() {
			a.sidebarWidth = msg.Width * 30 / 100
			if a.sidebarWidth > 40 {
				a.sidebarWidth = 40
			}
			if a.sidebarWidth < 20 {
				a.sidebarWidth = 20
			}
		}
		a.sidebar.SetSize(a.sidebarWidth, msg.Height-chromeHeight)

		// Update sub-models with remaining width
		mainWidth := msg.Width - a.sidebarWidth
		a.chat.SetSize(mainWidth, msg.Height-chromeHeight)
		a.tasks.SetSize(mainWidth, msg.Height-chromeHeight)
		a.queue.SetSize(mainWidth, msg.Height-chromeHeight)
		a.memory.SetSize(mainWidth, msg.Height-chromeHeight)

		return a, nil

	case tea.KeyPressMsg:
		// Handle Ctrl+C - double-press to exit, single press to stop work or show hint
		if msg.String() == "ctrl+c" {
			now := time.Now()
			if now.Sub(a.lastCtrlC) < a.doublePressTTL {
				a.rpc.Close()
				if a.eventRPC != nil {
					a.eventRPC.Close()
				}
				return a, tea.Quit
			}
			a.lastCtrlC = now

			// If chat is loading, stop current work
			if a.chat != nil && a.chat.IsLoading() {
				return a, a.stopCurrentWork()
			}

			// Show hint message
			a.statusMessage = "press ctrl+c again to exit"
			a.statusMessageTime = time.Now()
			return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
		}

		// Handle Ctrl+D - double-press to exit
		if msg.String() == "ctrl+d" {
			now := time.Now()
			if now.Sub(a.lastCtrlD) < a.doublePressTTL {
				a.rpc.Close()
				if a.eventRPC != nil {
					a.eventRPC.Close()
				}
				return a, tea.Quit
			}
			a.lastCtrlD = now
			a.statusMessage = "press ctrl+d again to exit"
			a.statusMessageTime = time.Now()
			return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
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

		// Check for Ctrl+S to open session picker directly
		if msg.String() == "ctrl+s" {
			a.activeModal = ModalSessionPicker
			a.sessionPicker.Show()
			return a, a.sessionPicker.RefreshSessions()
		}

		// Global escape handler
		if msg.String() == "esc" {
			// If sidebar is focused, unfocus and return to chat
			if a.appFocus == FocusSidebar {
				a.appFocus = FocusChat
				a.sidebar.SetFocused(false)
				if a.currentView == ViewChat {
					return a, a.chat.HandleEscape()
				}
				return a, nil
			}
			// If on non-chat view, switch to chat
			if a.currentView != ViewChat {
				a.currentView = ViewChat
				a.chat.SetFocus(models.FocusInput)
				return a, a.initCurrentView()
			}
			// Delegate to chat's escape handling
			return a, a.chat.HandleEscape()
		}

		// Delegate based on focus
		if a.appFocus == FocusSidebar && a.sidebar.IsVisible() {
			// If it's a printable character, redirect focus to chat input
			if a.currentView == ViewChat && isPrintableKey(msg) {
				a.appFocus = FocusChat
				a.sidebar.SetFocused(false)
				a.chat.SetFocusFromSidebar()
				// Forward the key to the chat view
				cmd := a.chat.Update(msg)
				return a, cmd
			}
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
			// Wire up session ID for tasks FilterMine feature
			a.tasks.SetCurrentSession(msg.Session.ID)
			sessionCmd := a.chat.SetSession(msg.Session)
			if msg.IsNew {
				a.statusMessage = fmt.Sprintf("Created session: %s", msg.Session.Name)
			} else {
				a.statusMessage = fmt.Sprintf("Resumed session: %s", msg.Session.Name)
			}
			a.statusMessageTime = time.Now()
			clearCmd := tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
			if sessionCmd != nil {
				return a, tea.Batch(sessionCmd, clearCmd)
			}
			return a, clearCmd
		}
		return a, nil

	case SessionListMsg:
		// Update session picker with session list
		if msg.Err == nil {
			a.sessionPicker.SetSessions(msg.Sessions)
			// Auto-select current session
			if a.currentSession != nil {
				a.sessionPicker.SetCurrentSession(a.currentSession.ID)
			}
		}
		return a, nil

	case SessionSwitchMsg:
		// Switch to selected session
		if msg.Session != nil {
			a.currentSession = msg.Session
			// Wire up session ID for tasks FilterMine feature
			a.tasks.SetCurrentSession(msg.Session.ID)
			sessionCmd := a.chat.SetSession(msg.Session)
			a.statusMessage = fmt.Sprintf("Switched to: %s", msg.Session.Name)
			a.statusMessageTime = time.Now()
			clearCmd := tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return StatusMessageClearMsg{}
			})
			if sessionCmd != nil {
				return a, tea.Batch(sessionCmd, clearCmd)
			}
			return a, clearCmd
		}
		return a, nil

	case SessionCreateMsg:
		// Create a new session
		return a, a.createSession(msg.Name)

	case SessionDeleteMsg:
		// Delete a session
		return a, a.deleteSession(msg.SessionID)

	case OpenRenameModalMsg:
		// Open rename modal for a session
		a.activeModal = ModalSessionRename
		a.sessionRename.Show(msg.SessionID, msg.CurrentName)
		return a, nil

	case SessionRenameMsg:
		// Rename a session (update description)
		return a, a.renameSession(msg.SessionID, msg.NewName)

	case SidebarDataMsg:
		// Delegate to sidebar
		return a, a.sidebar.Update(msg)

	case EventStreamTickMsg:
		// Forward event stream tick to sidebar for polling
		return a, a.sidebar.Update(msg)

	case EventStreamDataMsg:
		// Process progress events directly to update chat model
		var cmds []tea.Cmd
		for _, e := range msg.Events {
			switch e.Topic {
			case "agent.progress":
				// Extract progress data and update chat directly
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					progressMsg := models.ProgressUpdateMsg{}
					if v, ok := payloadMap["agent_id"].(string); ok {
						progressMsg.AgentID = v
					} else if v, ok := payloadMap["conversation_id"].(string); ok {
						progressMsg.AgentID = v
					}
					if v, ok := payloadMap["stage"].(string); ok {
						progressMsg.Stage = v
					}
					if v, ok := payloadMap["detail"].(string); ok {
						progressMsg.CurrentTool = v
					}
					if v, ok := payloadMap["percent"].(float64); ok {
						progressMsg.Percent = v
					} else if iteration, ok := payloadMap["iteration"].(float64); ok {
						progressMsg.Percent = iteration * 10.0
						if progressMsg.Percent > 100 {
							progressMsg.Percent = 100
						}
					}
					if v, ok := payloadMap["token_count"].(float64); ok {
						progressMsg.TokensUsed = int(v)
					}
					// Update chat directly
					if cmd := a.chat.Update(progressMsg); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			case "llm.tokens.used":
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					if totalTokens, ok := payloadMap["total_tokens"].(float64); ok {
						progressMsg := models.ProgressUpdateMsg{TokensUsed: int(totalTokens)}
						a.chat.Update(progressMsg)
					}
				}
			case "conversation.reset":
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					if resets, ok := payloadMap["resets"].(float64); ok {
						progressMsg := models.ProgressUpdateMsg{ContextResets: int(resets)}
						a.chat.Update(progressMsg)
					}
				}
			case "task.completed", "task.failed":
				// Inject task result message into chat
				if payloadMap, ok := e.Payload.(map[string]any); ok {
					resultMsg := models.ChatTaskResultMsg{
						State: "completed",
					}
					if e.Topic == "task.failed" {
						resultMsg.State = "failed"
					}
					if v, ok := payloadMap["task_id"].(string); ok {
						resultMsg.TaskID = v
					}
					if v, ok := payloadMap["name"].(string); ok {
						resultMsg.TaskName = v
					}
					if v, ok := payloadMap["completed_jobs"].(float64); ok {
						resultMsg.CompletedSteps = int(v)
					}
					if v, ok := payloadMap["total_jobs"].(float64); ok {
						resultMsg.TotalSteps = int(v)
					}
					if v, ok := payloadMap["result"].(string); ok {
						resultMsg.ResultSummary = v
					}
					if cmd := a.chat.Update(resultMsg); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
				// Flash the Tasks tab indicator if not currently viewing tasks
				if a.currentView != ViewTasks {
					a.tabFlash[ViewTasks] = true
					a.tabFlashTime = time.Now()
				}
			}
		}
		// Also forward to sidebar for activity feed updates
		if sidebarCmd := a.sidebar.Update(msg); sidebarCmd != nil {
			cmds = append(cmds, sidebarCmd)
		}
		if len(cmds) > 0 {
			return a, tea.Batch(cmds...)
		}
		return a, nil

	case models.ProgressUpdateMsg:
		// Forward progress updates to chat model
		return a, a.chat.Update(msg)

	case viz.VizTickMsg:
		// Forward viz tick to sidebar
		if a.sidebar.IsVisible() {
			return a, a.sidebar.Update(msg)
		}
		return a, nil

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
		// Copy silently - do not display a "Copied: ..." status message.
		return a, nil

	case CopyErrorMsg:
		a.statusMessage = fmt.Sprintf("Copy failed: %v", msg.Err)
		a.statusMessageTime = time.Now()

	case StopSessionResultMsg:
		a.activeModal = ModalNone
		if msg.Error != nil {
			a.statusMessage = fmt.Sprintf("Stop failed: %v", msg.Error)
		} else {
			workers := len(msg.Response.WorkersStopped)
			a.statusMessage = fmt.Sprintf("Stopped %d worker(s)", workers)
			// Reset loading state in chat
			if a.chat != nil {
				a.chat.ClearLoading()
			}
		}
		a.statusMessageTime = time.Now()
		return a, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusMessageClearMsg{}
		})

	case StatusMessageClearMsg:
		if time.Since(a.statusMessageTime) >= 2*time.Second {
			a.statusMessage = ""
		}
		return a, nil

	case models.SessionDescriptionUpdatedMsg:
		// Update the app-level session name and description so tab bar and picker reflect it
		if a.currentSession != nil && msg.SessionID == a.currentSession.ID {
			if msg.Name != "" {
				a.currentSession.Name = msg.Name
			}
			a.currentSession.Description = msg.Description
		}
		// Still delegate to chat model
	}

	// Delegate to current view
	var cmd tea.Cmd
	switch a.currentView {
	case ViewChat:
		cmd = a.chat.Update(msg)
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
		case keys.NewSession:
			// Create a new session directly with default name
			return a, a.createSession(a.clientConfig.Session.DefaultName)
		case keys.RenameSession:
			// Rename current session
			if a.currentSession != nil {
				currentName := a.currentSession.Description
				if currentName == "" {
					currentName = a.currentSession.Name
				}
				a.activeModal = ModalSessionRename
				a.sessionRename.Show(a.currentSession.ID, currentName)
			}
			return a, nil
		}
		return a, nil

	case ModalSessionPicker:
		cmd := a.sessionPicker.HandleKey(keyStr)
		if !a.sessionPicker.IsVisible() {
			a.activeModal = ModalNone
		}
		return a, cmd

	case ModalSessionRename:
		cmd := a.sessionRename.HandleKey(keyStr)
		if !a.sessionRename.IsVisible() {
			a.activeModal = ModalNone
		}
		return a, cmd

	case ModalConfirm:
		if a.confirmModal != nil {
			cmd := a.confirmModal.HandleKey(keyStr)
			if !a.confirmModal.IsVisible() {
				a.activeModal = ModalNone
			}
			return a, cmd
		}
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

// renameSession renames a session via RPC (updates description).
func (a *App) renameSession(sessionID, newName string) tea.Cmd {
	return func() tea.Msg {
		err := a.rpc.UpdateSessionDescription(sessionID, newName)
		if err != nil {
			return CopyErrorMsg{Err: err} // Reuse error display
		}
		// Return message to update UI
		return models.SessionDescriptionUpdatedMsg{
			SessionID:   sessionID,
			Description: newName,
		}
	}
}

// initCurrentView returns a command to initialize the current view.
func (a *App) initCurrentView() tea.Cmd {
	switch a.currentView {
	case ViewChat:
		return a.chat.Init()
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
func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		return tea.NewView("Loading...")
	}

	// Render modal overlay if active
	if a.activeModal != ModalNone {
		v := tea.NewView(a.renderModalOverlay())
		v.AltScreen = true
		v.WindowTitle = a.getWindowTitle()
		v.MouseMode = tea.MouseModeAllMotion
		return v
	}

	var b strings.Builder

	// Render header bar (orange with session info) if enabled
	if a.clientConfig.Rendering.ShowHeader {
		b.WriteString(a.renderHeader())
		b.WriteString("\n")
	}

	// Render main content area (view + optional sidebar)
	var mainView string
	if a.err != nil {
		mainView = a.renderError()
	} else {
		switch a.currentView {
		case ViewChat:
			mainView = a.chat.View()
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

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.WindowTitle = a.getWindowTitle()
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

// renderModalOverlay renders the active modal centered on a dimmed background.
func (a *App) renderModalOverlay() string {
	switch a.activeModal {
	case ModalCommandPalette:
		return a.commandPalette.View(a.width, a.height)
	case ModalSessionPicker:
		return a.sessionPicker.View(a.width, a.height)
	case ModalSessionRename:
		return a.sessionRename.View(a.width, a.height)
	case ModalConfirm:
		if a.confirmModal != nil {
			return a.confirmModal.View(a.width, a.height)
		}
	}
	return ""
}

func (a *App) renderHeader() string {
	// Check if header is disabled
	if !a.clientConfig.Rendering.ShowHeader {
		return ""
	}

	// Ensure we have a valid width
	width := a.width
	if width < 20 {
		width = 80 // fallback
	}

	// Session name - hide "default"
	sessionName := ""
	if a.currentSession != nil && a.currentSession.Name != "" && a.currentSession.Name != "default" {
		sessionName = a.currentSession.Name
	}

	// Session description (always show if present)
	desc := ""
	if a.currentSession != nil && a.currentSession.Description != "" {
		desc = a.currentSession.Description
	}

	// Build header content
	var content string
	if sessionName != "" && desc != "" {
		// Both name and description
		maxDescWidth := width - len(sessionName) - 5
		if maxDescWidth > 10 && len(desc) > maxDescWidth {
			desc = desc[:maxDescWidth-3] + "..."
		}
		content = sessionName + " │ " + desc
	} else if sessionName != "" {
		// Just session name
		content = sessionName
	} else if desc != "" {
		// Just description (for "default" session)
		if len(desc) > width-2 {
			desc = desc[:width-5] + "..."
		}
		content = desc
	} else {
		// Nothing to show
		content = "meept"
	}

	// Pad content to fill width
	if len(content) < width {
		content = content + strings.Repeat(" ", width-len(content))
	} else if len(content) > width {
		content = content[:width]
	}

	// Orange background, black text - render to exact width
	return a.styles.HeaderBar.
		Width(width).
		MaxWidth(width).
		Render(content)
}

// setTerminalTitle sets the terminal tab/window title using OSC escape sequence.
// getWindowTitle returns the terminal title string.
func (a *App) getWindowTitle() string {
	title := "meept"
	if a.currentSession != nil {
		if a.currentSession.Description != "" {
			title = "meept - " + a.currentSession.Description
		} else if a.currentSession.Name != "" && a.currentSession.Name != "default" {
			title = "meept - " + a.currentSession.Name
		}
	}
	return title
}

func (a *App) setTerminalTitle() {
	title := "meept"
	if a.currentSession != nil {
		// Prefer description over name for the title
		if a.currentSession.Description != "" {
			title = "meept - " + a.currentSession.Description
		} else if a.currentSession.Name != "" && a.currentSession.Name != "default" {
			title = "meept - " + a.currentSession.Name
		}
	}
	// OSC 0 sets window/tab title: \033]0;title\007
	fmt.Fprintf(os.Stdout, "\033]0;%s\007", title)
}

func (a *App) renderTabs() string {
	tabs := []struct {
		name string
		view ViewType
	}{
		{"Chat", ViewChat},
		{"Tasks", ViewTasks},
		{"Queue", ViewQueue},
		{"Memory", ViewMemory},
	}

	var renderedTabs []string
	for i, t := range tabs {
		style := a.styles.Tab
		if t.view == a.currentView {
			style = a.styles.ActiveTab
			// Clear flash when viewing the tab
			delete(a.tabFlash, t.view)
		}
		label := fmt.Sprintf("[%d] %s", i+1, t.name)
		// Add notification indicator if tab has flash
		if a.tabFlash[t.view] {
			label += " ●"
		}
		renderedTabs = append(renderedTabs, style.Render(label))
	}

	tabLine := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	// Add separator to fill width
	separatorWidth := max(0, a.width-lipgloss.Width(tabLine))
	separator := strings.Repeat("─", separatorWidth)

	return lipgloss.NewStyle().
		Width(a.width).
		Render(tabLine + a.styles.Muted.Render(separator))
}

func (a *App) renderStatusBar() string {
	// Connection status
	connectionStatus := "●"
	statusStyle := a.styles.StatusStopped
	if a.rpc.IsConnected() {
		statusStyle = a.styles.StatusRunning
	}

	// Project directory (shortened)
	projectDisplay := a.projectDir
	maxProjectLen := 25
	if len(projectDisplay) > maxProjectLen {
		projectDisplay = "..." + projectDisplay[len(projectDisplay)-maxProjectLen+3:]
	}

	// Build single line: status dot | mouse mode | context-sensitive keybindings | directory
	var parts []string

	// Status message takes priority if present
	if a.statusMessage != "" {
		parts = append(parts, a.styles.StatusRunning.Render(a.statusMessage))
	} else {
		parts = append(parts, statusStyle.Render(connectionStatus))
		// Add context-sensitive quick actions
		quickActions := a.getQuickActions()
		parts = append(parts, quickActions...)
		parts = append(parts, a.styles.Muted.Render(projectDisplay))
	}

	content := strings.Join(parts, " │ ")

	// Status bar spans only the main content area (excludes sidebar)
	statusWidth := a.width - a.sidebarWidth
	return a.styles.StatusBar.
		Width(statusWidth).
		MaxWidth(statusWidth).
		Render(content)
}

// getQuickActions returns context-sensitive keybinding hints based on current view and mode.
func (a *App) getQuickActions() []string {
	var actions []string

	// Always show menu, sessions, and quit
	actions = append(actions, a.styles.HelpKey.Render("^X")+" "+a.styles.HelpValue.Render("menu"))
	actions = append(actions, a.styles.HelpKey.Render("^S")+" "+a.styles.HelpValue.Render("sessions"))
	actions = append(actions, a.styles.HelpKey.Render("^C")+" "+a.styles.HelpValue.Render("quit"))

	switch a.currentView {
	case ViewChat:
		// Chat view actions depend on chat mode
		if a.chat != nil {
			chatMode := a.chat.GetMode()
			switch chatMode {
			case "insert":
				actions = append(actions, a.styles.HelpKey.Render("Esc")+" "+a.styles.HelpValue.Render("normal"))
				actions = append(actions, a.styles.HelpKey.Render("Enter")+" "+a.styles.HelpValue.Render("send"))
			case "visual":
				actions = append(actions, a.styles.HelpKey.Render("Esc")+" "+a.styles.HelpValue.Render("normal"))
				actions = append(actions, a.styles.HelpKey.Render("y")+" "+a.styles.HelpValue.Render("copy"))
			default: // normal mode
				actions = append(actions, a.styles.HelpKey.Render("i")+" "+a.styles.HelpValue.Render("insert"))
				actions = append(actions, a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("scroll"))
				actions = append(actions, a.styles.HelpKey.Render("/")+" "+a.styles.HelpValue.Render("search"))
			}
		} else {
			actions = append(actions, a.styles.HelpKey.Render("Esc")+" "+a.styles.HelpValue.Render("input"))
		}

	case ViewTasks:
		actions = append(actions, a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("navigate"))
		actions = append(actions, a.styles.HelpKey.Render("Enter")+" "+a.styles.HelpValue.Render("details"))
		actions = append(actions, a.styles.HelpKey.Render("r")+" "+a.styles.HelpValue.Render("refresh"))
		actions = append(actions, a.styles.HelpKey.Render("Tab")+" "+a.styles.HelpValue.Render("toggle view"))

	case ViewQueue:
		actions = append(actions, a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("navigate"))
		actions = append(actions, a.styles.HelpKey.Render("r")+" "+a.styles.HelpValue.Render("refresh"))
		actions = append(actions, a.styles.HelpKey.Render("Tab")+" "+a.styles.HelpValue.Render("toggle view"))

	case ViewMemory:
		actions = append(actions, a.styles.HelpKey.Render("j/k")+" "+a.styles.HelpValue.Render("navigate"))
		actions = append(actions, a.styles.HelpKey.Render("/")+" "+a.styles.HelpValue.Render("search"))
		actions = append(actions, a.styles.HelpKey.Render("r")+" "+a.styles.HelpValue.Render("refresh"))
	}

	// Add sidebar toggle hint if sidebar is hidden
	if !a.sidebar.IsVisible() {
		actions = append(actions, a.styles.HelpKey.Render("^X y")+" "+a.styles.HelpValue.Render("sidebar"))
	}

	return actions
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

// isPrintableKey returns true if the key message represents a printable character
// that should trigger auto-focus to the text input.
func isPrintableKey(msg tea.KeyPressMsg) bool {
	return len(msg.Text) > 0
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

// stopCurrentWork stops the current session's work and prompts for child tasks.
func (a *App) stopCurrentWork() tea.Cmd {
	if a.currentSession == nil {
		a.statusMessage = "no active session"
		a.statusMessageTime = time.Now()
		return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusMessageClearMsg{}
		})
	}

	// Check if there are child tasks
	tasks, _ := a.rpc.GetSessionChildTasks(a.currentSession.ID)

	if len(tasks) > 0 {
		// Show confirm modal to ask about child tasks
		if a.confirmModal == nil {
			a.confirmModal = NewConfirmModal(a.styles)
		}
		a.activeModal = ModalConfirm
		sessionID := a.currentSession.ID
		a.confirmModal.Show(
			"stop work",
			fmt.Sprintf("Stop current work? There are %d active tasks.", len(tasks)),
			func() tea.Cmd {
				return a.doStopSession(sessionID)
			},
			func() tea.Cmd {
				a.activeModal = ModalNone
				return nil
			},
		)
		return nil
	}

	// No child tasks, stop immediately
	return a.doStopSession(a.currentSession.ID)
}

// doStopSession performs the actual session stop RPC call.
func (a *App) doStopSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := a.rpc.StopSession(sessionID)
		if err != nil {
			return StopSessionResultMsg{Error: err}
		}
		return StopSessionResultMsg{Response: resp}
	}
}

// StopSessionResultMsg carries the result of stopping a session.
type StopSessionResultMsg struct {
	Response *types.StopSessionResponse
	Error    error
}
