package agent

// Output-filters tree, leaf 03, Task 4: the INDEPENDENT filter retry path.
//
// A filter rejection must requeue the step on FilterRetryCount alone (cap
// from the filterRetryLimiter seam, default 2), mirroring the
// validation-retry persistence sequence exactly. Exhaustion finalizes the
// step FAILED with the filter Reason as the error text. Critically, the two
// retry classes are independent in BOTH directions (master.md Contract 3):
// filter retries never touch ValidationRetryCount, and validation retries
// never touch FilterRetryCount.

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
)

// fixedLimiter is the test double for the filterRetryLimiter seam.
type fixedLimiter struct{ n int }

func (f fixedLimiter) MaxFilterRetries() int { return f.n }

func TestTaskServiceFilterRetryRequeues(t *testing.T) {
	trace := &traceRecorder{alwaysApplies: true}
	logger, capture := newFilterLogRecorder(t)
	ts, _, _, rq, cleanup := newFilterTestScheduler(t, func(cfg *TacticalSchedulerConfig, rq *recordingQueue) {
		cfg.Logger = logger
		// ValidatorManager intentionally NOT wired: a filter rejection
		// must requeue without any evidence-validation involvement.
	})
	defer cleanup()
	ts.SetFilterChain(validator.NewFilterChain([]validator.OutputFilter{
		&recordingFilter{name: "json_format", trace: trace, fail: "$.tools: invalid"},
	}, 2))

	parent := newFilterTestTask(t, ts, "filter-retry-requeue", 1)
	step := newFilterTestStep(t, ts, parent.ID, "job-fretry-1", "shell", `{"broken":tru}`)
	env := stepCompletedEnvelope(t, `{"broken":tru}`)
	if err := ts.stepStore.SetResult(step.ID, string(env)); err != nil {
		t.Fatalf("failed to seed step result: %v", err)
	}

	if err := ts.OnJobCompleted(t.Context(), "job-fretry-1", env); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	// Step requeued: state back to scheduled with a NEW job id, exactly as
	// the validation retry leaves it.
	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if persisted.State != task.StepScheduled {
		t.Errorf("step state = %q, want %q (requeued for filter retry)", persisted.State, task.StepScheduled)
	}
	if persisted.JobID == "" || persisted.JobID == "job-fretry-1" {
		t.Errorf("step job_id = %q, want a fresh retry job id", persisted.JobID)
	}

	// Filter counters advanced; validation counters untouched.
	if persisted.FilterRetryCount != 1 {
		t.Errorf("FilterRetryCount = %d, want 1", persisted.FilterRetryCount)
	}
	if persisted.FilterError != "$.tools: invalid" {
		t.Errorf("FilterError = %q, want the rejection Reason", persisted.FilterError)
	}
	if persisted.ValidationRetryCount != 0 {
		t.Errorf("ValidationRetryCount = %d, want 0 (filter retry must never touch validation accounting)", persisted.ValidationRetryCount)
	}

	// A retry job was actually enqueued and carries the step identity.
	job := rq.lastEnqueuedJob()
	if job == nil {
		t.Fatalf("no retry job enqueued")
	}
	var payload StepJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("retry payload decode: %v", err)
	}
	if payload.StepID != step.ID {
		t.Errorf("retry payload step_id = %q, want %q", payload.StepID, step.ID)
	}

	// Rejection logged with action=fail under the output_filter stage.
	if n := logLinesContaining(capture, "action=fail"); n == 0 {
		t.Errorf("filter rejection not logged with action=fail")
	}
	if n := logLinesContaining(capture, "stage=output_filter"); n == 0 {
		t.Errorf("rejection log missing stage=output_filter")
	}
}

func TestTaskServiceFilterRetryExhaustion(t *testing.T) {
	trace := &traceRecorder{alwaysApplies: true}
	logger, capture := newFilterLogRecorder(t)
	// Cap 1: the step arrives with its single retry budget already
	// consumed (persisted by the first round's requeue — the column from
	// leaf 03 Task 1), so THIS rejection must hit the terminus.
	ts, _, _, _, cleanup := newFilterTestScheduler(t, func(cfg *TacticalSchedulerConfig, rq *recordingQueue) {
		cfg.Logger = logger
	})
	defer cleanup()
	ts.filterRetryLimiter = fixedLimiter{n: 1}
	ts.SetFilterChain(validator.NewFilterChain([]validator.OutputFilter{
		&recordingFilter{name: "json_format", trace: trace, fail: "language mismatch lang=de confidence=0.97"},
	}, 2))

	parent := newFilterTestTask(t, ts, "filter-retry-exhaust", 1)
	step := newFilterTestStep(t, ts, parent.ID, "job-fexh-1", "shell", `{"broken":tru}`)
	// Burn the retry budget the way round 1's requeue would have persisted.
	step.FilterRetryCount = 1
	step.FilterError = "prior rejection"
	if err := ts.stepStore.Update(step); err != nil {
		t.Fatalf("failed to seed consumed retry: %v", err)
	}
	// Update writes the full row; restore the job binding the completion
	// path looks up, then seed the envelope result.
	if err := ts.stepStore.SetJobID(step.ID, "job-fexh-1"); err != nil {
		t.Fatalf("failed to restore job id: %v", err)
	}
	env := stepCompletedEnvelope(t, `{"broken":tru}`)
	if err := ts.stepStore.SetResult(step.ID, string(env)); err != nil {
		t.Fatalf("failed to seed step result: %v", err)
	}

	if err := ts.OnJobCompleted(t.Context(), "job-fexh-1", env); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	// Step finalizes FAILED with the filter Reason as the error text.
	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if persisted.State != task.StepFailed {
		t.Fatalf("step state = %q, want %q after retry exhaustion", persisted.State, task.StepFailed)
	}
	if persisted.FilterError != "language mismatch lang=de confidence=0.97" {
		t.Errorf("FilterError = %q, want the filter Reason as the error text", persisted.FilterError)
	}
	if persisted.ValidationRetryCount != 0 {
		t.Errorf("ValidationRetryCount = %d, want 0", persisted.ValidationRetryCount)
	}

	// Exhaustion logged with action=rejected_exhausted.
	if n := logLinesContaining(capture, "action=rejected_exhausted"); n == 0 {
		t.Errorf("exhaustion not logged with action=rejected_exhausted")
	}
}

// TestTaskServiceFilterRetryIndependence is THE both-directions probe
// (master.md Contract 3, "test-proven" clause):
//
//	direction A: a step rejected by the FILTER increments FilterRetryCount
//	             while ValidationRetryCount stays 0;
//	direction B: a step that fails evidence VALIDATION leaves
//	             FilterRetryCount at 0.
func TestTaskServiceFilterRetryIndependence(t *testing.T) {
	trace := &traceRecorder{alwaysApplies: true}
	ts, _, _, _, cleanup := newFilterTestScheduler(t, func(cfg *TacticalSchedulerConfig, rq *recordingQueue) {
		cfg.ValidatorManager = validator.NewValidatorManager()
		// Evidence validator for the "vstep" hint: always fails validation.
		cfg.ValidatorManager.RegisterValidator("vstep", &recordingValidator{trace: trace, fail: true})
	})
	defer cleanup()
	// The chain applies ONLY to the filter-target step (hint "shell"):
	// Applies scopes it, exactly like a real content filter that only
	// judges code-bearing steps. Step B's "vstep" hint is outside the
	// filter's scope but INSIDE the evidence gate's (its validator is
	// registered for "vstep").
	ts.SetFilterChain(validator.NewFilterChain([]validator.OutputFilter{
		&recordingFilter{name: "json_format", trace: trace, fail: "$.tools: invalid", hintScope: "shell"},
	}, 2))

	parent := newFilterTestTask(t, ts, "filter-independence", 2)

	// --- Direction A: filter rejection on step A ("shell" hint). --------
	stepA := newFilterTestStep(t, ts, parent.ID, "job-find-a", "shell", `{"broken":tru}`)
	envA := stepCompletedEnvelope(t, `{"broken":tru}`)
	if err := ts.stepStore.SetResult(stepA.ID, string(envA)); err != nil {
		t.Fatalf("failed to seed step A result: %v", err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-find-a", envA); err != nil {
		t.Fatalf("OnJobCompleted (filter-rejected step): %v", err)
	}
	persistedA, err := ts.stepStore.GetByID(stepA.ID)
	if err != nil || persistedA == nil {
		t.Fatalf("failed to re-read step A: %v", err)
	}
	if persistedA.FilterRetryCount != 1 {
		t.Errorf("direction A: FilterRetryCount = %d, want 1", persistedA.FilterRetryCount)
	}
	if persistedA.ValidationRetryCount != 0 {
		t.Errorf("direction A: ValidationRetryCount = %d, want 0 (filter retry must not consume a validation retry)", persistedA.ValidationRetryCount)
	}

	// --- Direction B: validation failure on step B ("vstep" hint). ------
	stepB := newFilterTestStep(t, ts, parent.ID, "job-find-b", "vstep", "artifact produced")
	envB := stepCompletedEnvelope(t, "artifact produced")
	if err := ts.stepStore.SetResult(stepB.ID, string(envB)); err != nil {
		t.Fatalf("failed to seed step B result: %v", err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-find-b", envB); err != nil {
		t.Fatalf("OnJobCompleted (validation-failed step): %v", err)
	}
	persistedB, err := ts.stepStore.GetByID(stepB.ID)
	if err != nil || persistedB == nil {
		t.Fatalf("failed to re-read step B: %v", err)
	}
	// ValidationRetryCount has NO store column by design (the in-memory
	// validationRetries map is authoritative — see the field comment in
	// tactical.go), so assert the effective count through the scheduler,
	// not the reloaded row.
	if ts.validationRetryCount(persistedB) == 0 {
		t.Errorf("direction B: effective ValidationRetryCount = 0, want > 0 (validation retry path did not run)")
	}
	if persistedB.FilterRetryCount != 0 {
		t.Errorf("direction B: FilterRetryCount = %d, want 0 (validation retry must not consume a filter retry)", persistedB.FilterRetryCount)
	}
	if persistedB.FilterError != "" {
		t.Errorf("direction B: FilterError = %q, want empty", persistedB.FilterError)
	}

}
