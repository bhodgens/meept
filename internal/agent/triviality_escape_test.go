package agent

// 06-planning TEST-GATE T4 (F13c): trivial inputs must escape
// over-decomposition, and the triviality cap must run BEFORE the
// approval gate counts steps.
//
// RED today for missing-implementation reasons (P4 / F-ORCH-14):
//   - the gate counted 21 steps for a trivial question even with
//     max_plan_steps=4: parsePlanOutput truncates to maxPlanSteps
//     (strategic.go :1673) but the multi-phase path
//     (FlattenPlanPhasesToSteps) is only capped PER PHASE, and the
//     approval gate (requiresApproval, :2185) counts whatever survives.
//     There is no triviality cap at all, and no ordering guarantee that
//     a cap runs before the gate count.
//   - a trivial question ("what is 2+2?") planned in plan mode with an
//     over-decomposed planner output trips the approval gate today;
//     the contract is that trivial input is capped to a single step (or
//     routed direct) BEFORE the gate ever counts it, so 2+2 never asks
//     for approval.
//
// ORDERING PIN (load-bearing, branch review checklist): the cap must be
// observable BEFORE requiresApproval counts - a 21-step decomposition of
// a trivial input must reach the gate as <= 1 step, never as 21 with a
// post-gate truncation. Complex plans must still gate correctly (the
// escape is conservative: it never caps genuine multi-step work).
//
// Gate tests: do not weaken. GREEN after 06-planning/03 T4.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// gateTrivialChatter answers every planner call with a fixed
// over-decomposed plan (21 steps) - the frozen P4 shape: the model
// over-decomposes a trivial question and the planner must cap it.
type gateTrivialChatter struct {
	resp string
}

func (c *gateTrivialChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{
		Content:      c.resp,
		FinishReason: "stop",
		Usage:        llm.TokenUsage{TotalTokens: 5},
	}, nil
}

func (c *gateTrivialChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *gateTrivialChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "trivial-escape-test"}
}

// gateOverDecomposedPlan builds a 21-step plan JSON (the P4 fixture).
func gateOverDecomposedPlan() string {
	steps := make([]map[string]any, 0, 21)
	for i := 0; i < 21; i++ {
		steps = append(steps, map[string]any{
			"description": fmt.Sprintf("unnecessary sub-step %d", i),
			"tool_hint":   "analyze",
			"depends_on":  []int{},
		})
	}
	raw, _ := json.Marshal(map[string]any{"steps": steps})
	return string(raw)
}

// gateTrivialWorld builds a planner whose registry resolves the planner
// agent to a loop wired to the given chatter (the emptyPlan fixture
// shape), with a low approval threshold so an uncapped 21-step plan
// WOULD trip the gate.
func gateTrivialWorld(t *testing.T, resp string) (*StrategicPlanner, *bus.MessageBus, *bus.Subscriber) {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })
	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	chatter := &gateTrivialChatter{resp: resp}
	reg := newEmptyPlanTestRegistry(chatter)
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		Registry:              reg,
		TaskStore:             taskStore,
		StepStore:             taskStore.StepStore(),
		Bus:                   msgBus,
		MaxPlanSteps:          4,
		ApprovalStepThreshold: 3,
		PlannerTimeout:        10 * time.Second,
		Logger:                slogDiscardLogger(),
	})
	pendSub := msgBus.Subscribe("gate-trivial-pending", "task.pending_approval")
	t.Cleanup(func() { msgBus.Unsubscribe(pendSub) })
	return sp, msgBus, pendSub
}

// TestTrivialityEscapeNeverGatesTrivialInput is the headline pin: a
// trivial question whose planner output over-decomposes into 21 steps
// must NOT trip the approval gate - the triviality cap reduces it before
// the gate counts. RED today: 21 steps (or the 4-step maxPlanSteps
// truncation) reach requiresApproval with threshold 3 -> gate fires.
func TestTrivialityEscapeNeverGatesTrivialInput(t *testing.T) {
	sp, _, pendSub := gateTrivialWorld(t, gateOverDecomposedPlan())

	tsk := newTestTask("trivial question", "what is 2+2?")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-trivial-escape",
		Input:     "what is 2+2?",
		Intent:    string(IntentPlan),
		Mode:      "plan",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// No approval gate for a trivial input.
	select {
	case msg := <-pendSub.Channel:
		t.Fatalf("F13c/P4 LOAD-BEARING: trivial input \"what is 2+2?\" tripped the approval gate (task.pending_approval published: %s) - the triviality cap must run BEFORE the gate counts steps", string(msg.Payload))
	case <-time.After(400 * time.Millisecond):
	}

	got, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State == task.StateAwaitingApproval {
		t.Fatalf("F13c/P4: trivial input parked at awaiting_approval (state=%q); 2+2 must never ask for approval", got.State)
	}
	// The executed plan is capped: a trivial input gets at most one step
	// (the escape), never the 21-step over-decomposition.
	steps, err := sp.stepStore.ListByTaskID(tsk.ID)
	if err != nil {
		t.Fatalf("list steps: %v", err)
	}
	if len(steps) > 1 {
		t.Fatalf("F13c/P4: trivial input kept %d steps; the triviality cap must reduce it to a single step before execution", len(steps))
	}
}

// TestTrivialityEscapeCapBeforeGateOrdering pins the ORDERING directly at
// the requiresApproval seam: the step slice the gate counts for a trivial
// input must already be capped. It drives Plan with the over-decomposed
// output and asserts the gate's own decision is false for the trivial
// input while a genuinely complex plan of the same raw size still gates.
// RED today: the trivial input's steps reach the gate uncapped.
func TestTrivialityEscapeCapBeforeGateOrdering(t *testing.T) {
	sp, _, _ := gateTrivialWorld(t, gateOverDecomposedPlan())

	// Trivial input: the gate must see a capped plan (no approval).
	trivialTask := newTestTask("trivial ordering", "what is 2+2?")
	if err := sp.taskStore.Create(trivialTask); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    trivialTask.ID,
		SessionID: "sess-trivial-order",
		Input:     "what is 2+2?",
		Intent:    string(IntentPlan),
		Mode:      "plan",
	}); err != nil {
		t.Fatalf("Plan trivial: %v", err)
	}
	gotTrivial, err := sp.taskStore.GetByID(trivialTask.ID)
	if err != nil || gotTrivial == nil {
		t.Fatalf("get trivial task: %v", err)
	}
	if gotTrivial.State == task.StateAwaitingApproval {
		t.Fatalf("F13c/P4 ORDERING: trivial input reached the gate uncapped and parked at awaiting_approval - cap must precede gate count")
	}

	// Complex input (same 21-step raw output): the gate MUST still fire.
	// The escape is conservative - it never caps genuine multi-step work.
	sp2, _, pendSub2 := gateTrivialWorld(t, gateOverDecomposedPlan())
	complexTask := newTestTask("complex ordering", "refactor the storage layer to support multi-tenant isolation and migrate all existing data")
	if err := sp2.taskStore.Create(complexTask); err != nil {
		t.Fatalf("create complex task: %v", err)
	}
	if err := sp2.Plan(context.Background(), PlanRequest{
		TaskID:    complexTask.ID,
		SessionID: "sess-complex-order",
		Input:     "refactor the storage layer to support multi-tenant isolation and migrate all existing data",
		Intent:    string(IntentPlan),
		Mode:      "plan",
	}); err != nil {
		t.Fatalf("Plan complex: %v", err)
	}
	select {
	case <-pendSub2.Channel:
		// Complex plan gated: correct.
	case <-time.After(400 * time.Millisecond):
		gotComplex, err := sp2.taskStore.GetByID(complexTask.ID)
		if err != nil || gotComplex == nil {
			t.Fatalf("get complex task: %v", err)
		}
		if gotComplex.State != task.StateAwaitingApproval {
			t.Fatalf("F13c/P4: complex multi-step plan did NOT gate (state=%q); the triviality escape must never cap genuine work", gotComplex.State)
		}
	}
}

// TestTrivialityEscapeHeuristicConservative pins that the escape only
// fires for genuinely trivial inputs: a short but real artifact request
// keeps its multi-step plan and gates normally. GREEN today (no escape
// exists); must stay GREEN after the escape lands (over-reach guard).
func TestTrivialityEscapeHeuristicConservative(t *testing.T) {
	sp, _, pendSub := gateTrivialWorld(t, gateOverDecomposedPlan())

	tsk := newTestTask("short artifact request", "create a file named notes.txt with the summary")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-artifact-guard",
		Input:     "create a file named notes.txt with the summary",
		Intent:    string(IntentPlan),
		Mode:      "plan",
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	select {
	case <-pendSub.Channel:
		// Gated: the artifact request was not over-capped. Correct.
	case <-time.After(400 * time.Millisecond):
		got, err := sp.taskStore.GetByID(tsk.ID)
		if err != nil || got == nil {
			t.Fatalf("get task: %v", err)
		}
		if got.State != task.StateAwaitingApproval {
			t.Fatalf("F13c/P4: a real artifact request lost its approval gate (state=%q); the triviality escape over-reached into genuine work", got.State)
		}
	}
}
