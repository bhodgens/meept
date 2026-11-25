package employee

// Pins for the employee cron lifecycle finding M15 (retire drain) from the
// 2026-11-24 bughunt:
//
//   - TestRetireDrainIsBoundedAndOrdered (M15) pins that Retire's invoke-
//     mutex drain never blocks an unbounded plain Lock() behind a minutes-
//     long goal round: retirement must proceed within a bounded window and
//     the in-flight round must still be able to finish on its own mutex.
//
// M17 (shutdown context) pins live in the follow-up commit.
import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// blockingReflector is a Reflector whose Chat blocks on ctx.Done(), plus a
// gate that lets the test cancel mid-call. It simulates the LLM round the
// daemon abandon at shutdown (M17).
type blockingReflector struct {
	mu      sync.Mutex
	block   chan struct{} // closed when Chat is inside its blocking select
	release chan struct{} // (unused in the cancel path; kept for symmetry)
	calls   int
}

func newBlockingReflector() *blockingReflector {
	return &blockingReflector{
		block:   make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (b *blockingReflector) inBlockingCall() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.block
}

func (b *blockingReflector) Chat(ctx context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	b.mu.Lock()
	b.calls++
	sig := b.block
	b.mu.Unlock()
	close(sig) // signal "inside the LLM call"
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b *blockingReflector) Config() *llm.ModelConfig { return nil }

func (b *blockingReflector) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, cb llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return b.Chat(ctx, msgs, opts...)
}

// TestRetireDrainIsBoundedAndOrdered pins M15: a Trigger round holding the
// per-employee invoke mutex (triggerViaGoalLoop shape) must NOT delay
// retirement unboundedly — Retire completes within retireDrainWait even
// though the round is still mid-Decide, and the in-flight round keeps its
// mutex (it finishes on the drained mutex; it is not dropped).
//
// Failing revert: retireEmployee draining with a plain, unbounded
// retireMu.Lock() BEFORE returning — Retire then blocks for the full round
// (the mutex is never released in this test) and the pin times out.
func TestRetireDrainIsBoundedAndOrdered(t *testing.T) {
	m, _ := newHirePersistenceManager(t)
	ctx := context.Background()

	const id = "emp-m15-bounded-drain"
	if err := m.botManager.CreateBot(ctx, bot.BotDefinition{
		ID:       id,
		Name:     "m15 bounded drain",
		Prompt:   "p",
		Enabled:  true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	reflector := newBlockingReflector()
	loop := NewGoalLoop(id, testTier2Constitution(), nil, slog.New(slog.DiscardHandler)).
		WithReflector(reflector)
	m.RegisterGoalLoop(id, loop)

	// Caller-side cancellation handle: proves the drained round's writer is
	// not stranded — the round can still be cancelled through its own ctx
	// and returns through the loop's normal error path.
	roundCtx, cancelRound := context.WithCancel(ctx)

	// Fire a trigger that will block inside Decide (holding invokeMu) for
	// as long as the test lets it.
	roundDone := make(chan error, 1)
	go func() {
		_, err := m.Trigger(roundCtx, id, map[string]any{"source": "manual", "prompt": "p"})
		roundDone <- err
	}()

	// Wait until the round is demonstrably inside the LLM call (and thus
	// holding the invoke mutex across Decide).
	select {
	case <-reflector.inBlockingCall():
	case <-time.After(2 * time.Second):
		t.Fatal("trigger round never reached the blocking LLM call")
	}

	// Retire while the round is mid-flight: must return within the drain
	// bound, NOT for the whole (unbounded) round.
	retired := make(chan error, 1)
	go func() {
		retired <- m.Retire(ctx, id)
	}()
	select {
	case err := <-retired:
		if err != nil {
			t.Fatalf("Retire: %v", err)
		}
	case <-time.After(retireDrainWait + 3*time.Second):
		t.Fatal("Retire blocked past retireDrainWait behind an in-flight round's invoke mutex (unbounded drain)")
	}

	// The map entry is pruned; a re-hire lazily creates a fresh mutex.
	// (Pruning itself is pinned by TestRetireDrainsInvokeMutexBeforeDelete;
	// here the drain bound is the assertion under test.)
	fresh := m.AcquireInvokeMutexForResume(id)
	if fresh == nil {
		t.Fatal("post-retire acquisition returned nil mutex")
	}

	// The in-flight round is NOT stranded by the bounded drain: the retired
	// mutex still serializes it, and the round completes on its own (here:
	// the reflector unblocks when the test cancels the round's caller ctx).
	// Prove the writer was not dropped: cancel the caller ctx and the
	// round returns cleanly with the loop's normal error path.
	cancelRound()
	select {
	case err := <-roundDone:
		if err == nil {
			t.Fatal("in-flight round completed with nil error after cancellation; want fail-closed ctx error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("in-flight round error = %v, want it to wrap context.Canceled (fail closed)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight round never unblocked after shutdown cancellation (still running to a dead backend)")
	}
}

// TestStopAllCancelsInFlightCronRound pins M17: a cron round blocked on a
// never-resolving LLM decide is cancelled when StopAll's drain bound
// expires — the abandoned round fails fast on ctx instead of running to a
// dead backend.
//
// Failing revert: fireCronTrigger using context.Background() (the manager
// shutdown ctx is never cancelled into the round) — the round blocks
// forever and this pin times out.
func TestStopAllCancelsInFlightCronRound(t *testing.T) {
	m, _ := newHirePersistenceManager(t)
	ctx := context.Background()

	const id = "emp-m17-shutdown-round"
	if err := m.botManager.CreateBot(ctx, bot.BotDefinition{
		ID:       id,
		Name:     "m17 shutdown round",
		Prompt:   "p",
		Enabled:  true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	reflector := newBlockingReflector()
	loop := NewGoalLoop(id, testTier2Constitution(), nil, slog.New(slog.DiscardHandler)).
		WithReflector(reflector)
	m.RegisterGoalLoop(id, loop)

	roundDone := make(chan error, 1)
	go func() {
		_, err := m.Trigger(ctx, id, map[string]any{"source": "cron", "prompt": "p"})
		roundDone <- err
	}()
	select {
	case <-reflector.inBlockingCall():
	case <-time.After(2 * time.Second):
		t.Fatal("cron round never reached the blocking LLM call")
	}

	// StopAll hits its drain bound and must cancel the shutdown context the
	// round was started with.
	m.StopAll()

	select {
	case err := <-roundDone:
		if err == nil {
			t.Fatal("abandoned cron round completed with nil error after StopAll; want fail-closed ctx error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("abandoned round error = %v, want it to wrap context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("abandoned cron round never cancelled after StopAll's drain bound (runs to a dead backend)")
	}
}

// TestTriggerAfterShutdownFailsFast pins M17: a Trigger landing after
// StopAll is refused immediately instead of being dispatched into a dead
// backend.
//
// Failing revert: stop checking shutdownCtx in Trigger — the call reaches
// GetBot (or worse, the goal loop) after shutdown and the pin fails on the
// not-cancelled assertion.
func TestTriggerAfterShutdownFailsFast(t *testing.T) {
	m, _ := newHirePersistenceManager(t)
	ctx := context.Background()

	const id = "emp-m17-post-stop"
	if err := m.botManager.CreateBot(ctx, bot.BotDefinition{
		ID:       id,
		Name:     "m17 post-stop",
		Prompt:   "p",
		Enabled:  true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	m.StopAll()
	_, err := m.Trigger(ctx, id, map[string]any{"source": "manual", "prompt": "p"})
	if err == nil {
		t.Fatal("Trigger after StopAll succeeded; want a shutdown refusal")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Trigger after StopAll error = %v, want it to wrap context.Canceled", err)
	}
}

// TestCronTriggerUsesManagerShutdownContext pins the M17 wiring seam:
// fireCronTrigger must derive its context from the manager shutdown
// context (not context.Background()), so a cron job started before
// shutdown carries a cancellable ctx into Trigger.
//
// Failing revert: restore `ctx := context.Background()` in fireCronTrigger
// — the captured context is then never cancelled by cancel() and the pin
// times out.
func TestCronTriggerUsesManagerShutdownContext(t *testing.T) {
	m, _ := newHirePersistenceManager(t)

	const id = "emp-m17-cron-ctx"
	if err := m.botManager.CreateBot(context.Background(), bot.BotDefinition{
		ID:       id,
		Name:     "m17 cron ctx",
		Prompt:   "p",
		Enabled:  true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeCron, Enabled: true, Schedule: "0 4 * * *"}},
	}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	captured := make(chan context.Context, 1)
	loop := NewGoalLoop(id, testTier2Constitution(), nil, slog.New(slog.DiscardHandler)).
		WithReflector(ctxCaptureReflector{capture: captured})
	m.RegisterGoalLoop(id, loop)

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.fireCronTrigger(id, "0 4 * * *", "p")
	}()

	var got context.Context
	select {
	case got = <-captured:
	case <-time.After(2 * time.Second):
		t.Fatal("fireCronTrigger never reached Trigger (no ctx captured)")
	}
	m.shutdownCancel() // what StopAll does after its drain bound
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fireCronTrigger did not fail fast after shutdown cancellation")
	}
	if err := got.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("fireCronTrigger ctx err = %v, want context.Canceled (fail fast did not reach the round)", err)
	}
	// Seam proof: the ctx fireCronTrigger passed must DERIVE from the
	// manager shutdown context, not context.Background(). WithCancel
	// children preserve the shutdownCtxKey value; Background has none.
	// (Fail-fast alone is not enough evidence — the manager-level fan-in
	// in triggerViaGoalLoop cancels any round regardless of the ctx
	// fireCronTrigger passed.)
	if got.Value(shutdownCtxKey{}) == nil {
		t.Fatal("fireCronTrigger passed a context NOT derived from ShutdownContext() — context.Background() regression")
	}
}

// ctxCaptureReflector captures the ctx the round's first LLM call carries.
type ctxCaptureReflector struct {
	capture chan<- context.Context
}

func (c ctxCaptureReflector) Chat(ctx context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	select {
	case c.capture <- ctx:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c ctxCaptureReflector) Config() *llm.ModelConfig { return nil }

func (c ctxCaptureReflector) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, cb llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}
