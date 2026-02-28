package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestNewConversation(t *testing.T) {
	conv := NewConversation()

	if conv == nil {
		t.Fatal("NewConversation returned nil")
	}

	if conv.Len() != 0 {
		t.Errorf("expected empty conversation, got %d messages", conv.Len())
	}

	if conv.maxMessages != DefaultMaxMessages {
		t.Errorf("expected maxMessages=%d, got %d", DefaultMaxMessages, conv.maxMessages)
	}
}

func TestConversationWithOptions(t *testing.T) {
	conv := NewConversation(
		WithMaxMessages(50),
		WithContextLimit(50000),
		WithSystemPrompt("Test system prompt"),
	)

	if conv.maxMessages != 50 {
		t.Errorf("expected maxMessages=50, got %d", conv.maxMessages)
	}

	if conv.contextLimit != 50000 {
		t.Errorf("expected contextLimit=50000, got %d", conv.contextLimit)
	}

	if conv.systemPrompt != "Test system prompt" {
		t.Errorf("expected systemPrompt='Test system prompt', got '%s'", conv.systemPrompt)
	}
}

func TestConversationAddMessage(t *testing.T) {
	conv := NewConversation()

	conv.AddMessage(llm.ChatMessage{
		Role:    llm.RoleUser,
		Content: "Hello",
	})

	if conv.Len() != 1 {
		t.Errorf("expected 1 message, got %d", conv.Len())
	}

	conv.AddUserMessage("User message")
	conv.AddAssistantMessage("Assistant message")

	if conv.Len() != 3 {
		t.Errorf("expected 3 messages, got %d", conv.Len())
	}
}

func TestConversationGetMessages(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("System prompt"))

	conv.AddUserMessage("Hello")
	conv.AddAssistantMessage("Hi there")

	messages := conv.GetMessages()

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (system + 2), got %d", len(messages))
	}

	if messages[0].Role != llm.RoleSystem {
		t.Errorf("first message should be system, got %s", messages[0].Role)
	}

	if messages[0].Content != "System prompt" {
		t.Errorf("system message content mismatch")
	}

	if messages[1].Role != llm.RoleUser {
		t.Errorf("second message should be user, got %s", messages[1].Role)
	}

	if messages[2].Role != llm.RoleAssistant {
		t.Errorf("third message should be assistant, got %s", messages[2].Role)
	}
}

func TestConversationClear(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("System prompt"))

	conv.AddUserMessage("Hello")
	conv.AddAssistantMessage("Hi")
	conv.Clear()

	if conv.Len() != 0 {
		t.Errorf("expected 0 messages after clear, got %d", conv.Len())
	}

	// System prompt should still be there
	messages := conv.GetMessages()
	if len(messages) != 1 {
		t.Errorf("expected 1 message (system prompt only), got %d", len(messages))
	}
}

func TestConversationTruncate(t *testing.T) {
	conv := NewConversation(WithMaxMessages(5))

	// Add 10 messages
	for i := 0; i < 10; i++ {
		conv.AddUserMessage("Message")
	}

	if conv.Len() != 10 {
		t.Errorf("expected 10 messages before truncate, got %d", conv.Len())
	}

	removed := conv.Truncate()

	if removed != 5 {
		t.Errorf("expected 5 messages removed, got %d", removed)
	}

	if conv.Len() != 5 {
		t.Errorf("expected 5 messages after truncate, got %d", conv.Len())
	}
}

func TestConversationTruncateByTokens(t *testing.T) {
	conv := NewConversation()

	// Each message is roughly 100 characters = 25 tokens
	for i := 0; i < 10; i++ {
		msg := "This is a test message that is approximately one hundred characters in length for token testing."
		conv.AddUserMessage(msg)
	}

	// Truncate to fit ~100 tokens (2 messages worth)
	removed := conv.TruncateByTokens(100)

	if removed == 0 {
		t.Error("expected some messages to be removed")
	}

	if conv.Len() >= 10 {
		t.Error("expected fewer messages after token truncation")
	}
}

func TestConversationClone(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("System prompt"))

	conv.AddUserMessage("Hello")
	conv.AddAssistantMessage("Hi")

	clone := conv.Clone()

	// Verify clone is independent
	if clone.Len() != conv.Len() {
		t.Errorf("clone length mismatch: %d vs %d", clone.Len(), conv.Len())
	}

	// Modify original
	conv.AddUserMessage("New message")

	// Clone should be unaffected
	if clone.Len() != 2 {
		t.Errorf("clone was affected by original modification")
	}
}

func TestConversationLastMessage(t *testing.T) {
	conv := NewConversation()

	if conv.LastMessage() != nil {
		t.Error("expected nil for empty conversation")
	}

	conv.AddUserMessage("First")
	conv.AddAssistantMessage("Second")

	last := conv.LastMessage()
	if last == nil {
		t.Fatal("expected non-nil last message")
	}

	if last.Content != "Second" {
		t.Errorf("expected 'Second', got '%s'", last.Content)
	}
}

func TestConversationRemoveLast(t *testing.T) {
	conv := NewConversation()

	conv.AddUserMessage("First")
	conv.AddAssistantMessage("Second")

	removed := conv.RemoveLast()
	if removed == nil {
		t.Fatal("expected removed message")
	}

	if removed.Content != "Second" {
		t.Errorf("expected 'Second', got '%s'", removed.Content)
	}

	if conv.Len() != 1 {
		t.Errorf("expected 1 message, got %d", conv.Len())
	}
}

func TestConversationToolMessages(t *testing.T) {
	conv := NewConversation()

	toolCalls := []llm.ToolCall{
		{
			ID:   "call_123",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "test_tool",
				Arguments: `{"arg": "value"}`,
			},
		},
	}

	conv.AddAssistantMessageWithToolCalls("", toolCalls)
	conv.AddToolResult("call_123", `{"result": "success"}`)

	messages := conv.GetMessages()
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if len(messages[0].ToolCalls) != 1 {
		t.Error("expected tool calls in assistant message")
	}

	if messages[1].Role != llm.RoleTool {
		t.Errorf("expected tool role, got %s", messages[1].Role)
	}

	if messages[1].ToolCallID != "call_123" {
		t.Errorf("expected tool_call_id='call_123', got '%s'", messages[1].ToolCallID)
	}
}

func TestConversationInjectContext(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("Main system prompt"))

	conv.AddUserMessage("User message")

	conv.InjectContext("Memory context 1")

	messages := conv.GetMessages()
	// Should have: system prompt + context + user message
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// Context should be at position 1 (after system prompt, before user)
	if messages[1].Role != llm.RoleSystem {
		t.Errorf("context should be system role, got %s", messages[1].Role)
	}

	// Inject new context - should replace old one
	conv.InjectContext("Memory context 2")

	messages = conv.GetMessages()
	if len(messages) != 3 {
		t.Errorf("expected 3 messages after re-inject, got %d", len(messages))
	}
}

func TestConversationStore(t *testing.T) {
	store := NewConversationStore(3)

	// Get creates new conversation
	conv1 := store.Get("conv1")
	if conv1 == nil {
		t.Fatal("expected new conversation")
	}

	conv1.AddUserMessage("Hello 1")

	// Get existing conversation
	conv1Again := store.Get("conv1")
	if conv1Again.Len() != 1 {
		t.Error("expected to get same conversation")
	}

	// Add more conversations
	store.Get("conv2")
	store.Get("conv3")

	if store.Size() != 3 {
		t.Errorf("expected 3 conversations, got %d", store.Size())
	}

	// Add one more - should evict oldest (conv1)
	store.Get("conv4")

	if store.Size() != 3 {
		t.Errorf("expected 3 conversations after eviction, got %d", store.Size())
	}

	// conv1 should be evicted
	if store.GetIfExists("conv1") != nil {
		t.Error("conv1 should have been evicted")
	}
}

func TestConversationStoreDelete(t *testing.T) {
	store := NewConversationStore(10)

	store.Get("conv1")
	store.Get("conv2")

	store.Delete("conv1")

	if store.Size() != 1 {
		t.Errorf("expected 1 conversation, got %d", store.Size())
	}

	if store.GetIfExists("conv1") != nil {
		t.Error("conv1 should be deleted")
	}
}

func TestConversationStoreLRU(t *testing.T) {
	store := NewConversationStore(3)

	store.Get("conv1")
	store.Get("conv2")
	store.Get("conv3")

	// Access conv1 to make it most recent
	store.Get("conv1")

	// Add conv4 - should evict conv2 (oldest after LRU update)
	store.Get("conv4")

	if store.GetIfExists("conv2") != nil {
		t.Error("conv2 should have been evicted (LRU)")
	}

	// conv1, conv3, conv4 should still exist
	if store.GetIfExists("conv1") == nil {
		t.Error("conv1 should still exist")
	}
	if store.GetIfExists("conv3") == nil {
		t.Error("conv3 should still exist")
	}
	if store.GetIfExists("conv4") == nil {
		t.Error("conv4 should still exist")
	}
}

func TestGetWindowedMessages(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("System prompt"))

	// Add multiple messages
	conv.AddUserMessage("Original user message")
	conv.AddAssistantMessage("First response")
	conv.AddUserMessage("Follow up")
	conv.AddAssistantMessage("Second response")
	conv.AddUserMessage("Another question")
	conv.AddAssistantMessage("Third response")

	// Test with large budget - should get all messages
	messages := conv.GetWindowedMessages(100000)
	if len(messages) != 7 { // system + 6 messages
		t.Errorf("expected 7 messages with large budget, got %d", len(messages))
	}

	// Test with small budget - should keep system prompt, original user, and recent
	// Using a budget that allows ~100 chars per message
	messages = conv.GetWindowedMessages(200) // Very small budget
	if len(messages) < 2 {
		t.Errorf("expected at least 2 messages with small budget, got %d", len(messages))
	}

	// First message should always be system prompt
	if messages[0].Role != llm.RoleSystem {
		t.Errorf("first message should be system prompt, got %s", messages[0].Role)
	}
}

func TestGetWindowedMessagesPreservesOriginalUser(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("Sys"))

	// Add original user message with identifiable content
	conv.AddUserMessage("ORIGINAL_USER_MESSAGE")
	conv.AddAssistantMessage("Response 1")
	conv.AddUserMessage("Follow up")
	conv.AddAssistantMessage("Response 2")
	conv.AddUserMessage("Another follow up")
	conv.AddAssistantMessage("Response 3")

	// Use medium budget that forces truncation but should preserve original
	messages := conv.GetWindowedMessages(500)

	// Check if original user message is preserved
	hasOriginalUser := false
	for _, msg := range messages {
		if msg.Content == "ORIGINAL_USER_MESSAGE" {
			hasOriginalUser = true
			break
		}
	}

	if !hasOriginalUser {
		t.Error("original user message should be preserved in windowed messages")
	}
}

func TestGetWindowedMessagesZeroBudget(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("System"))
	conv.AddUserMessage("User message")
	conv.AddAssistantMessage("Response")

	// Zero budget should return all messages
	messages := conv.GetWindowedMessages(0)
	if len(messages) != 3 {
		t.Errorf("expected 3 messages with zero budget, got %d", len(messages))
	}
}
