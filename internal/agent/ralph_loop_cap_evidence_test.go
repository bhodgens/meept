package agent

// Regression pins for the 2026-09-12 bughunt wave, group 1, ralph-loop half:
//
//	F5     - the replan cap must evaluate the FINAL granted attempt's
//	         evidence before failing the task; a task that finally produced
//	         sufficient evidence on the last attempt must complete
//	F6     - failTaskAtCap must not re-terminalize a task another path
//	         already terminalized (a late step could otherwise resurrect a
//	         capped-failed task and disarm the counter)
//	F39    - the task.failed payload must carry the key set its only
//	         subscriber (ChatHandler.handleTaskFailed) decodes, or the
//	         user-facing failure renders with an empty name and error

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
)

// newRalphCapFixture builds a RalphLoop over a temp task+step store with a
// live bus, returning everything the cap pins assert against.
func newRalphCapFixture(t *testing.T) (rl *RalphLoop, messageBus *bus.MessageBus, taskStore *task.Store, stepStore *task.StepStore) {
	t.Helper()

	taskStore, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}
	t.Cleanup(func() { taskStore.Close() })

	stepStore = taskStore.StepStore()
	logger := slogDiscardLogger()
	messageBus = bus.New(nil, logger)
	rl = NewRalphLoop(DefaultRalphLoopConfig(), nil, taskStore, stepStore, nil, messageBus, logger)
	return rl, messageBus, taskStore, stepStore
}

// driveToReplanCap consumes the granted replan attempts with evidence that
// does not substantiate the task, leaving the iteration counter at the cap.
func driveToReplanCap(t *testing.T, rl *RalphLoop, taskID string) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < rl.config.MaxIterations; i++ {
		result, _ := json.Marshal(map[string]any{
			"success":  true,
			"result":   "done",
			"evidence": []string{"unrelated evidence matching none of the key terms"},
		})
		isComplete, evidence, needsReplan := rl.CheckCompletion(ctx, taskID, result)
		if isComplete || !needsReplan {
			t.Fatalf("iteration %d: expected needsReplan=true/isComplete=false, got isComplete=%v needsReplan=%v evidence=%v",
				i+1, isComplete, needsReplan, evidence)
		}
		if err := rl.TriggerReplan(ctx, taskID, nil); err != nil {
			t.Fatalf("iteration %d: TriggerReplan: %v", i+1, err)
		}
	}
}

// TestRalphLoop_CapEvaluatesFinalAttemptEvidence pins F5: at the cap, a
// final attempt whose evidence DOES substantiate the task must be reported
// complete (the old code failed the task before parsing the result).
func TestRalphLoop_CapEvaluatesFinalAttemptEvidence(t *testing.T) {
	rl, messageBus, taskStore, _ := newRalphCapFixture(t)

	failSub := messageBus.Subscribe("ralph-final-attempt", "task.failed")
	defer messageBus.Unsubscribe(failSub)

	tk := task.NewTask("cap-final-attempt", "write answer file")
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	driveToReplanCap(t, rl, tk.ID)

	// The final granted attempt produced evidence naming the task's terms.
	result, _ := json.Marshal(map[string]any{
		"success":  true,
		"result":   "answer file written to the working directory",
		"evidence": []string{"answer file written to the working directory"},
	})
	isComplete, evidence, needsReplan := rl.CheckCompletion(context.Background(), tk.ID, result)
	if !isComplete {
		t.Errorf("isComplete = false, want true (the final attempt's evidence was sufficient)")
	}
	if needsReplan {
		t.Error("needsReplan = true, want false at the cap with sufficient evidence")
	}
	if len(evidence) == 0 {
		t.Error("evidence = empty, want the final attempt's evidence")
	}

	got, err := taskStore.GetByID(tk.ID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State.IsTerminal() {
		t.Fatalf("task state = %q, want non-terminal (the cap must not fail a task that succeeded on its last attempt)", got.State)
	}

	select {
	case msg := <-failSub.Channel:
		t.Fatalf("unexpected task.failed after a successful final attempt: %s", string(msg.Payload))
	case <-time.After(300 * time.Millisecond):
	}
}

// TestRalphLoop_CapFailsWhenEvidenceStillInsufficient is the flip side: with
// the cap reached and the final attempt's evidence still insufficient, the
// task must fail (the F5 fix must not turn the cap into a no-op).
func TestRalphLoop_CapFailsWhenEvidenceStillInsufficient(t *testing.T) {
	rl, _, taskStore, _ := newRalphCapFixture(t)

	tk := task.NewTask("cap-insufficient", "write answer file")
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	driveToReplanCap(t, rl, tk.ID)

	result, _ := json.Marshal(map[string]any{
		"success":  true,
		"result":   "done",
		"evidence": []string{"nothing to do with it"},
	})
	isComplete, _, needsReplan := rl.CheckCompletion(context.Background(), tk.ID, result)
	if isComplete || needsReplan {
		t.Fatalf("CheckCompletion = (%v, %v), want (false, false) at the cap", isComplete, needsReplan)
	}
	got, err := taskStore.GetByID(tk.ID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("task state = %q, want failed", got.State)
	}
}

// TestRalphLoop_FailAtCapDoesNotRevertTerminalTask pins F6: a task another
// path already terminalized must be left alone (no state flip, no second
// task.failed), or the orchestrator's TaskOutcome resets the counter.
func TestRalphLoop_FailAtCapDoesNotRevertTerminalTask(t *testing.T) {
	rl, messageBus, taskStore, _ := newRalphCapFixture(t)

	failSub := messageBus.Subscribe("ralph-terminal-guard", "task.failed")
	defer messageBus.Unsubscribe(failSub)

	tk := task.NewTask("already completed task", "terminal guard pin")
	tk.State = task.StateCompleted
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	rl.failTaskAtCap(tk.ID, "max ralph loop iterations reached without sufficient evidence")

	got, err := taskStore.GetByID(tk.ID)
	if err != nil || got == nil {
		t.Fatalf("get task: %v", err)
	}
	if got.State != task.StateCompleted {
		t.Fatalf("task state = %q, want completed (a terminal task must not be re-terminalized)", got.State)
	}

	select {
	case msg := <-failSub.Channel:
		t.Fatalf("unexpected task.failed for an already-terminal task: %s", string(msg.Payload))
	case <-time.After(300 * time.Millisecond):
	}
}

// TestRalphLoop_FailAtCapPayloadMatchesSubscriber pins F39: the task.failed
// payload must carry the keys ChatHandler.handleTaskFailed decodes
// (name/error/failed_jobs/completed_jobs/total_jobs/linked_sessions), or the
// user-facing failure renders as "## task failed: \n**error:**".
func TestRalphLoop_FailAtCapPayloadMatchesSubscriber(t *testing.T) {
	rl, messageBus, taskStore, stepStore := newRalphCapFixture(t)

	failSub := messageBus.Subscribe("ralph-payload-keys", "task.failed")
	defer messageBus.Unsubscribe(failSub)

	tk := task.NewTask("payload keys task", "F39 pin")
	tk.TotalJobs = 3
	tk.CompletedJobs = 1
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	// LinkedSessions is read back through the session_tasks join, so link it
	// the way the daemon does.
	if err := taskStore.LinkSession(tk.ID, "session-payload-1"); err != nil {
		t.Fatalf("link session: %v", err)
	}
	// One failed step so failed_jobs is a real count, not a constant.
	failedStep := task.NewTaskStep(tk.ID, "step that failed", 0)
	if err := stepStore.Create(failedStep); err != nil {
		t.Fatalf("create step: %v", err)
	}
	if err := stepStore.SetState(failedStep.ID, task.StepFailed); err != nil {
		t.Fatalf("fail step: %v", err)
	}

	rl.failTaskAtCap(tk.ID, "max ralph loop iterations reached without sufficient evidence")

	select {
	case msg := <-failSub.Channel:
		// Mirrors ChatHandler.handleTaskFailed's decode struct — the point
		// of the pin is that the subscriber can read these keys.
		var payload struct {
			TaskID         string   `json:"task_id"`
			Name           string   `json:"name"`
			FailedJobs     int      `json:"failed_jobs"`
			CompletedJobs  int      `json:"completed_jobs"`
			TotalJobs      int      `json:"total_jobs"`
			LinkedSessions []string `json:"linked_sessions"`
			Error          string   `json:"error,omitempty"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal task.failed payload: %v", err)
		}
		if payload.TaskID != tk.ID {
			t.Errorf("task_id = %q, want %q", payload.TaskID, tk.ID)
		}
		if payload.Name != tk.Name {
			t.Errorf("name = %q, want %q (empty name renders '## task failed: ')", payload.Name, tk.Name)
		}
		if payload.Error == "" {
			t.Error("error is empty, want the cap reason (the user-facing failure renders no error text)")
		}
		if payload.FailedJobs != 1 {
			t.Errorf("failed_jobs = %d, want 1", payload.FailedJobs)
		}
		if payload.CompletedJobs != tk.CompletedJobs || payload.TotalJobs != tk.TotalJobs {
			t.Errorf("progress = %d/%d, want %d/%d", payload.CompletedJobs, payload.TotalJobs, tk.CompletedJobs, tk.TotalJobs)
		}
		if len(payload.LinkedSessions) != 1 || payload.LinkedSessions[0] != "session-payload-1" {
			t.Errorf("linked_sessions = %v, want [session-payload-1]", payload.LinkedSessions)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.failed")
	}
}
