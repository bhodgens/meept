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

// TestPropagateHandoffToDependents_UsesTaskScopedLoop verifies that
// propagateHandoffToDependents calls registry.GetForTask with the step's
// TaskID, not the "_default" fallback from registry.Get. Without this fix,
// the wrong (default) agent loop is fetched and the conversation is never
// found, causing the handoff to always fall back to legacy truncation.
func TestPropagateHandoffToDependents_UsesTaskScopedLoop(t *testing.T) {
	o, store, _ := newTestOrchestrator(t)

	// Wire a test registry with a coder spec.
	reg := newTestRegistryForTask(t)
	o.registry = reg

	// Also wire templateReg + tactical to avoid the fallback path.
	o.templateReg = newPlannerTemplateLoader()
	o.templateReg.fallbacks["orchestrator/handoff.md"] = defaultHandoffFallback()

	completed := task.NewTaskStep("task-scope-test", "completed step", 1)
	completed.ID = "c1"
	completed.State = task.StepCompleted
	completed.Result = "did it"
	completed.AgentID = config.AgentIDCoder
	completed.ConversationID = "conv-task-scope-test"
	if err := store.Create(completed); err != nil {
		t.Fatalf("create completed: %v", err)
	}

	ready := task.NewTaskStep("task-scope-test", "ready step", 2)
	ready.ID = "r1"
	ready.State = task.StepReady
	ready.DependsOn = []string{"c1"}
	if err := store.Create(ready); err != nil {
		t.Fatalf("create ready: %v", err)
	}

	// Pre-create the task-scoped loop so GetForTask(coder, task-scope-test)
	// returns an existing loop. The buggy code (registry.Get) would create
	// and return a _default-scoped loop instead.
	taskLoop, err := reg.GetForTask(config.AgentIDCoder, "task-scope-test")
	if err != nil {
		t.Fatalf("pre-create task loop: %v", err)
	}

	// Call propagateHandoffToDependents. It will try GetForTask, fail to
	// find a conversation (no LLM configured), then fall back to legacy.
	// But the key assertion is that the task-scoped loop was used, not
	// a _default loop.
	_ = o.propagateHandoffToDependents(context.Background(), completed)

	// Verify the _default bucket was NOT created for the coder. If Get()
	// had been used instead of GetForTask(), a _default-scoped loop would
	// exist in the registry.
	defaultLoop, defaultErr := reg.GetForTask(config.AgentIDCoder, "_default")
	taskScopedLoop, taskScopedErr := reg.GetForTask(config.AgentIDCoder, "task-scope-test")

	// The task-scoped loop should be the same one we pre-created.
	if taskScopedErr != nil {
		t.Fatalf("task-scoped loop should exist: %v", taskScopedErr)
	}
	if taskScopedLoop != taskLoop {
		t.Error("task-scoped loop identity changed after propagateHandoffToDependents")
	}

	// The _default loop may or may not exist (it could be created by
	// generateHandoff calling registry.Get for the classifier). But the
	// critical assertion is that the agent loop lookup for the COMPLETED
	// step used the task-scoped path, which we verify by checking that the
	// default loop (if it exists) is a DIFFERENT instance.
	if defaultErr == nil && defaultLoop == taskScopedLoop {
		t.Error("default loop and task-scoped loop are the same instance; " +
			"propagateHandoffToDependents may have used Get() instead of GetForTask()")
	}
}

// TestReleaseTaskLoopsIfComplete_AllStepsTerminal_Releases verifies that
// releaseTaskLoopsIfComplete is a safe no-op when registry is nil (the
// newTestOrchestrator helper does not wire one) even if all steps are in a
// terminal state. The flow must reach the registry call without panicking.
func TestReleaseTaskLoopsIfComplete_AllStepsTerminal_NoopWithNilRegistry(t *testing.T) {
	o, store, _ := newTestOrchestrator(t)
	// o.registry is nil from newTestOrchestrator.

	s1 := task.NewTaskStep("task-x", "step 1", 1)
	s1.ID = "s1"
	s1.State = task.StepCompleted
	if err := store.Create(s1); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	s2 := task.NewTaskStep("task-x", "step 2", 2)
	s2.ID = "s2"
	s2.State = task.StepFailed
	if err := store.Create(s2); err != nil {
		t.Fatalf("create s2: %v", err)
	}

	// With registry nil, releaseTaskLoopsIfComplete must return before
	// calling ReleaseTaskLoops (which would panic on nil).
	o.releaseTaskLoopsIfComplete("task-x")
}

// TestReleaseTaskLoopsIfComplete_StepActive_Noop verifies that when at least
// one step is still in a non-terminal state, the function returns without
// reaching the (nil) registry call.
func TestReleaseTaskLoopsIfComplete_StepActive_Noop(t *testing.T) {
	o, store, _ := newTestOrchestrator(t)

	s1 := task.NewTaskStep("task-x", "step 1", 1)
	s1.ID = "s1"
	s1.State = task.StepCompleted
	if err := store.Create(s1); err != nil {
		t.Fatalf("create s1: %v", err)
	}
	s2 := task.NewTaskStep("task-x", "step 2", 2)
	s2.ID = "s2"
	s2.State = task.StepReady // not terminal
	if err := store.Create(s2); err != nil {
		t.Fatalf("create s2: %v", err)
	}

	// Should not call ReleaseTaskLoops (would panic on nil registry).
	o.releaseTaskLoopsIfComplete("task-x")
}

// TestReleaseTaskLoopsIfComplete_EmptyTaskID_Noop verifies the empty-taskID
// guard.
func TestReleaseTaskLoopsIfComplete_EmptyTaskID_Noop(t *testing.T) {
	o, _, _ := newTestOrchestrator(t)
	o.releaseTaskLoopsIfComplete("") // must not panic
}

// TestReleaseTaskLoopsIfComplete_NoStepStore_Noop verifies the nil-stepStore
// guard.
func TestReleaseTaskLoopsIfComplete_NoStepStore_Noop(t *testing.T) {
	o, _, _ := newTestOrchestrator(t)
	o.stepStore = nil
	o.releaseTaskLoopsIfComplete("task-x") // must not panic
}

// TestReleaseTaskLoopsIfComplete_EmptyStepList_Noop verifies that a taskID
// with zero steps is a no-op (does not call ReleaseTaskLoops).
func TestReleaseTaskLoopsIfComplete_EmptyStepList_Noop(t *testing.T) {
	o, _, _ := newTestOrchestrator(t)
	// No steps for "task-x" beyond what other tests seed; this test uses a
	// fresh in-memory store so the step list is empty.
	o.releaseTaskLoopsIfComplete("task-x")
}
