package agent

// Output-filters tree, leaf 03, Task 3: chain invocation + rewrite
// application. A wired chain that REWRITES the result must (a) replace
// step.Result with the chain's final output, (b) persist the rewritten
// result, (c) log the action with the output_filter stage keys, and (d)
// still let the evidence-validation gate run AFTER the rewrite — the gate
// must see the repaired content (master.md Contract 4: filter stage sits
// before the evidence gate, order FROZEN).

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
)

func TestTaskServiceFilterRewriteAppliesAndPersists(t *testing.T) {
	logger, capture := newFilterLogRecorder(t)

	trace := &traceRecorder{alwaysApplies: true}
	ts, _, _, _, cleanup := newFilterTestScheduler(t, func(cfg *TacticalSchedulerConfig, rq *recordingQueue) {
		cfg.Logger = logger
		cfg.ValidatorManager = validator.NewValidatorManager()
		// Register the sequence-recording evidence validator for the
		// step's tool hint so the gate actually invokes it.
		cfg.ValidatorManager.RegisterValidator("shell", &recordingValidator{trace: trace})
	})
	defer cleanup()
	ts.SetFilterChain(validator.NewFilterChain([]validator.OutputFilter{
		&onceRewriteFilter{trace: trace},
	}, 2))

	parent := newFilterTestTask(t, ts, "filter-rewrite", 1)
	step := newFilterTestStep(t, ts, parent.ID, "job-frew-1", "shell", `{"broken":tru}`)
	resultJSON := stepCompletedEnvelope(t, `{"broken":tru}`)
	// Seed the envelope result directly in the store (SetResult, not a full
	// row Update — Update would blank the job_id column because the test
	// step struct was materialized before SetJobID ran).
	if err := ts.stepStore.SetResult(step.ID, string(resultJSON)); err != nil {
		t.Fatalf("failed to seed step result: %v", err)
	}

	if err := ts.OnJobCompleted(t.Context(), "job-frew-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	// (a) In-memory: the step RESULT the rest of this invocation sees is
	// the rewritten output. (The `step` variable here is the caller's
	// struct from the harness; OnJobCompleted re-reads the persisted step
	// from the store, so assert on the reloaded row below for the durable
	// check — the in-memory copy this test holds is not the struct the
	// scheduler mutated.)

	// (b) Persisted: the reloaded row carries the rewritten output.
	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if persisted.Result != `{"fixed":true}` {
		t.Errorf("persisted result = %q, want the rewritten %q", persisted.Result, `{"fixed":true}`)
	}

	// (c) Logging contract: action=rewrite under stage=output_filter.
	if n := logLinesContaining(capture, "output filter action"); n == 0 {
		t.Fatalf("no %q log line recorded", "output filter action")
	}
	if n := logLinesContaining(capture, "action=rewrite"); n == 0 {
		t.Errorf("rewrite not logged with action=rewrite (Actions slice verbatim)")
	}
	if n := logLinesContaining(capture, "stage=output_filter"); n == 0 {
		t.Errorf("rewrite log line missing stage=output_filter")
	}

	// (d) Ordering probe: the evidence gate ran AFTER the filter (its
	// recorded trace entry follows the filter's).
	snap := trace.snapshot()
	filterIdx, validationIdx := -1, -1
	for i, entry := range snap {
		switch entry {
		case "filter:once_rewrite":
			filterIdx = i
		case "validation":
			validationIdx = i
		}
	}
	if filterIdx == -1 {
		t.Fatalf("filter never invoked (trace: %v)", snap)
	}
	if validationIdx == -1 {
		t.Fatalf("evidence validator never invoked after rewrite (trace: %v)", snap)
	}
	if filterIdx > validationIdx {
		t.Errorf("filter ran AFTER the evidence gate (trace: %v); order is FROZEN (master.md Contract 4)", snap)
	}

	// The step still completes normally after a rewrite.
	if persisted.State != task.StepCompleted {
		t.Errorf("step state = %q, want %q (a rewrite is not a rejection)", persisted.State, task.StepCompleted)
	}
}
