package hermes

// Pin tests for the three parent-verified defects in the hermes client:
//
//  1. do()/doHeaders() must require 2xx when expectStatus is omitted —
//     a 500 on StopRun, a 409 on SetSessionModel, and a 401 on
//     Capabilities must surface classified errors (and Capabilities must
//     NOT cache the failure).
//  2. StreamChat must send X-Hermes-Session-Key like the sync chat path.
//  3. Capabilities() must not hold capsMu across the HTTP request: a
//     second caller with a cancelled context returns promptly instead of
//     blocking behind an in-flight first fetch.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// --- finding 1: non-2xx without expectStatus must classify -------------------

func TestClient_StopRun_500ClassifiesError(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-abc/stop", http.StatusInternalServerError, `{"error":"boom"}`)
	c := newTestClient(s.srv.URL)
	err := c.StopRun(context.Background(), "run-abc")
	if err == nil {
		t.Fatal("StopRun on 500: want error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("StopRun on 500: want *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", apiErr.StatusCode)
	}
}

func TestClient_SteerRun_500ClassifiesError(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-abc/steer", http.StatusInternalServerError, `{"error":"boom"}`)
	c := newTestClient(s.srv.URL)
	err := c.SteerRun(context.Background(), "run-abc", "go left")
	if err == nil {
		t.Fatal("SteerRun on 500: want error, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("SteerRun on 500: want *APIError{500}, got %v", err)
	}
}

func TestClient_RunStatus_500ClassifiesError(t *testing.T) {
	s := newStubServer(t)
	s.handle("/v1/runs/run-abc", http.StatusInternalServerError, `{"error":"boom"}`)
	c := newTestClient(s.srv.URL)
	st, err := c.RunStatus(context.Background(), "run-abc")
	if err == nil {
		t.Fatalf("RunStatus on 500: want error, got nil (status %+v)", st)
	}
	if st != nil {
		t.Errorf("RunStatus on 500: want nil result, got %+v", st)
	}
}

func TestClient_SetSessionModel_409ClassifiesError(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/s1/model", http.StatusConflict,
		`{"error":{"message":"model lock unavailable","code":"model_lock_unavailable"}}`)
	c := newTestClient(s.srv.URL)
	res, err := c.SetSessionModel(context.Background(), "s1", "zai", "glm-5.3")
	if err == nil {
		t.Fatalf("SetSessionModel on 409: want error, got nil (res %+v)", res)
	}
	if res != nil {
		t.Errorf("SetSessionModel on 409: want nil result, got %+v", res)
	}
	// 409 without code=session_exists classifies through the shared
	// conflict path (ErrHermesSessionForeign family).
	if !errors.Is(err, ErrHermesSessionForeign) {
		t.Errorf("SetSessionModel on 409: want ErrHermesSessionForeign, got %v", err)
	}
}

func TestClient_SessionMessages_500ClassifiesError(t *testing.T) {
	s := newStubServer(t)
	s.handle("/api/sessions/s1/messages", http.StatusInternalServerError, `{"error":"boom"}`)
	c := newTestClient(s.srv.URL)
	msgs, err := c.SessionMessages(context.Background(), "s1", 0)
	if err == nil {
		t.Fatalf("SessionMessages on 500: want error, got nil (msgs %v)", msgs)
	}
	if msgs != nil {
		t.Errorf("SessionMessages on 500: want nil list, got %v", msgs)
	}
}

func TestClient_Capabilities_401ErrorsAndDoesNotCache(t *testing.T) {
	var mu sync.Mutex
	fail := true
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		shouldFail := fail
		mu.Unlock()
		if shouldFail {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"features":{"runs":true},"endpoints":{}}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.URL)

	_, err := c.Capabilities(context.Background())
	if err == nil {
		t.Fatal("Capabilities on 401: want error, got nil")
	}
	if !errors.Is(err, ErrHermesAuth) {
		t.Errorf("Capabilities on 401: want ErrHermesAuth, got %v", err)
	}

	// The failed probe must NOT be cached: once the gateway recovers, the
	// very next call re-probes and succeeds with real features.
	mu.Lock()
	fail = false
	mu.Unlock()
	caps, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities after recovery: %v", err)
	}
	if !caps.HasFeature("runs") {
		t.Error("Capabilities after recovery: cached empty document instead of re-probing")
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 2 {
		t.Errorf("server calls = %d, want 2 (failure not cached)", gotCalls)
	}
}

// --- finding 2: StreamChat must send X-Hermes-Session-Key --------------------

func TestStreamChat_SessionKeyHeader(t *testing.T) {
	gotKey := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey <- r.Header.Get("X-Hermes-Session-Key")
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			panic("httptest response is not a flusher")
		}
		_, _ = w.Write([]byte("event: run.completed\ndata: {\"run_id\":\"r1\"}\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := c.StreamChat(ctx, "s1", "hello", ChatOptions{SessionKey: "mem-scope-42"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	for range ch { // drain to terminal
	}

	select {
	case key := <-gotKey:
		if key != "mem-scope-42" {
			t.Errorf("X-Hermes-Session-Key = %q, want %q", key, "mem-scope-42")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stub never saw the stream request")
	}
}

func TestStreamChat_NoSessionKeyHeaderWhenEmpty(t *testing.T) {
	gotHeader := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present := r.Header["X-Hermes-Session-Key"]
		gotHeader <- present
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("event: run.completed\ndata: {}\n\n"))
		w.(http.Flusher).Flush()
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := c.StreamChat(ctx, "s1", "hello", ChatOptions{})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	for range ch {
	}
	select {
	case present := <-gotHeader:
		if present {
			t.Error("empty SessionKey sent an X-Hermes-Session-Key header; want none")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stub never saw the stream request")
	}
}

func TestStreamChat_SessionKeyRejectsControlChars(t *testing.T) {
	c := newTestClient("http://127.0.0.1:1")
	_, err := c.StreamChat(context.Background(), "s1", "hello", ChatOptions{SessionKey: "bad\r\nkey"})
	if err == nil {
		t.Fatal("StreamChat with control chars in SessionKey: want local refusal, got nil")
	}
}

// --- finding 3: Capabilities must not hold capsMu across the HTTP request ----

func TestClient_Capabilities_NoMutexAcrossRequest(t *testing.T) {
	release := make(chan struct{})
	inFlight := make(chan struct{}, 1)
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(inFlight) })
		<-release // hold the first request open
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"features":{"runs":true},"endpoints":{}}`))
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		srv.Close()
	})

	c := newTestClient(srv.URL)

	// Caller A: blocks in the slow stub.
	errA := make(chan error, 1)
	go func() {
		_, err := c.Capabilities(context.Background())
		errA <- err
	}()
	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatal("first caller never reached the stub")
	}

	// Caller B: cancelled context. It must fail promptly on its own
	// request, NOT block on capsMu behind caller A.
	ctxB, cancelB := context.WithCancel(context.Background())
	cancelB()
	start := time.Now()
	doneB := make(chan error, 1)
	go func() {
		_, err := c.Capabilities(ctxB)
		doneB <- err
	}()
	select {
	case err := <-doneB:
		if err == nil {
			t.Error("caller B with cancelled ctx: want error, got nil")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("caller B took %v; it blocked behind caller A's in-flight request", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("caller B blocked >2s behind the in-flight fetch (mutex held across I/O)")
	}

	close(release)
	select {
	case err := <-errA:
		if err != nil {
			t.Errorf("caller A: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller A never returned")
	}
}
