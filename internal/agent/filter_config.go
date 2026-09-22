package agent

import "github.com/caimlas/meept/internal/config"

// FilterConfig is the resolved output-filter configuration for one agent.
// It mirrors VerificationConfig (internal/agent/verification_config.go):
// daemon defaults flow through EffectiveFilterConfig, and per-agent
// AGENT.md front matter / runtime metadata overrides win when explicitly
// set (output-filters tree, master.md Contract 6).
type FilterConfig struct {
	// Enabled turns the output-filter stage on for this agent.
	Enabled bool `json:"enabled" yaml:"enabled"`
	// MaxPasses bounds the rewrite sweeps before chain fail. 0 = inherit
	// the daemon value.
	MaxPasses int `json:"max_passes" yaml:"max_passes"`
	// MaxFilterRetries caps filter-rejection requeues for this agent.
	// 0 = inherit the daemon value.
	MaxFilterRetries int `json:"max_filter_retries" yaml:"max_filter_retries"`
	// Filters names the builtin filters in execution order. The agent
	// list REPLACES the daemon list when non-empty.
	Filters []string `json:"filters" yaml:"filters"`
}

// DefaultFilterConfig returns the daemon defaults, aligned with
// config.DefaultConfig()'s OutputFilters block: disabled until opted in,
// both caps at 2, no filters.
func DefaultFilterConfig() FilterConfig {
	return FilterConfig{
		Enabled:          false,
		MaxPasses:        2,
		MaxFilterRetries: 2,
		Filters:          nil,
	}
}

// MaxFilterRetriesOrDefault returns the filter-rejection requeue cap,
// defaulting 0/negative to 2 (master.md Contract 3). It satisfies the
// TacticalScheduler's filterRetryLimiter seam (leaf 03) as the production
// config-snapshot implementation.
func (fc FilterConfig) MaxFilterRetriesOrDefault() int {
	if fc.MaxFilterRetries <= 0 {
		return 2
	}
	return fc.MaxFilterRetries
}

// EffectiveFilterConfig resolves the daemon defaults against the agent
// override, mirroring the verification-config semantics:
//
//   - Scalars (Enabled, MaxPasses, MaxFilterRetries): the agent wins when
//     explicitly set (non-zero); zero falls through to the daemon value.
//   - Filters: the agent list REPLACES the daemon list when non-empty;
//     otherwise the daemon list flows through.
//
// A nil agent pointer is the daemon-only case.
func EffectiveFilterConfig(daemon config.OutputFiltersConfig, agent *FilterConfig) FilterConfig {
	fc := FilterConfig{
		Enabled:          daemon.Enabled,
		MaxPasses:        daemon.MaxPasses,
		MaxFilterRetries: daemon.MaxFilterRetries,
		Filters:          daemon.Filters,
	}
	if agent == nil {
		return fc
	}
	if agent.Enabled {
		fc.Enabled = true
	}
	if agent.MaxPasses > 0 {
		fc.MaxPasses = agent.MaxPasses
	}
	if agent.MaxFilterRetries > 0 {
		fc.MaxFilterRetries = agent.MaxFilterRetries
	}
	if len(agent.Filters) > 0 {
		fc.Filters = agent.Filters
	}
	return fc
}
