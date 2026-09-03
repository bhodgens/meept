package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// TestNewHTTPHook_DefaultRetryCount: a zero-value (unset) retry_count must
// normalize to 3, matching the repo's other retry defaults (Job MaxRetries=3
// in internal/queue/job.go, retry_recovery.go MaxRetries=3). Regression test
// for the production bug where unset retry_count meant "never retry".
func TestNewHTTPHook_DefaultRetryCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	hook, err := NewHTTPHook(HTTPHookConfig{URL: srv.URL}, []string{srv.URL}, slog.Default())
	if err != nil {
		t.Fatalf("NewHTTPHook: %v", err)
	}
	if hook.config.RetryCount != 3 {
		t.Fatalf("RetryCount = %d, want 3 (constructor default)", hook.config.RetryCount)
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

// TestHTTPHook_TransientFailureRetriesByDefault: with retry_count unset,
// a transient 500 must be retried and the hook must succeed on the second
// attempt. This is the regression test for the production bug: before the
// constructor default existed, RetryCount=0 tripped the loop guard at
// attempt 0 and Execute failed permanently with "after 0 retries".
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

	// RetryCount deliberately unset → constructor default (3) must engage.
	hook := newTestHTTPHook(t, srv, HTTPHookConfig{
		URL:    srv.URL,
		Method: "POST",
	})
	if err := hook.Execute(context.Background(), map[string]any{"hi": true}); err != nil {
		t.Fatalf("Execute should succeed after one transient 500: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("server hit %d times, want 2 (1 failed + 1 retry)", got)
	}
}

// TestHTTPHook_NegativeRetryCountDisablesRetries: negative retry_count is the
// explicit opt-out (0 is indistinguishable from "unset" over the JSON config
// surface, so it normalizes to the default instead). A permanently failing
// server must be attempted exactly once with no backoff sleeps.
func TestHTTPHook_NegativeRetryCountDisablesRetries(t *testing.T) {
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
	err := hook.Execute(context.Background(), map[string]any{"hi": true})
	if err == nil {
		t.Fatal("Execute should fail when server always returns 500")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server hit %d times, want 1 (retries disabled)", got)
	}
}

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
