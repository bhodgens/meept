package agent

// H5 of the 2026-10-05 bughunt: the stale-completion guard discriminated on
// queue-JOB STATE alone. Once a cluster reclaim can reset a still-executing job
// to 'pending' and another worker re-claims it, state reads 'claimed' —
// "accept" — while the in-flight worker holds the OLDER attempt. Its completion
// was then processed as fresh: result written, step terminalized, task
// finalized around a result the queue had already superseded, and the step never
// terminalized on the real attempt.
//
// The guard now probes an optional completionFreshChecker capability. These pins
// hold the contract under BOTH shapes:
//   - attempt-aware queue (implements CompletionIsFresh): the QUEUE owns the
//     verdict, including the superseded-attempt case state cannot express;
//   - state-only queue (no such method): the original state test applies;
// and in both, the ORIGINAL bug stays blocked — a requeued or failed job's
// attempt-1 completion is never processed as fresh.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
)

// attemptAwareQueue wraps a real PersistentQueue and adds the optional
// completionFreshChecker capability, standing in for a queue that has grown
// attempt identity (claim token / epoch). It records the token it was asked
// about so the test can prove the guard CONSULTED the queue's predicate rather
// than re-implementing it.
type attemptAwareQueue struct {
	queue.Queue

	mu           sync.Mutex
	askedTokens  []string
	staleVerdict bool
	unreadable   bool
}

func (a *attemptAwareQueue) CompletionIsFresh(_ context.Context, _ string, claimToken string) (bool, bool) {
	a.mu.Lock()
	a.askedTokens = append(a.askedTokens, claimToken)
	a.mu.Unlock()
	if a.unreadable {
		return false, false // ok=false -> guard must fail OPEN
	}
	return !a.staleVerdict, true
}

// lastToken is the token the guard last asked the queue about. It is the pin
// for H2's second leg: the guard must present the token carried by the
// queue.job.completed event, NOT the empty token that degraded the attempt
// question to the state-only predicate.
func (a *attemptAwareQueue) lastToken() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.askedTokens) == 0 {
		return "<never consulted>"
	}
	return a.askedTokens[len(a.askedTokens)-1]
}

func (a *attemptAwareQueue) consulted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.askedTokens) > 0
}

// TestOnJobCompleted_AttemptAwareQueueOwnsTheFreshnessVerdict: with an
// attempt-aware queue reporting the attempt SUPERSEDED (job re-claimed under a
// newer attempt after a reclaim), the completion must be dropped — even though
// the job's state is 'claimed', which the old state-only guard accepted. That
// is the H5 bug itself.
func TestOnJobCompleted_AttemptAwareQueueOwnsTheFreshnessVerdict(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	q, err := queue.NewPersistentQueue(t.TempDir()+"/h5-attempt.db", msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	defer q.Close()

	// The real queue holds the job in 'claimed' (the reclaim + re-claim shape).
	parentTask := task.NewTask("h5-attempt", "superseded attempt")
	parentTask.TotalJobs = 1
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("create task: %v", err)
	}
	step := task.NewTaskStep(parentTask.ID, "do the work", 0)
	step.State = task.StepScheduled
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := q.Enqueue(t.Context(), mustB2Job(t, step)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := q.Claim(t.Context(), "worker-h5", nil, "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, claimed.ID); err != nil {
		t.Fatalf("SetJobID: %v", err)
	}

	// Sanity: the job really is in a state the OLD guard accepted. Without
	// this the test would pass trivially on the state test, not on the attempt
	// check.
	live, err := q.Get(t.Context(), claimed.ID)
	if err != nil || live == nil {
		t.Fatalf("Get job: %v", err)
	}
	if live.State != queue.StateClaimed {
		t.Fatalf("fixture precondition: job state = %q, want %q (the old guard accepts this)",
			live.State, queue.StateClaimed)
	}

	// Now the attempt-aware wrapper says this attempt was SUPERSEDED.
	wrapper := &attemptAwareQueue{Queue: q, staleVerdict: true}
	ts.queue = wrapper

	progressSub := msgBus.Subscribe("test-h5-attempt-progress", "task.progress")
	defer msgBus.Unsubscribe(progressSub)
	completedSub := msgBus.Subscribe("test-h5-attempt-completed", "task.completed")
	defer msgBus.Unsubscribe(completedSub)

	resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "SUPERSEDED attempt-1 result"})
	if err := ts.OnJobCompleted(t.Context(), claimed.ID, resultJSON); err != nil {
		t.Fatalf("OnJobCompleted must swallow a superseded completion, got: %v", err)
	}

	if !wrapper.consulted() {
		t.Fatal("the guard never consulted the attempt-aware queue predicate")
	}
	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("re-read step: %v", err)
	}
	if persisted.Result != "" {
		t.Errorf("step result = %q, want empty (a superseded attempt's result must not be stored)", persisted.Result)
	}
	if persisted.State == task.StepCompleted || persisted.State == task.StepApproved {
		t.Errorf("step state = %q, want not terminal (superseded attempt must not terminalize the step)", persisted.State)
	}
	gotTask, err := ts.taskStore.GetByID(parentTask.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.CompletedJobs != 0 {
		t.Errorf("task CompletedJobs = %d, want 0", gotTask.CompletedJobs)
	}
	for name, sub := range map[string]*bus.Subscriber{"task.progress": progressSub, "task.completed": completedSub} {
		select {
		case msg := <-sub.Channel:
			t.Fatalf("unexpected %s event after a superseded completion: %s", name, msg.Payload)
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// TestOnJobCompleted_AttemptAwareQueueFailsOpenOnUnreadableJob: ok=false (the
// job could not be read — dead row, store error) must FAIL OPEN. Dead jobs
// cannot be completed, and the pre-existing behaviour was to fall through; a
// guard that dropped these would strand steps for jobs whose rows are gone.
func TestOnJobCompleted_AttemptAwareQueueFailsOpenOnUnreadableJob(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	q, err := queue.NewPersistentQueue(t.TempDir()+"/h5-open.db", msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	defer q.Close()

	parentTask := task.NewTask("h5-open", "unreadable job")
	parentTask.TotalJobs = 1
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("create task: %v", err)
	}
	step := task.NewTaskStep(parentTask.ID, "do the work", 0)
	step.State = task.StepScheduled
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := q.Enqueue(t.Context(), mustB2Job(t, step)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := q.Claim(t.Context(), "worker-h5", nil, "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := q.Complete(t.Context(), claimed.ID, map[string]any{"success": true}); err != nil {
		t.Fatalf("q.Complete: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, claimed.ID); err != nil {
		t.Fatalf("SetJobID: %v", err)
	}

	ts.queue = &attemptAwareQueue{Queue: q, unreadable: true}

	resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "work done"})
	if err := ts.OnJobCompleted(t.Context(), claimed.ID, resultJSON); err != nil {
		t.Fatalf("OnJobCompleted must fail open on an unreadable job, got: %v", err)
	}
	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("re-read step: %v", err)
	}
	if persisted.State != task.StepCompleted && persisted.State != task.StepApproved {
		t.Errorf("step state = %q, want completed/approved (the guard must fail open)", persisted.State)
	}
}

// TestOnJobCompleted_OriginalBugStaysBlockedUnderBothLayers is the regression
// anchor for B2: a requeued or failed job's attempt-1 completion must never be
// processed as fresh — under the attempt-aware layer AND under the state-only
// layer. The audit's finding was that a guard fix must not come at the cost of
// the property that motivated the guard.
func TestOnJobCompleted_OriginalBugStaysBlockedUnderBothLayers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(q *queue.PersistentQueue, jobID string) error
	}{
		{"requeued to pending", func(q *queue.PersistentQueue, jobID string) error {
			return q.Requeue(t.Context(), jobID, time.Now().UTC().Add(time.Minute))
		}},
		{"failed awaiting retry", func(q *queue.PersistentQueue, jobID string) error {
			return q.Fail(t.Context(), jobID, errStaleBoom)
		}},
	} {
		for _, layer := range []string{"attempt-aware", "state-only"} {
			t.Run(tc.name+"/"+layer, func(t *testing.T) {
				ts, msgBus, cleanup := newTacticalTestSetup(t)
				defer cleanup()

				q, err := queue.NewPersistentQueue(t.TempDir()+"/h5-both.db", msgBus, nil)
				if err != nil {
					t.Fatalf("NewPersistentQueue: %v", err)
				}
				defer q.Close()

				parentTask := task.NewTask("h5-both", "both layers")
				parentTask.TotalJobs = 1
				parentTask.SetState(task.StateExecuting)
				if err := ts.taskStore.Create(parentTask); err != nil {
					t.Fatalf("create task: %v", err)
				}
				step := task.NewTaskStep(parentTask.ID, "do the work", 0)
				step.State = task.StepScheduled
				if err := ts.stepStore.Create(step); err != nil {
					t.Fatalf("create step: %v", err)
				}
				if err := q.Enqueue(t.Context(), mustB2Job(t, step)); err != nil {
					t.Fatalf("Enqueue: %v", err)
				}
				claimed, err := q.Claim(t.Context(), "worker-h5", nil, "")
				if err != nil {
					t.Fatalf("Claim: %v", err)
				}
				if err := ts.stepStore.SetJobID(step.ID, claimed.ID); err != nil {
					t.Fatalf("SetJobID: %v", err)
				}
				if err := tc.setup(q, claimed.ID); err != nil {
					t.Fatalf("setup(%s): %v", tc.name, err)
				}

				if layer == "attempt-aware" {
					// An attempt-aware queue must ALSO reject this: the job
					// is pending/failed, which no attempt accepts.
					wrapper := &attemptAwareQueue{Queue: q}
					ts.queue = attemptStateAware{attemptAwareQueue: wrapper}
				} else {
					ts.queue = q // plain state-only queue
				}

				resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "STALE attempt-1 result"})
				if err := ts.OnJobCompleted(t.Context(), claimed.ID, resultJSON); err != nil {
					t.Fatalf("OnJobCompleted must swallow stale completions, got: %v", err)
				}

				persisted, err := ts.stepStore.GetByID(step.ID)
				if err != nil || persisted == nil {
					t.Fatalf("re-read step: %v", err)
				}
				if persisted.Result != "" {
					t.Errorf("step result = %q, want empty", persisted.Result)
				}
				if persisted.State != task.StepScheduled {
					t.Errorf("step state = %q, want %q (unchanged)", persisted.State, task.StepScheduled)
				}
			})
		}
	}
}

// attemptStateAware is a queue whose attempt-aware predicate derives its
// verdict from the REAL job state, mirroring what the queue layer does: a
// pending/failed job is stale for any attempt.
type attemptStateAware struct {
	*attemptAwareQueue
}

func (a attemptStateAware) CompletionIsFresh(ctx context.Context, jobID, claimToken string) (bool, bool) {
	a.mu.Lock()
	a.askedTokens = append(a.askedTokens, claimToken)
	a.mu.Unlock()
	job, err := a.Get(ctx, jobID)
	if err != nil || job == nil {
		return false, false
	}
	switch job.State {
	case queue.StateClaimed, queue.StateProcessing, queue.StateCompleted:
		return true, true
	default:
		return false, true
	}
}


// TestOnJobCompleted_GuardPresentsTheEventsClaimToken pins H2's second leg.
//
// The wave threaded `claim_token` through the queue.job.completed event but
// never proved the guard USES it: it passed "" before, which
// Store.CompletionIsFresh treats as the state-only predicate, so the guard could
// never distinguish a superseded attempt even with the forwarder in place. This
// asserts the token reaching the predicate is the one the event carried.
//
// The `attemptAwareQueue` double records what it was asked, so the assertion is
// on the value that crossed the boundary — not on a fixture constant.
func TestOnJobCompleted_GuardPresentsTheEventsClaimToken(t *testing.T) {
	const eventToken = "claim-token-from-the-event"

	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	q, err := queue.NewPersistentQueue(t.TempDir()+"/h5-token.db", msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	defer q.Close()

	wrapper := &attemptAwareQueue{Queue: q, staleVerdict: false}
	ts.queue = wrapper

	parentTask := task.NewTask("h5-token", "token plumbing")
	parentTask.TotalJobs = 1
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("create task: %v", err)
	}
	step := task.NewTaskStep(parentTask.ID, "do the work", 0)
	step.State = task.StepScheduled
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := q.Enqueue(t.Context(), mustB2Job(t, step)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := q.Claim(t.Context(), "worker-h5-tok", nil, "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, claimed.ID); err != nil {
		t.Fatalf("SetJobID: %v", err)
	}

	resultJSON, _ := json.Marshal(map[string]string{"result": "done"})
	if err := ts.OnJobCompleted(t.Context(), claimed.ID, resultJSON, eventToken); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	if got := wrapper.lastToken(); got != eventToken {
		t.Errorf("guard asked the queue about token %q, want the event's %q — "+
			"an empty token degrades the attempt question to state-only (bughunt H2)",
			got, eventToken)
	}
}
