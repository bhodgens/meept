package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingRequiredTool fails the execution if it ever runs; the gate must
// reject before Execute so the counter stays zero.
type countingRequiredTool struct {
	runs atomic.Int32
}

func (t *countingRequiredTool) Name() string        { return "fake_required" }
func (t *countingRequiredTool) Description() string { return "test" }
func (t *countingRequiredTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"name": {Type: "string"},
		},
		Required: []string{"name"},
	}
}
func (t *countingRequiredTool) IsConcurrencySafe(args map[string]any) bool { return true }
func (t *countingRequiredTool) IsReadOnly(input map[string]any) bool       { return false }
func (t *countingRequiredTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	t.runs.Add(1)
	return "ran", nil
}

func TestExecutor_ArgGateRejectsBeforeExecute(t *testing.T) {
	tool := &countingRequiredTool{}
	e := NewExecutor(nil, nil)
	res, err := e.executeToolWithRetry(context.Background(), tool, BackoffConfig{}, "call-1", "fake_required", map[string]any{})
	require.Error(t, err)
	assert.Nil(t, res)
	var ae *tools.ArgValidationError
	require.True(t, errors.As(err, &ae), "error must be ArgValidationError, got %v", err)
	assert.Equal(t, "name", ae.Arg)
	assert.Equal(t, int32(0), tool.runs.Load(), "tool must never run after a gate rejection")
}

func TestExecutor_ArgGateAllowsValidArgs(t *testing.T) {
	tool := &countingRequiredTool{}
	e := NewExecutor(nil, nil)
	res, err := e.executeToolWithRetry(context.Background(), tool, BackoffConfig{}, "call-2", "fake_required", map[string]any{"name": "ok"})
	require.NoError(t, err)
	assert.Equal(t, "ran", res)
	assert.Equal(t, int32(1), tool.runs.Load())
}
