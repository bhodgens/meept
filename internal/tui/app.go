package tui

import (
	"fmt"
	"strings"

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

	// Key bindings
	keys KeyMap

	// Error state
	err error
}

// KeyMap defines the key bindings.
type KeyMap struct {
	Quit       key.Binding
	Tab        key.Binding
	Enter      key.Binding
	Escape     key.Binding
	Help       key.Binding
	ChatView   key.Binding
	StatusView key.Binding
	TasksView  key.Binding
	MemoryView key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "q"),
			key.WithHelp("ctrl+c/q", "quit"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch view"),
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
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		ChatView: key.NewBinding(
			key.WithKeys("1", "c"),
			key.WithHelp("1/c", "chat"),
		),
		StatusView: key.NewBinding(
			key.WithKeys("2", "s"),
			key.WithHelp("2/s", "status"),
		),
		TasksView: key.NewBinding(
			key.WithKeys("3", "t"),
			key.WithHelp("3/t", "tasks"),
		),
		MemoryView: key.NewBinding(
			key.WithKeys("4", "m"),
			key.WithHelp("4/m", "memory"),
		),
	}
}

// NewApp creates a new TUI application.
func NewApp(socketPath string) *App {
	rpc := NewRPCClient(socketPath)
	styles := DefaultStyles()

	return &App{
		styles:      styles,
		rpc:         rpc,
		currentView: ViewChat,
		chat:        models.NewChatModel(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage),
		status:      models.NewStatusModel(rpc),
		tasks:       models.NewTasksModel(rpc),
		memory:      models.NewMemoryModel(rpc),
		keys:        DefaultKeyMap(),
	}
}

// Init initializes the application.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.connectDaemon,
		tea.EnterAltScreen,
		tea.SetWindowTitle("Meept"),
	)
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
		// Update sub-models with new size
		a.chat.SetSize(msg.Width, msg.Height-4) // Account for tabs and status bar
		a.status.SetSize(msg.Width, msg.Height-4)
		a.tasks.SetSize(msg.Width, msg.Height-4)
		a.memory.SetSize(msg.Width, msg.Height-4)
		return a, nil

	case tea.KeyMsg:
		// Global key bindings
		switch {
		case key.Matches(msg, a.keys.Quit):
			a.rpc.Close()
			return a, tea.Quit

		case key.Matches(msg, a.keys.Tab):
			a.currentView = (a.currentView + 1) % 4
			return a, a.initCurrentView()

		case key.Matches(msg, a.keys.ChatView):
			a.currentView = ViewChat
			return a, a.initCurrentView()

		case key.Matches(msg, a.keys.StatusView):
			a.currentView = ViewStatus
			return a, a.initCurrentView()

		case key.Matches(msg, a.keys.TasksView):
			a.currentView = ViewTasks
			return a, a.initCurrentView()

		case key.Matches(msg, a.keys.MemoryView):
			a.currentView = ViewMemory
			return a, a.initCurrentView()
		}

	case ConnectSuccessMsg:
		a.err = nil
		// Initialize the current view
		return a, a.initCurrentView()

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

	// Render current view
	if a.err != nil {
		b.WriteString(a.renderError())
	} else {
		switch a.currentView {
		case ViewChat:
			b.WriteString(a.chat.View())
		case ViewStatus:
			b.WriteString(a.status.View())
		case ViewTasks:
			b.WriteString(a.tasks.View())
		case ViewMemory:
			b.WriteString(a.memory.View())
		}
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
		label := fmt.Sprintf("[%d] %s", i+1, t.name)
		renderedTabs = append(renderedTabs, style.Render(label))
	}

	tabLine := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	// Add separator
	separator := strings.Repeat("-", max(0, a.width-lipgloss.Width(tabLine)))

	return lipgloss.NewStyle().
		Width(a.width).
		Render(tabLine + a.styles.Muted.Render(separator))
}

func (a *App) renderStatusBar() string {
	left := a.styles.HelpKey.Render("tab") + a.styles.HelpValue.Render(" switch") + " | " +
		a.styles.HelpKey.Render("1-4") + a.styles.HelpValue.Render(" views") + " | " +
		a.styles.HelpKey.Render("q") + a.styles.HelpValue.Render(" quit")

	connectionStatus := "disconnected"
	statusStyle := a.styles.StatusStopped
	if a.rpc.IsConnected() {
		connectionStatus = "connected"
		statusStyle = a.styles.StatusRunning
	}
	right := statusStyle.Render(connectionStatus)

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
