package agent

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Leaf-06 task 2: Touch/AttachTask wiring completeness
//
// The single-point Touch inside publishWorkerEvent and the base
// Touch/Complete coverage tests already live in
// handler_terminal_event_test.go (TestPublishWorkerEvent_TouchesTrackedTurn
// and friends, leaf 01/03). This file adds the leaf-06 additions:
//   - all four worker-event topics flow through the touching path;
//   - AttachTask at the task-creation points so reaped events carry task_id;
//   - AttachTask no-op guards.
// ---------------------------------------------------------------------------

// TestPublishWorkerEvent_TouchCoversEveryCallerPath pins the "single point"
// wiring: every worker-event topic the handler publishes (started /
// state_changed / error / completed) flows through publishWorkerEvent's
// Touch, so no matter which lifecycle transition a live turn is in, its
// reaper clock resets.
func TestPublishWorkerEvent_TouchCoversEveryCallerPath(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC))
	reg := NewTurnRegistry()
	reg.now = clock.Now

	h := newTestChatHandlerWithBus(t)
	h.SetTurnRegistry(reg)
	reg.Register("turn-all", "conv-all")

	topics := []string{
		"chat.worker.started",
		"chat.worker.state_changed",
		"chat.worker.error",
		"chat.worker.completed",
	}
	for i, topic := range topics {
		clock.Advance(time.Duration(i+1) * time.Minute)
		h.publishWorkerEvent(topic, &Worker{ID: "w-all", TurnID: "turn-all"})
		rec, ok := registryGet(t, reg, "turn-all")
		if !ok {
			t.Fatalf("%s: turn dropped from registry", topic)
		}
		if !rec.LastProgressAt.Equal(clock.Now()) {
			t.Errorf("%s: Touch not applied (last_progress=%v, now=%v)", topic, rec.LastProgressAt, clock.Now())
		}
	}
}

// TestAttachTask_ReapedEventCarriesTaskID verifies the task-creation
// wiring: after attachTask records the dispatched task, a reaped terminal
// event for the turn carries its task_id (plus the full identity).
func TestAttachTask_ReapedEventCarriesTaskID(t *testing.T) {
	start := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	reg := NewTurnRegistry()
	reg.now = clock.Now

	h := newTestChatHandlerWithBus(t)
	h.SetTurnRegistry(reg)
	reg.Register("turn-task", "conv-task")

	// Task creation on the dispatch path attaches the orchestrator task.
	h.attachTask("turn-task", "task-42")

	clock.Advance(10 * time.Minute)

	em := &captureEmitter{}
	w := NewTurnWatchdog(reg, em.emit, discardLogger())
	w.RunOnce(time.Minute)

	events := em.snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].TaskID != "task-42" {
		t.Errorf("reaped event task_id = %q, want task-42", events[0].TaskID)
	}
	if events[0].ConversationID != "conv-task" || events[0].TurnID != "turn-task" {
		t.Errorf("reaped event identity = %+v", events[0])
	}
}

// TestAttachTask_NoopForUntracked verifies attachTask guards: empty turn or
// task ids and unknown turns are no-ops (legacy chat path never panics, and
// a nil registry is safe).
func TestAttachTask_NoopForUntracked(t *testing.T) {
	reg := NewTurnRegistry()
	h := newTestChatHandlerWithBus(t)
	h.SetTurnRegistry(reg)

	h.attachTask("", "task-x")          // empty turn id
	h.attachTask("turn-none", "")       // empty task id
	h.attachTask("turn-none", "task-x") // unknown turn

	hNil := newTestChatHandlerWithBus(t)
	hNil.attachTask("turn-y", "task-y") // nil registry

	if stale := reg.Stale(time.Hour); len(stale) != 0 {
		t.Errorf("registry churned: %+v", stale)
	}
}

// TestEmitTurnTerminal_ForwardsToBus proves the exported emit seam forwards
// to the frozen turn.terminal funnel (what the daemon composition hands the
// watchdog in production).
func TestEmitTurnTerminal_ForwardsToBus(t *testing.T) {
	h := newTestChatHandlerWithBus(t)

	sub := h.bus.Subscribe("test-turn-terminal", "turn.terminal")
	defer h.bus.Unsubscribe(sub)

	h.EmitTurnTerminal(TurnTerminalEvent{
		ConversationID: "conv-seam",
		TurnID:         "turn-seam",
		HandlerCase:    "turn_reaped",
		Status:         "failed",
		Reply:          "this turn stopped responding and was marked failed",
	})

	ev := waitTurnTerminal(t, sub)
	if ev.TurnID != "turn-seam" || ev.Status != "failed" || ev.HandlerCase != "turn_reaped" {
		t.Errorf("forwarded event = %+v", ev)
	}
}
