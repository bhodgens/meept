package session

import (
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

func TestAssembleBranch_EmptyInput(t *testing.T) {
	result := AssembleBranch(nil, nil)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}

	result = AssembleBranch([]Message{}, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for empty messages, got %d", len(result))
	}
}

func TestAssembleBranch_SimpleConversation(t *testing.T) {
	messages := []Message{
		{ID: 1, Role: "user", Content: "Hello", EntryType: "message"},
		{ID: 2, Role: "assistant", Content: "Hi there", EntryType: "message"},
		{ID: 3, Role: "user", Content: "How are you?", EntryType: "message"},
		{ID: 4, Role: "assistant", Content: "I'm doing well", EntryType: "message"},
	}

	result := AssembleBranch(messages, nil)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[0].Role != llm.RoleUser || result[0].Content != "Hello" {
		t.Errorf("first message mismatch: role=%s content=%s", result[0].Role, result[0].Content)
	}
	if result[1].Role != llm.RoleAssistant || result[1].Content != "Hi there" {
		t.Errorf("second message mismatch: role=%s content=%s", result[1].Role, result[1].Content)
	}
}

func TestAssembleBranch_WithCompaction(t *testing.T) {
	messages := []Message{
		{ID: 1, Role: "user", Content: "Hello", EntryType: "message"},
		{ID: 2, Role: "assistant", Content: "[Compacted Context] Summary of earlier messages", EntryType: "compaction"},
		{ID: 3, Role: "user", Content: "Follow up", EntryType: "message"},
		{ID: 4, Role: "assistant", Content: "Response", EntryType: "message"},
	}

	result := AssembleBranch(messages, nil)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages (compaction included), got %d", len(result))
	}

	// Compaction entry should be included as a system message with prefix
	if result[1].Role != llm.RoleSystem {
		t.Errorf("compaction entry should have system role, got %s", result[1].Role)
	}
	// Compaction content is passed through as-is (prefix is already in the persisted content)
	if result[1].Content != "[Compacted Context] Summary of earlier messages" {
		t.Errorf("compaction content mismatch: got %q", result[1].Content)
	}

	// Other messages should be unchanged
	if result[0].Role != llm.RoleUser || result[0].Content != "Hello" {
		t.Errorf("first message mismatch: role=%s content=%s", result[0].Role, result[0].Content)
	}
	if result[2].Role != llm.RoleUser || result[2].Content != "Follow up" {
		t.Errorf("third message mismatch: role=%s content=%s", result[2].Role, result[2].Content)
	}
	if result[3].Role != llm.RoleAssistant || result[3].Content != "Response" {
		t.Errorf("fourth message mismatch: role=%s content=%s", result[3].Role, result[3].Content)
	}
}

func TestAssembleBranch_WithSummary(t *testing.T) {
	messages := []Message{
		{ID: 1, Role: "user", Content: "Hello", EntryType: "message"},
		{ID: 2, Role: "system", Content: "Summary of branch", EntryType: "summary"},
		{ID: 3, Role: "user", Content: "Continuing", EntryType: "message"},
	}

	result := AssembleBranch(messages, nil)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages (summary included), got %d", len(result))
	}

	// Summary entry should be included as a system message with prefix
	if result[1].Role != llm.RoleSystem {
		t.Errorf("summary entry should have system role, got %s", result[1].Role)
	}
	expectedPrefix := "[Branch Summary] Summary of branch"
	if result[1].Content != expectedPrefix {
		t.Errorf("summary content mismatch: got %q, want %q", result[1].Content, expectedPrefix)
	}
}

func TestAssembleBranch_WithToolCalls(t *testing.T) {
	messages := []Message{
		{ID: 1, Role: "user", Content: "Read the file", EntryType: "message"},
		{ID: 2, Role: "assistant", Content: "", EntryType: "message"},
		{ID: 3, Role: "tool", Content: "file contents here", EntryType: "message", ToolCallID: "call_123"},
	}

	toolCallsMap := map[int64][]ToolCall{
		2: {
			{MessageID: 2, ToolName: "read_file", ToolCallID: "call_123", Arguments: `{"path":"/tmp/test"}`, Seq: 0},
		},
	}

	result := AssembleBranch(messages, toolCallsMap)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	if len(result[1].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call on assistant message, got %d", len(result[1].ToolCalls))
	}
	if result[1].ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("expected tool name 'read_file', got '%s'", result[1].ToolCalls[0].Function.Name)
	}
	if result[1].ToolCalls[0].ID != "call_123" {
		t.Errorf("expected tool call ID 'call_123', got '%s'", result[1].ToolCalls[0].ID)
	}
	if result[2].ToolCallID != "call_123" {
		t.Errorf("expected tool result with ToolCallID 'call_123', got '%s'", result[2].ToolCallID)
	}
}

func TestAssembleBranch_WithNameField(t *testing.T) {
	messages := []Message{
		{ID: 1, Role: "tool", Content: "result", EntryType: "message", Name: "read_file", ToolCallID: "tc_1"},
	}

	result := AssembleBranch(messages, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Name != "read_file" {
		t.Errorf("expected name 'read_file', got '%s'", result[0].Name)
	}
}

func TestLoadToolCallsForMessages_Empty(t *testing.T) {
	store := NewMemoryStore(slog.Default())
	result, err := LoadToolCallsForMessages(store, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty messages, got %v", result)
	}
}

func TestLoadToolCallsForMessages_WithSQLite(t *testing.T) {
	store, _ := testHelper(t)
	defer store.Close()

	// Create a session
	sess, err := store.Create("test")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Save messages
	msgs := []Message{
		{SessionID: sess.ID, Role: "user", Content: "hello", Timestamp: time.Now(), EntryType: "message"},
		{SessionID: sess.ID, Role: "assistant", Content: "hi", Timestamp: time.Now(), EntryType: "message"},
	}
	if err := store.SaveMessages(sess.ID, msgs); err != nil {
		t.Fatalf("failed to save messages: %v", err)
	}

	// Get messages back to get IDs
	saved, err := store.GetMessages(sess.ID, 0, 10)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(saved) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(saved))
	}

	// Save tool calls for the assistant message
	assistantMsg := saved[1]
	tcs := []ToolCall{
		{MessageID: assistantMsg.ID, ToolName: "test_tool", ToolCallID: "tc_1", Arguments: `{"x":1}`, Seq: 0},
	}
	if err := store.SaveToolCalls(assistantMsg.ID, tcs); err != nil {
		t.Fatalf("failed to save tool calls: %v", err)
	}

	// Load tool calls
	result, err := LoadToolCallsForMessages(store, saved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 message with tool calls, got %d", len(result))
	}
	stored, ok := result[assistantMsg.ID]
	if !ok {
		t.Fatal("expected tool calls for assistant message")
	}
	if len(stored) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(stored))
	}
	if stored[0].ToolName != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got '%s'", stored[0].ToolName)
	}
}

func TestAssembleBranch_Mixed(t *testing.T) {
	// Test a realistic mix of compaction, summary, branch_point, and regular messages
	messages := []Message{
		{ID: 1, Role: "user", Content: "First question", EntryType: "message"},
		{ID: 2, Role: "assistant", Content: "First answer", EntryType: "message"},
		{ID: 3, Role: "assistant", Content: "[Compacted Context] Compacted: early discussion about Go basics", EntryType: "compaction"},
		{ID: 4, Role: "user", Content: "Second question", EntryType: "branch_point"},
		{ID: 5, Role: "system", Content: "Abandoned branch explored error handling", EntryType: "summary"},
		{ID: 6, Role: "user", Content: "Third question", EntryType: "message"},
		{ID: 7, Role: "assistant", Content: "Third answer", EntryType: "message"},
	}

	result := AssembleBranch(messages, nil)
	if len(result) != 7 {
		t.Fatalf("expected 7 messages, got %d", len(result))
	}

	// Verify ordering and types
	tests := []struct {
		idx      int
		role     llm.Role
		contains string
	}{
		{0, llm.RoleUser, "First question"},
		{1, llm.RoleAssistant, "First answer"},
		{2, llm.RoleSystem, "[Compacted Context] Compacted: early discussion about Go basics"},
		{3, llm.RoleUser, "Second question"},
		{4, llm.RoleSystem, "[Branch Summary] Abandoned branch explored error handling"},
		{5, llm.RoleUser, "Third question"},
		{6, llm.RoleAssistant, "Third answer"},
	}

	for _, tt := range tests {
		if result[tt.idx].Role != tt.role {
			t.Errorf("message %d: expected role %s, got %s", tt.idx, tt.role, result[tt.idx].Role)
		}
		if result[tt.idx].Content != tt.contains {
			t.Errorf("message %d: expected content %q, got %q", tt.idx, tt.contains, result[tt.idx].Content)
		}
	}
}

func TestAssembleBranch_CompactionPreservesOrder(t *testing.T) {
	// Multiple compaction entries should maintain their relative order
	messages := []Message{
		{ID: 1, Role: "assistant", Content: "[Compacted Context] Early compaction", EntryType: "compaction"},
		{ID: 2, Role: "user", Content: "question 1", EntryType: "message"},
		{ID: 3, Role: "assistant", Content: "answer 1", EntryType: "message"},
		{ID: 4, Role: "assistant", Content: "[Compacted Context] Later compaction", EntryType: "compaction"},
		{ID: 5, Role: "user", Content: "question 2", EntryType: "message"},
	}

	result := AssembleBranch(messages, nil)
	if len(result) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(result))
	}

	// First compaction (content passed through as-is)
	if result[0].Role != llm.RoleSystem || result[0].Content != "[Compacted Context] Early compaction" {
		t.Errorf("first compaction mismatch: role=%s content=%s", result[0].Role, result[0].Content)
	}
	// Second compaction
	if result[3].Role != llm.RoleSystem || result[3].Content != "[Compacted Context] Later compaction" {
		t.Errorf("second compaction mismatch: role=%s content=%s", result[3].Role, result[3].Content)
	}
}
