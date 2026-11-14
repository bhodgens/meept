package services

// M-4 (bughunt 2026-11-14 round 2) pin: TaskService.Cancel must refuse to
// flip a TERMINAL task into cancelled. The old path called raw
// registry.UpdateState(ctx, id, StateCancelled) with no terminal check,
// resurrecting completed/failed/rejected tasks as cancelled;
// Registry.CancelTask (which the service now routes through) treats a
// terminal task as a no-op success — the caller's intent (not-live) holds.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

func newTestTaskService(t *testing.T) *TaskService {
	t.Helper()
	msgBus := bus.New(nil, nil)
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("task.NewRegistry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	return NewTaskService(reg)
}

// mustUpdateState drives a task through a registry state write (raw, as any
// internal caller would; legality comes from the caller's chain).
func mustUpdateState(t *testing.T, svc *TaskService, ctx context.Context, taskID string, state task.TaskState) {
	t.Helper()
	if err := svc.registry.UpdateState(ctx, taskID, state); err != nil {
		t.Fatalf("UpdateState(%s): %v", state, err)
	}
}

// TestCancelTerminalTaskIsNoOp pins M-4: cancelling a task already in a
// terminal state is a no-op success — the state is NOT flipped to
// cancelled.
func TestCancelTerminalTaskIsNoOp(t *testing.T) {
	svc := newTestTaskService(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		terminal task.TaskState
		setup    func(t *testing.T, svc *TaskService, ctx context.Context, taskID string)
	}{
		{
			name:     "completed",
			terminal: task.StateCompleted,
			setup: func(t *testing.T, svc *TaskService, ctx context.Context, taskID string) {
				// pending -> executing -> testing -> completed (legal chain).
				mustUpdateState(t, svc, ctx, taskID, task.StateExecuting)
				mustUpdateState(t, svc, ctx, taskID, task.StateTesting)
				mustUpdateState(t, svc, ctx, taskID, task.StateCompleted)
			},
		},
		{
			name:     "failed",
			terminal: task.StateFailed,
			setup: func(t *testing.T, svc *TaskService, ctx context.Context, taskID string) {
				// pending -> planning -> failed (legal chain).
				mustUpdateState(t, svc, ctx, taskID, task.StatePlanning)
				mustUpdateState(t, svc, ctx, taskID, task.StateFailed)
			},
		},
		{
			name:     "rejected",
			terminal: task.StateRejected,
			setup: func(t *testing.T, svc *TaskService, ctx context.Context, taskID string) {
				// pending -> awaiting_approval -> rejected (legal chain).
				mustUpdateState(t, svc, ctx, taskID, task.StateAwaitingApproval)
				mustUpdateState(t, svc, ctx, taskID, task.StateRejected)
			},
		},
		{
			name:     "cancelled",
			terminal: task.StateCancelled,
			setup: func(t *testing.T, svc *TaskService, ctx context.Context, taskID string) {
				mustUpdateState(t, svc, ctx, taskID, task.StateCancelled)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created, err := svc.Create(ctx, CreateTaskRequest{Name: "terminal pin " + tc.name})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			// Drive the task to the terminal state via legal transitions.
			tc.setup(t, svc, ctx, created.ID)

			// Cancel must succeed (no-op success, parity with CancelTask).
			if err := svc.Cancel(ctx, CancelTaskRequest{ID: created.ID}); err != nil {
				t.Fatalf("Cancel on %s task: %v (want no-op success)", tc.terminal, err)
			}

			got, err := svc.Get(ctx, GetTaskRequest{ID: created.ID})
			if err != nil {
				t.Fatalf("Get after Cancel: %v", err)
			}
			if got.State != tc.terminal {
				t.Fatalf("state after Cancel = %s, want %s — the service path resurrected a terminal task (M-4)", got.State, tc.terminal)
			}
		})
	}
}

// TestCancelActiveTaskCancels pins the positive direction: cancelling an
// ACTIVE task via the service path still drives it to cancelled.
func TestCancelActiveTaskCancels(t *testing.T) {
	svc := newTestTaskService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateTaskRequest{Name: "active cancel pin"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// pending -> executing is a legal edge; an active task is cancellable.
	t0, err := svc.Get(ctx, GetTaskRequest{ID: created.ID})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := t0.SetStateChecked(task.StateExecuting); err != nil {
		t.Fatalf("SetStateChecked(executing): %v", err)
	}
	if err := svc.registry.Update(ctx, t0); err != nil {
		t.Fatalf("registry.Update: %v", err)
	}

	if err := svc.Cancel(ctx, CancelTaskRequest{ID: created.ID}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	got, err := svc.Get(ctx, GetTaskRequest{ID: created.ID})
	if err != nil {
		t.Fatalf("Get after Cancel: %v", err)
	}
	if got.State != task.StateCancelled {
		t.Fatalf("state after Cancel = %s, want cancelled", got.State)
	}
}
