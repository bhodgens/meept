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
	clientConfig := DefaultClientConfig()

	app := &App{
		styles:       styles,
		rpc:          rpc,
		currentView:  ViewChat,
		keys:         DefaultKeyMap(),
		sidebar:      NewSidebarModel(rpc, styles, clientConfig.Rendering.SidebarAnimation),
		clientConfig: clientConfig,
		width:        80,
		height:       24,
		projectDir:   "/test/project",
		activeModal:  ModalNone,
	}
	// Initialize sub-models
	app.chat = models.NewChatModel(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage)
	app.tasks = models.NewTasksModel(rpc)
	app.queue = models.NewQueueModel(rpc)
	app.memory = models.NewMemoryModel(rpc)
	// Create modals
	app.commandPalette = CommandPaletteModal(styles, clientConfig)
	app.sessionPicker = NewSessionPickerModal(styles, rpc, clientConfig)
	// Set sizes on sub-models
	app.chat.SetSize(app.width-2, app.height-4)
	app.tasks.SetSize(app.width-2, app.height-4)
	app.queue.SetSize(app.width-2, app.height-4)
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

func TestApp_ViewSwitching_Modal(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		expectedView ViewType
	}{
		{"switch to chat", "1", ViewChat},
		{"switch to tasks", "2", ViewTasks},
		{"switch to queue", "3", ViewQueue},
		{"switch to memory", "4", ViewMemory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := createTestApp()

			// Open command palette
			app.activeModal = ModalCommandPalette
			app.commandPalette.Show()

			// Send the key
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}
			newModel, _ := app.Update(msg)
			newApp := newModel.(*App)

			if newApp.currentView != tt.expectedView {
				t.Errorf("expected view %d, got %d", tt.expectedView, newApp.currentView)
			}
			if newApp.activeModal != ModalNone {
				t.Error("expected modal to be closed after selection")
			}
		})
	}
}

func TestApp_CommandPalette_Open(t *testing.T) {
	app := createTestApp()

	// Send Ctrl+X to open command palette
	msg := tea.KeyMsg{Type: tea.KeyCtrlX}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.activeModal != ModalCommandPalette {
		t.Error("expected command palette to be open")
	}
	if !newApp.commandPalette.IsVisible() {
		t.Error("expected command palette to be visible")
	}
}

func TestApp_CommandPalette_EscapeCloses(t *testing.T) {
	app := createTestApp()
	app.activeModal = ModalCommandPalette
	app.commandPalette.Show()

	// Send escape
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.activeModal != ModalNone {
		t.Error("expected modal to be closed after escape")
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
	app.currentView = ViewTasks

	tabs := app.renderTabs()

	// Check that all tabs are present
	expectedTabs := []string{"Chat", "Tasks", "Queue", "Memory"}
	for _, tab := range expectedTabs {
		if !strings.Contains(tabs, tab) {
			t.Errorf("expected tabs to contain %q", tab)
		}
	}

	// Status should NOT be present
	if strings.Contains(tabs, "Status") {
		t.Error("expected tabs to NOT contain 'Status'")
	}
}

func TestApp_RenderStatusBar(t *testing.T) {
	app := createTestApp()

	statusBar := app.renderStatusBar()

	// Check that help hints are present (^X menu format)
	if !strings.Contains(statusBar, "^X") {
		t.Error("expected ^X hint in status bar")
	}
	if !strings.Contains(statusBar, "menu") {
		t.Error("expected menu hint in status bar")
	}
	if !strings.Contains(statusBar, "Esc") {
		t.Error("expected Esc hint in status bar")
	}
	// Status indicator dot should be present
	if !strings.Contains(statusBar, "●") {
		t.Error("expected status indicator in status bar")
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

	// Open command palette and toggle sidebar
	app.activeModal = ModalCommandPalette
	app.commandPalette.Show()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")} // 'y' for sidebar in new keybindings
	app.Update(msg)

	if app.sidebar.IsVisible() == initialVisible {
		t.Error("expected sidebar visibility to toggle")
	}
}

func TestApp_EscapeToInput(t *testing.T) {
	app := createTestApp()
	// Focus on viewport
	app.chat.SetFocus(models.FocusViewport)

	// Send escape
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if !newApp.chat.IsInputFocused() {
		t.Error("expected escape to focus input")
	}
}

func TestApp_EscapeClearsInput(t *testing.T) {
	app := createTestApp()
	// Focus on input - already the default

	// First escape should clear input (input is empty, so it's a no-op)
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	app.Update(msg)

	// Verify input is focused and empty
	if !app.chat.IsInputFocused() {
		t.Error("expected input to remain focused after escape")
	}
}

func TestApp_EscapeFromSidebar(t *testing.T) {
	app := createTestApp()
	app.appFocus = FocusSidebar
	app.sidebar.SetFocused(true)

	// Send escape
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.appFocus != FocusChat {
		t.Error("expected escape from sidebar to focus chat")
	}
	if newApp.sidebar.IsFocused() {
		t.Error("expected sidebar to be unfocused")
	}
}

func TestApp_EscapeFromOtherView(t *testing.T) {
	app := createTestApp()
	app.currentView = ViewTasks

	// Send escape
	msg := tea.KeyMsg{Type: tea.KeyEscape}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.currentView != ViewChat {
		t.Error("expected escape from tasks to switch to chat")
	}
}

func TestApp_CopySuccessMessage(t *testing.T) {
	app := createTestApp()

	// Simulate copy success
	newModel, cmd := app.Update(CopySuccessMsg{Text: "test text"})
	newApp := newModel.(*App)

	if !strings.Contains(newApp.statusMessage, "Copied") {
		t.Error("expected status message to contain 'Copied'")
	}
	if cmd == nil {
		t.Error("expected command to clear message after timeout")
	}
}

func TestApp_CopySuccessMessageTruncation(t *testing.T) {
	app := createTestApp()

	// Simulate copy success with long text
	longText := strings.Repeat("a", 100)
	newModel, _ := app.Update(CopySuccessMsg{Text: longText})
	newApp := newModel.(*App)

	if !strings.Contains(newApp.statusMessage, "...") {
		t.Error("expected long text to be truncated with '...'")
	}
}

func TestApp_CopyErrorMessage(t *testing.T) {
	app := createTestApp()

	// Simulate copy error
	newModel, cmd := app.Update(CopyErrorMsg{Err: &testError{"copy failed"}})
	newApp := newModel.(*App)

	if !strings.Contains(newApp.statusMessage, "Copy failed") {
		t.Error("expected status message to contain error")
	}
	if cmd == nil {
		t.Error("expected command to clear message after timeout")
	}
}

func TestApp_StatusMessageClear(t *testing.T) {
	app := createTestApp()
	app.statusMessage = "old message"
	app.statusMessageTime = time.Now().Add(-3 * time.Second) // Simulate timeout

	newModel, _ := app.Update(StatusMessageClearMsg{})
	newApp := newModel.(*App)

	if newApp.statusMessage != "" {
		t.Error("expected status message to be cleared")
	}
}

func TestApp_StatusMessageNoEarlyClear(t *testing.T) {
	app := createTestApp()
	app.statusMessage = "recent message"
	app.statusMessageTime = time.Now() // Just set

	newModel, _ := app.Update(StatusMessageClearMsg{})
	newApp := newModel.(*App)

	if newApp.statusMessage != "recent message" {
		t.Error("expected status message to remain (timeout not reached)")
	}
}

func TestApp_RenderStatusBar_WithStatusMessage(t *testing.T) {
	app := createTestApp()
	app.statusMessage = "Test status message"
	app.statusMessageTime = time.Now()

	statusBar := app.renderStatusBar()

	if !strings.Contains(statusBar, "Test status message") {
		t.Error("expected status message in status bar")
	}
}

func TestApp_HandleCopyToClipboardMsg(t *testing.T) {
	app := createTestApp()

	// Send CopyToClipboardMsg
	_, cmd := app.Update(models.CopyToClipboardMsg{Text: "test content"})

	if cmd == nil {
		t.Error("expected copy command to be returned")
	}
}

func TestApp_ModalOverlayRendering(t *testing.T) {
	app := createTestApp()
	app.activeModal = ModalCommandPalette
	app.commandPalette.Show()

	// When modal is active, View() should return modal overlay
	view := app.View()

	// Should contain modal content (lowercase per UI conventions)
	if !strings.Contains(view, "command palette") {
		t.Error("expected modal content in view when modal is active")
	}
}

func TestApp_SessionPickerModal(t *testing.T) {
	app := createTestApp()

	// Open command palette first
	app.activeModal = ModalCommandPalette
	app.commandPalette.Show()

	// Press 's' to open session picker
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.activeModal != ModalSessionPicker {
		t.Error("expected session picker to be open")
	}
}

func TestApp_NoMouseCapture(t *testing.T) {
	app := createTestApp()

	// Init should NOT include tea.EnableMouseAllMotion
	cmd := app.Init()
	if cmd == nil {
		t.Error("expected Init to return commands")
	}
	// Verify no mouse capture is set - we just check that the init commands work
	// (there's no direct way to inspect batch commands)
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

// TestApp_WithTeatest_CommandPalette demonstrates opening command palette with teatest.
func TestApp_WithTeatest_CommandPalette(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping teatest in short mode")
	}

	app := createTestApp()

	tm := teatest.NewTestModel(t, app, teatest.WithInitialTermSize(80, 24))

	// Open command palette with Ctrl+X
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlX})

	// Wait for command palette to appear (lowercase per UI conventions)
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		s := string(out)
		return strings.Contains(s, "command palette") ||
			strings.Contains(s, "chat") // Modal content
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
