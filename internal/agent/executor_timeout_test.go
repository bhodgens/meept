package agent

// F9 gate T1/T1b executor arm (04-tool-surface/00T-tests.md): the live
// dispatch wrapper (executeToolWithRetry) arms a per-tool deadline ONLY when
// TimeoutMs() > 0, substitutes the structured TOOL_TIMEOUT error on expiry,
// and never swallows turn-level cancellation. Pins contract B1
// (.hermes/contracts/04-tool-surface.md).
//
// Activated from the parked gate file (executor_timeout_test.go.staged,
// 762dc9d3). Drift fixed vs the staged draft: the deadlineProbeTool's seen
// channel typed as chan (struct{...}) (invalid parenthesized element type)
// is now a proper struct channel; the timeout-shape assertions target the
// executor's real substitution (a wrapped tools.ErrToolTimeout error whose
// message names the declared budget and never leaks a raw
// context.DeadlineExceeded string) — the ErrCode "TOOL_TIMEOUT" envelope
// path through the LIVE Execute is already pinned by
// executor_timeout_leaf_test.go (T3).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seenDeadline is what the probe's Execute observed about its context.
type seenDeadline struct {
	hasDeadline bool
	deadline    time.Time
}

// deadlineProbeTool records the deadline its Execute context carries.
type deadlineProbeTool struct {
	tools.ToolDefaults
	timeout time.Duration
	seen    chan seenDeadline
}

func (t *deadlineProbeTool) Name() string                       { return "deadline_probe" }
func (t *deadlineProbeTool) Description() string                { return "probe" }
func (t *deadlineProbeTool) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (t *deadlineProbeTool) IsReadOnly(map[string]any) bool     { return true }
func (t *deadlineProbeTool) IsConcurrencySafe(map[string]any) bool {
	return true
}
func (t *deadlineProbeTool) TimeoutMs() time.Duration { return t.timeout }
func (t *deadlineProbeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	dl, ok := ctx.Deadline()
	t.seen <- seenDeadline{ok, dl}
	return "ran", nil
}

// sleepingTool blocks longer than its declared budget.
type sleepingTool struct {
	tools.ToolDefaults
	timeout time.Duration
	sleep   time.Duration
}

func (t *sleepingTool) Name() string                       { return "sleeping_probe" }
func (t *sleepingTool) Description() string                { return "probe" }
func (t *sleepingTool) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (t *sleepingTool) IsReadOnly(map[string]any) bool     { return true }
func (t *sleepingTool) IsConcurrencySafe(map[string]any) bool {
	return true
}
func (t *sleepingTool) TimeoutMs() time.Duration { return t.timeout }
func (t *sleepingTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	select {
	case <-time.After(t.sleep):
		return "finished", nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestExecutor_NoDeclaredTimeout_ArmsNoDeadline(t *testing.T) {
	// TimeoutMs()==0 (ToolDefaults semantics): the executor must NOT derive
	// a new deadline; the tool runs unchanged.
	probe := &deadlineProbeTool{timeout: 0, seen: make(chan seenDeadline, 1)}
	e := NewExecutor(nil, nil)
	res, err := e.executeToolWithRetry(context.Background(), probe, BackoffConfig{}, "call-t1", "deadline_probe", map[string]any{}, nil)
	require.NoError(t, err)
	assert.Equal(t, "ran", res)

	got := <-probe.seen
	assert.False(t, got.hasDeadline, "executor armed a deadline for a tool that declared none (opt-in per tool)")
}

func TestExecutor_DeclaredTimeout_ToolsWithinBudgetSucceed(t *testing.T) {
	// A declared budget larger than the tool's runtime: success, and the
	// parent context keeps NO deadline after the call (the derived deadline
	// must not leak onto the caller's ctx).
	probe := &deadlineProbeTool{timeout: time.Second, seen: make(chan seenDeadline, 1)}
	e := NewExecutor(nil, nil)
	parent := context.Background()
	res, err := e.executeToolWithRetry(parent, probe, BackoffConfig{}, "call-t1b-ok", "deadline_probe", map[string]any{}, nil)
	require.NoError(t, err)
	assert.Equal(t, "ran", res)

	got := <-probe.seen
	assert.True(t, got.hasDeadline, "declared TimeoutMs>0 must arm a derived deadline")
	if _, leaked := parent.Deadline(); leaked {
		t.Error("derived deadline leaked onto the parent context")
	}
}

func TestExecutor_ToolTimeout_StructuredResult(t *testing.T) {
	// T1b: expiry substitutes the structured TOOL_TIMEOUT error, never a
	// raw context.DeadlineExceeded string.
	tool := &sleepingTool{timeout: 100 * time.Millisecond, sleep: 2 * time.Second}
	e := NewExecutor(nil, nil)
	res, err := e.executeToolWithRetry(context.Background(), tool, BackoffConfig{}, "call-t1b", "sleeping_probe", map[string]any{}, nil)

	require.Error(t, err)
	assert.Nil(t, res)
	assert.True(t, errors.Is(err, tools.ErrToolTimeout),
		"errors.Is(err, tools.ErrToolTimeout) = false; got %v", err)
	msg := err.Error()
	assert.True(t, strings.Contains(msg, "timed out after 100ms") || strings.Contains(msg, "timed out after 0.1s"),
		"message %q must name the declared budget", msg)
	assert.False(t, strings.Contains(msg, context.DeadlineExceeded.Error()),
		"raw context.DeadlineExceeded string leaked: %q", msg)
}

func TestExecutor_TurnCancellation_NotSwallowed(t *testing.T) {
	// The wrapper must NOT convert turn-level cancellation into a tool
	// timeout: cancelling the parent ctx yields context.Canceled, and
	// errors.Is(_, tools.ErrToolTimeout) stays false.
	tool := &sleepingTool{timeout: 5 * time.Second, sleep: 5 * time.Second}
	e := NewExecutor(nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	res, err := e.executeToolWithRetry(ctx, tool, BackoffConfig{}, "call-cancel", "sleeping_probe", map[string]any{}, nil)
	require.Error(t, err)
	assert.Nil(t, res)
	assert.True(t, errors.Is(err, context.Canceled), "turn cancellation must surface as context.Canceled, got %v", err)
	assert.False(t, errors.Is(err, tools.ErrToolTimeout),
		"turn-level cancellation was misclassified as a tool timeout")
}
