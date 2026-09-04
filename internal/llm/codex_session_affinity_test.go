package llm

import (
	"context"
	"testing"
)

// Wire-level tests for CodexClient per-turn session affinity: the
// "${session_id}" sentinel in ModelConfig.ExtraHeaders (and the built-in
// session_id header slot) must be substituted with the current turn's
// session ID supplied via WithTaskScope, mirroring the OpenAI-shaped
// client's applyExtraHeaders contract (exact full-value sentinel match;
// empty-after-substitution values are omitted — net/http strips empty
// headers, so "omitted" is asserted via map-key absence).

// TestCodexSessionAffinity_SentinelOnWire proves a codex request with
// ExtraHeaders {"session_id": "${session_id}"} carries the real session id
// on the wire when WithTaskScope supplies one, and that the same sentinel
// works on any other config header name.
func TestCodexSessionAffinity_SentinelOnWire(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[],"usage":{"input_tokens":0,"output_tokens":0}}`})

	cfg := &ModelConfig{
		BaseURL:    srv.URL,
		ModelID:    "gpt-5.1-codex",
		ProviderID: "openai-codex",
		ExtraHeaders: map[string]string{
			"session_id": sessionIDHeaderSentinel,
		},
	}
	client := NewCodexClient(cfg)

	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithTaskScope("task-1", "session-abc123")); err != nil {
		t.Fatalf("chat: %v", err)
	}
	req := <-ch
	if got := req.Header.Get("session_id"); got != "session-abc123" {
		t.Errorf("session_id = %q, want session-abc123", got)
	}

	// Same sentinel substitution under a different header name, and the
	// task scope does not leak into unrelated headers.
	srv2, ch2 := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	cfg2 := &ModelConfig{
		BaseURL:    srv2.URL,
		ModelID:    "gpt-5.1-codex",
		ProviderID: "openai-codex",
		ExtraHeaders: map[string]string{
			"X-Trace-Id":   "static-trace",
			"X-Ops-Anchor": sessionIDHeaderSentinel,
		},
	}
	client2 := NewCodexClient(cfg2)
	if _, err := client2.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithTaskScope("task-1", "session-abc123")); err != nil {
		t.Fatalf("chat 2: %v", err)
	}
	req2 := <-ch2
	if got := req2.Header.Get("X-Ops-Anchor"); got != "session-abc123" {
		t.Errorf("x-ops-anchor = %q, want session-abc123", got)
	}
	if got := req2.Header.Get("X-Trace-Id"); got != "static-trace" {
		t.Errorf("x-trace-id = %q, want static-trace (unrelated header untouched)", got)
	}
}

// TestCodexSessionAffinity_BuiltInSlotFromTaskScope proves the built-in
// session_id header slot (codex-rs contract) is populated from
// WithTaskScope even without any ExtraHeaders config.
func TestCodexSessionAffinity_BuiltInSlotFromTaskScope(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})

	client := newCodexClientForTest(t, srv.URL)
	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithTaskScope("task-1", "sess-uuid-42")); err != nil {
		t.Fatalf("chat: %v", err)
	}
	req := <-ch
	if got := req.Header.Get("session_id"); got != "sess-uuid-42" {
		t.Errorf("session_id = %q, want sess-uuid-42", got)
	}
}

// TestCodexSessionAffinity_NoSessionOmitsHeader covers the absent-scope and
// empty-after-substitution cases: with no WithTaskScope (or an empty session
// id) the session_id header — built-in slot or sentinel-substituted — is
// omitted from the wire.
func TestCodexSessionAffinity_NoSessionOmitsHeader(t *testing.T) {
	// Built-in slot: no WithTaskScope → header absent.
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	client := newCodexClientForTest(t, srv.URL)
	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	req := <-ch
	if len(req.Header.Values("session_id")) != 0 {
		t.Errorf("session_id must be absent without a task scope, got %q", req.Header.Get("session_id"))
	}

	// Sentinel in ExtraHeaders with no scope → substituted value is empty →
	// header omitted, not sent as a literal sentinel or empty string.
	srv2, ch2 := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	cfg2 := &ModelConfig{
		BaseURL:    srv2.URL,
		ModelID:    "gpt-5.1-codex",
		ProviderID: "openai-codex",
		ExtraHeaders: map[string]string{
			"session_id": sessionIDHeaderSentinel,
		},
	}
	client2 := NewCodexClient(cfg2)
	if _, err := client2.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat 2: %v", err)
	}
	req2 := <-ch2
	if len(req2.Header.Values("session_id")) != 0 {
		t.Errorf("sentinel header with no session must be omitted, got %q", req2.Header.Get("session_id"))
	}
	// The raw sentinel must never reach the wire.
	if v := req2.Header.Get("session_id"); v == sessionIDHeaderSentinel {
		t.Error("raw ${session_id} sentinel leaked onto the wire")
	}

	// WithTaskScope with an empty session id behaves the same as no scope.
	srv3, ch3 := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})
	client3 := newCodexClientForTest(t, srv3.URL)
	if _, err := client3.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithTaskScope("task-1", "")); err != nil {
		t.Fatalf("chat 3: %v", err)
	}
	req3 := <-ch3
	if len(req3.Header.Values("session_id")) != 0 {
		t.Errorf("empty session id must omit the header, got %q", req3.Header.Get("session_id"))
	}
}

// TestCodexSessionAffinity_StaticConfigStillLiteral pins that a non-sentinel
// session_id value in ExtraHeaders is sent verbatim (static pinning remains
// possible) and wins over the built-in slot via apply-last ordering.
func TestCodexSessionAffinity_StaticConfigStillLiteral(t *testing.T) {
	srv, ch := newCodexTestServer(t, codexResponder{status: 200, body: `{"output":[]}`})

	cfg := &ModelConfig{
		BaseURL:    srv.URL,
		ModelID:    "gpt-5.1-codex",
		ProviderID: "openai-codex",
		ExtraHeaders: map[string]string{
			"session_id": "pinned-static-session",
		},
	}
	client := NewCodexClient(cfg)
	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithTaskScope("task-1", "session-abc123")); err != nil {
		t.Fatalf("chat: %v", err)
	}
	req := <-ch
	if got := req.Header.Get("session_id"); got != "pinned-static-session" {
		t.Errorf("session_id = %q, want pinned-static-session (config wins over built-in slot)", got)
	}
}
