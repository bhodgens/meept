package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// openaiContentFilterBody is a minimal OpenAI-format completion whose choice
// finished with finish_reason "content_filter" and no content.
const openaiContentFilterBody = `{"id":"c1","object":"chat.completion","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"content_filter"}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`

// TestClientChat_ContentFilterFinishReasonSurfacesRefusalError pins Task 4:
// a content_filter finish reason on the non-streaming Chat path surfaces as
// *RefusalError, not a generic (empty) completion.
func TestClientChat_ContentFilterFinishReasonSurfacesRefusalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openaiContentFilterBody))
	}))
	defer srv.Close()

	client := NewClient(&ModelConfig{
		ProviderID: "openai-test",
		BaseURL:    srv.URL + "/v1",
		ModelID:    "test-model",
		MaxTokens:  100,
	})

	resp, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatalf("Chat = %+v, want RefusalError", resp)
	}
	var refusalErr *RefusalError
	if !errors.As(err, &refusalErr) {
		t.Fatalf("err = %T (%v), want *RefusalError", err, err)
	}
	if refusalErr.Source != "finish_reason" {
		t.Errorf("Source = %q, want finish_reason", refusalErr.Source)
	}
	if refusalErr.FinishReason != "content_filter" {
		t.Errorf("FinishReason = %q, want content_filter", refusalErr.FinishReason)
	}
	if refusalErr.ProviderID != "openai-test" {
		t.Errorf("ProviderID = %q, want openai-test", refusalErr.ProviderID)
	}
}

// TestClientStreaming_ContentFilterFinishReasonSurfacesRefusalError pins the
// streaming path: the terminal error from ChatWithDeltaCallback is a
// *RefusalError when the stream ends with finish_reason content_filter.
func TestClientStreaming_ContentFilterFinishReasonSurfacesRefusalError(t *testing.T) {
	chunks := []map[string]any{
		{"choices": []any{map[string]any{
			"delta": map[string]any{"role": "assistant", "content": "I "},
		}}},
		{"choices": []any{map[string]any{
			"delta":         map[string]any{},
			"finish_reason": "content_filter",
		}}},
		{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f, _ := w.(http.Flusher)
		for _, ch := range chunks {
			b, _ := json.Marshal(map[string]any{"id": "x", "object": "chat.completion.chunk", "model": "test-model", "choices": ch["choices"], "usage": ch["usage"]})
			_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
			if f != nil {
				f.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewClient(&ModelConfig{
		ProviderID: "openai-test",
		BaseURL:    srv.URL,
		ModelID:    "test-model",
		MaxTokens:  128,
	})

	resp, err := client.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error { return nil },
	)
	if err == nil {
		t.Fatalf("ChatWithDeltaCallback = %+v, want RefusalError", resp)
	}
	var refusalErr *RefusalError
	if !errors.As(err, &refusalErr) {
		t.Fatalf("err = %T (%v), want *RefusalError", err, err)
	}
	if refusalErr.Source != "finish_reason" || refusalErr.FinishReason != "content_filter" {
		t.Errorf("source/finishReason = %q/%q, want finish_reason/content_filter", refusalErr.Source, refusalErr.FinishReason)
	}
}

// TestAnthropicChat_RefusalStopReasonSurfacesRefusalError pins the Anthropic
// non-streaming path: stop_reason "refusal" surfaces as *RefusalError.
func TestAnthropicChat_RefusalStopReasonSurfacesRefusalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"I cannot help with that."}],"stop_reason":"refusal","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatalf("Chat = %+v, want RefusalError", resp)
	}
	var refusalErr *RefusalError
	if !errors.As(err, &refusalErr) {
		t.Fatalf("err = %T (%v), want *RefusalError", err, err)
	}
	if refusalErr.Source != "stop_reason" || refusalErr.FinishReason != "refusal" {
		t.Errorf("source/finishReason = %q/%q, want stop_reason/refusal", refusalErr.Source, refusalErr.FinishReason)
	}
}

// TestAnthropicStreaming_RefusalStopReasonSurfacesRefusalError pins the
// Anthropic streaming path: stop_reason "refusal" from message_delta
// surfaces as *RefusalError from ChatWithProgress.
func TestAnthropicStreaming_RefusalStopReasonSurfacesRefusalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"id\":\"b1\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"I cannot help with that.\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	resp, err := c.ChatWithProgress(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}}, nil)
	if err == nil {
		t.Fatalf("ChatWithProgress = %+v, want RefusalError", resp)
	}
	var refusalErr *RefusalError
	if !errors.As(err, &refusalErr) {
		t.Fatalf("err = %T (%v), want *RefusalError", err, err)
	}
	if refusalErr.Source != "stop_reason" || refusalErr.FinishReason != "refusal" {
		t.Errorf("source/finishReason = %q/%q, want stop_reason/refusal", refusalErr.Source, refusalErr.FinishReason)
	}
}

// TestDetectRefusalFromBody wired at the error-body site: a 403 carrying
// safeguard text surfaces as *RefusalError from Chat.
func TestAnthropicChat_SafeguardErrorBodySurfacesRefusalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Our safeguards flagged this message and it cannot be processed."}}`))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("Chat = nil error, want RefusalError")
	}
	var refusalErr *RefusalError
	if !errors.As(err, &refusalErr) {
		t.Fatalf("err = %T (%v), want *RefusalError", err, err)
	}
	if refusalErr.Source != "error_body" || refusalErr.StatusCode != http.StatusForbidden {
		t.Errorf("source/status = %q/%d, want error_body/403", refusalErr.Source, refusalErr.StatusCode)
	}
}
