package plan

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// TestSweepStalledExecutingAllStepsTerminalParentPending pins bughunt
// 2026-11-24 H4: the sweep's liveness predicate only caught a missing or
// FAILED parent. The synthesized parent is never scheduled (Synthesize
// calls CreateTask without ScheduleSteps), so it sits at `pending`
// forever while the plan's phases record the real progress — the exact
// "all children terminal, OnTaskCompleted lost" shape the sweep exists
// for was invisible to it. A stale executing plan whose parent is
// `pending` and whose phases show every step terminal must be swept.
func TestSweepStalledExecutingAllStepsTerminalParentPending(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sweeppending.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	creator := &noopCreator{}
	mgr := NewPlanManager(store, nil, config.PlansConfig{}, creator, logger)
	ctx := context.Background()

	stale := time.Now().UTC().Add(-8 * time.Hour)

	// Parent task stuck at pending (the production synthesized shape).
	pending := task.NewTask("unscheduled parent", "d")
	pending.State = task.StatePending
	creator.seed(pending)
	seedPlan(t, mgr, "plan-pending-parent", StateExecuting, stale, "")
	if err := store.UpdatePlan(ctx, &Plan{
		ID: "plan-pending-parent", Title: "t", State: StateExecuting,
		TaskID: pending.ID, UpdatedAt: stale,
	}); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	// All 6 steps terminal across 2 phases: completed 4 + failed 2.
	for i := range 2 {
		ph := NewPlanPhase("plan-pending-parent", "Phase", i+1, 3)
		ph.TotalSteps = 3
		ph.CompletedSteps = 2
		ph.FailedSteps = 1
		if err := store.CreatePhase(ctx, ph); err != nil {
			t.Fatalf("CreatePhase %d: %v", i, err)
		}
	}

	// Control: same shape but one phase still has 1 step unterminal ->
	// genuinely still moving, NOT swept.
	moving := task.NewTask("moving parent", "d")
	moving.State = task.StatePending
	creator.seed(moving)
	seedPlan(t, mgr, "plan-still-moving", StateExecuting, stale, "")
	if err := store.UpdatePlan(ctx, &Plan{
		ID: "plan-still-moving", Title: "t", State: StateExecuting,
		TaskID: moving.ID, UpdatedAt: stale,
	}); err != nil {
		t.Fatalf("backdate moving: %v", err)
	}
	phMoving := NewPlanPhase("plan-still-moving", "Phase", 1, 3)
	phMoving.TotalSteps = 3
	phMoving.CompletedSteps = 2
	if err := store.CreatePhase(ctx, phMoving); err != nil {
		t.Fatalf("CreatePhase moving: %v", err)
	}

	swept, err := mgr.SweepStalledExecuting(ctx, 6*time.Hour)
	if err != nil {
		t.Fatalf("SweepStalledExecuting: %v", err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want 1 (all-terminal pending-parent only)", swept)
	}
	got, err := store.GetPlan(ctx, "plan-pending-parent")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateFailed {
		t.Fatalf("pending-parent plan state = %q, want failed", got.State)
	}
	kept, err := store.GetPlan(ctx, "plan-still-moving")
	if err != nil {
		t.Fatalf("GetPlan moving: %v", err)
	}
	if kept.State != StateExecuting {
		t.Fatalf("still-moving plan must stay executing, got %q", kept.State)
	}
}
