package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastFailurePolicyCfg keeps in-loop waits tiny so scripted tests stay fast:
// base 30s steps become 0-delay Continue inside the loop's context select.
var fastFailurePolicyCfg = &FailurePolicyConfig{
	Horizon:      time.Hour,
	BaseThrottle: time.Millisecond,
	PollFloor:    2 * time.Millisecond,
	ShortRetries: 3,
}

// newFailurePolicyTestClient builds a Client pointed at srv with a fast
// failure-policy config injected (leaf Task 5 seam).
func newFailurePolicyTestClient(t *testing.T, srv *httptest.Server, cfg *FailurePolicyConfig) *Client {
	t.Helper()
	c := NewClient(&ModelConfig{
		ProviderID: "openai",
		ModelID:    "gpt-test",
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		MaxTokens:  16,
	}, WithLogger(discardLogger()))
	c.SetFailurePolicyConfig(cfg)
	return c
}

// scriptedResponse is one step of a scripted server sequence.
type scriptedResponse struct {
	status     int
	retryAfter string // optional Retry-After header value (delta seconds)
	body       string // response body
}

// newScriptedServer serves the given sequence in order; the last response
// repeats forever. hits counts every request the server saw.
func newScriptedServer(t *testing.T, seq []scriptedResponse, hits *int32) *httptest.Server {
	t.Helper()
	idx := int32(0)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		i := int(atomic.LoadInt32(&idx))
		if i >= len(seq) {
			i = len(seq) - 1
		} else {
			atomic.AddInt32(&idx, 1)
		}
		step := seq[i]
		if step.retryAfter != "" {
			w.Header().Set("Retry-After", step.retryAfter)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.status)
		_, _ = w.Write([]byte(step.body))
	}))
}

func okChatBody() string {
	resp := ChatResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-test",
		Choices: []Choice{{
			Index: 0,
			Message: ResponseMessage{
				Role:    "assistant",
				Content: json.RawMessage(`"ok"`),
			},
			FinishReason: "stop",
		}},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// TestClientChat_RetryAfterThenSuccess (leaf Task 2, openai non-streaming):
// a throttled response carrying Retry-After:1 is retried once, then the
// scripted success lands. Asserts server Retry-After is honored for the
// in-loop delay (D4/D8 short horizon; ParseRetryAfter subsumes the old
// rlErr.RetryAfter seam).
func TestClientChat_RetryAfterThenSuccess(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusTooManyRequests, retryAfter: "1", body: `{"error":{"message":"slow down"}}`},
		{status: http.StatusOK, body: okChatBody()},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat after retry = %v, want success", err)
	}
	if resp == nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, want content ok", resp)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2 (1 throttled + 1 success)", got)
	}
}

// TestClientChat_Bare429ExhaustionReturnsThrottleBackoff pins the core
// leaf semantics: sustained bare-429 throttling exhausts the short budget
// and returns ThrottleBackoffError (NOT ClientError) with RetryAt inside
// the plan windows [Base, Base*2^(N-1)] and capped by the horizon (D8
// plan-derived, D4 throttle≠quota).
func TestClientChat_Bare429ExhaustionReturnsThrottleBackoff(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusTooManyRequests, body: `{"error":{"message":"429 bare"}}`},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	start := time.Now()
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, &ClientError{}) {
		t.Fatalf("got ClientError, want ThrottleBackoffError: %v", err)
	}
	var tbErr *ThrottleBackoffError
	if !errors.As(err, &tbErr) {
		t.Fatalf("expected ThrottleBackoffError, got %T: %v", err, err)
	}
	if got := atomic.LoadInt32(&hits); got != int32(fastFailurePolicyCfg.ShortRetries) { //nolint:gosec // G115: ShortRetries is a small config int (bounded ≤ 10)
		t.Errorf("server hits = %d, want %d (ShortRetries budget)", got, fastFailurePolicyCfg.ShortRetries)
	}
	if tbErr.ProviderID != "openai" || tbErr.ModelID != "gpt-test" {
		t.Errorf("context = %s/%s, want openai/gpt-test", tbErr.ProviderID, tbErr.ModelID)
	}
	if tbErr.Attempt != fastFailurePolicyCfg.ShortRetries {
		t.Errorf("Attempt = %d, want %d", tbErr.Attempt, fastFailurePolicyCfg.ShortRetries)
	}
	// RetryAt windows from the plan measured from the run start: the next
	// step for attempt N is Base*2^(N-1) (≤ plan horizon by construction).
	// The upper bound relaxes by the monotonic reading offset between the
	// two time.Now() calls (sub-millisecond; the window itself is what is
	// pinned, not scheduler jitter).
	lower := start.Add(fastFailurePolicyCfg.BaseThrottle)
	upper := start.Add(fastFailurePolicyCfg.BaseThrottle*1<<uint(fastFailurePolicyCfg.ShortRetries-1) + 10*time.Millisecond)
	if tbErr.RetryAt.Before(lower) {
		t.Errorf("RetryAt = %v, want ≥ %v (plan-derived)", tbErr.RetryAt, lower)
	}
	if tbErr.RetryAt.After(upper) {
		t.Errorf("RetryAt = %v, want ≤ %v (plan horizon)", tbErr.RetryAt, upper)
	}
	// The cause chain stays intact: errors.As must reach the APIError.
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("cause chain lost APIError{429}: %v", err)
	}
}

// TestClientChat_402QuotaImmediate pins the quota invariant: a scripted 402
// surfaces QuotaResetError after exactly ONE attempt (quota never re-enters
// the short-retry loop; SHARED-CONVENTIONS §2).
func TestClientChat_402QuotaImmediate(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusPaymentRequired, body: `{"error":{"type":"insufficient_quota","message":"billing"}}`},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (quota exits immediately)", got)
	}
	if _, ok := errors.AsType[*QuotaResetError](err); !ok {
		t.Fatalf("expected QuotaResetError, got %T: %v", err, err)
	}
}

// TestClientChat_500ThenSuccess pins the 5xx path: a server error is
// short-retried and the success lands (bounded retry via the plan, D8).
func TestClientChat_500ThenSuccess(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusInternalServerError, body: `{"error":{"message":"boom"}}`},
		{status: http.StatusOK, body: okChatBody()},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
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

// TestClientChat_500ExhaustionKeepsClientError pins the leaf Notes rule:
// server-error exhaustion keeps the historical ClientError
// "All N attempts failed" shape — only THROTTLE exhaustion changes type.
func TestClientChat_500ExhaustionKeepsClientError(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusInternalServerError, body: `{"error":{"message":"boom"}}`},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	var clientErr *ClientError
	if !errors.As(err, &clientErr) {
		t.Fatalf("expected ClientError on 5xx exhaustion, got %T: %v", err, err)
	}
	want := fmt.Sprintf("All %d attempts failed", fastFailurePolicyCfg.ShortRetries)
	if !strings.Contains(clientErr.Message, "attempts failed") {
		t.Errorf("Message = %q, want the 'All N attempts failed' shape", clientErr.Message)
	}
	_ = want
	if got := atomic.LoadInt32(&hits); got != int32(fastFailurePolicyCfg.ShortRetries) { //nolint:gosec // G115: ShortRetries is a small config int (bounded ≤ 10)
		t.Errorf("server hits = %d, want %d", got, fastFailurePolicyCfg.ShortRetries)
	}
}

// TestClientChat_QuotaBody429Immediate pins D7 on the loop: a 429 whose body
// carries a quota shape classifies FailureQuota and exits after ONE attempt
// with QuotaResetError (never short-retried; existing corpus stays green).
func TestClientChat_QuotaBody429Immediate(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusTooManyRequests, body: `{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":1893456000}}`},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	c := newFailurePolicyTestClient(t, srv, fastFailurePolicyCfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (quota 429 exits immediately)", got)
	}
	if _, ok := errors.AsType[*QuotaResetError](err); !ok {
		t.Fatalf("expected QuotaResetError, got %T: %v", err, err)
	}
}

// TestClientChat_ShortRetriesOneHonored is leaf Task 5: the loop's retry
// count comes from FailurePolicyConfig.ShortRetries (nil-safe default 3
// covered elsewhere); ShortRetries=1 must yield exactly one attempt.
func TestClientChat_ShortRetriesOneHonored(t *testing.T) {
	var hits int32
	seq := []scriptedResponse{
		{status: http.StatusTooManyRequests, body: `{"error":{"message":"429 bare"}}`},
	}
	srv := newScriptedServer(t, seq, &hits)
	defer srv.Close()

	cfg := *fastFailurePolicyCfg
	cfg.ShortRetries = 1
	c := newFailurePolicyTestClient(t, srv, &cfg)
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
