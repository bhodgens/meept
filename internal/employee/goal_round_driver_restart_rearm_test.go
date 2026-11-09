package employee

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// TestGoalRoundDriver_RestartRearmsDeferredRound (DSH-3 residual) pins the
// restart re-arm contract: the scheduler's loadPersistedJobs deliberately
// drops interval-type jobs at boot, so an in-flight one-shot deferred goal
// round is lost on restart. The driver's poll loop is the recovery path —
// every tick it scans the roster and re-drives any employee whose goal is
// drivable (active goals are armed-by-default after a fresh process; see
// isArmed's "active default"), re-reserving the round and re-scheduling the
// deferred turn with zero human action.
//
// This test simulates the restart by driving settlement through a FRESH
// driver (new armed/inflight maps, mirroring a new process) over the same
// goal store: the reserved round from the "pre-restart" driver must be
// re-driven by the new driver's poll loop without any manual Arm call or
// RPC advance.
func TestGoalRoundDriver_RestartRearmsDeferredRound(t *testing.T) {
	ctx := context.Background()
	mgr := newDriverAutoTestManager(t, "emp_restart")

	g, err := mgr.CreateGoal(ctx, GoalSpec{
		EmployeeID: "emp_restart", Title: "restart", Mandate: "m", Source: "user", MaxRounds: 3,
	})
	if err != nil {
		t.Fatalf("create goal: %v", err)
	}

	// Pre-restart process: reserve round 1 (the deferred scheduler dies with
	// the process — the harness holds the registration, but a restart loses
	// both the process memory AND the scheduler's interval jobs).
	preHarness := newGoalRoundDriverAutoTestHarness()
	pre := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(preHarness)
	if outcome, err := pre.OnRoundSettled(ctx, "emp_restart"); err != nil || outcome != GoalRoundAdvanced {
		t.Fatalf("pre-restart settle = %v, %v; want advanced", outcome, err)
	}
	// The deferred turn never fires (restart kills the scheduler job).
	reloaded, err := mgr.GetGoal(ctx, g.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.RoundsStarted != 1 {
		t.Fatalf("pre-restart rounds_started = %d, want 1", reloaded.RoundsStarted)
	}

	// Restart: a fresh driver (empty armed/inflight maps — the new process's
	// memory shape) over the SAME goal store. Its Start loop must re-arm the
	// missed round on its own: the active goal is armed-by-default, quiescence
	// passes (nil check), and the next tick reserves + schedules round 2.
	postHarness := newGoalRoundDriverAutoTestHarness()
	post := NewGoalRoundDriver(mgr, slog.New(slog.DiscardHandler)).
		WithDeferredScheduler(postHarness).
		WithPollInterval(5 * time.Millisecond)
	post.Start(ctx)
	t.Cleanup(post.Stop)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if postHarness.count() > 0 {
			// The loop re-scheduled the deferred round after restart.
			post.mu.Lock()
			inflight := post.inflight["emp_restart"]
			post.mu.Unlock()
			if !inflight {
				t.Fatal("re-armed deferred turn must mark the employee in-flight")
			}
			round2, err := mgr.GetGoal(ctx, g.ID)
			if err != nil {
				t.Fatalf("reload round 2: %v", err)
			}
			if round2.RoundsStarted != 2 {
				t.Fatalf("rounds_started = %d, want 2 (loop must reserve the missed round)", round2.RoundsStarted)
			}
			return // re-arm verified: no manual advance was needed
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("fresh driver's poll loop never re-armed the deferred round after restart")
}
