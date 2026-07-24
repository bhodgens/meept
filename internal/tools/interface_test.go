package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolDefaults(t *testing.T) {
	d := ToolDefaults{}
	assert.False(t, d.IsReadOnly(nil), "default IsReadOnly should be false")
	assert.False(t, d.IsConcurrencySafe(nil), "default IsConcurrencySafe should be false")
	assert.False(t, d.IsReadOnly(map[string]any{"key": "val"}), "default IsReadOnly with input should be false")
	assert.False(t, d.IsConcurrencySafe(map[string]any{"key": "val"}), "default IsConcurrencySafe with input should be false")
}
