package tools

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingFakeTool records how many times Execute ran, so tests can pin
// that the registry validation gate never invokes the tool on invalid args.
type countingFakeTool struct {
	mockTool
	runCount int
}

func (c *countingFakeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	c.runCount++
	return map[string]any{"ok": true}, nil
}

func TestExecute_RejectsMissingRequiredBeforeToolRun(t *testing.T) {
	reg := NewRegistry(nil)
	fake := &countingFakeTool{mockTool: mockTool{
		name: "fake",
		params: llm.FunctionParameters{
			Type:       "object",
			Properties: map[string]llm.ParameterProperty{"name": {Type: "string"}},
			Required:   []string{"name"},
		},
	}}
	reg.Register(fake)

	res, err := reg.Execute(context.Background(), "fake", map[string]any{})
	require.Nil(t, err) // error goes in the envelope, not the error return
	require.NotNil(t, res)
	assert.False(t, res.Success)
	assert.Equal(t, "invalid_args", res.ErrCode)
	assert.Contains(t, res.Error, "name is missing")
	assert.Equal(t, 0, fake.runCount) // tool NEVER ran
}

func TestExecute_RejectsEmptyRequiredBeforeToolRun(t *testing.T) {
	reg := NewRegistry(nil)
	fake := &countingFakeTool{mockTool: mockTool{
		name: "fake",
		params: llm.FunctionParameters{
			Type:       "object",
			Properties: map[string]llm.ParameterProperty{"name": {Type: "string"}},
			Required:   []string{"name"},
		},
	}}
	reg.Register(fake)

	res, err := reg.Execute(context.Background(), "fake", map[string]any{"name": "   "})
	require.Nil(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "invalid_args", res.ErrCode)
	assert.Contains(t, res.Error, "name is empty")
	assert.Equal(t, 0, fake.runCount)
}

func TestExecute_ValidArgsRunNormally(t *testing.T) {
	reg := NewRegistry(nil)
	fake := &countingFakeTool{mockTool: mockTool{
		name: "fake",
		params: llm.FunctionParameters{
			Type:       "object",
			Properties: map[string]llm.ParameterProperty{"name": {Type: "string"}},
			Required:   []string{"name"},
		},
	}}
	reg.Register(fake)

	res, err := reg.Execute(context.Background(), "fake", map[string]any{"name": "fine"})
	require.Nil(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)
	assert.Equal(t, "", res.ErrCode) // zero value on the normal path
	assert.Equal(t, 1, fake.runCount)
}

func TestExecute_NoRequiredSchemaUnaffected(t *testing.T) {
	// Tools whose schema declares nothing required must be fully
	// backwards-compatible: empty args still run.
	reg := NewRegistry(nil)
	fake := &countingFakeTool{mockTool: mockTool{
		name: "fake",
		params: llm.FunctionParameters{
			Type:       "object",
			Properties: map[string]llm.ParameterProperty{},
		},
	}}
	reg.Register(fake)

	res, err := reg.Execute(context.Background(), "fake", map[string]any{})
	require.Nil(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Success)
	assert.Equal(t, 1, fake.runCount)
}

func TestExecute_ValidationErrIsTyped(t *testing.T) {
	reg := NewRegistry(nil)
	fake := &countingFakeTool{mockTool: mockTool{
		name: "fake",
		params: llm.FunctionParameters{
			Type:       "object",
			Properties: map[string]llm.ParameterProperty{"name": {Type: "string"}},
			Required:   []string{"name"},
		},
	}}
	reg.Register(fake)

	res, err := reg.Execute(context.Background(), "fake", map[string]any{})
	require.Nil(t, err)
	require.NotNil(t, res)
	var ae *ArgValidationError
	require.ErrorAs(t, res.Err, &ae)
	assert.Equal(t, "name", ae.Arg)
}
