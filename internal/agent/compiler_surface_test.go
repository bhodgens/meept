package agent

// 06-planning TEST-GATE T3 (F13c): the plan-compiler draft must be
// reachable from the chat surface, keyed by task_id, with an explicit
// human seal (option A) and no self-seal of a raw scaffold.
//
// SURFACE-CONTRACT ADJUSTMENT (usability merge): 06-planning/03 T1 described
// a draft-STATUS EVENT as the chat surface (any "task.*" topic carrying a
// draft/seal signal). That event publisher was never implemented on this
// tree: no production call site emits a draft-status topic. The shipped
// surface is a PULL model instead -- internal/rpc/plan_seal.go registers
// "plan.draft get/save" and "plan.seal", all keyed by task_id, backed by
// StrategicPlanner.DraftFor / SaveDraft / SealDraft over task metadata.
// Asserting an event that production never emits would pin behavior this
// tree does not have, so these gates now exercise the real surface: the
// seeded/refined draft is observable through DraftFor(taskID) and stays
// unsealed until an explicit SealDraft. TestDraftChatSurfaceRawScaffoldNeverAutoSealed
// below keeps the load-bearing option-A pin intact.

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// gateDraftSurfaceWorld builds a real StrategicPlanner with the compiler
// flag ON over a real task store + bus. The bus is retained because the
// load-bearing no-auto-seal pin below watches orchestrator.schedule.
func gateDraftSurfaceWorld(t *testing.T) (*StrategicPlanner, *bus.MessageBus) {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	t.Cleanup(func() { msgBus.Close() })

	taskStore, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:    taskStore,
		StepStore:    taskStore.StepStore(),
		Bus:          msgBus,
		Logger:       slogDiscardLogger(),
		MaxPlanSteps: 10,
	})
	sp.SetPlanCompilerEnabled(true)
	return sp, msgBus
}

// TestDraftChatSurfaceSeeded pins F13c: when the compiler branch seeds a
// brainstorm draft, the chat surface (plan.draft get, keyed by task_id)
// must be able to read a draft awaiting seal with a visible status.
// The seeded scaffold parks in StatePlanning until an explicit plan.seal.
func TestDraftChatSurfaceSeeded(t *testing.T) {
	sp, _ := gateDraftSurfaceWorld(t)

	tsk := newTestTask("draft surface task", "design the storage rework")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-draft-surface",
		Input:     "design the storage rework",
		Intent:    string(IntentPlan),
		Mode:      "plan",
	})
	if err != nil {
		t.Fatalf("Plan (compiler branch): %v", err)
	}

	// The draft is the chat surface's payload: plan.draft get reads it
	// through DraftFor(taskID). A seeded draft must carry markdown and a
	// visible unsealed status (SealedHash empty) the card can render as
	// "draft awaiting your seal", not a bare task id.
	d, ok := sp.DraftFor(tsk.ID)
	if !ok || d == nil || d.Markdown == "" {
		t.Fatalf("F13c: plan.draft get exposed no seeded draft for task %s (ok=%v) - the raw scaffold parks forever with no chat surface", tsk.ID, ok)
	}
	if d.SealedHash != "" {
		t.Errorf("F13c: seeded draft has a seal (%q) before an explicit plan.seal; the chat card would not render 'awaiting your seal'", d.SealedHash)
	}
	got, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StatePlanning {
		t.Errorf("F13c: seeded draft task state = %q, want planning (parked awaiting seal)", got.State)
	}
}

// TestDraftChatSurfaceRefinement pins the refinement half (02-routing/03
// continuation reaches the draft): saving a revised draft while planning
// must update the SAME task-keyed draft the chat surface reads through
// plan.draft get, bumping its version rather than forking a second draft
// or losing the refinement.
func TestDraftChatSurfaceRefinement(t *testing.T) {
	sp, _ := gateDraftSurfaceWorld(t)

	tsk := newTestTask("draft refinement task", "design the storage rework")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-draft-refine",
		Input:     "design the storage rework",
		Intent:    string(IntentPlan),
		Mode:      "plan",
	}); err != nil {
		t.Fatalf("Plan (compiler branch): %v", err)
	}

	before, ok := sp.DraftFor(tsk.ID)
	if !ok || before == nil {
		t.Fatal("no draft seeded (fixture broken)")
	}

	const refined = "# Plan: refined\n\n## Goal\n\nRefined goal.\n"
	if err := sp.SaveDraft(tsk.ID, refined); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	// The chat surface reads the SAME task's draft after refinement: the
	// markdown is updated in place and the version is bumped (never forked).
	d, ok := sp.DraftFor(tsk.ID)
	if !ok || d == nil {
		t.Fatalf("F13c: plan.draft get exposed no draft for task %s after SaveDraft", tsk.ID)
	}
	if d.Markdown != refined {
		t.Fatalf("F13c: refined draft markdown not visible to the chat surface: %q", d.Markdown)
	}
	if d.Version < 2 {
		t.Fatalf("F13c: refinement did not bump the same draft version: ok=%v version=%d", ok, d.Version)
	}
}

// TestDraftChatSurfaceRawScaffoldNeverAutoSealed is the load-bearing
// option-A pin: seeding a raw scaffold must NEVER seal it or schedule
// execution - sealing is an explicit human action through plan.seal.
// The pin guards the future surface against self-seal regressions.
func TestDraftChatSurfaceRawScaffoldNeverAutoSealed(t *testing.T) {
	sp, msgBus := gateDraftSurfaceWorld(t)
	schedSub := msgBus.Subscribe("gate-draft-schedule", "orchestrator.schedule")
	t.Cleanup(func() { msgBus.Unsubscribe(schedSub) })

	tsk := newTestTask("raw scaffold task", "design the storage rework")
	if err := sp.taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := sp.Plan(context.Background(), PlanRequest{
		TaskID:    tsk.ID,
		SessionID: "sess-draft-noseal",
		Input:     "design the storage rework",
		Intent:    string(IntentPlan),
		Mode:      "plan",
	}); err != nil {
		t.Fatalf("Plan (compiler branch): %v", err)
	}

	d, ok := sp.DraftFor(tsk.ID)
	if !ok {
		t.Fatalf("no draft seeded (fixture broken)")
	}
	if d.SealedHash != "" {
		t.Fatalf("F13c LOAD-BEARING: raw scaffold was auto-sealed (SealedHash=%q); sealing must be an explicit human action", d.SealedHash)
	}
	got, err := sp.taskStore.GetByID(tsk.ID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State == task.StateExecuting {
		t.Fatalf("F13c LOAD-BEARING: raw scaffold task moved to executing without a human seal (state=%q)", got.State)
	}
	select {
	case msg := <-schedSub.Channel:
		t.Fatalf("F13c LOAD-BEARING: raw scaffold published orchestrator.schedule without a human seal: %s", string(msg.Payload))
	case <-time.After(300 * time.Millisecond):
	}
}
