package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// stubSinkTaskCreator satisfies plan.TaskCreator so ApprovePlan's trailing
// Synthesize succeeds without the real task registry.
type stubSinkTaskCreator struct{}

func (stubSinkTaskCreator) CreateTask(context.Context, string, string) (*task.Task, error) {
	return task.NewTask("sink-stub", "sink stub task"), nil
}

func (stubSinkTaskCreator) CreateTaskStep(_ context.Context, taskID, description string, sequence int) (*task.TaskStep, error) {
	return task.NewTaskStep(taskID, description, sequence), nil
}

func (stubSinkTaskCreator) UpdateTaskStep(context.Context, *task.TaskStep) error {
	return nil
}

func (stubSinkTaskCreator) LinkSession(context.Context, string, string) error {
	return nil
}

func (stubSinkTaskCreator) SetTaskJobCount(context.Context, string, int) error {
	return nil
}

func (stubSinkTaskCreator) ScheduleSteps(context.Context, string) error {
	return nil
}

// TestPlanHandler_EvolverSinkFallback verifies that a plan living in the
// evolver's dedicated sink store (plan-sink leaf 01) is listable, gettable,
// and approvable through the shared RPC handler — the approval dead end
// must not reappear at the CLI/RPC layer.
func TestPlanHandler_EvolverSinkFallback(t *testing.T) {
	dir := t.TempDir()

	sharedStore, err := plan.NewSQLiteStore(filepath.Join(dir, "shared.db"), slog.Default())
	if err != nil {
		t.Fatalf("shared store: %v", err)
	}
	t.Cleanup(func() {
		if err := sharedStore.Close(); err != nil {
			t.Logf("shared store close: %v", err)
		}
	})
	sinkStore, err := plan.NewSQLiteStore(filepath.Join(dir, "sink.db"), slog.Default())
	if err != nil {
		t.Fatalf("sink store: %v", err)
	}
	t.Cleanup(func() {
		if err := sinkStore.Close(); err != nil {
			t.Logf("sink store close: %v", err)
		}
	})

	ctx := context.Background()
	sinkMgr := plan.NewPlanManager(sinkStore, nil, config.Config{}.Plans, stubSinkTaskCreator{}, slog.Default())
	if sinkMgr == nil {
		t.Fatal("sink manager construction failed")
	}

	// Seed one plan in each store, bypassing the managers (direct store
	// writes) so the shared-side plan is not pending-approval gated.
	sharedPlan := &plan.Plan{ID: "plan-shared-0001", Title: "human plan", ProjectID: "proj", State: plan.StatePendingApproval}
	if err := sharedStore.CreatePlan(ctx, sharedPlan); err != nil {
		t.Fatalf("seed shared plan: %v", err)
	}
	sinkPlan := &plan.Plan{ID: "plan-sink-0001", Title: "evolver plan", ProjectID: "proj", State: plan.StatePendingApproval}
	if err := sinkStore.CreatePlan(ctx, sinkPlan); err != nil {
		t.Fatalf("seed sink plan: %v", err)
	}

	sharedMgr := plan.NewPlanManager(sharedStore, nil, config.Config{}.Plans, nil, slog.Default())
	h := NewPlanHandler(sharedMgr, sharedStore)
	h.SetEvolverSink(sinkMgr, sinkStore)

	// List: union of both stores.
	raw, err := h.handleList(ctx, json.RawMessage(`{"project_id":"proj"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	resp, _ := raw.(map[string]any)
	if resp == nil {
		t.Fatal("list response shape")
	}
	plans, _ := resp["plans"].([]*plan.Plan)
	if len(plans) != 2 {
		t.Fatalf("list returned %d plans, want 2 (shared + sink)", len(plans))
	}

	// Get: sink-only plan found via fallback.
	rawGet, err := h.handleGet(ctx, json.RawMessage(`{"id":"plan-sink-0001"}`))
	if err != nil {
		t.Fatalf("get sink plan: %v", err)
	}
	got, _ := rawGet.(*plan.Plan)
	if got == nil || got.ID != "plan-sink-0001" {
		t.Fatalf("get returned %+v, want the sink plan", rawGet)
	}

	// Approve: routes to the sink manager (shared side misses).
	rawApprove, err := h.handleApprove(ctx, json.RawMessage(`{"plan_id":"plan-sink-0001","session_id":"s1","by":"tester"}`))
	if err != nil {
		t.Fatalf("approve sink plan: %v", err)
	}
	approved, _ := rawApprove.(*plan.Plan)
	if approved == nil {
		t.Fatal("approve returned no plan")
	}
	// Synthesize may legitimately advance approved -> executing (task
	// creator present); the invariant is that the shared-side plan state
	// never changed and the sink plan LEFT pending_approval.
	if approved.State == plan.StatePendingApproval {
		t.Fatalf("approved plan still pending_approval: %+v", approved)
	}
	// Shared plan untouched.
	sp, err := sharedStore.GetPlan(ctx, "plan-shared-0001")
	if err != nil {
		t.Fatalf("shared get: %v", err)
	}
	if sp.State != plan.StatePendingApproval {
		t.Fatalf("shared plan state = %q, want pending_approval (cross-store approval must not happen)", sp.State)
	}
}
