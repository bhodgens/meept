package agent

// Output-filters tree, leaf 03, Task 5: the ordering-contract regression
// guard.
//
// master.md Contract 4 (FROZEN): the post-step pipeline order is
//
//	1. step job result arrives
//	2. claim-vs-evidence marking
//	3. OUTPUT FILTER CHAIN
//	4. evidence validation gate
//	5. ReviewStep policy/reviewer
//	6. adversarial verification
//
// Filters run STRICTLY before the evidence gate — the filter repairs
// content, the gate validates side-effects, and the order never swaps. This
// test records both stages into one shared trace and fails the moment a
// future edit moves the filter block after the validation gate (or the gate
// before the filter). It is intentionally redundant with the ordering probe
// in tactical_filter_rewrite_test.go: that one pins rewrite-then-validate
// for Task 3, this one pins the CONTRACT itself.

import (
	"testing"

	"github.com/caimlas/meept/internal/validator"
)

func TestTaskServiceFilterBeforeValidationOrder(t *testing.T) {
	trace := &traceRecorder{alwaysApplies: true}
	ts, _, _, _, cleanup := newFilterTestScheduler(t, func(cfg *TacticalSchedulerConfig, rq *recordingQueue) {
		cfg.ValidatorManager = validator.NewValidatorManager()
		// Sequence-recording evidence validator for the step's hint.
		cfg.ValidatorManager.RegisterValidator("shell", &recordingValidator{trace: trace})
	})
	defer cleanup()
	// Sequence-recording filter chain: pass-through (a rejection would
	// requeue and skip the gate entirely, which would defeat the probe).
	ts.SetFilterChain(validator.NewFilterChain([]validator.OutputFilter{
		&recordingFilter{name: "json_format", trace: trace},
	}, 2))

	parent := newFilterTestTask(t, ts, "filter-order", 1)
	step := newFilterTestStep(t, ts, parent.ID, "job-ford-1", "shell", "artifact produced")
	env := stepCompletedEnvelope(t, "artifact produced")
	if err := ts.stepStore.SetResult(step.ID, string(env)); err != nil {
		t.Fatalf("failed to seed step result: %v", err)
	}

	if err := ts.OnJobCompleted(t.Context(), "job-ford-1", env); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	// The recorded global sequence must be exactly
	// [filter..., validation...] — master.md Contract 4, gate order FROZEN.
	snap := trace.snapshot()
	var filterIdxs, validationIdxs []int
	for i, entry := range snap {
		switch {
		case entry == "filter:json_format":
			filterIdxs = append(filterIdxs, i)
		case entry == "validation":
			validationIdxs = append(validationIdxs, i)
		}
	}
	if len(filterIdxs) == 0 {
		t.Fatalf("output filter chain never ran (trace: %v)", snap)
	}
	if len(validationIdxs) == 0 {
		t.Fatalf("evidence validation gate never ran (trace: %v)", snap)
	}
	lastFilter := filterIdxs[len(filterIdxs)-1]
	firstValidation := validationIdxs[0]
	if lastFilter > firstValidation {
		t.Fatalf("GATE ORDER VIOLATED (master.md Contract 4 is FROZEN): filter ran at trace index %d AFTER validation at index %d (trace: %v)",
			lastFilter, firstValidation, snap)
	}
	// And no validation invocation may precede ANY filter invocation: the
	// filter stage sits entirely before the gate, not interleaved.
	for _, vi := range validationIdxs {
		for _, fi := range filterIdxs {
			if vi < fi {
				t.Fatalf("GATE ORDER VIOLATED (master.md Contract 4 is FROZEN): validation at trace index %d ran before filter at index %d (trace: %v)",
					vi, fi, snap)
			}
		}
	}
}
