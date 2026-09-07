package task

// Regression test for the 2026-09-07 counter-drift finding: a task finalized
// as "2/1 completed, 200%" because revision-creation and failure counting
// used read-modify-write full-row updates that raced concurrent completions.
// RecountJobs is the authoritative repair: derive counters from step rows.

import (
	"testing"
	"time"
)

func TestStore_RecountJobs(t *testing.T) {
	store := newTestStore(t)

	now := time.Now().UTC()
	tk := &Task{
		ID:          "task-recount",
		Name:        "recount test",
		Description: "counter repair",
		State:       StateExecuting,
		TotalJobs:   99, // deliberately wrong
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Steps: 2 approved, 1 completed, 1 failed, 1 rejected, 1 running.
	states := []struct {
		id    string
		state StepState
	}{
		{"step-a1", StepApproved},
		{"step-a2", StepApproved},
		{"step-c1", StepCompleted},
		{"step-f1", StepFailed},
		{"step-r1", StepRejected},
		{"step-run", StepRunning},
	}
	steps := store.StepStore()
	for _, s := range states {
		step := NewTaskStep("task-recount", "work "+s.id, 0)
		step.ID = s.id
		if err := steps.Create(step); err != nil {
			t.Fatalf("create step %s: %v", s.id, err)
		}
		if err := steps.SetState(s.id, s.state); err != nil {
			t.Fatalf("set state %s: %v", s.id, err)
		}
	}

	total, completed, failed, err := store.RecountJobs("task-recount")
	if err != nil {
		t.Fatalf("RecountJobs: %v", err)
	}
	if total != 6 {
		t.Errorf("total = %d, want 6", total)
	}
	if completed != 3 {
		t.Errorf("completed = %d, want 3 (approved+completed)", completed)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}

	got, err := store.GetByID("task-recount")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.TotalJobs != 6 || got.CompletedJobs != 3 || got.FailedJobs != 1 {
		t.Errorf("persisted counters = %d/%d/%d, want 6/3/1",
			got.TotalJobs, got.CompletedJobs, got.FailedJobs)
	}
}

func TestStore_IncrementTotalJobs_And_FailedJobs(t *testing.T) {
	store := newTestStore(t)

	now := time.Now().UTC()
	tk := &Task{
		ID:          "task-incr",
		Name:        "increment test",
		Description: "atomic increments",
		State:       StateExecuting,
		TotalJobs:   2,
		FailedJobs:  0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if err := store.IncrementTotalJobs("task-incr"); err != nil {
		t.Fatalf("IncrementTotalJobs: %v", err)
	}
	if err := store.IncrementTotalJobs("task-incr"); err != nil {
		t.Fatalf("IncrementTotalJobs: %v", err)
	}
	if err := store.IncrementFailedJobs("task-incr"); err != nil {
		t.Fatalf("IncrementFailedJobs: %v", err)
	}

	got, err := store.GetByID("task-incr")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.TotalJobs != 4 {
		t.Errorf("total = %d, want 4", got.TotalJobs)
	}
	if got.FailedJobs != 1 {
		t.Errorf("failed = %d, want 1", got.FailedJobs)
	}
}
