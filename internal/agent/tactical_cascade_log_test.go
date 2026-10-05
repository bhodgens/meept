package agent

// B3 pin: inside failBlockedDependents' fixpoint loop the cascade log
// reported "failed_dep", failedStepID — the ORIGINAL failed step at every
// level. In a chain A(failed) → B → C, C's log line claimed its blocker was
// A when it was B. Each step's log line must name the dependency that
// actually blocked it.

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

func TestFailBlockedDependents_CascadeLogNamesDirectBlocker(t *testing.T) {
	ts, _, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	var buf bytes.Buffer
	ts.logger = slog.New(slog.NewTextHandler(&buf, nil))

	parent := newTestTask("task-b3-cascade-log", "cascade blocker attribution")
	if err := ts.taskStore.Create(parent); err != nil {
		t.Fatalf("create task: %v", err)
	}

	stepA := task.NewTaskStep(parent.ID, "failed root", 0)
	if err := ts.stepStore.Create(stepA); err != nil {
		t.Fatalf("create step A: %v", err)
	}
	stepB := task.NewTaskStep(parent.ID, "depends on A", 1)
	stepB.DependsOn = []string{stepA.ID}
	if err := ts.stepStore.Create(stepB); err != nil {
		t.Fatalf("create step B: %v", err)
	}
	stepC := task.NewTaskStep(parent.ID, "depends on B", 2)
	stepC.DependsOn = []string{stepB.ID}
	if err := ts.stepStore.Create(stepC); err != nil {
		t.Fatalf("create step C: %v", err)
	}

	if err := ts.stepStore.SetState(stepA.ID, task.StepFailed); err != nil {
		t.Fatalf("fail step A: %v", err)
	}

	if err := ts.failBlockedDependents(parent.ID, stepA.ID); err != nil {
		t.Fatalf("failBlockedDependents: %v", err)
	}

	log := buf.String()
	if !strings.Contains(log, "step_id="+stepB.ID) {
		t.Fatalf("expected a log line for step B, got:\n%s", log)
	}
	if !strings.Contains(log, "step_id="+stepC.ID) {
		t.Fatalf("expected a log line for step C, got:\n%s", log)
	}
	wantB := "step_id=" + stepB.ID + " failed_dep=" + stepA.ID
	wantC := "step_id=" + stepC.ID + " failed_dep=" + stepB.ID
	if !strings.Contains(log, wantB) {
		t.Errorf("step B log line must attribute blocker %s, got:\n%s", stepA.ID, log)
	}
	if !strings.Contains(log, wantC) {
		t.Errorf("step C log line must attribute blocker %s (not the root %s), got:\n%s", stepB.ID, stepA.ID, log)
	}
}
