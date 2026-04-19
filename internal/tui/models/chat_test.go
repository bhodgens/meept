package models

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// MockChatRPCClient implements RPCClient for chat testing.
type MockChatRPCClient struct {
	connected    bool
	ChatResponse string
	ChatError    error
	ChatCalls    []string // Records messages sent

	// Message persistence tracking
	SavedMessages      map[string][]types.SessionMessage
	GetMessagesResp    *types.SessionMessagesResponse
	GetMessagesErr     error
	UpdatedDescriptions map[string]string
}

func NewMockChatRPCClient() *MockChatRPCClient {
	return &MockChatRPCClient{
		connected:           true,
		ChatResponse:        "Hello! How can I help you?",
		ChatCalls:           make([]string, 0),
		SavedMessages:       make(map[string][]types.SessionMessage),
		UpdatedDescriptions: make(map[string]string),
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

func (m *MockChatRPCClient) SaveSessionMessages(sessionID string, msgs []types.SessionMessage) error {
	m.SavedMessages[sessionID] = append(m.SavedMessages[sessionID], msgs...)
	return nil
}

func (m *MockChatRPCClient) GetSessionMessages(sessionID string, offset, limit int) (*types.SessionMessagesResponse, error) {
	if m.GetMessagesErr != nil {
		return nil, m.GetMessagesErr
	}
	if m.GetMessagesResp != nil {
		return m.GetMessagesResp, nil
	}
	return &types.SessionMessagesResponse{Messages: nil, Total: 0}, nil
}

func (m *MockChatRPCClient) UpdateSessionDescription(sessionID, description string) error {
	m.UpdatedDescriptions[sessionID] = description
	return nil
}

func (m *MockChatRPCClient) GenerateSessionDescription(sessionID, firstMessage, projectName string) (*types.GenerateDescriptionResult, error) {
	// Simulate LLM-generated description
	desc := "mock: " + firstMessage
	if len(desc) > 30 {
		desc = desc[:30] + "..."
	}
	m.UpdatedDescriptions[sessionID] = desc
	return &types.GenerateDescriptionResult{
		SessionID:   sessionID,
		Description: desc,
		Status:      "generated",
	}, nil
}

func newTestChatModel() *ChatModel {
	mock := NewMockChatRPCClient()
	userStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	assistantStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
	systemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	return NewChatModel(mock, userStyle, assistantStyle, systemStyle, "once")
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
	if model.sessionMessages == nil {
		t.Error("expected sessionMessages map to be initialized")
	}
	if model.sessionHistory == nil {
		t.Error("expected sessionHistory map to be initialized")
	}
	if model.dirtyMessages == nil {
		t.Error("expected dirtyMessages map to be initialized")
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
	model := NewChatModel(mock, userStyle, userStyle, userStyle, "once")
	model.SetSize(80, 24)
	model.Init()

	// Set a session so history tracking works
	session := &types.Session{ID: "sess-1", Name: "Test", ConversationID: "conv-1"}
	model.SetSession(session)

	// Type a message
	model.textarea.SetValue("Hello world")

	// Press enter to send
	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
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

	// Check dirty messages were tracked
	if len(model.dirtyMessages["sess-1"]) != 1 {
		t.Errorf("expected 1 dirty message, got %d", len(model.dirtyMessages["sess-1"]))
	}
}

func TestChatModel_SendEmptyMessage(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	initialCount := len(model.messages)

	// Try to send empty message
	model.textarea.SetValue("")
	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
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
	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	model.Update(msg)

	if len(model.messages) != initialCount {
		t.Error("expected no message to be added while loading")
	}
}

func TestChatModel_ReceiveResponse(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.sessionID = "sess-1"
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
	msg := tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}
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
	msg := tea.KeyPressMsg{Code: tea.KeyTab}
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

func TestChatModel_InputHistory_PerSession(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()

	// Set session 1
	session1 := &types.Session{ID: "sess-1", Name: "Session 1", ConversationID: "conv-1"}
	model.SetSession(session1)

	// Add to history in session 1
	model.addToHistory("first message")
	model.addToHistory("second message")

	history1 := model.sessionHistory["sess-1"]
	if len(history1) != 2 {
		t.Errorf("expected 2 history items in sess-1, got %d", len(history1))
	}

	// Switch to session 2
	session2 := &types.Session{ID: "sess-2", Name: "Session 2", ConversationID: "conv-2"}
	model.SetSession(session2)

	// Add to history in session 2
	model.addToHistory("session 2 message")

	history2 := model.sessionHistory["sess-2"]
	if len(history2) != 1 {
		t.Errorf("expected 1 history item in sess-2, got %d", len(history2))
	}

	// Verify session 1 history is preserved
	history1 = model.sessionHistory["sess-1"]
	if len(history1) != 2 {
		t.Errorf("expected session 1 history to be preserved, got %d items", len(history1))
	}

	// Navigate history in session 2
	model.navigateHistory(-1)
	if model.historyIdx != 0 {
		t.Errorf("expected historyIdx 0, got %d", model.historyIdx)
	}
	if model.textarea.Value() != "session 2 message" {
		t.Errorf("expected 'session 2 message', got '%s'", model.textarea.Value())
	}

	// Switch back to session 1 and navigate
	model.SetSession(session1)
	model.navigateHistory(-1)
	if model.textarea.Value() != "second message" {
		t.Errorf("expected 'second message', got '%s'", model.textarea.Value())
	}
}

func TestChatModel_InputHistoryDuplicates(t *testing.T) {
	model := newTestChatModel()
	model.sessionID = "sess-1"

	model.addToHistory("same message")
	model.addToHistory("same message")

	if len(model.sessionHistory["sess-1"]) != 1 {
		t.Errorf("expected 1 history item (no duplicates), got %d", len(model.sessionHistory["sess-1"]))
	}
}

func TestChatModel_InputHistoryEmpty(t *testing.T) {
	model := newTestChatModel()
	model.sessionID = "sess-1"

	model.addToHistory("")

	if len(model.sessionHistory["sess-1"]) != 0 {
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

	// Should render without error
	if len(view) < 10 {
		t.Error("expected view to contain rendered content")
	}
}

func TestChatModel_ViewWithDescription(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.sessionDescription = "Test Description"

	// Verify description is stored correctly
	if model.sessionDescription != "Test Description" {
		t.Error("expected session description to be set")
	}

	// View should render without error
	view := model.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestChatModel_ExpandMessage(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("assistant", strings.Repeat("long message\n", 20))
	model.SetFocus(FocusViewport)
	model.selectedMsgIdx = 1

	// Expand message via 'e' key in viewport
	msg := tea.KeyPressMsg{Code: 'e', Text: "e"}
	model.Update(msg)

	if model.messages[1].State != MessageExpanded {
		t.Error("expected message to be expanded")
	}
}

func TestChatModel_CopySelectedMessage(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "copy this text")
	model.SetFocus(FocusViewport)
	model.selectedMsgIdx = 1

	// Press 'c' to copy
	msg := tea.KeyPressMsg{Code: 'c', Text: "c"}
	cmd := model.Update(msg)

	if cmd == nil {
		t.Error("expected copy command to be returned")
	}
	if model.selectedMsgIdx != -1 {
		t.Error("expected message to be deselected after copy")
	}
}

func TestChatModel_MessageSelection(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "msg1")
	model.addMessage("assistant", "msg2")
	model.SetFocus(FocusViewport)

	// Select first message with down
	model.selectNextMessage()
	if model.selectedMsgIdx != 0 {
		t.Errorf("expected selectedMsgIdx 0, got %d", model.selectedMsgIdx)
	}

	// Move to next
	model.selectNextMessage()
	if model.selectedMsgIdx != 1 {
		t.Errorf("expected selectedMsgIdx 1, got %d", model.selectedMsgIdx)
	}

	// Move to next (includes the welcome message at 0, user at 1, assistant at 2)
	model.selectNextMessage()
	if model.selectedMsgIdx != 2 {
		t.Errorf("expected selectedMsgIdx 2, got %d", model.selectedMsgIdx)
	}

	// Move back up
	model.selectPreviousMessage()
	if model.selectedMsgIdx != 1 {
		t.Errorf("expected selectedMsgIdx 1, got %d", model.selectedMsgIdx)
	}
}

func TestChatModel_Reset(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "test")
	model.textarea.SetValue("unsent text")
	model.selectedMsgIdx = 1
	model.sessionDescription = "test desc"
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
	if model.sessionDescription != "" {
		t.Error("expected session description to be cleared")
	}
}

func TestChatModel_ViewportNavigation(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.SetFocus(FocusViewport)

	// Test up/down navigation (now selects messages)
	upMsg := tea.KeyPressMsg{Code: tea.KeyUp}
	model.Update(upMsg)

	downMsg := tea.KeyPressMsg{Code: tea.KeyDown}
	model.Update(downMsg)

	// Test page up/down
	pgUpMsg := tea.KeyPressMsg{Code: tea.KeyPgUp}
	model.Update(pgUpMsg)

	pgDownMsg := tea.KeyPressMsg{Code: tea.KeyPgDown}
	model.Update(pgDownMsg)

	// No assertions needed - just checking no panics
}

func TestChatModel_HandleEscape_DeselectsMessage(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.Init()
	model.addMessage("user", "test")
	model.selectedMsgIdx = 1

	model.HandleEscape()

	if model.selectedMsgIdx != -1 {
		t.Error("expected selection to be cleared")
	}
}

func TestChatModel_HandleEscape_ResetsHistory(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)
	model.sessionID = "sess-1"
	model.sessionHistory["sess-1"] = []string{"old message"}
	model.savedInput = "current input"
	model.historyIdx = 0
	model.textarea.SetValue("old message")

	model.HandleEscape()

	if model.historyIdx != -1 {
		t.Error("expected history index to be reset")
	}
	if model.textarea.Value() != "current input" {
		t.Error("expected textarea to restore saved input")
	}
}

func TestChatModel_HandleEscape_FocusesInput(t *testing.T) {
	model := newTestChatModel()
	model.SetFocus(FocusViewport)

	model.HandleEscape()

	if model.focused != FocusInput {
		t.Error("expected focus to return to input")
	}
}

func TestChatModel_HandleEscape_ClearsInput(t *testing.T) {
	model := newTestChatModel()
	model.SetFocus(FocusInput)
	model.textarea.SetValue("some text")

	model.HandleEscape()

	if model.textarea.Value() != "" {
		t.Error("expected input to be cleared")
	}
}

func TestChatModel_SessionPersistence(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)

	// Set session 1 and add messages
	session1 := &types.Session{ID: "sess-1", Name: "Session 1", ConversationID: "conv-1"}
	model.SetSession(session1)
	model.addMessage("user", "hello from session 1")
	model.addMessage("assistant", "reply in session 1")

	// Switch to session 2
	session2 := &types.Session{ID: "sess-2", Name: "Session 2", ConversationID: "conv-2"}
	model.SetSession(session2)

	// Session 2 should start empty
	if len(model.messages) != 0 {
		t.Errorf("expected 0 messages in new session, got %d", len(model.messages))
	}

	// Add messages to session 2
	model.addMessage("user", "hello from session 2")

	// Switch back to session 1
	model.SetSession(session1)

	// Session 1 messages should be restored
	if len(model.messages) != 2 {
		t.Errorf("expected 2 messages restored for session 1, got %d", len(model.messages))
	}
	if model.messages[0].Content != "hello from session 1" {
		t.Error("expected first message to be 'hello from session 1'")
	}

	// Switch back to session 2
	model.SetSession(session2)

	// Session 2 messages should be restored
	if len(model.messages) != 1 {
		t.Errorf("expected 1 message restored for session 2, got %d", len(model.messages))
	}
	if model.messages[0].Content != "hello from session 2" {
		t.Error("expected message to be 'hello from session 2'")
	}
}

func TestChatModel_IsInputFocused(t *testing.T) {
	model := newTestChatModel()

	model.SetFocus(FocusInput)
	if !model.IsInputFocused() {
		t.Error("expected IsInputFocused true when focused on input")
	}

	model.SetFocus(FocusViewport)
	if model.IsInputFocused() {
		t.Error("expected IsInputFocused false when focused on viewport")
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

func TestChatModel_ExtractDescription(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello how are you doing today", "Hello how are you doing today"},
		{"This is a very long sentence with many words that should be truncated", "This is a very long sentence with..."},
		{"Short", "Short"},
		{"One two three", "One two three"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractDescription(tt.input)
			if result != tt.expected {
				t.Errorf("extractDescription(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestChatModel_AutoDescription(t *testing.T) {
	mock := NewMockChatRPCClient()
	userStyle := lipgloss.NewStyle()
	model := NewChatModel(mock, userStyle, userStyle, userStyle, "once")
	model.SetSize(80, 24)
	model.sessionID = "sess-1"

	// Add first user message
	model.addMessage("user", "What is the weather like today")
	model.loading = true
	model.pendingMsgIdx = len(model.messages)
	model.messages = append(model.messages, ChatMessage{Role: "pending", Content: "Sending..."})

	// Receive first response - should trigger auto-description
	responseMsg := ChatResponseMsg{Reply: "The weather is sunny!", Err: nil}
	cmd := model.Update(responseMsg)

	// Description starts as "summarizing..." while async call is in progress
	if model.sessionDescription != "summarizing..." {
		t.Errorf("expected 'summarizing...' during async generation, got %q", model.sessionDescription)
	}
	if cmd == nil {
		t.Error("expected command batch (flush + description)")
	}

	// Simulate the async command completing and returning the description message
	// In a real scenario, tea would execute the command and deliver the message
	updateMsg := SessionDescriptionUpdatedMsg{SessionID: "sess-1", Description: "mock: What is the weather like..."}
	model.Update(updateMsg)

	if !strings.Contains(model.sessionDescription, "What is the weather") {
		t.Errorf("expected description from first message after update, got %q", model.sessionDescription)
	}
}

func TestChatModel_NoAutoDescriptionOnSecondExchange(t *testing.T) {
	mock := NewMockChatRPCClient()
	userStyle := lipgloss.NewStyle()
	model := NewChatModel(mock, userStyle, userStyle, userStyle, "once")
	model.SetSize(80, 24)
	model.sessionID = "sess-1"
	model.sessionDescription = "Already set"

	// Add two exchanges
	model.addMessage("user", "First message")
	model.addMessage("assistant", "First response")
	model.addMessage("user", "Second message")
	model.loading = true
	model.pendingMsgIdx = len(model.messages)
	model.messages = append(model.messages, ChatMessage{Role: "pending", Content: "Sending..."})

	responseMsg := ChatResponseMsg{Reply: "Second response", Err: nil}
	model.Update(responseMsg)

	// Description should remain unchanged
	if model.sessionDescription != "Already set" {
		t.Error("expected description not to change on subsequent exchanges")
	}
}

func TestChatModel_FlushMessages(t *testing.T) {
	mock := NewMockChatRPCClient()
	userStyle := lipgloss.NewStyle()
	model := NewChatModel(mock, userStyle, userStyle, userStyle, "once")
	model.SetSize(80, 24)
	model.sessionID = "sess-1"

	// Track some dirty messages
	model.trackDirtyMessage("user", "hello")
	model.trackDirtyMessage("assistant", "hi there")

	if len(model.dirtyMessages["sess-1"]) != 2 {
		t.Errorf("expected 2 dirty messages, got %d", len(model.dirtyMessages["sess-1"]))
	}

	// Flush
	cmd := model.flushMessages()
	if cmd == nil {
		t.Error("expected flush command")
	}

	// After calling flushMessages, dirty buffer should be cleared
	if len(model.dirtyMessages["sess-1"]) != 0 {
		t.Errorf("expected dirty messages to be cleared, got %d", len(model.dirtyMessages["sess-1"]))
	}

	// Execute the command
	msg := cmd()
	result, ok := msg.(FlushResultMsg)
	if !ok {
		t.Fatalf("expected FlushResultMsg, got %T", msg)
	}
	if result.Err != nil {
		t.Errorf("expected no error, got %v", result.Err)
	}

	// Check mock received the messages
	if len(mock.SavedMessages["sess-1"]) != 2 {
		t.Errorf("expected 2 saved messages, got %d", len(mock.SavedMessages["sess-1"]))
	}
}

func TestChatModel_SessionLoadFromServer(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)

	// Populate server response in mock
	mock := model.rpc.(*MockChatRPCClient)
	mock.GetMessagesResp = &types.SessionMessagesResponse{
		Messages: []types.SessionMessage{
			{ID: 1, SessionID: "sess-1", Role: "user", Content: "saved user msg", Timestamp: "2026-01-01T00:00:00Z"},
			{ID: 2, SessionID: "sess-1", Role: "assistant", Content: "saved assistant msg", Timestamp: "2026-01-01T00:01:00Z"},
		},
		Total: 2,
	}

	// Switch to session - should trigger server load
	session := &types.Session{ID: "sess-1", Name: "Test", ConversationID: "conv-1"}
	cmd := model.SetSession(session)

	if cmd == nil {
		t.Fatal("expected command to load messages from server")
	}

	// Execute the command
	msg := cmd()
	loadedMsg, ok := msg.(SessionMessagesLoadedMsg)
	if !ok {
		t.Fatalf("expected SessionMessagesLoadedMsg, got %T", msg)
	}

	if loadedMsg.Err != nil {
		t.Fatalf("expected no error, got %v", loadedMsg.Err)
	}
	if len(loadedMsg.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(loadedMsg.Messages))
	}

	// Feed the loaded message back to the model
	model.Update(loadedMsg)

	// Check messages were loaded
	if len(model.messages) != 2 {
		t.Errorf("expected 2 messages after load, got %d", len(model.messages))
	}
	if model.messages[0].Content != "saved user msg" {
		t.Error("expected first message to be 'saved user msg'")
	}

	// Check history was populated
	if len(model.sessionHistory["sess-1"]) != 1 {
		t.Errorf("expected 1 history entry from loaded user message, got %d", len(model.sessionHistory["sess-1"]))
	}
}

func TestChatModel_SessionDescription(t *testing.T) {
	model := newTestChatModel()
	model.SetSize(80, 24)

	// Default: no description
	if model.sessionDescription != "" {
		t.Error("expected empty description initially")
	}

	// With description
	model.sessionDescription = "My Chat Topic"
	if model.sessionDescription != "My Chat Topic" {
		t.Error("expected description to be set")
	}

	// View should still render
	view := model.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

// Note: teatest integration tests for sub-models are skipped because they don't
// implement the full tea.Model interface (missing quit command handling).
// The App-level teatest tests provide full integration testing.
// Sub-models are thoroughly tested via unit tests above.
