package agent

// Pin test (docs/plans/20261101-security-gate-followups/02): streaming tool
// calls must pass the centralized gate bracket in executeToolWithRetry.
// GateCheck already runs BEFORE the retry loop whose StreamingTool branch
// calls ExecuteStreaming; this test records the execution sequence so a
// future refactor that moves the streaming branch ahead of GateCheck fails
// loudly. The mutation proof lives in the leaf doc (T2): moving GateCheck
// after the loop makes this test RED with a sequence assertion error.

import (
	"context"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sequenceRecorder collects ordered event labels from the stubs below.
type sequenceRecorder struct {
	mu    sync.Mutex
	steps []string
}

func (r *sequenceRecorder) record(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}

func (r *sequenceRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.steps))
	copy(out, r.steps)
	return out
}

// gatedStreamingStub is both a tools.Tool and a tools.StreamingTool. Its
// streaming body records its own label so the test can order it against the
// gate calls.
type gatedStreamingStub struct {
	seq *sequenceRecorder
}

func (t *gatedStreamingStub) Name() string        { return "fake_streaming" }
func (t *gatedStreamingStub) Description() string { return "test streaming stub" }
func (t *gatedStreamingStub) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object"}
}
func (t *gatedStreamingStub) IsReadOnly(input map[string]any) bool        { return true }
func (t *gatedStreamingStub) IsConcurrencySafe(input map[string]any) bool { return true }
func (t *gatedStreamingStub) Execute(ctx context.Context, args map[string]any) (any, error) {
	t.seq.record("execute_body")
	return "non-streaming", nil
}
func (t *gatedStreamingStub) ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(tools.ProgressUpdate)) (any, error) {
	t.seq.record("stream_body")
	onUpdate(tools.ProgressUpdate{Message: "working", Percent: 50})
	return "streamed", nil
}

// gatedRegistryStub implements ToolRegistry + GatedExecutionRegistry,
// recording gate events in sequence.
type gatedRegistryStub struct {
	seq *sequenceRecorder
}

func (r *gatedRegistryStub) Get(name string) tools.Tool           { return nil }
func (r *gatedRegistryStub) List() []tools.Tool                   { return nil }
func (r *gatedRegistryStub) GetDefinitions() []llm.ToolDefinition { return nil }
func (r *gatedRegistryStub) GateCheck(ctx context.Context, name string, tool tools.Tool, args map[string]any) error {
	r.seq.record("gate_check")
	return nil
}
func (r *gatedRegistryStub) GateComplete(ctx context.Context, name string, tool tools.Tool, args map[string]any, result any, execErr error) {
	r.seq.record("gate_complete")
}

func TestStreamingCallPassesGateCheck(t *testing.T) {
	seq := &sequenceRecorder{}
	tool := &gatedStreamingStub{seq: seq}
	reg := &gatedRegistryStub{seq: seq}

	e := NewExecutor(nil, nil, WithExecutorBus(bus.New(nil, nil)))

	res, err := e.executeToolWithRetry(
		context.Background(), tool, BackoffConfig{},
		"call-stream-1", "fake_streaming", map[string]any{}, reg,
	)
	require.NoError(t, err)
	assert.Equal(t, "streamed", res, "streaming result must be returned")

	// The pin: GateCheck ran BEFORE the streaming body, and GateComplete
	// observed the streaming result afterwards. If a refactor moves the
	// StreamingTool branch ahead of GateCheck, this ordering breaks.
	steps := seq.snapshot()
	require.Len(t, steps, 3, "expected gate_check -> stream_body -> gate_complete, got %v", steps)
	assert.Equal(t, []string{"gate_check", "stream_body", "gate_complete"}, steps,
		"GateCheck must run before the streaming body and GateComplete after it")
}
