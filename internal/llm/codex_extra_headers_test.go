package llm

import (
	"context"
	"testing"
)

// TestCodexExtraHeaders_OnWire proves the configured extra headers land on
// the /responses request; static application only — no session sentinel.
func TestCodexExtraHeaders_OnWire(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[],"usage":{"input_tokens":0,"output_tokens":0}}`})

	cfg := &ModelConfig{
		BaseURL:    srv.URL,
		ModelID:    "gpt-5.1-codex",
		ProviderID: "openai-codex",
		ExtraHeaders: map[string]string{
			"X-Trace-Id": "codex-trace-1",
		},
	}
	client := NewCodexClient(cfg)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	req := <-ch
	if got := req.Header.Get("X-Trace-Id"); got != "codex-trace-1" {
		t.Errorf("x-trace-id = %q, want codex-trace-1", got)
	}
	// Defaults stay intact.
	if got := req.Header.Get("User-Agent"); got != codexUserAgent {
		t.Errorf("user-agent = %q, want default %q", got, codexUserAgent)
	}
	if got := req.Header.Get("Originator"); got != codexOriginator {
		t.Errorf("originator = %q, want default %q", got, codexOriginator)
	}
}

// TestCodexExtraHeaders_OverrideDefaults pins that config headers are
// applied after the built-in Cloudflare/client headers and win per key.
func TestCodexExtraHeaders_OverrideDefaults(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})

	cfg := &ModelConfig{
		BaseURL:    srv.URL,
		ModelID:    "gpt-5.1-codex",
		ProviderID: "openai-codex",
		ExtraHeaders: map[string]string{
			"User-Agent": "custom-agent/9.9",
			"Originator": "custom",
		},
	}
	client := NewCodexClient(cfg)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	req := <-ch
	if got := req.Header.Get("User-Agent"); got != "custom-agent/9.9" {
		t.Errorf("user-agent = %q, want config override custom-agent/9.9", got)
	}
	if got := req.Header.Get("Originator"); got != "custom" {
		t.Errorf("originator = %q, want config override custom", got)
	}
}

// TestCodexExtraHeaders_AbsentOrEmptySendsNothing covers the no-config and
// empty-value cases.
func TestCodexExtraHeaders_AbsentOrEmptySendsNothing(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})

	client := newCodexClientForTest(t, srv.URL)
	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	req := <-ch
	if _, present := req.Header["X-Trace-Id"]; present {
		t.Error("x-trace-id must be absent without extra_headers config")
	}

	// Empty-string values are never sent.
	srv2, ch2 := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	cfg2 := &ModelConfig{
		BaseURL:      srv2.URL,
		ModelID:      "gpt-5.1-codex",
		ProviderID:   "openai-codex",
		ExtraHeaders: map[string]string{"X-Empty": ""},
	}
	client2 := NewCodexClient(cfg2)
	if _, err := client2.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	req2 := <-ch2
	if _, present := req2.Header["X-Empty"]; present {
		t.Error("empty-value extra header must be omitted from the request")
	}
}
