package employee

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// goalRoundDriverAutoTestHarness is the deferred-turn scheduler double used
// by the autonomy tests: registrations are kept per name (mirroring the
// daemon adapter's replace-per-name semantics) and fired synchronously.
type goalRoundDriverAutoTestHarness struct {
	mu      sync.Mutex
	pending map[string]func()
}

func newGoalRoundDriverAutoTestHarness() *goalRoundDriverAutoTestHarness {
	return &goalRoundDriverAutoTestHarness{pending: make(map[string]func())}
}

func (h *goalRoundDriverAutoTestHarness) ScheduleOneShot(name string, fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending[name] = fn
}

func (h *goalRoundDriverAutoTestHarness) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending)
}

func (h *goalRoundDriverAutoTestHarness) fireAll() {
	h.mu.Lock()
	fns := make([]func(), 0, len(h.pending))
	for _, fn := range h.pending {
		fns = append(fns, fn)
	}
	h.pending = make(map[string]func())
	h.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// newDriverAutoTestManager builds a Manager over a shared temp SQLite DB
// with a live bot manager (so Manager.Trigger works) and the goal store.
// The employee row is hired enabled, mirroring the daemon wiring.
func newDriverAutoTestManager(t *testing.T, employeeID string) *Manager {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { botStore.Close() })
	goalStore, err := NewGoalStore(dbPath, nil)
	if err != nil {
		t.Fatalf("goal store: %v", err)
	}
	t.Cleanup(func() { goalStore.Close() })

	mgr := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, nil, goalStore, nil, slog.New(slog.DiscardHandler))

	// Seed the FK anchor + enabled bot definition so Trigger's GetBot
	// lookup passes (mirrors the daemon's hired-employee row).
	ctx := context.Background()
	if err := botStore.Create(ctx, bot.BotDefinition{
		ID:      employeeID,
		Name:    employeeID,
		Prompt:  "goal round driver test employee",
		Enabled: true,
	}); err != nil {
		t.Fatalf("seed employee anchor: %v", err)
	}
	return mgr
}

// TestGoalRoundDriver_AutonomousDeferredTurn (leaf T2) pins the zero-human-
// turn loop: OnRoundSettled reserves the round AND schedules the deferred
// turn re-entering Manager.Trigger with the source:"goal" payload carrying
// the reserved round's identity.
func TestGoalRoundDriver_AutonomousDeferredTurn(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_auto")
	if _, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_auto", Title: "auto", Mandate: "m", Source: "user", MaxRounds: 2,
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}

	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness)

	outcome, err := d.OnRoundSettled(ctx, "emp_auto")
	if err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("settle = %v, %v; want advanced", outcome, err)
	}
	if harness.count() != 1 {
		t.Fatalf("no deferred turn scheduled (pending=%d)", harness.count())
	}

	// The deferred turn is registered inflight; firing it re-enters
	// Manager.Trigger and clears the flag.
	d.mu.Lock()
	inflight := d.inflight["emp_auto"]
	d.mu.Unlock()
	if !inflight {
		t.Fatal("inflight flag must be set when the deferred turn is scheduled")
	}
	harness.fireAll()
	d.mu.Lock()
	inflight = d.inflight["emp_auto"]
	d.mu.Unlock()
	if inflight {
		t.Error("inflight flag must clear after the deferred turn settles")
	}
}

// TestGoalRoundDriver_AutoAdvanceGatedOff pins the manual-only mode:
// WithAutoAdvance(false) makes Start a no-op (no loop launches) while
// OnRoundSettled keeps working for the RPC/CLI path.
func TestGoalRoundDriver_AutoAdvanceGatedOff(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_manual")
	if _, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_manual", Title: "manual", Mandate: "m", Source: "user", MaxRounds: 2,
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness).
		WithAutoAdvance(false)

	// Manual advance still schedules the deferred turn (reserve-only would
	// be a regression for the RPC path).
	if _, err := d.OnRoundSettled(ctx, "emp_manual"); err != nil {
		t.Fatalf("manual settle: %v", err)
	}
	if harness.count() != 1 {
		t.Fatalf("manual path must still schedule the deferred turn (pending=%d)", harness.count())
	}

	// Start with autoAdvance=false must not launch the loop.
	d.Start(ctx)
	d.Stop() // joins nothing; must not hang or panic
}

// TestGoalRoundDriver_QuiescenceGatesAdvance pins that a NOT-quiet employee
// (parked episode or queued steer) is never advanced by the loop, and that
// the in-flight dedup prevents double-scheduling.
func TestGoalRoundDriver_QuiescenceGatesAdvance(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_quiet")
	if _, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_quiet", Title: "quiet", Mandate: "m", Source: "user", MaxRounds: 5,
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()

	quiet := false
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness).
		WithQuiescenceCheck(func(string) bool { return quiet })

	// Not quiet: no advance.
	if outcome, err := d.settleEmployeeIfQuietOutcome(ctx, "emp_quiet"); err != nil || outcome != GoalRoundIdle {
		t.Fatalf("not-quiet settle = %v, %v; want idle", outcome, err)
	}
	if harness.count() != 0 {
		t.Fatalf("not-quiet employee must not advance (pending=%d)", harness.count())
	}

	// Quiet: advance + one deferred turn.
	quiet = true
	if outcome, err := d.settleEmployeeIfQuietOutcome(ctx, "emp_quiet"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("quiet settle = %v, %v; want advanced", outcome, err)
	}
	if harness.count() != 1 {
		t.Fatalf("quiet employee must advance exactly once (pending=%d)", harness.count())
	}

	// In-flight: a second pass must not double-schedule.
	if outcome, err := d.settleEmployeeIfQuietOutcome(ctx, "emp_quiet"); err != nil || outcome != GoalRoundIdle {
		t.Fatalf("in-flight settle = %v, %v; want idle", outcome, err)
	}
	if harness.count() != 1 {
		t.Fatalf("in-flight employee must not double-schedule (pending=%d)", harness.count())
	}
}

// TestGoalRoundDriver_LoopAdvancesToAutoBlock drives the full Start loop:
// an armed MaxRounds=2 goal advances, runs its deferred rounds (fired
// synchronously from the harness), and lands in the round-limit auto-block
// with zero human turns.
func TestGoalRoundDriver_LoopAdvancesToAutoBlock(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_loop")
	g, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_loop", Title: "loop", Mandate: "m", Source: "user", MaxRounds: 2,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness).
		WithPollInterval(5 * time.Millisecond)
	d.Start(ctx)
	t.Cleanup(d.Stop)

	// The loop's first tick reserves round 1; each fired deferred turn
	// settles and the next tick reserves the next round until the cap
	// auto-blocks. Poll synchronously with a deadline.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		harness.fireAll()
		reloaded, err := mgr.GetGoal(ctx, g.ID)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if reloaded.State == GoalBlocked {
			if reloaded.BlockedReason == nil || reloaded.BlockedReason.Code != RoundLimitBlockCode {
				t.Fatalf("blocked reason = %+v, want code round-limit", reloaded.BlockedReason)
			}
			if reloaded.RoundsStarted != 2 {
				t.Fatalf("rounds_started = %d, want 2", reloaded.RoundsStarted)
			}
			return // the zero-human-turn loop reached the cap auto-block
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("goal never auto-blocked within the deadline (rounds_started progress incomplete)")
}

// TestManager_QuiescenceAccessors pins the Manager-side quiescence seams:
// no registered loop is quiet by definition.
func TestManager_QuiescenceAccessors(t *testing.T) {
	mgr := newDriverTestManager(t, "emp_acc")
	if mgr.PendingSteerCount("emp_acc") != 0 {
		t.Error("loop-less employee must report zero pending steer")
	}
	if mgr.HasParkedEpisode("emp_acc") {
		t.Error("loop-less employee must report no parked episode")
	}
}
