package models

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MockChatRPCClient implements RPCClient for chat testing.
type MockChatRPCClient struct {
	connected    bool
	ChatResponse string
	ChatError    error
	ChatCalls    []string // Records messages sent
}

func NewMockChatRPCClient() *MockChatRPCClient {
	return &MockChatRPCClient{
		connected:    true,
		ChatResponse: "Hello! How can I help you?",
		ChatCalls:    make([]string, 0),
	}
}

func (m *MockChatRPCClient) Chat(message, conversationID string) (string, error) {
	m.ChatCalls = append(m.ChatCalls, message)
	if m.ChatError != nil {
		return "", m.ChatError
	}
	if m.ChatResponse != "" {
		return m.ChatResponse, nil
	}
	return "Mock response to: " + message, nil
}

func (m *MockChatRPCClient) IsConnected() bool {
	return m.connected
}

func newTestChatModel() *ChatModel {
	mock := NewMockChatRPCClient()
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	assistantStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
	systemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	return NewChatModel(mock, userStyle, assistantStyle, systemStyle)
}

func TestChatModel_NewChatModel(t *testing.T) {
	model := newTestChatModel()

	if model == nil {
		t.Fatal("expected non-nil chat model")
	}
	if model.conversationID == "" {
		t.Error("expected conversation ID to be generated")
	}
	if model.focused != FocusInput {
		t.Error("expected initial focus on input")
	}
	if model.selectedMsgIdx != -1 {
		t.Error("expected no message selected initially")
	}
	if model.pendingMsgIdx != -1 {
		t.Error("expected no pending message initially")
	}
}

func TestChatModel_Init(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	cmd := model.Init()

	if cmd == nil {
		t.Error("expected Init to return a command")
	}

	if len(model.messages) != 1 {
		t.Errorf("expected 1 welcome message, got %d", len(model.messages))
	}
	if model.messages[0].Role != "system" {
		t.Errorf("expected system message, got %s", model.messages[0].Role)
	}
}

func TestChatModel_SetSize(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(100, 40)

	if model.width != 100 {
		t.Errorf("expected width 100, got %d", model.width)
	}
	if model.height != 40 {
		t.Errorf("expected height 40, got %d", model.height)
	}
}

func TestChatModel_SendMessage(t *testing.T) {
	mock := NewMockChatRPCClient()
	userStyle := lipgloss.NewStyle()
	model := NewChatModel(mock, userStyle, userStyle, userStyle)
	model.SetSize(80, 24)
	model.Init()

	// Type a message
	model.textarea.SetValue("Hello world")

	// Press enter to send
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	cmd := model.Update(msg)

	// Check user message was added
	found := false
	for _, m := range model.messages {
		if m.Role == "user" && m.Content == "Hello world" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected user message to be added")
	}

	// Check pending message exists
	hasPending := false
	for _, m := range model.messages {
		if m.Role == "pending" {
			hasPending = true
			break
		}
	}
	if !hasPending {
		t.Error("expected pending message while loading")
	}

	// Check loading state
	if !model.loading {
		t.Error("expected loading state to be true")
	}

	// Check command was returned
	if cmd == nil {
		t.Error("expected command to be returned for async chat")
	}

	// Check textarea was cleared
	if model.textarea.Value() != "" {
		t.Error("expected textarea to be cleared after send")
	}
}

func TestChatModel_SendEmptyMessage(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	initialCount := len(model.messages)

	// Try to send empty message
	model.textarea.SetValue("")
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	model.Update(msg)

	if len(model.messages) != initialCount {
		t.Error("expected no message to be added for empty input")
	}
}

func TestChatModel_SendWhileLoading(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.loading = true
	initialCount := len(model.messages)

	model.textarea.SetValue("test message")
	msg := tea.KeyMsg{Type: tea.KeyEnter}
	model.Update(msg)

	if len(model.messages) != initialCount {
		t.Error("expected no message to be added while loading")
	}
}

func TestChatModel_ReceiveResponse(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.loading = true
	model.pendingMsgIdx = len(model.messages)
	model.messages = append(model.messages, ChatMessage{
		Role:    "pending",
		Content: "Sending...",
	})

	// Receive response
	responseMsg := ChatResponseMsg{Reply: "This is the response", Err: nil}
	model.Update(responseMsg)

	if model.loading {
		t.Error("expected loading to be false after response")
	}
	if model.pendingMsgIdx != -1 {
		t.Error("expected pending message index to be reset")
	}

	// Check assistant message was added
	found := false
	for _, m := range model.messages {
		if m.Role == "assistant" && m.Content == "This is the response" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected assistant message to be added")
	}

	// Check pending message was removed
	for _, m := range model.messages {
		if m.Role == "pending" {
			t.Error("expected pending message to be removed")
		}
	}
}

func TestChatModel_ReceiveError(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.loading = true
	model.pendingMsgIdx = len(model.messages)
	model.messages = append(model.messages, ChatMessage{
		Role:    "pending",
		Content: "Sending...",
	})

	// Receive error
	responseMsg := ChatResponseMsg{Err: errors.New("connection failed")}
	model.Update(responseMsg)

	// Check error message was added
	found := false
	for _, m := range model.messages {
		if m.Role == "system" && strings.Contains(m.Content, "connection failed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error message to be added")
	}
}

func TestChatModel_ClearChat(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "test message")
	model.addMessage("assistant", "test response")
	originalConvID := model.conversationID

	// Press Ctrl+L to clear
	msg := tea.KeyMsg{Type: tea.KeyCtrlL}
	model.Update(msg)

	if len(model.messages) != 0 {
		t.Errorf("expected messages to be cleared, got %d", len(model.messages))
	}
	if model.conversationID == originalConvID {
		t.Error("expected new conversation ID after clear")
	}
}

func TestChatModel_FocusCycling(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)

	// Initial focus should be input
	if model.focused != FocusInput {
		t.Error("expected initial focus on input")
	}

	// Tab to viewport
	msg := tea.KeyMsg{Type: tea.KeyTab}
	model.Update(msg)
	if model.focused != FocusViewport {
		t.Error("expected focus on viewport after tab")
	}

	// Tab again should signal sidebar focus
	toSidebar := model.CycleFocus()
	if !toSidebar {
		t.Error("expected CycleFocus to return true when cycling from viewport")
	}
}

func TestChatModel_SetFocus(t *testing.T) {
	model := newTestChatModel()

	model.SetFocus(FocusViewport)
	if model.focused != FocusViewport {
		t.Error("expected focus on viewport")
	}
	if !model.viewportActive {
		t.Error("expected viewport to be active")
	}

	model.SetFocus(FocusInput)
	if model.focused != FocusInput {
		t.Error("expected focus on input")
	}
	if model.viewportActive {
		t.Error("expected viewport to be inactive")
	}
}

func TestChatModel_InputHistory(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()

	// Add to history
	model.addToHistory("first message")
	model.addToHistory("second message")
	model.addToHistory("third message")

	if len(model.inputHistory) != 3 {
		t.Errorf("expected 3 history items, got %d", len(model.inputHistory))
	}

	// Navigate up through history
	model.navigateHistory(-1)
	if model.historyIdx != 2 {
		t.Errorf("expected historyIdx 2, got %d", model.historyIdx)
	}
	if model.textarea.Value() != "third message" {
		t.Errorf("expected 'third message', got '%s'", model.textarea.Value())
	}

	model.navigateHistory(-1)
	if model.textarea.Value() != "second message" {
		t.Errorf("expected 'second message', got '%s'", model.textarea.Value())
	}

	// Navigate down
	model.navigateHistory(1)
	if model.textarea.Value() != "third message" {
		t.Errorf("expected 'third message', got '%s'", model.textarea.Value())
	}
}

func TestChatModel_InputHistoryDuplicates(t *testing.T) {
	model := newTestChatModel()

	model.addToHistory("same message")
	model.addToHistory("same message")

	if len(model.inputHistory) != 1 {
		t.Errorf("expected 1 history item (no duplicates), got %d", len(model.inputHistory))
	}
}

func TestChatModel_InputHistoryEmpty(t *testing.T) {
	model := newTestChatModel()

	initialLen := len(model.inputHistory)
	model.addToHistory("")

	if len(model.inputHistory) != initialLen {
		t.Error("expected empty string not to be added to history")
	}
}

func TestChatModel_MessageStates(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)

	msg := ChatMessage{
		Role:    "assistant",
		Content: strings.Repeat("line\n", 20),
		State:   MessageCollapsed,
	}
	model.messages = append(model.messages, msg)

	content := model.getMessageContent(model.messages[0])
	if !strings.Contains(content, "lines hidden") {
		t.Error("expected collapsed indicator in content")
	}
}

func TestChatModel_View(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()

	view := model.View()

	// Should contain the viewport and textarea areas
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestChatModel_ViewWithContextMenu(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "test message")
	model.selectedMsgIdx = 1
	model.showContextMenu = true

	view := model.View()

	// Should contain context menu
	if !strings.Contains(view, "Copy") {
		t.Error("expected context menu in view")
	}
}

func TestChatModel_HandleContextMenuKey(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "test message")
	model.selectedMsgIdx = 1
	model.showContextMenu = true

	// Press escape to close menu
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("esc")}
	model.handleContextMenuKey(msg)

	if model.showContextMenu {
		t.Error("expected context menu to be closed")
	}
}

func TestChatModel_ExpandCollapseMessage(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("assistant", strings.Repeat("long message\n", 20))
	model.selectedMsgIdx = 1
	model.showContextMenu = true

	// Expand message
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}
	model.handleContextMenuKey(msg)

	if model.messages[1].State != MessageExpanded {
		t.Error("expected message to be expanded")
	}

	// Collapse message
	model.showContextMenu = true
	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}
	model.handleContextMenuKey(msg)

	if model.messages[1].State != MessageCollapsed {
		t.Error("expected message to be collapsed")
	}
}

func TestChatModel_Reset(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "test")
	model.textarea.SetValue("unsent text")
	model.selectedMsgIdx = 1
	originalConvID := model.conversationID

	model.Reset()

	if len(model.messages) != 0 {
		t.Error("expected messages to be cleared")
	}
	if model.textarea.Value() != "" {
		t.Error("expected textarea to be cleared")
	}
	if model.selectedMsgIdx != -1 {
		t.Error("expected selection to be cleared")
	}
	if model.conversationID == originalConvID {
		t.Error("expected new conversation ID")
	}
}

func TestChatModel_ViewportNavigation(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.SetFocus(FocusViewport)

	// Test up/down navigation
	upMsg := tea.KeyMsg{Type: tea.KeyUp}
	model.Update(upMsg)

	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	model.Update(downMsg)

	// Test page up/down
	pgUpMsg := tea.KeyMsg{Type: tea.KeyPgUp}
	model.Update(pgUpMsg)

	pgDownMsg := tea.KeyMsg{Type: tea.KeyPgDown}
	model.Update(pgDownMsg)

	// No assertions needed - just checking no panics
}

func TestChatModel_EscapeDeselects(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "test")
	model.selectedMsgIdx = 1
	model.hasTextSelection = true

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	model.Update(msg)

	if model.selectedMsgIdx != -1 {
		t.Error("expected selection to be cleared")
	}
	if model.hasTextSelection {
		t.Error("expected text selection to be cleared")
	}
}

func TestChatModel_EscapeResetsHistory(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.addToHistory("old message")
	model.savedInput = "current input"
	model.historyIdx = 0
	model.textarea.SetValue("old message")

	msg := tea.KeyMsg{Type: tea.KeyEscape}
	model.Update(msg)

	if model.historyIdx != -1 {
		t.Error("expected history index to be reset")
	}
	if model.textarea.Value() != "current input" {
		t.Error("expected textarea to restore saved input")
	}
}

func TestChatModel_IsFocused(t *testing.T) {
	model := newTestChatModel()

	model.focused = FocusInput
	if !model.IsFocused() {
		t.Error("expected IsFocused true for FocusInput")
	}

	model.focused = FocusViewport
	if !model.IsFocused() {
		t.Error("expected IsFocused true for FocusViewport")
	}

	model.focused = FocusSidebar
	if model.IsFocused() {
		t.Error("expected IsFocused false for FocusSidebar")
	}
}

func TestChatModel_FormatMessage(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		width    int
		expected int // expected number of lines
	}{
		{"short message", "hello", 80, 1},
		{"long message", strings.Repeat("word ", 50), 40, 7}, // ~6-7 lines
		{"with newlines", "line1\nline2\nline3", 80, 3},
		{"zero width", "test", 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMessage(tt.text, tt.width)
			lines := strings.Count(result, "\n") + 1
			// Allow some variance due to word wrapping
			if lines < tt.expected-1 || lines > tt.expected+1 {
				t.Errorf("expected ~%d lines, got %d", tt.expected, lines)
			}
		})
	}
}

// Note: teatest integration tests for sub-models are skipped because they don't
// implement the full tea.Model interface (missing quit command handling).
// The App-level teatest tests provide full integration testing.
// Sub-models are thoroughly tested via unit tests above.
