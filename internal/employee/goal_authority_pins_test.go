// Package employee — goal_authority_pins_test.go pins the bughunt fixes on
// the goal authority seam and the round driver (H8/A6-1, A6-4, A6-5, A6-13,
// A6-10): per-round CONTEXT authority instead of the process-global flag,
// no-scheduler inflight release + advance dedup, transient-error arm
// retention, tracking-map reclamation, and the nested gate-resume payload
// decode.
package employee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// ---------------------------------------------------------------------------
// H8/A6-1 — authority rides the round context, per-turn isolation
// ---------------------------------------------------------------------------

// observerAutonomousGlobal mirrors the OLD mechanism's shape (a process-
// global written by the driver's observer seam at round entry/exit) purely
// so the pin can PROVE the leak the old code had: while a driver round runs,
// the global reads true for every concurrent turn, while the per-round ctx
// verdict (the authority the tools now enforce) stays false on non-driver
// rounds.
var observerAutonomousGlobal atomic.Bool

// newCandidateReflector returns a stub reflector answering exactly one
// candidate for every call (tier-1 Assess → one Execute).
func newCandidateReflector(candidateJSON string) *stubReflector {
	s := newStubReflector()
	s.default_ = &llm.Response{Content: candidateJSON}
	return s
}

// authorityProbeExecutor is a stub BotExecutor that records the autonomous
// ctx stamp it observes at the deepest turn point (the same ctx the agent
// loop later hands the goal tools' Execute), holds the round open until
// both concurrent rounds have observed (so the observations are genuinely
// OVERLAPPING), then finishes.
func authorityProbeExecutor(dst *atomic.Bool, globalObserved *atomic.Bool, arrived *sync.WaitGroup, release <-chan struct{}) *stubExecutor {
	return &stubExecutor{execFn: func(ctx context.Context, _, _ string) (string, int, error) {
		dst.Store(builtin.GoalAutonomousFromContext(ctx))
		if globalObserved != nil {
			globalObserved.Store(observerAutonomousGlobal.Load())
		}
		arrived.Done()
		<-release
		return "round done", 1, nil
	}}
}

// TestGoalRoundDriver_ConcurrentRoundsGetDifferentAuthority pins H8/A6-1
// end-to-end through the driver's real Trigger path: one employee's driver
// round (deferred turn) and another employee's plain trigger run at the same
// instant. The driver round's ctx must carry the autonomous stamp (the
// tools then refuse its create/edit/pause/resume and gate its sub-threshold
// self-block); the plain trigger's ctx must NOT (direct-human verdict kept).
// Under the old process-global code the PLAIN round observed the same
// global=true the driver round set — one verdict for both callers — and the
// first round to finish flipped the other off mid-flight. The pin asserts
// the divergence: the global observer was true for the plain turn (old
// leak, non-vacuity) while the ctx authority it carries stayed false (the
// per-round fix).
func TestGoalRoundDriver_ConcurrentRoundsGetDifferentAuthority(t *testing.T) {
	dbPath := t.TempDir() + "/shared.db"
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
	constStore, err := NewConstitutionStore(dbPath, nil)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { constStore.Close() })
	mgr := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, constStore, goalStore, nil, slog.New(slog.DiscardHandler))

	ctx := context.Background()
	for _, id := range []string{"emp_drv", "emp_plain"} {
		if err := botStore.Create(ctx, bot.BotDefinition{
			ID: id, Name: id, Prompt: "authority pin employee", Enabled: true,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if err := constStore.Put(ctx, id, validConstitution()); err != nil {
			t.Fatalf("seed constitution %s: %v", id, err)
		}
	}

	// Both loops run one candidate round (tier-1 Assess → Execute).
	candidate := `{"candidates":[{"title":"t","description":"d","prompt":"p"}]}`
	drvStamp := new(atomic.Bool)
	plainStamp := new(atomic.Bool)
	globalSeenByPlain := new(atomic.Bool)
	var arrived sync.WaitGroup
	arrived.Add(2)
	release := make(chan struct{})

	drvLoop := NewGoalLoop("emp_drv", testTier1Constitution(), goalStore, nil).
		WithReflector(newCandidateReflector(candidate)).
		WithExecutor(authorityProbeExecutor(drvStamp, nil, &arrived, release))
	plainLoop := NewGoalLoop("emp_plain", testTier1Constitution(), goalStore, nil).
		WithReflector(newCandidateReflector(candidate)).
		WithExecutor(authorityProbeExecutor(plainStamp, globalSeenByPlain, &arrived, release))
	mgr.RegisterGoalLoop("emp_drv", drvLoop)
	mgr.RegisterGoalLoop("emp_plain", plainLoop)

	if _, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_drv", Title: "drv", Mandate: "m", Source: "user", MaxRounds: 3,
	}); err != nil {
		t.Fatalf("create driver goal: %v", err)
	}

	harness := newGoalRoundDriverAutoTestHarness()
	observerAutonomousGlobal.Store(false)
	// The setter mirrors the production wiring (builtin.SetGoalAutonomousRound)
	// plus a process-local mirror the probe can observe. On the FIXED code
	// the builtin setter is an intentional no-op (authority = ctx stamp) and
	// the observer mirror documents the old knob's window; on the pre-fix
	// (overlay) build the builtin setter IS the whole mechanism, which is
	// exactly why the plain round leaks into the driver's verdict.
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness).
		WithAutonomousRoundSetter(func(v bool) {
			observerAutonomousGlobal.Store(v)
			builtin.SetGoalAutonomousRound(v)
		})
	t.Cleanup(func() { builtin.SetGoalAutonomousRound(false) })

	if outcome, err := d.OnRoundSettled(ctx, "emp_drv"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("reserve driver round = %v, %v; want advanced", outcome, err)
	}

	// Fire the driver's deferred round; wait until its observer seam is
	// raised (the old global), THEN start the plain trigger, so the plain
	// round provably overlaps a raised global. Both turns block inside
	// their executor until BOTH have recorded verdicts.
	var driverWG sync.WaitGroup
	driverWG.Go(func() { harness.fireAll() })
	deadline := time.Now().Add(5 * time.Second)
	for !observerAutonomousGlobal.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !observerAutonomousGlobal.Load() {
		t.Fatal("driver round never raised the observer seam — cannot demonstrate overlap")
	}
	go func() {
		if _, err := mgr.Trigger(context.Background(), "emp_plain", map[string]any{"source": "manual"}); err != nil {
			t.Errorf("plain trigger: %v", err)
		}
	}()
	arrived.Wait() // both rounds are simultaneously inside their turn

	if !drvStamp.Load() {
		t.Error("driver round ctx lost the autonomous stamp (H8/A6-1: authority must ride the round context)")
	}
	if plainStamp.Load() {
		t.Error("plain concurrent trigger observed autonomous authority — the per-turn leak is back (H8/A6-1)")
	}
	// Non-vacuity against the old mechanism: while the plain round ran, the
	// process-global WAS true (the old code would have handed the plain
	// turn the driver's verdict — refused create/edit/pause/resume it must
	// allow, or allowed human-only actions it must refuse). The ctx verdict
	// (plainStamp=false) diverges from the global (true), proving the fix
	// changes behavior, not just plumbing.
	if !globalSeenByPlain.Load() {
		t.Error("plain round did not observe the raised global observer — overlap not demonstrated; pin is vacuous")
	}

	close(release)
	driverWG.Wait()
	// The driver round settled: no global to clear (the per-round ctx dies
	// with the turn). The inflight flag must be released.
	settle := time.Now().Add(3 * time.Second)
	for d.isInflight("emp_drv") && time.Now().Before(settle) {
		time.Sleep(5 * time.Millisecond)
	}
	if d.isInflight("emp_drv") {
		t.Error("driver round settled but the employee stayed inflight")
	}
}

// ---------------------------------------------------------------------------
// A6-4 — no-scheduler inflight wedge + round.advance phantom-reserve dedup
// ---------------------------------------------------------------------------

// TestGoalRoundDriver_NoSchedulerDoesNotStickInflight pins A6-4 leg 1: with
// NO DeferredScheduler bound, OnRoundSettled still reserves the round
// (reserve-only manual mode stays) but must NOT leave the employee inflight
// forever — the old code marked inflight then returned early, wedging the
// poll loop and every later settlement for that employee.
func TestGoalRoundDriver_NoSchedulerDoesNotStickInflight(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_nosched")
	if _, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_nosched", Title: "ns", Mandate: "m", Source: "user", MaxRounds: 5,
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)) // no scheduler

	if outcome, err := d.OnRoundSettled(ctx, "emp_nosched"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("reserve-only settle = %v, %v; want advanced", outcome, err)
	}
	if d.isInflight("emp_nosched") {
		t.Fatal("reserve-only mode must not leave the employee inflight (A6-4: no deferred turn will ever clear it)")
	}
	// The poll-loop seam must therefore keep working on the next tick.
	if outcome, err := d.settleEmployeeIfQuietOutcome(ctx, "emp_nosched"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("second settle after reserve-only = %v, %v; want advanced (unwedged)", outcome, err)
	}
}

// TestGoalRoundDriver_AdvanceDedupsInflight pins A6-4 leg 2: the manual
// round.advance path (OnRoundSettled called directly, bypassing the
// quiet/dedup wrapper) must refuse to reserve a second round while the
// first reserved round is still in flight — the old shape charged
// RoundsStarted for rounds that never ran.
func TestGoalRoundDriver_AdvanceDedupsInflight(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_dedup")
	g, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_dedup", Title: "dd", Mandate: "m", Source: "user", MaxRounds: 5,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).WithDeferredScheduler(harness)

	if outcome, err := d.OnRoundSettled(ctx, "emp_dedup"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("first advance = %v, %v; want advanced", outcome, err)
	}
	// Deferred turn registered but NOT fired: the round is pending.
	if outcome, err := d.OnRoundSettled(ctx, "emp_dedup"); err != nil || outcome != GoalRoundIdle {
		t.Fatalf("second advance while inflight = %v, %v; want idle (phantom-reserve dedup)", outcome, err)
	}
	reloaded, err := mgr.GetGoal(ctx, g.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.RoundsStarted != 1 {
		t.Fatalf("rounds_started = %d, want 1 — the in-flight advance must not reserve a second never-run round", reloaded.RoundsStarted)
	}
	// Once the deferred turn fires (settles), the advance path reopens.
	harness.fireAll()
	if outcome, err := d.OnRoundSettled(ctx, "emp_dedup"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("advance after settle = %v, %v; want advanced", outcome, err)
	}
}

// ---------------------------------------------------------------------------
// A6-5 — transient reserve errors retry (arm kept); terminal errors disarm
// ---------------------------------------------------------------------------

// TestGoalReserveErrorTerminal pins the error taxonomy the disarm decision
// uses: only not-found / transition-fence failures are terminal. CAS
// conflicts, SQLITE_BUSY/locked, deadlines and generic I/O blips are
// transient and must keep the arm.
func TestGoalReserveErrorTerminal(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		terminal bool
	}{
		{"nil", nil, false},
		{"not found", fmt.Errorf("update goal: %w", ErrGoalNotFound), true},
		{"transition", fmt.Errorf("%w: active -> retired", ErrGoalTransition), true},
		{"revision conflict", fmt.Errorf("update goal: %w: goal g", ErrGoalRevisionConflict), false},
		{"sqlite busy", errors.New("update goal: busy: database is locked"), false},
		{"deadline", fmt.Errorf("update goal: %w", context.DeadlineExceeded), false},
		{"io blip", errors.New("update goal: disk i/o timeout"), false},
	}
	for _, tc := range cases {
		if got := goalReserveErrorTerminal(tc.err); got != tc.terminal {
			t.Errorf("goalReserveErrorTerminal(%s) = %v, want %v", tc.name, got, tc.terminal)
		}
	}
}

// TestGoalRoundDriver_TransientReserveErrorKeepsArm pins A6-5: a transient
// reserve write failure (CAS blip) must NOT disarm the goal on the first
// hit — it retries on the next tick; only after driverReserveFailureCap
// consecutive failures does the C27 hot-loop brake engage. Under the old
// code the FIRST error of any kind disarmed permanently.
func TestGoalRoundDriver_TransientReserveErrorKeepsArm(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_transient")
	g, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_transient", Title: "tr", Mandate: "m", Source: "user", MaxRounds: 5,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness)
	setReserveWriteHook(t, d, func(*Goal) error {
		return fmt.Errorf("update goal: %w: concurrent update detected", ErrGoalRevisionConflict)
	})

	// First transient failure: idle + error, but the arm SURVIVES.
	if outcome, err := d.OnRoundSettled(ctx, "emp_transient"); err == nil || outcome != GoalRoundIdle {
		t.Fatalf("transient reserve = %v, %v; want idle+error", outcome, err)
	}
	d.mu.Lock()
	override, ok := d.armed[g.ID]
	d.mu.Unlock()
	if !ok || override == nil || !*override {
		t.Fatal("transient CAS error must keep the goal armed for retry (A6-5); it was disarmed")
	}
	if d.isInflight("emp_transient") {
		t.Fatal("a failed reserve must not leave the employee inflight")
	}

	// Cap: consecutive transient failures eventually engage the C27 brake
	// (first failure above already counted as strike 1).
	for i := 1; i < driverReserveFailureCap; i++ {
		if _, err := d.OnRoundSettled(ctx, "emp_transient"); err == nil {
			t.Fatalf("transient reserve %d unexpectedly succeeded", i)
		}
	}
	d.mu.Lock()
	brake, hasBrake := d.armed[g.ID]
	d.mu.Unlock()
	if !hasBrake || brake == nil || *brake {
		t.Fatal("reserve failures reaching the cap must disarm the goal (hot-loop brake)")
	}
}

// TestGoalRoundDriver_TerminalReserveErrorDisarms pins A6-5's terminal leg:
// a not-found write failure disarms immediately (the old behavior, kept for
// the genuinely-dead case).
func TestGoalRoundDriver_TerminalReserveErrorDisarms(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_terminal")
	g, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_terminal", Title: "tm", Mandate: "m", Source: "user", MaxRounds: 5,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness)
	setReserveWriteHook(t, d, func(*Goal) error {
		return fmt.Errorf("update goal: %w", ErrGoalNotFound)
	})
	if outcome, err := d.OnRoundSettled(ctx, "emp_terminal"); err == nil || outcome != GoalRoundIdle {
		t.Fatalf("terminal reserve = %v, %v; want idle+error", outcome, err)
	}
	d.mu.Lock()
	override, ok := d.armed[g.ID]
	d.mu.Unlock()
	if !ok || override == nil || *override {
		t.Fatal("terminal not-found reserve failure must disarm the goal")
	}
}

// TestGoalRoundDriver_TransientRecoveryClearsStreak pins that a successful
// reserve after transient blips resets the failure streak (so occasional
// conflicts never accumulate toward the cap).
func TestGoalRoundDriver_TransientRecoveryClearsStreak(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_recover")
	if _, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_recover", Title: "rc", Mandate: "m", Source: "user", MaxRounds: 5,
	}); err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()
	var blips int
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(harness)
	setReserveWriteHook(t, d, func(*Goal) error {
		if blips == 0 {
			blips++
			return fmt.Errorf("update goal: %w", ErrGoalRevisionConflict)
		}
		return nil // fall through to the real store write
	})
	if _, err := d.OnRoundSettled(ctx, "emp_recover"); err == nil {
		t.Fatal("injected transient failure did not surface")
	}
	if outcome, err := d.OnRoundSettled(ctx, "emp_recover"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("recovery settle = %v, %v; want advanced", outcome, err)
	}
	d.mu.Lock()
	strikes := d.reserveFails["emp_recover"]
	d.mu.Unlock()
	if strikes != 0 {
		t.Fatalf("reserveFails = %d, want 0 after a successful reserve", strikes)
	}
}

// ---------------------------------------------------------------------------
// A6-13 — armed/deferredGoal/goalOwner reclamation
// ---------------------------------------------------------------------------

// TestGoalRoundDriver_AutoBlockPurgesTracking pins A6-13 (block edge): when
// a goal auto-blocks at the cap, the driver drops its per-goal tracking
// entries entirely (the old shape kept a permanent armed=false entry — one
// per goal the process ever drove).
func TestGoalRoundDriver_AutoBlockPurgesTracking(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_purge")
	g, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_purge", Title: "pg", Mandate: "m", Source: "user", MaxRounds: 1,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).WithDeferredScheduler(harness)

	if outcome, err := d.OnRoundSettled(ctx, "emp_purge"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("advance = %v, %v", outcome, err)
	}
	d.mu.Lock()
	_, armedBefore := d.armed[g.ID]
	ownerBefore := d.goalOwner[g.ID]
	d.mu.Unlock()
	if !armedBefore || ownerBefore != "emp_purge" {
		t.Fatalf("armed/goalOwner entries missing after reserve (armed=%v owner=%q)", armedBefore, ownerBefore)
	}
	harness.fireAll()

	if outcome, err := d.OnRoundSettled(ctx, "emp_purge"); err != nil || outcome != GoalRoundBlocked {
		t.Fatalf("cap settle = %v, %v; want blocked", outcome, err)
	}
	d.mu.Lock()
	_, armedAfter := d.armed[g.ID]
	_, ownerAfter := d.goalOwner[g.ID]
	dgAfter := d.deferredGoal["emp_purge"]
	d.mu.Unlock()
	if armedAfter || ownerAfter {
		t.Fatal("auto-block must purge armed+goalOwner entries (A6-13)")
	}
	if dgAfter != "" {
		t.Fatalf("deferredGoal must clear for the blocked goal, got %q", dgAfter)
	}
}

// TestGoalRoundDriver_TrackingSizeStable pins A6-13 (size stability):
// driving a series of goals to their caps keeps the tracking maps bounded —
// the old code accumulated one armed entry per goal forever.
func TestGoalRoundDriver_TrackingSizeStable(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_bloat")
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).WithDeferredScheduler(harness)

	const goals = 12
	for i := range goals {
		if _, err := mgr.CreateGoal(ctx, GoalSpec{
			EmployeeID: "emp_bloat", Title: fmt.Sprintf("g%d", i), Mandate: "m",
			Source: "user", MaxRounds: 1,
		}); err != nil {
			t.Fatalf("create goal %d: %v", i, err)
		}
		if _, err := d.OnRoundSettled(ctx, "emp_bloat"); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
		harness.fireAll() // settles the deferred round
		if _, err := d.OnRoundSettled(ctx, "emp_bloat"); err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
		// Production GC path: the roster settle pass ends with the
		// tracking sweep (settleQuietEmployees -> gcGoalTracking).
		d.settleQuietEmployees(ctx)
		d.mu.Lock()
		armedN, ownerN := len(d.armed), len(d.goalOwner)
		d.mu.Unlock()
		if armedN > 1 || ownerN > 1 {
			t.Fatalf("after goal %d: armed=%d goalOwner=%d; must stay bounded (A6-13 unbounded growth)", i, armedN, ownerN)
		}
	}
	// Every goal blocked through the cap: nothing should remain tracked.
	d.mu.Lock()
	finalArmed, finalOwner := len(d.armed), len(d.goalOwner)
	d.mu.Unlock()
	if finalArmed != 0 || finalOwner != 0 {
		t.Fatalf("final tracking size armed=%d owner=%d, want 0/0", finalArmed, finalOwner)
	}
}

// TestGoalRoundDriver_GarbageCollectsTerminalGoals pins A6-13 (cross-
// surface reclamation): a goal retired through another surface (the
// goal_update tool or human RPC, not the driver's own block edge) is
// dropped from the tracking maps by the periodic GC.
func TestGoalRoundDriver_GarbageCollectsTerminalGoals(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_gc")
	g, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_gc", Title: "gc", Mandate: "m", Source: "user", MaxRounds: 5,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}
	harness := newGoalRoundDriverAutoTestHarness()
	d := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).WithDeferredScheduler(harness)
	if _, err := d.OnRoundSettled(ctx, "emp_gc"); err != nil {
		t.Fatalf("advance: %v", err)
	}
	harness.fireAll()

	// Retire out-of-band (the human goal_update complete path).
	reloaded, err := mgr.GetGoal(ctx, g.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	reloaded.State = GoalRetired
	if err := mgr.UpdateGoal(ctx, reloaded); err != nil {
		t.Fatalf("retire: %v", err)
	}

	// Production GC path: the roster settle pass sweeps tracking.
	d.settleQuietEmployees(ctx)
	d.mu.Lock()
	_, stillArmed := d.armed[g.ID]
	_, stillOwner := d.goalOwner[g.ID]
	d.mu.Unlock()
	if stillArmed || stillOwner {
		t.Fatal("GC must drop tracking entries for retired goals (A6-13)")
	}
}

// ---------------------------------------------------------------------------
// A6-10 — gate resume decodes the NESTED reentry payload (Result survives)
// ---------------------------------------------------------------------------

// setReserveWriteHook installs the driver's test-only reserve-write failure
// injection seam (the field is package-internal; production never sets it).
func setReserveWriteHook(t *testing.T, d *GoalRoundDriver, fn func(g *Goal) error) {
	t.Helper()
	d.mu.Lock()
	d.reserveWriteHook = fn
	d.mu.Unlock()
	t.Cleanup(func() {
		d.mu.Lock()
		d.reserveWriteHook = nil
		d.mu.Unlock()
	})
}

// agentGateParkData mirrors agent.GateParkData's JSON shape locally so the
// resume pin round-trips the park row through real JSON (persisted-row
// shapes) before handing TurnReentry to the resume path.
type agentGateParkData struct {
	GateKind    string         `json:"gate_kind"`
	GateID      string         `json:"gate_id"`
	TurnReentry map[string]any `json:"turn_reentry"`
}

// TestResumeGoalGateApproval_ReplaysRecordedResult pins A6-10: the park
// writes the full goalGateResumePayload (plan_id, prompt, RESULT) nested
// under the outer TurnReentry "reentry" key; the resume must decode the
// nested payload so Reflect replays with the recorded result — a SUCCESS.
// Under the old outer-map decode p.Result was always nil, the resume took
// Reflect's failure path and bumped consecutiveFailures.
func TestResumeGoalGateApproval_ReplaysRecordedResult(t *testing.T) {
	loop, _, cap, _ := newApprovalGateLoop(t)

	result := &bot.BotExecutionResult{BotID: "emp-gate-1", Success: true, Output: "the work", TokensUsed: 42}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "plan-r1", Prompt: "p"}, result); err != nil {
		t.Fatalf("parking reflect: %v", err)
	}
	if len(cap.parked) != 1 {
		t.Fatalf("parked rows = %d, want 1", len(cap.parked))
	}
	// Simulate the FULL persistence round-trip: the row goes through the
	// park store's JSON (GateParkData) and comes back decoded — exactly the
	// bytes the daemon hands ResumeGoalGate.
	raw, err := json.Marshal(cap.parked[0])
	if err != nil {
		t.Fatalf("marshal park row: %v", err)
	}
	var decoded agentGateParkData
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode park row: %v", err)
	}
	// Sanity: the nested reentry survived the round-trip carrying the
	// result — the key the old code never read.
	if _, ok := decoded.TurnReentry["reentry"]; !ok {
		t.Fatalf("park row lost the nested reentry payload: %#v", decoded.TurnReentry)
	}

	loop.ResumeGoalGate(context.Background(), decoded.TurnReentry)
	if len(cap.parked) != 1 {
		t.Fatalf("approved re-entry parked again (%d rows)", len(cap.parked))
	}
	// The discriminator (A6-10): Reflect replays WITH the recorded result,
	// so the loop's lastResult is the SUCCESS result the park stored. The
	// old outer-map decode handed Reflect a nil Result — lastResult ended
	// nil and the success branch (LLM assessment, speak-notify, gate
	// replay) never ran at all.
	loop.mu.Lock()
	lr := loop.lastResult
	loop.mu.Unlock()
	if lr == nil || !lr.Success || lr.Output != "the work" {
		t.Fatalf("resume did not replay the recorded result (A6-10): lastResult = %+v, want the parked success", lr)
	}
	if got := loop.ConsecutiveFailures(); got != 0 {
		t.Fatalf("ConsecutiveFailures after resume = %d, want 0", got)
	}
}

// TestResumeGoalGateApproval_LegacyOuterShapeFallback pins the dual-read: a
// reentry map WITHOUT a nested "reentry" blob (older rows) still resumes
// with plan_id/prompt/result from the outer fields.
func TestResumeGoalGateApproval_LegacyOuterShapeFallback(t *testing.T) {
	loop, _, cap, _ := newApprovalGateLoop(t)
	result := &bot.BotExecutionResult{BotID: "emp-gate-1", Success: true, Output: "done"}
	if _, err := loop.Reflect(context.Background(), PlanRef{ID: "plan-legacy", Prompt: "p"}, result); err != nil {
		t.Fatalf("parking reflect: %v", err)
	}
	loop.ResumeGoalGateApproval(context.Background(), map[string]any{
		"plan_id": "plan-legacy",
		"prompt":  "p",
		"result":  result,
	})
	if loop.ConsecutiveFailures() != 0 {
		t.Fatalf("legacy outer-shape resume must still replay the result; failures=%d", loop.ConsecutiveFailures())
	}
	if len(cap.parked) != 1 {
		t.Fatalf("legacy resume re-parked (%d)", len(cap.parked))
	}
}

// TestResumeGoalGateApproval_MissingPlanIDDropped pins the guard: a payload
// without any decodable plan id is logged and dropped (no panic, no
// second park).
func TestResumeGoalGateApproval_MissingPlanIDDropped(t *testing.T) {
	loop, _, cap, _ := newApprovalGateLoop(t)
	loop.ResumeGoalGateApproval(context.Background(), map[string]any{"employee_id": "emp-gate-1"})
	if len(cap.parked) != 0 {
		t.Fatalf("planless resume ran a park/reflect (%d rows)", len(cap.parked))
	}
}

// TestNormalizeReentryBlob pins the lenient blob coercion (A6-10):
// json.RawMessage, []byte, string-of-JSON and map shapes all decode; junk
// yields nil so the outer fallback decides.
func TestNormalizeReentryBlob(t *testing.T) {
	payload := `{"plan_id":"p1","result":{"success":true}}`
	if got := string(normalizeReentryBlob(json.RawMessage(payload))); got != payload {
		t.Errorf("RawMessage: got %q", got)
	}
	if got := string(normalizeReentryBlob([]byte(payload))); got != payload {
		t.Errorf("[]byte: got %q", got)
	}
	if got := string(normalizeReentryBlob(payload)); got != payload {
		t.Errorf("string: got %q", got)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if got := normalizeReentryBlob(m); len(got) == 0 {
		t.Error("map shape produced no bytes")
	}
	if normalizeReentryBlob(42) != nil || normalizeReentryBlob(nil) != nil {
		t.Error("junk/nil shapes must yield nil (outer fallback decides)")
	}
	if normalizeReentryBlob("   ") != nil {
		t.Error("blank string must yield nil")
	}
}
