package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newAnthropicHeaderServer serves a minimal valid Messages response (or SSE
// stream) while recording request headers for assertion.
func newAnthropicHeaderServer(t *testing.T, sse bool) (*httptest.Server, func() http.Header) {
	t.Helper()
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(anthropicSSEBody))
			return
		}
		resp := map[string]any{
			"id":          "x",
			"type":        "message",
			"role":        "assistant",
			"model":       "m",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "hi"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		b, err := json.Marshal(resp)
		if err != nil {
			t.Errorf("marshal fixture: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv, func() http.Header { return headers }
}

func TestAnthropicExtraHeaders_NonStream(t *testing.T) {
	srv, hdrs := newAnthropicHeaderServer(t, false)
	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5",
		BaseURL:    srv.URL,
		APIKey:     "sk-extra",
		ExtraHeaders: map[string]string{
			"X-Trace-Id": "trace-42",
		},
	}
	client := NewAnthropicClient(cfg)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := hdrs().Get("X-Trace-Id"); got != "trace-42" {
		t.Errorf("x-trace-id = %q, want trace-42", got)
	}
}

func TestAnthropicExtraHeaders_Stream(t *testing.T) {
	srv, hdrs := newAnthropicHeaderServer(t, true)
	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5",
		BaseURL:    srv.URL,
		APIKey:     "sk-extra",
		ExtraHeaders: map[string]string{
			"X-Trace-Id": "trace-stream",
		},
	}
	client := NewAnthropicClient(cfg)

	resp, err := client.ChatWithProgress(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		func(ProgressStage, string) {})
	if err != nil {
		t.Fatalf("chat with progress: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want ok", resp.Content)
	}
	if got := hdrs().Get("X-Trace-Id"); got != "trace-stream" {
		t.Errorf("x-trace-id = %q, want trace-stream", got)
	}
}

// TestAnthropicExtraHeaders_OverrideDefault pins that config headers are
// applied AFTER auth/defaults and may override them (anthropic-version).
func TestAnthropicExtraHeaders_OverrideDefault(t *testing.T) {
	srv, hdrs := newAnthropicHeaderServer(t, false)
	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5",
		BaseURL:    srv.URL,
		APIKey:     "sk-extra",
		ExtraHeaders: map[string]string{
			"anthropic-version": "2099-01-01",
			"X-Api-Key":         "sk-override",
		},
	}
	client := NewAnthropicClient(cfg)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := hdrs().Get("Anthropic-Version"); got != "2099-01-01" {
		t.Errorf("anthropic-version = %q, want config override 2099-01-01", got)
	}
	if got := hdrs().Get("X-Api-Key"); got != "sk-override" {
		t.Errorf("x-api-key = %q, want config override sk-override", got)
	}
}

func TestAnthropicExtraHeaders_AbsentOrEmptyConfigSendsNothing(t *testing.T) {
	srv, hdrs := newAnthropicHeaderServer(t, false)
	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5",
		BaseURL:    srv.URL,
		APIKey:     "sk-extra",
	}
	client := NewAnthropicClient(cfg)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := hdrs().Get("X-Trace-Id"); got != "" {
		t.Errorf("x-trace-id must be absent without extra_headers, got %q", got)
	}
	// Defaults stay intact when no override is configured.
	if got := hdrs().Get("Anthropic-Version"); got != anthropicAPIVersion {
		t.Errorf("anthropic-version = %q, want default %q", got, anthropicAPIVersion)
	}

	// Empty-string values are never sent.
	srv2, hdrs2 := newAnthropicHeaderServer(t, false)
	cfg2 := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5",
		BaseURL:    srv2.URL,
		APIKey:     "sk-extra",
		ExtraHeaders: map[string]string{
			"X-Empty": "",
		},
	}
	client2 := NewAnthropicClient(cfg2)
	if _, err := client2.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if _, present := hdrs2()["X-Empty"]; present {
		t.Error("empty-value extra header must be omitted from the request")
	}
}
