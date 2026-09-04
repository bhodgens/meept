package agent

// Tests for honest task completion (leaf 02 of chat-dispatch-ux): a task
// with any failed step must finalize as StateFailed, with the task.completed
// payload carrying "status":"failed" and the step error text as "result".

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// TestTacticalScheduler_FailedStepFailsTask drives OnJobCompleted for the
// last surviving step of a task where another step already failed. The task
// must finalize as StateFailed and the task.completed payload must name the
// failure instead of pretending success.
func TestTacticalScheduler_FailedStepFailsTask(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	completedSub := msgBus.Subscribe("test-honest-completion", "task.completed")
	defer msgBus.Unsubscribe(completedSub)

	parentTask := task.NewTask("honest-completion-test", "task with a failed step")
	parentTask.TotalJobs = 2
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Step 0: already failed, with the error text the job processor stored.
	failedStep := task.NewTaskStep(parentTask.ID, "write the reminder file", 0)
	failedStep.State = task.StepFailed
	failedStep.Result = "error: file_write failed: no path specified"
	if err := ts.stepStore.Create(failedStep); err != nil {
		t.Fatalf("failed to create failed step: %v", err)
	}

	// Step 1: completes successfully — drives the task-finalization path.
	okStep := task.NewTaskStep(parentTask.ID, "report status", 1)
	if err := ts.stepStore.Create(okStep); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	if err := ts.stepStore.SetJobID(okStep.ID, "job-honest-1"); err != nil {
		t.Fatalf("failed to set job ID: %v", err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "status reported"})

	if err := ts.OnJobCompleted(t.Context(), "job-honest-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	updatedTask, err := ts.taskStore.GetByID(parentTask.ID)
	if err != nil {
		t.Fatalf("failed to get task after completion: %v", err)
	}
	if updatedTask.State != task.StateFailed {
		t.Fatalf("task state = %q, want %q (task with a failed step completed as success)", updatedTask.State, task.StateFailed)
	}

	select {
	case msg := <-completedSub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to unmarshal task.completed event: %v", err)
		}
		if payload["task_id"] != parentTask.ID {
			t.Errorf("task.completed task_id = %v, want %s", payload["task_id"], parentTask.ID)
		}
		if payload["status"] != "failed" {
			t.Errorf("task.completed status = %v, want \"failed\"", payload["status"])
		}
		result, _ := payload["result"].(string)
		if !strings.Contains(result, "file_write failed") {
			t.Errorf("task.completed result = %q, want the step error text", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.completed event")
	}
}

// TestTacticalScheduler_SuccessTaskStillCompletes guards the flip side: a
// task with no failed steps must still finalize as StateCompleted with
// "status":"completed" — the honest-completion change must not break the
// success path its subscribers depend on.
func TestTacticalScheduler_SuccessTaskStillCompletes(t *testing.T) {
	ts, msgBus, cleanup := newTacticalTestSetup(t)
	defer cleanup()

	completedSub := msgBus.Subscribe("test-honest-completion-ok", "task.completed")
	defer msgBus.Unsubscribe(completedSub)

	parentTask := task.NewTask("honest-success-test", "task that succeeds")
	parentTask.TotalJobs = 1
	parentTask.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(parentTask); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	okStep := task.NewTaskStep(parentTask.ID, "do the work", 0)
	if err := ts.stepStore.Create(okStep); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	if err := ts.stepStore.SetJobID(okStep.ID, "job-honest-ok-1"); err != nil {
		t.Fatalf("failed to set job ID: %v", err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"success": true, "result": "work done"})

	if err := ts.OnJobCompleted(t.Context(), "job-honest-ok-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	updatedTask, err := ts.taskStore.GetByID(parentTask.ID)
	if err != nil {
		t.Fatalf("failed to get task after completion: %v", err)
	}
	if updatedTask.State != task.StateCompleted {
		t.Fatalf("task state = %q, want %q", updatedTask.State, task.StateCompleted)
	}

	select {
	case msg := <-completedSub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to unmarshal task.completed event: %v", err)
		}
		if payload["status"] != "completed" {
			t.Errorf("task.completed status = %v, want \"completed\"", payload["status"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.completed event")
	}
}
