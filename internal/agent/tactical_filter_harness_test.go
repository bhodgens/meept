package agent

// Test harness shared by the output-filter gate tests (output-filters tree,
// leaf 03, Tasks 2-5). The stubs here are SEQUENCE RECORDERS: every
// invocation appends a marker to a shared slice so the frozen gate order
// (filter before evidence validation) and the retry-class independence can
// be asserted from the recorded trace.

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
)

// newFilterTestScheduler builds a TacticalScheduler over a temp task store,
// a live bus, and a recording queue, letting the caller wire the
// validator/review managers and the filter chain. It mirrors
// newGateTestScheduler (tactical_validation_gate_test.go) but adds the
// recording queue the filter-retry tests need to observe requeues.
func newFilterTestScheduler(t *testing.T, configure func(cfg *TacticalSchedulerConfig, rq *recordingQueue)) (*TacticalScheduler, *task.Store, *bus.MessageBus, *recordingQueue, func()) {
	t.Helper()

	taskStore, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	if err != nil {
		t.Fatalf("failed to create task store: %v", err)
	}

	msgBus := bus.New(nil, slogDiscardLogger())
	rq := &recordingQueue{}
	cfg := TacticalSchedulerConfig{
		StepStore: taskStore.StepStore(),
		TaskStore: taskStore,
		Bus:       msgBus,
		Queue:     rq,
		Logger:    slogDiscardLogger(),
	}
	if configure != nil {
		configure(&cfg, rq)
	}
	ts := NewTacticalScheduler(cfg)

	return ts, taskStore, msgBus, rq, func() { taskStore.Close() }
}

// recordingQueue is a mockQueue that records every Enqueue call, so the
// filter-retry tests can assert a retry job was created (and read its
// payload back).
type recordingQueue struct {
	mockQueue

	mu      sync.Mutex
	enqJob  *queue.Job
	enqErr  error
	enqList []*queue.Job
}

func (q *recordingQueue) Enqueue(_ context.Context, job *queue.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.enqErr != nil {
		return q.enqErr
	}
	q.enqJob = job
	q.enqList = append(q.enqList, job)
	return nil
}

// lastEnqueuedJob returns the most recent job passed to Enqueue (nil if none).
func (q *recordingQueue) lastEnqueuedJob() *queue.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.enqList) == 0 {
		return nil
	}
	return q.enqList[len(q.enqList)-1]
}

// recordingFilter is a stub validator.OutputFilter that records its own
// invocations into the shared trace and returns a scripted outcome. A
// non-empty hintScope restricts Applies to steps with that tool hint
// (mirroring how a real content filter scopes itself); with an empty scope
// it applies to every step.
type recordingFilter struct {
	name      string
	trace     *traceRecorder
	rewrite   string // non-empty => FilterRewrite with this output
	fail      string // non-empty => FilterFail with this reason
	hintScope string // non-empty => applies only to steps with this hint
}

func (f *recordingFilter) Name() string { return f.name }
func (f *recordingFilter) Applies(step *task.TaskStep) bool {
	if f.hintScope != "" {
		return step.ToolHint == f.hintScope
	}
	return f.rewrite != "" || f.fail != "" || f.trace.alwaysApplies
}
func (f *recordingFilter) Process(_ context.Context, _ *task.TaskStep, _ string) validator.FilterResult {
	f.trace.record("filter:" + f.name)
	switch {
	case f.fail != "":
		return validator.FilterResult{Outcome: validator.FilterFail, Filter: f.name, Reason: f.fail}
	case f.rewrite != "":
		return validator.FilterResult{Outcome: validator.FilterRewrite, Filter: f.name, Output: f.rewrite}
	default:
		return validator.FilterResult{Outcome: validator.FilterPass, Filter: f.name}
	}
}

// onceRewriteFilter is a stub OutputFilter that rewrites on its first
// invocation and passes on every later one — the idempotent "repair once,
// then converge" shape the chain semantics expect (parent Contract 2: a
// rewrite re-runs the sweep with the new output; a filter that keeps
// rewriting forever triggers the non-convergence fail).
type onceRewriteFilter struct {
	trace *traceRecorder
	runs  int
	once  sync.Once
}

func (f *onceRewriteFilter) Name() string { return "once_rewrite" }
func (f *onceRewriteFilter) Applies(_ *task.TaskStep) bool {
	return f.trace.alwaysApplies
}
func (f *onceRewriteFilter) Process(_ context.Context, _ *task.TaskStep, output string) validator.FilterResult {
	f.trace.record("filter:" + f.Name())
	first := false
	f.once.Do(func() { first = true })
	if first {
		f.runs++
		return validator.FilterResult{Outcome: validator.FilterRewrite, Filter: f.Name(), Output: `{"fixed":true}`}
	}
	return validator.FilterResult{Outcome: validator.FilterPass, Filter: f.Name()}
}

// recordingValidator is a stub evidence Validator recording into the same
// trace. It can be registered on a real ValidatorManager per tool hint.
type recordingValidator struct {
	trace *traceRecorder
	fail  bool
}

func (v *recordingValidator) Validate(_ context.Context, _ *task.TaskStep) validator.ValidationResult {
	v.trace.record("validation")
	if v.fail {
		return validator.ValidationResult{Valid: false, Errors: []string{"evidence missing"}}
	}
	return validator.ValidationResult{Valid: true}
}

// traceRecorder is the shared, mutex-guarded sequence log.
type traceRecorder struct {
	mu            sync.Mutex
	entries       []string
	alwaysApplies bool
}

func (tr *traceRecorder) record(entry string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.entries = append(tr.entries, entry)
}

func (tr *traceRecorder) snapshot() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	out := make([]string, len(tr.entries))
	copy(out, tr.entries)
	return out
}

// stepCompletedEnvelope marshals the daemon-shaped success envelope with the
// given result text and one tool-issued evidence entry (so the evidence gate
// has something to validate).
func stepCompletedEnvelope(t *testing.T, result string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"success": true,
		"result":  result,
		"tool_evidence": []map[string]any{
			{"type": "shell_output", "subject": "echo ok", "value": "ok"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// newFilterTestStep mirrors newGateTestStep: a scheduled step bound to jobID.
func newFilterTestStep(t *testing.T, ts *TacticalScheduler, taskID, jobID, hint, result string) *task.TaskStep {
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

// newFilterTestTask mirrors newGateTestTask: an executing task with totalJobs.
func newFilterTestTask(t *testing.T, ts *TacticalScheduler, name string, totalJobs int) *task.Task {
	t.Helper()
	tk := task.NewTask(name, "filter pin task")
	tk.TotalJobs = totalJobs
	tk.SetState(task.StateExecuting)
	if err := ts.taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	return tk
}

// filterLogRecorder adapts the existing capturingLogger (loop_restore_test.go)
// to capture slog output for the logging-contract assertions below.
func newFilterLogRecorder(t *testing.T) (*slog.Logger, *capturingLogger) {
	t.Helper()
	cl := newCapturingLogger()
	return cl.log, cl
}

// logLinesContaining counts captured log lines containing needle.
func logLinesContaining(cl *capturingLogger, needle string) int {
	return cl.countContaining(needle)
}
