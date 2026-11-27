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
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
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

// ---------------------------------------------------------------------------
// Bughunt 2026-11-24 fix pins (H7, M1, M3, L3, L4) — landed WITH the restore.
// ---------------------------------------------------------------------------

// nonRetryableTransportStringError carries a transport substring marker in
// its message AND declares NonRetryable() (the L3 shape: QuotaResetError and
// friends expose NonRetryable; a synthetic string-matching error proves the
// guard runs BEFORE the substring block).
type nonRetryableTransportStringError struct{}

func (nonRetryableTransportStringError) Error() string {
	return "stream read failed: unexpected EOF"
}
func (nonRetryableTransportStringError) NonRetryable() bool { return true }

// Pin L3: a NonRetryable error whose message matches the substring markers
// must NOT classify as transient — the explicit NonRetryable() guard exits
// before the substring block.
func TestTransientTransportError_NonRetryableBeatsSubstringMarkers(t *testing.T) {
	err := nonRetryableTransportStringError{}
	if isTransientTransportError(err) {
		t.Fatal("NonRetryable error with EOF/stream markers classified transient (L3 guard missing)")
	}
	// wrapped: errors.AsType must see it through a ClientError wrapper
	if isTransientTransportError(&ClientError{Message: "wrap", Cause: err}) {
		t.Fatal("wrapped NonRetryable error classified transient (L3 guard does not unwrap)")
	}
	// control: the same message without NonRetryable() still classifies
	if !isTransientTransportError(&ClientError{Message: "stream read failed: unexpected EOF", Cause: io.ErrUnexpectedEOF}) {
		t.Fatal("control: plain transport error must still classify transient")
	}
}

// redialProbeClient wraps a dispatch closure in the streaming lane wrapper so
// a pin can count calls without an HTTP server (unit-level guard proof).
func TestStreamLane_ReasoningOnlyDeathNotRedialed(t *testing.T) {
	// Pin H7 (unit): the duplication guard must fire on REASONING-only
	// partial state. Build retryState exactly as savePartialState leaves it
	// after a reasoning-then-death attempt (no content, no tool calls).
	var calls atomic.Int32
	state := &streamRetryState{toolCallAccums: map[int]*toolCallAccum{}}
	state.reasoning.WriteString("deep thought...") // delivered reasoning deltas
	c := NewClient(&ModelConfig{ModelID: "m", TransientRetries: 3})
	_, _, err := c.doStreamRequestRetryTransport(context.Background(),
		&ModelConfig{ModelID: "m", TransientRetries: 3}, state,
		func() (*Response, int, error) {
			calls.Add(1)
			return nil, 0, &ClientError{Message: "stream read failed", Cause: io.ErrUnexpectedEOF}
		})
	if err == nil {
		t.Fatal("want transport error surfaced")
	}
	if calls.Load() != 1 {
		t.Fatalf("lane dialed %d times, want 1: reasoning-only stream must NOT be re-dialed (H7)", calls.Load())
	}
}

// reasoningKillHandler delivers ONE reasoning delta per request, then dies.
// killCount requests die; after that, streams a clean content answer.
func reasoningKillHandler(kills *atomic.Int32, killCount int32, reasoning, full string) http.HandlerFunc {
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
			// reasoning delta (the H7 shape: thinking tokens, no content)
			chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"reasoning_content": reasoning}}}})
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

// Pin H7 (e2e-through-client): a stream that delivered REASONING deltas then
// died on the transport is NOT re-dialed by the transient lane.
func TestChatWithDeltaCallback_ReasoningStreamKillDoesNotRedial(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(reasoningKillHandler(&hits, 1, "pondering", "whole"))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "k",
		MaxTokens:        64,
		TransientRetries: 2,
	}
	c := NewClient(cfg)
	_, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(d string) error { return nil })
	if err == nil {
		t.Fatal("reasoning-stream kill must surface the transport error")
	}
	// Pre-H7 the guard only checked content+tool calls: the lane would have
	// burned the re-dial (2 hits). Post-H7: exactly one dial, error surfaces.
	if hits.Load() != 1 {
		t.Fatalf("provider hit %d times, want 1 (H7: reasoning-only deltas block the re-dial)", hits.Load())
	}
}

// Pin H7 (replay): saved reasoning carries into the resumed attempt's
// Response.Reasoning via the pre-populate twin (mirrors the content replay).
func TestDoStreamRequest_ResumedAttemptReplaysSavedReasoning(t *testing.T) {
	// green stream that ends cleanly with content + usage
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + `{"id":"x","choices":[{"index":0,"delta":{"content":"tail"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	cfg := &ModelConfig{ModelID: "m", BaseURL: srv.URL, MaxTokens: 64}
	c := NewClient(cfg)
	body, err := json.Marshal(map[string]any{"model": "m", "messages": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	state := &streamRetryState{
		toolCallAccums: map[int]*toolCallAccum{},
		isResume:       true,
	}
	state.reasoning.WriteString("saved-thought")
	resp, _, err := c.doStreamRequest(context.Background(), body, func(string) error { return nil }, state, cfg, false, "", "", &chatOptions{})
	if err != nil {
		t.Fatalf("doStreamRequest: %v", err)
	}
	if !strings.Contains(resp.Reasoning, "saved-thought") {
		t.Fatalf("resumed response lost saved reasoning: %q", resp.Reasoning)
	}
}

// Pin M1: an alias-selected model's transient_retries governs the lane —
// withRequestModelOverride must carry the budget into the effective copy
// (pre-fix, only costs rode along, so the client DEFAULT model's budget
// silently applied to alias-served calls).
func TestWithRequestModelOverride_CarriesTransientRetries(t *testing.T) {
	clientCfg := &ModelConfig{ProviderID: "p", ModelID: "default", TransientRetries: 0}
	alias := &ModelConfig{ProviderID: "p", ModelID: "flaky", TransientRetries: 3, CostPerMillionInput: 1, CostPerMillionOutput: 2}

	// alias lane (WithResolvedModel)
	var o chatOptions
	WithResolvedModel(alias)(&o)
	eff := withRequestModelOverride(clientCfg, &o)
	if eff.TransientRetries != 3 {
		t.Fatalf("alias-selected budget = %d, want 3 (M1: key is a no-op without the carry)", eff.TransientRetries)
	}
	if eff.ModelID != "flaky" {
		t.Fatal("override did not apply")
	}

	// user-directive lane (WithModelOverride)
	var o2 chatOptions
	WithModelOverride(alias)(&o2)
	eff2 := withRequestModelOverride(clientCfg, &o2)
	if eff2.TransientRetries != 3 {
		t.Fatalf("directive-selected budget = %d, want 3 (M1 user lane)", eff2.TransientRetries)
	}

	// zero-is-real: alias declaring no budget must NOT inherit the client's
	clientFlaky := &ModelConfig{ProviderID: "p", ModelID: "default", TransientRetries: 5}
	var o3 chatOptions
	WithResolvedModel(&ModelConfig{ProviderID: "p", ModelID: "calm"})(&o3)
	eff3 := withRequestModelOverride(clientFlaky, &o3)
	if eff3.TransientRetries != 0 {
		t.Fatalf("unbudgeted alias inherited client budget %d, want 0", eff3.TransientRetries)
	}
}

// Pin M1 (behavioural): with the client default budget ZERO and the
// alias-selected model budget 2, an alias-served call re-dials once and
// succeeds. Pre-M1 this pins red: the lane read the default's 0 and
// surfaced the EOF instead of re-dialing.
func TestChatWithDeltaCallback_AliasSelectedBudgetGovernsLane(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(flakySSEHandler(&hits, 1))
	defer srv.Close()

	// client cfg has NO transient budget; the RESOLVED model carries it.
	cfg := &ModelConfig{ProviderID: "p", ModelID: "default", BaseURL: srv.URL, MaxTokens: 64, TransientRetries: 0}
	c := NewClient(cfg)
	aliasMC := &ModelConfig{ProviderID: "p", ModelID: "flaky-alias", TransientRetries: 2}
	_, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error { return nil },
		WithResolvedModel(aliasMC))
	if err != nil {
		t.Fatalf("alias budget (2) must fund the re-dial of the pre-first-delta kill: %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("provider hit %d times, want 2 (1 death + 1 re-dial funded by the ALIAS model's key)", hits.Load())
	}
}

// Pin M3: a streaming attempt that dies mid-body writes a failure-shaped
// metrics row (provider_requests), mirroring the non-streaming doRequest
// failure row. Pre-M3 the streaming death wrote NOTHING.
func TestChatWithDeltaCallback_StreamDeathWritesMetricsRow(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(midStreamKillHandler(&hits, 99, "part", "whole"))
	defer srv.Close()

	store, err := newTestMetricsStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &ModelConfig{
		ProviderID: "p",
		ModelID:    "m",
		BaseURL:    srv.URL,
		APIKey:     "k",
		MaxTokens:  64,
		// budget 0 + mid-stream deltas: guard blocks the re-dial, error
		// surfaces — exactly the died-attempt shape the row must capture.
	}
	c := NewClient(cfg, WithMetricsStore(store))
	_, err = c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error { return nil })
	if err == nil {
		t.Fatal("permanent mid-stream kill must surface an error")
	}

	// Record() is async; poll the row count up.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := store.RefreshStats(context.Background()); err != nil {
			t.Fatalf("RefreshStats: %v", err)
		}
		stats, err := store.GetStats(context.Background(), "p", "m", 1)
		if err != nil {
			t.Fatalf("GetStats: %v", err)
		}
		if stats != nil && stats.RequestCount > 0 && stats.ErrorCount > 0 {
			return // the died streaming attempt ledgered a failure row
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no provider_requests failure row for the died streaming attempt (M3)")
}

func newTestMetricsStore(t *testing.T) (*metrics.Store, error) {
	t.Helper()
	storeCfg := metrics.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "metrics.db"),
		RetentionDays:    1,
		StatsWindowHours: 1,
		RefreshInterval:  time.Minute,
	}
	store, err := metrics.NewStore(storeCfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Initialize(context.Background()); err != nil {
		store.Close()
		t.Fatalf("Initialize: %v", err)
	}
	// recordWorker consumes Record() only after StartBackground (mirrors
	// the anthropic_test metrics harness).
	store.StartBackground(context.Background())
	return store, nil
}

// Pin L4: the outer short-retry loop must hand each attempt a FRESH
// retryState, so deltas saved by a CANCELLED/partial earlier attempt cannot
// suppress a later legitimate pre-first-delta re-dial. Sequence through the
// real client: attempt 1 serves an empty completion (the loop continues,
// retryState still live), attempt 2 dies BEFORE its first delta
// (legitimate re-dial), attempt 3 answers. With a shared/stale retryState
// carrying deltas, the guard would block the attempt-2 re-dial.
func TestChatWithDeltaCallback_EmptyRetryThenTransportDeathRedialed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunk := func(o map[string]any) {
			b, _ := json.Marshal(o)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
		switch n {
		case 1:
			// empty completion: role chunk + [DONE], no content
			chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"role": "assistant"}}}})
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			// pre-first-delta transport death (would be blocked by stale
			// partial state under the pre-L4 shared-retryState shape)
			chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"role": "assistant"}}}})
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
		default:
			// clean answer
			chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": "at-last"}}}})
			chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}}})
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		}
	}))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "k",
		MaxTokens:        64,
		TransientRetries: 2,
	}
	c := NewClient(cfg)
	var got strings.Builder
	resp, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(d string) error { got.WriteString(d); return nil })
	if err != nil {
		t.Fatalf("empty-retry then transport death must recover via re-dial: %v", err)
	}
	if resp.Content != "at-last" || got.String() != "at-last" {
		t.Fatalf("content=%q callback=%q, want at-last once", resp.Content, got.String())
	}
}

// Pin L4 (unit twin): the wrapper's guard reads ONLY the state it is handed,
// so the loop's fresh-per-attempt construction is what makes a stale saved
// page invisible to the next attempt. Directly: stale accumulated state
// blocks (the B-03/H7 correctness), fresh zeroed state re-dials (L4's loop
// invariant).
func TestStreamLane_StaleStateBlocksButFreshAttemptStateRedialed(t *testing.T) {
	cfg := &ModelConfig{ModelID: "m", TransientRetries: 2}
	c := NewClient(cfg)

	// stale accumulated (a prior attempt's delivered page) — the guard must
	// block THIS state's lane:
	stale := &streamRetryState{toolCallAccums: map[int]*toolCallAccum{}}
	stale.accumulated.WriteString("prior page")
	var calls atomic.Int32
	_, _, _ = c.doStreamRequestRetryTransport(context.Background(), cfg, stale,
		func() (*Response, int, error) {
			calls.Add(1)
			return nil, 0, &ClientError{Message: "stream read failed", Cause: io.ErrUnexpectedEOF}
		})
	if calls.Load() != 1 {
		t.Fatalf("stale-state lane dialed %d times, want 1 (duplication guard)", calls.Load())
	}

	// fresh state (what the loop now builds per attempt) — the lane must
	// spend its budget:
	fresh := &streamRetryState{toolCallAccums: map[int]*toolCallAccum{}}
	calls.Store(0)
	_, _, _ = c.doStreamRequestRetryTransport(context.Background(), cfg, fresh,
		func() (*Response, int, error) {
			calls.Add(1)
			return nil, 0, &ClientError{Message: "stream read failed", Cause: io.ErrUnexpectedEOF}
		})
	if calls.Load() != 3 { // 1 dial + 2 re-dials
		t.Fatalf("fresh-state lane dialed %d times, want 3 (budget re-armed on fresh state — L4)", calls.Load())
	}
}

// ---------------------------------------------------------------------------
// Bughunt 2026-11-27 fix-wave F5 pins (M2 + L9 + L10).
// ---------------------------------------------------------------------------

// ttsrAbortServer serves one content delta then keeps the connection
// open (streaming context); the ABORT comes from the callback itself —
// the exact wrappedOnDelta shape (loop.go): the TTSR rule condition is
// user-authored and here contains "EOF", so pre-fix the abort error's
// message classified transient through the bare "EOF" marker AND the
// duplication guard saw zero saved deltas (the onDelta exit skipped
// savePartialState), so the lane RE-DIALED and re-emitted the prefix.
func ttsrAbortServer(hits *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
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
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": "partial "}}}})
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": "more"}}}})
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}}})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}
}

// TestStreamLane_TTSRAbortAfterDeltaNotRedialed (M2): a mid-stream TTSR
// abort (onDelta error) must (a) never re-dial — the duplication guard
// must see the saved partial state — and (b) never be retried — the abort
// is NonRetryable. Driven through the REAL ChatWithDeltaCallback lane
// over httptest, with the abort injected via the callback exactly as the
// agent loop's wrappedOnDelta does it.
func TestStreamLane_TTSRAbortAfterDeltaNotRedialed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(ttsrAbortServer(&hits))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "k",
		MaxTokens:        64,
		TransientRetries: 3,
	}
	c := NewClient(cfg)
	var got strings.Builder
	var err error
	_, err = c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(d string) error {
			got.WriteString(d)
			if strings.Contains(got.String(), "partial") {
				// The wrappedOnDelta shape: rule condition containing EOF.
				return &StreamAbortedError{RuleName: "heredoc-guard", Reason: `pattern "\bEOF\b" matched in delta`}
			}
			return nil
		})
	if err == nil {
		t.Fatal("TTSR abort must surface")
	}
	var sabort *StreamAbortedError
	if !errors.As(err, &sabort) {
		t.Fatalf("err = %v, want *StreamAbortedError", err)
	}
	if hits := hits.Load(); hits != 1 {
		t.Fatalf("provider hit %d times, want exactly 1 (duplication guard must see the saved partial state AND the abort must be NonRetryable — M2)", hits)
	}
	if got.String() != "partial " {
		t.Fatalf("deltas = %q, want exactly the one delivered prefix (no re-emission)", got.String())
	}
}

// TestStreamLane_CallerErrorWithTransportMarkerAfterDeltaNotRedialed
// (M2, save half): a CALLER-side onDelta error (not a TTSR abort — any
// consumer failure, e.g. a dead WS connection) whose message happens to
// carry a transport marker must never re-dial once a delta was
// delivered. This shape is NOT covered by the StreamAbortedError
// NonRetryable guard: the error is an arbitrary caller error, so the
// duplication guard's saved partial state (savePartialState on the
// onDelta exit) is the ONLY thing blocking the re-dial.
func TestStreamLane_CallerErrorWithTransportMarkerAfterDeltaNotRedialed(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(ttsrAbortServer(&hits))
	defer srv.Close()

	cfg := &ModelConfig{
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "k",
		MaxTokens:        64,
		TransientRetries: 3,
	}
	c := NewClient(cfg)
	var got strings.Builder
	callerErr := errors.New("consumer write failed: broken pipe")
	_, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(d string) error {
			got.WriteString(d)
			if strings.Contains(got.String(), "partial") {
				return callerErr
			}
			return nil
		})
	if !errors.Is(err, callerErr) {
		t.Fatalf("err = %v, want the caller error", err)
	}
	if hits := hits.Load(); hits != 1 {
		t.Fatalf("provider hit %d times, want exactly 1 (the saved partial state must block the re-dial — M2 save half)", hits)
	}
	if got.String() != "partial " {
		t.Fatalf("deltas = %q, want exactly the one delivered prefix", got.String())
	}
}

// TestStreamLane_TTSRAbortIsNotRetriedAsTransport pins the NonRetryable()
// declaration directly at the lane's classification seam.
func TestStreamLane_TTSRAbortIsNotRetriedAsTransport(t *testing.T) {
	abort := &StreamAbortedError{RuleName: "r", Reason: `pattern "\bEOF\b" matched in delta`}
	if isTransientTransportError(abort) {
		t.Fatal("StreamAbortedError carrying an EOF substring classified transient (M2: NonRetryable guard missing)")
	}
	if !abort.NonRetryable() {
		t.Fatal("StreamAbortedError must declare NonRetryable (M2)")
	}
	var target NonRetryableError
	if !errors.As(abort, &target) || !target.NonRetryable() {
		t.Fatal("errors.As must reach the NonRetryable declaration through the concrete error")
	}
}

// TestTransientTransportError_StreamAbortedErrorNeverTransient (L9): the
// classification table pin for the narrowed substring list.
func TestTransientTransportError_StreamAbortedErrorNeverTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		// L9: the bare 3-char "EOF" marker no longer matches any untyped
		// message; the real wire shape is the wrapped "unexpected EOF".
		{"untyped EOF-only message", errors.New("read TCP: EOF"), false},
		{"untyped unexpected EOF", errors.New("stream read failed: unexpected EOF"), true},
		{"wrapped unexpected EOF", &ClientError{Message: "stream read failed", Cause: errors.New("unexpected EOF")}, true},
		{"typed io.EOF still transient", io.EOF, true},
		{"typed io.ErrUnexpectedEOF still transient", io.ErrUnexpectedEOF, true},
		{"ttsr abort with EOF condition", &StreamAbortedError{RuleName: "g", Reason: `pattern "\bEOF\b" matched`}, false},
		{"random untyped error", errors.New("context mojo EOF vibes"), false},
	}
	for _, tc := range cases {
		if got := isTransientTransportError(tc.err); got != tc.want {
			t.Errorf("%s: isTransientTransportError(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

// preResponseKillHandler kills the connection BEFORE any HTTP response
// for the first killCount requests (the "worker killed before answering"
// shape — the lane's founding incident), then streams a proper SSE
// answer (ChatWithDeltaCallback consumes SSE, not the JSON body).
func preResponseKillHandler(kills *atomic.Int32, killCount int32, content string) http.HandlerFunc {
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
			_ = conn.Close() // die BEFORE answering: EOF, no status
			return
		}
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
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{"content": content}}}})
		chunk(map[string]any{"id": "x", "choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}}})
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}
}

// TestChatWithDeltaCallback_PreResponseDeathWritesMetricsRow (L10): a
// streaming transport death BEFORE the response headers must write the
// failure-shaped provider_requests row (previously dark; M3's mirror
// covers only the mid-body scanner.Err branch).
func TestChatWithDeltaCallback_PreResponseDeathWritesMetricsRow(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(preResponseKillHandler(&hits, 2, "hello"))
	defer srv.Close()

	store, err := newTestMetricsStore(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &ModelConfig{
		ProviderID:       "p",
		ModelID:          "m",
		BaseURL:          srv.URL,
		APIKey:           "k",
		MaxTokens:        64,
		TransientRetries: 2,
	}
	c := NewClient(cfg, WithMetricsStore(store))
	resp, err := c.ChatWithDeltaCallback(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hi"}},
		func(string) error { return nil })
	if err != nil {
		t.Fatalf("lane must absorb the pre-response deaths and succeed: %v", err)
	}
	if resp == nil || resp.Content != "hello" {
		t.Fatalf("content = %+v, want the recovered answer", resp)
	}
	if hits.Load() != 3 {
		t.Fatalf("provider hit %d times, want 3 (two deaths + one recovery)", hits.Load())
	}

	// Exactly one failure-shaped row per dead dial: 2 deaths -> 2 error
	// rows, plus the recovery's success row -> RequestCount 3.
	// Record() is async; poll the row counts up.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rerr := store.RefreshStats(context.Background()); rerr != nil {
			t.Fatalf("RefreshStats: %v", rerr)
		}
		stats, serr := store.GetStats(context.Background(), "p", "m", 1)
		if serr != nil {
			t.Fatalf("GetStats: %v", serr)
		}
		if stats != nil && stats.RequestCount == 3 && stats.ErrorCount == 2 {
			return // the pre-response deaths each ledgered a failure row
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("pre-response stream deaths did not write failure metrics rows (L10)")
}
