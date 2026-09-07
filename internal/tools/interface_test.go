package tools

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
)

func TestToolDefaults(t *testing.T) {
	d := ToolDefaults{}
	assert.False(t, d.IsReadOnly(nil), "default IsReadOnly should be false")
	assert.False(t, d.IsConcurrencySafe(nil), "default IsConcurrencySafe should be false")
	assert.False(t, d.IsReadOnly(map[string]any{"key": "val"}), "default IsReadOnly with input should be false")
	assert.False(t, d.IsConcurrencySafe(map[string]any{"key": "val"}), "default IsConcurrencySafe with input should be false")
}

// resultSizerStub implements ResultSizer with a declared floor.
type resultSizerStub struct {
	ToolDefaults
	floor int
}

func (s resultSizerStub) Name() string                       { return "result_sizer_stub" }
func (s resultSizerStub) Description() string                { return "stub" }
func (s resultSizerStub) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (s resultSizerStub) Execute(ctx context.Context, args map[string]any) (any, error) {
	return nil, nil
}
func (s resultSizerStub) MaxResultTokens() int { return s.floor }

// plainToolStub does NOT implement ResultSizer.
type plainToolStub struct {
	ToolDefaults
}

func (s plainToolStub) Name() string                       { return "plain_tool_stub" }
func (s plainToolStub) Description() string                { return "stub" }
func (s plainToolStub) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (s plainToolStub) Execute(ctx context.Context, args map[string]any) (any, error) {
	return nil, nil
}

func TestGetMaxResultTokens(t *testing.T) {
	t.Run("tool without ResultSizer returns 0", func(t *testing.T) {
		assert.Equal(t, 0, GetMaxResultTokens(plainToolStub{}))
	})

	t.Run("negative floor returns 0", func(t *testing.T) {
		assert.Equal(t, 0, GetMaxResultTokens(resultSizerStub{floor: -5}))
	})

	t.Run("zero floor returns 0", func(t *testing.T) {
		assert.Equal(t, 0, GetMaxResultTokens(resultSizerStub{floor: 0}))
	})

	t.Run("declared floor 1400 returned as-is", func(t *testing.T) {
		// The raw declared value is returned uncapped: ToolResultMaxTokens
		// lives in internal/agent and the tools package MUST NOT import
		// agent (import cycle), so the caller caps, not this helper.
		assert.Equal(t, 1400, GetMaxResultTokens(resultSizerStub{floor: 1400}))
	})
}
