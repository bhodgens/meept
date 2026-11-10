package agent

// Producer pins for the task.step_failed seam (statemachine-remediation
// follow-on to a94995c8): every site that terminalizes a step as FAILED must
// publish task.step_failed with the same payload keys the task.step_completed
// mirror carries (task_id, step_id, state=failed, result = the failure-text
// analog), so the plan handler's OnStepFailed (PhaseFailed writer, audit M-6)
// sees step failures in production.
//
//	Site 1: terminalizeStepFailedAfterFilter (filter-retry exhaustion)
//	Site 2: the completion-flow publish (validation-retry exhaustion falls
//	        through with the step already terminal; stepEventState=failed)

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
)

// drainStepFailed waits for one task.step_failed event on the subscription
// and decodes its payload. Fails the test if none arrives.
func drainStepFailed(t *testing.T, sub *bus.Subscriber) map[string]any {
	t.Helper()
	select {
	case msg, ok := <-sub.Channel:
		if !ok {
			t.Fatalf("task.step_failed subscription closed")
		}
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("decode task.step_failed payload %s: %v", msg.Payload, err)
		}
		return payload
	case <-time.After(5 * time.Second):
		t.Fatalf("no task.step_failed event published")
	}
	return nil
}

// TestTacticalStepFailedPublishedOnFilterExhaustion pins site 1: the
// filter-exhaustion terminalization emits task.step_failed alongside its
// task.step_completed (state=failed) terminal event.
func TestTacticalStepFailedPublishedOnFilterExhaustion(t *testing.T) {
	trace := &traceRecorder{alwaysApplies: true}
	ts, _, msgBus, _, cleanup := newFilterTestScheduler(t, nil)
	defer cleanup()
	ts.filterRetryLimiter = fixedLimiter{n: 1}
	ts.SetFilterChain(validator.NewFilterChain([]validator.OutputFilter{
		&recordingFilter{name: "json_format", trace: trace, fail: "language mismatch lang=de confidence=0.97"},
	}, 2))

	stepFailedSub := msgBus.Subscribe("step-failed-pin-filter", "task.step_failed")
	defer msgBus.Unsubscribe(stepFailedSub)

	parent := newFilterTestTask(t, ts, "step-failed-publish-filter", 1)
	step := newFilterTestStep(t, ts, parent.ID, "job-sfp-1", "shell", `{"broken":tru}`)
	step.FilterRetryCount = 1
	step.FilterError = "prior rejection"
	if err := ts.stepStore.Update(step); err != nil {
		t.Fatalf("seed consumed retry: %v", err)
	}
	if err := ts.stepStore.SetJobID(step.ID, "job-sfp-1"); err != nil {
		t.Fatalf("restore job id: %v", err)
	}
	env := stepCompletedEnvelope(t, `{"broken":tru}`)
	if err := ts.stepStore.SetResult(step.ID, string(env)); err != nil {
		t.Fatalf("seed envelope result: %v", err)
	}

	if err := ts.OnJobCompleted(t.Context(), "job-sfp-1", env); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	payload := drainStepFailed(t, stepFailedSub)
	if payload["task_id"] != parent.ID {
		t.Errorf("task_id = %v, want %q", payload["task_id"], parent.ID)
	}
	if payload["step_id"] != step.ID {
		t.Errorf("step_id = %v, want %q", payload["step_id"], step.ID)
	}
	if payload["state"] != string(task.StepFailed) {
		t.Errorf("state = %v, want %q", payload["state"], task.StepFailed)
	}
	// Same key set as the completed mirror: no schema invention.
	if _, ok := payload["result"]; !ok {
		t.Errorf("payload %v missing the result key (the completed payload's failure-text analog)", payload)
	}
}

// TestTacticalStepFailedPublishedOnValidationExhaustion pins site 2: the
// validation-exhaustion terminus falls through to the completion-flow publish
// with the step terminal-FAILED, and must emit task.step_failed too.
func TestTacticalStepFailedPublishedOnValidationExhaustion(t *testing.T) {
	ts, _, msgBus, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		vm := validator.NewValidatorManager()
		vm.RegisterValidator("file_write", alwaysFailValidator{})
		cfg.ValidatorManager = vm
		cfg.Queue = &mockQueue{}
	})
	defer cleanup()

	stepFailedSub := msgBus.Subscribe("step-failed-pin-validation", "task.step_failed")
	defer msgBus.Unsubscribe(stepFailedSub)

	parent := newGateTestTask(t, ts, "step-failed-publish-validation", 1)
	step := newGateTestStep(t, ts, parent.ID, "job-sfv-0", "file_write", "wrote the artifact")

	resultJSON, err := json.Marshal(map[string]any{"success": true, "result": "wrote the artifact"})
	if err != nil {
		t.Fatal(err)
	}

	// Default MaxValidationLoops 3 -> two retries, then exhaustion on the
	// third round (mirrors TestTacticalScheduler_ValidationExhaustionTerminalizesStep).
	for round := range 3 {
		row, gerr := ts.stepStore.GetByID(step.ID)
		if gerr != nil || row == nil {
			t.Fatalf("round %d: re-read step: %v", round, gerr)
		}
		if err := ts.OnJobCompleted(t.Context(), row.JobID, resultJSON); err != nil {
			t.Fatalf("round %d: OnJobCompleted: %v", round, err)
		}
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("re-read step: %v", err)
	}
	if persisted.State != task.StepFailed {
		t.Fatalf("step state = %q, want terminal failed", persisted.State)
	}

	payload := drainStepFailed(t, stepFailedSub)
	if payload["task_id"] != parent.ID {
		t.Errorf("task_id = %v, want %q", payload["task_id"], parent.ID)
	}
	if payload["step_id"] != step.ID {
		t.Errorf("step_id = %v, want %q", payload["step_id"], step.ID)
	}
	if payload["state"] != string(task.StepFailed) {
		t.Errorf("state = %v, want %q", payload["state"], task.StepFailed)
	}
}
