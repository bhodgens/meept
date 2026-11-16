package employee

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/kanban"
)

// newGatedGoalStore returns the package's temp-DB GoalStore (testGoalStore,
// goal_test.go) with a live bus armed on the goal-state topic. Mirrors
// internal/queue/kanban_events_test.go's newGatedQueue shape (leaf 02/04).
func newGatedGoalStore(t *testing.T) (*GoalStore, *bus.Subscriber) {
	t.Helper()
	store := testGoalStore(t)
	msgBus := bus.New(nil, nil)
	sub := msgBus.Subscribe("kanban-goal-events-test", kanban.TopicGoalState.Name)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	store.SetKanbanEventBus(msgBus)
	store.SetKanbanEventPublishing(true)
	return store, sub
}

func drainGoalEvents(t *testing.T, sub *bus.Subscriber, n int) []kanban.StateEvent {
	t.Helper()
	out := make([]kanban.StateEvent, 0, n)
	deadline := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				t.Fatalf("channel closed after %d/%d events", len(out), n)
			}
			var ev kanban.StateEvent
			if err := json.Unmarshal(m.Payload, &ev); err != nil {
				t.Fatalf("decode StateEvent: %v", err)
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatalf("timed out after %d/%d events", len(out), n)
		}
	}
	return out
}

func expectNoGoalEvent(t *testing.T, sub *bus.Subscriber) {
	t.Helper()
	select {
	case m := <-sub.Channel:
		t.Fatalf("unexpected goal StateEvent: %s", m.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestGoalTransitions_EveryEdgeHasPublisherAndReason walks goalTransitions —
// the C-P3 machine authority — and asserts every legal edge carries a
// non-empty StateEvent.Reason via goalEventReason. The store paths that
// publish: UpdateWithTransition (every fenced edge incl. the human bypass)
// and Retire (every *-to-retired edge with the true From).
func TestGoalTransitions_EveryEdgeHasPublisherAndReason(t *testing.T) {
	edges := 0
	for from, tos := range goalTransitions {
		for to := range tos {
			edges++
			if r := goalEventReason(to); r == "" {
				t.Errorf("edge %s -> %s resolves to an empty StateEvent.Reason", from, to)
			}
		}
	}
	// active->{paused,blocked,retired}, paused->{active,blocked,retired},
	// blocked->{active,retired}, retired->{} — 8 edges total.
	if edges != 8 {
		t.Errorf("goalTransitions edge count = %d, want 8 (table drift)", edges)
	}
	if goalEventReason(GoalActive) != kanban.EventReasonResume {
		t.Errorf("blocked->active (human bypass) reason = %q, want resume", goalEventReason(GoalActive))
	}
	if goalEventReason(GoalRetired) != kanban.EventReasonRetire {
		t.Errorf("retire reason = %q, want retire", goalEventReason(GoalRetired))
	}
}

// TestGoalPublish_TransitionEdges drives the fenced writes through the real
// store methods and pins each edge's From/To/Reason triple, incl. the
// same-state write and refused-transition dispositions (both silent).
func TestGoalPublish_TransitionEdges(t *testing.T) {
	ctx := context.Background()
	store, sub := newGatedGoalStore(t)

	g := mustCreateGoal(t, store, &Goal{}) // lands active; Create is not a transition

	// active -> paused (park).
	g.State = GoalPaused
	if err := store.UpdateWithTransition(ctx, g, false); err != nil {
		t.Fatalf("pause: %v", err)
	}
	ev := drainGoalEvents(t, sub, 1)[0]
	if ev.Kind != kanban.EventKindGoal || ev.ID != g.ID ||
		ev.From != GoalActive.String() || ev.To != GoalPaused.String() ||
		ev.Reason != kanban.EventReasonPark {
		t.Fatalf("pause event: %+v", ev)
	}

	// Same-state field update: publishes nothing.
	g.Title = "updated title"
	if err := store.UpdateWithTransition(ctx, g, false); err != nil {
		t.Fatalf("same-state update: %v", err)
	}
	expectNoGoalEvent(t, sub)

	// paused -> blocked.
	g.State = GoalBlocked
	if err := store.UpdateWithTransition(ctx, g, false); err != nil {
		t.Fatalf("block: %v", err)
	}
	ev = drainGoalEvents(t, sub, 1)[0]
	if ev.From != GoalPaused.String() || ev.To != GoalBlocked.String() || ev.Reason != kanban.EventReasonBlock {
		t.Fatalf("block event: %+v", ev)
	}

	// blocked -> active WITHOUT the bypass is fence-refused: no write, no event.
	g.State = GoalActive
	if err := store.UpdateWithTransition(ctx, g, false); err == nil {
		t.Fatal("expected ErrGoalTransition for blocked->active without bypass")
	}
	expectNoGoalEvent(t, sub)

	// blocked -> active WITH the human bypass (the goal_update resume path).
	if err := store.UpdateWithTransition(ctx, g, true); err != nil {
		t.Fatalf("resume bypass: %v", err)
	}
	ev = drainGoalEvents(t, sub, 1)[0]
	if ev.From != GoalBlocked.String() || ev.To != GoalActive.String() || ev.Reason != kanban.EventReasonResume {
		t.Fatalf("resume event: %+v", ev)
	}

	// Retire via the dedicated primitive: From carries the true pre-state.
	if err := store.Retire(ctx, g.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	ev = drainGoalEvents(t, sub, 1)[0]
	if ev.From != GoalActive.String() || ev.To != GoalRetired.String() || ev.Reason != kanban.EventReasonRetire {
		t.Fatalf("retire event: %+v", ev)
	}
}

// TestGoalPublish_RetireFromBlocked pins the gate-armed pre-state read in
// Retire: the event's From is the persisted state, not a constant.
func TestGoalPublish_RetireFromBlocked(t *testing.T) {
	ctx := context.Background()
	store, sub := newGatedGoalStore(t)
	g := mustCreateGoal(t, store, &Goal{})
	g.State = GoalBlocked
	if err := store.UpdateWithTransition(ctx, g, false); err != nil {
		t.Fatalf("block: %v", err)
	}
	drainGoalEvents(t, sub, 1)

	if err := store.Retire(ctx, g.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	ev := drainGoalEvents(t, sub, 1)[0]
	if ev.From != GoalBlocked.String() || ev.To != GoalRetired.String() {
		t.Fatalf("retire-from-blocked event: %+v", ev)
	}
}

// TestGoalPublish_GateOffIsSilent pins the opt-in default: a bus wired but
// the gate unarmed means no StateEvent ever reaches the topic.
func TestGoalPublish_GateOffIsSilent(t *testing.T) {
	ctx := context.Background()
	store := testGoalStore(t)
	msgBus := bus.New(nil, nil)
	sub := msgBus.Subscribe("gate-off-goal", kanban.TopicGoalState.Name)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	store.SetKanbanEventBus(msgBus)
	// Gate NOT armed — the default.

	g := mustCreateGoal(t, store, &Goal{})
	g.State = GoalPaused
	if err := store.UpdateWithTransition(ctx, g, false); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := store.Retire(ctx, g.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	expectNoGoalEvent(t, sub)
}

// TestGoalPublish_NilBusSafety: gate armed but no bus set = silent, no panic.
func TestGoalPublish_NilBusSafety(t *testing.T) {
	ctx := context.Background()
	store := testGoalStore(t)
	store.SetKanbanEventPublishing(true)
	// SetKanbanEventBus never called: kanbanBus stays nil.
	g := mustCreateGoal(t, store, &Goal{})
	g.State = GoalRetired
	if err := store.UpdateWithTransition(ctx, g, false); err != nil {
		t.Fatalf("retire via update: %v", err)
	}
	if err := store.Retire(ctx, NewGoalID(), time.Now().UTC()); err == nil {
		// Retire on a missing row is ErrGoalNotFound; the pre-state read is
		// gate-armed here, so confirm the typed error survives (no panic).
		t.Fatal("expected ErrGoalNotFound")
	}
}
