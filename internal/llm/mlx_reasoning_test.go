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

	// Content is empty and there are no tool calls, so parseResponse still
	// returns ErrEmptyResponse — but the reasoning must be visible on the
	// struct so callers can distinguish "said nothing" from "thought only".
	resp, err := client.parseResponse(&chatResp)
	if err == nil {
		if resp.Reasoning != "thinking hard about the answer" {
			t.Errorf("resp.Reasoning = %q, want the mlx reasoning text", resp.Reasoning)
		}
		return
	}
	if err != ErrEmptyResponse {
		t.Fatalf("parseResponse error = %v, want ErrEmptyResponse", err)
	}
	// The alias must at least be readable from the message so the loop's
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
