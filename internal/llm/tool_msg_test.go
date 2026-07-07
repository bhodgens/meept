package llm

import (
	"testing"
)

// TestToOpenAIDict_ToolMessageNoToolCalls verifies that tool response messages
// (role="tool") do NOT include tool_calls - only tool_call_id.
// This matches the OpenAI API spec and prevents Qwen error 1214.
func TestToOpenAIDict_ToolMessageNoToolCalls(t *testing.T) {
	msg := &ChatMessage{
		Role:       RoleTool,
		Content:    "function result",
		ToolCallID: "call_abc123",
		ToolCalls: []ToolCall{
			{
				ID:   "call_abc123",
				Type: "function",
				Function: ToolCallFunction{
					Name:      "test_func",
					Arguments: `{"arg": "value"}`,
				},
			},
		},
	}

	dict := msg.ToOpenAIDict()

	// Verify role and content
	if dict["role"] != "tool" {
		t.Errorf("expected role='tool', got %v", dict["role"])
	}
	if dict["content"] != "function result" {
		t.Errorf("expected content='function result', got %v", dict["content"])
	}
	if dict["tool_call_id"] != "call_abc123" {
		t.Errorf("expected tool_call_id='call_abc123', got %v", dict["tool_call_id"])
	}

	// CRITICAL: tool_calls should NOT be present for role="tool" messages
	if _, hasToolCalls := dict["tool_calls"]; hasToolCalls {
		t.Error("tool_calls should NOT be present for role='tool' messages - this causes Qwen error 1214")
	}
}

// TestToOpenAIDict_AssistantMessageWithToolCalls verifies that assistant messages
// with tool calls DO include tool_calls as expected.
func TestToOpenAIDict_AssistantMessageWithToolCalls(t *testing.T) {
	msg := &ChatMessage{
		Role:    RoleAssistant,
		Content: "I'll call the function",
		ToolCalls: []ToolCall{
			{
				ID:   "call_xyz789",
				Type: "function",
				Function: ToolCallFunction{
					Name:      "test_func",
					Arguments: `{"arg": "value"}`,
				},
			},
		},
	}

	dict := msg.ToOpenAIDict()

	// Verify role and content
	if dict["role"] != "assistant" {
		t.Errorf("expected role='assistant', got %v", dict["role"])
	}
	if dict["content"] != "I'll call the function" {
		t.Errorf("expected content='I'll call the function', got %v", dict["content"])
	}

	// tool_calls SHOULD be present for assistant messages
	toolCalls, ok := dict["tool_calls"].([]map[string]any)
	if !ok {
		t.Fatal("tool_calls should be present for assistant messages with tool calls")
	}
	if len(toolCalls) != 1 {
		t.Errorf("expected 1 tool_call, got %d", len(toolCalls))
	}
}

func TestToOpenAIDict_AssistantToolCallsEmptyContent(t *testing.T) {
	msg := &ChatMessage{
		Role:    RoleAssistant,
		Content: "",
		ToolCalls: []ToolCall{
			{ID: "call_empty", Type: "function", Function: ToolCallFunction{Name: "test_func", Arguments: "{}"}},
		},
	}
	dict := msg.ToOpenAIDict()
	if _, hasContent := dict["content"]; hasContent {
		t.Errorf("content key should be absent for assistant+tool_calls+empty content, got %v", dict["content"])
	}
	if _, ok := dict["tool_calls"].([]map[string]any); !ok {
		t.Error("tool_calls should be present")
	}
}

func TestToOpenAIDict_EmptyContentNoToolCalls(t *testing.T) {
	msg := &ChatMessage{Role: RoleUser, Content: ""}
	dict := msg.ToOpenAIDict()
	content, hasContent := dict["content"]
	if !hasContent {
		t.Fatal("content key should be present as nil")
	}
	if content != nil {
		t.Errorf("expected nil content, got %v", content)
	}
}
