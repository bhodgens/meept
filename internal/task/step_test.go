package task

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newTestStepStore(t *testing.T) *StepStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store, err := NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("failed to create step store: %v", err)
	}
	return store
}

func TestStepStore_CreateAndGetByID(t *testing.T) {
	store := newTestStepStore(t)

	step := NewTaskStep("task-1", "do something", 1)
	step.ToolHint = "code"

	if err := store.Create(step); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected step, got nil")
	}
	if got.Description != "do something" {
		t.Errorf("expected description %q, got %q", "do something", got.Description)
	}
	if got.ToolHint != "code" {
		t.Errorf("expected tool_hint %q, got %q", "code", got.ToolHint)
	}
	if got.State != StepPending {
		t.Errorf("expected state %q, got %q", StepPending, got.State)
	}
}

func TestStepStore_ListByTaskID(t *testing.T) {
	store := newTestStepStore(t)

	// Create steps for two tasks
	for i := 0; i < 3; i++ {
		step := NewTaskStep("task-A", "step A", i)
		if err := store.Create(step); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}
	stepB := NewTaskStep("task-B", "step B", 0)
	if err := store.Create(stepB); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	steps, err := store.ListByTaskID("task-A")
	if err != nil {
		t.Fatalf("ListByTaskID failed: %v", err)
	}
	if len(steps) != 3 {
		t.Errorf("expected 3 steps for task-A, got %d", len(steps))
	}

	// Verify ordering by sequence
	for i, s := range steps {
		if s.Sequence != i {
			t.Errorf("step %d: expected sequence %d, got %d", i, i, s.Sequence)
		}
	}
}

func TestStepStore_SetState(t *testing.T) {
	store := newTestStepStore(t)

	step := NewTaskStep("task-1", "do something", 0)
	if err := store.Create(step); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := store.SetState(step.ID, StepRunning); err != nil {
		t.Fatalf("SetState failed: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.State != StepRunning {
		t.Errorf("expected state %q, got %q", StepRunning, got.State)
	}
}

func TestStepStore_SetJobID(t *testing.T) {
	store := newTestStepStore(t)

	step := NewTaskStep("task-1", "do something", 0)
	if err := store.Create(step); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := store.SetJobID(step.ID, "job-123"); err != nil {
		t.Fatalf("SetJobID failed: %v", err)
	}

	got, err := store.GetByJobID("job-123")
	if err != nil {
		t.Fatalf("GetByJobID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected step, got nil")
	}
	if got.ID != step.ID {
		t.Errorf("expected step ID %q, got %q", step.ID, got.ID)
	}
}

func TestStepStore_SetResult(t *testing.T) {
	store := newTestStepStore(t)

	step := NewTaskStep("task-1", "do something", 0)
	if err := store.Create(step); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := store.SetResult(step.ID, "all done"); err != nil {
		t.Fatalf("SetResult failed: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.Result != "all done" {
		t.Errorf("expected result %q, got %q", "all done", got.Result)
	}
}

func TestStepStore_DependsOn(t *testing.T) {
	store := newTestStepStore(t)

	step := NewTaskStep("task-1", "dependent step", 1)
	step.DependsOn = []string{"step-1", "step-2"}
	if err := store.Create(step); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if len(got.DependsOn) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(got.DependsOn))
	}
	if got.DependsOn[0] != "step-1" || got.DependsOn[1] != "step-2" {
		t.Errorf("unexpected dependencies: %v", got.DependsOn)
	}
}

func TestStepStore_PromoteReadySteps(t *testing.T) {
	store := newTestStepStore(t)

	// Create: step-0 (no deps), step-1 (depends on step-0), step-2 (depends on step-0)
	s0 := NewTaskStep("task-1", "root step", 0)
	s0.ID = "step-0"
	s1 := NewTaskStep("task-1", "child 1", 1)
	s1.ID = "step-1"
	s1.DependsOn = []string{"step-0"}
	s2 := NewTaskStep("task-1", "child 2", 2)
	s2.ID = "step-2"
	s2.DependsOn = []string{"step-0"}

	for _, s := range []*TaskStep{s0, s1, s2} {
		if err := store.Create(s); err != nil {
			t.Fatalf("Create %s failed: %v", s.ID, err)
		}
	}

	// Initially: all pending, only s0 has no deps
	// Promote should make s0 ready (empty deps)
	promoted, err := store.PromoteReadySteps("task-1")
	if err != nil {
		t.Fatalf("PromoteReadySteps failed: %v", err)
	}
	if len(promoted) != 1 || promoted[0].ID != "step-0" {
		t.Errorf("expected step-0 promoted, got %v", promoted)
	}

	// Mark step-0 completed
	if err := store.SetState("step-0", StepCompleted); err != nil {
		t.Fatalf("SetState failed: %v", err)
	}

	// Now step-1 and step-2 should be promotable
	promoted, err = store.PromoteReadySteps("task-1")
	if err != nil {
		t.Fatalf("PromoteReadySteps failed: %v", err)
	}
	if len(promoted) != 2 {
		t.Errorf("expected 2 promoted steps, got %d", len(promoted))
	}
}

func TestStepStore_GetReadySteps(t *testing.T) {
	store := newTestStepStore(t)

	s0 := NewTaskStep("task-1", "ready step", 0)
	s0.State = StepReady
	if err := store.Create(s0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// Set state in DB
	if err := store.SetState(s0.ID, StepReady); err != nil {
		t.Fatalf("SetState failed: %v", err)
	}

	s1 := NewTaskStep("task-1", "pending step", 1)
	if err := store.Create(s1); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	ready, err := store.GetReadySteps("task-1")
	if err != nil {
		t.Fatalf("GetReadySteps failed: %v", err)
	}
	if len(ready) != 1 {
		t.Errorf("expected 1 ready step, got %d", len(ready))
	}
}

func TestStepStore_AreAllCompleted(t *testing.T) {
	store := newTestStepStore(t)

	s0 := NewTaskStep("task-1", "step 1", 0)
	s1 := NewTaskStep("task-1", "step 2", 1)
	for _, s := range []*TaskStep{s0, s1} {
		if err := store.Create(s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// Not all completed
	done, err := store.AreAllCompleted("task-1")
	if err != nil {
		t.Fatalf("AreAllCompleted failed: %v", err)
	}
	if done {
		t.Error("expected not all completed")
	}

	// Complete both
	store.SetState(s0.ID, StepCompleted)
	store.SetState(s1.ID, StepCompleted)

	done, err = store.AreAllCompleted("task-1")
	if err != nil {
		t.Fatalf("AreAllCompleted failed: %v", err)
	}
	if !done {
		t.Error("expected all completed")
	}
}

func TestStepStore_AreAllCompleted_WithFailure(t *testing.T) {
	store := newTestStepStore(t)

	s0 := NewTaskStep("task-1", "step 1", 0)
	s1 := NewTaskStep("task-1", "step 2", 1)
	for _, s := range []*TaskStep{s0, s1} {
		if err := store.Create(s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	store.SetState(s0.ID, StepCompleted)
	store.SetState(s1.ID, StepFailed)

	// Failed steps mean "not all completed"
	done, err := store.AreAllCompleted("task-1")
	if err != nil {
		t.Fatalf("AreAllCompleted failed: %v", err)
	}
	if done {
		t.Error("expected not all completed when one failed")
	}
}

func TestStepStore_HasFailures(t *testing.T) {
	store := newTestStepStore(t)

	s0 := NewTaskStep("task-1", "step 1", 0)
	if err := store.Create(s0); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	has, err := store.HasFailures("task-1")
	if err != nil {
		t.Fatalf("HasFailures failed: %v", err)
	}
	if has {
		t.Error("expected no failures")
	}

	store.SetState(s0.ID, StepFailed)

	has, err = store.HasFailures("task-1")
	if err != nil {
		t.Fatalf("HasFailures failed: %v", err)
	}
	if !has {
		t.Error("expected failures")
	}
}

func TestStepStore_CountByState(t *testing.T) {
	store := newTestStepStore(t)

	s0 := NewTaskStep("task-1", "step 0", 0)
	s1 := NewTaskStep("task-1", "step 1", 1)
	s2 := NewTaskStep("task-1", "step 2", 2)

	for _, s := range []*TaskStep{s0, s1, s2} {
		if err := store.Create(s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	store.SetState(s0.ID, StepCompleted)
	store.SetState(s1.ID, StepRunning)
	// s2 stays pending

	counts, err := store.CountByState("task-1")
	if err != nil {
		t.Fatalf("CountByState failed: %v", err)
	}
	if counts[StepCompleted] != 1 {
		t.Errorf("expected 1 completed, got %d", counts[StepCompleted])
	}
	if counts[StepRunning] != 1 {
		t.Errorf("expected 1 running, got %d", counts[StepRunning])
	}
	if counts[StepPending] != 1 {
		t.Errorf("expected 1 pending, got %d", counts[StepPending])
	}
}

func TestStepStore_DeleteByTaskID(t *testing.T) {
	store := newTestStepStore(t)

	for i := 0; i < 3; i++ {
		s := NewTaskStep("task-1", "step", i)
		if err := store.Create(s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	if err := store.DeleteByTaskID("task-1"); err != nil {
		t.Fatalf("DeleteByTaskID failed: %v", err)
	}

	steps, err := store.ListByTaskID("task-1")
	if err != nil {
		t.Fatalf("ListByTaskID failed: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("expected 0 steps after delete, got %d", len(steps))
	}
}

func TestStepState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    StepState
		terminal bool
	}{
		{StepPending, false},
		{StepReady, false},
		{StepScheduled, false},
		{StepRunning, false},
		{StepReviewing, false},
		{StepCompleted, true},
		{StepApproved, true},
		{StepRejected, true}, // Rejected is terminal - revision step handles redo
		{StepFailed, true},
		{StepSkipped, true},
	}

	for _, tt := range tests {
		got := tt.state.IsTerminal()
		if got != tt.terminal {
			t.Errorf("state %q: expected IsTerminal=%v, got %v", tt.state, tt.terminal, got)
		}
	}
}

func TestStepState_IsSuccessfullyTerminal(t *testing.T) {
	tests := []struct {
		state   StepState
		success bool
	}{
		{StepPending, false},
		{StepReady, false},
		{StepScheduled, false},
		{StepRunning, false},
		{StepReviewing, false},
		{StepCompleted, true},
		{StepApproved, true},
		{StepRejected, false}, // Rejected is NOT successfully terminal
		{StepFailed, false},
		{StepSkipped, false},
	}

	for _, tt := range tests {
		got := tt.state.IsSuccessfullyTerminal()
		if got != tt.success {
			t.Errorf("state %q: expected IsSuccessfullyTerminal=%v, got %v", tt.state, tt.success, got)
		}
	}
}

func TestStepStore_AreAllCompleted_WithRejectedAndRevision(t *testing.T) {
	store := newTestStepStore(t)

	// Create original step and set it to rejected
	original := NewTaskStep("task-1", "original work", 0)
	original.ID = "step-original"
	if err := store.Create(original); err != nil {
		t.Fatalf("Create original failed: %v", err)
	}
	if err := store.SetState("step-original", StepRejected); err != nil {
		t.Fatalf("SetState rejected failed: %v", err)
	}

	// Task should NOT be complete - rejected step without revision
	done, err := store.AreAllCompleted("task-1")
	if err != nil {
		t.Fatalf("AreAllCompleted failed: %v", err)
	}
	if done {
		t.Error("expected task NOT complete with rejected step without revision")
	}

	// Create revision step that depends on original
	revision := NewTaskStep("task-1", "revision work", 1)
	revision.ID = "step-revision"
	revision.DependsOn = []string{"step-original"}
	if err := store.Create(revision); err != nil {
		t.Fatalf("Create revision failed: %v", err)
	}

	// Set revision to completed
	if err := store.SetState("step-revision", StepCompleted); err != nil {
		t.Fatalf("SetState completed failed: %v", err)
	}

	// Now task SHOULD be complete - rejected step has successful revision
	done, err = store.AreAllCompleted("task-1")
	if err != nil {
		t.Fatalf("AreAllCompleted failed: %v", err)
	}
	if !done {
		t.Error("expected task complete when rejected step has successful revision")
	}
}

func TestStepStore_AreAllCompleted_MultipleRejections(t *testing.T) {
	store := newTestStepStore(t)

	// Create two steps, both rejected
	step1 := NewTaskStep("task-1", "step 1", 0)
	step1.ID = "step-1"
	step2 := NewTaskStep("task-1", "step 2", 1)
	step2.ID = "step-2"

	for _, s := range []*TaskStep{step1, step2} {
		if err := store.Create(s); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if err := store.SetState(s.ID, StepRejected); err != nil {
			t.Fatalf("SetState rejected failed: %v", err)
		}
	}

	// Create revisions
	rev1 := NewTaskStep("task-1", "rev 1", 2)
	rev1.ID = "rev-1"
	rev1.DependsOn = []string{"step-1"}

	rev2 := NewTaskStep("task-1", "rev 2", 3)
	rev2.ID = "rev-2"
	rev2.DependsOn = []string{"step-2"}

	for _, s := range []*TaskStep{rev1, rev2} {
		if err := store.Create(s); err != nil {
			t.Fatalf("Create revision failed: %v", err)
		}
	}

	// Only one revision approved - should not be complete
	if err := store.SetState("rev-1", StepApproved); err != nil {
		t.Fatalf("SetState approved failed: %v", err)
	}

	done, err := store.AreAllCompleted("task-1")
	if err != nil {
		t.Fatalf("AreAllCompleted failed: %v", err)
	}
	if done {
		t.Error("expected task NOT complete - rev-2 is still pending")
	}

	// Both revisions approved - should be complete
	if err := store.SetState("rev-2", StepApproved); err != nil {
		t.Fatalf("SetState approved failed: %v", err)
	}

	done, err = store.AreAllCompleted("task-1")
	if err != nil {
		t.Fatalf("AreAllCompleted failed: %v", err)
	}
	if !done {
		t.Error("expected task complete - all rejected steps have successful revisions")
	}
}

func TestNewTaskStep(t *testing.T) {
	step := NewTaskStep("task-42", "write tests", 3)

	if step.TaskID != "task-42" {
		t.Errorf("expected TaskID %q, got %q", "task-42", step.TaskID)
	}
	if step.Description != "write tests" {
		t.Errorf("expected Description %q, got %q", "write tests", step.Description)
	}
	if step.Sequence != 3 {
		t.Errorf("expected Sequence 3, got %d", step.Sequence)
	}
	if step.State != StepPending {
		t.Errorf("expected State %q, got %q", StepPending, step.State)
	}
	if step.ID == "" {
		t.Error("expected ID to be set")
	}
}

func TestTaskStep_TokenUsage(t *testing.T) {
	step := NewTaskStep("task-1", "test step", 0)

	if step.TokenUsage != 0 {
		t.Errorf("expected 0 initial tokens, got %d", step.TokenUsage)
	}

	step.AddTokenUsage(800)
	if step.TokenUsage != 800 {
		t.Errorf("expected 800 tokens, got %d", step.TokenUsage)
	}

	step.AddTokenUsage(200)
	if step.TokenUsage != 1000 {
		t.Errorf("expected 1000 tokens, got %d", step.TokenUsage)
	}
}

func TestStepStore_TokenUsage(t *testing.T) {
	store := newTestStepStore(t)

	step := NewTaskStep("task-1", "test step", 0)
	step.TokenUsage = 500

	if err := store.Create(step); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.TokenUsage != 500 {
		t.Errorf("expected token usage 500, got %d", got.TokenUsage)
	}

	// Test Update
	got.AddTokenUsage(300)
	if err := store.Update(got); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got2, err := store.GetByID(step.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got2.TokenUsage != 800 {
		t.Errorf("expected token usage 800 after update, got %d", got2.TokenUsage)
	}
}
