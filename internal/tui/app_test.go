package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/components"
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
		sidebar:      NewSidebarModel(rpc, nil, styles, clientConfig.Rendering.SidebarAnimation),
		clientConfig: clientConfig,
		width:        80,
		height:       24,
		projectDir:   "/test/project",
		activeModal:  ModalNone,
	}
	// Initialize sub-models
	app.chat = models.NewChatModel(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage, "once")
	app.tasks = models.NewTasksModel(rpc)
	app.queue = models.NewQueueModel(rpc)
	app.memory = models.NewMemoryModel(rpc)
	// Create modals
	app.commandPalette = CommandPaletteModal(styles, clientConfig)
	app.sessionPicker = NewSessionPickerModal(styles, rpc, clientConfig)
	// Initialize notification manager
	app.notifications = components.NewNotificationManager()
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
		{"switch to chat", "c", ViewChat},
		{"switch to tasks", "t", ViewTasks},
		{"switch to queue", "q", ViewQueue},
		{"switch to memory", "m", ViewMemory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := createTestApp()

			// Open command palette
			app.activeModal = ModalCommandPalette
			app.commandPalette.Show()

			// Send the key
			msg := tea.KeyPressMsg{Code: rune(tt.key[0]), Text: tt.key}
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
	msg := tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
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
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.activeModal != ModalNone {
		t.Error("expected modal to be closed after escape")
	}
}

func TestApp_Quit(t *testing.T) {
	app := createTestApp()

	// Send Ctrl+C
	msg := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
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

	tabs := ansi.Strip(app.renderTabs())

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
	if !strings.Contains(statusBar, "esc") {
		t.Error("expected esc hint in status bar")
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
	msg := tea.KeyPressMsg{Code: 'y', Text: "y"} // 'y' for sidebar in new keybindings
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
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
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
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
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
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
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
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.currentView != ViewChat {
		t.Error("expected escape from tasks to switch to chat")
	}
}

func TestApp_CopySuccessMessage(t *testing.T) {
	app := createTestApp()

	// Copy success should be silent: no status message displayed.
	newModel, _ := app.Update(CopySuccessMsg{Text: "test text"})
	newApp := newModel.(*App)

	if newApp.statusMessage != "" {
		t.Errorf("expected no status message on copy success, got %q", newApp.statusMessage)
	}
}

func TestApp_CopyErrorMessage(t *testing.T) {
	app := createTestApp()

	// Copy errors should still surface to the user.
	newModel, _ := app.Update(CopyErrorMsg{Err: &testError{"copy failed"}})
	newApp := newModel.(*App)

	if !strings.Contains(newApp.statusMessage, "Copy failed") {
		t.Error("expected status message to contain error")
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
	if !strings.Contains(view.Content, "command palette") {
		t.Error("expected modal content in view when modal is active")
	}
}

func TestApp_SessionPickerModal(t *testing.T) {
	app := createTestApp()

	// Open command palette first
	app.activeModal = ModalCommandPalette
	app.commandPalette.Show()

	// Press 's' to open session picker
	msg := tea.KeyPressMsg{Code: 's', Text: "s"}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.activeModal != ModalSessionPicker {
		t.Error("expected session picker to be open")
	}
}

func TestApp_NoMouseCapture(t *testing.T) {
	app := createTestApp()

	// Init should NOT include mouse capture commands
	cmd := app.Init()
	if cmd == nil {
		t.Error("expected Init to return commands")
	}
	// Verify no mouse capture is set - we just check that the init commands work
	// (there's no direct way to inspect batch commands)
}

// TestApp_Program_BasicRender verifies that the App renders without panicking
// when run under a bubbletea v2 Program with in-memory I/O.
func TestApp_Program_BasicRender(t *testing.T) {
	var buf bytes.Buffer
	var in bytes.Buffer

	rpc := NewRPCClient("/tmp/meept-test-nonexistent.sock")
	styles := DefaultStyles()
	clientConfig := DefaultClientConfig()
	app := &App{
		styles:       styles,
		rpc:          rpc,
		currentView:  ViewChat,
		keys:         DefaultKeyMap(),
		sidebar:      NewSidebarModel(rpc, nil, styles, clientConfig.Rendering.SidebarAnimation),
		clientConfig: clientConfig,
		width:        80,
		height:       24,
		projectDir:   "/test/project",
		activeModal:  ModalNone,
	}
	app.chat = models.NewChatModel(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage, "once")
	app.tasks = models.NewTasksModel(rpc)
	app.queue = models.NewQueueModel(rpc)
	app.memory = models.NewMemoryModel(rpc)
	app.commandPalette = CommandPaletteModal(styles, clientConfig)
	app.sessionPicker = NewSessionPickerModal(styles, rpc, clientConfig)
	app.notifications = components.NewNotificationManager()
	app.chat.SetSize(app.width-2, app.height-4)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := tea.NewProgram(app,
		tea.WithContext(ctx),
		tea.WithInput(&in),
		tea.WithOutput(&buf),
	)

	// Run in background; send quit after brief delay
	go func() {
		time.Sleep(500 * time.Millisecond)
		p.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	}()

	_, err := p.Run()
	if err != nil && !strings.Contains(err.Error(), "context") {
		t.Fatalf("program.Run() error: %v", err)
	}

	// Verify output was produced
	if buf.Len() == 0 {
		t.Error("expected program to produce output")
	}
}

// TestApp_Program_CommandPalette verifies that the command palette can be
// opened and closed without panicking under a real Program.
func TestApp_Program_CommandPalette(t *testing.T) {
	var buf bytes.Buffer
	var in bytes.Buffer

	rpc := NewRPCClient("/tmp/meept-test-nonexistent.sock")
	styles := DefaultStyles()
	clientConfig := DefaultClientConfig()
	app := &App{
		styles:       styles,
		rpc:          rpc,
		currentView:  ViewChat,
		keys:         DefaultKeyMap(),
		sidebar:      NewSidebarModel(rpc, nil, styles, clientConfig.Rendering.SidebarAnimation),
		clientConfig: clientConfig,
		width:        80,
		height:       24,
		projectDir:   "/test/project",
		activeModal:  ModalNone,
	}
	app.chat = models.NewChatModel(rpc, styles.UserMessage, styles.AssistantMessage, styles.SystemMessage, "once")
	app.tasks = models.NewTasksModel(rpc)
	app.queue = models.NewQueueModel(rpc)
	app.memory = models.NewMemoryModel(rpc)
	app.commandPalette = CommandPaletteModal(styles, clientConfig)
	app.sessionPicker = NewSessionPickerModal(styles, rpc, clientConfig)
	app.notifications = components.NewNotificationManager()
	app.chat.SetSize(app.width-2, app.height-4)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := tea.NewProgram(app,
		tea.WithContext(ctx),
		tea.WithInput(&in),
		tea.WithOutput(&buf),
	)

	// Send: Ctrl+X (open palette), Escape (close), Ctrl+C (quit)
	go func() {
		time.Sleep(300 * time.Millisecond)
		p.Send(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
		time.Sleep(200 * time.Millisecond)
		p.Send(tea.KeyPressMsg{Code: tea.KeyEscape})
		time.Sleep(200 * time.Millisecond)
		p.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	}()

	_, err := p.Run()
	if err != nil && !strings.Contains(err.Error(), "context") {
		t.Fatalf("program.Run() error: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("expected program to produce output")
	}
}
