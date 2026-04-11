// Package lite provides a minimal, shell-like TUI for meept.
// Unlike the full meept TUI with its sidebar, tabs, and modals, meept-lite
// offers a simple 2-line prompt interface with a scrollback viewport.
package lite

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tui"
	"github.com/caimlas/meept/internal/tui/handlers"
	"github.com/caimlas/meept/internal/tui/types"
)

// FocusTarget indicates which component has keyboard focus.
type FocusTarget int

const (
	FocusViewport FocusTarget = iota
	FocusInput
)

// InputMode indicates the current input mode for special prompts.
type InputMode int

const (
	InputModeNormal InputMode = iota
	InputModeSessionName
	InputModeMemorySearch
	InputModeTaskName
)

// App is the main BubbleTea model for meept-lite.
// It provides a minimal shell-like interface with:
// - Messages printed to terminal scrollback (native selection)
// - A 2-line prompt (status line + input line)
// - A dashboard showing background activity
// - A Ctrl+X menu overlay for commands
type App struct {
	width  int
	height int
	rpc    *tui.RPCClient

	// Components
	viewport  *Viewport        // Still used for history storage, not rendering
	printer   *MessagePrinter  // Prints messages to terminal scrollback
	prompt    *Prompt
	menu      *Menu
	dashboard *Dashboard

	// Event streaming for real-time task notifications
	eventStream      *tui.EventStream
	eventRPC         *tui.RPCClient           // Separate RPC client for events to avoid blocking
	taskEventHandler *handlers.TaskEventHandler // Shared handler with rate limiting

	// State
	session   *types.Session
	showMenu  bool
	focus     FocusTarget
	inputMode InputMode

	// Cached data
	sessions []types.Session

	// Configuration
	socketPath string

	// Double-press tracking for Ctrl+C
	lastCtrlC      time.Time
	doublePressTTL time.Duration

	// Error state
	err error
}

// AppKeys provides app-specific key bindings.
// The comprehensive KeyMap is defined in keys.go.
var appKeys = struct {
	quit       string
	menuToggle string
	focusUp    string
	focusDown  string
}{
	quit:       "ctrl+c",
	menuToggle: "ctrl+x",
	focusUp:    "shift+tab",
	focusDown:  "tab",
}

// NewApp creates a new meept-lite application.
// socketPath should be the path to the meept daemon socket.
func NewApp(socketPath string) *App {
	// Expand tilde in socket path
	if strings.HasPrefix(socketPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			socketPath = home + socketPath[1:]
		}
	}

	rpc := tui.NewRPCClient(socketPath)
	// Separate RPC client for event stream to avoid blocking on main client
	eventRPC := tui.NewRPCClient(socketPath)

	// Configure event stream for task notifications
	eventStreamCfg := &tui.EventStreamConfig{
		Topics: []string{
			"task.completed",
			"task.failed",
			"task.progress",
			"task.step_completed",
			"agent.execution_complete",
		},
		BufferSize:   20,
		PollInterval: 500 * time.Millisecond,
	}

	app := &App{
		rpc:              rpc,
		eventRPC:         eventRPC,
		eventStream:      tui.NewEventStream(eventRPC, eventStreamCfg),
		taskEventHandler: handlers.NewTaskEventHandler(),
		viewport:         NewViewport(),   // For history storage only
		printer:          NewMessagePrinter(),
		prompt:           NewPrompt(),
		menu:             NewMenu(),
		dashboard:        NewDashboard(rpc),
		focus:            FocusInput, // Start with focus on input
		socketPath:       socketPath,
		doublePressTTL:   500 * time.Millisecond,
	}

	return app
}

// NewAppFromConfig creates a new app using the default configuration.
func NewAppFromConfig() (*App, error) {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return NewApp(cfg.Daemon.SocketPath), nil
}

// Init initializes the application.
func (a *App) Init() tea.Cmd {
	// Print startup banner to scrollback
	banner := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981")).
		Bold(true).
		Render("─── meept-lite ───")

	return tea.Batch(
		tea.Println(banner),
		a.connectDaemon,
		a.connectEventStream,
		a.dashboard.Init(),
		tea.SetWindowTitle("meept-lite"),
	)
}

// connectEventStream connects the event RPC and starts the event stream.
func (a *App) connectEventStream() tea.Msg {
	if err := a.eventRPC.Connect(); err != nil {
		// Non-fatal: event stream is optional
		return nil
	}
	return EventStreamStartMsg{}
}

// EventStreamStartMsg signals that event stream should start.
type EventStreamStartMsg struct{}

// connectDaemon attempts to connect to the meept daemon.
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

// SessionLoadedMsg indicates a session was loaded or created.
type SessionLoadedMsg struct {
	Session *types.Session
	IsNew   bool
	Err     error
}

// ChatResponseMsg carries a chat response from the daemon.
type ChatResponseMsg struct {
	Reply string
	Err   error
}

// StatusClearMsg clears the status message.
type StatusClearMsg struct{}

// loadSession attempts to load or create a session.
func (a *App) loadSession() tea.Msg {
	if !a.rpc.IsConnected() {
		return SessionLoadedMsg{Err: fmt.Errorf("not connected")}
	}

	// Try to get the most recent session
	session, err := a.rpc.GetMostRecentSession()
	if err == nil && session != nil {
		return SessionLoadedMsg{Session: session, IsNew: false}
	}

	// Create a new session
	session, err = a.rpc.CreateSession("default")
	if err != nil {
		return SessionLoadedMsg{Err: err}
	}

	return SessionLoadedMsg{Session: session, IsNew: true}
}

// printMsg stores a message in history and returns a command to print it to scrollback.
func (a *App) printMsg(role, content string) tea.Cmd {
	a.viewport.AddMessage(role, content) // Store for history
	return a.printer.PrintMessage(role, content)
}

// Update handles messages and updates the model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

		// Update component widths (viewport not rendered, but keep for history)
		a.viewport.SetSize(a.width, a.height)
		a.printer.SetWidth(a.width)
		a.prompt.SetSize(a.width)
		a.menu.SetSize(a.width, a.height)
		a.dashboard.SetSize(a.width)

		return a, nil

	case tea.KeyMsg:
		// Handle Ctrl+C - double-press to exit
		if msg.String() == "ctrl+c" {
			now := time.Now()
			if now.Sub(a.lastCtrlC) < a.doublePressTTL {
				a.rpc.Close()
				return a, tea.Quit
			}
			a.lastCtrlC = now

			// Show hint
			return a, tea.Batch(
				a.printMsg("system", "press ctrl+c again to exit"),
				tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
					return StatusClearMsg{}
				}),
			)
		}

		// Handle menu toggle
		if msg.String() == "ctrl+x" {
			a.showMenu = !a.showMenu
			if a.showMenu {
				a.menu.Show()
			} else {
				a.menu.Hide()
			}
			return a, nil
		}

		// Handle slash command at start of input
		if msg.String() == "/" && a.focus == FocusInput && a.prompt.Value() == "" {
			a.showMenu = true
			a.menu.Show()
			return a, nil
		}

		// If menu is visible, delegate to menu
		if a.showMenu {
			action, _ := a.menu.Update(msg)
			if action != "" {
				a.showMenu = false
				a.menu.Hide()
				return a.handleMenuAction(action)
			}
			if !a.menu.IsVisible() {
				a.showMenu = false
			}
			return a, nil
		}

		// Handle focus switching
		if msg.String() == "tab" {
			if a.focus == FocusViewport {
				a.focus = FocusInput
				a.prompt.Focus()
			}
			return a, nil
		}
		if msg.String() == "shift+tab" || msg.String() == "esc" {
			if a.focus == FocusInput {
				a.focus = FocusViewport
				a.prompt.Blur()
			}
			return a, nil
		}

		// Delegate to focused component
		if a.focus == FocusViewport {
			cmd := a.viewport.Update(msg)
			return a, cmd
		}

		// Handle special input modes
		if a.inputMode != InputModeNormal {
			if msg.String() == "esc" {
				a.inputMode = InputModeNormal
				a.prompt.Reset()
				return a, a.printMsg("system", "cancelled")
			}
			if msg.String() == "enter" {
				input := strings.TrimSpace(a.prompt.Value())
				a.prompt.Reset()
				switch a.inputMode {
				case InputModeSessionName:
					if input != "" && a.session != nil {
						return a, a.renameSession(input)
					}
				case InputModeMemorySearch:
					if input != "" {
						return a, a.searchMemory(input)
					}
				case InputModeTaskName:
					if input != "" {
						return a, a.createTask(input)
					}
				}
				a.inputMode = InputModeNormal
				return a, nil
			}
			// Delegate to prompt for input editing
			cmd, _ := a.prompt.Update(msg)
			return a, cmd
		}

		// Input focus - handle Enter to send message
		if msg.String() == "enter" {
			input := strings.TrimSpace(a.prompt.Value())
			if input != "" {
				// Add to history before resetting
				a.prompt.Input().AddHistory(input)
				a.prompt.Reset()

				// Print user message and send to daemon
				return a, tea.Batch(
					a.printMsg("user", input),
					a.sendChat(input),
				)
			}
			return a, nil
		}

		// Delegate to prompt
		cmd, _ := a.prompt.Update(msg)
		return a, cmd

	case ConnectSuccessMsg:
		a.err = nil
		cmds = append(cmds, a.printMsg("system", "connected to daemon"))
		cmds = append(cmds, a.loadSession)
		cmds = append(cmds, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusClearMsg{}
		}))
		return a, tea.Batch(cmds...)

	case ConnectErrorMsg:
		a.err = msg.Err
		return a, a.printMsg("system", fmt.Sprintf("error: %v", msg.Err))

	case SessionLoadedMsg:
		if msg.Err != nil {
			cmds = append(cmds, a.printMsg("system", fmt.Sprintf("session error: %v", msg.Err)))
		} else if msg.Session != nil {
			// Check if switching to a different session
			switching := a.session != nil && a.session.ID != msg.Session.ID
			a.session = msg.Session
			// Set session ID for per-session history
			a.prompt.Input().SetSessionID(msg.Session.ID)

			if switching {
				// Clear viewport history when switching sessions
				a.viewport.ClearMessages()
				cmds = append(cmds, a.printMsg("system", fmt.Sprintf("switched to session: %s", msg.Session.Name)))
			} else if msg.IsNew {
				cmds = append(cmds, a.printMsg("system", fmt.Sprintf("created session: %s", msg.Session.Name)))
			} else {
				cmds = append(cmds, a.printMsg("system", fmt.Sprintf("resumed session: %s", msg.Session.Name)))
			}
			// Load conversation history
			cmds = append(cmds, a.loadHistory)
		}
		cmds = append(cmds, tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return StatusClearMsg{}
		}))
		return a, tea.Batch(cmds...)

	case ChatResponseMsg:
		a.prompt.StopAgent()
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("error: %v", msg.Err))
		}
		return a, a.printMsg("assistant", msg.Reply)

	case HistoryLoadedMsg:
		if msg.Err == nil {
			for _, m := range msg.Messages {
				// Store in viewport for history, print to scrollback
				cmds = append(cmds, a.printMsg(m.Role, m.Content))
				// Add user messages to input history for recall with up arrow
				if m.Role == "user" {
					a.prompt.Input().AddHistory(m.Content)
				}
			}
		}
		return a, tea.Batch(cmds...)

	case StatusClearMsg:
		// Status messages are now in scrollback, nothing to clear
		return a, nil

	case MenuActionMsg:
		return a.handleMenuAction(msg.Action)

	case DashboardRefreshTick, DashboardDataMsg:
		// Forward to dashboard
		cmd := a.dashboard.Update(msg)
		return a, cmd

	case TickMsg:
		// Forward tick to prompt for agent status animation
		cmd, _ := a.prompt.Update(msg)
		return a, cmd

	case ProgressUpdateMsg:
		// Forward progress updates to prompt for agent status display
		cmd, _ := a.prompt.Update(msg)
		return a, cmd

	case SessionListMsg:
		if msg.Err != nil {
			a.menu.ClearDynamicContent()
			a.showMenu = false
			a.menu.Hide()
			return a, a.printMsg("system", fmt.Sprintf("error: %v", msg.Err))
		}
		items := make([]DynamicItem, len(msg.Sessions))
		for i, s := range msg.Sessions {
			marker := ""
			if a.session != nil && s.ID == a.session.ID {
				marker = "* "
			}
			name := s.Name
			if s.Description != "" {
				name = s.Description
			}
			items[i] = DynamicItem{
				Key:    fmt.Sprintf("%d", i+1),
				Label:  marker + name,
				Action: "session:switch:" + s.ID,
				Data:   s,
			}
		}
		a.menu.SetDynamicContent("sessions", items, "press 1-9 to switch, esc to go back")
		a.sessions = msg.Sessions
		return a, nil

	case WorkersListMsg:
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("failed to list workers: %v", msg.Err))
		}
		return a, a.displayWorkerStatus(msg)

	case TasksListMsg:
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("failed to list tasks: %v", msg.Err))
		}
		return a, a.displayTaskList(msg)

	case MemoryQueryMsg:
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("memory query failed: %v", msg.Err))
		}
		return a, a.displayMemoryResults(msg)

	case MemoryRecentMsg:
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("failed to get recent memories: %v", msg.Err))
		}
		return a, a.displayRecentMemories(msg)

	case SessionRenamedMsg:
		a.inputMode = InputModeNormal
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("failed to rename session: %v", msg.Err))
		}
		return a, a.printMsg("system", fmt.Sprintf("session renamed to: %s", msg.Name))

	case SessionDeletedMsg:
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("failed to delete session: %v", msg.Err))
		}
		cmds = append(cmds, a.printMsg("system", "session deleted"))
		cmds = append(cmds, a.loadSession)
		return a, tea.Batch(cmds...)

	case SessionStoppedMsg:
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("failed to stop session: %v", msg.Err))
		}
		return a, a.printMsg("system", fmt.Sprintf("stopped %d workers", msg.WorkersStopped))

	case TaskCreatedMsg:
		a.inputMode = InputModeNormal
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("failed to create task: %v", msg.Err))
		}
		return a, a.printMsg("system", fmt.Sprintf("created task: %s", msg.Task.Name))

	case ModelStatusMsg:
		if msg.Err != nil {
			return a, a.printMsg("system", fmt.Sprintf("failed to get status: %v", msg.Err))
		}
		return a, a.displayModelStatus(msg)

	case EventStreamStartMsg:
		// Start the event stream polling
		if a.eventStream != nil {
			return a, a.eventStream.Start()
		}
		return a, nil

	case tui.EventStreamTickMsg:
		// Forward to event stream
		if a.eventStream != nil {
			return a, a.eventStream.Update(msg)
		}
		return a, nil

	case tui.EventStreamDataMsg:
		// Process incoming events
		if a.eventStream != nil {
			a.eventStream.Update(msg)
		}
		return a, a.handleBusEvents(msg.Events)
	}

	return a, nil
}

// handleBusEvents processes bus events and displays notifications.
func (a *App) handleBusEvents(events []tui.BusEvent) tea.Cmd {
	var cmds []tea.Cmd
	for _, e := range events {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			continue
		}

		var notification *handlers.TaskNotification
		switch e.Topic {
		case "task.completed":
			notification = a.taskEventHandler.HandleTaskCompleted(payload)
			// Clear rate limiting state for completed task
			if taskID, ok := payload["task_id"].(string); ok {
				a.taskEventHandler.ClearTaskProgress(taskID)
			}
		case "task.failed":
			notification = a.taskEventHandler.HandleTaskFailed(payload)
			// Clear rate limiting state for failed task
			if taskID, ok := payload["task_id"].(string); ok {
				a.taskEventHandler.ClearTaskProgress(taskID)
			}
		case "task.progress":
			notification = a.taskEventHandler.HandleTaskProgress(payload)
		case "task.step_completed":
			// Step completions are debounced by progress events, don't show separately
			continue
		}

		if notification != nil {
			cmds = append(cmds, a.printMsg("notification", notification.Message))
		}
	}
	return tea.Batch(cmds...)
}

// HistoryLoadedMsg carries conversation history.
type HistoryLoadedMsg struct {
	Messages []types.SessionMessage
	Err      error
}

// MenuActionMsg carries a menu action.
type MenuActionMsg struct {
	Action string
}

// SessionListMsg carries session list results.
type SessionListMsg struct {
	Sessions []types.Session
	Err      error
}

// WorkersListMsg carries worker list results.
type WorkersListMsg struct {
	Workers []types.Worker
	Count   int
	Err     error
}

// TasksListMsg carries task list results.
type TasksListMsg struct {
	Tasks []types.TaskExtended
	Err   error
}

// MemoryQueryMsg carries memory query results.
type MemoryQueryMsg struct {
	Items []types.MemoryItem
	Query string
	Err   error
}

// MemoryRecentMsg carries recent memory results.
type MemoryRecentMsg struct {
	Items []types.MemoryItem
	Err   error
}

// SessionRenamedMsg indicates session rename result.
type SessionRenamedMsg struct {
	Name string
	Err  error
}

// SessionDeletedMsg indicates session delete result.
type SessionDeletedMsg struct {
	Err error
}

// SessionStoppedMsg indicates session stop result.
type SessionStoppedMsg struct {
	WorkersStopped int
	Err            error
}

// TaskCreatedMsg indicates task creation result.
type TaskCreatedMsg struct {
	Task *types.Task
	Err  error
}

// ModelStatusMsg carries model status info.
type ModelStatusMsg struct {
	Model         string
	DefaultModel  string
	TokensUsed    int
	BudgetUsed    float64
	UptimeSeconds float64
	Err           error
}

// sendChat sends a chat message to the daemon.
func (a *App) sendChat(message string) tea.Cmd {
	// Start thinking animation
	thinkCmd := a.prompt.StartThinking()

	return tea.Batch(thinkCmd, func() tea.Msg {
		conversationID := ""
		if a.session != nil {
			conversationID = a.session.ConversationID
		}

		reply, err := a.rpc.Chat(message, conversationID)
		if err != nil {
			return ChatResponseMsg{Err: err}
		}
		return ChatResponseMsg{Reply: reply}
	})
}

// loadHistory loads conversation history for the current session.
func (a *App) loadHistory() tea.Msg {
	if a.session == nil {
		return HistoryLoadedMsg{Err: fmt.Errorf("no session")}
	}

	resp, err := a.rpc.GetSessionMessages(a.session.ID, 0, 100)
	if err != nil {
		return HistoryLoadedMsg{Err: err}
	}

	return HistoryLoadedMsg{Messages: resp.Messages}
}

// handleMenuAction handles a menu action selection.
func (a *App) handleMenuAction(action string) (tea.Model, tea.Cmd) {
	// Handle dynamic session switch
	if strings.HasPrefix(action, "session:switch:") {
		sessionID := strings.TrimPrefix(action, "session:switch:")
		a.menu.Hide()
		a.showMenu = false
		return a, a.switchToSession(sessionID)
	}

	// Handle dynamic task select
	if strings.HasPrefix(action, "task:select:") {
		// Could show task details or actions in the future
		a.menu.Hide()
		a.showMenu = false
		return a, nil
	}

	switch action {
	// Session actions
	case ActionSessionNew:
		return a, a.createNewSession

	case ActionSessionList:
		// Keep menu open, show loading state
		a.menu.SetLoading("sessions")
		return a, a.fetchSessionList

	case ActionSessionRename:
		a.showMenu = false
		a.menu.Hide()
		a.inputMode = InputModeSessionName
		return a, a.printMsg("system", "enter new session name (enter to confirm, esc to cancel):")

	case ActionSessionDelete:
		if a.session != nil {
			return a, a.deleteCurrentSession
		}
		return a, a.printMsg("system", "no active session to delete")

	// Agent actions
	case ActionAgentStatus:
		// Keep menu open, show loading state
		a.menu.SetLoading("workers")
		return a, a.fetchWorkerStatus

	case ActionAgentStop:
		if a.session != nil {
			return a, a.stopCurrentSession
		}
		return a, a.printMsg("system", "no active session to stop")

	case ActionAgentModel:
		// Keep menu open, show loading state
		a.menu.SetLoading("agent status")
		return a, a.fetchModelStatus

	// Task actions
	case ActionTaskList:
		// Keep menu open, show loading state
		a.menu.SetLoading("tasks")
		return a, a.fetchTaskList

	case ActionTaskCreate:
		a.showMenu = false
		a.menu.Hide()
		a.inputMode = InputModeTaskName
		return a, a.printMsg("system", "enter task name (enter to confirm, esc to cancel):")

	case ActionTaskCancel:
		// Show task list for selection
		a.menu.SetLoading("tasks")
		return a, a.fetchTaskList

	// Memory actions
	case ActionMemorySearch:
		a.showMenu = false
		a.menu.Hide()
		a.inputMode = InputModeMemorySearch
		return a, a.printMsg("system", "enter search query (enter to search, esc to cancel):")

	case ActionMemoryRecent:
		// Keep menu open, show loading state
		a.menu.SetLoading("memories")
		return a, a.fetchRecentMemories

	case ActionMemoryClear:
		a.viewport.ClearMessages()
		return a, a.printMsg("system", "scrollback cleared (terminal history preserved)")

	// Config actions
	case ActionConfigEdit:
		return a, a.openConfigEditor()

	case ActionConfigReload:
		return a, a.printMsg("system", "config reload requested (daemon will apply on next operation)")

	// View actions
	case ActionViewChat:
		return a, a.printMsg("system", "chat view is the default view")

	case ActionViewTasks:
		// Keep menu open, show loading state
		a.menu.SetLoading("tasks")
		return a, a.fetchTaskList

	case ActionViewQueue:
		// Keep menu open, show loading state
		a.menu.SetLoading("tasks")
		return a, a.fetchTaskList

	case ActionViewMemory:
		// Keep menu open, show loading state
		a.menu.SetLoading("memories")
		return a, a.fetchRecentMemories

	// Help actions
	case ActionHelpKeybindings:
		return a, a.printMsg("system", a.helpText())

	case ActionHelpCommands:
		return a, a.printMsg("system", a.commandsText())

	case "quit":
		a.rpc.Close()
		return a, tea.Quit
	}

	// Handle other actions with a fallback message
	if action != "" {
		return a, a.printMsg("system", fmt.Sprintf("action %q not yet implemented", action))
	}
	return a, nil
}

// createNewSession creates a new session.
func (a *App) createNewSession() tea.Msg {
	session, err := a.rpc.CreateSession("default")
	if err != nil {
		return SessionLoadedMsg{Err: err}
	}
	return SessionLoadedMsg{Session: session, IsNew: true}
}

// fetchSessionList fetches the list of sessions.
func (a *App) fetchSessionList() tea.Msg {
	resp, err := a.rpc.ListSessions()
	if err != nil {
		return SessionListMsg{Err: err}
	}
	return SessionListMsg{Sessions: resp.Sessions}
}

// switchToSession switches to a different session.
func (a *App) switchToSession(sessionID string) tea.Cmd {
	return func() tea.Msg {
		// Find the session in the cached list
		for _, s := range a.sessions {
			if s.ID == sessionID {
				return SessionLoadedMsg{Session: &s, IsNew: false}
			}
		}
		return SessionLoadedMsg{Err: fmt.Errorf("session not found")}
	}
}

// deleteCurrentSession deletes the current session.
func (a *App) deleteCurrentSession() tea.Msg {
	if a.session == nil {
		return SessionDeletedMsg{Err: fmt.Errorf("no active session")}
	}
	err := a.rpc.DeleteSession(a.session.ID)
	if err != nil {
		return SessionDeletedMsg{Err: err}
	}
	a.session = nil
	return SessionDeletedMsg{}
}

// renameSession renames the current session.
func (a *App) renameSession(name string) tea.Cmd {
	return func() tea.Msg {
		if a.session == nil {
			return SessionRenamedMsg{Err: fmt.Errorf("no active session")}
		}
		err := a.rpc.UpdateSessionDescription(a.session.ID, name)
		if err != nil {
			return SessionRenamedMsg{Err: err}
		}
		a.session.Description = name
		return SessionRenamedMsg{Name: name}
	}
}

// stopCurrentSession stops all work for the current session.
func (a *App) stopCurrentSession() tea.Msg {
	if a.session == nil {
		return SessionStoppedMsg{Err: fmt.Errorf("no active session")}
	}
	resp, err := a.rpc.StopSession(a.session.ID)
	if err != nil {
		return SessionStoppedMsg{Err: err}
	}
	return SessionStoppedMsg{WorkersStopped: len(resp.WorkersStopped)}
}

// fetchWorkerStatus fetches worker status.
func (a *App) fetchWorkerStatus() tea.Msg {
	resp, err := a.rpc.ListWorkers()
	if err != nil {
		return WorkersListMsg{Err: err}
	}
	return WorkersListMsg{Workers: resp.Workers, Count: resp.Count}
}

// fetchTaskList fetches the task list.
func (a *App) fetchTaskList() tea.Msg {
	resp, err := a.rpc.ListTasksExtended()
	if err != nil {
		return TasksListMsg{Err: err}
	}
	return TasksListMsg{Tasks: resp.Tasks}
}

// createTask creates a new task.
func (a *App) createTask(name string) tea.Cmd {
	return func() tea.Msg {
		task, err := a.rpc.CreateTask(name, "")
		if err != nil {
			return TaskCreatedMsg{Err: err}
		}
		return TaskCreatedMsg{Task: task}
	}
}

// searchMemory searches memory with a query.
func (a *App) searchMemory(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := a.rpc.QueryMemory(query, 10)
		if err != nil {
			return MemoryQueryMsg{Err: err, Query: query}
		}
		return MemoryQueryMsg{Items: resp.GetItems(), Query: query}
	}
}

// fetchRecentMemories fetches recent memories.
func (a *App) fetchRecentMemories() tea.Msg {
	resp, err := a.rpc.GetRecentMemories(10)
	if err != nil {
		return MemoryRecentMsg{Err: err}
	}
	return MemoryRecentMsg{Items: resp.GetItems()}
}

// fetchModelStatus fetches daemon status including model info.
func (a *App) fetchModelStatus() tea.Msg {
	resp, err := a.rpc.Status()
	if err != nil {
		return ModelStatusMsg{Err: err}
	}
	return ModelStatusMsg{
		Model:         resp.Model,
		DefaultModel:  resp.DefaultModel,
		TokensUsed:    resp.TokensUsed,
		BudgetUsed:    resp.BudgetUsed,
		UptimeSeconds: resp.UptimeSeconds,
	}
}

// openConfigEditor opens the config file in an editor.
func (a *App) openConfigEditor() tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	home, _ := os.UserHomeDir()
	configPath := home + "/.meept/meept.toml"

	c := exec.Command(editor, configPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return MenuActionMsg{Action: ""} // Signal completion
		}
		return MenuActionMsg{Action: ""} // Signal completion
	})
}

// displaySessionPicker shows the session picker (unused, sessions go through menu).
func (a *App) displaySessionPicker() tea.Cmd {
	var sb strings.Builder
	sb.WriteString("sessions (press 1-9 to switch, esc to cancel):\n")
	for i, s := range a.sessions {
		if i >= 9 {
			sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(a.sessions)-9))
			break
		}
		marker := " "
		if a.session != nil && s.ID == a.session.ID {
			marker = "*"
		}
		name := s.Name
		if s.Description != "" {
			name = s.Description
		}
		sb.WriteString(fmt.Sprintf("%s [%d] %s\n", marker, i+1, name))
	}
	return a.printMsg("system", sb.String())
}

// displayWorkerStatus shows worker status.
func (a *App) displayWorkerStatus(msg WorkersListMsg) tea.Cmd {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("agent workers (%d total):\n", msg.Count))
	if len(msg.Workers) == 0 {
		sb.WriteString("  no active workers\n")
	} else {
		for _, w := range msg.Workers {
			state := w.State
			tool := ""
			if w.CurrentTool != "" {
				tool = fmt.Sprintf(" [%s]", w.CurrentTool)
			}
			id := w.ID
			if len(id) > 8 {
				id = id[:8]
			}
			sb.WriteString(fmt.Sprintf("  %s: %s%s\n", id, state, tool))
		}
	}
	return a.printMsg("system", sb.String())
}

// displayTaskList shows tasks.
func (a *App) displayTaskList(msg TasksListMsg) tea.Cmd {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("tasks (%d total):\n", len(msg.Tasks)))
	if len(msg.Tasks) == 0 {
		sb.WriteString("  no tasks\n")
	} else {
		for _, t := range msg.Tasks {
			name := t.Name
			if name == "" {
				id := t.ID
				if len(id) > 8 {
					id = id[:8]
				}
				name = id
			}
			progress := ""
			if t.TotalJobs > 0 {
				progress = fmt.Sprintf(" [%d/%d]", t.CompletedJobs, t.TotalJobs)
			}
			sb.WriteString(fmt.Sprintf("  %s: %s%s\n", t.State, name, progress))
		}
	}
	return a.printMsg("system", sb.String())
}

// displayMemoryResults shows memory search results.
func (a *App) displayMemoryResults(msg MemoryQueryMsg) tea.Cmd {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("memory search \"%s\" (%d results):\n", msg.Query, len(msg.Items)))
	if len(msg.Items) == 0 {
		sb.WriteString("  no matches\n")
	} else {
		for _, m := range msg.Items {
			content := m.Content
			if len(content) > 60 {
				content = content[:57] + "..."
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", m.GetType(), content))
		}
	}
	return a.printMsg("system", sb.String())
}

// displayRecentMemories shows recent memories.
func (a *App) displayRecentMemories(msg MemoryRecentMsg) tea.Cmd {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("recent memories (%d):\n", len(msg.Items)))
	if len(msg.Items) == 0 {
		sb.WriteString("  no recent memories\n")
	} else {
		for _, m := range msg.Items {
			content := m.Content
			if len(content) > 60 {
				content = content[:57] + "..."
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", m.GetType(), content))
		}
	}
	return a.printMsg("system", sb.String())
}

// displayModelStatus shows model and daemon status.
func (a *App) displayModelStatus(msg ModelStatusMsg) tea.Cmd {
	var sb strings.Builder
	sb.WriteString("daemon status:\n")

	model := msg.Model
	if model == "" {
		model = msg.DefaultModel
	}
	if model == "" {
		model = "(not set)"
	}
	sb.WriteString(fmt.Sprintf("  model: %s\n", model))

	// Format uptime
	uptime := int(msg.UptimeSeconds)
	hours := uptime / 3600
	mins := (uptime % 3600) / 60
	secs := uptime % 60
	sb.WriteString(fmt.Sprintf("  uptime: %dh %dm %ds\n", hours, mins, secs))

	sb.WriteString(fmt.Sprintf("  tokens used: %d\n", msg.TokensUsed))
	sb.WriteString(fmt.Sprintf("  budget used: $%.4f\n", msg.BudgetUsed))

	if a.session != nil {
		sb.WriteString(fmt.Sprintf("  session: %s\n", a.session.Name))
	}

	return a.printMsg("system", sb.String())
}

// helpText returns the help text for the app.
func (a *App) helpText() string {
	return `meept-lite keyboard shortcuts:

ctrl+x          open command menu
/               open menu (when input is empty)
tab             switch focus to input
shift+tab/esc   switch focus to viewport (scrollback)
ctrl+c          double-press to quit
enter           send message
up/down         browse input history
j/k             scroll viewport (when focused)
g/G             scroll to top/bottom
page up/down    page scroll
`
}

// commandsText returns the slash commands help text.
func (a *App) commandsText() string {
	return `meept-lite commands (via ctrl+x menu):

sessions:
  l  list sessions    - show all sessions, switch with 1-9
  n  new session      - create a new session
  r  rename session   - rename current session
  d  delete session   - delete current session

agent:
  s  status           - show active agent workers
  x  stop             - stop current session's work
  m  model            - show current model info

tasks:
  l  list             - show all tasks
  c  create           - create a new task
  x  cancel           - cancel a task

memory:
  s  search           - search memories
  r  recent           - show recent memories
  c  clear            - clear viewport

config:
  e  edit             - open config in $EDITOR
  r  reload           - reload configuration
`
}

// View renders the application.
// In primitive scrollback mode, we only render the prompt and dashboard.
// Messages are printed above via tea.Println and stay in terminal scrollback.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "loading..."
	}

	// If menu is visible, render overlay
	if a.showMenu {
		return a.menu.View()
	}

	// Render error state if not connected
	if a.err != nil {
		return a.renderError()
	}

	var b strings.Builder

	// Only render prompt and dashboard - messages are in terminal scrollback
	b.WriteString(a.prompt.View())
	b.WriteString("\n")
	b.WriteString(a.dashboard.View())

	return b.String()
}

// renderError renders the error state.
func (a *App) renderError() string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EF4444")).
		Padding(1, 2)

	msg := fmt.Sprintf(`connection error: %v

Make sure the meept daemon is running:
  meept daemon start

Press ctrl+c twice to exit.`, a.err)

	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center,
		style.Render(msg))
}

// Run starts the BubbleTea program.
// Runs inline (no alt-screen) - messages go to terminal scrollback.
// This enables native text selection with mouse.
func (a *App) Run() error {
	p := tea.NewProgram(a)
	_, err := p.Run()
	return err
}
