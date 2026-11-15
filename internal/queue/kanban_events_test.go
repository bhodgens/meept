package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/kanban"
)

// newGatedQueue returns a queue with the kanban StateEvent gate ARMED and a
// raw subscriber on the job-state topic (raw so the test owns the channel —
// SubscribeT's reader goroutine would race it).
func newGatedQueue(t *testing.T) (*PersistentQueue, *bus.Subscriber, context.Context) {
	t.Helper()
	q := newTestQueue(t)
	sub := q.bus.Subscribe("kanban-job-events-test", kanban.TopicJobState.Name)
	t.Cleanup(func() { q.bus.Unsubscribe(sub) })
	q.SetKanbanEventPublishing(true)
	return q, sub, context.Background()
}

func expectOneJobEvent(t *testing.T, sub *bus.Subscriber) kanban.StateEvent {
	t.Helper()
	select {
	case m := <-sub.Channel:
		var ev kanban.StateEvent
		if err := json.Unmarshal(m.Payload, &ev); err != nil {
			t.Fatalf("decode StateEvent: %v", err)
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("expected exactly one job StateEvent, got none")
		return kanban.StateEvent{}
	}
}

func expectNoJobEvent(t *testing.T, sub *bus.Subscriber) {
	t.Helper()
	select {
	case m := <-sub.Channel:
		t.Fatalf("unexpected job StateEvent: %s", m.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

// preStageJobState force-sets a job row (and optionally exhausts the retry
// budget) so an edge can be driven from a mid-machine state without replaying
// every upstream transition. Test-only surgery on the same DB the queue owns.
func preStageJobState(t *testing.T, q *PersistentQueue, jobID string, state JobState, exhaustBudget bool) {
	t.Helper()
	set := "state = ?"
	args := []any{string(state), jobID}
	if exhaustBudget {
		set += ", retry_count = max_retries"
	}
	if _, err := q.DB().Exec("UPDATE jobs SET "+set+" WHERE id = ?", args...); err != nil {
		t.Fatalf("pre-stage job %s to %s: %v", jobID, state, err)
	}
}

func enqueueJob(t *testing.T, q *PersistentQueue, ctx context.Context) *Job {
	t.Helper()
	job, err := NewJob(JobTypeOneOff, map[string]string{"p": "1"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	if err := q.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return job
}

// claimTo drives the job to 'claimed' (or deeper) via REAL transitions and
// returns the live claim token.
func claimTo(t *testing.T, q *PersistentQueue, ctx context.Context, jobID string, upto JobState) string {
	t.Helper()
	job, err := q.Claim(ctx, "w1", nil, "")
	if err != nil || job == nil || job.ID != jobID {
		t.Fatalf("Claim(%s): job=%v err=%v", jobID, job, err)
	}
	// Consume any other pending rows is unnecessary: one job per fresh queue.
	if upto == StateClaimed {
		return job.ClaimToken
	}
	if err := q.MarkProcessing(ctx, jobID, job.ClaimToken); err != nil {
		t.Fatalf("MarkProcessing: %v", err)
	}
	return job.ClaimToken
}

// jobEdge is one legal jobTransitions entry and the public method path that
// publishes its StateEvent.
type jobEdge struct {
	from, to JobState
	reason   string
	drive    func(t *testing.T, q *PersistentQueue, ctx context.Context, jobID string) error
}

var jobEdgePaths = []jobEdge{
	{StatePending, StateClaimed, kanban.EventReasonClaim, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		_, err := q.Claim(ctx, "w1", nil, "")
		return err
	}},
	{StatePending, StateFailed, kanban.EventReasonFail, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		return q.Fail(ctx, id, fmt.Errorf("boom"))
	}},
	{StatePending, StateDead, kanban.EventReasonFail, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		preStageJobState(t, q, id, StatePending, true) // budget exhausted -> Fail resolves dead
		return q.Fail(ctx, id, fmt.Errorf("boom"))
	}},
	{StateClaimed, StateProcessing, kanban.EventReasonProcessing, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		job, err := q.Claim(ctx, "w1", nil, "")
		if err != nil {
			return err
		}
		return q.MarkProcessing(ctx, job.ID, job.ClaimToken)
	}},
	{StateClaimed, StateCompleted, kanban.EventReasonComplete, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		claimTo(t, q, ctx, id, StateClaimed)
		return q.Complete(ctx, id, "ok")
	}},
	{StateClaimed, StateFailed, kanban.EventReasonFail, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		claimTo(t, q, ctx, id, StateClaimed)
		return q.Fail(ctx, id, fmt.Errorf("boom"))
	}},
	{StateClaimed, StatePending, kanban.EventReasonRetry, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		claimTo(t, q, ctx, id, StateClaimed)
		return q.Retry(ctx, id)
	}},
	{StateClaimed, StatePending, kanban.EventReasonRequeue, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		claimTo(t, q, ctx, id, StateClaimed)
		return q.Requeue(ctx, id, time.Now().UTC())
	}},
	{StateProcessing, StateCompleted, kanban.EventReasonComplete, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		tok := claimTo(t, q, ctx, id, StateProcessing)
		return q.CompleteAttempt(ctx, id, "ok", tok)
	}},
	{StateProcessing, StateFailed, kanban.EventReasonFail, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		tok := claimTo(t, q, ctx, id, StateProcessing)
		return q.FailAttempt(ctx, id, fmt.Errorf("boom"), tok)
	}},
	{StateProcessing, StatePending, kanban.EventReasonRequeue, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		claimTo(t, q, ctx, id, StateProcessing)
		return q.Requeue(ctx, id, time.Now().UTC())
	}},
	{StateProcessing, StateDead, kanban.EventReasonFail, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		tok := claimTo(t, q, ctx, id, StateProcessing)
		preStageJobState(t, q, id, StateProcessing, true)
		return q.FailAttempt(ctx, id, fmt.Errorf("boom"), tok)
	}},
	{StateFailed, StatePending, kanban.EventReasonRetry, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		preStageJobState(t, q, id, StateFailed, false)
		return q.Retry(ctx, id)
	}},
	{StateFailed, StateDead, kanban.EventReasonFail, func(t *testing.T, q *PersistentQueue, ctx context.Context, id string) error {
		preStageJobState(t, q, id, StateFailed, true)
		return q.Fail(ctx, id, fmt.Errorf("boom"))
	}},
}

// TestJobTransitions_AllEdgesHavePublisherPath walks jobTransitions — the
// machine authority (plan 02/02) — and asserts every legal from->to edge has a
// wired publisher path in jobEdgePaths.
func TestJobTransitions_AllEdgesHavePublisherPath(t *testing.T) {
	for from, targets := range jobTransitions {
		for _, to := range targets {
			found := false
			for _, e := range jobEdgePaths {
				if e.from == from && e.to == to {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("jobTransitions edge %s->%s has no publisher path", from, to)
			}
		}
	}
	// Terminal states map to no edges — nothing may publish from them.
	for _, terminal := range []JobState{StateCompleted, StateDead} {
		if len(jobTransitions[terminal]) != 0 {
			t.Errorf("terminal state %s has outgoing edges %v", terminal, jobTransitions[terminal])
		}
	}
}

// TestJobEdgePaths_EmitStateEvent drives every wired path on a real queue and
// asserts the exact StateEvent payload on the C2 topic.
func TestJobEdgePaths_EmitStateEvent(t *testing.T) {
	for i, e := range jobEdgePaths {
		e := e
		t.Run(fmt.Sprintf("%d:%s->%s:%s", i, e.from, e.to, e.reason), func(t *testing.T) {
			q, sub, ctx := newGatedQueue(t)
			job := enqueueJob(t, q, ctx)
			// Enqueue is not a machine edge — nothing may land before the drive.
			expectNoJobEvent(t, sub)

			if err := e.drive(t, q, ctx, job.ID); err != nil {
				t.Fatalf("drive %s->%s: %v", e.from, e.to, err)
			}
			// Paths that stage intermediate states (claim, processing)
			// legitimately publish their own edges first; the edge under
			// test is the LAST event on the topic.
			events := drainJobEventsAll(t, sub)
			if len(events) == 0 {
				t.Fatalf("edge %s->%s published no StateEvent", e.from, e.to)
			}
			ev := events[len(events)-1]
			if ev.Kind != kanban.EventKindJob || ev.ID != job.ID {
				t.Errorf("event kind/id = %q/%q, want job/%q", ev.Kind, ev.ID, job.ID)
			}
			if ev.From != string(e.from) || ev.To != string(e.to) {
				t.Errorf("event from/to = %q/%q, want %q/%q", ev.From, ev.To, e.from, e.to)
			}
			if ev.Reason != e.reason {
				t.Errorf("event reason = %q, want %q", ev.Reason, e.reason)
			}
			if ev.TS.IsZero() {
				t.Error("event TS must be filled")
			}
		})
	}
}

func drainJobEventsAll(t *testing.T, sub *bus.Subscriber) []kanban.StateEvent {
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

// TestJobKanbanEvents_GateOffIsSilent: default (gate off) drives the same
// transitions and NOTHING lands on the topic.
func TestJobKanbanEvents_GateOffIsSilent(t *testing.T) {
	q := newTestQueue(t) // gate NOT armed — the production default
	sub := q.bus.Subscribe("kanban-gate-off-test", kanban.TopicJobState.Name)
	t.Cleanup(func() { q.bus.Unsubscribe(sub) })
	ctx := context.Background()

	job := enqueueJob(t, q, ctx)
	claimed, err := q.Claim(ctx, "w1", nil, "")
	if err != nil || claimed == nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := q.MarkProcessing(ctx, job.ID, claimed.ClaimToken); err != nil {
		t.Fatalf("MarkProcessing: %v", err)
	}
	if err := q.CompleteAttempt(ctx, job.ID, "done", claimed.ClaimToken); err != nil {
		t.Fatalf("CompleteAttempt: %v", err)
	}
	expectNoJobEvent(t, sub)
}

// TestJobTerminalAndIdempotentPaths_PublishNothing: superseded completes,
// idempotent retries, and terminal no-ops must emit no StateEvent.
func TestJobTerminalAndIdempotentPaths_PublishNothing(t *testing.T) {
	q, sub, ctx := newGatedQueue(t)
	job := enqueueJob(t, q, ctx)
	claimed, err := q.Claim(ctx, "w1", nil, "")
	if err != nil || claimed == nil {
		t.Fatalf("Claim: %v", err)
	}
	expectOneJobEvent(t, sub) // pending->claimed

	// Idempotent re-MarkProcessing from processing publishes nothing.
	if err := q.MarkProcessing(ctx, job.ID, claimed.ClaimToken); err != nil {
		t.Fatalf("first MarkProcessing: %v", err)
	}
	expectOneJobEvent(t, sub) // claimed->processing
	if err := q.MarkProcessing(ctx, job.ID, claimed.ClaimToken); err != nil {
		t.Fatalf("idempotent MarkProcessing: %v", err)
	}
	expectNoJobEvent(t, sub)

	if err := q.CompleteAttempt(ctx, job.ID, "ok", claimed.ClaimToken); err != nil {
		t.Fatalf("CompleteAttempt: %v", err)
	}
	expectOneJobEvent(t, sub) // processing->completed

	// Idempotent retry of the SAME completion: no second event.
	if err := q.CompleteAttempt(ctx, job.ID, "ok", claimed.ClaimToken); err != nil {
		t.Fatalf("idempotent CompleteAttempt: %v", err)
	}
	expectNoJobEvent(t, sub)

	// Terminal no-ops on a completed row.
	if err := q.Fail(ctx, job.ID, fmt.Errorf("late")); err == nil {
		t.Fatal("Fail on completed job must be refused")
	}
	if err := q.Retry(ctx, job.ID); err == nil {
		t.Fatal("Retry on completed job must be refused")
	}
	if err := q.Requeue(ctx, job.ID, time.Now().UTC()); err == nil {
		t.Fatal("Requeue on completed job must be refused")
	}
	expectNoJobEvent(t, sub)
}

// TestSetKanbanEventPublishing_ZeroValueSafe covers the setters_test.go
// pattern: the setter works on a zero-value instance (mu is ready-to-use).
func TestSetKanbanEventPublishing_ZeroValueSafe(t *testing.T) {
	pq := &PersistentQueue{}
	pq.SetKanbanEventPublishing(true)
	pq.SetKanbanEventPublishing(false)
	// publishJobState on a zero queue with gate on but nil bus must be a
	// silent no-op (kanban publishers are nil-bus safe).
	pq.SetKanbanEventPublishing(true)
	pq.publishJobState("j1", "pending", "claimed", kanban.EventReasonClaim)
}
