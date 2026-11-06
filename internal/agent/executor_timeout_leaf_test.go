package agent

// TDD unit tests for dsh-convergence leaf 04-tool-surface/01-tool-timeout
// (T1-T5 per the leaf's Given/When/Then shapes). The orchestrator's parked
// gate tests (executor_timeout_test.go.staged) cover the same contract via
// executeToolWithRetry; these exercise the LIVE Execute envelope path
// (ErrCode "TOOL_TIMEOUT", IsBreakableRepeat false) plus the zero-budget
// unadorned-ctx case (T4).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sleepTimeoutTool embeds ToolDefaults and overrides only TimeoutMs.
type sleepTimeoutTool struct {
	tools.ToolDefaults
	sleep time.Duration
}

func (t *sleepTimeoutTool) Name() string                       { return "file_read" }
func (t *sleepTimeoutTool) Description() string                { return "probe" }
func (t *sleepTimeoutTool) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (t *sleepTimeoutTool) IsReadOnly(map[string]any) bool     { return true }
func (t *sleepTimeoutTool) IsConcurrencySafe(map[string]any) bool {
	return true
}
func (t *sleepTimeoutTool) TimeoutMs() time.Duration { return 100 * time.Millisecond }
func (t *sleepTimeoutTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	select {
	case <-time.After(t.sleep):
		return "finished", nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// T3: declared budget expires -> structured TOOL_TIMEOUT result through the
// live Execute path: Success false, ErrCode "TOOL_TIMEOUT", message names
// the budget, IsBreakableRepeat false, error routeable via errors.Is.
func TestExecute_Timeout_StructuredTOOL_TIMEOUT(t *testing.T) {
	reg := NewPlaceholderToolRegistry()
	reg.Register(&sleepTimeoutTool{sleep: 2 * time.Second})
	ex := NewExecutor(reg, security.NewPermissionChecker(security.Config{}))
	start := time.Now()
	res := ex.Execute(context.Background(), llm.ToolCall{
		ID: "c1",
		Function: llm.ToolCallFunction{
			Name:      "file_read",
			Arguments: "{}",
		},
	})
	elapsed := time.Since(start)

	require.NotNil(t, res)
	assert.False(t, res.Success)
	assert.Equal(t, "TOOL_TIMEOUT", res.ErrCode)
	assert.False(t, res.IsBreakableRepeat, "a timeout must not feed the repeat-error breaker")
	assert.True(t, strings.Contains(res.Error, "timed out after"),
		"message %q must carry the budget", res.Error)
	assert.Less(t, elapsed, 1500*time.Millisecond, "returned near the 100ms budget, not after the 2s sleep")
	assert.False(t, strings.Contains(res.Error, context.DeadlineExceeded.Error()),
		"raw context.DeadlineExceeded leaked: %q", res.Error)
}

// T3 sentinel routeability: the substituted error classifies via errors.Is.
func TestExecute_Timeout_ErrorIsSentinel(t *testing.T) {
	tool := &sleepTimeoutTool{sleep: 2 * time.Second}
	ex := NewExecutor(nil, nil)
	_, err := ex.executeToolWithRetry(context.Background(), tool, BackoffConfig{}, "c2", "timeout_probe", map[string]any{}, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, tools.ErrToolTimeout), "got %v", err)
}

// T4: a zero-budget (ToolDefaults default) tool dispatches with the upstream
// ctx UNADORNED — no derived deadline on the ctx the tool receives.
func TestExecute_ZeroBudget_NoDeadlineArmed(t *testing.T) {
	probe := &deadlineRecordingTool{done: make(chan context.Context, 1)}
	ex := NewExecutor(nil, nil)
	_, err := ex.executeToolWithRetry(context.Background(), probe, BackoffConfig{}, "c3", "zero_probe", map[string]any{}, nil)
	require.NoError(t, err)
	got := <-probe.done
	_, hasDeadline := got.Deadline()
	assert.False(t, hasDeadline, "executor armed a deadline for a tool that declared no budget")
}

type deadlineRecordingTool struct {
	tools.ToolDefaults
	done chan context.Context
}

func (t *deadlineRecordingTool) Name() string                       { return "zero_probe" }
func (t *deadlineRecordingTool) Description() string                { return "probe" }
func (t *deadlineRecordingTool) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (t *deadlineRecordingTool) IsReadOnly(map[string]any) bool     { return true }
func (t *deadlineRecordingTool) IsConcurrencySafe(map[string]any) bool {
	return true
}
func (t *deadlineRecordingTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	t.done <- ctx
	return "ran", nil
}
