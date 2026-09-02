package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastAnthropicPolicyCfg keeps in-loop waits tiny so scripted tests stay
// fast (leaf rule: injected config, no long real sleeps).
var fastAnthropicPolicyCfg = &FailurePolicyConfig{
	Horizon:      time.Hour,
	BaseThrottle: time.Millisecond,
	PollFloor:    2 * time.Millisecond,
	ShortRetries: 3,
}

// newAnthropicPolicyTestClient builds an AnthropicClient pointed at srv with
// a fast failure-policy config injected (leaf Task 5 seam).
func newAnthropicPolicyTestClient(t *testing.T, srv *httptest.Server, cfg *FailurePolicyConfig) *AnthropicClient {
	t.Helper()
	c := NewAnthropicClient(&ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		MaxTokens:  128,
	}, WithAnthropicLogger(discardLogger()))
	c.SetFailurePolicyConfig(cfg)
	return c
}

// anthropicOKBody is a minimal successful non-streaming Messages response.
const anthropicOKBody = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

// anthropicSSEBody is a minimal successful SSE stream (message → text delta
// → stop), matching parseStreamingResponse's event shapes.
const anthropicSSEBody = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"usage\":{\"input_tokens\":1}}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"id\":\"b1\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

// TestAnthropicChat_RetryAfterThenSuccess (leaf Task 3): a throttled 429
// with Retry-After:1 is retried once, then the scripted success lands.
func TestAnthropicChat_RetryAfterThenSuccess(t *testing.T) {
	var hits int32
	idx := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		if atomic.CompareAndSwapInt32(&idx, 0, 1) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			// Plain-text body: no rate_limit_error type or quota keywords,
			// so the request layer keeps this on the RateLimitError path
			// (D7 bare throttle) instead of classifying it quota.
			_, _ = w.Write([]byte("Too many requests, please slow down."))
			return
		}
		_, _ = w.Write([]byte(anthropicOKBody))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat after retry = %v, want success", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, want content ok", resp)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
}

// TestAnthropicChat_Bare429ExhaustionReturnsThrottleBackoff pins the core
// leaf semantics on the anthropic non-streaming loop: sustained bare-429
// throttling exhausts the short budget and returns ThrottleBackoffError
// (NOT ClientError) with plan-derived RetryAt (D4/D8).
func TestAnthropicChat_Bare429ExhaustionReturnsThrottleBackoff(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		// Plain-text body: no rate_limit_error type, no quota keywords —
		// the D7 bare-throttle bucket (parity corpus, anthropic_ratelimit
		// "429 with plain text body" case).
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Too many requests, please slow down."))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	start := time.Now()
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		t.Fatalf("got ClientError, want ThrottleBackoffError: %v", err)
	}
	var tbErr *ThrottleBackoffError
	if !errors.As(err, &tbErr) {
		t.Fatalf("expected ThrottleBackoffError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&hits); got != int32(fastAnthropicPolicyCfg.ShortRetries) {
		t.Errorf("server hits = %d, want %d (ShortRetries budget)", got, fastAnthropicPolicyCfg.ShortRetries)
	}
	if tbErr.ProviderID != "anthropic" || tbErr.ModelID != "claude-test" {
		t.Errorf("context = %s/%s, want anthropic/claude-test", tbErr.ProviderID, tbErr.ModelID)
	}
	if tbErr.Attempt != fastAnthropicPolicyCfg.ShortRetries {
		t.Errorf("Attempt = %d, want %d", tbErr.Attempt, fastAnthropicPolicyCfg.ShortRetries)
	}
	lower := start.Add(fastAnthropicPolicyCfg.BaseThrottle)
	upper := start.Add(fastAnthropicPolicyCfg.BaseThrottle*1<<uint(fastAnthropicPolicyCfg.ShortRetries-1) + 10*time.Millisecond)
	if tbErr.RetryAt.Before(lower) || tbErr.RetryAt.After(upper) {
		t.Errorf("RetryAt = %v, want within [%v, %v] (plan-derived)", tbErr.RetryAt, lower, upper)
	}
}

// TestAnthropicChat_402QuotaImmediate pins the quota invariant on the
// anthropic loop: a scripted 402 surfaces QuotaResetError after exactly ONE
// attempt (402 = retry-with-estimate; SHARED-CONVENTIONS §2).
func TestAnthropicChat_402QuotaImmediate(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"type":"insufficient_quota","message":"billing"}}`))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (quota exits immediately)", got)
	}
	var quotaErr *QuotaResetError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("expected QuotaResetError, got %T: %v", err, err)
	}
}

// TestAnthropicChat_500ThenSuccess pins the 5xx path: a server error is
// short-retried and the success lands.
func TestAnthropicChat_500ThenSuccess(t *testing.T) {
	var hits int32
	idx := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		if atomic.CompareAndSwapInt32(&idx, 0, 1) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"boom"}}`))
			return
		}
		_, _ = w.Write([]byte(anthropicOKBody))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat after 500 retry = %v, want success", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, want content ok", resp)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
}

// TestAnthropicChat_500ExhaustionKeepsClientError pins the leaf Notes rule:
// server-error exhaustion keeps the historical ClientError shape.
func TestAnthropicChat_500ExhaustionKeepsClientError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"boom"}}`))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var clientErr *ClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("expected ClientError on 5xx exhaustion, got %T: %v", err, err)
	}
	if !strings.Contains(clientErr.Message, "attempts failed") {
		t.Errorf("Message = %q, want the 'All N attempts failed' shape", clientErr.Message)
	}
	if got := atomic.LoadInt32(&hits); got != int32(fastAnthropicPolicyCfg.ShortRetries) {
		t.Errorf("server hits = %d, want %d", got, fastAnthropicPolicyCfg.ShortRetries)
	}
}

// TestAnthropicChatWithProgress_Bare429ExhaustionReturnsThrottleBackoff
// pins the same semantics on the anthropic STREAMING loop (pre-first-token
// gating preserved: the 429 arrives before the SSE scanner starts).
func TestAnthropicChatWithProgress_Bare429ExhaustionReturnsThrottleBackoff(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Too many requests, please slow down."))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	start := time.Now()
	_, err := c.ChatWithProgress(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(ProgressStage, string) {})
	if err == nil {
		t.Fatal("expected error")
	}
	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		t.Fatalf("got ClientError, want ThrottleBackoffError: %v", err)
	}
	var tbErr *ThrottleBackoffError
	if !errors.As(err, &tbErr) {
		t.Fatalf("expected ThrottleBackoffError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&hits); got != int32(fastAnthropicPolicyCfg.ShortRetries) {
		t.Errorf("server hits = %d, want %d", got, fastAnthropicPolicyCfg.ShortRetries)
	}
	lower := start.Add(fastAnthropicPolicyCfg.BaseThrottle)
	upper := start.Add(fastAnthropicPolicyCfg.BaseThrottle*1<<uint(fastAnthropicPolicyCfg.ShortRetries-1) + 10*time.Millisecond)
	if tbErr.RetryAt.Before(lower) || tbErr.RetryAt.After(upper) {
		t.Errorf("RetryAt = %v, want within [%v, %v] (plan-derived)", tbErr.RetryAt, lower, upper)
	}
}

// TestAnthropicChatWithProgress_RetryAfterThenSuccess pins the streaming
// retry path with SSE-encoded success after a throttled first response.
func TestAnthropicChatWithProgress_RetryAfterThenSuccess(t *testing.T) {
	var hits int32
	idx := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		if atomic.CompareAndSwapInt32(&idx, 0, 1) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			// Plain-text body keeps this on the RateLimitError path (D7
			// bare throttle) instead of the request layer classifying it
			// quota via the structured error type.
			_, _ = w.Write([]byte("Too many requests, please slow down."))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicSSEBody))
	}))
	defer srv.Close()

	c := newAnthropicPolicyTestClient(t, srv, fastAnthropicPolicyCfg)
	resp, err := c.ChatWithProgress(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(ProgressStage, string) {})
	if err != nil {
		t.Fatalf("ChatWithProgress after retry = %v, want success", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, want content ok", resp)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
}

// TestAnthropicChat_ShortRetriesOneHonored is leaf Task 5 on anthropic:
// ShortRetries=1 must yield exactly one attempt before escalation.
func TestAnthropicChat_ShortRetriesOneHonored(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Too many requests, please slow down."))
	}))
	defer srv.Close()

	cfg := *fastAnthropicPolicyCfg
	cfg.ShortRetries = 1
	c := newAnthropicPolicyTestClient(t, srv, &cfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var tbErr *ThrottleBackoffError
	if !errors.As(err, &tbErr) {
		t.Fatalf("expected ThrottleBackoffError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (ShortRetries=1)", got)
	}
	if tbErr.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", tbErr.Attempt)
	}
}
