package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// Pins for the failure-aware replan context (issue #58 capability 2):
// a replan PlanRequest for a task with a failed step must carry the
// "## Previous attempt failed" block with the step's concrete error text,
// and a task with no failed steps must carry no block.

func newReplanTestPlanner(t *testing.T) (*StrategicPlanner, *task.Store, *task.StepStore, *bus.MessageBus) {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })

	tmpDir := t.TempDir()
	taskStore, err := newTestTaskStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		// Empty registry: Plan() falls through to the single-step
		// fallback path, whose persisted step description contains the
		// replan text with the SessionContext block prepended — the
		// observable artifact we assert against.
		Registry:       NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()}),
		TaskStore:      taskStore,
		StepStore:      taskStore.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         slogDiscardLogger(),
	})
	return sp, taskStore, taskStore.StepStore(), msgBus
}

// replanFailureBlockText returns the persisted replan step description (or
// "" when no RE-PLAN step was persisted).
func replanFailureBlockText(t *testing.T, stepStore *task.StepStore, taskID string) string {
	t.Helper()
	steps, err := stepStore.ListByTaskID(taskID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	for _, s := range steps {
		if strings.Contains(s.Description, "RE-PLAN") {
			return s.Description
		}
	}
	return ""
}

// Pin: a replan for a task with a failed step carries the failure block
// with the step description, agent, and error text.
func TestReplanFailedTask_CarriesFailureBlock(t *testing.T) {
	sp, taskStore, stepStore, msgBus := newReplanTestPlanner(t)

	tsk := newTestTask("task-failblock-1", "populate the sprint backlog")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	failedStep := task.NewTaskStep(tsk.ID, "Create the sprint task via task_create", 0)
	failedStep.State = task.StepFailed
	failedStep.AgentID = "task-manager"
	failedStep.Result = "task_create failed: name is missing"
	if err := stepStore.Create(failedStep); err != nil {
		t.Fatalf("failed to create failed step: %v", err)
	}

	plannedSub := msgBus.Subscribe("test-observer", "task.planned")
	defer msgBus.Unsubscribe(plannedSub)

	if err := sp.ReplanFailedTask(context.Background(), tsk.ID, "step 'step-x' failed: task_create failed"); err != nil {
		t.Fatalf("ReplanFailedTask failed: %v", err)
	}

	desc := replanFailureBlockText(t, stepStore, tsk.ID)
	if desc == "" {
		t.Fatal("no RE-PLAN fallback step found")
	}
	if !strings.Contains(desc, "## Previous attempt failed") {
		t.Errorf("expected failure block in replan context, got: %s", desc)
	}
	if !strings.Contains(desc, "task_create failed: name is missing") {
		t.Errorf("expected step error text in failure block, got: %s", desc)
	}
	if !strings.Contains(desc, "Agent: task-manager") {
		t.Errorf("expected failing step agent in failure block, got: %s", desc)
	}
	if !strings.Contains(desc, "Create the sprint task via task_create") {
		t.Errorf("expected failing step description in failure block, got: %s", desc)
	}

	select {
	case <-plannedSub.Channel:
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task.planned event from replan")
	}
}

// Pin: a replan for a task with NO failed steps carries no failure block.
func TestReplanFailedTask_NoFailedSteps_NoFailureBlock(t *testing.T) {
	sp, taskStore, stepStore, _ := newReplanTestPlanner(t)

	tsk := newTestTask("task-failblock-2", "populate the sprint backlog")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Only a pending (not failed) step exists.
	pendingStep := task.NewTaskStep(tsk.ID, "Write the report", 0)
	pendingStep.State = task.StepPending
	if err := stepStore.Create(pendingStep); err != nil {
		t.Fatalf("failed to create pending step: %v", err)
	}

	if err := sp.ReplanFailedTask(context.Background(), tsk.ID, "escalation fired without a failed step"); err != nil {
		t.Fatalf("ReplanFailedTask failed: %v", err)
	}

	desc := replanFailureBlockText(t, stepStore, tsk.ID)
	if desc == "" {
		t.Fatal("no RE-PLAN fallback step found")
	}
	if strings.Contains(desc, "## Previous attempt failed") {
		t.Errorf("task with no failed steps must not carry a failure block, got: %s", desc)
	}
}

// Pin: the escalation manager's own replan path attaches the failure
// context from the step store.
func TestEscalationManager_ReplanCarriesFailureContext(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sp, taskStore, stepStore, _ := newReplanTestPlanner(t)

	tsk := newTestTask("task-failblock-3", "ship the release notes")
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	failedStep := task.NewTaskStep(tsk.ID, "Generate changelog via git log", 0)
	failedStep.State = task.StepFailed
	failedStep.AgentID = "coder"
	failedStep.Result = "git log failed: not a git repository"
	if err := stepStore.Create(failedStep); err != nil {
		t.Fatalf("failed to create failed step: %v", err)
	}

	em := NewEscalationManager(EscalationManagerConfig{
		Config: EscalationConfig{
			Enabled:             true,
			MaxEscalationLevels: 3,
		},
		Planner:   sp,
		TaskStore: taskStore,
		StepStore: stepStore,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	if err := em.Escalate(context.Background(), FailureContext{
		TaskID:    tsk.ID,
		StepID:    failedStep.ID,
		AgentID:   "coder",
		Error:     "git log failed: not a git repository",
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("Escalate failed: %v", err)
	}

	desc := replanFailureBlockText(t, stepStore, tsk.ID)
	if desc == "" {
		t.Fatal("no RE-PLAN fallback step found after escalation replan")
	}
	if !strings.Contains(desc, "## Previous attempt failed") {
		t.Errorf("expected failure block in escalation replan, got: %s", desc)
	}
	if !strings.Contains(desc, "git log failed: not a git repository") {
		t.Errorf("expected failed step error text in escalation replan, got: %s", desc)
	}

	em.ClearEscalation(tsk.ID)
}

// Pin: without a step store the escalation manager attaches no failure
// block (backward-compatible no-op).
func TestEscalationManager_NoStepStore_NoFailureContext(t *testing.T) {
	em := NewEscalationManager(EscalationManagerConfig{
		Config: EscalationConfig{Enabled: true, MaxEscalationLevels: 3},
	})
	if got := em.buildReplanFailureContext("task-xyz"); got != "" {
		t.Errorf("expected empty failure context without step store, got: %q", got)
	}
}
