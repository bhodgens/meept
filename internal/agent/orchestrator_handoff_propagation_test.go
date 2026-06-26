package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// TestPropagateHandoffToDependents_NoDeps_FallsBackToLegacy verifies that
// when templateReg or registry is nil, the function falls back to the legacy
// path (or returns nil if tactical is also nil).
func TestPropagateHandoffToDependents_NoDeps_FallsBackToLegacy(t *testing.T) {
	o, store, _ := newTestOrchestrator(t)
	// o.templateReg, o.registry, and o.tactical are all nil from newTestOrchestrator.

	completed := task.NewTaskStep("task-x", "completed step", 1)
	completed.ID = "c1"
	completed.State = task.StepCompleted
	completed.Result = "did it"
	completed.AgentID = config.AgentIDCoder
	if err := store.Create(completed); err != nil {
		t.Fatalf("create completed: %v", err)
	}

	ready := task.NewTaskStep("task-x", "ready step", 2)
	ready.ID = "r1"
	ready.State = task.StepReady
	ready.DependsOn = []string{"c1"}
	if err := store.Create(ready); err != nil {
		t.Fatalf("create ready: %v", err)
	}

	// tactical is nil on the test orchestrator → legacyPropagate is a no-op.
	// This test verifies propagateHandoffToDependents returns nil without panicking.
	err := o.propagateHandoffToDependents(context.Background(), completed)
	if err != nil {
		t.Fatalf("propagateHandoffToDependents with no deps: %v", err)
	}

	if o.tactical == nil {
		// No tactical → no propagation occurred. Verify ready step is untouched.
		got, _ := store.GetByID("r1")
		if got.AccumulatedContext != "" {
			t.Errorf("ready step context should be empty with no tactical; got %q", got.AccumulatedContext)
		}
		return
	}

	// If tactical were wired, legacy path should populate "Step completed".
	got, _ := store.GetByID("r1")
	if !strings.Contains(got.AccumulatedContext, "Step completed") {
		t.Errorf("ready step context not populated; got %q", got.AccumulatedContext)
	}
}

// TestPropagateHandoffToDependents_NoReadySteps_Noop verifies that when there
// are no ready steps, the function returns nil without calling LLM.
func TestPropagateHandoffToDependents_NoReadySteps_Noop(t *testing.T) {
	o, store, _ := newTestOrchestrator(t)
	completed := task.NewTaskStep("task-x", "solo", 1)
	completed.ID = "c1"
	completed.State = task.StepCompleted
	if err := store.Create(completed); err != nil {
		t.Fatalf("create completed: %v", err)
	}

	err := o.propagateHandoffToDependents(context.Background(), completed)
	if err != nil {
		t.Errorf("expected nil with no ready steps; got %v", err)
	}
}

// TestPropagateHandoffToDependents_NoStepStore_Noop verifies that when
// stepStore is nil, the function is a no-op.
func TestPropagateHandoffToDependents_NoStepStore_Noop(t *testing.T) {
	o, _, _ := newTestOrchestrator(t)
	o.stepStore = nil

	completed := task.NewTaskStep("task-x", "solo", 1)
	completed.ID = "c1"

	err := o.propagateHandoffToDependents(context.Background(), completed)
	if err != nil {
		t.Errorf("expected nil with nil stepStore; got %v", err)
	}
}
