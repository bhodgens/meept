package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

// mlx_lm 0.31.3 (LFM2.5-8B-A1B-MLX-4bit) returns the whole assistant reply
// in a `reasoning` field with `content` absent — verified by direct probe
// 2026-09-11. meept only understood `reasoning_content` (DeepSeek/o1), so
// every mlx_lm reply read as an EMPTY response and the loop burned its
// nudge ladder to "stopped after extended thinking".

func TestReasoningText_PrefersReasoningContent(t *testing.T) {
	m := &ResponseMessage{
		ReasoningContent: "deepseek style",
		Reasoning:        "mlx style",
	}
	if got := m.ReasoningText(); got != "deepseek style" {
		t.Errorf("ReasoningText() = %q, want the reasoning_content value", got)
	}
}

func TestReasoningText_FallsBackToReasoning(t *testing.T) {
	m := &ResponseMessage{Reasoning: "mlx style"}
	if got := m.ReasoningText(); got != "mlx style" {
		t.Errorf("ReasoningText() = %q, want the reasoning alias value", got)
	}
}

func TestParseResponse_ReadsMlxReasoningField(t *testing.T) {
	raw := `{
		"model": "LFM2.5-8B-A1B-MLX-4bit",
		"choices": [{
			"index": 0,
			"finish_reason": "stop",
			"message": {"role": "assistant", "reasoning": "thinking hard about the answer"}
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`
	var chatResp ChatResponse
	if err := json.Unmarshal([]byte(raw), &chatResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	client := NewClient(&ModelConfig{ModelID: "mlx-test"})

	// The whole reply lives in `reasoning` with `content` absent and no tool
	// call recovered, so parseResponse must PROMOTE the reasoning text to
	// Content instead of returning ErrEmptyResponse (audit finding F59). Before
	// the fix this returned ErrEmptyResponse, so every mlx prose turn was read
	// as an empty response and the loop burned its nudge ladder.
	resp, err := client.parseResponse(&chatResp)
	if err != nil {
		t.Fatalf("parseResponse: %v (mlx prose must be promoted, not error)", err)
	}
	if resp.Content != "thinking hard about the answer" {
		t.Errorf("resp.Content = %q, want the mlx reasoning text promoted", resp.Content)
	}
	if resp.Reasoning != "thinking hard about the answer" {
		t.Errorf("resp.Reasoning = %q, want the mlx reasoning text", resp.Reasoning)
	}
	// The alias must also stay readable from the message so the loop's
	// reasoning watchdog sees the tokens instead of treating the turn as
	// blank.
	if got := chatResp.Choices[0].Message.ReasoningText(); got == "" {
		t.Error("ReasoningText() returned empty for the mlx reasoning field")
	}
}

func TestParseResponse_RecoversToolCallsFromMlxReasoning(t *testing.T) {
	// The mlx shape with a native LFM marker inside the reasoning text:
	// recovery must surface the call so the turn executes it.
	marker := `<|tool_call_start|>[file_write(path="hello.txt", content="hello")]<|tool_call_end|>`
	raw := `{
		"model": "LFM2.5-8B-A1B-MLX-4bit",
		"choices": [{
			"index": 0,
			"finish_reason": "stop",
			"message": {"role": "assistant", "reasoning": "` +
		strings.ReplaceAll(marker, `"`, `\"`) + `"}
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`
	var chatResp ChatResponse
	if err := json.Unmarshal([]byte(raw), &chatResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	client := NewClient(&ModelConfig{ModelID: "mlx-test"})

	resp, err := client.parseResponse(&chatResp)
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(resp.ToolCalls) == 0 {
		t.Fatal("expected the LFM tool call recovered from the reasoning field")
	}
	if got := resp.ToolCalls[0].Function.Name; got != "file_write" {
		t.Errorf("recovered tool name = %q, want file_write", got)
	}
}

// TestParseResponse_StillEmptyWhenNoContentAndNoReasoning pins that the F59
// promotion is narrow: a genuinely empty reply (no content, no reasoning, no
// tool calls) still surfaces ErrEmptyResponse.
func TestParseResponse_StillEmptyWhenNoContentAndNoReasoning(t *testing.T) {
	raw := `{
		"model": "mlx-test",
		"choices": [{"index": 0, "finish_reason": "stop", "message": {"role": "assistant"}}],
		"usage": {"prompt_tokens": 1, "completion_tokens": 0, "total_tokens": 1}
	}`
	var chatResp ChatResponse
	if err := json.Unmarshal([]byte(raw), &chatResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	client := NewClient(&ModelConfig{ModelID: "mlx-test"})
	if _, err := client.parseResponse(&chatResp); err != ErrEmptyResponse {
		t.Fatalf("parseResponse on a truly empty reply = %v, want ErrEmptyResponse", err)
	}
}
