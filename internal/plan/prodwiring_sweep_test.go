package plan_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/daemon"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// TestSweepStalledExecuting_ProductionTaskLookupWiring pins bughunt H4: the
// REAL daemon taskCreatorAdapter (the exact type the daemon installs as
// plan.TaskCreator in production) must satisfy plan.TaskLookup, and a
// terminal-failed parent task read from a REAL task.Store/Registry must
// drive SweepStalledExecuting to fail the plan.
//
// The earlier sweep tests used a hand-rolled noopCreator that declared
// GetTask itself, so they passed even when production's adapter lacked the
// seam entirely — the test-double trap. This test builds the adapter through
// daemon.NewTaskCreatorAdapter over a real task.Registry/Store, never a
// double, so the assertion can only pass when the production seam is live.
func TestSweepStalledExecuting_ProductionTaskLookupWiring(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	// Real task store + registry, same constructor the daemon uses.
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, logger), logger)
	if err != nil {
		t.Fatalf("task.NewRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	// The real production adapter, wired over the real registry. Compile-time
	// interface assertions below also guard the concrete type and the seam.
	creator := daemon.NewTaskCreatorAdapter(reg)
	if _, ok := creator.(plan.TaskLookup); !ok {
		t.Fatalf("production %T does not satisfy plan.TaskLookup: the stalled-executing sweep's read seam is dead", creator)
	}
	lookup, ok := creator.(plan.TaskLookup)
	if !ok {
		t.Fatalf("production %T lost plan.TaskLookup between assertions", creator)
	}

	// Create a real parent task and drive it to terminal-failed through the
	// registry's real state transition.
	parent, err := reg.Create(ctx, "failed parent", "d")
	if err != nil {
		t.Fatalf("registry.Create: %v", err)
	}
	// Drive the real task to terminal-failed along a legal edge:
	// pending -> executing -> failed (pending -> failed is not in the table).
	if err := reg.UpdateState(ctx, parent.ID, task.StateExecuting); err != nil {
		t.Fatalf("registry.UpdateState(executing): %v", err)
	}
	if err := reg.UpdateState(ctx, parent.ID, task.StateFailed); err != nil {
		t.Fatalf("registry.UpdateState(failed): %v", err)
	}
	readBack, err := lookup.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("lookup.GetTask(%s): %v", parent.ID, err)
	}
	if readBack == nil || readBack.State != task.StateFailed {
		t.Fatalf("lookup.GetTask = %+v, want a failed task", readBack)
	}

	// Real plan store and manager; the manager uses the real production
	// adapter as its taskCreator.
	store, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "sweep.db"), logger)
	if err != nil {
		t.Fatalf("plan.NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	msgBus := bus.New(nil, logger)
	sub := msgBus.Subscribe("prod-wiring-sweep", "plan.failed")
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	mgr := plan.NewPlanManager(store, msgBus, config.PlansConfig{}, creator, logger)

	// A stale executing plan whose recorded parent task is terminal-failed.
	mdPath := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(mdPath, []byte("# Plan: prod wiring\n\n## Meta\n\n- status: executing\n\n## Summary\n\nsweep pin\n"), 0o600); err != nil {
		t.Fatalf("write plan.md: %v", err)
	}
	p := plan.NewPlan("prod wiring", "sweep pin", "", mdPath, "sess-prod")
	p.State = plan.StateExecuting
	stale := time.Now().UTC().Add(-8 * time.Hour)
	p.CreatedAt = stale
	p.UpdatedAt = stale
	p.TaskID = parent.ID
	if err := store.CreatePlan(ctx, p); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	swept, err := mgr.SweepStalledExecuting(ctx, 6*time.Hour)
	if err != nil {
		t.Fatalf("SweepStalledExecuting: %v", err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want 1 — the failed-parent arm never fired in production", swept)
	}
	got, err := store.GetPlan(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != plan.StateFailed {
		t.Fatalf("plan state = %q, want failed", got.State)
	}

	// plan.failed must carry the actionable failed-parent reason.
	select {
	case m := <-sub.Channel:
		if !strings.Contains(string(m.Payload), p.ID) {
			t.Errorf("plan.failed payload %q missing plan id %s", m.Payload, p.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for plan.failed")
	}

	// L8: plan.md status must be stamped failed, not left saying executing.
	body, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read plan.md: %v", err)
	}
	if !strings.Contains(string(body), "- status: failed") {
		t.Fatalf("plan.md still says executing after sweep; want failed status line. Got:\n%s", body)
	}
}
