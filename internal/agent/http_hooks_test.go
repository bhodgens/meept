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
