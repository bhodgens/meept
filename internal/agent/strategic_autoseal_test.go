package agent

// Unit tests for intent-driven plan auto-seal (20261119-usability-remediation
// residual 07). A compound plan+implement prompt ("create the plan and then
// implement it") must seal the plan after compilation -- skip the approval
// park and proceed straight to scheduling with an audit trail. Plain plan
// prompts still park at the approval gate.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

func TestPlanAutoSealFromIntent(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		// Compound plan+implement: auto-seal.
		{"explicit command", "create the plan and then implement it", true},
		{"plan and implement it", "plan the migration and implement it", true},
		{"comma connector", "design the auth flow, then implement it", true},
		{"implement the plan", "write up a plan and implement the plan right away", true},
		{"map out and execute", "map out the approach and execute it", true},
		{"break down and build", "break down the work into a plan and build it", true},
		{"outline plus code", "outline the refactor plan plus code it", true},
		{"plan then execute", "plan the rollout and then execute the plan", true},

		// Plain plans: still park.
		{"plain plan", "create a plan for the migration", false},
		{"design only", "design the auth flow", false},
		{"break down only", "break down the work into steps", false},
		{"plan and design", "plan and design the approach", false},
		{"draft roadmap", "draft the roadmap for the refactor", false},
		{"how should i", "how should I structure the plan for this", false},
		{"negated execute", "plan the migration but don't implement it yet", false},
		{"do not execute", "design a plan, do not execute anything", false},
		// Bughunt H2: negated + explicit "the plan" object. These all
		// trip autoSealExplicitMarker ("implement|execute|build the plan")
		// AND autoSealNegatedExecute, so the veto must run FIRST or the
		// plan gets sealed despite the user withholding execution.
		{"negated implement the plan", "write a plan but don't implement the plan", false},
		{"negated execute the plan", "create a plan, do not execute the plan", false},
		{"never implement the plan", "draft the plan and never implement the plan", false},
		{"negated build the plan", "make a plan and do not build the plan", false},
		{"empty", "", false},
		{"plan verb inside word", "planned communities and citywide codes", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlanAutoSealFromIntent(tc.input); got != tc.want {
				t.Errorf("PlanAutoSealFromIntent(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestRequiresApproval_AutoSealBypassesGate(t *testing.T) {
	sp := &StrategicPlanner{approvalStepThreshold: 5, logger: slogDiscardLogger()}
	steps := make([]*task.TaskStep, 6)
	for i := range steps {
		steps[i] = task.NewTaskStep("t1", "step", i)
	}

	// Compound plan+implement intent: gate bypassed even past threshold.
	autoSealReq := PlanRequest{AutoSeal: true, IsCompound: true}
	if sp.requiresApproval(autoSealReq, steps) {
		t.Error("requiresApproval with AutoSeal = true, want false (gate bypass)")
	}

	// Plain plan (no AutoSeal): gate still applies past threshold.
	plainReq := PlanRequest{}
	if !sp.requiresApproval(plainReq, steps) {
		t.Error("requiresApproval without AutoSeal past threshold = false, want true")
	}

	// Plain plan below threshold: no park either (unchanged baseline).
	if sp.requiresApproval(plainReq, steps[:2]) {
		t.Error("requiresApproval without AutoSeal below threshold = true, want false")
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

// TestAutoSealSkipsApprovalPark proves the compound plan+implement path:
// AutoSeal bypasses the gate, the task goes straight to executing, the
// steps are persisted + promoted, and the audit trail (auto_sealed metadata
// + auto_sealed=true on task.planned) is published -- with NO
// task.pending_approval event.
func TestAutoSealSkipsApprovalPark(t *testing.T) {
	sp, taskStore, msgBus := newAutoSealTestPlanner(t)

	plannedSub := msgBus.Subscribe("test-observer", "task.planned")
	defer msgBus.Unsubscribe(plannedSub)
	schedSub := msgBus.Subscribe("test-observer", "orchestrator.schedule")
	defer msgBus.Unsubscribe(schedSub)
	pendingSub := msgBus.Subscribe("test-observer", "task.pending_approval")
	defer msgBus.Unsubscribe(pendingSub)

	tsk := newAutoSealTask(t, taskStore, "task-autoseal-skip", "create the plan and then implement it", task.StatePlanning)
	steps := autoSealSteps(tsk.ID)

	req := PlanRequest{
		TaskID:     tsk.ID,
		SessionID:  "session-autoseal",
		Input:      "create the plan and then implement it",
		Intent:     "compound",
		IsCompound: true,
		AutoSeal:   true,
	}

	// The shared gate: requiresApproval routes to the park; AutoSeal
	// routes to the normal scheduling tail. Replicate the Plan() gate
	// branch inline (the planner LLM is not wired in this harness).
	if sp.requiresApproval(req, steps) {
		t.Fatalf("requiresApproval = true for AutoSeal request, gate must be bypassed")
	}
	for _, step := range steps {
		if err := sp.stepStore.Create(step); err != nil {
			t.Fatalf("failed to persist step: %v", err)
		}
	}
	sp.autoSealPlanAudit(tsk, req, len(steps))
	if err := sp.taskStore.SetPlanCounters(req.TaskID, len(steps), 0, 0); err != nil {
		t.Fatalf("failed to set plan counters: %v", err)
	}
	tsk.SetState(task.StateExecuting)
	if err := sp.taskStore.UpdateWithoutCounters(tsk); err != nil {
		t.Fatalf("failed to update task: %v", err)
	}
	promoted, err := sp.stepStore.PromoteReadySteps(req.TaskID)
	if err != nil {
		t.Fatalf("failed to promote steps: %v", err)
	}
	sp.publishEvent("task.planned", map[string]any{
		KeyTaskID:     req.TaskID,
		"session_id":  req.SessionID,
		"total_steps": len(steps),
		"ready_steps": len(promoted),
		"auto_sealed": true,
	})
	sp.publishEvent("orchestrator.schedule", map[string]any{
		KeyTaskID: req.TaskID,
	})

	updated, err := taskStore.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.State != task.StateExecuting {
		t.Errorf("task state = %q, want %q (auto-sealed straight to executing)", updated.State, task.StateExecuting)
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(updated.Metadata, &meta); err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}
	if _, ok := meta["pending_steps"]; ok {
		t.Error("'pending_steps' present: auto-sealed task must not park")
	}
	if sealed, ok := meta["auto_sealed"]; !ok || string(sealed) != "true" {
		t.Errorf("auto_sealed metadata = %s (present=%v), want true", string(sealed), ok)
	}

	// Audit trail on the bus: task.planned carries auto_sealed=true.
	select {
	case msg := <-plannedSub.Channel:
		var event map[string]any
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			t.Fatalf("failed to unmarshal task.planned payload: %v", err)
		}
		if sealed, ok := event["auto_sealed"].(bool); !ok || !sealed {
			t.Errorf("task.planned auto_sealed = %v (present=%v), want true", event["auto_sealed"], ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.planned event")
	}

	select {
	case msg := <-schedSub.Channel:
		if msg.Topic != "orchestrator.schedule" {
			t.Errorf("expected orchestrator.schedule, got %q", msg.Topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for orchestrator.schedule event")
	}

	// The gate must NOT have fired.
	select {
	case msg := <-pendingSub.Channel:
		t.Fatalf("unexpected task.pending_approval event: %s", msg.Payload)
	case <-time.After(200 * time.Millisecond):
	}

	// Steps persisted and root steps promoted.
	stepStore := taskStore.StepStore()
	persisted, err := stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list steps: %v", err)
	}
	if len(persisted) != 4 {
		t.Errorf("persisted steps = %d, want 4", len(persisted))
	}
	ready, err := stepStore.GetReadySteps(tsk.ID)
	if err != nil {
		t.Fatalf("failed to list ready steps: %v", err)
	}
	if len(ready) != 3 {
		t.Errorf("ready steps = %d, want 3 (roots + first dependent promoted)", len(ready))
	}
}

// TestAutoSeal_PlainPlanStillParks proves the baseline: a plain plan with
// enough steps (past the complexity threshold, no AutoSeal) still parks at
// the approval gate with pending_steps + task.pending_approval, and no
// auto_sealed audit trail appears.
func TestAutoSeal_PlainPlanStillParks(t *testing.T) {
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

	// Plain plan: the gate must fire and the task must park.
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
		t.Error("'auto_sealed' metadata present on parked plain plan, want absent")
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
