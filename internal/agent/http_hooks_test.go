package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// helper: create an HTTPHook pointing at the given test server, with the
// URL added to the allowlist.
func newTestHTTPHook(t *testing.T, srv *httptest.Server, cfg HTTPHookConfig) *HTTPHook {
	t.Helper()
	hook, err := NewHTTPHook(cfg, []string{srv.URL}, slog.Default())
	if err != nil {
		t.Fatalf("NewHTTPHook: %v", err)
	}
	return hook
}

// TestNewHTTPHook_DefaultRetryCount: retry_count is a *int upstream in the
// config surface; the daemon wiring resolves nil (key omitted) to the default
// of 3 and passes the concrete value here. The constructor itself passes
// RetryCount through untouched — 3 in, 3 out.
func TestNewHTTPHook_DefaultRetryCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	hook, err := NewHTTPHook(HTTPHookConfig{URL: srv.URL, RetryCount: 3}, []string{srv.URL}, slog.Default())
	if err != nil {
		t.Fatalf("NewHTTPHook: %v", err)
	}
	if hook.config.RetryCount != 3 {
		t.Fatalf("RetryCount = %d, want 3 (passed through)", hook.config.RetryCount)
	}
}

// TestNewHTTPHook_ExplicitRetryCountRespected: an explicitly-set retry_count
// must pass through untouched.
func TestNewHTTPHook_ExplicitRetryCountRespected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	for _, want := range []int{1, 5} {
		hook, err := NewHTTPHook(
			HTTPHookConfig{URL: srv.URL, RetryCount: want},
			[]string{srv.URL}, slog.Default(),
		)
		if err != nil {
			t.Fatalf("NewHTTPHook(RetryCount=%d): %v", want, err)
		}
		if hook.config.RetryCount != want {
			t.Fatalf("RetryCount = %d, want %d", hook.config.RetryCount, want)
		}
	}
}

// TestHTTPHook_TransientFailureRetriesByDefault: with the wiring default
// retry_count (3), a transient 500 must be retried and the hook must succeed
// on the second attempt. This is the regression test for the original
// production bug where RetryCount=0 tripped the loop guard at attempt 0 and
// Execute failed permanently with "after 0 retries".
func TestHTTPHook_TransientFailureRetriesByDefault(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Wiring default (nil → 3 in epistemic_wiring.go).
	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:        srv.URL,
		Method:     "POST",
		RetryCount: 3,
	})
	if err := hook.Execute(context.Background(), map[string]any{"hi": true}); err != nil {
		t.Fatalf("Execute should succeed after one transient 500: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("server hit %d times, want 2 (1 failed + 1 retry)", got)
	}
}

// TestHTTPHook_RetryResendsBody: a retry after a failed attempt must carry
// the FULL request body again. The retry loop rebuilds the request per
// attempt; reusing one http.Request would send Content-Length=N with a
// drained body on attempt 2+ ("http: ContentLength=30 with Body length 0").
func TestHTTPHook_RetryResendsBody(t *testing.T) {
	const payload = `{"prompt":"retry-me"}`
	var hits atomic.Int32
	var secondBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		n := hits.Add(1)
		if n == 2 {
			secondBody = string(b)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:        srv.URL,
		Method:     "POST",
		RetryCount: 1,
	})
	_ = hook.Execute(context.Background(), json.RawMessage(payload))
	if got := hits.Load(); got != 2 {
		t.Fatalf("server hit %d times, want 2 (retry_count=1)", got)
	}
	if secondBody != payload {
		t.Fatalf("retry attempt sent body %q, want %q", secondBody, payload)
	}
}

// TestHTTPHook_ZeroRetryCountMeansNoRetries: an explicit retry_count of 0
// must perform exactly ONE attempt with no retries and no backoff sleeps.
// This is only expressible because the config surface carries retry_count as
// a *int (nil = omitted = 3), so 0 is a real operator decision.
func TestHTTPHook_ZeroRetryCountMeansNoRetries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:        srv.URL,
		Method:     "POST",
		RetryCount: 0,
	})
	err := hook.Execute(context.Background(), map[string]any{"hi": true})
	if err == nil {
		t.Fatal("Execute should fail when server always returns 500")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hit %d times, want 1 (retry_count=0 means no retries)", got)
	}
}

// TestHTTPHook_UnlimitedRetryStopsOnAuthFailure pins the non-retryable
// guard: with retry_count=-1 a permanent 401 must be returned on the FIRST
// attempt — the old unconditional shouldRetryHookError fallback retried
// auth failures forever (bounded only by backoff and context), hammering a
// dead endpoint from a background goroutine.
func TestHTTPHook_UnlimitedRetryStopsOnAuthFailure(t *testing.T) {
	t.Cleanup(func() {
		clearPerOperationOverrides()
	})
	SetPerOperationBackoffOverride("http", BackoffConfig{
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   1 * time.Millisecond,
		Multiplier: 1.0,
		Jitter:     0,
	})

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:        srv.URL,
		Method:     "POST",
		RetryCount: -1,
	})
	err := hook.Execute(context.Background(), map[string]any{"hi": true})
	if err == nil {
		t.Fatal("Execute should fail on permanent 401")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hit %d times, want 1 (401 is non-retryable even with retry_count=-1)", got)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q lost the HTTP status detail", err.Error())
	}
}

// TestHTTPHook_UnlimitedRetryCount: retry_count -1 means UNLIMITED retries.
// A permanently failing server must keep being retried until the context is
// cancelled. The backoff override forces a tiny deterministic sleep so the
// loop spins quickly; cancellation bounds the test.
func TestHTTPHook_UnlimitedRetryCount(t *testing.T) {
	t.Cleanup(func() {
		clearPerOperationOverrides()
		clearDefaultBackoffOverride()
	})
	SetPerOperationBackoffOverride("http", BackoffConfig{
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   1 * time.Millisecond,
		Multiplier: 1.0,
		Jitter:     0,
	})

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:        srv.URL,
		Method:     "POST",
		RetryCount: -1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := hook.Execute(ctx, map[string]any{"hi": true})
	if err == nil {
		t.Fatal("Execute should fail when context is cancelled during unlimited retries")
	}
	got := int(atomic.LoadInt32(&hits))
	if got < 3 {
		t.Fatalf("server hit %d times, want >= 3 (retry_count=-1 must keep retrying until context cancellation)", got)
	}
}

// TestHTTPHook_NegativeRetryCountDisablesRetries was removed: under the new
// contract -1 means UNLIMITED retries (see TestHTTPHook_UnlimitedRetryCount).
// The explicit "no retries" value is 0 (see TestHTTPHook_ZeroRetryCountMeansNoRetries).

func TestHTTPHook_SyncExecute(t *testing.T) {
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:    srv.URL,
		Method: "POST",
	})

	if err := hook.Execute(context.Background(), map[string]any{"hi": true}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := atomic.LoadInt32(&called); got != 1 {
		t.Fatalf("server called %d times, want 1", got)
	}
}

func TestHTTPHook_AsyncExecute(t *testing.T) {
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:    srv.URL,
		Method: "POST",
		Async:  true,
	})

	// Async Execute returns immediately.
	if err := hook.Execute(context.Background(), map[string]any{"hi": true}); err != nil {
		t.Fatalf("Execute returned error in async mode: %v", err)
	}

	hook.Wait()
	if got := atomic.LoadInt32(&called); got != 1 {
		t.Fatalf("server called %d times, want 1", got)
	}
}

func TestHTTPHook_AsyncRewake(t *testing.T) {
	var serverHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&serverHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	mb := bus.New(nil, slog.Default())
	sub := mb.Subscribe("test-rewake", HookAsyncRewakeTopic)

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:         srv.URL,
		Method:      "POST",
		Async:       true,
		AsyncRewake: true,
	})
	hook.SetBus(mb)
	hook.SetSessionID("test-session-123")
	hook.SetHookType("test_hook")

	if err := hook.Execute(context.Background(), map[string]any{"hi": true}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Wait for async goroutine.
	hook.Wait()

	// Verify rewake bus signal.
	select {
	case msg := <-sub.Channel:
		if msg.Topic != HookAsyncRewakeTopic {
			t.Errorf("rewake topic = %q, want %q", msg.Topic, HookAsyncRewakeTopic)
		}
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload["session_id"] != "test-session-123" {
			t.Errorf("session_id = %v, want test-session-123", payload["session_id"])
		}
		if payload["hook_type"] != "test_hook" {
			t.Errorf("hook_type = %v, want test_hook", payload["hook_type"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rewake signal")
	}

	if got := atomic.LoadInt32(&serverHits); got != 1 {
		t.Fatalf("server called %d times, want 1", got)
	}
}

func TestHTTPHook_AsyncRewake_NilBus(t *testing.T) {
	var serverHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&serverHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// AsyncRewake=true but SetBus never called: hook should still
	// succeed (with warning log), not publish anything.
	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:         srv.URL,
		Method:      "POST",
		Async:       true,
		AsyncRewake: true,
	})

	if err := hook.Execute(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	hook.Wait()
	if got := atomic.LoadInt32(&serverHits); got != 1 {
		t.Fatalf("server called %d times, want 1", got)
	}
}

func TestHTTPHook_SetBus_NilSafe(t *testing.T) {
	hook := &HTTPHook{}
	// Must not panic.
	hook.SetBus((*bus.MessageBus)(nil))
	hook.SetSessionID("")
	hook.SetHookType("")
}

func TestHTTPHook_OnSessionStart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:    srv.URL,
		Method: "POST",
	})
	transform := hook.OnSessionStart(context.Background(), SessionLifecycleState{
		SessionID: "abc",
		AgentID:   "test-agent",
	})
	if transform.Modified {
		t.Error("OnSessionStart should not modify context")
	}
}

func TestHTTPHook_OnSessionEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:    srv.URL,
		Method: "POST",
	})
	err := hook.OnSessionEnd(context.Background(), SessionLifecycleState{
		SessionID: "abc",
	}, SessionLifecycleResult{
		Success: true,
	})
	if err != nil {
		t.Fatalf("OnSessionEnd: %v", err)
	}
}
