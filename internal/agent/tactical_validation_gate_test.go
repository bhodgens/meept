package agent

// Regression pins for the 2026-09-12 bughunt wave, group 1 (step state +
// validation gates). Each test names the audit finding it pins and fails if
// the corresponding fix is reverted.
//
//	F2/F7  - the validation verdict must be persisted AFTER it is assigned
//	         (the b31c3f6e write happened before the assignment, so the row
//	         kept validated=false and the task gate blocked forever)
//	F8     - the claim-vs-evidence predicate must survive the daemon's
//	         []string evidence decode artifact, and the artifact must not be
//	         stored as the step's evidence
//	F4/F50 - the claim-vs-evidence branch must not return before the step is
//	         terminalized and the task finalized
//	F3     - a reviewer ERROR must not strand the task: the gate keys on an
//	         explicit verdict, not on a missing Validated flag
//	F9     - the heuristic approval guard must use the structural evidence
//	         predicate
//	F10    - a vacuous validator pass (nothing to verify) must not be
//	         recorded as validated
//	F11    - an approval with no tool-issued evidence must not stamp
//	         Validated
//	F1     - the approval paths must not write a stale in-memory state back
//	         over the terminal state
//	F51    - validation exhaustion must terminalize the step (it used to set
//	         no state at all and only return an error)
//	F6     - a late step must not resurrect an already-terminal task
//	F72    - needs_info force-completion must refresh before writing

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
	"github.com/caimlas/meept/pkg/models"
)

// newGateTestScheduler builds a TacticalScheduler over a temp task store and
// a live bus, letting the caller wire the validator/review managers.
func newGateTestScheduler(t *testing.T, configure func(cfg *TacticalSchedulerConfig)) (*TacticalScheduler, *task.Store, *bus.MessageBus, func()) {
	t.Helper()

	taskStore, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}

	msgBus := bus.New(nil, slogDiscardLogger())
	cfg := TacticalSchedulerConfig{
		StepStore: taskStore.StepStore(),
		TaskStore: taskStore,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	}
	if configure != nil {
		configure(&cfg)
	}
	ts := NewTacticalScheduler(cfg)

	return ts, taskStore, msgBus, func() { taskStore.Close() }
}

// newGateTestTask creates an executing task with totalJobs steps.
func newGateTestTask(t *testing.T, ts *TacticalScheduler, name string, totalJobs int) *task.Task {
	t.Helper()
	tk := task.NewTask(name, "gate pin task")
	tk.TotalJobs = totalJobs
	tk.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	return tk
}

// newGateTestStep creates a scheduled step bound to jobID.
func newGateTestStep(t *testing.T, ts *TacticalScheduler, taskID, jobID, hint, result string) *task.TaskStep {
	t.Helper()
	step := task.NewTaskStep(taskID, "produce the artifact", 0)
	step.ToolHint = hint
	step.AgentID = "coder"
	step.Result = result
	if err := ts.stepStore.Create(step); err != nil {
		t.Fatalf("failed to create step: %v", err)
	}
	if err := ts.stepStore.SetState(step.ID, task.StepScheduled); err != nil {
		t.Fatalf("failed to schedule step: %v", err)
	}
	if jobID != "" {
		if err := ts.stepStore.SetJobID(step.ID, jobID); err != nil {
			t.Fatalf("failed to set job id: %v", err)
		}
	}
	return step
}

// TestTacticalScheduler_ValidationVerdictPersistedAfterAssignment pins F2/F7:
// with a validator wired and real tool-issued evidence, the re-read row must
// carry validated=true AND validation_error="". With the Update issued before
// the assignments (the b31c3f6e shape) the row keeps validated=false and the
// task-level gate then fails the task.
func TestTacticalScheduler_ValidationVerdictPersistedAfterAssignment(t *testing.T) {
	ts, _, _, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		cfg.ValidatorManager = validator.NewValidatorManager()
	})
	defer cleanup()

	parent := newGateTestTask(t, ts, "verdict-persist", 1)
	step := newGateTestStep(t, ts, parent.ID, "job-verdict-1", "file_write", "artifact produced")

	// Daemon-shaped envelope: tool_evidence is the typed, tool-issued
	// projection the validator runs against.
	resultJSON, err := json.Marshal(map[string]any{
		"success": true,
		"result":  "artifact produced",
		"tool_evidence": []map[string]any{
			{"type": "shell_output", "subject": "echo ok", "value": "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-verdict-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if !persisted.Validated {
		t.Fatalf("persisted validated = false, want true (the verdict must be written AFTER it is assigned)")
	}
	if persisted.ValidationError != "" {
		t.Errorf("persisted validation_error = %q, want empty", persisted.ValidationError)
	}

	got, err := ts.taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to re-read task: %v", err)
	}
	if got.State != task.StateCompleted {
		t.Fatalf("task state = %q, want %q (a validated step must not be blocked by its own gate)", got.State, task.StateCompleted)
	}
}

// TestTacticalScheduler_DaemonEnvelopeClaimsGate pins F8 + F4/F50 together
// with the exact daemon step-job envelope shape (evidence as []string of
// prose + claims). The claim-vs-evidence backstop must fire, drop the
// zero-value decode artifact, keep the step terminal, and finalize the task
// as a FAILURE rather than hanging it in executing.
func TestTacticalScheduler_DaemonEnvelopeClaimsGate(t *testing.T) {
	ts, _, msgBus, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		cfg.ValidatorManager = validator.NewValidatorManager()
	})
	defer cleanup()

	completedSub := msgBus.Subscribe("claims-gate-pin", "task.completed")
	defer msgBus.Unsubscribe(completedSub)

	parent := newGateTestTask(t, ts, "claims-gate", 1)
	step := newGateTestStep(t, ts, parent.ID, "job-claims-1", "file_write", "Created file hello.txt")

	// Exactly what internal/daemon/components.go emits for a step job.
	resultJSON, err := json.Marshal(map[string]any{
		"success":  true,
		"result":   "Created file hello.txt",
		"evidence": []string{"job job-claims-1 completed by agent coder: Created file hello.txt"},
		"claims":   []string{"Created file hello.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-claims-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if !strings.Contains(persisted.ValidationError, "unverified narration") {
		t.Fatalf("persisted validation_error = %q, want the unverified-narration marker (the gate never fired)", persisted.ValidationError)
	}
	if len(persisted.Evidence) != 0 {
		t.Errorf("persisted evidence = %v, want empty (the zero-value decode artifact must not be stored)", persisted.Evidence)
	}
	if persisted.State != task.StepCompleted {
		t.Fatalf("persisted step state = %q, want %q (the branch must not return before terminalizing)", persisted.State, task.StepCompleted)
	}

	got, err := ts.taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to re-read task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("task state = %q, want %q (an unverified-narration step must fail the task, not hang it)", got.State, task.StateFailed)
	}

	select {
	case msg := <-completedSub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal task.completed: %v", err)
		}
		if payload["status"] != "failed" {
			t.Errorf("task.completed status = %v, want failed", payload["status"])
		}
		if res, _ := payload["result"].(string); !strings.Contains(res, "unverified narration") {
			t.Errorf("task.completed result = %q, want the validation-block reason", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.completed")
	}
}

// TestTacticalScheduler_ReviewerErrorDoesNotBlockTask pins F3: with a
// validator wired, a step terminalized through the reviewer-error path must
// still let the task finalize. The gate keys on an explicit verdict, not on
// the absence of a Validated flag (the pre-fix gate failed the task here).
func TestTacticalScheduler_ReviewerErrorDoesNotBlockTask(t *testing.T) {
	var registry *AgentRegistry
	ts, _, _, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		cfg.ValidatorManager = validator.NewValidatorManager()
		registry = NewAgentRegistry(RegistryConfig{Logger: slogDiscardLogger()})
		// The reviewer spec resolves, but its loop has no LLM client, so
		// RunOnce returns ErrNoLLMClient and ReviewStep returns an error —
		// the reviewer-ERROR terminalization path.
		if err := registry.RegisterSpec(&AgentSpec{ID: "test-reviewer", Name: "test-reviewer"}); err != nil {
			t.Fatalf("register reviewer spec: %v", err)
		}
		cfg.ReviewManager = NewReviewManager(ReviewManagerConfig{
			Registry:  registry,
			StepStore: cfg.StepStore,
			TaskStore: cfg.TaskStore,
			Logger:    slogDiscardLogger(),
		})
	})
	defer cleanup()

	// Three steps so the review policy does not take the trivial-task
	// heuristic path; two are already complete, so driving the third
	// finalizes the task.
	parent := newGateTestTask(t, ts, "reviewer-error", 3)
	for i := 0; i < 2; i++ {
		done := newGateTestStep(t, ts, parent.ID, "", "", "already done")
		if err := ts.stepStore.SetState(done.ID, task.StepCompleted); err != nil {
			t.Fatalf("complete sibling step: %v", err)
		}
	}
	step := newGateTestStep(t, ts, parent.ID, "job-rev-err-1", "file_write", "artifact produced")

	resultJSON, err := json.Marshal(map[string]any{"success": true, "result": "artifact produced"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-rev-err-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if persisted.State != task.StepCompleted {
		t.Fatalf("persisted step state = %q, want %q (the reviewer-error path must terminalize; an approval would mean the reviewer ran)", persisted.State, task.StepCompleted)
	}
	if persisted.Validated {
		t.Errorf("persisted validated = true, want false (no validation flow ran for this step)")
	}

	got, err := ts.taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to re-read task: %v", err)
	}
	if got.State != task.StateCompleted {
		t.Fatalf("task state = %q, want %q (a reviewer ERROR must not block the task)", got.State, task.StateCompleted)
	}
}

// TestTacticalScheduler_VacuousValidatorPassNotValidated pins F10 and F11:
// a validator that runs over zero/zero-value evidence reports Valid without
// verifying anything, and an approval with no tool-issued evidence verifies
// nothing either — neither may be recorded as validated=true. The task must
// still finalize (the gate must not strand a step with nothing to verify).
func TestTacticalScheduler_VacuousValidatorPassNotValidated(t *testing.T) {
	ts, _, _, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		cfg.ValidatorManager = validator.NewValidatorManager()
		cfg.ReviewManager = NewReviewManager(ReviewManagerConfig{
			StepStore: cfg.StepStore,
			TaskStore: cfg.TaskStore,
			Logger:    slogDiscardLogger(),
		})
	})
	defer cleanup()

	parent := newGateTestTask(t, ts, "vacuous-pass", 1)
	step := newGateTestStep(t, ts, parent.ID, "job-vacuous-1", "file_write", "produced the artifact")

	// No evidence at all: the filesystem validator has nothing to check and
	// returns Valid. A single-step task takes the heuristic approval path.
	resultJSON, err := json.Marshal(map[string]any{"success": true, "result": "produced the artifact"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-vacuous-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if persisted.State != task.StepApproved {
		t.Fatalf("persisted step state = %q, want %q", persisted.State, task.StepApproved)
	}
	if persisted.Validated {
		t.Error("persisted validated = true, want false (nothing was verified: no evidence to validate)")
	}

	got, err := ts.taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to re-read task: %v", err)
	}
	if got.State != task.StateCompleted {
		t.Fatalf("task state = %q, want %q (a step with nothing to verify must not strand the task)", got.State, task.StateCompleted)
	}
}

// TestTacticalScheduler_ValidationExhaustionTerminalizesStep pins F51: once
// the validation retries are exhausted the step must become terminal (it
// used to keep StepScheduled while OnJobCompleted returned an error that
// only got logged), so the task finalizes as failed instead of hanging.
func TestTacticalScheduler_ValidationExhaustionTerminalizesStep(t *testing.T) {
	ts, _, _, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		vm := validator.NewValidatorManager()
		vm.RegisterValidator("file_write", alwaysFailValidator{})
		cfg.ValidatorManager = vm
		cfg.Queue = &mockQueue{}
	})
	defer cleanup()

	parent := newGateTestTask(t, ts, "validation-exhaustion", 1)
	step := newGateTestStep(t, ts, parent.ID, "job-exhaust-0", "file_write", "wrote the artifact")

	resultJSON, err := json.Marshal(map[string]any{"success": true, "result": "wrote the artifact"})
	if err != nil {
		t.Fatal(err)
	}

	// Default MaxValidationLoops is 3 -> 2 retries, then exhaustion. Each
	// retry re-stamps the step's job id, so re-read it every round (exactly
	// what the daemon's retry job does).
	for round := 0; round < 3; round++ {
		row, gerr := ts.stepStore.GetByID(step.ID)
		if gerr != nil || row == nil {
			t.Fatalf("round %d: re-read step: %v", round, gerr)
		}
		if row.JobID == "" {
			t.Fatalf("round %d: step has no job id", round)
		}
		if err := ts.OnJobCompleted(t.Context(), row.JobID, resultJSON); err != nil {
			t.Fatalf("round %d: OnJobCompleted returned %v; want nil (the step is terminalized instead)", round, err)
		}
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if persisted.State != task.StepFailed {
		t.Fatalf("persisted step state = %q, want %q (the retry cap must be finite and terminalize)", persisted.State, task.StepFailed)
	}
	if persisted.ValidationError == "" {
		t.Error("persisted validation_error is empty, want the validation failure text")
	}

	got, err := ts.taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to re-read task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("task state = %q, want %q", got.State, task.StateFailed)
	}
}

// TestTacticalScheduler_LateStepDoesNotResurrectTerminalTask pins F6 on the
// tactical side: a step that completes after another path terminalized the
// task must neither flip the state back nor emit a second task.completed
// (that is what let the orchestrator's TaskOutcome reset the ralph counter).
func TestTacticalScheduler_LateStepDoesNotResurrectTerminalTask(t *testing.T) {
	ts, _, msgBus, cleanup := newGateTestScheduler(t, nil)
	defer cleanup()

	completedSub := msgBus.Subscribe("late-step-pin", "task.completed")
	defer msgBus.Unsubscribe(completedSub)

	parent := newGateTestTask(t, ts, "late-step", 1)
	// The ralph cap (or a cancellation) already failed the task.
	parent.SetState(task.StateFailed)
	if err := ts.taskStore.Update(parent); err != nil {
		t.Fatalf("failed to fail task: %v", err)
	}
	newGateTestStep(t, ts, parent.ID, "job-late-1", "", "done")

	resultJSON, err := json.Marshal(map[string]any{"success": true, "result": "done"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-late-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	got, err := ts.taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to re-read task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("task state = %q, want %q (a late step must not resurrect a terminal task)", got.State, task.StateFailed)
	}

	select {
	case msg := <-completedSub.Channel:
		t.Fatalf("unexpected task.completed for an already-terminal task: %s", string(msg.Payload))
	case <-time.After(300 * time.Millisecond):
	}
}

// TestTacticalScheduler_NeedsInfoPersistsTerminalStateWithEvidence pins F72:
// the needs_info force-completion must not write its stale pre-review copy
// back over the terminal state it just set.
func TestTacticalScheduler_NeedsInfoPersistsTerminalStateWithEvidence(t *testing.T) {
	ts, _, _, cleanup := newGateTestScheduler(t, nil)
	defer cleanup()

	parent := newGateTestTask(t, ts, "needs-info", 1)
	step := newGateTestStep(t, ts, parent.ID, "", "file_write", "artifact produced")
	// Evidence on record: the review had something to judge.
	step.Evidence = []models.Evidence{{Type: models.EvidenceShellOutput, Subject: "echo ok", Value: "ok"}}
	if err := ts.stepStore.Update(step); err != nil {
		t.Fatalf("failed to persist evidence: %v", err)
	}
	// The review path leaves the row 'reviewing' (DB-only write).
	if err := ts.stepStore.SetState(step.ID, task.StepReviewing); err != nil {
		t.Fatalf("failed to set reviewing: %v", err)
	}

	// The tactical-side copy is the pre-review one (state scheduled).
	ts.reviewManager = NewReviewManager(ReviewManagerConfig{
		StepStore: ts.stepStore,
		TaskStore: ts.taskStore,
		Logger:    slogDiscardLogger(),
	})
	if err := ts.handleReviewResult(t.Context(), step, &ReviewResult{
		Status:     ReviewNeedsInfo,
		Feedback:   "need more info from the operator",
		Confidence: 0.7,
	}); err != nil {
		t.Fatalf("handleReviewResult: %v", err)
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if persisted.State != task.StepCompleted {
		t.Fatalf("persisted step state = %q, want %q (the stale in-memory state must not revert it)", persisted.State, task.StepCompleted)
	}
	if !persisted.Validated {
		t.Error("persisted validated = false, want true (the review had tool-issued evidence)")
	}
}

// alwaysFailValidator is a Validator that always reports failure, used to
// drive the validation-exhaustion path deterministically.
type alwaysFailValidator struct{}

func (alwaysFailValidator) Validate(_ context.Context, _ *task.TaskStep) validator.ValidationResult {
	return validator.ValidationResult{Valid: false, Errors: []string{"evidence does not resolve on disk"}}
}

// TestTacticalScheduler_RetryPassClearsStaleValidationError pins the stale
// validation error the wave introduced. On a validation retry the pass arm
// cleared ValidationError INSIDE the evidence branch, so a retry that passes
// over non-meaningful evidence kept round 1's failure text — and the task
// gate's explicit-verdict arm then failed the task with a reason the
// validator no longer supported. A PASS must clear the field unconditionally;
// only Validated stays gated on evidence.
func TestTacticalScheduler_RetryPassClearsStaleValidationError(t *testing.T) {
	ts, _, _, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		cfg.ValidatorManager = validator.NewValidatorManager()
	})
	defer cleanup()

	parent := newGateTestTask(t, ts, "stale-validation-error", 1)
	step := newGateTestStep(t, ts, parent.ID, "job-stale-1", "file_write", "artifact produced")

	// Round 1 failed validation; the row carries its verdict. Re-stamp the
	// job id on the in-memory copy: SetJobID writes the DB only, so a plain
	// Update would clear it.
	step.JobID = "job-stale-1"
	step.ValidationError = "validation failed: evidence does not resolve on disk"
	if err := ts.stepStore.Update(step); err != nil {
		t.Fatalf("seed validation error: %v", err)
	}

	// Round 2: no tool evidence, so the filesystem validator has nothing to
	// check and returns a vacuous PASS (meaningful-evidence is false).
	resultJSON, err := json.Marshal(map[string]any{"success": true, "result": "artifact produced"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-stale-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("re-read step: %v", err)
	}
	if persisted.ValidationError != "" {
		t.Errorf("persisted validation_error = %q, want empty (a PASS clears the verdict, even over non-meaningful evidence)",
			persisted.ValidationError)
	}
	if persisted.Validated {
		t.Error("persisted validated = true, want false (nothing meaningful was verified)")
	}

	got, err := ts.taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read task: %v", err)
	}
	if got.State != task.StateCompleted {
		t.Fatalf("task state = %q, want %q (a stale verdict must not fail the task the validator passed)",
			got.State, task.StateCompleted)
	}
}

// TestTacticalScheduler_ValidationResidualBlocksTask documents the residual
// safety net (task gate) end-to-end: a successfully-terminal step carrying a
// validator for its tool hint and tool-issued evidence but no verdict at all
// fails the task. This is a DEFENSE-IN-DEPTH arm, not production coverage —
// the residual state it hand-builds has no production writer today (every
// production path that finds a validator either records a verdict or records a
// ValidationError, and the no-flow paths leave evidence empty), so the row is
// constructed here rather than driven through the scheduler. It is kept as the
// end-to-end record that the arm is ARMED and blocks; the arm's actual rule is
// pinned by TestTacticalScheduler_ValidationResidualRule, which fails if the
// helper's decision changes.
func TestTacticalScheduler_ValidationResidualBlocksTask(t *testing.T) {
	ts, _, msgBus, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		cfg.ValidatorManager = validator.NewValidatorManager()
	})
	defer cleanup()

	completedSub := msgBus.Subscribe("residual-net-pin", "task.completed")
	defer msgBus.Unsubscribe(completedSub)

	parent := newGateTestTask(t, ts, "residual-net", 2)

	// Step A: terminal, tool-issued evidence on record, no verdict (the
	// residual shape).
	stepA := newGateTestStep(t, ts, parent.ID, "", "file_write", "artifact already produced")
	stepA.Evidence = []models.Evidence{{Type: models.EvidenceShellOutput, Subject: "echo ok", Value: "ok"}}
	if err := ts.stepStore.Update(stepA); err != nil {
		t.Fatalf("persist residual evidence: %v", err)
	}
	if err := ts.stepStore.SetState(stepA.ID, task.StepCompleted); err != nil {
		t.Fatalf("complete residual step: %v", err)
	}

	// Step B: completes normally and drives finalization.
	stepB := newGateTestStep(t, ts, parent.ID, "job-residual-1", "file_write", "artifact produced")
	resultJSON, err := json.Marshal(map[string]any{
		"success": true,
		"result":  "artifact produced",
		"tool_evidence": []map[string]any{
			{"type": "shell_output", "subject": "echo ok", "value": "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.OnJobCompleted(t.Context(), "job-residual-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	persistedB, err := ts.stepStore.GetByID(stepB.ID)
	if err != nil || persistedB == nil {
		t.Fatalf("re-read step B: %v", err)
	}
	if !persistedB.Validated {
		t.Fatal("precondition: step B must validate, so only the residual step can fail the task")
	}

	got, err := ts.taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read task: %v", err)
	}
	if got.State != task.StateFailed {
		t.Fatalf("task state = %q, want %q (a residual step with evidence and a validator but no verdict must block)",
			got.State, task.StateFailed)
	}

	select {
	case msg := <-completedSub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("unmarshal task.completed: %v", err)
		}
		if payload["status"] != "failed" {
			t.Errorf("task.completed status = %v, want failed", payload["status"])
		}
		if res, _ := payload["result"].(string); !strings.Contains(res, "completed but not validated") {
			t.Errorf("task.completed result = %q, want the residual-block reason", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for task.completed")
	}
}

// TestTacticalScheduler_IntervalGateIgnoresHonestUnvalidatedSteps pins the
// second validation gate. runValidationGate still keyed on the OLD broad rule
// (IsSuccessfullyTerminal() && !Validated), which the wave made dishonest:
// Validated=false is now the correct record for a step whose validator had
// nothing to check, so the interval gate warned "N completed steps not
// validated" about honest steps every interval. It must use the same residual
// rule as the task gate.
func TestTacticalScheduler_IntervalGateIgnoresHonestUnvalidatedSteps(t *testing.T) {
	ts, _, _, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		cfg.ValidatorManager = validator.NewValidatorManager()
	})
	defer cleanup()

	parent := newGateTestTask(t, ts, "interval-gate-honest", 1)

	// Honest unvalidated: terminal, but nothing meaningful to verify.
	honest := newGateTestStep(t, ts, parent.ID, "", "file_write", "nothing to verify")
	if err := ts.stepStore.SetState(honest.ID, task.StepCompleted); err != nil {
		t.Fatalf("complete honest step: %v", err)
	}

	if err := ts.runValidationGate(context.Background(), parent.ID); err != nil {
		t.Fatalf("runValidationGate = %v, want nil (an honest unvalidated step must not warn)", err)
	}

	// The gate must still fire on a genuine residual.
	residual := newGateTestStep(t, ts, parent.ID, "", "file_write", "artifact produced")
	residual.Evidence = []models.Evidence{{Type: models.EvidenceShellOutput, Subject: "echo ok", Value: "ok"}}
	if err := ts.stepStore.Update(residual); err != nil {
		t.Fatalf("persist residual evidence: %v", err)
	}
	if err := ts.stepStore.SetState(residual.ID, task.StepCompleted); err != nil {
		t.Fatalf("complete residual step: %v", err)
	}
	if err := ts.runValidationGate(context.Background(), parent.ID); err == nil {
		t.Fatal("runValidationGate = nil, want an error naming the residual step")
	} else if !strings.Contains(err.Error(), residual.ID) {
		t.Errorf("runValidationGate error = %v, want it to name the residual step %s", err, residual.ID)
	}
}

// TestReviewManager_ApprovalPersistsApprovedState pins F1 on the reviewer
// approval path: SetState writes the DB only, and the step was loaded while
// still 'reviewing', so the full-row Update must carry the approved state or
// the approval reverts and the task can never finalize.
func TestReviewManager_ApprovalPersistsApprovedState(t *testing.T) {
	rm := newTestReviewManager(t)
	step := &task.TaskStep{
		ID:       "step-appr-state",
		TaskID:   "task-appr-state",
		Sequence: 0,
		ToolHint: "file_write",
		State:    task.StepReviewing, // what ReviewStep's DB-only SetState left
		Result:   "artifact produced",
		Evidence: []models.Evidence{{Type: models.EvidenceShellOutput, Subject: "echo ok", Value: "ok"}},
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	if _, err := rm.HandleReviewResult(context.Background(), step.ID, &ReviewResult{
		Status:     ReviewApproved,
		Feedback:   "looks good",
		Confidence: 0.9,
	}, nil); err != nil {
		t.Fatalf("HandleReviewResult: %v", err)
	}

	persisted, err := rm.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != task.StepApproved {
		t.Fatalf("persisted state = %q, want %q (the stale in-memory state must not overwrite the approval)", persisted.State, task.StepApproved)
	}
	if !persisted.Validated {
		t.Error("persisted validated = false, want true (the approval had tool-issued evidence)")
	}
}

// TestHeuristicReview_ArtifactClaimsWithZeroValueEvidenceRejected pins F9:
// the daemon envelope's []string evidence decodes to a one-element slice
// holding a zero-value Evidence, so the structural predicate must be used —
// len(step.Evidence) == 0 can never refuse a job-driven step.
func TestHeuristicReview_ArtifactClaimsWithZeroValueEvidenceRejected(t *testing.T) {
	rm := &ReviewManager{logger: testLogger()}
	step := &task.TaskStep{
		ToolHint: "coder",
		Result:   "Created file hello.txt containing 'hello'",
		// Decode artifact: one zero-value struct in the slice.
		Evidence: []models.Evidence{{}},
	}
	if rm.heuristicReviewPasses(step) {
		t.Error("artifact claims with zero-value evidence must NOT auto-approve")
	}
}

// TestHeuristicReview_ApprovalWithoutEvidenceNotValidated pins the F11 half
// that lives in heuristicReviewPasses' caller: an approval over a step with
// no tool-issued evidence must not be recorded as validated.
func TestHeuristicReview_ApprovalWithoutEvidenceNotValidated(t *testing.T) {
	stepStore := newTestStepStore(t)
	rm := NewReviewManager(ReviewManagerConfig{StepStore: stepStore, Logger: testLogger()})

	step := &task.TaskStep{
		ID:       "step-heur-noev",
		TaskID:   "task-heur-noev",
		Sequence: 0,
		ToolHint: "file_write",
		State:    task.StepScheduled,
		Result:   "produced the artifact",
	}
	if err := stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status != ReviewApproved {
		t.Fatalf("status = %v, want ReviewApproved (trivial-task heuristic)", res.Status)
	}

	persisted, err := stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if persisted.State != task.StepApproved {
		t.Fatalf("persisted state = %q, want %q", persisted.State, task.StepApproved)
	}
	if persisted.Validated {
		t.Error("persisted validated = true, want false (an approval with no evidence verifies nothing)")
	}
}

// TestTacticalScheduler_ValidationResidualRule pins the DECISION RULE of
// stepValidationResidual directly, one clause at a time, so the rule cannot
// change without a red test. The task-gate arm it feeds is a defense-in-depth
// net whose residual state has no production writer today (see
// TestTacticalScheduler_ValidationResidualBlocksTask), so the end-to-end test
// alone cannot catch a change to the predicate — this table can, and it also
// records WHY each condition is load-bearing.
func TestTacticalScheduler_ValidationResidualRule(t *testing.T) {
	ts, _, _, cleanup := newGateTestScheduler(t, func(cfg *TacticalSchedulerConfig) {
		cfg.ValidatorManager = validator.NewValidatorManager()
	})
	defer cleanup()

	// Precondition: the shape the rule is built around — a hint a validator
	// exists for, and evidence a tool actually produced.
	withValidator := "file_write"
	if !ts.validatorManager.HasValidator(withValidator) {
		t.Fatalf("precondition: no validator registered for hint %q", withValidator)
	}
	realEvidence := []models.Evidence{{Type: models.EvidenceShellOutput, Subject: "echo ok", Value: "ok"}}

	// residual is the one blocking shape; every other case mutates exactly one
	// clause of it, so a rule that drops any clause changes at least one row.
	base := func() *task.TaskStep {
		return &task.TaskStep{
			ID:       "step-rule",
			TaskID:   "task-rule",
			ToolHint: withValidator,
			State:    task.StepCompleted,
			Result:   "artifact produced",
			Evidence: realEvidence,
		}
	}

	for _, tc := range []struct {
		name string
		mut  func(s *task.TaskStep)
		want bool
	}{
		{"residual: validator + evidence + no verdict", func(*task.TaskStep) {}, true},
		{"only the residual shape blocks", func(*task.TaskStep) {}, true},
		{"not successfully terminal", func(s *task.TaskStep) { s.State = task.StepFailed }, false},
		{"still scheduled", func(s *task.TaskStep) { s.State = task.StepScheduled }, false},
		{"already validated", func(s *task.TaskStep) { s.Validated = true }, false},
		{"explicit validation error", func(s *task.TaskStep) { s.ValidationError = "claim without evidence" }, false},
		{"no validator for the hint", func(s *task.TaskStep) { s.ToolHint = "unregistered_hint" }, false},
		{"empty tool hint", func(s *task.TaskStep) { s.ToolHint = "" }, false},
		{"no evidence", func(s *task.TaskStep) { s.Evidence = nil }, false},
		{"zero-value evidence decode artifact", func(s *task.TaskStep) {
			s.Evidence = []models.Evidence{{}}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := base()
			tc.mut(step)
			if got := ts.stepValidationResidual(step); got != tc.want {
				t.Fatalf("stepValidationResidual = %v, want %v (state=%q validated=%v err=%q hint=%q ev=%d)",
					got, tc.want, step.State, step.Validated, step.ValidationError, step.ToolHint, len(step.Evidence))
			}
		})
	}

	// nil step must not panic the gate.
	if ts.stepValidationResidual(nil) {
		t.Error("stepValidationResidual(nil) = true, want false")
	}
}
