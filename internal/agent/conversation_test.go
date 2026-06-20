package agent

import (
	"fmt"
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
	for range 10 {
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
	for range 10 {
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

func TestRestoreFromMessages_Empty(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("System prompt"))
	conv.AddUserMessage("Should be replaced")
	conv.RestoreFromMessages(nil)

	if conv.Len() != 0 {
		t.Errorf("expected 0 messages after restore with nil, got %d", conv.Len())
	}

	// System prompt should be preserved
	messages := conv.GetMessages()
	if len(messages) != 1 || messages[0].Role != llm.RoleSystem {
		t.Error("system prompt should be preserved after restore")
	}
}

func TestRestoreFromMessages_Basic(t *testing.T) {
	conv := NewConversation(WithSystemPrompt("System prompt"))

	restoreMsgs := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "Restored user message"},
		{Role: llm.RoleAssistant, Content: "Restored assistant message"},
		{Role: llm.RoleUser, Content: "Another question"},
	}
	conv.RestoreFromMessages(restoreMsgs)

	if conv.Len() != 3 {
		t.Fatalf("expected 3 messages, got %d", conv.Len())
	}

	// Verify messages are as expected (not the original)
	messages := conv.GetMessages()
	// messages[0] is system prompt, messages[1..3] are restored
	if messages[1].Content != "Restored user message" {
		t.Errorf("expected restored user message at index 1, got '%s'", messages[1].Content)
	}
}

func TestRestoreFromMessages_WithToolCalls(t *testing.T) {
	conv := NewConversation()

	toolCalls := []llm.ToolCall{
		{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "search", Arguments: `{"q":"test"}`}},
	}
	restoreMsgs := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "Search for test"},
		{Role: llm.RoleAssistant, Content: "", ToolCalls: toolCalls},
		{Role: llm.RoleTool, Content: "search results", ToolCallID: "call_1"},
	}
	conv.RestoreFromMessages(restoreMsgs)

	if conv.Len() != 3 {
		t.Fatalf("expected 3 messages, got %d", conv.Len())
	}

	messages := conv.GetMessages()
	if len(messages[1].ToolCalls) != 1 {
		t.Errorf("expected 1 tool call on assistant message, got %d", len(messages[1].ToolCalls))
	}
	if messages[2].ToolCallID != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got '%s'", messages[2].ToolCallID)
	}
}

func TestRestoreFromMessages_Classification(t *testing.T) {
	conv := NewConversation()

	restoreMsgs := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "Hello"},
		{Role: llm.RoleAssistant, Content: "In summary, this is a conclusion"},
	}
	conv.RestoreFromMessages(restoreMsgs)

	if len(conv.messageTypes) != 2 {
		t.Fatalf("expected 2 message types, got %d", len(conv.messageTypes))
	}
	if conv.messageTypes[0] != MessageUserInput {
		t.Errorf("expected first message classified as UserInput, got %d", conv.messageTypes[0])
	}
	if conv.messageTypes[1] != MessageAssistantConclusion {
		t.Errorf("expected second message classified as Conclusion, got %d", conv.messageTypes[1])
	}
}

func TestConversationStoreGetOrRestore_CacheHit(t *testing.T) {
	store := NewConversationStore(10)

	// Pre-populate
	conv := store.Get("conv-1")
	conv.AddUserMessage("Hello")

	// GetOrRestore should return the cached conversation
	restored, err := store.GetOrRestore("conv-1", func() ([]llm.ChatMessage, error) {
		t.Error("restore function should not be called on cache hit")
		return nil, nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if restored.Len() != 1 {
		t.Errorf("expected 1 message, got %d", restored.Len())
	}
}

func TestConversationStoreGetOrRestore_RestoreSuccess(t *testing.T) {
	store := NewConversationStore(10)

	restoreMsgs := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "Restored message"},
		{Role: llm.RoleAssistant, Content: "Restored response"},
	}

	conv, err := store.GetOrRestore("conv-1", func() ([]llm.ChatMessage, error) {
		return restoreMsgs, nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if conv.Len() != 2 {
		t.Errorf("expected 2 restored messages, got %d", conv.Len())
	}

	// Subsequent Get should return the same cached conversation
	conv2 := store.Get("conv-1")
	if conv2.Len() != 2 {
		t.Errorf("expected 2 messages from cache, got %d", conv2.Len())
	}
}

func TestConversationStoreGetOrRestore_RestoreFailure(t *testing.T) {
	store := NewConversationStore(10)

	conv, err := store.GetOrRestore("conv-1", func() ([]llm.ChatMessage, error) {
		return nil, fmt.Errorf("store unavailable")
	})
	if err == nil {
		t.Error("expected error from restore failure")
	}
	// Should still return an empty conversation
	if conv == nil {
		t.Fatal("expected non-nil conversation even on restore failure")
	}
	if conv.Len() != 0 {
		t.Errorf("expected empty conversation on restore failure, got %d messages", conv.Len())
	}
}

func TestConversationStoreWithPersistence(t *testing.T) {
	var persistedID string

	store := NewConversationStore(10, WithPersistence(func(conversationID string, messages []llm.ChatMessage) error {
		persistedID = conversationID
		return nil
	}))

	if store.persistFn == nil {
		t.Error("expected persistFn to be set")
	}

	// Verify the function works
	err := store.persistFn("test-conv", []llm.ChatMessage{{Role: llm.RoleUser, Content: "test"}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if persistedID != "test-conv" {
		t.Errorf("expected persistedID 'test-conv', got '%s'", persistedID)
	}
}

func TestConversationStoreSetPersistence(t *testing.T) {
	store := NewConversationStore(10)

	if store.persistFn != nil {
		t.Error("expected nil persistFn initially")
	}

	called := false
	store.SetPersistence(func(conversationID string, messages []llm.ChatMessage) error {
		called = true
		return nil
	})

	if store.persistFn == nil {
		t.Error("expected persistFn to be set after SetPersistence")
	}

	_ = store.persistFn("test", nil)
	if !called {
		t.Error("expected persistFn to be called")
	}
}

func TestConversationStoreGetOrRestore_Eviction(t *testing.T) {
	store := NewConversationStore(2)

	// Fill up store
	_, _ = store.GetOrRestore("conv-1", func() ([]llm.ChatMessage, error) {
		return []llm.ChatMessage{{Role: llm.RoleUser, Content: "msg1"}}, nil
	})
	_, _ = store.GetOrRestore("conv-2", func() ([]llm.ChatMessage, error) {
		return []llm.ChatMessage{{Role: llm.RoleUser, Content: "msg2"}}, nil
	})

	if store.Size() != 2 {
		t.Fatalf("expected 2 conversations, got %d", store.Size())
	}

	// Add one more - should evict oldest
	_, _ = store.GetOrRestore("conv-3", func() ([]llm.ChatMessage, error) {
		return []llm.ChatMessage{{Role: llm.RoleUser, Content: "msg3"}}, nil
	})

	if store.Size() != 2 {
		t.Errorf("expected 2 conversations after eviction, got %d", store.Size())
	}
	if store.GetIfExists("conv-1") != nil {
		t.Error("conv-1 should have been evicted")
	}
}

func TestSetBranchPoint(t *testing.T) {
	t.Run("basic truncation", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("first")
		conv.AddAssistantMessage("second")
		conv.AddUserMessage("third")
		conv.AddAssistantMessage("fourth")

		// Branch at index 1 (keeps "first" and "second")
		conv.SetBranchPoint(1)

		if conv.Len() != 2 {
			t.Fatalf("expected 2 messages after branch, got %d", conv.Len())
		}
		msgs := conv.GetMessages()
		if msgs[0].Content != "first" {
			t.Errorf("expected first message 'first', got %q", msgs[0].Content)
		}
		if msgs[1].Content != "second" {
			t.Errorf("expected second message 'second', got %q", msgs[1].Content)
		}
	})

	t.Run("preserves messageTypes sync", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("a")
		conv.AddAssistantMessage("b")
		conv.AddUserMessage("c")
		conv.AddAssistantMessage("d")
		conv.AddUserMessage("e")

		conv.SetBranchPoint(2)

		if conv.Len() != 3 {
			t.Fatalf("expected 3 messages, got %d", conv.Len())
		}
		// Verify messageTypes slice is same length as messages
		conv.mu.RLock()
		msgCount := len(conv.messages)
		typeCount := len(conv.messageTypes)
		conv.mu.RUnlock()
		if msgCount != typeCount {
			t.Errorf("messageTypes (%d) out of sync with messages (%d)", typeCount, msgCount)
		}
	})

	t.Run("negative index", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("a")
		conv.AddAssistantMessage("b")

		conv.SetBranchPoint(-1)

		if conv.Len() != 2 {
			t.Errorf("negative index should be no-op, expected 2 messages, got %d", conv.Len())
		}
	})

	t.Run("index beyond length", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("a")
		conv.AddAssistantMessage("b")

		conv.SetBranchPoint(10)

		if conv.Len() != 2 {
			t.Errorf("index >= len should be no-op, expected 2 messages, got %d", conv.Len())
		}
	})

	t.Run("branch at last message", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("a")
		conv.AddAssistantMessage("b")

		conv.SetBranchPoint(1)

		if conv.Len() != 2 {
			t.Errorf("branching at last message should be no-op, expected 2, got %d", conv.Len())
		}
	})

	t.Run("branch at first message", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("a")
		conv.AddAssistantMessage("b")
		conv.AddUserMessage("c")

		conv.SetBranchPoint(0)

		if conv.Len() != 1 {
			t.Fatalf("expected 1 message after branch at 0, got %d", conv.Len())
		}
		msgs := conv.GetMessages()
		if msgs[0].Content != "a" {
			t.Errorf("expected message 'a', got %q", msgs[0].Content)
		}
	})

	t.Run("empty conversation", func(t *testing.T) {
		conv := NewConversation()

		conv.SetBranchPoint(0)

		if conv.Len() != 0 {
			t.Errorf("branch on empty conversation should be no-op, got %d", conv.Len())
		}
	})
}

func TestFindMessageByContent(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("hello world")
		conv.AddAssistantMessage("hi there")
		conv.AddUserMessage("what is 2+2?")

		idx := conv.FindMessageByContent("hello")
		if idx != 0 {
			t.Errorf("expected index 0, got %d", idx)
		}

		idx = conv.FindMessageByContent("hi there")
		if idx != 1 {
			t.Errorf("expected index 1, got %d", idx)
		}

		idx = conv.FindMessageByContent("what is")
		if idx != 2 {
			t.Errorf("expected index 2, got %d", idx)
		}
	})

	t.Run("not found", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("hello")

		idx := conv.FindMessageByContent("goodbye")
		if idx != -1 {
			t.Errorf("expected -1 for not found, got %d", idx)
		}
	})

	t.Run("empty conversation", func(t *testing.T) {
		conv := NewConversation()

		idx := conv.FindMessageByContent("anything")
		if idx != -1 {
			t.Errorf("expected -1 for empty conversation, got %d", idx)
		}
	})

	t.Run("exact prefix match required", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("hello world")

		// Partial non-prefix should not match
		idx := conv.FindMessageByContent("world")
		if idx != -1 {
			t.Errorf("non-prefix should not match, got index %d", idx)
		}

		// Exact full content should match
		idx = conv.FindMessageByContent("hello world")
		if idx != 0 {
			t.Errorf("exact content should match at 0, got %d", idx)
		}
	})
}

func TestGetCompactionCandidates(t *testing.T) {
	t.Run("empty conversation", func(t *testing.T) {
		conv := NewConversation()
		candidates, report := conv.GetCompactionCandidates(0.6)
		if len(candidates) != 0 {
			t.Errorf("expected no candidates for empty conversation, got %d", len(candidates))
		}
		if report.MessagesBefore != 0 {
			t.Errorf("expected 0 messages before, got %d", report.MessagesBefore)
		}
	})

	t.Run("below threshold - no compression needed", func(t *testing.T) {
		conv := NewConversation()
		// Use a 1.0 target ratio so nothing needs to be compressed
		conv.AddUserMessage("hello")
		conv.AddAssistantMessage("hi there")

		candidates, report := conv.GetCompactionCandidates(1.0)
		if len(candidates) != 0 {
			t.Errorf("expected no candidates at 1.0 ratio, got %d", len(candidates))
		}
		if report.TokensRemoved != 0 {
			t.Errorf("expected 0 tokens removed, got %d", report.TokensRemoved)
		}
	})

	t.Run("returns low-importance candidates", func(t *testing.T) {
		conv := NewConversation()

		// Add a user message (ImportanceCritical - should never be removed)
		conv.AddUserMessage("important user query")

		// Add several assistant messages with reasoning content (low importance)
		for i := range 10 {
			conv.AddAssistantMessage(fmt.Sprintf("let me think about step %d, considering various options and analyzing the data", i))
		}

		// Add a high-importance conclusion
		conv.AddAssistantMessage("in conclusion, the final answer is 42. summary: done.")

		candidates, report := conv.GetCompactionCandidates(0.5)
		if len(candidates) == 0 {
			t.Fatal("expected some candidates for large conversation")
		}
		if report.MessagesBefore != 12 {
			t.Errorf("expected 12 messages before, got %d", report.MessagesBefore)
		}
		if report.TokensBefore <= report.TokensAfter {
			t.Errorf("expected tokens before (%d) > tokens after (%d)", report.TokensBefore, report.TokensAfter)
		}
		if report.TokensRemoved <= 0 {
			t.Errorf("expected positive tokens removed, got %d", report.TokensRemoved)
		}

		// Verify the user message (index 0) is not among candidates
		for _, idx := range candidates {
			if idx == 0 {
				t.Error("user message (index 0) should not be a compaction candidate")
			}
		}
	})

	t.Run("preserves anchor messages", func(t *testing.T) {
		conv := NewConversation()

		conv.AddUserMessage("query")

		// Add anchor message
		conv.AddAnchorMessage(llm.RoleSystem, "critical validation instruction")

		// Add lots of reasoning content
		for i := range 10 {
			conv.AddAssistantMessage(fmt.Sprintf("reasoning step %d with lots of content that makes it low importance due to patterns like let me think", i))
		}

		conv.AddAssistantMessage("final conclusion summary done")

		candidates, _ := conv.GetCompactionCandidates(0.5)

		// Index 1 is the anchor message, it should never be a candidate
		for _, idx := range candidates {
			if idx == 1 {
				t.Error("anchor message (index 1) should not be a compaction candidate")
			}
		}
	})

	t.Run("does not modify conversation state", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("query")
		for i := range 10 {
			conv.AddAssistantMessage(fmt.Sprintf("reasoning step %d let me think about this carefully", i))
		}
		conv.AddAssistantMessage("in conclusion, done. summary:")

		originalLen := conv.Len()
		originalMsgs := conv.GetMessages()

		conv.GetCompactionCandidates(0.5)

		if conv.Len() != originalLen {
			t.Errorf("GetCompactionCandidates modified conversation length: was %d, now %d", originalLen, conv.Len())
		}

		afterMsgs := conv.GetMessages()
		if len(afterMsgs) != len(originalMsgs) {
			t.Errorf("GetCompactionCandidates changed message count: was %d, now %d", len(originalMsgs), len(afterMsgs))
		}
	})
}

func TestRemoveCompactedMessages(t *testing.T) {
	t.Run("empty indices", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("hello")
		conv.AddAssistantMessage("hi")

		saved := conv.RemoveCompactedMessages(nil)
		if saved != 0 {
			t.Errorf("expected 0 tokens saved for empty indices, got %d", saved)
		}
		if conv.Len() != 2 {
			t.Errorf("expected 2 messages, got %d", conv.Len())
		}
	})

	t.Run("removes specified indices", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("user query")           // index 0
		conv.AddAssistantMessage("reasoning step")  // index 1 (to remove)
		conv.AddAssistantMessage("another thought") // index 2 (to remove)
		conv.AddAssistantMessage("final answer")    // index 3

		saved := conv.RemoveCompactedMessages([]int{1, 2})
		if saved <= 0 {
			t.Errorf("expected positive tokens saved, got %d", saved)
		}
		if conv.Len() != 2 {
			t.Fatalf("expected 2 messages after removal, got %d", conv.Len())
		}

		msgs := conv.GetMessages()
		// First non-system message should be user query
		if msgs[0].Role != llm.RoleUser || msgs[0].Content != "user query" {
			t.Errorf("expected first message to be 'user query', got %s: %s", msgs[0].Role, msgs[0].Content)
		}
		// Second should be final answer
		if msgs[1].Role != llm.RoleAssistant || msgs[1].Content != "final answer" {
			t.Errorf("expected second message to be 'final answer', got %s: %s", msgs[1].Role, msgs[1].Content)
		}
	})

	t.Run("syncs messageTypes slice", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("query")                              // MessageUserInput
		conv.AddAssistantMessage("let me think")                  // MessageReasoningStep (to remove)
		conv.AddAssistantMessage("in conclusion, done. summary:") // MessageAssistantConclusion

		conv.RemoveCompactedMessages([]int{1})

		if conv.Len() != 2 {
			t.Fatalf("expected 2 messages, got %d", conv.Len())
		}

		// Verify messageTypes is still aligned by running GetCompactionCandidates
		// which reads messageTypes -- if misaligned it would panic
		candidates, _ := conv.GetCompactionCandidates(0.5)
		_ = candidates
	})

	t.Run("out of range indices are ignored", func(t *testing.T) {
		conv := NewConversation()
		conv.AddUserMessage("hello")
		conv.AddAssistantMessage("world")

		saved := conv.RemoveCompactedMessages([]int{-1, 100, 500})
		if saved != 0 {
			t.Errorf("expected 0 tokens saved for out-of-range indices, got %d", saved)
		}
		if conv.Len() != 2 {
			t.Errorf("expected 2 messages unchanged, got %d", conv.Len())
		}
	})

	t.Run("empty conversation", func(t *testing.T) {
		conv := NewConversation()
		saved := conv.RemoveCompactedMessages([]int{0, 1})
		if saved != 0 {
			t.Errorf("expected 0 for empty conversation, got %d", saved)
		}
	})

	t.Run("full compaction flow: candidates then remove", func(t *testing.T) {
		conv := NewConversation()

		// Build a conversation with 50+ messages
		conv.AddUserMessage("important user query")
		for i := range 50 {
			conv.AddAssistantMessage(fmt.Sprintf("let me think about reasoning step %d considering various options and analyzing the data thoroughly", i))
		}
		conv.AddAssistantMessage("in conclusion, the final answer is 42. summary: done.")

		originalLen := conv.Len()

		// Get candidates
		candidates, report := conv.GetCompactionCandidates(0.6)
		if len(candidates) == 0 {
			t.Fatal("expected candidates for 52-message conversation")
		}
		if report.MessagesBefore != originalLen {
			t.Errorf("expected %d messages before, got %d", originalLen, report.MessagesBefore)
		}

		// Remove them
		saved := conv.RemoveCompactedMessages(candidates)
		if saved <= 0 {
			t.Error("expected positive tokens saved")
		}

		remaining := conv.Len()
		if remaining >= originalLen {
			t.Errorf("expected fewer messages after removal: was %d, now %d", originalLen, remaining)
		}
		if remaining != report.MessagesAfter {
			t.Errorf("expected %d messages after (from report), got %d", report.MessagesAfter, remaining)
		}
	})
}

// TestAddAnchorMessage_KeepsMessageTypesAligned is a regression test for a
// bug where AddAnchorMessage appended to c.messages but not c.messageTypes,
// causing length divergence. Downstream compaction/importance code iterates
// the two slices in parallel and silently mistreats anchored messages when
// they are unaligned.
func TestAddAnchorMessage_KeepsMessageTypesAligned(t *testing.T) {
	conv := NewConversation()

	conv.AddAnchorMessage(llm.RoleSystem, "validation: do not skip tests")
	conv.AddUserMessage("hello")

	conv.mu.RLock()
	msgLen := len(conv.messages)
	typeLen := len(conv.messageTypes)
	firstType := conv.messageTypes[0]
	conv.mu.RUnlock()

	if msgLen != typeLen {
		t.Fatalf("messages/types length diverged: messages=%d types=%d", msgLen, typeLen)
	}
	if firstType != MessageAnchor {
		t.Errorf("expected first classification to be MessageAnchor, got %v", firstType)
	}
	if got := getMessageImportance(firstType); got != ImportanceCritical {
		t.Errorf("expected anchored message to have ImportanceCritical, got %v", got)
	}
}

// TestAddAnchorMessage_DoesNotClaimFirstUserMessage verifies that adding an
// anchor before the first user message does not cause the first user message
// to be misclassified (the previous bug set len(messages) > 1 so the
// isFirstUserMsg check in AddMessage was always false after an anchor).
func TestAddAnchorMessage_DoesNotClaimFirstUserMessage(t *testing.T) {
	conv := NewConversation()
	conv.AddAnchorMessage(llm.RoleSystem, "anchor")
	conv.AddUserMessage("first real user message")

	conv.mu.RLock()
	userType := conv.messageTypes[1]
	conv.mu.RUnlock()

	if userType != MessageUserInput {
		t.Errorf("expected first user message after anchor to be MessageUserInput, got %v", userType)
	}
}
