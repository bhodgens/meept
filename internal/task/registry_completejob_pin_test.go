package task

// M-3 (bughunt 2026-11-14 round 2) pin: Registry.CompleteJob's auto-complete
// must only fire through a LEGAL taskTransitions edge. Previously it called
// raw SetState(StateCompleted) from ANY state, so a pending/planning/
// awaiting_approval task with all its jobs externally marked done was
// "completed" without ever executing.

import (
	"context"
	"testing"
)

// TestCompleteJobPendingDoesNotComplete pins M-3: CompleteJob on a task
// still in pending does NOT complete it, even when the job counters are
// full.
func TestCompleteJobPendingDoesNotComplete(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	created, err := reg.Create(ctx, "m3 pin", "never executed")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// One job, completed via CompleteJob -> counters full, state pending.
	if err := reg.IncrementJobCount(ctx, created.ID); err != nil {
		t.Fatalf("IncrementJobCount: %v", err)
	}
	if err := reg.CompleteJob(ctx, created.ID); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}

	got, err := reg.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State == StateCompleted {
		t.Fatal("pending task auto-completed via CompleteJob — the transition guard must refuse the illegal pending->completed edge (M-3)")
	}
	if got.State != StatePending {
		t.Fatalf("state = %s, want pending (illegal edge must leave the state untouched)", got.State)
	}
	if got.CompletedJobs != 1 || got.TotalJobs != 1 {
		t.Fatalf("job counters = %d/%d, want 1/1 — bookkeeping must still land", got.CompletedJobs, got.TotalJobs)
	}
}

// TestCompleteJobLegalAutoCompleteFromExecuting pins the positive direction:
// from executing (a legal source for completed), the auto-complete still
// fires.
func TestCompleteJobLegalAutoCompleteFromExecuting(t *testing.T) {
	reg := newTestRegistry(t)
	ctx := context.Background()

	created, err := reg.Create(ctx, "m3 legal pin", "executing task")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// pending -> executing is legal.
	if err := reg.UpdateState(ctx, created.ID, StateExecuting); err != nil {
		t.Fatalf("UpdateState(executing): %v", err)
	}
	if err := reg.IncrementJobCount(ctx, created.ID); err != nil {
		t.Fatalf("IncrementJobCount: %v", err)
	}
	if err := reg.CompleteJob(ctx, created.ID); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}

	got, err := reg.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != StateCompleted {
		t.Fatalf("state = %s, want completed — legal executing->completed auto-complete must fire", got.State)
	}
}

// TestValidTaskState pins the enum validator used by the tool boundary.
func TestValidTaskState(t *testing.T) {
	for _, s := range []TaskState{StatePending, StatePlanning, StateAwaitingApproval, StateExecuting, StateTesting, StateCompleted, StateFailed, StateRejected, StateCancelled} {
		if !ValidTaskState(s) {
			t.Errorf("ValidTaskState(%s) = false, want true", s)
		}
	}
	for _, s := range []TaskState{"", "bogus", "PENDING", "done"} {
		if ValidTaskState(s) {
			t.Errorf("ValidTaskState(%q) = true, want false", s)
		}
	}
}
