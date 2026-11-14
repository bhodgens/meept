package llm

// Gate tests for the context-warm seam (phase2-context-warm leaf 01,
// root Contract C4). White-box: package llm, fake OpenAI-compatible
// upstream via httptest (the pattern codex_sse_test.go uses).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- fake upstream + log capture harness ---

// warmRecorded is one request the fake upstream saw.
type warmRecorded struct {
	Path   string
	Header http.Header
	Body   map[string]any
}

// warmUpstream is an httptest OpenAI-compatible recorder. handler may be nil
// (deterministic 1-token completion).
func warmUpstream(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, func() []warmRecorded) {
	t.Helper()
	var (
		mu   sync.Mutex
		recs []warmRecorded
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Hand the handler a fresh reader over the same bytes.
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		mu.Lock()
		recs = append(recs, warmRecorded{Path: r.URL.Path, Header: r.Header.Clone(), Body: parsed})
		mu.Unlock()
		if handler != nil {
			handler(w, r)
			return
		}
		resp := map[string]any{
			"id":     "chatcmpl-warm",
			"object": "chat.completion",
			"model":  "test-model",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "w"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 1, "total_tokens": 8},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	snapshot := func() []warmRecorded {
		mu.Lock()
		defer mu.Unlock()
		out := make([]warmRecorded, len(recs))
		copy(out, recs)
		return out
	}
	return srv, snapshot
}

// lockedBuilder collects slog output safely.
type lockedBuilder struct {
	mu sync.Mutex
	sb strings.Builder
}

func (w *lockedBuilder) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sb.Write(p)
}

func (w *lockedBuilder) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sb.String()
}

// debugLogger returns a DEBUG-level capture logger.
func debugLogger() (*lockedBuilder, *slog.Logger) {
	w := &lockedBuilder{}
	return w, slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func warmTestClient(t *testing.T, baseURL string, opts ...ClientOption) *Client {
	t.Helper()
	cfg := &ModelConfig{
		BaseURL:    baseURL + "/v1",
		ModelID:    "test-model",
		ProviderID: "openai-test",
		MaxTokens:  4096,
	}
	all := append([]ClientOption{WithLogger(slog.New(slog.DiscardHandler))}, opts...)
	return NewClient(cfg, all...)
}

func warmAffinity() *AffinityConfig {
	return &AffinityConfig{Enabled: true, Header: "session_id"}
}

func warmMessages() []ChatMessage {
	return []ChatMessage{
		{Role: RoleSystem, Content: "You are a terse voice assistant."},
		{Role: RoleUser, Content: "What is the weather in Denver right now, in one sentence?"},
	}
}

// waitFor polls cond until true or the deadline; returns the final verdict.
func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// laneIdle reports whether the client's warm lane has no active handle.
func laneIdle(c *Client) bool {
	l := c.lane()
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active == nil
}

func jsonAny(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<unmarshalable>"
	}
	return string(b)
}

// --- C4 signature pin ---

// warmFnSig is the C4 contract shape (pin type: if Client.Warm's signature
// drifts, the assignment below stops compiling).
type warmFnSig = func(ctx context.Context, alias string, messages []ChatMessage, sessionScope string)

// TestWarmSignature_C4 pins the frozen contract shape (minus the codebase's
// real message type name, ChatMessage) by assigning the method value to a
// func variable of the exact signature — a compile-time check.
func TestWarmSignature_C4(t *testing.T) {
	c := NewClient(&ModelConfig{BaseURL: "http://127.0.0.1:0/v1", ModelID: "m", ProviderID: "p"})
	// The explicit warmFnSig annotation IS the contract pin (drop it and
	// the check silently stops checking C4), so QF1011's "omit type" is
	// deliberately declined here.
	var fn warmFnSig = c.Warm //nolint:staticcheck // QF1011: explicit type is the point
	if name := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name(); !strings.Contains(name, "llm.(*Client).Warm") {
		t.Fatalf("Client.Warm method value resolved to %s", name)
	}
	// Non-OpenAI-compat classes expose the same seam shape (no-op).
	var _ = (*AnthropicClient)(nil).Warm
	var _ = (*CodexClient)(nil).Warm
}

// --- payload shape + prefix identity ---

// TestWarm_PayloadShape pins max_tokens=1, non-streaming, the prompt_cache_key
// lane, and the session-affinity header on the warm request.
func TestWarm_PayloadShape(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	c := warmTestClient(t, srv.URL)
	c.SetSessionAffinityConfig(warmAffinity())

	c.Warm(context.Background(), "voice-fast", warmMessages(), "sess-42")

	if !waitFor(func() bool { return len(recs()) == 1 }, 3*time.Second) {
		t.Fatalf("warm request never reached upstream; got %d requests", len(recs()))
	}
	req := recs()[0]
	if req.Path != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", req.Path)
	}
	if mt, _ := req.Body["max_tokens"].(float64); mt != 1 {
		t.Errorf("max_tokens = %v, want 1", req.Body["max_tokens"])
	}
	if st, ok := req.Body["stream"].(bool); !ok || st {
		t.Errorf("stream = %v, want explicit false (non-streaming)", req.Body["stream"])
	}
	if pk, _ := req.Body["prompt_cache_key"].(string); pk != "sess-42" {
		t.Errorf("prompt_cache_key = %q, want sess-42", pk)
	}
	if h := req.Header.Get("session_id"); h != "sess-42" {
		t.Errorf("affinity header = %q, want sess-42", h)
	}
	if _, has := req.Body["tools"]; has {
		t.Error("warm payload must carry no tools")
	}
}

// TestWarm_EmptyScopeSendsNoAffinity pins the inherited client rule: an empty
// scope sends NO affinity header and NO prompt_cache_key (never empty values).
func TestWarm_EmptyScopeSendsNoAffinity(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	c := warmTestClient(t, srv.URL)
	c.SetSessionAffinityConfig(warmAffinity())

	c.Warm(context.Background(), "voice-fast", warmMessages(), "")

	if !waitFor(func() bool { return len(recs()) == 1 }, 3*time.Second) {
		t.Fatal("warm request never reached upstream")
	}
	req := recs()[0]
	if _, has := req.Body["prompt_cache_key"]; has {
		t.Error("prompt_cache_key present for empty scope")
	}
	if req.Header.Get("session_id") != "" {
		t.Error("empty affinity header sent")
	}
}

// TestWarm_PrefixIdentity byte-compares the warm request against the SAME
// turn's real Chat request — the whole point of the seam: every payload key
// except the two intentional deltas (max_tokens, stream) must be identical,
// or the provider cache never hits.
func TestWarm_PrefixIdentity(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	c := warmTestClient(t, srv.URL)
	c.SetSessionAffinityConfig(warmAffinity())
	msgs := warmMessages()

	// Real turn first (request #1).
	if _, err := c.Chat(context.Background(), msgs, WithTaskScope("task-1", "sess-42")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	// Warm of the same prefix (request #2).
	c.Warm(context.Background(), "voice-fast", msgs, "sess-42")
	if !waitFor(func() bool { return len(recs()) == 2 }, 3*time.Second) {
		t.Fatalf("expected 2 requests, got %d", len(recs()))
	}
	turn, warm := recs()[0], recs()[1]

	// Messages render byte-identical (the prefix itself).
	if got, want := jsonAny(warm.Body["messages"]), jsonAny(turn.Body["messages"]); got != want {
		t.Errorf("warm messages differ from turn messages:\n warm=%s\n turn=%s", got, want)
	}
	// Every other key is identical; only max_tokens/stream may differ.
	for k, wv := range warm.Body {
		if k == "max_tokens" || k == "stream" {
			continue
		}
		if tv, has := turn.Body[k]; !has || jsonAny(tv) != jsonAny(wv) {
			t.Errorf("payload key %q differs: warm=%s turn=%s", k, jsonAny(wv), jsonAny(tv))
		}
	}
	for k := range turn.Body {
		if k == "max_tokens" || k == "stream" {
			continue
		}
		if _, has := warm.Body[k]; !has {
			t.Errorf("turn payload key %q missing from warm payload", k)
		}
	}
	if mt, _ := turn.Body["max_tokens"].(float64); mt != 4096 {
		t.Errorf("turn max_tokens = %v, want config 4096", mt)
	}
	if mt, _ := warm.Body["max_tokens"].(float64); mt != 1 {
		t.Errorf("warm max_tokens = %v, want 1", mt)
	}
}

// TestWarm_SystemCacheBoundaryStripped pins that the warm path shares the
// buildChatRequest boundary strip (prefix identity across the two paths).
func TestWarm_SystemCacheBoundaryStripped(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	c := warmTestClient(t, srv.URL)
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "static half" + PromptCacheBoundary + " dynamic half"},
		{Role: RoleUser, Content: "hi"},
	}
	c.Warm(context.Background(), "a", msgs, "s")
	if !waitFor(func() bool { return len(recs()) == 1 }, 3*time.Second) {
		t.Fatal("no warm request")
	}
	sysList, _ := recs()[0].Body["messages"].([]any)
	if len(sysList) != 2 {
		t.Fatalf("messages = %s", jsonAny(recs()[0].Body["messages"]))
	}
	sys, _ := sysList[0].(map[string]any)
	content, _ := sys["content"].(string)
	if strings.Contains(content, PromptCacheBoundary) {
		t.Errorf("boundary marker leaked to wire: %q", content)
	}
	if !strings.Contains(content, "static half") || !strings.Contains(content, "dynamic half") {
		t.Errorf("system content mangled: %q", content)
	}
}

// --- silent-failure posture (R8) ---

// TestWarm_QuotaUpstream_QuietNoRetry pins the R8 posture against a quota-
// blocked upstream: exactly ONE request (no retry storm) and a DEBUG line
// naming alias + reason; nothing surfaced (Warm returns nothing by design).
func TestWarm_QuotaUpstream_QuietNoRetry(t *testing.T) {
	srv, recs := warmUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"usage_limit_reached","message":"limit reached","resets_at":1893456000}}`))
	})
	logs, h := debugLogger()
	c := warmTestClient(t, srv.URL, WithLogger(h))

	c.Warm(context.Background(), "voice-fast", warmMessages(), "sess-42")
	if !waitFor(func() bool { return len(recs()) >= 1 }, 3*time.Second) {
		t.Fatal("quota warm never reached upstream")
	}
	// Give any hypothetical retry loop time to fire a second request.
	time.Sleep(400 * time.Millisecond)

	if n := len(recs()); n != 1 {
		t.Errorf("quota upstream saw %d requests, want 1 (Warm must never retry)", n)
	}
	l := logs.String()
	if !strings.Contains(l, "level=DEBUG") || !strings.Contains(l, "Warm request failed") {
		t.Errorf("expected DEBUG failure line, got:\n%s", l)
	}
	if !strings.Contains(l, "voice-fast") {
		t.Errorf("DEBUG failure must name the alias, got:\n%s", l)
	}
}

// TestWarm_TransportErrorQuiet pins that a dead upstream is swallowed at
// DEBUG without panics.
func TestWarm_TransportErrorQuiet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // client now dials a dead port

	logs, h := debugLogger()
	cfg := &ModelConfig{BaseURL: url + "/v1", ModelID: "m", ProviderID: "p", MaxTokens: 100}
	c := NewClient(cfg, WithLogger(h))
	c.Warm(context.Background(), "dead-alias", warmMessages(), "s")

	if !waitFor(func() bool { return laneIdle(c) }, 3*time.Second) {
		t.Fatal("lane pinned after transport failure")
	}
	l := logs.String()
	if !strings.Contains(l, "Warm request failed") || !strings.Contains(l, "dead-alias") {
		t.Errorf("expected DEBUG transport-failure line naming alias, got:\n%s", l)
	}
}

// TestWarm_ReturnedTextNeverSurfaces pins that the completion text of a warm
// never reaches the logs at any level (a marker body proves it).
func TestWarm_ReturnedTextNeverSurfaces(t *testing.T) {
	const marker = "WARM-COMPLETION-MUST-NEVER-BE-LOGGED"
	srv, _ := warmUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":     "chatcmpl-warm",
			"object": "chat.completion",
			"model":  "test-model",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": marker},
				"finish_reason": "stop",
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	logs, h := debugLogger()
	c := warmTestClient(t, srv.URL, WithLogger(h))

	c.Warm(context.Background(), "a", warmMessages(), "s")
	time.Sleep(300 * time.Millisecond)

	l := logs.String()
	if strings.Contains(l, marker) {
		t.Errorf("completion text leaked into logs:\n%s", l)
	}
	if !strings.Contains(l, "Warm request completed") {
		t.Errorf("expected a DEBUG completion line, got:\n%s", l)
	}
}

// --- no side effects on turn machinery ---

// TestWarm_NoLLMCallsRow pins the no-turn-row rule: with a metrics.db usage
// store attached, a COMPLETED warm writes zero llm_calls rows (the real
// turn's path writes them; Warm bypasses recordUsageStore entirely).
func TestWarm_NoLLMCallsRow(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	store := newUsageTestStore(t)
	c := warmTestClient(t, srv.URL)
	c.SetUsageStore(store)

	c.Warm(context.Background(), "voice-fast", warmMessages(), "sess-42")
	if !waitFor(func() bool { return len(recs()) == 1 && laneIdle(c) }, 3*time.Second) {
		t.Fatal("warm did not complete")
	}
	// recordUsageStore writes from an async goroutine; grace it before
	// asserting emptiness.
	time.Sleep(300 * time.Millisecond)

	var n int
	if err := store.DB().Get(&n, `SELECT COUNT(*) FROM llm_calls`); err != nil {
		t.Fatalf("count llm_calls: %v", err)
	}
	if n != 0 {
		t.Errorf("warm wrote %d llm_calls rows, want 0", n)
	}
}

// TestWarm_BudgetExhaustedStillWarms pins that Warm never consults or
// consumes the budget gate: an EXHAUSTED budget must not stop the warm —
// and, as the control, the same client's real turn is refused.
func TestWarm_BudgetExhaustedStillWarms(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	b := NewBudget(BudgetConfig{HourlyLimit: 1, DailyLimit: 1}, slog.New(slog.DiscardHandler))
	b.RecordUsage(TokenUsage{PromptTokens: 5, CompletionTokens: 5})
	c := warmTestClient(t, srv.URL, WithBudget(b))

	c.Warm(context.Background(), "voice-fast", warmMessages(), "s")
	if !waitFor(func() bool { return len(recs()) == 1 }, 3*time.Second) {
		t.Fatal("warm blocked by budget gate")
	}
	// Control: a real turn against the same exhausted budget is refused —
	// proving the warm path genuinely skips the gate, not that the gate is
	// misconfigured-open.
	if _, err := c.Chat(context.Background(), warmMessages()); err == nil {
		t.Error("Chat unexpectedly succeeded against an exhausted budget")
	} else if _, ok := errors.AsType[*BudgetExceededError](err); !ok {
		t.Errorf("Chat error = %v, want BudgetExceededError", err)
	}
	// And the warm's tokens never landed in the budget counter: usage is
	// still exactly the 10 tokens recorded before (a warm would add more).
	if used := b.hourlyUsed(); used != 10 {
		t.Errorf("budget hourly used = %d, want 10 (warm must not record usage)", used)
	}
}

// TestWarm_NeverQueuesBehindRealTurn pins "a warm never queues behind another
// request": with the model's interactive slot gate saturated by a hung real
// turn ON THE SAME CLIENT, the warm still reaches the upstream immediately
// (dedicated lane, no acquireConcurrencyLimit).
func TestWarm_NeverQueuesBehindRealTurn(t *testing.T) {
	var (
		mu      sync.Mutex
		warmGot int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		mt, _ := parsed["max_tokens"].(float64)
		if mt == 1 {
			// The warm: answer instantly.
			mu.Lock()
			warmGot++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "c", "model": "m",
				"choices": []map[string]any{{"index": 0,
					"message": map[string]any{"role": "assistant", "content": "x"}}},
			})
			return
		}
		// The real turn: hang until its client ctx dies.
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := warmTestClient(t, srv.URL, WithConcurrencyLimit(1))
	chatCtx, chatCancel := context.WithCancel(context.Background())
	defer chatCancel()
	go func() { _, _ = c.Chat(chatCtx, warmMessages()) }()
	time.Sleep(150 * time.Millisecond) // let Chat take the single slot + connect

	c.Warm(context.Background(), "voice-fast", warmMessages(), "s")

	if !waitFor(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return warmGot == 1
	}, 3*time.Second) {
		t.Fatal("warm queued behind the real turn's slot gate")
	}
	chatCancel() // release the hung Chat so the fake server can close
}

// --- lane semantics: cap 1, replace-discard, panic-safe, ceiling ---

// TestWarm_ReplaceDiscardPrevious pins cap-1 replace-discard: while warm #1
// is in flight, warm #2 CANCELS #1 instead of queueing behind it. The fake
// upstream hangs every request until its ctx dies and reports which one was
// cancelled first.
func TestWarm_ReplaceDiscardPrevious(t *testing.T) {
	// Small ceiling so the second (still-hanging) warm releases the lane
	// and the fake server can close promptly.
	old := warmRequestCeiling
	warmRequestCeiling = 250 * time.Millisecond
	defer func() { warmRequestCeiling = old }()

	var once sync.Once
	arrived := make(chan struct{}, 1)
	cancelled := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		tag := "latest"
		if strings.Contains(string(body), "stale") {
			tag = "stale"
		}
		once.Do(func() { arrived <- struct{}{} })
		<-r.Context().Done()
		select {
		case cancelled <- tag:
		default:
		}
	}))
	defer srv.Close()

	c := warmTestClient(t, srv.URL)
	msgs := func(content string) []ChatMessage {
		return []ChatMessage{{Role: RoleUser, Content: content}}
	}
	c.Warm(context.Background(), "stale", msgs("stale"), "s")
	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("first warm never arrived")
	}

	c.Warm(context.Background(), "latest", msgs("latest"), "s")
	select {
	case got := <-cancelled:
		if got != "stale" {
			t.Errorf("cancelled request was %q, want the stale one (replace-discard broken)", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second Warm did not cancel the first (replace-discard broken)")
	}
	// And the lane must go idle on its own via the ceiling, not stay pinned
	// by the second (still-hanging) warm.
	if !waitFor(laneIdleFunc(c), 3*time.Second) {
		t.Error("lane pinned after replace-discard")
	}
}

// TestWarm_LaneReleasedAfterCompletion pins the cap-1 invariant: the lane
// empties when a warm finishes, and the next warm runs.
func TestWarm_LaneReleasedAfterCompletion(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	c := warmTestClient(t, srv.URL)

	c.Warm(context.Background(), "a", warmMessages(), "s")
	if !waitFor(func() bool { return len(recs()) == 1 && laneIdle(c) }, 3*time.Second) {
		t.Fatal("lane not released after warm completion")
	}
	c.Warm(context.Background(), "b", warmMessages(), "s")
	if !waitFor(func() bool { return len(recs()) == 2 }, 3*time.Second) {
		t.Error("second warm after release never fired")
	}
}

// panicResolver panics mid-dispatch to prove the lane release is panic-safe.
type panicResolver struct{}

func (panicResolver) ResolveToken(context.Context, string) (string, error) {
	panic("boom: token resolver exploded")
}

// TestWarm_PanicReleasesLane pins panic-safe lane release.
func TestWarm_PanicReleasesLane(t *testing.T) {
	srv, _ := warmUpstream(t, nil)
	cfg := &ModelConfig{
		BaseURL:       srv.URL + "/v1",
		ModelID:       "m",
		ProviderID:    "p",
		OAuthProvider: "evil",
	}
	c := NewClient(cfg,
		WithLogger(slog.New(slog.DiscardHandler)),
		WithTokenResolver(panicResolver{}, "evil"))

	c.Warm(context.Background(), "a", warmMessages(), "s")
	if !waitFor(laneIdleFunc(c), 3*time.Second) {
		t.Fatal("lane pinned after a panicking warm")
	}
	// A follow-up warm must still run after the panic released the lane.
	c2srv, recs := warmUpstream(t, nil)
	c2 := warmTestClient(t, c2srv.URL)
	c2.Warm(context.Background(), "b", warmMessages(), "s")
	if !waitFor(func() bool { return len(recs()) == 1 }, 3*time.Second) {
		t.Error("unrelated client unaffected")
	}
}

func laneIdleFunc(c *Client) func() bool { return func() bool { return laneIdle(c) } }

// TestWarm_CallerCancelDiscards pins that cancelling the caller ctx kills an
// in-flight warm (voice call end must not leave zombie requests).
func TestWarm_CallerCancelDiscards(t *testing.T) {
	arrived := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	srv, _ := warmUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		select {
		case cancelled <- struct{}{}:
		default:
		}
	})
	c := warmTestClient(t, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	c.Warm(ctx, "a", warmMessages(), "s")
	<-arrived
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("caller cancel did not discard the in-flight warm")
	}
}

// TestWarm_CeilingBoundsHungRequest pins the hard internal ceiling: against
// a hung upstream the warm lane frees within the ceiling even though the
// caller ctx has no deadline.
func TestWarm_CeilingBoundsHungRequest(t *testing.T) {
	old := warmRequestCeiling
	warmRequestCeiling = 120 * time.Millisecond
	defer func() { warmRequestCeiling = old }()

	arrived := make(chan struct{}, 1)
	srv, _ := warmUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	})
	c := warmTestClient(t, srv.URL)

	c.Warm(context.Background(), "a", warmMessages(), "s")
	<-arrived
	if !waitFor(laneIdleFunc(c), 3*time.Second) {
		t.Fatal("hung warm pinned the lane past the internal ceiling")
	}
}

// TestWarm_EmptyMessagesNoRequest pins the no-op guard: nothing to warm with
// means zero upstream traffic.
func TestWarm_EmptyMessagesNoRequest(t *testing.T) {
	srv, recs := warmUpstream(t, nil)
	c := warmTestClient(t, srv.URL)
	c.Warm(context.Background(), "a", nil, "s")
	c.Warm(context.Background(), "a", []ChatMessage{}, "s")
	time.Sleep(200 * time.Millisecond)
	if n := len(recs()); n != 0 {
		t.Errorf("empty-messages warm sent %d requests, want 0", n)
	}
}

// TestWarm_NilClientNoPanic pins nil-receiver safety on all three classes.
func TestWarm_NilClientNoPanic(t *testing.T) {
	var c *Client
	c.Warm(context.Background(), "a", warmMessages(), "s")
	var ac *AnthropicClient
	ac.Warm(context.Background(), "a", warmMessages(), "s")
	var cc *CodexClient
	cc.Warm(context.Background(), "a", warmMessages(), "s")
}

// --- non-OpenAI-compat classes: no-op with DEBUG ---

// TestWarm_AnthropicCodexNoop pins leaf 01 task 4: Warm on the Anthropic and
// Codex client classes is a DEBUG no-op that issues ZERO requests, so leaf
// 02 can call the seam without knowing which class resolved.
func TestWarm_AnthropicCodexNoop(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logs, h := debugLogger()

	ac := NewAnthropicClient(&ModelConfig{BaseURL: srv.URL, ModelID: "claude-test", ProviderID: "anthropic"}, WithAnthropicLogger(h))
	ac.Warm(context.Background(), "anthropic-alias", warmMessages(), "s")

	cc := NewCodexClient(&ModelConfig{BaseURL: srv.URL, ModelID: "gpt-5.1-codex", ProviderID: "openai-codex"}, WithCodexLogger(h))
	cc.Warm(context.Background(), "codex-alias", warmMessages(), "s")

	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	got := hits
	mu.Unlock()
	if got != 0 {
		t.Errorf("non-compat Warm issued %d requests, want 0", got)
	}
	l := logs.String()
	if !strings.Contains(l, "Warm unsupported for Anthropic") || !strings.Contains(l, "Warm unsupported for Codex") {
		t.Errorf("expected DEBUG no-op lines, got:\n%s", l)
	}
	if !strings.Contains(l, "anthropic-alias") || !strings.Contains(l, "codex-alias") {
		t.Errorf("no-op DEBUG must name the alias, got:\n%s", l)
	}
}
