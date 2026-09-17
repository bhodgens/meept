package agent

// F10 (2026-09-17 bughunt) pin: when validation retries are exhausted the
// step is failed, but its PENDING dependent steps can never run
// (PromoteReadySteps requires successfully-terminal deps) — they stayed
// pending forever and allStepsTerminalWithFailures returned false, so the
// task never finalized. failBlockedDependents must terminalize the
// transitively-blocked pending dependents as failed.
//
// Fixture: 2-step dependency graph — step A (fails validation
// exhaustively) ← step B depends on A.

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestFailBlockedDependents_PendingDependentTerminalized(t *testing.T) {
	ts, _, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	parent := newTestTask("task-f10-dep", "validation exhaustion dependency graph")
	if err := ts.taskStore.Create(parent); err != nil {
		t.Fatalf("create task: %v", err)
	}

	stepA := task.NewTaskStep(parent.ID, "fails validation exhaustively", 0)
	if err := ts.stepStore.Create(stepA); err != nil {
		t.Fatalf("create step A: %v", err)
	}
	stepB := task.NewTaskStep(parent.ID, "depends on A", 1)
	stepB.DependsOn = []string{stepA.ID}
	if err := ts.stepStore.Create(stepB); err != nil {
		t.Fatalf("create step B: %v", err)
	}

	// Precondition: B is pending and A has just been failed (the
	// validation-exhaustion branch's state before the fix).
	if err := ts.stepStore.SetState(stepA.ID, task.StepFailed); err != nil {
		t.Fatalf("fail step A: %v", err)
	}

	// Precondition pin: before the cascade, the task is NOT
	// failure-finalizable — B's pending state blocks it. This is the hang.
	terminal, err := ts.allStepsTerminalWithFailures(parent.ID)
	if err != nil {
		t.Fatalf("allStepsTerminalWithFailures (pre): %v", err)
	}
	if terminal {
		t.Fatal("pre-condition: task reported terminal while dependent is pending")
	}

	if err := ts.failBlockedDependents(parent.ID, stepA.ID); err != nil {
		t.Fatalf("failBlockedDependents: %v", err)
	}

	gotB, err := ts.stepStore.GetByID(stepB.ID)
	if err != nil || gotB == nil {
		t.Fatalf("get step B: %v (step nil: %v)", err, gotB == nil)
	}
	if gotB.State != task.StepFailed {
		t.Errorf("dependent step B state = %q, want %q", gotB.State, task.StepFailed)
	}

	// Post-condition: the task is now failure-finalizable.
	terminal, err = ts.allStepsTerminalWithFailures(parent.ID)
	if err != nil {
		t.Fatalf("allStepsTerminalWithFailures (post): %v", err)
	}
	if !terminal {
		t.Fatal("post-condition: task still not failure-finalizable after dependents terminalized")
	}
}

// An UNRELATED pending step (no dependency on the failed step) must stay
// pending — the cascade only claims steps actually blocked by the failure.
func TestFailBlockedDependents_LeavesUnrelatedPendingAlone(t *testing.T) {
	ts, _, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	parent := newTestTask("task-f10-unrel", "cascade scope check")
	if err := ts.taskStore.Create(parent); err != nil {
		t.Fatalf("create task: %v", err)
	}

	failed := task.NewTaskStep(parent.ID, "failed step", 0)
	if err := ts.stepStore.Create(failed); err != nil {
		t.Fatalf("create failed step: %v", err)
	}
	unrelated := task.NewTaskStep(parent.ID, "independent pending step", 1)
	if err := ts.stepStore.Create(unrelated); err != nil {
		t.Fatalf("create unrelated step: %v", err)
	}

	if err := ts.stepStore.SetState(failed.ID, task.StepFailed); err != nil {
		t.Fatalf("fail step: %v", err)
	}
	if err := ts.failBlockedDependents(parent.ID, failed.ID); err != nil {
		t.Fatalf("failBlockedDependents: %v", err)
	}

	got, err := ts.stepStore.GetByID(unrelated.ID)
	if err != nil || got == nil {
		t.Fatalf("get unrelated step: %v", err)
	}
	if got.State != task.StepPending {
		t.Errorf("unrelated pending step state = %q, want pending (cascade over-reached)", got.State)
	}
}
