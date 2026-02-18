package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/models"
)

// ViewType represents the different views in the TUI.
type ViewType int

const (
	ViewChat ViewType = iota
	ViewStatus
	ViewTasks
	ViewMemory
)

// commandModeTimeout is how long command mode stays active before auto-canceling.
const commandModeTimeout = 2 * time.Second

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
	memory *models.MemoryModel

	// Sidebar
	sidebar *SidebarModel

	// Focus management
	appFocus AppFocus

	// Key bindings
	keys KeyMap

	// Command mode state (Ctrl+X prefix)
	commandMode     bool      // true when Ctrl+X prefix is active
	commandModeTime time.Time // when command mode was activated

	// Project directory
	projectDir string

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

	// Get current working directory for display
	projectDir, _ := os.Getwd()

	return &App{
		styles:      styles,
		rpc:         rpc,
		currentView: ViewChat,
		chat:        models.NewChatModel(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage),
		status:      models.NewStatusModel(rpc),
		tasks:       models.NewTasksModel(rpc),
		memory:      models.NewMemoryModel(rpc),
		sidebar:     NewSidebarModel(rpc, styles),
		keys:        DefaultKeyMap(),
		projectDir:  projectDir,
	}
}

// Init initializes the application.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.connectDaemon,
		tea.EnterAltScreen,
		tea.SetWindowTitle("Meept"),
		tea.EnableMouseCellMotion,
	)
}

// commandModeTimeoutMsg is sent when command mode times out.
type commandModeTimeoutMsg struct{}

// tickCommandMode returns a command that sends a timeout message after the timeout duration.
func (a *App) tickCommandMode() tea.Cmd {
	return tea.Tick(commandModeTimeout, func(t time.Time) tea.Msg {
		return commandModeTimeoutMsg{}
	})
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
		a.memory.SetSize(mainWidth, msg.Height-4)
		return a, nil

	case commandModeTimeoutMsg:
		// Only timeout if still in command mode and enough time has passed
		if a.commandMode && time.Since(a.commandModeTime) >= commandModeTimeout {
			a.commandMode = false
		}
		return a, nil

	case tea.MouseMsg:
		// Handle mouse clicks on tab bar (first row)
		if msg.Type == tea.MouseLeft && msg.Y == 0 {
			// Calculate which tab was clicked based on X position
			// Tab layout: "[1] Chat  [2] Status  [3] Tasks  [4] Memory"
			// Approximate positions (accounting for padding):
			tabWidths := []int{10, 12, 11, 12} // "[1] Chat", "[2] Status", "[3] Tasks", "[4] Memory"
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

		// Check for Ctrl+X prefix to enter command mode
		if key.Matches(msg, a.keys.Command) {
			a.commandMode = true
			a.commandModeTime = time.Now()
			return a, a.tickCommandMode()
		}

		// In command mode, process UI shortcuts
		if a.commandMode {
			a.commandMode = false // Exit command mode after any key
			switch msg.String() {
			case "1":
				a.currentView = ViewChat
				return a, a.initCurrentView()
			case "2":
				a.currentView = ViewStatus
				return a, a.initCurrentView()
			case "3":
				a.currentView = ViewTasks
				return a, a.initCurrentView()
			case "4":
				a.currentView = ViewMemory
				return a, a.initCurrentView()
			case "s":
				// Toggle sidebar
				a.sidebar.Toggle()
				// Trigger resize to recalculate layout
				return a, func() tea.Msg {
					return tea.WindowSizeMsg{Width: a.width, Height: a.height}
				}
			case "esc":
				// Just exit command mode, already done above
				return a, nil
			default:
				// Unknown command, just exit command mode
				return a, nil
			}
		}

		// Not in command mode - delegate based on focus
		if a.appFocus == FocusSidebar && a.sidebar.IsVisible() {
			cmd := a.sidebar.Update(msg)
			return a, cmd
		}
		// Otherwise fall through to delegate to current view

	case ConnectSuccessMsg:
		a.err = nil
		// Initialize the current view and sidebar
		return a, tea.Batch(a.initCurrentView(), a.sidebar.Init())

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
	case ViewMemory:
		cmd = a.memory.Update(msg)
	}
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
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

func (a *App) renderTabs() string {
	tabs := []struct {
		name string
		view ViewType
	}{
		{"Chat", ViewChat},
		{"Status", ViewStatus},
		{"Tasks", ViewTasks},
		{"Memory", ViewMemory},
	}

	var renderedTabs []string
	for i, t := range tabs {
		style := a.styles.Tab
		if t.view == a.currentView {
			style = a.styles.ActiveTab
		}
		// In command mode, highlight all tabs to show they're selectable
		if a.commandMode {
			style = a.styles.CommandModeTab
		}
		label := fmt.Sprintf("[%d] %s", i+1, t.name)
		renderedTabs = append(renderedTabs, style.Render(label))
	}

	tabLine := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	// Add command mode indicator
	var indicator string
	if a.commandMode {
		indicator = a.styles.CommandModeIndicator.Render(" Ctrl+X ")
	}

	// Add separator
	separatorWidth := max(0, a.width-lipgloss.Width(tabLine)-lipgloss.Width(indicator))
	separator := strings.Repeat("-", separatorWidth)

	return lipgloss.NewStyle().
		Width(a.width).
		Render(tabLine + a.styles.Muted.Render(separator) + indicator)
}

func (a *App) renderStatusBar() string {
	var left string
	if a.commandMode {
		// Show available commands in command mode
		left = a.styles.CommandModeIndicator.Render(" Ctrl+X: ") +
			a.styles.HelpKey.Render("1") + a.styles.HelpValue.Render("-Chat ") +
			a.styles.HelpKey.Render("2") + a.styles.HelpValue.Render("-Status ") +
			a.styles.HelpKey.Render("3") + a.styles.HelpValue.Render("-Tasks ") +
			a.styles.HelpKey.Render("4") + a.styles.HelpValue.Render("-Memory ") +
			a.styles.HelpKey.Render("s") + a.styles.HelpValue.Render("-Sidebar ") +
			a.styles.HelpKey.Render("Esc") + a.styles.HelpValue.Render("-Cancel")
	} else {
		left = a.styles.HelpKey.Render("Ctrl+X") + a.styles.HelpValue.Render(" command") + " | " +
			a.styles.HelpKey.Render("Ctrl+C") + a.styles.HelpValue.Render(" quit")
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
