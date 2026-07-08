package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentLoop_ModelOverride_SetGetClear(t *testing.T) {
	loop := NewAgentLoop()

	// Initially empty
	assert.Empty(t, loop.GetModelOverride(), "initial model override should be empty")

	// Set and get
	loop.SetModelOverride("zai/glm-4.7")
	assert.Equal(t, "zai/glm-4.7", loop.GetModelOverride())

	// Clear
	loop.ClearModelOverride()
	assert.Empty(t, loop.GetModelOverride(), "model override should be empty after clear")
}

func TestAgentLoop_ModelOverride_WithOption(t *testing.T) {
	loop := NewAgentLoop(
		WithModelOverride("anthropic/claude-3-opus"),
	)

	assert.Equal(t, "anthropic/claude-3-opus", loop.GetModelOverride())
}

func TestAgentLoop_ExtractModelOverrideFromMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata json.RawMessage
		want     string
	}{
		{
			name:     "nil metadata",
			metadata: nil,
			want:     "",
		},
		{
			name:     "empty metadata",
			metadata: json.RawMessage(`{}`),
			want:     "",
		},
		{
			name:     "valid model override",
			metadata: json.RawMessage(`{"model_override": "zai/glm-4.7", "model_scope": "coding"}`),
			want:     "zai/glm-4.7",
		},
		{
			name:     "model override with target intent",
			metadata: json.RawMessage(`{"model_override": "anthropic/claude-3-opus", "model_scope": "synthesis", "model_target_intent": "plan"}`),
			want:     "anthropic/claude-3-opus",
		},
		{
			name:     "empty model override string",
			metadata: json.RawMessage(`{"model_override": ""}`),
			want:     "",
		},
		{
			name:     "non-string model override",
			metadata: json.RawMessage(`{"model_override": 42}`),
			want:     "",
		},
		{
			name:     "malformed JSON",
			metadata: json.RawMessage(`{invalid`),
			want:     "",
		},
		{
			name:     "unrelated metadata",
			metadata: json.RawMessage(`{"other_key": "value"}`),
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loop := NewAgentLoop()
			got := loop.extractModelOverrideFromMetadata(tt.metadata)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAgentLoop_PersistentModelOverride_DoesNotAutoClear(t *testing.T) {
	loop := NewAgentLoop()

	// Initially empty and not persistent
	assert.Empty(t, loop.GetModelOverride(), "initial model override should be empty")
	assert.False(t, loop.IsModelOverridePersistent(), "initial persistent flag should be false")

	// Set persistent override
	loop.SetPersistentModelOverride("test-model")
	assert.Equal(t, "test-model", loop.GetModelOverride(), "override should be set")
	assert.True(t, loop.IsModelOverridePersistent(), "persistent flag should be true")

	// Simulate the consumption site: apply override, then check if we should clear.
	// The consumption site only auto-clears when NOT persistent.
	if !loop.IsModelOverridePersistent() {
		loop.ClearModelOverride()
	}

	// Override should STILL be present (persistent mode does not auto-clear)
	assert.Equal(t, "test-model", loop.GetModelOverride(),
		"persistent override should NOT be cleared after consumption site")
	assert.True(t, loop.IsModelOverridePersistent(),
		"persistent flag should still be true after consumption site")

	// Explicit clear should remove the override and reset persistent flag
	loop.ClearModelOverride()
	assert.Empty(t, loop.GetModelOverride(), "override should be empty after explicit clear")
	assert.False(t, loop.IsModelOverridePersistent(),
		"persistent flag should be false after explicit clear")
}

func TestAgentLoop_SetModelOverride_ResetsPersistentFlag(t *testing.T) {
	loop := NewAgentLoop()

	// Set persistent first
	loop.SetPersistentModelOverride("persistent-model")
	assert.True(t, loop.IsModelOverridePersistent())

	// Set one-shot should reset persistent flag
	loop.SetModelOverride("one-shot-model")
	assert.False(t, loop.IsModelOverridePersistent(),
		"SetModelOverride should reset persistent flag to false")
	assert.Equal(t, "one-shot-model", loop.GetModelOverride())

	// Simulate consumption site: one-shot auto-clears
	if !loop.IsModelOverridePersistent() {
		loop.ClearModelOverride()
	}

	assert.Empty(t, loop.GetModelOverride(),
		"one-shot override should be cleared after consumption site")
}
