package tools

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeToolWith builds a minimal Tool with the given schema for
// ValidateToolArgs tests.
func fakeToolWith(required []string, props map[string]llm.ParameterProperty) Tool {
	return &mockTool{
		name:        "fake",
		description: "fake tool for arg validation tests",
		params: llm.FunctionParameters{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	}
}

func TestValidateToolArgs_MissingRequired(t *testing.T) {
	tool := fakeToolWith([]string{"name"}, map[string]llm.ParameterProperty{"name": {Type: "string"}})
	err := ValidateToolArgs(tool, map[string]any{})
	require.Error(t, err)
	var ae *ArgValidationError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "fake", ae.Tool)
	assert.Equal(t, "name", ae.Arg)
	assert.Equal(t, "missing", ae.Problem)
}

func TestValidateToolArgs_EmptyString(t *testing.T) {
	tool := fakeToolWith([]string{"name"}, map[string]llm.ParameterProperty{"name": {Type: "string"}})
	err := ValidateToolArgs(tool, map[string]any{"name": "  "})
	require.Error(t, err)
	var ae *ArgValidationError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "name", ae.Arg)
	assert.Equal(t, "empty", ae.Problem)
}

func TestValidateToolArgs_NilValue(t *testing.T) {
	tool := fakeToolWith([]string{"name"}, map[string]llm.ParameterProperty{"name": {Type: "string"}})
	err := ValidateToolArgs(tool, map[string]any{"name": nil})
	require.Error(t, err)
	var ae *ArgValidationError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "missing", ae.Problem)
}

func TestValidateToolArgs_WrongType(t *testing.T) {
	tool := fakeToolWith([]string{"count"}, map[string]llm.ParameterProperty{"count": {Type: "string"}})
	err := ValidateToolArgs(tool, map[string]any{"count": float64(3)})
	require.Error(t, err)
	var ae *ArgValidationError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "wrong type: want string, got number", ae.Problem)
}

func TestValidateToolArgs_TypedNilString(t *testing.T) {
	tool := fakeToolWith([]string{"name"}, map[string]llm.ParameterProperty{"name": {Type: "string"}})
	var sp *string
	err := ValidateToolArgs(tool, map[string]any{"name": sp})
	require.Error(t, err)
	var ae *ArgValidationError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "missing", ae.Problem)
}

func TestValidateToolArgs_TypedNilObject(t *testing.T) {
	tool := fakeToolWith([]string{"cfg"}, map[string]llm.ParameterProperty{"cfg": {Type: "object"}})
	type cfg map[string]any
	var m cfg
	err := ValidateToolArgs(tool, map[string]any{"cfg": m})
	require.Error(t, err)
	var ae *ArgValidationError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "missing", ae.Problem)
}

func TestValidateToolArgs_ArrayAndObjectTypes(t *testing.T) {
	tool := fakeToolWith(
		[]string{"items", "conf"},
		map[string]llm.ParameterProperty{
			"items": {Type: "array"},
			"conf":  {Type: "object"},
		},
	)
	err := ValidateToolArgs(tool, map[string]any{
		"items": []any{"a", "b"},
		"conf":  map[string]any{"k": "v"},
	})
	assert.NoError(t, err)
}

func TestValidateToolArgs_NumberAndBooleanTypes(t *testing.T) {
	tool := fakeToolWith(
		[]string{"ratio", "flag"},
		map[string]llm.ParameterProperty{
			"ratio": {Type: "number"},
			"flag":  {Type: "boolean"},
		},
	)
	assert.NoError(t, ValidateToolArgs(tool, map[string]any{"ratio": 1.5, "flag": true}))

	err := ValidateToolArgs(tool, map[string]any{"ratio": "high", "flag": true})
	require.Error(t, err)
	var ae *ArgValidationError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "ratio", ae.Arg)
}

func TestValidateToolArgs_UndeclaredRequiredRejected(t *testing.T) {
	// Required names not present in Properties are still enforced
	// (schema drift defense), with an unknown expected type.
	tool := fakeToolWith([]string{"mystery"}, map[string]llm.ParameterProperty{})
	err := ValidateToolArgs(tool, map[string]any{})
	require.Error(t, err)
	var ae *ArgValidationError
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "mystery", ae.Arg)
	assert.Equal(t, "missing", ae.Problem)
}

func TestValidateToolArgs_NoRequiredPasses(t *testing.T) {
	tool := fakeToolWith(nil, map[string]llm.ParameterProperty{})
	assert.NoError(t, ValidateToolArgs(tool, map[string]any{}))
}

func TestValidateToolArgs_ExtraKeysAllowed(t *testing.T) {
	tool := fakeToolWith([]string{"name"}, map[string]llm.ParameterProperty{"name": {Type: "string"}})
	err := ValidateToolArgs(tool, map[string]any{"name": "ok", "extra": "forward-compat"})
	assert.NoError(t, err)
}

func TestArgValidationError_Message(t *testing.T) {
	ae := &ArgValidationError{Tool: "task_create", Arg: "name", Problem: "missing"}
	assert.Equal(t,
		"task_create: name is missing (required by the tool schema; pass it exactly as named)",
		ae.Error())
}
