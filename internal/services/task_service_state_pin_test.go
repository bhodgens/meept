package services

// Issue #135 pin: TaskService.Update is the RPC/HTTP task-state write surface.
// It used to call t.SetState(task.TaskState(req.State)) with an UNVALIDATED
// string, so an RPC caller could write completed->pending or an arbitrary
// string. Both are now refused: unknown enum value first, then the
// taskTransitions edge.

import (
	"context"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// TestTaskUpdate_IllegalEdgeRejected pins that an illegal edge through the
// service surface is refused and nothing is persisted.
func TestTaskUpdate_IllegalEdgeRejected(t *testing.T) {
	svc := newTestTaskService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateTaskRequest{Name: "rpc illegal edge"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// pending -> executing -> completed (legal chain).
	if err := svc.registry.UpdateState(ctx, created.ID, task.StateExecuting); err != nil {
		t.Fatalf("UpdateState(executing): %v", err)
	}
	if err := svc.registry.UpdateState(ctx, created.ID, task.StateCompleted); err != nil {
		t.Fatalf("UpdateState(completed): %v", err)
	}

	_, err = svc.Update(ctx, UpdateTaskRequest{ID: created.ID, State: string(task.StatePending)})
	if err == nil {
		t.Fatal("Update(completed->pending) = nil, want an illegal-transition error")
	}
	if !errors.Is(err, task.ErrInvalidTransition) {
		t.Errorf("error = %v, want task.ErrInvalidTransition", err)
	}
	// The wire shape is a parameter error, so transports render 400, not 500.
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput (transport parameter class)", err)
	}

	got, getErr := svc.Get(ctx, GetTaskRequest{ID: created.ID})
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.State != task.StateCompleted {
		t.Errorf("state = %q, want completed — the RPC surface must not persist an illegal edge", got.State)
	}
}

// TestTaskUpdate_UnknownStateRejected pins state-string validation: "done" is
// a typo, not a state, and must never reach the store.
func TestTaskUpdate_UnknownStateRejected(t *testing.T) {
	svc := newTestTaskService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateTaskRequest{Name: "rpc unknown state"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Update(ctx, UpdateTaskRequest{ID: created.ID, State: "done"})
	if err == nil {
		t.Fatal(`Update(state="done") = nil, want an unknown-state error`)
	}
	if !errors.Is(err, task.ErrInvalidTaskState) {
		t.Errorf("error = %v, want task.ErrInvalidTaskState", err)
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput (transport parameter class)", err)
	}

	got, getErr := svc.Get(ctx, GetTaskRequest{ID: created.ID})
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.State != task.StatePending {
		t.Errorf("state = %q, want pending — an unknown state string must never persist", got.State)
	}
}

// TestTaskUpdate_LegalEdgeStillWorks pins the positive direction: the ordinary
// RPC state change still lands, and a name-only update does not trip the guard.
func TestTaskUpdate_LegalEdgeStillWorks(t *testing.T) {
	svc := newTestTaskService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, CreateTaskRequest{Name: "rpc legal edge"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(ctx, UpdateTaskRequest{ID: created.ID, State: string(task.StateExecuting)})
	if err != nil {
		t.Fatalf("Update(pending->executing) = %v, want nil", err)
	}
	if updated.State != task.StateExecuting {
		t.Errorf("state = %q, want executing", updated.State)
	}

	renamed, err := svc.Update(ctx, UpdateTaskRequest{ID: created.ID, Name: "renamed only"})
	if err != nil {
		t.Fatalf("Update(name only) = %v, want nil", err)
	}
	if renamed.Name != "renamed only" {
		t.Errorf("name = %q, want %q", renamed.Name, "renamed only")
	}
	if renamed.State != task.StateExecuting {
		t.Errorf("state = %q, want executing — a name-only update must not touch state", renamed.State)
	}
}
