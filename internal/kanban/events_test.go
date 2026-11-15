package kanban

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

func TestTopicNames(t *testing.T) {
	// C2 pins the exact topic strings — a rename here silently orphans the
	// projector subscribers (branch 03) with no compiler help.
	if TopicTaskState.Name != "kanban.event.task.state" {
		t.Errorf("TopicTaskState.Name = %q, want %q", TopicTaskState.Name, "kanban.event.task.state")
	}
	if TopicJobState.Name != "kanban.event.job.state" {
		t.Errorf("TopicJobState.Name = %q, want %q", TopicJobState.Name, "kanban.event.job.state")
	}
}

// TestStateEventJSONKeys pins the C1 wire contract: the payload decodes to
// exactly kind,id,from,to,reason,ts.
func TestStateEventJSONKeys(t *testing.T) {
	raw, err := json.Marshal(StateEvent{
		Kind: "task", ID: "task-1", From: "pending", To: "planning",
		Reason: "update-state", TS: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"kind", "id", "from", "to", "reason", "ts"} {
		if _, ok := got[key]; !ok {
			t.Errorf("StateEvent JSON missing key %q in %s", key, raw)
		}
	}
	if len(got) != 6 {
		t.Errorf("StateEvent JSON has %d keys, want 6 (identity only, C1): %s", len(got), raw)
	}
}

// TestPublishHelpers_NilBusSafety: a nil bus must be a silent no-op for both
// helpers — the daemon runs queue/registry instances without a bus in tests
// and lite paths.
func TestPublishHelpers_NilBusSafety(t *testing.T) {
	PublishTaskState(nil, StateEvent{ID: "t1", From: "pending", To: "planning"})
	PublishJobState(nil, StateEvent{ID: "j1", From: "pending", To: "claimed"})
}

// TestPublishHelpers_Defaults pins the contract-pinned default filling:
// Kind defaults to the machine name per helper, TS defaults to now(UTC), and
// caller-set values are preserved verbatim. Subscriptions are raw so the test
// owns the channel read (SubscribeT's reader goroutine would race it).
func TestPublishHelpers_Defaults(t *testing.T) {
	b := bus.New(nil, nil)
	taskSub := b.Subscribe("test-task-defaults", TopicTaskState.Name)
	t.Cleanup(func() { b.Unsubscribe(taskSub) })
	jobSub := b.Subscribe("test-job-defaults", TopicJobState.Name)
	t.Cleanup(func() { b.Unsubscribe(jobSub) })

	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	PublishTaskState(b, StateEvent{ID: "t1", From: "pending", To: "planning", Reason: "update-state", TS: fixed})
	PublishTaskState(b, StateEvent{Kind: "overridden", ID: "t2"})
	PublishJobState(b, StateEvent{ID: "j1", From: "pending", To: "claimed", Reason: "claim"})

	before := time.Now().UTC().Add(-time.Minute)

	got := drainStateEvents(t, taskSub, 2)
	if got[0].Kind != EventKindTask || got[0].ID != "t1" || !got[0].TS.Equal(fixed) {
		t.Errorf("event 0: kind=%q id=%q ts=%v; want task/t1/%v", got[0].Kind, got[0].ID, got[0].TS, fixed)
	}
	if got[1].Kind != "overridden" {
		t.Errorf("caller-set Kind must not be overwritten, got %q", got[1].Kind)
	}
	if got[1].TS.Before(before) || got[1].TS.After(time.Now().UTC().Add(time.Minute)) {
		t.Errorf("zero TS must default to now(UTC), got %v", got[1].TS)
	}

	jobs := drainStateEvents(t, jobSub, 1)
	if jobs[0].Kind != EventKindJob || jobs[0].ID != "j1" || jobs[0].Reason != "claim" {
		t.Errorf("job event: %+v", jobs[0])
	}
}

func drainStateEvents(t *testing.T, sub *bus.Subscriber, n int) []StateEvent {
	t.Helper()
	out := make([]StateEvent, 0, n)
	deadline := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				t.Fatalf("subscriber channel closed after %d/%d events", len(out), n)
			}
			var ev StateEvent
			if err := json.Unmarshal(m.Payload, &ev); err != nil {
				t.Fatalf("payload %s: %v", m.Payload, err)
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatalf("timed out after %d/%d events", len(out), n)
		}
	}
	return out
}

// TestTopicNames_Leaf0204 pins the three topics added by plan leaf 02/04
// (plan/step/goal machines) to their C2 strings.
func TestTopicNames_Leaf0204(t *testing.T) {
	if TopicPlanState.Name != "kanban.event.plan.state" {
		t.Errorf("TopicPlanState.Name = %q, want %q", TopicPlanState.Name, "kanban.event.plan.state")
	}
	if TopicStepState.Name != "kanban.event.step.state" {
		t.Errorf("TopicStepState.Name = %q, want %q", TopicStepState.Name, "kanban.event.step.state")
	}
	if TopicGoalState.Name != "kanban.event.goal.state" {
		t.Errorf("TopicGoalState.Name = %q, want %q", TopicGoalState.Name, "kanban.event.goal.state")
	}
}

// TestPublishHelpers0204_NilBusSafety: the three new helpers must be silent
// no-ops on a nil bus, same contract as PublishTaskState/PublishJobState.
func TestPublishHelpers0204_NilBusSafety(t *testing.T) {
	PublishPlanState(nil, StateEvent{ID: "p1", From: "draft", To: "pending_approval"})
	PublishStepState(nil, StateEvent{ID: "s1", From: "pending", To: "ready"})
	PublishGoalState(nil, StateEvent{ID: "g1", From: "active", To: "paused"})
}

// TestPublishHelpers0204_Defaults pins the same default-filling contract on
// the new helpers: Kind defaults to the machine name per helper, caller-set
// Kind is preserved, and the payload stays the C1 six-key shape.
func TestPublishHelpers0204_Defaults(t *testing.T) {
	b := bus.New(nil, nil)
	planSub := b.Subscribe("test-plan-defaults", TopicPlanState.Name)
	t.Cleanup(func() { b.Unsubscribe(planSub) })
	stepSub := b.Subscribe("test-step-defaults", TopicStepState.Name)
	t.Cleanup(func() { b.Unsubscribe(stepSub) })
	goalSub := b.Subscribe("test-goal-defaults", TopicGoalState.Name)
	t.Cleanup(func() { b.Unsubscribe(goalSub) })

	fixed := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	PublishPlanState(b, StateEvent{ID: "p1", From: "draft", To: "pending_approval", Reason: EventReasonSubmit, TS: fixed})
	PublishPlanState(b, StateEvent{Kind: "overridden", ID: "p2"})
	PublishStepState(b, StateEvent{ID: "s1", From: "pending", To: "ready", Reason: EventReasonStateChange})
	PublishGoalState(b, StateEvent{ID: "g1", From: "active", To: "blocked", Reason: EventReasonBlock})

	before := time.Now().UTC().Add(-time.Minute)

	plans := drainStateEvents(t, planSub, 2)
	if plans[0].Kind != EventKindPlan || plans[0].ID != "p1" || !plans[0].TS.Equal(fixed) || plans[0].Reason != EventReasonSubmit {
		t.Errorf("plan event 0: %+v", plans[0])
	}
	if plans[1].Kind != "overridden" {
		t.Errorf("caller-set Kind must not be overwritten, got %q", plans[1].Kind)
	}
	if plans[1].TS.Before(before) || plans[1].TS.After(time.Now().UTC().Add(time.Minute)) {
		t.Errorf("zero TS must default to now(UTC), got %v", plans[1].TS)
	}

	steps := drainStateEvents(t, stepSub, 1)
	if steps[0].Kind != EventKindStep || steps[0].ID != "s1" || steps[0].Reason != EventReasonStateChange {
		t.Errorf("step event: %+v", steps[0])
	}

	goals := drainStateEvents(t, goalSub, 1)
	if goals[0].Kind != EventKindGoal || goals[0].ID != "g1" || goals[0].From != "active" || goals[0].To != "blocked" {
		t.Errorf("goal event: %+v", goals[0])
	}

	// JSON shape reuse: the C1 six-key payload must be identical across all
	// five topics (a new helper adding fields would break the projector).
	for _, ev := range []StateEvent{plans[0], steps[0], goals[0]} {
		raw, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got) != 6 {
			t.Errorf("StateEvent JSON has %d keys, want 6 (identity only, C1): %s", len(got), raw)
		}
	}
}
