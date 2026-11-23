package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// Leaf 10 — plan lifecycle hardening (20261121 interactive-usability):
// (a) zero-phase synthesis completes the plan honestly instead of
// creating an orphan executing parent task; (b) the stalled-executing
// sweep fails an executing plan whose children are terminal-or-missing
// beyond the configured window and publishes plan.failed; a zero window
// disables it.

// noopCreator is a TaskCreator that records every parent create and serves
// seeded tasks back through the TaskLookup seam; used to prove the
// zero-phase guard never touches the task system and to drive the sweep's
// terminal-child arm in ISOLATION (no real task store). This GetTask method
// is a test-double stand-in for the production seam; it is NOT proof that
// the production taskCreatorAdapter implements TaskLookup — that is pinned
// separately by TestSweepStalledExecuting_ProductionTaskLookupWiring, which
// uses the real daemon adapter over a real task.Registry.
type noopCreator struct {
	parents int
	tasks   map[string]*task.Task
}

func (c *noopCreator) CreateTask(_ context.Context, _, _ string) (*task.Task, error) {
	c.parents++
	return task.NewTask("x", "d"), nil
}
func (c *noopCreator) CreateTaskStep(_ context.Context, _, _ string, _ int) (*task.TaskStep, error) {
	return task.NewTaskStep("t", "s", 0), nil
}
func (c *noopCreator) UpdateTaskStep(context.Context, *task.TaskStep) error { return nil }
func (c *noopCreator) LinkSession(context.Context, string, string) error    { return nil }
func (c *noopCreator) SetTaskJobCount(context.Context, string, int) error   { return nil }
func (c *noopCreator) ScheduleSteps(context.Context, string) error          { return nil }

// GetTask is the TaskLookup seam the stalled-executing sweep reads.
// A genuinely absent parent uses the store's ErrTaskNotFound sentinel so the
// sweep's "missing parent" arm fires; other errors are treated as transient
// by stalledExecutingReason and leave the plan alone.
func (c *noopCreator) GetTask(_ context.Context, taskID string) (*task.Task, error) {
	if tk, ok := c.tasks[taskID]; ok {
		return tk, nil
	}
	return nil, fmt.Errorf("task %s: %w", taskID, task.ErrTaskNotFound)
}

func (c *noopCreator) seed(tk *task.Task) {
	if c.tasks == nil {
		c.tasks = make(map[string]*task.Task)
	}
	c.tasks[tk.ID] = tk
}

// TestSynthesizeZeroPhasesCompletesHonestly pins guard (b): an approved
// plan whose markdown parses to zero phases completes the plan without
// creating any task — the evolver approval bridge already applied the
// document, so there is nothing to expand and executing is a lie.
func TestSynthesizeZeroPhasesCompletesHonestly(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "zerophase.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	creator := &noopCreator{}
	cfg := config.PlansConfig{
		Approval: config.PlansApprovalConfig{RequireApproval: true},
	}
	mgr := NewPlanManager(store, nil, cfg, creator, logger)
	ctx := context.Background()

	// A plan whose file parses to ZERO phases (summary only, no "## Phase").
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "plan.md")
	body := "# Plan: evolver doc\n\n## Summary\n\napplied document, no phases\n"
	if err := os.WriteFile(mdPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write plan.md: %v", err)
	}

	p := NewPlan("evolver doc", "d", "", mdPath, "")
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := store.SetPlanState(ctx, p.ID, StateApproved); err != nil {
		t.Fatalf("SetPlanState approved: %v", err)
	}

	if err := mgr.Synthesize(ctx, p.ID); err != nil {
		t.Fatalf("Synthesize zero-phase: %v", err)
	}

	if creator.parents != 0 {
		t.Fatalf("zero-phase Synthesize created %d task(s); must skip task creation entirely", creator.parents)
	}

	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateCompleted {
		t.Fatalf("zero-phase plan state = %q, want completed (honest completion)", got.State)
	}
	if got.TaskID != "" {
		t.Fatalf("zero-phase plan carries task_id %q; must stay taskless", got.TaskID)
	}
}

// drainFailed collects n plan.failed payloads off the bus.
func drainFailed(t *testing.T, sub *bus.Subscriber, n int) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, n)
	deadline := time.After(5 * time.Second)
	for len(out) < n {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				t.Fatalf("channel closed after %d/%d events", len(out), n)
			}
			var p map[string]any
			if err := json.Unmarshal(m.Payload, &p); err != nil {
				t.Fatalf("decode plan.failed payload: %v", err)
			}
			out = append(out, p)
		case <-deadline:
			t.Fatalf("timed out after %d/%d plan.failed events", len(out), n)
		}
	}
	return out
}

// TestSweepStalledExecutingPlans pins guard (c): an executing plan whose
// parent task is MISSING (no task_id recorded / task not in the store) and
// whose UpdatedAt sits beyond the sweep window is transitioned to failed
// and publishes plan.failed {plan_id, session_id, reason}. A zero window
// disables the sweep entirely (the separate disabled pin below).
func TestSweepStalledExecutingPlans(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sweep.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	msgBus := bus.New(nil, logger)
	sub := msgBus.Subscribe("l10-sweep", "plan.failed")
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	creator := &noopCreator{}
	cfg := config.PlansConfig{
		Approval: config.PlansApprovalConfig{RequireApproval: true},
	}
	mgr := NewPlanManager(store, msgBus, cfg, creator, logger)
	ctx := context.Background()

	// Stale executing plan, missing parent task, last touched 8h ago.
	stale := time.Now().UTC().Add(-8 * time.Hour)
	seedPlan(t, mgr, "stale-exec", StateExecuting, stale, "")
	if err := store.UpdatePlan(ctx, &Plan{
		ID: "stale-exec", Title: "stale", State: StateExecuting,
		SourceSession: "sess-stale", UpdatedAt: stale,
	}); err != nil {
		t.Fatalf("backdate stale-exec: %v", err)
	}

	// Fresh executing plan within the window: untouched.
	fresh := time.Now().UTC()
	seedPlan(t, mgr, "fresh-exec", StateExecuting, fresh, "")

	// Sweep with a 6h window: the stale plan fails.
	swept, err := mgr.SweepStalledExecuting(ctx, 6*time.Hour)
	if err != nil {
		t.Fatalf("SweepStalledExecuting: %v", err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want 1", swept)
	}

	got, err := store.GetPlan(ctx, "stale-exec")
	if err != nil {
		t.Fatalf("GetPlan stale: %v", err)
	}
	if got.State != StateFailed {
		t.Fatalf("stale plan state = %q, want failed", got.State)
	}

	freshGot, err := store.GetPlan(ctx, "fresh-exec")
	if err != nil {
		t.Fatalf("GetPlan fresh: %v", err)
	}
	if freshGot.State != StateExecuting {
		t.Fatalf("fresh plan state = %q, want untouched executing", freshGot.State)
	}

	// plan.failed published with the contract payload.
	events := drainFailed(t, sub, 1)
	payload := events[0]
	if payload["plan_id"] != "stale-exec" {
		t.Errorf("plan.failed plan_id = %v, want stale-exec", payload["plan_id"])
	}
	if payload["session_id"] != "sess-stale" {
		t.Errorf("plan.failed session_id = %v, want sess-stale", payload["session_id"])
	}
	if reason, _ := payload["reason"].(string); reason == "" {
		t.Error("plan.failed reason empty; must carry an actionable reason")
	}
}

// TestSweepStalledExecutingDisabled pins the off switch: a zero window is
// a no-op sweep (0 swept, no state changes, no events).
func TestSweepStalledExecutingDisabled(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sweepoff.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mgr := NewPlanManager(store, nil, config.PlansConfig{}, &noopCreator{}, logger)
	ctx := context.Background()

	stale := time.Now().UTC().Add(-100 * time.Hour)
	seedPlan(t, mgr, "ancient-exec", StateExecuting, stale, "")

	swept, err := mgr.SweepStalledExecuting(ctx, 0)
	if err != nil {
		t.Fatalf("SweepStalledExecuting(0): %v", err)
	}
	if swept != 0 {
		t.Fatalf("swept = %d with disabled window, want 0", swept)
	}
	got, err := store.GetPlan(ctx, "ancient-exec")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateExecuting {
		t.Fatalf("plan state = %q after disabled sweep, want untouched executing", got.State)
	}
}

// TestSweepStalledExecutingTerminalChildren pins the terminal-child arm of
// the sweep: an executing plan whose parent task EXISTS in a terminal
// state is swept — a FAILED parent fails the plan; a COMPLETED parent is
// left alone (OnTaskCompleted owns completion; the sweep must never
// complete a plan it did not verify phase-by-phase).
func TestSweepStalledExecutingTerminalChildren(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "sweepterm.db"), logger)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	creator := &noopCreator{}
	mgr := NewPlanManager(store, nil, config.PlansConfig{}, creator, logger)
	ctx := context.Background()

	stale := time.Now().UTC().Add(-8 * time.Hour)

	// Failed parent task, stale executing plan -> swept to failed.
	tk := task.NewTask("failed parent", "d")
	tk.State = task.StateFailed
	creator.seed(tk)
	seedPlan(t, mgr, "plan-failed-child", StateExecuting, stale, "")
	if err := store.UpdatePlan(ctx, &Plan{
		ID: "plan-failed-child", Title: "t", State: StateExecuting,
		TaskID: tk.ID, UpdatedAt: stale,
	}); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// Completed parent task, stale executing plan -> NOT the sweep's job;
	// left untouched.
	ok := task.NewTask("done parent", "d")
	ok.State = task.StateCompleted
	creator.seed(ok)
	seedPlan(t, mgr, "plan-done-child", StateExecuting, stale, "")
	if err := store.UpdatePlan(ctx, &Plan{
		ID: "plan-done-child", Title: "t", State: StateExecuting,
		TaskID: ok.ID, UpdatedAt: stale,
	}); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	swept, err := mgr.SweepStalledExecuting(ctx, 6*time.Hour)
	if err != nil {
		t.Fatalf("SweepStalledExecuting: %v", err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want 1 (failed child only)", swept)
	}
	got, err := store.GetPlan(ctx, "plan-failed-child")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != StateFailed {
		t.Fatalf("failed-child plan state = %q, want failed", got.State)
	}
	kept, err := store.GetPlan(ctx, "plan-done-child")
	if err != nil {
		t.Fatalf("GetPlan done-child: %v", err)
	}
	if kept.State != StateExecuting {
		t.Fatalf("done-child plan state = %q, want untouched executing", kept.State)
	}
}

// transientLookupError is a TaskLookup whose read fails with a non-sentinel
// error, modeling a locked/lagging store rather than a missing parent.
type transientLookupError struct{}

func (transientLookupError) GetTask(context.Context, string) (*task.Task, error) {
	return nil, fmt.Errorf("database is locked")
}

func TestStalledExecutingReason_TransientLookupErrorLeavesPlanAlone(t *testing.T) {
	m := &PlanManager{logger: slog.New(slog.DiscardHandler)}
	p := &Plan{ID: "plan-transient", TaskID: "task-live"}
	stalled, reason := m.stalledExecutingReason(context.Background(), p, transientLookupError{})
	if stalled || reason != "" {
		t.Fatalf("transient lookup error must leave plan alone: stalled=%v reason=%q", stalled, reason)
	}
}
