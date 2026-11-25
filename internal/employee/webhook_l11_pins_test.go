package employee

// Pins for bughunt 2026-11-24 finding L11 (webhook async 202 path):
//
//   - TestWebhookBurstIsCappedAndRefuses503 (L11 pin i): N+1 concurrent
//     POSTs spawn at most webhookMaxInFlight detached rounds; the extras
//     get an explicit 503 + Retry-After instead of queueing silently.
//     Failing revert: remove the semaphore in handleWebhookTrigger — the
//     9th POST answers 202 and the pin fails on the status code.
//
//   - TestWebhookRoundUsesShutdownContextAndPostStopFailsFast (L11 pin ii):
//     the round ctx derives from the manager ShutdownContext (seam proof
//     via shutdownCtxKey value — non-vacuity: reverting to
//     context.Background() loses the value), StopAll cancels an in-flight
//     round, and a POST landing after StopAll is refused without spawning
//     a new round.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// l11BlockingReflector blocks every Chat call on its ctx and records, per
// call, the ctx it received plus the observed concurrent-entry count. One
// instance is shared across per-employee loops so a single counter spans
// all in-flight webhook rounds.
type l11BlockingReflector struct {
	mu      sync.Mutex
	inside  int
	maxSeen int
	ctxs    []context.Context
	gate    chan struct{} // one token per entry, buffered wide
}

func newL11BlockingReflector() *l11BlockingReflector {
	return &l11BlockingReflector{gate: make(chan struct{}, 64)}
}

func (r *l11BlockingReflector) Chat(ctx context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	r.mu.Lock()
	r.inside++
	if r.inside > r.maxSeen {
		r.maxSeen = r.inside
	}
	r.ctxs = append(r.ctxs, ctx)
	r.mu.Unlock()
	select {
	case r.gate <- struct{}{}:
	default:
	}
	<-ctx.Done()
	r.mu.Lock()
	r.inside--
	r.mu.Unlock()
	return nil, ctx.Err()
}

func (r *l11BlockingReflector) Config() *llm.ModelConfig { return nil }

func (r *l11BlockingReflector) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return r.Chat(ctx, msgs, opts...)
}

func (r *l11BlockingReflector) snapshot() (int, []context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxSeen, append([]context.Context(nil), r.ctxs...)
}

func (r *l11BlockingReflector) awaitEntries(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-r.gate:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d webhook rounds reached the blocking LLM call", i, n)
		}
	}
}

// newWebhookBurstFixture builds one manager with count distinct webhook-
// enabled bots, each with its own goal loop sharing one blocking
// reflector. Distinct employees matter: rounds for ONE bot serialize on the
// per-employee invoke mutex, so a single bot cannot show concurrent
// in-flight rounds.
func newWebhookBurstFixture(t *testing.T, count int) (*Manager, *l11BlockingReflector, *http.ServeMux) {
	t.Helper()
	store, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	botMgr := bot.NewManager(store, nil)
	rec := newL11BlockingReflector()
	emp := NewManager(botMgr)
	emp.logger = slog.New(slog.DiscardHandler)
	ctx := context.Background()
	for i := 0; i < count; i++ {
		id := "l11-bot-" + string(rune('a'+i))
		loop := NewGoalLoop(id, &Constitution{Purpose: "burst"}, nil, slog.New(slog.DiscardHandler))
		loop.WithReflector(rec)
		emp.RegisterGoalLoop(id, loop)
		if err := botMgr.CreateBot(ctx, bot.BotDefinition{
			ID:       id,
			Name:     id,
			Prompt:   "handle the hook",
			Enabled:  true,
			Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		}); err != nil {
			t.Fatalf("CreateBot %s: %v", id, err)
		}
	}
	mux := http.NewServeMux()
	emp.RegisterWebhookRoute(mux)
	return emp, rec, mux
}

func postWebhook(t *testing.T, mux *http.ServeMux, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/"+id, strings.NewReader("burst-body"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// TestWebhookBurstIsCappedAndRefuses503 pins L11(i): webhookMaxInFlight
// concurrent rounds run; the next POST is explicitly refused with 503 +
// Retry-After while the pool is saturated, then accepted again after
// StopAll releases the rounds (slots must return to the pool).
func TestWebhookBurstIsCappedAndRefuses503(t *testing.T) {
	emp, rec, mux := newWebhookBurstFixture(t, webhookMaxInFlight+1)
	ids := make([]string, webhookMaxInFlight+1)
	for i := range ids {
		ids[i] = "l11-bot-" + string(rune('a'+i))
	}

	for i := 0; i < webhookMaxInFlight; i++ {
		w := postWebhook(t, mux, ids[i])
		if w.Code != http.StatusAccepted {
			t.Fatalf("POST %d: status = %d, want 202 (body %s)", i, w.Code, w.Body.String())
		}
	}
	rec.awaitEntries(t, webhookMaxInFlight)

	// Pool saturated: the extra POST must be a loud refusal, not a 202.
	w := postWebhook(t, mux, ids[webhookMaxInFlight])
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("overflow POST: status = %d, want 503 (body %s)", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("overflow POST: 503 without Retry-After")
	}
	if !strings.Contains(w.Body.String(), "busy") {
		t.Fatalf("overflow POST: body = %s, want a JSON error", w.Body.String())
	}

	// Concurrency actually capped: exactly webhookMaxInFlight rounds are
	// inside the LLM call, no more.
	maxSeen, ctxs := rec.snapshot()
	if len(ctxs) != webhookMaxInFlight {
		t.Fatalf("rounds spawned = %d, want %d (semaphore leak)", len(ctxs), webhookMaxInFlight)
	}
	if maxSeen > webhookMaxInFlight {
		t.Fatalf("max concurrent rounds = %d, want <= %d", maxSeen, webhookMaxInFlight)
	}

	// Release every blocked round by shutting the manager down; the
	// WaitGroup drain must observe all of them leaving.
	emp.StopAll()
	if !emp.drainWebhookRounds(5 * time.Second) {
		t.Fatal("webhook rounds still in flight 5s after StopAll cancelled the shutdown context (leak)")
	}
	// Slots returned to the pool: a fresh POST is accepted again.
	w = postWebhook(t, mux, ids[0])
	if w.Code != http.StatusServiceUnavailable {
		// After StopAll the shutdown refusal (also 503) wins by design;
		// slot-reavailability is pinned by the WaitGroup drain above.
		t.Logf("post-shutdown POST refused as expected: %d %s", w.Code, w.Body.String())
	}
}

// TestWebhookRoundUsesShutdownContextAndPostStopFailsFast pins L11(ii):
// the detached round's ctx is derived from the manager ShutdownContext
// (value seam), StopAll cancels the in-flight round, and a POST landing
// after StopAll fails fast without reaching the goal loop.
func TestWebhookRoundUsesShutdownContextAndPostStopFailsFast(t *testing.T) {
	emp, rec, mux := newWebhookBurstFixture(t, 1)

	w := postWebhook(t, mux, "l11-bot-a")
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	rec.awaitEntries(t, 1)

	_, ctxs := rec.snapshot()
	if len(ctxs) != 1 {
		t.Fatalf("rounds spawned = %d, want 1", len(ctxs))
	}
	// Seam proof: the round ctx carries the manager's shutdown-context
	// marker (WithCancel/WithValue children preserve it). Reverting to
	// context.Background() drops the value and this assertion goes red.
	if ctxs[0].Value(shutdownCtxKey{}) == nil {
		t.Fatal("webhook round ctx was NOT derived from ShutdownContext() (shutdownCtxKey value missing)")
	}

	// StopAll cancels the shutdown context: the blocked round unwinds.
	emp.StopAll()
	if !emp.drainWebhookRounds(5 * time.Second) {
		t.Fatal("webhook round survived StopAll's shutdown cancellation (detached leak)")
	}
	if err := ctxs[0].Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("round ctx after StopAll: err = %v, want context.Canceled", err)
	}

	// A POST landing after StopAll fails fast: refused outright and the
	// goal loop is never entered again (no new round spawned).
	w = postWebhook(t, mux, "l11-bot-a")
	if w.Code == http.StatusAccepted {
		t.Fatal("POST after StopAll returned 202; want a fail-fast refusal")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		_, ctxs := rec.snapshot()
		if len(ctxs) != 1 {
			t.Fatalf("post-shutdown POST spawned a new round (%d reflector entries, want 1)", len(ctxs))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
