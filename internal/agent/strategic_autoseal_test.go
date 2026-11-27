package agent

// Unit tests for the approval gate after the M6/D3 auto-seal removal
// (2026-11-27). The lexical seal verdict (PlanAutoSealFromIntent regexes),
// the PlanRequest.AutoSeal field, its requiresApproval bypass, and the
// autoSealPlanAudit trail are DELETED: approval posture is intent-driven
// (TrueIntentAnalysis.ApprovalRequested parks the plan; the plan-contract
// lanes in tree 20261125-plan-contract-quickplan execute; probe-sentence
// behavior table lives in approval_adjudication_test.go).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// TestRequiresApproval_ThresholdGate pins the surviving gate shape after
// the AutoSeal bypass was deleted: past the complexity threshold a plan
// parks; below it, it does not. (Formerly
// TestRequiresApproval_AutoSealBypassesGate — the bypass arm died with
// M6/D3; the baselines it also pinned stay load-bearing.)
func TestRequiresApproval_ThresholdGate(t *testing.T) {
	sp := &StrategicPlanner{approvalStepThreshold: 5, logger: slogDiscardLogger()}
	steps := make([]*task.TaskStep, 6)
	for i := range steps {
		steps[i] = task.NewTaskStep("t1", "step", i)
	}

	req := PlanRequest{}
	if !sp.requiresApproval(req, steps) {
		t.Error("requiresApproval past threshold = false, want true")
	}
	if sp.requiresApproval(req, steps[:2]) {
		t.Error("requiresApproval below threshold = true, want false")
	}
}

// newAutoSealTestPlanner builds a StrategicPlanner wired to a real task/step
// store + bus, mirroring the TestAwaitUserApproval harness.
func newAutoSealTestPlanner(t *testing.T) (*StrategicPlanner, *task.Store, *bus.MessageBus) {
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
		TaskStore:             taskStore,
		StepStore:             taskStore.StepStore(),
		Bus:                   msgBus,
		MaxPlanSteps:          5,
		PlannerTimeout:        10 * time.Second,
		ApprovalStepThreshold: 3,
		Logger:                slogDiscardLogger(),
	})
	return sp, taskStore, msgBus
}

func newAutoSealTask(t *testing.T, taskStore *task.Store, id, desc string, state task.TaskState) *task.Task {
	t.Helper()
	tsk := newTestTask(id, desc)
	tsk.SetState(state)
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	return tsk
}

func autoSealSteps(taskID string) []*task.TaskStep {
	steps := []*task.TaskStep{
		task.NewTaskStep(taskID, "Design the plan", 0),
		task.NewTaskStep(taskID, "Implement part one", 1),
		task.NewTaskStep(taskID, "Implement part two", 2),
		task.NewTaskStep(taskID, "Verify results", 3),
	}
	steps[3].DependsOn = []string{steps[1].ID, steps[2].ID}
	return steps
}

// TestPlainPlanParksAndNoSealTrail proves the baseline: a plain plan past
// the complexity threshold parks at the approval gate with pending_steps +
// task.pending_approval, and the deleted auto-seal machinery leaves NO
// auto_sealed metadata behind anywhere.
func TestPlainPlanParksAndNoSealTrail(t *testing.T) {
	sp, taskStore, msgBus := newAutoSealTestPlanner(t)

	pendingSub := msgBus.Subscribe("test-observer", "task.pending_approval")
	defer msgBus.Unsubscribe(pendingSub)

	tsk := newAutoSealTask(t, taskStore, "task-plain-plan", "create a plan for the migration", task.StatePlanning)
	steps := autoSealSteps(tsk.ID)

	req := PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "session-plain",
		Input:     "create a plan for the migration",
		Intent:    "plan",
		PlanningCtx: &plan.PlanningContext{
			InterviewCompleted: true,
			UserApproved:       false,
		},
	}

	if !sp.requiresApproval(req, steps) {
		t.Fatalf("requiresApproval = false for plain plan past threshold, want true")
	}
	if err := sp.awaitUserApproval(context.Background(), tsk, steps, req); err != nil {
		t.Fatalf("awaitUserApproval failed: %v", err)
	}

	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateAwaitingApproval {
		t.Errorf("task state = %q, want %q (plain plans still park)", updated.State, task.StateAwaitingApproval)
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(updated.Metadata, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}
	if _, ok := meta["pending_steps"]; !ok {
		t.Error("expected 'pending_steps' metadata on parked plan")
	}
	if _, ok := meta["auto_sealed"]; ok {
		t.Error("'auto_sealed' metadata present after M6/D3 deletion, want gone")
	}

	select {
	case msg := <-pendingSub.Channel:
		if msg.Topic != "task.pending_approval" {
			t.Errorf("expected task.pending_approval, got %q", msg.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.pending_approval event")
	}
}
