package agent

// B2 stale-completion guard tests for OnJobCompleted: a step whose queue job
// was requeued (same job ID, job back in pending/failed state) must NOT have
// a stale completion event processed as a fresh one. The guard discriminates
// on the job's state, NOT the step state — StepScheduled is the de-facto
// in-flight state (dispatch sets it; StepRunning is never set in production),
// so a step-state guard would skip every legitimate completion.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
)

// TestOnJobCompleted_StaleCompletionRejected drives the entry guard: a step
// whose job_id points at a requeued (pending) job — the B2 shape after
// queue-level Retry/Requeue — gets no result write, no state transition, and
// no task events when a stale completion arrives for the old job ID.
func TestOnJobCompleted_StaleCompletionRejected(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
			ts, msgBus, cleanup := newTacticalTestSetup(t)
			defer cleanup()

			// Real queue so the guard can read the job's true state.
			q, err := queue.NewPersistentQueue(t.TempDir()+"/b2-tactical.db", msgBus, nil)
			if err != nil {
				t.Fatalf("NewPersistentQueue: %v", err)
			}
			defer q.Close()
			ts.queue = q

			progressSub := msgBus.Subscribe("test-b2-stale-progress", "task.progress")
			defer msgBus.Unsubscribe(progressSub)
			completedSub := msgBus.Subscribe("test-b2-stale-completed", "task.completed")
			defer msgBus.Unsubscribe(completedSub)

			parentTask := task.NewTask("b2-stale-test", "stale completion guard")
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

			// Claim the job exactly as dispatch would (this is the only
			// legal completion path; a fresh pending job has no worker).
			if err := q.Enqueue(t.Context(), mustB2Job(t, step)); err != nil {
				t.Fatalf("Enqueue: %v", err)
			}
			claimed, err := q.Claim(t.Context(), "worker-b2", nil, "")
			if err != nil {
				t.Fatalf("Claim: %v", err)
			}
			if err := ts.stepStore.SetJobID(step.ID, claimed.ID); err != nil {
				t.Fatalf("SetJobID: %v", err)
			}

			// Requeue/Fail the job: it keeps the SAME ID but leaves the
			// claimable set — any completion event for it is stale.
			if err := tc.setup(q, claimed.ID); err != nil {
				t.Fatalf("setup(%s): %v", tc.name, err)
			}

			resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "STALE attempt-1 result"})
			if err := ts.OnJobCompleted(t.Context(), claimed.ID, resultJSON); err != nil {
				t.Fatalf("OnJobCompleted must swallow stale completions, got: %v", err)
			}

			// Step result unchanged, no state transition recorded.
			persisted, err := ts.stepStore.GetByID(step.ID)
			if err != nil || persisted == nil {
				t.Fatalf("re-read step: %v", err)
			}
			if persisted.Result != "" {
				t.Errorf("step result = %q, want empty (stale result must not be stored)", persisted.Result)
			}
			if persisted.State != task.StepScheduled {
				t.Errorf("step state = %q, want %q (unchanged)", persisted.State, task.StepScheduled)
			}

			// Parent task untouched: no progress/completed events, counters intact.
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
					t.Fatalf("unexpected %s event after stale completion: %s", name, msg.Payload)
				case <-time.After(300 * time.Millisecond):
					// expected: no events
				}
			}
		})
	}
}

// TestOnJobCompleted_FreshCompletionStillProcessed: the control case — a
// claimed (in-flight) job's completion MUST still be fully processed, proving
// the guard discriminates on job state rather than blocking all completions.
func TestOnJobCompleted_FreshCompletionStillProcessed(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	q, err := queue.NewPersistentQueue(t.TempDir()+"/b2-tactical-ok.db", msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue: %v", err)
	}
	defer q.Close()
	ts.queue = q

	parentTask := task.NewTask("b2-fresh-test", "fresh completion control")
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
	claimed, err := q.Claim(t.Context(), "worker-b2", nil, "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, claimed.ID); err != nil {
		t.Fatalf("SetJobID: %v", err)
	}

	// Mirror the production sequence: the worker completes the job (state
	// -> completed) BEFORE the bus event fires OnJobCompleted.
	if err := q.Complete(t.Context(), claimed.ID, map[string]any{"success": true}); err != nil {
		t.Fatalf("q.Complete: %v", err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "fresh work done"})
	if err := ts.OnJobCompleted(t.Context(), claimed.ID, resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("re-read step: %v", err)
	}
	if persisted.State != task.StepCompleted && persisted.State != task.StepApproved {
		t.Errorf("step state = %q, want completed/approved", persisted.State)
	}

	gotTask, err := ts.taskStore.GetByID(parentTask.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.State != task.StateCompleted {
		t.Errorf("task state = %q, want %q", gotTask.State, task.StateCompleted)
	}
}

// errStaleBoom and mustB2Job are file-local helpers.
var errStaleBoom = errorStale{}

type errorStale struct{}

func (errorStale) Error() string { return "boom" }

func mustB2Job(t *testing.T, step *task.TaskStep) *queue.Job {
	t.Helper()
	job, err := queue.NewJob(queue.JobTypeProjectTask, map[string]string{"prompt": "b2 step"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}
	job.WithTaskID(step.TaskID).WithAgentID(step.AgentID)
	return job
}
