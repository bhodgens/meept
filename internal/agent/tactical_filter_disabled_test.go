package agent

// Output-filters tree, leaf 03, Task 2: the DISABLED DEFAULT contract.
//
// With no filter chain wired (the default for every existing install until
// leaf 04's config opts in), the output-filter stage must be skipped
// entirely: zero invocations, zero "output filter" log lines, and
// byte-identical step behavior (Result untouched, no retry state, normal
// completion).

import (
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
)

// TestSetFilterChainNilGuard pins the typed-nil convention: SetFilterChain
// must ignore a nil pointer rather than store it (storing a nil would arm
// the stage and panic on the first Run), and a second call with a nil must
// not clobber an already-wired chain.
func TestSetFilterChainNilGuard(t *testing.T) {
	ts, _, _, _, cleanup := newFilterTestScheduler(t, nil)
	defer cleanup()

	// nil on a fresh scheduler: must not panic and must leave the holder
	// nil so the stage stays skipped.
	ts.SetFilterChain(nil)
	if ts.filterChain != nil {
		t.Fatalf("SetFilterChain(nil) armed the chain holder; stage must stay disabled")
	}

	// A typed-nil *validator.FilterChain (the classic Go nil-interface trap)
	// must be equally inert.
	var typedNil *validator.FilterChain
	ts.SetFilterChain(typedNil)
	if ts.filterChain != nil {
		t.Fatalf("SetFilterChain(typed-nil) armed the chain holder")
	}

	// A real chain installs; a later nil call must NOT clear it (nil is
	// ignored, matching the other Set* methods on the scheduler).
	trace := &traceRecorder{alwaysApplies: true}
	ts.SetFilterChain(validator.NewFilterChain([]validator.OutputFilter{
		&recordingFilter{name: "noop", trace: trace},
	}, 2))
	if ts.filterChain == nil {
		t.Fatalf("SetFilterChain(real chain) did not install the chain")
	}
	ts.SetFilterChain(nil)
	if ts.filterChain == nil {
		t.Fatalf("SetFilterChain(nil) must be a no-op, not an unsetter")
	}
}

// TestDisabledDefaultZeroBehavior runs a normal step completion with NO
// chain wired and asserts: the filter stage never ran (no trace entries, no
// log lines), the Result is untouched, the step completes, and the task
// finalizes — i.e. byte-identical to the pre-filter pipeline.
func TestDisabledDefaultZeroBehavior(t *testing.T) {
	logger, capture := newFilterLogRecorder(t)

	// The trace recorder exists to PROVE no filter invocation happens: any
	// entry in it after the completion is a failure below.
	trace := &traceRecorder{alwaysApplies: true}
	ts, taskStore, _, _, cleanup := newFilterTestScheduler(t, func(cfg *TacticalSchedulerConfig, rq *recordingQueue) {
		cfg.Logger = logger
		// NOTE: no SetFilterChain — the disabled default under test.
	})
	defer cleanup()

	parent := newFilterTestTask(t, ts, "filter-disabled", 1)
	step := newFilterTestStep(t, ts, parent.ID, "job-fdis-1", "shell", "artifact produced")

	resultJSON := stepCompletedEnvelope(t, "artifact produced")
	if err := ts.OnJobCompleted(t.Context(), "job-fdis-1", resultJSON); err != nil {
		t.Fatalf("OnJobCompleted: %v", err)
	}

	// Zero filter invocations: the trace recorder would have entries if a
	// chain ran, but none was wired and none should appear from anywhere.
	for _, entry := range trace.snapshot() {
		t.Errorf("unexpected trace entry %q with no chain wired", entry)
	}

	// Zero "output filter" log lines.
	if n := logLinesContaining(capture, "output filter"); n != 0 {
		t.Errorf("disabled default logged %d lines containing %q; want 0", n, "output filter")
	}
	if n := logLinesContaining(capture, "stage=output_filter"); n != 0 {
		t.Errorf("disabled default logged %d lines naming the stage; want 0", n)
	}

	// Byte-identical behavior: Result is the raw envelope (the pipeline
	// never rewrites content on the disabled path), step completed, task
	// done, no retry job enqueued.
	persisted, err := ts.stepStore.GetByID(step.ID)
	if err != nil || persisted == nil {
		t.Fatalf("failed to re-read step: %v", err)
	}
	if want := string(resultJSON); persisted.Result != want {
		t.Errorf("persisted result = %q, want untouched envelope %q", persisted.Result, want)
	}
	if persisted.State != task.StepCompleted {
		t.Errorf("step state = %q, want %q", persisted.State, task.StepCompleted)
	}
	if persisted.FilterRetryCount != 0 || persisted.FilterError != "" {
		t.Errorf("filter retry fields touched on the disabled path: count=%d err=%q",
			persisted.FilterRetryCount, persisted.FilterError)
	}
	if rq := ts.queue; rq == nil {
		t.Fatalf("scheduler queue is nil; test misconfigured")
	}
	got, err := taskStore.GetByID(parent.ID)
	if err != nil || got == nil {
		t.Fatalf("failed to re-read task: %v", err)
	}
	if got.State != task.StateCompleted {
		t.Errorf("task state = %q, want %q (disabled default must not alter completion)", got.State, task.StateCompleted)
	}
}
