package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/config"
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

func TestDaemonDefaultsOverride(t *testing.T) {
	cfg := config.DefaultConfig()
	vd := cfg.Daemon.Verification

	assert.True(t, vd.Enabled, "daemon default verification should be enabled")
	assert.Equal(t, 3, vd.MaxFixLoops, "daemon default max_fix_loops should be 3")
	assert.Equal(t, 3, vd.AutoTriggerThreshold, "daemon default auto_trigger_threshold should be 3")
	assert.Empty(t, vd.DefaultModel, "daemon default model should be empty (inherit)")
}

func TestAgentOverridesDaemon(t *testing.T) {
	// Daemon defaults provide the baseline.
	cfg := config.DefaultConfig()
	daemonDefaults := cfg.Daemon.Verification
	require.True(t, daemonDefaults.Enabled)
	require.Equal(t, 3, daemonDefaults.MaxFixLoops)
	require.Equal(t, 3, daemonDefaults.AutoTriggerThreshold)

	t.Run("agent metadata overrides daemon defaults", func(t *testing.T) {
		enabled := false
		autoTrigger := false
		meta := &agents.VerificationMetadata{
			Enabled:     &enabled,
			Model:       "agent-verifier",
			AutoTrigger: &autoTrigger,
			MaxFixLoops: 10,
		}
		vc := verificationFromMetadata(meta)

		// Agent values override daemon defaults.
		assert.False(t, vc.Enabled, "agent should override daemon enabled=true")
		assert.Equal(t, "agent-verifier", vc.Model, "agent model should override daemon empty model")
		assert.False(t, vc.AutoTrigger, "agent should override daemon auto_trigger")
		assert.Equal(t, 10, vc.MaxFixLoops, "agent should override daemon max_fix_loops=3")
	})

	t.Run("nil agent metadata falls back to defaults", func(t *testing.T) {
		vc := verificationFromMetadata(nil)

		// Defaults match daemon defaults where applicable.
		assert.True(t, vc.Enabled, "nil metadata should use default enabled=true")
		assert.Empty(t, vc.Model, "nil metadata should have empty model (inherit)")
		assert.True(t, vc.AutoTrigger, "nil metadata should use default auto_trigger=true")
		assert.Equal(t, 3, vc.MaxFixLoops, "nil metadata should use default max_fix_loops=3")
	})

	t.Run("partial agent metadata preserves defaults for unset fields", func(t *testing.T) {
		enabled := false
		meta := &agents.VerificationMetadata{
			Enabled: &enabled,
			// Model, AutoTrigger, MaxFixLoops all unset.
		}
		vc := verificationFromMetadata(meta)

		assert.False(t, vc.Enabled, "agent explicitly disables verification")
		assert.Empty(t, vc.Model, "unset model should remain empty (inherit)")
		assert.True(t, vc.AutoTrigger, "unset auto_trigger should keep default true")
		assert.Equal(t, 3, vc.MaxFixLoops, "unset max_fix_loops should keep default 3")
	})
}
