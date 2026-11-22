package agent

// 06-planning TEST-GATE T3 (F13c): the plan-compiler draft must get a
// chat-visible status + seal action keyed by task_id.
//
// RED today for missing-implementation reasons:
//   - the compiler branch of Plan (strategic.go, plan mode +
//     planCompilerFlag) seeds the raw scaffold via seedDraftFromRequest
//     and returns nil with the task parked in StatePlanning - it
//     publishes NO event, so no chat surface ever learns a draft exists
//     ("raw scaffold parks forever", P1/F-ORCH-1).
//   - SaveDraft (plan.draft save / refinement) likewise publishes
//     nothing: a refined draft never reaches the chat surface.
//   - nothing auto-seals today, but no pin guards it; the gate pins
//     "raw scaffold never auto-sealed" so the future surface cannot
//     regress into self-seal (option A: explicit human seal).
//
// Event contract (06-planning/03 T1 exposes): a draft-status event keyed
// by task_id carrying the draft state + a seal affordance reference.
// The topic name is not frozen by the parent contracts; the gate accepts
// any "task.*" topic whose payload carries task_id AND a draft/seal
// signal, so the impl leaf may name it (task.draft_status recommended).
//
// Gate tests: do not weaken. GREEN after 06-planning/03 T1.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// gateDraftEvent is one captured task.* event with a decoded payload.
type gateDraftEvent struct {
	Topic   string
	Payload map[string]any
}

// gateDraftSurfaceWorld builds a real StrategicPlanner with the compiler
// flag ON over a real task store + bus, subscribing to task.* so any
// draft-status event the planner emits is observable.
func gateDraftSurfaceWorld(t *testing.T) (*StrategicPlanner, *bus.MessageBus, *bus.Subscriber) {
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
	sub := msgBus.Subscribe("gate-draft-surface", "task.*")
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	return sp, msgBus, sub
}

// gateCollectDraftEvents drains the subscriber for a bounded window and
// returns every task.* event whose payload mentions this task.
func gateCollectDraftEvents(sub *bus.Subscriber, taskID string, window time.Duration) []gateDraftEvent {
	var out []gateDraftEvent
	deadline := time.After(window)
	for {
		select {
		case msg, ok := <-sub.Channel:
			if !ok {
				return out
			}
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				continue
			}
			if id, _ := payload["task_id"].(string); id == taskID {
				out = append(out, gateDraftEvent{Topic: msg.Topic, Payload: payload})
			}
		case <-deadline:
			return out
		}
	}
}

// gateDraftSignal reports whether an event is a draft-status surface
// signal: the topic or payload names the draft and/or the seal action.
func gateDraftSignal(ev gateDraftEvent) bool {
	if strings.Contains(ev.Topic, "draft") || strings.Contains(ev.Topic, "seal") {
		return true
	}
	for k, v := range ev.Payload {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "draft") || strings.Contains(lk, "seal") {
			return true
		}
		if s, ok := v.(string); ok {
			ls := strings.ToLower(s)
			if strings.Contains(ls, "draft") || strings.Contains(ls, "seal") {
				return true
			}
		}
	}
	return false
}

// TestDraftChatSurfaceSeeded pins F13c: when the compiler branch seeds a
// brainstorm draft, the chat surface must learn about it - a draft-status
// event keyed by task_id with a seal affordance. RED today: the compiler
// branch returns nil silently (zero events), the draft parks forever.
func TestDraftChatSurfaceSeeded(t *testing.T) {
	sp, _, sub := gateDraftSurfaceWorld(t)

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

	// The draft itself exists (the seed worked) - the gap is the surface.
	d, ok := sp.DraftFor(tsk.ID)
	if !ok || d == nil || d.Markdown == "" {
		t.Fatalf("compiler branch did not seed a draft (fixture broken): ok=%v", ok)
	}

	events := gateCollectDraftEvents(sub, tsk.ID, 1500*time.Millisecond)
	var signals []gateDraftEvent
	for _, ev := range events {
		if gateDraftSignal(ev) {
			signals = append(signals, ev)
		}
	}
	if len(signals) == 0 {
		t.Fatalf("F13c: seeded compiler draft published no draft-status/seal event keyed by task_id %s (captured task.* events: %d) - the raw scaffold parks forever with no chat surface", tsk.ID, len(events))
	}
	// The surface signal must carry the draft state so the chat card can
	// render "draft awaiting your seal", not a bare task id.
	ev := signals[0]
	if _, hasStatus := ev.Payload["status"]; !hasStatus {
		if _, hasState := ev.Payload["state"]; !hasState {
			t.Errorf("F13c: draft-status event %s carries no status/state field for the chat card: %+v", ev.Topic, ev.Payload)
		}
	}
}

// TestDraftChatSurfaceRefinement pins the refinement half (02-routing/03
// continuation reaches the draft): saving a revised draft while planning
// must update the SAME task-keyed surface (a fresh draft-status event),
// not spawn a second draft or stay silent. RED today: SaveDraft publishes
// nothing.
func TestDraftChatSurfaceRefinement(t *testing.T) {
	sp, _, sub := gateDraftSurfaceWorld(t)

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
	// Drain the seed events so the refinement assertion is isolated.
	gateCollectDraftEvents(sub, tsk.ID, 300*time.Millisecond)

	if err := sp.SaveDraft(tsk.ID, "# Plan: refined\n\n## Goal\n\nRefined goal.\n"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	events := gateCollectDraftEvents(sub, tsk.ID, 1500*time.Millisecond)
	found := false
	for _, ev := range events {
		if gateDraftSignal(ev) {
			found = true
		}
	}
	if !found {
		t.Fatalf("F13c: draft refinement (SaveDraft) published no draft-status event for task %s - the chat surface never learns the draft changed", tsk.ID)
	}
	// Same draft, bumped version - refinement updates, never forks.
	d, ok := sp.DraftFor(tsk.ID)
	if !ok || d.Version < 2 {
		t.Fatalf("refinement did not bump the same draft version: ok=%v version=%d", ok, d.Version)
	}
}

// TestDraftChatSurfaceRawScaffoldNeverAutoSealed is the load-bearing
// option-A pin: seeding a raw scaffold must NEVER seal it or schedule
// execution - sealing is an explicit human action through plan.seal.
// The pin guards the future surface against self-seal regressions.
func TestDraftChatSurfaceRawScaffoldNeverAutoSealed(t *testing.T) {
	sp, msgBus, _ := gateDraftSurfaceWorld(t)
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
