package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/kanban"
)

// newGatedRegistry returns a registry with the kanban StateEvent gate ARMED
// and a raw subscriber on the task-state topic (raw so the test owns the
// channel — SubscribeT's reader goroutine would race it).
func newGatedRegistry(t *testing.T) (*Registry, *bus.Subscriber, context.Context) {
	t.Helper()
	reg := newTestRegistry(t)
	sub := reg.bus.Subscribe("kanban-task-events-test", kanban.TopicTaskState.Name)
	t.Cleanup(func() { reg.bus.Unsubscribe(sub) })
	reg.SetKanbanEventPublishing(true)
	return reg, sub, context.Background()
}

func drainTaskEventsAll(t *testing.T, sub *bus.Subscriber) []kanban.StateEvent {
	t.Helper()
	var out []kanban.StateEvent
	for {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				return out
			}
			var ev kanban.StateEvent
			if err := json.Unmarshal(m.Payload, &ev); err != nil {
				t.Fatalf("decode StateEvent: %v", err)
			}
			out = append(out, ev)
		case <-time.After(150 * time.Millisecond):
			return out
		}
	}
}

func expectNoTaskEvent(t *testing.T, sub *bus.Subscriber) {
	t.Helper()
	select {
	case m := <-sub.Channel:
		t.Fatalf("unexpected task StateEvent: %s", m.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

// allTaskStates walks the TaskState enum for the coverage test.
var allTaskStates = []TaskState{
	StatePending, StatePlanning, StateAwaitingApproval, StateExecuting,
	StateTesting, StateCompleted, StateFailed, StateRejected, StateCancelled,
}

// TestTaskTransitions_EveryStateHasPublisherPath walks taskTransitions — the
// machine authority (plan 02/01) — and asserts (a) every enum state appears
// in the table, and (b) every legal edge is covered by a publisher path:
// Registry.UpdateState publishes ANY legal from->to edge with reason
// "update-state" (the generic machine driver), and CancelTask covers every
// *-to-cancelled edge with reason "cancel".
func TestTaskTransitions_EveryStateHasPublisherPath(t *testing.T) {
	for _, s := range allTaskStates {
		edges, ok := taskTransitions[s]
		if !ok {
			// The task machine expresses terminal states by ABSENCE from the
			// table (taskAllows has no source row) — unlike the queue's
			// empty-slice idiom. A non-terminal state missing here is drift.
			if s.IsTerminal() {
				continue
			}
			t.Errorf("non-terminal TaskState %q missing from taskTransitions", s)
			continue
		}
		for _, to := range edges {
			if !taskAllows(s, to) {
				t.Errorf("taskTransitions entry %s->%s not reflected by taskAllows", s, to)
			}
			// Publisher paths: Registry.UpdateState publishes ANY edge with
			// reason "update-state" (driven per-edge in
			// TestUpdateState_PublishesStateEvent); CancelTask covers every
			// *-to-cancelled edge with reason "cancel" (driven in
			// TestCancelTask_PublishesCancelledEdge).
		}
	}
	// Terminal states (completed/failed/rejected/cancelled) intentionally map
	// to NO entry in taskTransitions — nothing leaves them, so they must
	// publish nothing, mirroring the queue machine's terminal pins.
	for _, s := range allTaskStates {
		if s.IsTerminal() {
			if _, ok := taskTransitions[s]; ok {
				t.Errorf("terminal TaskState %q has outgoing edges in taskTransitions", s)
			}
		}
	}
}

// TestUpdateState_PublishesStateEvent drives every legal edge from a state
// reachable through the generic UpdateState driver and asserts the payload.
func TestUpdateState_PublishesStateEvent(t *testing.T) {
	for from, targets := range taskTransitions {
		for _, to := range targets {
			from, to := from, to
			t.Run(fmt.Sprintf("%s->%s", from, to), func(t *testing.T) {
				reg, sub, ctx := newGatedRegistry(t)
				task, err := reg.Create(ctx, "edge-task", "")
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				// Stage the pre-state directly in the store: the machine
				// authority itself is pinned by transitions_test.go; here the
				// publisher wiring for the from->to edge is what's under test.
				if from != StatePending {
					if _, err := reg.Store().DB().Exec(
						"UPDATE tasks SET state = ? WHERE id = ?", string(from), task.ID); err != nil {
						t.Fatalf("stage %s: %v", from, err)
					}
				}

				if err := reg.UpdateState(ctx, task.ID, to); err != nil {
					t.Fatalf("UpdateState(%s->%s): %v", from, to, err)
				}

				events := drainTaskEventsAll(t, sub)
				if len(events) != 1 {
					t.Fatalf("events = %d, want exactly 1 for %s->%s", len(events), from, to)
				}
				ev := events[0]
				if ev.Kind != kanban.EventKindTask || ev.ID != task.ID {
					t.Errorf("event kind/id = %q/%q, want task/%q", ev.Kind, ev.ID, task.ID)
				}
				if ev.From != string(from) || ev.To != string(to) {
					t.Errorf("event from/to = %q/%q, want %q/%q", ev.From, ev.To, from, to)
				}
				if ev.Reason != kanban.EventReasonUpdateState {
					t.Errorf("event reason = %q, want %q", ev.Reason, kanban.EventReasonUpdateState)
				}
				if ev.TS.IsZero() {
					t.Error("event TS must be filled")
				}
			})
		}
	}
}

// TestUpdateState_NoChangePublishesNothing: a same-state write publishes no
// StateEvent (the state did not transition).
func TestUpdateState_NoChangePublishesNothing(t *testing.T) {
	reg, sub, ctx := newGatedRegistry(t)
	task, err := reg.Create(ctx, "same-state", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.UpdateState(ctx, task.ID, StatePending); err != nil {
		t.Fatalf("UpdateState(pending->pending): %v", err)
	}
	expectNoTaskEvent(t, sub)
}

// TestCancelTask_PublishesCancelledEdge: every non-terminal source state
// reaches cancelled with reason "cancel"; a terminal task is a no-op that
// publishes nothing.
func TestCancelTask_PublishesCancelledEdge(t *testing.T) {
	nonTerminal := []TaskState{StatePending, StatePlanning, StateAwaitingApproval, StateExecuting, StateTesting}
	for _, from := range nonTerminal {
		from := from
		t.Run(string(from)+"->cancelled", func(t *testing.T) {
			reg, sub, ctx := newGatedRegistry(t)
			task, err := reg.Create(ctx, "cancel-task", "")
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if from != StatePending {
				if _, err := reg.Store().DB().Exec(
					"UPDATE tasks SET state = ? WHERE id = ?", string(from), task.ID); err != nil {
					t.Fatalf("stage %s: %v", from, err)
				}
			}
			if err := reg.CancelTask(ctx, task.ID, "operator"); err != nil {
				t.Fatalf("CancelTask: %v", err)
			}
			events := drainTaskEventsAll(t, sub)
			if len(events) != 1 {
				t.Fatalf("events = %d, want 1", len(events))
			}
			ev := events[0]
			if ev.From != string(from) || ev.To != string(StateCancelled) || ev.Reason != kanban.EventReasonCancel {
				t.Errorf("event = %+v, want %s->cancelled reason=cancel", ev, from)
			}
		})
	}

	t.Run("terminal no-op publishes nothing", func(t *testing.T) {
		reg, sub, ctx := newGatedRegistry(t)
		task, err := reg.Create(ctx, "done-task", "")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := reg.Store().DB().Exec(
			"UPDATE tasks SET state = ? WHERE id = ?", string(StateCompleted), task.ID); err != nil {
			t.Fatalf("stage completed: %v", err)
		}
		if err := reg.CancelTask(ctx, task.ID, "late"); err != nil {
			t.Fatalf("CancelTask on terminal must be a no-op success: %v", err)
		}
		expectNoTaskEvent(t, sub)
	})
}

// TestTaskKanbanEvents_GateOffIsSilent: default (gate off) drives the same
// transitions and NOTHING lands on the topic.
func TestTaskKanbanEvents_GateOffIsSilent(t *testing.T) {
	reg := newTestRegistry(t) // gate NOT armed — the production default
	sub := reg.bus.Subscribe("kanban-task-gate-off-test", kanban.TopicTaskState.Name)
	t.Cleanup(func() { reg.bus.Unsubscribe(sub) })
	ctx := context.Background()

	task, err := reg.Create(ctx, "silent", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.UpdateState(ctx, task.ID, StatePlanning); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if err := reg.UpdateState(ctx, task.ID, StateExecuting); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if err := reg.CancelTask(ctx, task.ID, "why"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	expectNoTaskEvent(t, sub)
}

// TestSetKanbanEventPublishing_ZeroValueSafe: the setter works on a
// zero-value registry (mu ready-to-use) and publishTaskState with a nil bus
// is a silent no-op (kanban publishers are nil-bus safe).
func TestSetKanbanEventPublishing_ZeroValueSafe(t *testing.T) {
	r := &Registry{}
	r.SetKanbanEventPublishing(true)
	r.SetKanbanEventPublishing(false)
	r.SetKanbanEventPublishing(true)
	r.publishTaskState("t1", StatePending, StatePlanning, kanban.EventReasonUpdateState)
}

// ---------------------------------------------------------------------------
// Step-machine coverage (plan leaf 02/04). Append-only: the task-machine and
// registry cases above (02/03) are untouched.
// ---------------------------------------------------------------------------

// openStepTestDB mirrors newTestStepStore (step_test.go): a shared
// in-memory SQLite DB for StepStore coverage cases (leaf 02/04).
func openStepTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newGatedStepStore returns a StepStore on a shared in-memory DB with a
// live bus armed on the step-state topic and the kanban gate ON (leaf 02/04;
// mirrors newGatedRegistry above).
func newGatedStepStore(t *testing.T) (*StepStore, *bus.Subscriber) {
	t.Helper()
	msgBus := bus.New(nil, nil)
	db := openStepTestDB(t)
	store, err := NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("NewStepStore: %v", err)
	}
	sub := msgBus.Subscribe("kanban-step-events-test", kanban.TopicStepState.Name)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	store.SetKanbanEventBus(msgBus)
	store.SetKanbanEventPublishing(true)
	return store, sub
}

func drainStepEvents(t *testing.T, sub *bus.Subscriber, n int) []kanban.StateEvent {
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

func expectNoStepEvent(t *testing.T, sub *bus.Subscriber) {
	t.Helper()
	select {
	case m := <-sub.Channel:
		t.Fatalf("unexpected step StateEvent: %s", m.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

// allStepStates walks the StepState enum for the coverage case. The step
// machine has NO frozen transitions table (unlike taskTransitions/
// planTransitions/goalTransitions), so per the leaf contract this coverage
// test asserts edges from the ACTUAL state-setting code paths:
// StepStore.SetState (reason state_change) and StepStore.SetStateWithReason
// (reason verbatim) are the two chokepoints every production step transition
// funnels through — the guarded UPDATE statements inside them are the only
// state writers in the step machine.
var allStepStates = []StepState{
	StepPending, StepReady, StepScheduled, StepRunning, StepReviewing,
	StepApproved, StepRejected, StepCompleted, StepFailed, StepSkipped,
}

// TestStepMachine_SetStateChokepointCoversEveryState proves every enum
// state is reachable through SetState with a publishing From, i.e. the
// chokepoint covers the whole state space (state-setting code paths, not a
// table).
func TestStepMachine_SetStateChokepointCoversEveryState(t *testing.T) {
	store, sub := newGatedStepStore(t)

	for i, to := range allStepStates {
		step := NewTaskStep("cover-task", "cover "+to.String(), i)
		if err := store.Create(step); err != nil {
			t.Fatalf("Create(%s): %v", to, err)
		}
		if to == StepPending {
			// Create lands pending: a same-state SetState must publish
			// nothing (from == to disposition).
			if err := store.SetState(step.ID, StepPending); err != nil {
				t.Fatalf("SetState pending: %v", err)
			}
			expectNoStepEvent(t, sub)
			continue
		}
		if err := store.SetState(step.ID, to); err != nil {
			t.Fatalf("SetState(%s): %v", to, err)
		}
		ev := drainStepEvents(t, sub, 1)[0]
		if ev.Kind != kanban.EventKindStep || ev.ID != step.ID ||
			ev.From != StepPending.String() || ev.To != to.String() ||
			ev.Reason != kanban.EventReasonStateChange {
			t.Fatalf("event into %s: %+v", to, ev)
		}
	}
}

// TestStepMachine_SetStateWithReasonCarriesReason pins the reason-param
// contract: a non-empty caller reason rides StateEvent.Reason verbatim;
// an empty reason falls back to state_change.
func TestStepMachine_SetStateWithReasonCarriesReason(t *testing.T) {
	store, sub := newGatedStepStore(t)

	step := NewTaskStep("reason-task", "explicit reason", 0)
	if err := store.Create(step); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetStateWithReason(step.ID, StepRunning, "claim"); err != nil {
		t.Fatalf("SetStateWithReason: %v", err)
	}
	ev := drainStepEvents(t, sub, 1)[0]
	if ev.Reason != "claim" || ev.From != StepPending.String() || ev.To != StepRunning.String() {
		t.Fatalf("reason event: %+v", ev)
	}

	// Empty reason falls back to the generic label.
	if err := store.SetStateWithReason(step.ID, StepReviewing, ""); err != nil {
		t.Fatalf("SetStateWithReason empty: %v", err)
	}
	ev = drainStepEvents(t, sub, 1)[0]
	if ev.Reason != kanban.EventReasonStateChange || ev.To != StepReviewing.String() {
		t.Fatalf("fallback reason: %+v", ev)
	}

	// Same-state write publishes nothing even with a reason.
	if err := store.SetStateWithReason(step.ID, StepReviewing, "noop"); err != nil {
		t.Fatalf("same-state: %v", err)
	}
	expectNoStepEvent(t, sub)
}

// TestStepMachine_SilentDispositionsPublishNothing pins the non-publishing
// dispositions of the chokepoints: missing step (error path) and the
// same-state re-issue that the guarded UPDATE swallows as a lost race.
func TestStepMachine_SilentDispositionsPublishNothing(t *testing.T) {
	store, sub := newGatedStepStore(t)

	step := NewTaskStep("race-task", "silent dispositions", 0)
	if err := store.Create(step); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Missing step: SetState errors, publishes nothing.
	if err := store.SetState("no-such-step", StepRunning); err == nil {
		t.Fatal("expected error for missing step")
	}
	expectNoStepEvent(t, sub)

	// Real transition, drained...
	if err := store.SetState(step.ID, StepRunning); err != nil {
		t.Fatalf("first SetState: %v", err)
	}
	drainStepEvents(t, sub, 1)
	// ...then the same-state re-issue: the in-tx SELECT reads running,
	// oldState==state, the helper's from==to disposition publishes nothing.
	if err := store.SetState(step.ID, StepRunning); err != nil {
		t.Fatalf("second SetState: %v", err)
	}
	expectNoStepEvent(t, sub)
}

// TestStepMachine_GateOffIsSilent pins the opt-in default: with the bus
// wired but the gate unarmed, SetState/SetStateWithReason behave
// byte-identically and kanban.event.step.state sees nothing.
func TestStepMachine_GateOffIsSilent(t *testing.T) {
	msgBus := bus.New(nil, nil)
	db := openStepTestDB(t)
	store, err := NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("NewStepStore: %v", err)
	}
	sub := msgBus.Subscribe("gate-off-step", kanban.TopicStepState.Name)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	store.SetKanbanEventBus(msgBus)
	// Gate NOT armed — the default.

	step := NewTaskStep("quiet-task", "no events", 0)
	if err := store.Create(step); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetState(step.ID, StepRunning); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := store.SetStateWithReason(step.ID, StepCompleted, "complete"); err != nil {
		t.Fatalf("SetStateWithReason: %v", err)
	}
	expectNoStepEvent(t, sub)
}

// TestStepMachine_NilBusSafety: gate armed, no bus set = silent no-op, no
// panic (mirrors the kanban helper contract).
func TestStepMachine_NilBusSafety(t *testing.T) {
	db := openStepTestDB(t)
	store, err := NewStepStore(db, nil)
	if err != nil {
		t.Fatalf("NewStepStore: %v", err)
	}
	store.SetKanbanEventPublishing(true)
	step := NewTaskStep("nilbus-task", "armed but silent", 0)
	if err := store.Create(step); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetState(step.ID, StepRunning); err != nil {
		t.Fatalf("SetState: %v", err)
	}
}
