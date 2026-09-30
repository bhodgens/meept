package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
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
		"empty content":      emptyChatBody(),
		"whitespace content": whitespaceChatBody(),
		"zero choices":       noChoicesChatBody(),
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
	refusalErr, ok := errors.AsType[*RefusalError](err)
	if !ok {
		t.Fatalf("err = %v (%T), want RefusalError", err, err)
	}
	_ = refusalErr
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (refusal must not re-enter the retry loop)", got)
	}
}

// TestChatWithDeltaCallback_EmptyStreamSurfacesEmptySentinel: a stream that
// terminates cleanly but produced no content, no tool calls, and no
// reasoning surfaces ErrEmptyResponse through the delta path, which then
// retries within the short budget.
func TestChatWithDeltaCallback_EmptyStreamSurfacesEmptySentinel(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
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
	if got := hits.Load(); got != int32(fastFailurePolicyCfg.ShortRetries) { //nolint:gosec // G115: ShortRetries is a small config int (bounded ≤ 10)
		t.Errorf("server hits = %d, want %d (short budget)", got, fastFailurePolicyCfg.ShortRetries)
	}
}

// TestChatWithDeltaCallback_LeadingWhitespaceStreamPasses: a stream whose
// content is "\n\nok" (the real agnes shape) is NOT empty.
func TestChatWithDeltaCallback_LeadingWhitespaceStreamPasses(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
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
	if got := hits.Load(); got != 1 {
		t.Errorf("server hits = %d, want 1", got)
	}
}

// TestChatWithDeltaCallback_StreamEmptyExhaustionIsBareSentinel pins L2
// (2026-09-29 bughunt): the streaming delta loop is 0-INDEXED
// (for attempt := range shortRetries), so its empty-completion exhaustion
// guard must compare attempt < shortRetries-1. A 1-indexed bound let the
// last empty attempt fall through to the loop tail and surface a WRAPPED
// ClientError ("streaming failed after N attempts") instead of the bare
// ErrEmptyResponse sentinel. errors.Is alone is insufficient here (it also
// matches through the wrap): the error must BE the sentinel.
func TestChatWithDeltaCallback_StreamEmptyExhaustionIsBareSentinel(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
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
	// Identity check: the surfaced error IS the sentinel, not a wrapping
	// ClientError that merely contains it (the sibling Chat loop returns
	// the bare sentinel; the streaming loop must match).
	if err != error(ErrEmptyResponse) { //nolint:errorlint // deliberate pointer-identity check on the bare sentinel
		t.Fatalf("err identity = %p (%T: %v), want the bare ErrEmptyResponse sentinel (%p)",
			err, err, err, error(ErrEmptyResponse))
	}
	if got := hits.Load(); got != int32(fastFailurePolicyCfg.ShortRetries) { //nolint:gosec // G115: ShortRetries is a small config int (bounded ≤ 10)
		t.Errorf("server hits = %d, want %d (short budget)", got, fastFailurePolicyCfg.ShortRetries)
	}
}

// TestClientChat_EmptyExhaustionRecordsFailureUsageRow pins L4: exhausting
// the short budget on blank completions appends ONE failure-shaped llm_calls
// row (error=1, zero completion tokens) so the burned prompt tokens are
// visible in the metrics ledger — previously the empty-exhaustion path
// recorded nothing.
func TestClientChat_EmptyExhaustionRecordsFailureUsageRow(t *testing.T) {
	var hits int32
	srv := newScriptedServer(t, []scriptedResponse{
		{status: http.StatusOK, body: whitespaceChatBody()},
	}, &hits)
	defer srv.Close()

	store := newUsageTestStore(t)
	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	c.SetUsageStore(store)
	_, err := c.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithAgentScope("coder"))
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}

	// recordUsageStore writes async; poll for the failure row.
	pollUntil(t, func() bool {
		got, qerr := store.QueryLLMCallUsage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), true)
		return qerr == nil && len(got) == 1 && got[0].Errors > 0
	})
	rows, qerr := store.QueryLLMCallUsage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), true)
	if qerr != nil || len(rows) != 1 {
		t.Fatalf("llm_calls rows = %d (err %v), want exactly 1 failure row", len(rows), qerr)
	}
	r := rows[0]
	if r.Provider != "openai" || r.AgentID != "coder" {
		t.Errorf("row attribution = %s/%s, want openai/coder", r.Provider, r.AgentID)
	}
	if r.Calls != 1 || r.Errors != 1 {
		t.Errorf("row calls/errors = %d/%d, want 1/1", r.Calls, r.Errors)
	}
	if r.TokensRecv != 0 {
		t.Errorf("row completion tokens = %d, want 0 (no content was produced)", r.TokensRecv)
	}
}

// TestChatWithDeltaCallback_StreamEmptyExhaustionRecordsFailureUsageRow is
// the streaming twin of the L4 pin: the delta-path exhaustion must also
// ledger the failure-shaped row.
func TestChatWithDeltaCallback_StreamEmptyExhaustionRecordsFailureUsageRow(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	store := newUsageTestStore(t)
	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	c.SetUsageStore(store)
	_, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error { return nil },
		WithAgentScope("coder"),
	)
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}

	pollUntil(t, func() bool {
		got, qerr := store.QueryLLMCallUsage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), true)
		return qerr == nil && len(got) == 1 && got[0].Errors > 0
	})
	rows, qerr := store.QueryLLMCallUsage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), true)
	if qerr != nil || len(rows) != 1 {
		t.Fatalf("llm_calls rows = %d (err %v), want exactly 1 failure row", len(rows), qerr)
	}
	if rows[0].Provider != "openai" || rows[0].AgentID != "coder" || rows[0].Errors != 1 || rows[0].TokensRecv != 0 {
		t.Errorf("stream failure row = %+v, want openai/coder, errors=1, tokens_received=0", rows[0])
	}
}
