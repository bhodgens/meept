package agent

import "github.com/caimlas/meept/internal/agents"

// VerificationConfig configures post-completion verification for an agent.
//
//nolint:revive // stutter with package name is intentional for API clarity
type VerificationConfig struct {
	// Enabled controls whether verification runs after this agent completes.
	Enabled bool `json:"enabled" yaml:"enabled"`
	// Model overrides the model used for verification. Empty = use agent's model.
	Model string `json:"model" yaml:"model"`
	// AutoTrigger controls whether verification triggers automatically on completion.
	AutoTrigger bool `json:"auto_trigger" yaml:"auto_trigger"`
	// MaxFixLoops is the maximum number of fix-reverify cycles.
	MaxFixLoops int `json:"max_fix_loops" yaml:"max_fix_loops"`
}

// DefaultVerificationConfig returns sensible verification defaults.
func DefaultVerificationConfig() VerificationConfig {
	return VerificationConfig{
		Enabled:     true,
		Model:       "",
		AutoTrigger: true,
		MaxFixLoops: 3,
	}
}

// EffectiveModel returns the verification model to use: the explicit override
// if set, otherwise the agent's own model.
func (v VerificationConfig) EffectiveModel(agentModel string) string {
	if v.Model != "" {
		return v.Model
	}
	return agentModel
}

// verificationFromMetadata converts AGENT.md frontmatter verification metadata
// to a runtime VerificationConfig, applying defaults for absent fields.
func verificationFromMetadata(meta *agents.VerificationMetadata) VerificationConfig {
	if meta == nil {
		return DefaultVerificationConfig()
	}
	vc := DefaultVerificationConfig()
	if meta.Enabled != nil {
		vc.Enabled = *meta.Enabled
	}
	if meta.Model != "" {
		vc.Model = meta.Model
	}
	if meta.AutoTrigger != nil {
		vc.AutoTrigger = *meta.AutoTrigger
	}
	if meta.MaxFixLoops != 0 {
		vc.MaxFixLoops = meta.MaxFixLoops
	}
	return vc
}
