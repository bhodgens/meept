package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Tests for the empty/malformed-completion classification (agnes-2.5-flash
// hardening, 2026-09-26 e2e runs): a 200-OK body with blank or
// whitespace-only content is a provider flake and must retry within the
// short budget, then surface ErrEmptyResponse so the agent loop's generic
// branch records the alias failure and rotation lands on the local
// fallback. Leading whitespace on REAL content is normal for
// agnes-2.5-flash ("\n\nok") and must keep passing.

// emptyChatBody returns a well-formed OpenAI chat completion with NO
// content (the provider "said nothing").
func emptyChatBody() string {
	return `{"id":"chatcmpl-e","model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`
}

// whitespaceChatBody returns a completion whose content is whitespace only.
func whitespaceChatBody() string {
	return `{"id":"chatcmpl-w","model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"  \n\t\n"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`
}

// noChoicesChatBody returns a syntactically valid completion with zero
// choices (the malformed-provider shape).
func noChoicesChatBody() string {
	return `{"id":"chatcmpl-n","model":"gpt-test","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`
}

// TestParseResponseWithTools_EmptyAndWhitespaceBodies pins the parser-level
// classification: empty content, whitespace-only content, and a zero-choice
// body all surface ErrEmptyResponse; a normal body with leading whitespace
// passes through (agnes prefixes "\n\n" to real content).
func TestParseResponseWithTools_EmptyAndWhitespaceBodies(t *testing.T) {
	c := NewClient(&ModelConfig{ProviderID: "openai", ModelID: "gpt-test"})

	for name, body := range map[string]string{
		"empty content":       emptyChatBody(),
		"whitespace content":  whitespaceChatBody(),
		"zero choices":        noChoicesChatBody(),
	} {
		var resp ChatResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("%s: fixture unmarshal: %v", name, err)
		}
		if _, err := c.parseResponseWithTools(&resp, false, "openai", "gpt-test", ""); !errors.Is(err, ErrEmptyResponse) {
			t.Errorf("%s: err = %v, want ErrEmptyResponse", name, err)
		}
	}

	// Normal body WITH leading whitespace must pass (real agnes shape).
	leading := `{"id":"chatcmpl-ok","model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":"\n\nok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	var resp ChatResponse
	if err := json.Unmarshal([]byte(leading), &resp); err != nil {
		t.Fatalf("leading-whitespace fixture unmarshal: %v", err)
	}
	got, err := c.parseResponseWithTools(&resp, false, "openai", "gpt-test", "")
	if err != nil {
		t.Fatalf("leading-whitespace body: unexpected error %v", err)
	}
	if got.Content != "\n\nok" {
		t.Errorf("Content = %q, want %q byte-identical", got.Content, "\n\nok")
	}
}

// TestClientChat_EmptyCompletionRetriesThenSucceeds: the first turn returns
// a whitespace-only completion, the retry returns ok. The flake must be
// absorbed in-loop (2 server hits, success, no alias failure needed).
func TestClientChat_EmptyCompletionRetriesThenSucceeds(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusOK, body: whitespaceChatBody()},
		{status: http.StatusOK, body: okChatBody()},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat after empty-completion retry = %v, want success", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, want content ok", resp)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2 (1 empty + 1 success)", got)
	}
}

// TestClientChat_WhitespaceCompletionExhaustsToEmptySentinel: sustained
// whitespace-only completions exhaust the short budget and surface the
// BARE ErrEmptyResponse sentinel (not the All-attempts ClientError) so
// callers classify the failure as an empty response and the alias
// resolver fails over.
func TestClientChat_WhitespaceCompletionExhaustsToEmptySentinel(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusOK, body: whitespaceChatBody()},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}
	var clientErr *ClientError
	if errors.As(err, &clientErr) && clientErr.Message != ErrEmptyResponse.Message {
		t.Errorf("sentinel mutated: %q, want %q", clientErr.Message, ErrEmptyResponse.Message)
	}
	if got := atomic.LoadInt32(&hits); got != int32(fastFailurePolicyCfg.ShortRetries) { //nolint:gosec // G115: ShortRetries is a small config int (bounded ≤ 10)
		t.Errorf("server hits = %d, want %d (ShortRetries budget)", got, fastFailurePolicyCfg.ShortRetries)
	}
}

// TestClientChat_NoChoicesBodyIsRetryableEmpty: the malformed zero-choice
// 200-OK body follows the same empty-completion path (retry then sentinel).
func TestClientChat_NoChoicesBodyIsRetryableEmpty(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusOK, body: noChoicesChatBody()},
		{status: http.StatusOK, body: okChatBody()},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat after no-choices retry = %v, want success", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, want content ok", resp)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
}

// TestClientChat_EmptyCompletionNeverSwallowsRefusalOrQuota: the new
// empty-completion branch must sit AFTER the refusal/quota early-exits —
// a refusal stays a one-hop non-retryable and quota never short-retries.
func TestClientChat_EmptyCompletionNeverSwallowsRefusalOrQuota(t *testing.T) {
	// refusal-shaped 200 body: content_filter finish reason with no text.
	refusalBody := `{"id":"chatcmpl-r","model":"gpt-test","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"content_filter"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`

	var hits int32
	srv := newScriptedServer(t, []scriptedResponse{
		{status: http.StatusOK, body: refusalBody},
	}, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	var refusalErr *RefusalError
	if !errors.As(err, &refusalErr) {
		t.Fatalf("err = %v (%T), want RefusalError", err, err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (refusal must not re-enter the retry loop)", got)
	}
}

// TestChatWithDeltaCallback_EmptyStreamSurfacesEmptySentinel: a stream that
// terminates cleanly but produced no content, no tool calls, and no
// reasoning surfaces ErrEmptyResponse through the delta path, which then
// retries within the short budget.
func TestChatWithDeltaCallback_EmptyStreamSurfacesEmptySentinel(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		// One chunk with an empty delta and a stop finish — the
		// "stream ended, nothing was produced" flake shape.
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	_, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error { return nil },
	)
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}
	if got := atomic.LoadInt32(&hits); got != int32(fastFailurePolicyCfg.ShortRetries) { //nolint:gosec // G115: ShortRetries is a small config int (bounded ≤ 10)
		t.Errorf("server hits = %d, want %d (short budget)", got, fastFailurePolicyCfg.ShortRetries)
	}
}

// TestChatWithDeltaCallback_LeadingWhitespaceStreamPasses: a stream whose
// content is "\n\nok" (the real agnes shape) is NOT empty.
func TestChatWithDeltaCallback_LeadingWhitespaceStreamPasses(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\\n\\nok\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	resp, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error { return nil },
	)
	if err != nil {
		t.Fatalf("ChatWithDeltaCallback = %v, want success", err)
	}
	if resp.Content != "\n\nok" {
		t.Errorf("Content = %q, want %q", resp.Content, "\n\nok")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1", got)
	}
}
