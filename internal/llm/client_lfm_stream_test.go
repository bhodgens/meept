package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStreamRequest_LFMToolCallMarkersReccovered verifies the streaming
// path applies the same LFM2.5 marker recovery as the non-streaming Chat
// path: markers streamed as content deltas are converted to structured
// tool calls and stripped from the returned content (live-smoke finding —
// MLX-served LFM2.5 emits markers as plain content because mlx_lm server
// has no tool-call extractor, and the agent loop only ever streams).
func TestStreamRequest_LFMToolCallMarkersReccovered(t *testing.T) {
	chunks := []map[string]any{
		{"choices": []any{map[string]any{
			"delta": map[string]any{"role": "assistant", "content": "<|tool_call_start|>"},
		}}},
		{"choices": []any{map[string]any{
			"delta": map[string]any{"content": `[file_write(path="hello.txt", content="hi")]`},
		}}},
		{"choices": []any{map[string]any{
			"delta": map[string]any{"content": "<|tool_call_end|>"},
		}}},
		{"choices": []any{map[string]any{
			"delta":         map[string]any{},
			"finish_reason": "tool_calls",
		}}},
		{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 20}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		for _, ch := range chunks {
			b, _ := json.Marshal(map[string]any{"id": "x", "object": "chat.completion.chunk", "model": "m", "choices": ch["choices"], "usage": ch["usage"]})
			w.Write([]byte("data: " + string(b) + "\n\n"))
			if f != nil {
				f.Flush()
			}
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:   "/Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-MLX-4bit",
		BaseURL:   srv.URL,
		APIKey:    "test",
		MaxTokens: 1024,
	}
	client := NewClient(cfg)

	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "create hello.txt"}},
		func(string) error { return nil },
	)
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback: %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1 (markers must be recovered): %+v", len(resp.ToolCalls), resp.ToolCalls)
	}
	tc := resp.ToolCalls[0]
	if tc.Function.Name != "file_write" {
		t.Errorf("tool name = %q, want file_write", tc.Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %v (%q)", err, tc.Function.Arguments)
	}
	if args["path"] != "hello.txt" || args["content"] != "hi" {
		t.Errorf("args = %v, want path+content", args)
	}
	if resp.Content != "" {
		t.Errorf("content = %q, want markers stripped to empty", resp.Content)
	}
}
