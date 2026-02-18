package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/caimlas/meept/internal/tui/models"
)

// createTestApp creates an App with a mock RPC client for testing.
// Since RPCClient is a concrete type, we create a disconnected one for testing.
func createTestApp() *App {
	// Create a real RPC client pointing to a non-existent socket
	rpc := NewRPCClient("/tmp/meept-test-nonexistent.sock")
	styles := DefaultStyles()
	app := &App{
		styles:      styles,
		rpc:         rpc,
		currentView: ViewChat,
		keys:        DefaultKeyMap(),
		sidebar:     NewSidebarModel(rpc, styles),
		width:       80,
		height:      24,
		projectDir:  "/test/project",
	}
	// Initialize sub-models
	app.chat = models.NewChatModel(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage)
	app.status = models.NewStatusModel(rpc)
	app.tasks = models.NewTasksModel(rpc)
	app.memory = models.NewMemoryModel(rpc)
	// Set sizes on sub-models
	app.chat.SetSize(app.width-2, app.height-4)
	app.status.SetSize(app.width-2, app.height-4)
	app.tasks.SetSize(app.width-2, app.height-4)
	app.memory.SetSize(app.width-2, app.height-4)
	return app
}

func TestApp_Init(t *testing.T) {
	app := createTestApp()

	cmds := app.Init()
	if cmds == nil {
		t.Error("expected Init to return commands")
	}
}

func TestApp_ViewSwitching_CommandMode(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		expectedView ViewType
	}{
		{"switch to chat", "1", ViewChat},
		{"switch to status", "2", ViewStatus},
		{"switch to tasks", "3", ViewTasks},
		{"switch to memory", "4", ViewMemory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := createTestApp()

			// Enter command mode
			app.commandMode = true
			app.commandModeTime = time.Now()

			// Send the key
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			newModel, _ := app.Update(msg)
			newApp := newModel.(*App)

			if newApp.currentView != tt.expectedView {
				t.Errorf("expected view %d, got %d", tt.expectedView, newApp.currentView)
			}
			if newApp.commandMode {
				t.Error("expected command mode to be disabled after key press")
			}
		})
	}
}

func TestApp_CommandMode_Timeout(t *testing.T) {
	app := createTestApp()
	app.commandMode = true
	app.commandModeTime = time.Now().Add(-3 * time.Second) // Simulate timeout passed

	// Send timeout message
	newModel, _ := app.Update(commandModeTimeoutMsg{})
	newApp := newModel.(*App)

	if newApp.commandMode {
		t.Error("expected command mode to be disabled after timeout")
	}
}

func TestApp_CommandMode_NoTimeout_RecentActivation(t *testing.T) {
	app := createTestApp()
	app.commandMode = true
	app.commandModeTime = time.Now() // Just activated

	// Send timeout message (should not timeout yet)
	newModel, _ := app.Update(commandModeTimeoutMsg{})
	newApp := newModel.(*App)

	if !newApp.commandMode {
		t.Error("expected command mode to still be active (timeout not reached)")
	}
}

func TestApp_Quit(t *testing.T) {
	app := createTestApp()

	// Send Ctrl+C
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := app.Update(msg)

	// Check that we get a quit command
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestApp_WindowResize(t *testing.T) {
	app := createTestApp()

	msg := tea.WindowSizeMsg{Width: 100, Height: 40}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.width != 100 {
		t.Errorf("expected width 100, got %d", newApp.width)
	}
	if newApp.height != 40 {
		t.Errorf("expected height 40, got %d", newApp.height)
	}
}

func TestApp_RenderTabs(t *testing.T) {
	app := createTestApp()
	app.currentView = ViewStatus

	tabs := app.renderTabs()

	// Check that all tabs are present
	expectedTabs := []string{"Chat", "Status", "Tasks", "Memory"}
	for _, tab := range expectedTabs {
		if !strings.Contains(tabs, tab) {
			t.Errorf("expected tabs to contain %q", tab)
		}
	}
}

func TestApp_RenderTabs_CommandMode(t *testing.T) {
	app := createTestApp()
	app.commandMode = true
	app.commandModeTime = time.Now()

	tabs := app.renderTabs()

	// Check that command mode indicator is present
	if !strings.Contains(tabs, "Ctrl+X") {
		t.Error("expected command mode indicator in tabs")
	}
}

func TestApp_RenderStatusBar(t *testing.T) {
	app := createTestApp()

	statusBar := app.renderStatusBar()

	// Check that help hints are present
	if !strings.Contains(statusBar, "Ctrl+X") {
		t.Error("expected Ctrl+X hint in status bar")
	}
	// Disconnected client should show disconnected
	if !strings.Contains(statusBar, "disconnected") {
		t.Error("expected disconnected status in status bar")
	}
}

func TestApp_RenderStatusBar_CommandMode(t *testing.T) {
	app := createTestApp()
	app.commandMode = true
	app.commandModeTime = time.Now()

	statusBar := app.renderStatusBar()

	// Check that command mode help is shown
	expectedHints := []string{"Chat", "Status", "Tasks", "Memory", "Sidebar", "Cancel"}
	for _, hint := range expectedHints {
		if !strings.Contains(statusBar, hint) {
			t.Errorf("expected %q hint in command mode status bar", hint)
		}
	}
}

func TestApp_RenderError(t *testing.T) {
	app := createTestApp()
	app.err = &testError{"connection failed"}

	errorView := app.renderError()

	if !strings.Contains(errorView, "Connection Error") {
		t.Error("expected 'Connection Error' in error view")
	}
	if !strings.Contains(errorView, "connection failed") {
		t.Error("expected error message in error view")
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestApp_ConnectMessages(t *testing.T) {
	app := createTestApp()
	app.err = &testError{"previous error"}

	// Test connect success clears error
	newModel, _ := app.Update(ConnectSuccessMsg{})
	newApp := newModel.(*App)
	if newApp.err != nil {
		t.Error("expected error to be cleared on connect success")
	}

	// Test connect error sets error
	connectErr := &testError{"new connection error"}
	newModel, _ = app.Update(ConnectErrorMsg{Err: connectErr})
	newApp = newModel.(*App)
	if newApp.err != connectErr {
		t.Error("expected error to be set on connect error")
	}
}

func TestApp_SidebarToggle(t *testing.T) {
	app := createTestApp()
	initialVisible := app.sidebar.IsVisible()

	// Enter command mode and toggle sidebar
	app.commandMode = true
	app.commandModeTime = time.Now()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}
	app.Update(msg)

	if app.sidebar.IsVisible() == initialVisible {
		t.Error("expected sidebar visibility to toggle")
	}
}

func TestApp_EscapeCommandMode(t *testing.T) {
	app := createTestApp()
	app.commandMode = true
	app.commandModeTime = time.Now()

	// Send escape
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.commandMode {
		t.Error("expected escape to exit command mode")
	}
}

func TestApp_UnknownCommandModeKey(t *testing.T) {
	app := createTestApp()
	app.commandMode = true
	app.commandModeTime = time.Now()

	// Send unknown key in command mode
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.commandMode {
		t.Error("expected unknown key to exit command mode")
	}
	// View should remain unchanged
	if newApp.currentView != ViewChat {
		t.Error("expected view to remain unchanged")
	}
}

// TestApp_WithTeatest demonstrates using teatest for integration testing.
// This test verifies the full app lifecycle including initialization and basic rendering.
func TestApp_WithTeatest_BasicRender(t *testing.T) {
	// Skip if running in short mode (CI without terminal)
	if testing.Short() {
		t.Skip("skipping teatest in short mode")
	}

	app := createTestApp()

	// Create test model with initial terminal size
	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Wait for initial render - check for any UI element (including error state)
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		// Check for basic UI elements (tabs, error, or loading)
		return strings.Contains(s, "[1]") || // Tab indicator
			strings.Contains(s, "Error") ||
			strings.Contains(s, "Loading") ||
			strings.Contains(s, "Ctrl+X")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestApp_WithTeatest_ViewSwitch demonstrates switching views with teatest.
func TestApp_WithTeatest_ViewSwitch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping teatest in short mode")
	}

	app := createTestApp()

	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Enter command mode
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlX})

	// Small delay for command mode to activate
	time.Sleep(50 * time.Millisecond)

	// Switch to status view
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})

	// Wait for status view indicators
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return strings.Contains(s, "Status") || strings.Contains(s, "Loading")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
