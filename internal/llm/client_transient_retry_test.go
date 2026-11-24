package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Transient-transport retry lane (ModelConfig.TransientRetries): a proxy
// that kills its serving worker mid-request (socrates litellm uvicorn
// healthcheck storm, 2026-11-23) leaves an EOF/reset-shaped connection
// death with NO HTTP status. The lane re-dials the SAME provider before
// the error reaches the classification switch, so one hop-death neither
// consumes a shortRetries attempt nor rotates the request to a fallback
// model. Zero budget (default) must be byte-identical to the old behavior.

// flakyBody serves a completion normally; the first N requests instead
// write SSE headers + a chunk, then HARD-CLOSE the connection (the
// "worker died mid-stream" shape).
func flakyEOFHandler(kills *atomic.Int32, killCount int32, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := kills.Add(1)
		if n <= killCount {
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "no hijack", 500)
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				return
			}
			// Die BEFORE answering: the client's RoundTrip sees EOF with
			// no HTTP response (the url.Error shape the worker-kill storm
			// produced on the rig: "request failed: Post ...: EOF").
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func goodBody() string {
	return `{"id":"chatcmpl-ok","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
}

func TestTransientTransportErrorClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"wrapped unexpected EOF", &ClientError{Message: "stream read failed", Cause: io.ErrUnexpectedEOF}, true},
		{"bare unexpected EOF", io.ErrUnexpectedEOF, true},
		{"conn reset op error", &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, true},
		{"wrapped ctx deadline", &ClientError{Message: "request failed", Cause: context.DeadlineExceeded}, false},
		{"api error with status", &APIError{StatusCode: 500, Detail: "boom"}, false},
		{"404 api error", &APIError{StatusCode: 404, Detail: "gone"}, false},
		{"random error", errors.New("something else"), false},
	}
	for _, tc := range tests {
		got := isTransientTransportError(tc.err)
		if got != tc.want {
			t.Errorf("%s: isTransientTransportError(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

func TestChat_TransientRetriesRedialsSameProvider(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(flakyEOFHandler(&hits, 1, goodBody()))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "***",
		MaxTokens:        64,
		TransientRetries: 2,
	}
	c := NewClient(cfg)
	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat failed despite the transport lane: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("content = %q, want hello", resp.Content)
	}
	if hits.Load() != 2 {
		t.Fatalf("provider hit %d times, want 2 (one death + one re-dial)", hits.Load())
	}
}

func TestChat_TransientRetriesZeroIsPassThrough(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(flakyEOFHandler(&hits, 1, goodBody()))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:   "m",
		BaseURL:   srv.URL,
		APIKey:    "***",
		MaxTokens: 64,
		// TransientRetries absent = 0: the lane must never fire.
	}
	c := NewClient(cfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("flag-off run absorbed a transport error (must be byte-identical: surfaces immediately)")
	}
	if hits.Load() != 1 {
		t.Fatalf("provider hit %d times, want exactly 1 per shortRetries attempt", hits.Load())
	}
}

func TestChat_TransientRetriesExhaustedSurfacesTransportError(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(flakyEOFHandler(&hits, 99, goodBody()))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "***",
		MaxTokens:        64,
		TransientRetries: 2,
	}
	c := NewClient(cfg)
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("permanent EOF must eventually surface")
	}
	// Lane consumed first, then the outer default branch returns: the
	// provider saw the initial dial + 2 re-dials = 3 inside ONE outer
	// attempt (the default lane returns the error without further outer
	// retries).
	if hits.Load() != 3 {
		t.Fatalf("provider hit %d times, want 3 (1 + 2 transient re-dials)", hits.Load())
	}
}

func TestChat_CtxCancelDoesNotRedial(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(flakyEOFHandler(&hits, 99, goodBody()))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "***",
		MaxTokens:        64,
		TransientRetries: 3,
	}
	c := NewClient(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already dead: the lane must not spend any re-dial
	_, _ = c.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if hits.Load() > 0 {
		t.Fatalf("provider hit %d times on a dead ctx, want 0", hits.Load())
	}
}

// flakySSEHandler streams SSE chunks; the first killCount requests die
// MID-BODY after one content delta; later requests stream the full answer.
func flakySSEHandler(kills *atomic.Int32, killCount int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := kills.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunk := func(o map[string]any) {
			b, _ := json.Marshal(o)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"role": "assistant"}}}})
		if n <= killCount {
			// die before any CONTENT delta (empty delta content)
			chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": ""}}}})
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, err := hj.Hijack()
				if err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": "done"}}}})
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}}})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}
}

func TestChatWithDeltaCallback_StreamRedialsBeforeFirstDelta(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(flakySSEHandler(&hits, 1))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "***",
		MaxTokens:        64,
		TransientRetries: 2,
	}
	c := NewClient(cfg)
	var got strings.Builder
	resp, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(d string) error { got.WriteString(d); return nil })
	if err != nil {
		t.Fatalf("stream failed despite pre-first-delta re-dial: %v", err)
	}
	if resp.Content != "done" {
		t.Fatalf("content = %q, want done", resp.Content)
	}
	// No duplicated text: the killed stream delivered zero content deltas,
	// the re-dial delivered exactly "done" once.
	if got.String() != "done" {
		t.Fatalf("callback deltas = %q, want exactly %q (no duplication)", got.String(), "done")
	}
}

func TestTransientRetryBudgetWiredFromModelDef(t *testing.T) {
	providers := map[string]ProviderConfig{
		"p": {
			API:     "openai",
			Options: ProviderOptionsConfig{BaseURL: "http://x", APIKey: "k"},
			Models: map[string]ModelDef{
				"m": {
					Name:             "m",
					TransientRetries: 2,
				},
				"z": {Name: "z"},
			},
		},
	}
	mc := modelConfigFrom("p", "m", providers["p"], providers["p"].Models["m"])
	if mc.TransientRetries != 2 {
		t.Fatalf("TransientRetries = %d, want 2 (config key not wired)", mc.TransientRetries)
	}
	mz := modelConfigFrom("p", "z", providers["p"], providers["p"].Models["z"])
	if mz.TransientRetries != 0 {
		t.Fatalf("unset model TransientRetries = %d, want 0 (absent = today's behavior)", mz.TransientRetries)
	}
}

func TestTransientLaneRespectsBackoffCeiling(t *testing.T) {
	// Sanity: a re-dial loop cannot outlive a tight per-attempt deadline.
	// The lane dials immediately (no plan sleep) so it is bounded by the
	// budget count alone; a slow-death server with a 50ms handler stays
	// well under one second for 2 re-dials.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(10 * time.Millisecond)
		hj, _ := w.(http.Hijacker)
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "***",
		MaxTokens:        64,
		TransientRetries: 2,
	}
	c := NewClient(cfg)
	start := time.Now()
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("want error on permanent death")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("lane took %v, want < 5s (3 dials x 10ms + outer default return)", elapsed)
	}
}

// midStreamKillHandler delivers ONE content delta, then closes the
// connection without [DONE]. After the kill budget is spent it streams
// clean answers.
func midStreamKillHandler(kills *atomic.Int32, killCount int32, partial, full string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := kills.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunk := func(o map[string]any) {
			b, _ := json.Marshal(o)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"role": "assistant"}}}})
		if n <= killCount {
			chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": partial}}}})
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
			return
		}
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": full}}}})
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}}})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}
}

// TestChatWithDeltaCallback_MidStreamKillDoesNotRedial pins the DUPLICATION
// GUARD: a stream that already delivered a content delta is never re-dialed
// by the transient lane (a second full answer would concatenate with the
// first). The partial text reaches the callback exactly once and the
// transport error surfaces to the outer loop.
func TestChatWithDeltaCallback_MidStreamKillDoesNotRedial(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(midStreamKillHandler(&hits, 1, "part", "whole"))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "***",
		MaxTokens:        64,
		TransientRetries: 2,
	}
	c := NewClient(cfg)
	var got strings.Builder
	_, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(d string) error { got.WriteString(d); return nil })
	if err == nil {
		t.Fatal("mid-stream kill must surface an error (the guard blocks the re-dial)")
	}
	// The guard fires inside the wrapper: one connection, partial text
	// once. The outer loop's default branch then returns the error.
	if got.String() != "part" {
		t.Fatalf("callback deltas = %q, want exactly %q (no duplicated text, no second answer)", got.String(), "part")
	}
}
