package agent

import (
	"strings"
	"testing"
)

func TestTurnCompactor_CompactToolCalls(t *testing.T) {
	tc := NewTurnCompactor()
	turns := []Turn{
		{Type: "tool_call", ToolName: "view_trace", ToolInput: map[string]any{"trace_id": "123"}, TokenCount: 50},
		{Type: "tool_response", Content: "trace data here", TokenCount: 200},
	}

	compacted := tc.CompactTurns(turns)

	if len(compacted) != 1 {
		t.Fatalf("expected 1 compacted turn, got %d", len(compacted))
	}

	ct := compacted[0]
	if ct.Type != "tool" {
		t.Errorf("expected type 'tool', got %q", ct.Type)
	}
	if ct.ToolName != "view_trace" {
		t.Errorf("expected tool 'view_trace', got %q", ct.ToolName)
	}
	if ct.ToolOutput != "trace data here" {
		t.Errorf("expected output 'trace data here', got %q", ct.ToolOutput)
	}
	if ct.TokenCount != 250 {
		t.Errorf("expected 250 tokens, got %d", ct.TokenCount)
	}
}

func TestTurnCompactor_CollapseThinking(t *testing.T) {
	tc := NewTurnCompactor()
	turns := []Turn{
		{Type: "thinking", Content: "thinking 1", TokenCount: 10},
		{Type: "thinking", Content: "thinking 2", TokenCount: 10},
		{Type: "thinking", Content: "thinking 3", TokenCount: 10},
		{Type: "thinking", Content: "thinking 4", TokenCount: 10}, // 4th turn -> collapse
		{Type: "final", Content: "final answer", TokenCount: 50},
	}

	compacted := tc.CompactTurns(turns)

	// Should have 2 turns: 1 collapsed thinking + 1 final.
	if len(compacted) != 2 {
		t.Fatalf("expected 2 compacted turns, got %d: %#v", len(compacted), compacted)
	}

	if compacted[0].Type != "thinking" {
		t.Errorf("expected first turn to be 'thinking', got %q", compacted[0].Type)
	}
	if compacted[1].Type != "final" {
		t.Errorf("expected second turn to be 'final', got %q", compacted[1].Type)
	}
	// Verify collapse markers are present.
	if compacted[0].Thinking == "" {
		t.Error("expected collapsed thinking to be non-empty")
	}
	if !strings.Contains(compacted[0].Thinking, "[...]") {
		t.Error("expected collapse marker [...] in thinking")
	}
}

func TestTurnCompactor_EmptyInput(t *testing.T) {
	tc := NewTurnCompactor()
	compacted := tc.CompactTurns([]Turn{})
	if len(compacted) != 0 {
		t.Errorf("expected empty result for empty input, got %d turns", len(compacted))
	}
}

func TestTurnCompactor_ToolCallWithoutResponse(t *testing.T) {
	tc := NewTurnCompactor()
	turns := []Turn{
		{Type: "tool_call", ToolName: "view_trace", ToolInput: map[string]any{"trace_id": "123"}, TokenCount: 50},
		// No tool_response - should be dropped
		{Type: "final", Content: "final answer", TokenCount: 50},
	}

	compacted := tc.CompactTurns(turns)

	// Should have 1 turn: just the final (tool_call without response is dropped)
	if len(compacted) != 1 {
		t.Fatalf("expected 1 compacted turn, got %d", len(compacted))
	}
	if compacted[0].Type != "final" {
		t.Errorf("expected final turn, got %q", compacted[0].Type)
	}
}

func TestTurnCompactor_ThinkingFlush(t *testing.T) {
	tc := NewTurnCompactor()
	turns := []Turn{
		{Type: "thinking", Content: "thinking 1", TokenCount: 10},
		{Type: "thinking", Content: "thinking 2", TokenCount: 10},
		{Type: "final", Content: "final answer", TokenCount: 50},
	}

	compacted := tc.CompactTurns(turns)

	// Should have 2 turns: 1 thinking (not collapsed, just flushed) + 1 final
	if len(compacted) != 2 {
		t.Fatalf("expected 2 compacted turns, got %d", len(compacted))
	}
	// Both thinking turns should be present (not collapsed since <= 2)
	if !strings.Contains(compacted[0].Thinking, "thinking 1") {
		t.Error("expected 'thinking 1' in output")
	}
	if !strings.Contains(compacted[0].Thinking, "thinking 2") {
		t.Error("expected 'thinking 2' in output")
	}
}
