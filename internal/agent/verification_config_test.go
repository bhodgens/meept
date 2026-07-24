package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/agents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDefaultVerificationConfig(t *testing.T) {
	vc := DefaultVerificationConfig()
	assert.True(t, vc.Enabled)
	assert.Empty(t, vc.Model)
	assert.True(t, vc.AutoTrigger)
	assert.Equal(t, 3, vc.MaxFixLoops)
}

func TestEffectiveModel(t *testing.T) {
	tests := []struct {
		name       string
		config     VerificationConfig
		agentModel string
		want       string
	}{
		{
			name:       "empty model returns agent model",
			config:     VerificationConfig{Model: ""},
			agentModel: "gpt-4",
			want:       "gpt-4",
		},
		{
			name:       "non-empty model overrides",
			config:     VerificationConfig{Model: "verifier-model"},
			agentModel: "gpt-4",
			want:       "verifier-model",
		},
		{
			name:       "both empty returns empty",
			config:     VerificationConfig{Model: ""},
			agentModel: "",
			want:       "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.config.EffectiveModel(tt.agentModel))
		})
	}
}

func TestVerificationConfigParsing(t *testing.T) {
	t.Run("with verification field", func(t *testing.T) {
		yamlStr := `
id: test-agent
name: Test
role: executor
verification:
  enabled: true
  auto_trigger: false
  max_fix_loops: 5
  model: verifier
`
		var meta agents.AgentMetadata
		require.NoError(t, yaml.Unmarshal([]byte(yamlStr), &meta))
		require.NotNil(t, meta.Verification)
		require.NotNil(t, meta.Verification.Enabled)
		assert.True(t, *meta.Verification.Enabled)
		require.NotNil(t, meta.Verification.AutoTrigger)
		assert.False(t, *meta.Verification.AutoTrigger)
		assert.Equal(t, 5, meta.Verification.MaxFixLoops)
		assert.Equal(t, "verifier", meta.Verification.Model)
	})

	t.Run("without verification field", func(t *testing.T) {
		yamlStr := `
id: test-agent
name: Test
role: executor
`
		var meta agents.AgentMetadata
		require.NoError(t, yaml.Unmarshal([]byte(yamlStr), &meta))
		assert.Nil(t, meta.Verification)
	})
}

func TestVerificationConfigBackwardCompat(t *testing.T) {
	t.Run("nil metadata gets defaults", func(t *testing.T) {
		vc := verificationFromMetadata(nil)
		assert.Equal(t, DefaultVerificationConfig(), vc)
	})

	t.Run("partial metadata fills defaults", func(t *testing.T) {
		enabled := false
		meta := &agents.VerificationMetadata{
			Enabled: &enabled,
		}
		vc := verificationFromMetadata(meta)
		assert.False(t, vc.Enabled)
		assert.True(t, vc.AutoTrigger)    // default preserved
		assert.Equal(t, 3, vc.MaxFixLoops) // default preserved
		assert.Empty(t, vc.Model)
	})

	t.Run("full metadata overrides all defaults", func(t *testing.T) {
		enabled := true
		autoTrigger := false
		meta := &agents.VerificationMetadata{
			Enabled:     &enabled,
			Model:       "custom-verifier",
			AutoTrigger: &autoTrigger,
			MaxFixLoops: 7,
		}
		vc := verificationFromMetadata(meta)
		assert.True(t, vc.Enabled)
		assert.Equal(t, "custom-verifier", vc.Model)
		assert.False(t, vc.AutoTrigger)
		assert.Equal(t, 7, vc.MaxFixLoops)
	})
}
