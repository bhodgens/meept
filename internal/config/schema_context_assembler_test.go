package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestContextAssemblerDefault pins the opt-in posture (T1): a zero-value
// ContextAssemblerConfig is DISABLED, and the [agent] section a fresh
// Config produces exposes agent.context_assembler.enabled == false. The
// assembler never ships default-on (docs/agents/opt-in-defaults.md).
func TestContextAssemblerDefault(t *testing.T) {
	var zero ContextAssemblerConfig
	assert.False(t, zero.Enabled, "zero-value ContextAssemblerConfig must be disabled")

	cfg := Config{}
	assert.False(t, cfg.Agent.ContextAssembler.Enabled,
		"fresh Config must expose agent.context_assembler.enabled=false")
}

// TestContextAssemblerExplicitEnable pins that the only way the assembler
// arms is an explicit true in config.
func TestContextAssemblerExplicitEnable(t *testing.T) {
	cfg := Config{}
	cfg.Agent.ContextAssembler.Enabled = true
	assert.True(t, cfg.Agent.ContextAssembler.Enabled)
}
