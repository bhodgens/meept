package daemon

// Serving-tier policy daemon wiring (llm-serving-tiers tree leaf 01 T5
// arm-up, issue #82). internal/llm.ProviderManager.SetTierPolicy existed
// with the schema (llm.tiers) and the normalization
// (config.NormalizeTierPolicyDefaults) landed, but NO daemon call site
// armed it. This file is that call site: components.go invokes
// applyTierPolicyToProviderManager right where the ProviderManager is
// built, mapping config.TierPolicyConfig onto llm.TierPolicy
// field-for-field.
//
// Disabled posture (the opt-in default): the policy maps to nil,
// SetTierPolicy(nil) installs the cleared/disabled state, the rotation
// stays unbounded and zero metadata keys change - byte-identical to the
// pre-leaf daemon.

import (
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// tierPolicyFromSchema maps the canonical config.TierPolicyConfig onto
// the llm-internal mirror (llm.TierPolicy is defined locally to dodge
// the config import cycle; the daemon is the one conversion point, per
// the type doc). Returns nil when the policy is disabled so the setter
// lands the explicit disabled posture rather than a stale policy.
// TurnCeilings is copied (not aliased) so later config mutation cannot
// race the rotation loop's reads.
func tierPolicyFromSchema(tp config.TierPolicyConfig) *llm.TierPolicy {
	if !tp.Enabled {
		return nil
	}
	policy := &llm.TierPolicy{
		Enabled:        true,
		MaxDepth:       tp.MaxDepth,
		SignalDegraded: tp.SignalDegraded,
	}
	if len(tp.TurnCeilings) > 0 {
		policy.TurnCeilings = make(map[string]int, len(tp.TurnCeilings))
		for capName, ceil := range tp.TurnCeilings {
			policy.TurnCeilings[capName] = ceil
		}
	}
	return policy
}

// applyTierPolicyToProviderManager is the ONE daemon arm for llm.tiers:
// maps the config and installs onto the manager via SetTierPolicy. Nil
// manager is a no-op (typed-nil setter rule); a disabled/nil-effective
// policy explicitly clears. Returns the installed policy (nil when
// disabled) so callers and tests can pin the mapping without reaching
// into the manager.
func applyTierPolicyToProviderManager(pm *llm.ProviderManager, cfg *config.Config) *llm.TierPolicy {
	policy := tierPolicyFromSchema(cfg.LLM.Tiers)
	pm.SetTierPolicy(policy)
	return policy
}
