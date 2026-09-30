package services

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// stubServiceTaskCreator satisfies plan.TaskCreator so ApprovePlan's
// trailing Synthesize succeeds without the real task registry (same shape
// as internal/rpc/plan_sink_fallback_test.go's stub).
type stubServiceTaskCreator struct{}

func (stubServiceTaskCreator) CreateTask(context.Context, string, string) (*task.Task, error) {
	return task.NewTask("svc-stub", "svc stub task"), nil
}

func (stubServiceTaskCreator) CreateTaskStep(_ context.Context, taskID, description string, sequence int) (*task.TaskStep, error) {
	return task.NewTaskStep(taskID, description, sequence), nil
}

func (stubServiceTaskCreator) UpdateTaskStep(context.Context, *task.TaskStep) error {
	return nil
}

func (stubServiceTaskCreator) LinkSession(context.Context, string, string) error {
	return nil
}

func (stubServiceTaskCreator) SetTaskJobCount(context.Context, string, int) error {
	return nil
}

func (stubServiceTaskCreator) ScheduleSteps(context.Context, string) error {
	return nil
}

// newSinkFallbackTestStores builds a shared store + manager and a sink
// store + manager, with one pending plan seeded in each.
func newSinkFallbackTestStores(t *testing.T) (sharedMgr *plan.PlanManager, sharedStore plan.PlanStore, sinkMgr *plan.PlanManager, sinkStore plan.PlanStore) {
	t.Helper()
	dir := t.TempDir()

	var err error
	sharedStore, err = plan.NewSQLiteStore(filepath.Join(dir, "shared.db"), slog.Default())
	if err != nil {
		t.Fatalf("shared store: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := sharedStore.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})
	sinkStore, err = plan.NewSQLiteStore(filepath.Join(dir, "sink.db"), slog.Default())
	if err != nil {
		t.Fatalf("sink store: %v", err)
	}
	t.Cleanup(func() {
		if closer, ok := sinkStore.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	})

	ctx := context.Background()
	sinkPlan := &plan.Plan{ID: "plan-sink-svc-1", Title: "evolver plan", ProjectID: "proj", State: plan.StatePendingApproval}
	if err := sinkStore.CreatePlan(ctx, sinkPlan); err != nil {
		t.Fatalf("seed sink plan: %v", err)
	}
	sinkPlan2 := &plan.Plan{ID: "plan-sink-svc-2", Title: "evolver plan 2", ProjectID: "proj", State: plan.StatePendingApproval}
	if err := sinkStore.CreatePlan(ctx, sinkPlan2); err != nil {
		t.Fatalf("seed sink plan 2: %v", err)
	}
	sharedPlan := &plan.Plan{ID: "plan-shared-svc-1", Title: "human plan", ProjectID: "proj", State: plan.StatePendingApproval}
	if err := sharedStore.CreatePlan(ctx, sharedPlan); err != nil {
		t.Fatalf("seed shared plan: %v", err)
	}

	sinkMgr = plan.NewPlanManager(sinkStore, nil, config.Config{}.Plans, stubServiceTaskCreator{}, slog.Default())
	sharedMgr = plan.NewPlanManager(sharedStore, nil, config.Config{}.Plans, stubServiceTaskCreator{}, slog.Default())
	return sharedMgr, sharedStore, sinkMgr, sinkStore
}

// Pins the L6 fix: HTTP plan transitions must fall back to the evolver sink
// when the shared side misses, and the read-back must come from the sink —
// the same contract the RPC surface pinned in d9688804.
func TestPlanService_SinkFallback(t *testing.T) {
	sharedMgr, sharedStore, sinkMgr, sinkStore := newSinkFallbackTestStores(t)
	svc := NewPlanService(sharedMgr, sharedStore)
	svc.SetEvolverSink(sinkMgr, sinkStore)
	ctx := context.Background()

	t.Run("approve sink plan", func(t *testing.T) {
		p, err := svc.Approve(ctx, ApprovePlanRequest{PlanID: "plan-sink-svc-1", SessionID: "s1", By: "tester"})
		if err != nil {
			t.Fatalf("Approve sink plan: %v", err)
		}
		// ApprovePlan runs Synthesize, which advances the plan past
		// pending_approval (to executing when phases exist); the exact
		// post-approve state is manager policy — the pin here is that the
		// transition SUCCEEDED and the read-back came from the sink.
		if p == nil || p.State == plan.StatePendingApproval {
			t.Fatalf("sink plan state = %+v, want transitioned out of pending_approval", p)
		}
	})

	t.Run("reject sink plan", func(t *testing.T) {
		p, err := svc.Reject(ctx, RejectPlanRequest{PlanID: "plan-sink-svc-2", SessionID: "s1", By: "tester", Reason: "nope"})
		if err != nil {
			t.Fatalf("Reject sink plan: %v", err)
		}
		if p == nil || p.State != plan.StateCancelled {
			t.Fatalf("sink plan state = %+v, want cancelled", p)
		}
	})

	t.Run("shared plan still works", func(t *testing.T) {
		p, err := svc.Approve(ctx, ApprovePlanRequest{PlanID: "plan-shared-svc-1", SessionID: "s1", By: "tester"})
		if err != nil {
			t.Fatalf("Approve shared plan: %v", err)
		}
		if p == nil || p.State == plan.StatePendingApproval {
			t.Fatalf("shared plan state = %+v, want transitioned out of pending_approval", p)
		}
	})

	t.Run("unknown plan still errors", func(t *testing.T) {
		if _, err := svc.Approve(ctx, ApprovePlanRequest{PlanID: "plan-does-not-exist", SessionID: "s1", By: "tester"}); err == nil {
			t.Fatal("Approve unknown plan: want error, got nil")
		}
	})
}

// Without the sink wired, a sink-plan transition must still fail cleanly
// (no fabricated success).
func TestPlanService_NoSinkWired(t *testing.T) {
	sharedMgr, sharedStore, _, _ := newSinkFallbackTestStores(t)
	svc := NewPlanService(sharedMgr, sharedStore)
	ctx := context.Background()
	if _, err := svc.Approve(ctx, ApprovePlanRequest{PlanID: "plan-sink-svc-1", SessionID: "s1", By: "tester"}); err == nil {
		t.Fatal("Approve sink plan without sink wiring: want error, got nil")
	}
}
