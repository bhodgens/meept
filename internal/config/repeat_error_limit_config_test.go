package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeAgentGuardsDefaults_RepeatErrorLimit pins the
// agent.guards.repeat_error_limit plumbing contract: zero AND negative raw
// values fall back to the shipped default (3) at load normalization, and an
// explicit positive value passes through untouched. The daemon threads the
// normalized value into the agent loop via WithRepeatErrorBudget, so this
// boundary is where the fallback must hold.
func TestNormalizeAgentGuardsDefaults_RepeatErrorLimit(t *testing.T) {
	// Zero value falls back to the default.
	zero := AgentGuardsConfig{}
	NormalizeAgentGuardsDefaults(&zero)
	assert.Equal(t, DefaultCfgRepeatErrorLimit, zero.RepeatErrorLimit,
		"zero repeat_error_limit must fall back to the default")

	// Negative value also falls back to the default.
	neg := AgentGuardsConfig{RepeatErrorLimit: -1}
	NormalizeAgentGuardsDefaults(&neg)
	assert.Equal(t, DefaultCfgRepeatErrorLimit, neg.RepeatErrorLimit,
		"negative repeat_error_limit must fall back to the default")

	// An explicit positive value is preserved verbatim.
	explicit := AgentGuardsConfig{RepeatErrorLimit: 10}
	NormalizeAgentGuardsDefaults(&explicit)
	assert.Equal(t, 10, explicit.RepeatErrorLimit,
		"an explicit repeat_error_limit must pass through unchanged")

	// The default constant mirrors the agent-side breaker default (3):
	// internal/config cannot import internal/agent, so this is the sync pin.
	assert.Equal(t, 3, DefaultCfgRepeatErrorLimit)
}

// TestLoadJSON5_GuardsRepeatErrorLimit proves the JSON5 load path picks up
// agent.guards.repeat_error_limit end to end (JSON tag wiring, not just the
// normalizer).
func TestLoadJSON5_GuardsRepeatErrorLimit(t *testing.T) {
	cfg, err := LoadJSON5Config("../../config/meept.json5")
	if err != nil {
		t.Fatalf("shipped template must parse: %v", err)
	}
	assert.Equal(t, DefaultCfgRepeatErrorLimit, cfg.Agent.Guards.RepeatErrorLimit,
		"shipped template ships the default repeat_error_limit")
}
