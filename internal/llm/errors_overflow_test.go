package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// F-A1 pins: the llama.cpp HTTP-500 "Context size has been exceeded." shape
// (2026-09-18 e2e: 10 hopeless retries per turn, each surfaced as
// "streaming failed after 3 attempts") must classify as a typed
// *ContextOverflowError, exit every client short-retry loop immediately,
// and never reach ProviderManager recordFailure/rotation.

const overflowBody500 = `{"error":{"code":500,"message":"Context size has been exceeded.","type":"server_error"}}`

func TestContextOverflowError_NonRetryable(t *testing.T) {
	err := &ContextOverflowError{ProviderID: "p1", ModelID: "m1", StatusCode: 500, Message: "x"}
	if !err.NonRetryable() {
		t.Fatal("ContextOverflowError must be NonRetryable")
	}
	if !IsNonRetryable(err) {
		t.Fatal("IsNonRetryable must see ContextOverflowError")
	}
	if !IsContextOverflowError(err) {
		t.Fatal("IsContextOverflowError misses its own type")
	}
	if !IsContextOverflowError(fmt.Errorf("wrapped: %w", err)) {
		t.Fatal("IsContextOverflowError must unwrap chains")
	}
	if IsContextOverflowError(errors.New("other")) {
		t.Fatal("IsContextOverflowError matched an unrelated error")
	}
}

// TestDetectContextOverflowFromBody_Markers pins typed classification from
// each marker, case-insensitively, and no-match => nil.
func TestDetectContextOverflowFromBody_Markers(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"llama.cpp 500", `{"error":{"code":500,"message":"Context size has been exceeded.","type":"server_error"}}`, true},
		{"llama.cpp lowercase", "ctx: context size has been exceeded, abort", true},
		{"openai code", `{"error":{"code":"context_length_exceeded","message":"..."}}`, true},
		{"openai prose", "This model's maximum context length is 16384 tokens", true},
		{"anthropic prose", "prompt is too long: 200000 tokens > maximum context length of 16384", true},
		{"refusal shape", "safeguards flagged this message", false},
		{"unrelated 500", `{"error":{"code":500,"message":"internal error"}}`, false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		got := DetectContextOverflowFromBody("p1", "m1", 500, tc.body)
		if got != nil && !tc.want {
			t.Errorf("%s: classified as overflow but must not be", tc.name)
		}
		if got == nil && tc.want {
			t.Errorf("%s: overflow marker not detected", tc.name)
		}
	}
	// The anthropic prose case above: verify "maximum context length" DOES
	// match when it appears alone.
	if got := DetectContextOverflowFromBody("p1", "m1", 400, `Maximum Context Length exceeded`); got == nil {
		t.Error("case-insensitive 'maximum context length' marker missed")
	}
	// Truncation: the body detail caps at 500 chars.
	long := strings.Repeat("x", 1000) + " context size has been exceeded"
	if got := DetectContextOverflowFromBody("p1", "m1", 500, long); got != nil && len(got.Message) > 500 {
		t.Errorf("Message not truncated: %d chars", len(got.Message))
	}
}

// overflowTestClient builds a Client pointed at srv with a tiny context so
// the test server is the serving provider.
func overflowTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return NewClient(&ModelConfig{
		ProviderID: "p1",
		BaseURL:    srv.URL + "/v1",
		ModelID:    "test-model",
		MaxTokens:  10,
	})
}

// TestClientChat_OverflowBodyNoShortRetry pins: (a) the llama.cpp 500
// overflow body classifies as *ContextOverflowError on the non-streaming
// path; (b) the short-retry loop exits immediately — the server sees ONE
// request, not 3.
func TestClientChat_OverflowBodyNoShortRetry(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(overflowBody500))
	}))
	defer srv.Close()

	client := overflowTestClient(t, srv)
	_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if !IsContextOverflowError(err) {
		t.Fatalf("err = %v (%T), want *ContextOverflowError", err, err)
	}
	if got := requests; got != 1 {
		t.Fatalf("server saw %d requests, want 1 (short-retry must early-exit on overflow)", got)
	}
}

// TestClientChat_StreamingOverflowNoShortRetry pins the streaming delta
// path: the overflow body surfaces as *ContextOverflowError and the loop
// makes exactly one attempt (pre-fix: "streaming failed after 3 attempts").
func TestClientChat_StreamingOverflowNoShortRetry(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(overflowBody500))
	}))
	defer srv.Close()

	client := overflowTestClient(t, srv)
	_, err := client.ChatWithDeltaCallback(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}}, func(string) error { return nil })
	if !IsContextOverflowError(err) {
		t.Fatalf("err = %v (%T), want *ContextOverflowError", err, err)
	}
	if got := requests; got != 1 {
		t.Fatalf("server saw %d requests, want 1", got)
	}
}

// TestProviderManager_OverflowNoRotateNoRecordFailure pins the manager
// contract: an overflow from the primary returns immediately to the caller
// — the fallback is NOT attempted (no rotation) and the primary's health
// counters are untouched (no recordFailure).
func TestProviderManager_OverflowNoRotateNoRecordFailure(t *testing.T) {
	primary := &streamingStubChatter{
		chatErr: &ContextOverflowError{
			ProviderID: "primary",
			ModelID:    "m1",
			StatusCode: 500,
			Cause:      &APIError{StatusCode: 500, Detail: overflowBody500},
		},
	}
	fallback := &streamingStubChatter{
		streamOK: &Response{Content: "ok"},
	}
	pm := newStreamingTestPM(t, map[string]*streamingStubChatter{
		"primary":  primary,
		"fallback": fallback,
	})

	_, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if !IsContextOverflowError(err) {
		t.Fatalf("err = %v, want the ContextOverflowError returned (no rotation)", err)
	}
	if fallback.streamCalled {
		t.Fatal("fallback was called — overflow must NOT rotate")
	}
	entry := findEntry(pm, "primary")
	if entry.Health.FailureCount != 0 || entry.Health.ConsecutiveFails != 0 {
		t.Fatalf("primary health recorded failure: count=%d consecutive=%d — overflow must NOT recordFailure",
			entry.Health.FailureCount, entry.Health.ConsecutiveFails)
	}
}

// TestContextFirewall_CompactForOverflow pins the aggressive-compaction
// seam: a long history reduces to the system prompt + recent tail, and the
// trimmed flag is true.
func TestContextFirewall_CompactForOverflow(t *testing.T) {
	fw := NewContextFirewall(
		&stubChatter{},
		&ModelConfig{ContextLimit: 512},
		ContextFirewallConfig{Enabled: true, DropContextOnHardLimit: true},
		nil,
		slog.New(slog.DiscardHandler),
		nil,
	)

	messages := []ChatMessage{
		{Role: RoleSystem, Content: "system prompt"},
	}
	for i := 0; i < 50; i++ {
		messages = append(messages,
			ChatMessage{Role: RoleUser, Content: fmt.Sprintf("question %d %s", i, strings.Repeat("q", 200))},
			ChatMessage{Role: RoleAssistant, Content: fmt.Sprintf("answer %d %s", i, strings.Repeat("a", 200))},
		)
	}

	compacted, trimmed := fw.CompactForOverflow(context.Background(), messages)
	if !trimmed {
		t.Fatal("CompactForOverflow reported nothing trimmed on a bloated history")
	}
	if len(compacted) >= len(messages) {
		t.Fatalf("messages not reduced: before=%d after=%d", len(messages), len(compacted))
	}
	if compacted[0].Role != RoleSystem || compacted[0].Content != "system prompt" {
		t.Fatalf("system prompt not preserved: %+v", compacted[0])
	}
}

// TestContextFirewall_CompactForOverflow_MinimalContext pins the honest
// false: an already-minimal context cannot shrink, so the caller must not
// retry.
func TestContextFirewall_CompactForOverflow_MinimalContext(t *testing.T) {
	fw := NewContextFirewall(
		&stubChatter{},
		&ModelConfig{ContextLimit: 4096},
		ContextFirewallConfig{Enabled: true, DropContextOnHardLimit: true},
		nil,
		slog.New(slog.DiscardHandler),
		nil,
	)
	messages := []ChatMessage{
		{Role: RoleSystem, Content: "s"},
		{Role: RoleUser, Content: "u"},
	}
	_, trimmed := fw.CompactForOverflow(context.Background(), messages)
	if trimmed {
		t.Fatal("minimal context reported as trimmed — a retry would be hopeless")
	}
}

// TestQuotaWaitChatter_OverflowPassthrough pins the resolver-direct quota
// waiter does not swallow an overflow into its wait/retry machinery.
func TestQuotaWaitChatter_OverflowPassthrough(t *testing.T) {
	overflow := &ContextOverflowError{ProviderID: "p1", ModelID: "m1", StatusCode: 500}
	inner := &streamingStubChatter{chatErr: overflow}
	w := newQuotaWaitChatter(inner, QuotaWaitConfig{MaxWait: time.Hour}, slog.New(slog.DiscardHandler))

	_, err := w.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if !IsContextOverflowError(err) {
		t.Fatalf("err = %v, want overflow passthrough", err)
	}
}

// TestIsRetryableError_OverflowFalse covers the broker gate.
func TestIsRetryableError_OverflowFalse(t *testing.T) {
	if isRetryableError(&ContextOverflowError{StatusCode: 500}) {
		t.Fatal("overflow must not be retryable")
	}
	if !isRetryableError(&APIError{StatusCode: 500}) {
		t.Fatal("control: a plain 5xx must stay retryable")
	}
}
