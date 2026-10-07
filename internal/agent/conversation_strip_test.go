package agent

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestConversationStripThinkingOnAdd pins the Conversation add-path choke
// point: every assistant message's Content passes through llm.StripThinking
// before append, so inline <think> reasoning never persists in multi-turn
// history. Tool call arguments are NOT stripped (only the Content field).

func TestConversationAddAssistantMessageStripsThinkBlocks(t *testing.T) {
	conv := NewConversation()
	conv.AddAssistantMessage("<think>reasoning</think>Final answer.")

	msgs := conv.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := msgs[0].Content; got != "Final answer." {
		t.Errorf("content not stripped: got %q, want %q", got, "Final answer.")
	}
	if msgs[0].Role != llm.RoleAssistant {
		t.Errorf("role changed: got %q", msgs[0].Role)
	}
}

func TestConversationAddAssistantMessageStripsUnclosedThink(t *testing.T) {
	conv := NewConversation()
	conv.AddAssistantMessage("<think>partial")

	msgs := conv.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := msgs[0].Content; got != "" {
		t.Errorf("unclosed think block not stripped: got %q, want empty", got)
	}
}

func TestConversationAddAssistantMessageStripsReasoningContentFragment(t *testing.T) {
	conv := NewConversation()
	conv.AddAssistantMessage(`"reasoning_content": "hidden chain of thought"` + "\nVisible answer.")

	msgs := conv.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := msgs[0].Content; got != "Visible answer." {
		t.Errorf("reasoning_content fragment not stripped: got %q", got)
	}
}

func TestConversationAddAssistantMessageWithToolCallsStripsContentKeepsCalls(t *testing.T) {
	conv := NewConversation()
	args := `{"path": "<think>not reasoning</think>/tmp/file.txt"}`
	calls := []llm.ToolCall{
		{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: args}},
	}
	conv.AddAssistantMessageWithToolCalls("<think>r</think>Calling.", calls)

	msgs := conv.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := msgs[0].Content; got != "Calling." {
		t.Errorf("content not stripped: got %q, want %q", got, "Calling.")
	}
	if len(msgs[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msgs[0].ToolCalls))
	}
	// The arguments JSON must survive byte-identical — reasoning-shaped
	// prose inside an argument string is data, not reasoning.
	if got := msgs[0].ToolCalls[0].Function.Arguments; got != args {
		t.Errorf("tool call arguments mutated: got %q, want %q", got, args)
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(msgs[0].ToolCalls[0].Function.Arguments), &parsed); err != nil {
		t.Fatalf("arguments no longer valid JSON: %v", err)
	}
}

func TestConversationAddAssistantMessagePlainProseByteIdentical(t *testing.T) {
	conv := NewConversation()
	prose := "The answer is 42, computed via careful analysis."
	conv.AddAssistantMessage(prose)

	msgs := conv.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := msgs[0].Content; got != prose {
		t.Errorf("plain prose altered: got %q, want %q", got, prose)
	}
}

// Empty-after-strip content stays an empty-string message (it may carry
// ToolCalls downstream); the message is never dropped.
func TestConversationAddAssistantMessageEmptyAfterStripKeepsMessage(t *testing.T) {
	conv := NewConversation()
	conv.AddAssistantMessage("<think>all reasoning, no answer</think>")

	msgs := conv.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected message to be kept, got %d messages", len(msgs))
	}
	if msgs[0].Content != "" {
		t.Errorf("expected empty content, got %q", msgs[0].Content)
	}
}

// History-build check: the message slice built for the LLM shows only
// stripped content — mixed reasoning and clean turns stay clean.
func TestConversationHistoryForLLMShowsOnlyStrippedContent(t *testing.T) {
	conv := NewConversation()
	conv.AddUserMessage("hello")
	conv.AddAssistantMessage("<think>deepseek reasoning</think>World.")
	conv.AddUserMessage("again")
	conv.AddAssistantMessage("Clean turn.")

	msgs := conv.GetMessages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	for i, msg := range msgs {
		if msg.Role != llm.RoleAssistant {
			continue
		}
		if got := msg.Content; got != "World." && got != "Clean turn." {
			t.Errorf("message %d carries unstripped content: %q", i, got)
		}
		if containsThinkTag(msg.Content) {
			t.Errorf("message %d contains think tag: %q", i, msg.Content)
		}
	}
}

// Restore path: legacy persisted history that already carries inline
// reasoning is stripped once at RestoreFromMessages, so a reloaded
// Conversation never replays reasoning either.
func TestConversationRestoreFromMessagesStripsAssistantContent(t *testing.T) {
	conv := NewConversation()
	conv.RestoreFromMessages([]llm.ChatMessage{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, Content: "<think>old reasoning</think>Restored answer."},
		{Role: llm.RoleAssistant, Content: "", ToolCalls: []llm.ToolCall{
			{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"x"}`}},
		}},
	})

	msgs := conv.GetMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if got := msgs[1].Content; got != "Restored answer." {
		t.Errorf("restore did not strip: got %q", got)
	}
	if len(msgs[2].ToolCalls) != 1 {
		t.Errorf("tool calls lost on restore")
	}
}

func containsThinkTag(s string) bool {
	return len(s) >= 7 && (stringContains(s, "<think>") || stringContains(s, "</think>"))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
