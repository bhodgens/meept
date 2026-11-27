package employee

// Pins for the 2026-11-27 fix wave F4 — the retire/re-hire lifecycle
// (bughunt H6/T-1, M9/T-2, T-7):
//
//   - TestRehiredEmployeeRunsRounds (H6): Hire→Retire→Hire must clear the
//     fail-closed retirement marker so the re-hired id runs rounds instead
//     of being skipped forever as "employee retired mid-flight".
//   - TestRetireFailsInFlightRoundClosedAfterDecide (M9): the post-Decide
//     re-check must coerce a round that raced retirement to
//     Status:"skipped"/SkipReason:"employee retired mid-flight" instead of
//     reporting "completed" against the deleted bot.
//   - TestHireRollbackLeavesNoMarker (T-7): a rolled-back hire must leave
//     no retirement marker on a never-lived id, so a corrected re-hire
//     runs rounds immediately.
import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// racingReflector lets the test fire Retire WHILE a round is inside Decide:
// the first Chat unblocks a waiter (the round is demonstrably mid-Decide),
// then blocks until the test releases it. The gate/executor path stays on
// the loop's nil defaults (no plans, no executions), so Decide returns
// normally once Chat is released — exactly the raced-round shape M9 fixes.
type racingReflector struct {
	once      sync.Once
	midDecide chan struct{}
	release   chan struct{}
}

func newRacingReflector() *racingReflector {
	return &racingReflector{
		midDecide: make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (r *racingReflector) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	r.once.Do(func() { close(r.midDecide) })
	<-r.release
	return &llm.Response{Content: `{"candidates":[]}`}, nil
}

func (r *racingReflector) Config() *llm.ModelConfig { return nil }

func (r *racingReflector) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, cb llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return r.Chat(ctx, msgs, opts...)
}

func (r *racingReflector) unblock() { close(r.release) }

// hireViaRealPath runs the full production Hire path (validate →
// CreateBot → mirror → constitution persist) for an id.
func hireViaRealPath(t *testing.T, m *Manager, id, name string) {
	t.Helper()
	_, err := m.Hire(context.Background(), HireRequest{
		ID:           id,
		Name:         name,
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, validConstitution()),
	})
	if err != nil {
		t.Fatalf("Hire(%s): %v", id, err)
	}
}

// TestRehiredEmployeeRunsRounds pins H6/T-1: without the clearRetired in
// Hire, the second Hire inherits the retired marker and every round is
// skipped ("employee retired mid-flight"); with it, the round RUNS.
//
// Failing revert: delete the m.clearRetired(req.ID) call after CreateBot in
// Hire — the post-re-hire Trigger then returns Status:"skipped" and this
// pin fails.
func TestRehiredEmployeeRunsRounds(t *testing.T) {
	m, _ := newHirePersistenceManager(t)
	ctx := context.Background()

	const id = "emp-f4-rehire-runs"
	hireViaRealPath(t, m, id, "first life")
	if err := m.Retire(ctx, id); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if !m.isRetired(id) {
		t.Fatal("precondition: marker must be set after Retire")
	}
	hireViaRealPath(t, m, id, "second life")
	if m.isRetired(id) {
		t.Fatal("re-hire must clear the retired marker (H6)")
	}

	// Register a goal loop for the fresh bot and prove a round actually
	// runs (production Trigger path, not a skip receipt).
	reflector := newStubReflector()
	loop := NewGoalLoop(id, testTier2Constitution(), nil, slog.New(slog.DiscardHandler)).
		WithReflector(reflector)
	m.RegisterGoalLoop(id, loop)

	res, err := m.Trigger(ctx, id, map[string]any{"source": "manual", "prompt": "p"})
	if err != nil {
		t.Fatalf("Trigger after re-hire: %v", err)
	}
	if res.Status == "skipped" && res.SkipReason == "employee retired mid-flight" {
		t.Fatalf("re-hired employee round skipped as retired: %+v (H6 unfixed)", res)
	}
	if res.Status != "completed" {
		t.Fatalf("re-hired employee round status = %q, want completed; result %+v", res.Status, res)
	}
	if reflector.calls.Load() == 0 {
		t.Fatal("reflector never invoked: the round did not reach Decide")
	}
}

// TestRetireFailsInFlightRoundClosedAfterDecide pins M9/T-2: a round that
// passed the entry isRetired check and is inside Decide when Retire lands
// (Retire returns within the bounded drain) must NOT report "completed" —
// the post-Decide re-check fails it closed with the same SkipReason shape.
//
// Failing revert: delete the post-Decide isRetired re-check in
// triggerViaGoalLoop — the raced round then returns Status:"completed"
// and this pin fails.
func TestRetireFailsInFlightRoundClosedAfterDecide(t *testing.T) {
	m, _ := newHirePersistenceManager(t)
	ctx := context.Background()

	const id = "emp-f4-raced-round"
	hireViaRealPath(t, m, id, "raced round")

	reflector := newRacingReflector()
	loop := NewGoalLoop(id, testTier2Constitution(), nil, slog.New(slog.DiscardHandler)).
		WithReflector(reflector)
	m.RegisterGoalLoop(id, loop)

	roundDone := make(chan *TriggerResult, 1)
	roundErr := make(chan error, 1)
	go func() {
		res, err := m.Trigger(ctx, id, map[string]any{"source": "manual", "prompt": "p"})
		roundDone <- res
		roundErr <- err
	}()

	// Wait until the round is demonstrably inside Decide's LLM call.
	select {
	case <-reflector.midDecide:
	case <-time.After(2 * time.Second):
		t.Fatal("round never reached the LLM call inside Decide")
	}

	// Retire mid-Decide: bounded drain must let it return while the round
	// still holds the invoke mutex (M15 behavior, already pinned).
	if err := m.Retire(ctx, id); err != nil {
		t.Fatalf("Retire mid-round: %v", err)
	}

	// Let Decide finish. The round already passed the entry check, so the
	// ONLY thing standing between it and a false "completed" is the
	// post-Decide re-check.
	reflector.unblock()

	var res *TriggerResult
	select {
	case res = <-roundDone:
	case <-time.After(5 * time.Second):
		t.Fatal("raced round never finished after release")
	}
	if err := <-roundErr; err != nil {
		t.Fatalf("raced round returned error: %v", err)
	}
	if res.Status != "skipped" || res.SkipReason != "employee retired mid-flight" {
		t.Fatalf("raced round reported %+v; want skipped/\"employee retired mid-flight\" (M9 unfixed: round completed against a deleted bot)", res)
	}
}

// TestHireRollbackLeavesNoMarker pins T-7: a hire that rolls back after
// CreateBot (goal-creation failure) must not leave a retirement marker on
// the never-lived id, or the corrected re-hire is silently dead. The
// rollback sweep itself re-marks via retireEmployee; the marker the pin
// guards against is the one a REAL re-hire must clear.
//
// Failing revert: delete the m.clearRetired(req.ID) call in Hire — the
// re-hire after rollback inherits the stale marker and the round is
// skipped as retired.
func TestHireRollbackLeavesNoMarker(t *testing.T) {
	m, dbPath := newHirePersistenceManager(t)
	ctx := context.Background()

	const id = "emp-f4-rollback"
	c := validConstitution()
	// Hire whose declared-goal pass fails (bad spec): Hire succeeds through
	// CreateBot + constitution persist, then rolls back everything.
	_, hireErr := m.Hire(ctx, HireRequest{
		ID:           id,
		Name:         "doomed hire",
		Prompt:       "test prompt",
		Enabled:      true,
		Triggers:     []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
		Constitution: constitutionToMap(t, c),
		Goals: []GoalSpec{{
			EmployeeID: id,
			Title:      "broken",
			// Mandate missing → Validate() fails → rollback.
		}},
	})
	if hireErr == nil {
		t.Fatal("expected Hire to fail on invalid goal spec")
	}
	// Partial-hire rows are gone (the rollback sweep, existing behavior).
	if n := countRows(t, dbPath, `SELECT COUNT(*) FROM bot_definitions WHERE id = ?`, id); n != 0 {
		t.Fatalf("bot_definitions rows for %q after rollback = %d, want 0", id, n)
	}
	// The rollback sweep marks the id retired as a side effect; a corrected
	// re-hire must clear it and the employee must RUN.
	hireViaRealPath(t, m, id, "corrected re-hire")
	if m.isRetired(id) {
		t.Fatal("corrected re-hire after rollback still marked retired (T-7 unfixed)")
	}

	reflector := newStubReflector()
	loop := NewGoalLoop(id, testTier2Constitution(), nil, slog.New(slog.DiscardHandler)).
		WithReflector(reflector)
	m.RegisterGoalLoop(id, loop)

	res, err := m.Trigger(ctx, id, map[string]any{"source": "manual", "prompt": "p"})
	if err != nil {
		t.Fatalf("Trigger after corrected re-hire: %v", err)
	}
	if res.Status == "skipped" && res.SkipReason == "employee retired mid-flight" {
		t.Fatalf("corrected re-hire round skipped as retired: %+v (T-7 unfixed)", res)
	}
	if res.Status != "completed" {
		t.Fatalf("corrected re-hire round status = %q, want completed; result %+v", res.Status, res)
	}
}
