package llm

// TierPolicy is the llm-internal mirror of the canonical
// config.TierPolicyConfig (llm.tiers knobs, llm-serving-tiers tree leaf
// 01, Contract C1). Defined locally to avoid the internal/llm ->
// internal/config import cycle (internal/config transitively imports
// internal/llm via tools/mcp - see FailurePolicyConfig in
// failure_policy.go for the established pattern); the canonical type
// satisfies this shape 1:1 and the daemon wiring maps field-for-field.
//
// A nil *TierPolicy (or Enabled=false) is the DISABLED posture: the
// rotation is unbounded exactly as before this leaf and no metadata keys
// are written - zero behavior change.
type TierPolicy struct {
	// Enabled toggles the whole policy. Default false.
	Enabled bool
	// MaxDepth is the per-request degradation limit: at most
	// MaxDepth+1 members dispatch (primary + MaxDepth degradations).
	// <=0 means DefaultTierDepth when Enabled (see EffectiveDepth).
	MaxDepth int
	// TurnCeilings maps a capability name to the per-turn maximum TOTAL
	// degradations for turns whose primary carries that capability.
	// Absent/zero entries fall back to MaxDepth.
	TurnCeilings map[string]int
	// SignalDegraded controls whether the tier signaling metadata keys
	// (MetaKeyLLMTier / MetaKeyLLMTierDegraded / MetaKeyLLMTierDepth)
	// are written into the Response. Only consulted when Enabled.
	SignalDegraded bool
}

// DefaultTierDepth is the effective depth limit when a policy is enabled
// with MaxDepth <= 0 (mirrors config.DefaultTierPolicyMaxDepth; kept as a
// package-local constant so internal/llm never imports internal/config).
const DefaultTierDepth = 2

// Tier signaling metadata keys (Contract C3, PINNED channel - per-step
// response metadata, never a new bus topic, never a new WS type). Written
// into Response.Metadata ONLY when Enabled && SignalDegraded; written on
// EVERY completed call (degraded or not). Zero bytes of metadata change
// when the feature is off.
const (
	// MetaKeyLLMTier: string - canonical "provider/model-id" that served.
	MetaKeyLLMTier = "llm_tier"
	// MetaKeyLLMTierDegraded: bool - true when depth > 0.
	MetaKeyLLMTierDegraded = "llm_tier_degraded"
	// MetaKeyLLMTierDepth: int - degradations from the primary.
	MetaKeyLLMTierDepth = "llm_tier_depth"
)

// EffectiveDepth resolves the policy's effective per-request limit:
// MaxDepth when positive, else DefaultTierDepth (Contract C1: "default 2
// when Enabled"). Callers must only consult this when Enabled.
func (p *TierPolicy) EffectiveDepth() int {
	if p.MaxDepth > 0 {
		return p.MaxDepth
	}
	return DefaultTierDepth
}

// turnLimitFor resolves the binding per-turn limit for a turn whose
// primary carries the given capability map: the TurnCeilings entry for
// the FIRST matching capability wins when present and positive; otherwise
// the policy-wide EffectiveDepth applies. Capability iteration order is
// irrelevant in practice (a turn's primary rarely declares two capped
// capabilities); for determinism the smallest matching ceiling wins.
func (p *TierPolicy) turnLimitFor(caps map[string]bool) (limit int, capability string) {
	limit = p.EffectiveDepth()
	if len(p.TurnCeilings) == 0 {
		return limit, ""
	}
	for capName, ceil := range p.TurnCeilings {
		if !caps[capName] || ceil <= 0 {
			continue
		}
		if capability == "" || ceil < limit {
			limit = ceil
			capability = capName
		}
	}
	return limit, capability
}

// SetTierPolicy installs the tier policy on the manager (daemon wiring
// seam, mirroring SetFailurePolicyConfig / SetSessionAffinityConfig).
// Nil policy (or a copy with Enabled=false) disables the policy - the
// typed-nil setter rule: a nil pointer clears to disabled, it never
// panics and never leaves a stale policy installed. Call once at daemon
// wiring time; the policy struct is copied so callers may mutate their
// own copy afterwards.
func (pm *ProviderManager) SetTierPolicy(policy *TierPolicy) {
	if pm == nil {
		return
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if policy == nil {
		pm.tierPolicy = nil
		return
	}
	c := *policy // copy value: caller's struct is not aliased
	pm.tierPolicy = &c
}

// tierGate is the per-rotation-loop tier bookkeeping. It lives on the
// STACK of one Chat/ChatWithProgress/streaming call - never on the shared
// manager, never in package globals - so concurrent turns cannot share
// counters (per-turn ceiling bookkeeping rule, leaf Notes). Constructed
// once per call; its depth counter resets naturally with each new call.
type tierGate struct {
	policy     *TierPolicy
	limit      int    // effective binding limit for THIS turn
	capability string // TurnCeilings key when a ceiling (not MaxDepth) binds
	signals    bool   // write metadata keys on completed responses
}

// newTierGate builds the per-call gate from the manager's policy and the
// primary candidate's capability map. Returns nil when the policy is
// disabled - every gate method is then skipped and the rotation runs
// byte-identically to pre-leaf behavior.
func (pm *ProviderManager) newTierGate(primaryCaps map[string]bool) *tierGate {
	pm.mu.RLock()
	policy := pm.tierPolicy
	pm.mu.RUnlock()
	if policy == nil || !policy.Enabled {
		return nil
	}
	limit, capability := policy.turnLimitFor(primaryCaps)
	return &tierGate{
		policy:     policy,
		limit:      limit,
		capability: capability,
		signals:    policy.SignalDegraded,
	}
}

// nextHopBlocked reports whether dispatching the NEXT member would exceed
// the turn's degradation limit: dispatched counts the members already
// tried, so the hop about to run would be degradation number `dispatched`
// (the primary is member 1, not a degradation). The hop is legal while
// degradations so far < limit; at limit degradations served the floor
// fires (Contract C2 worked example: MaxDepth=2 on a 4-member chain
// dispatches 3 members - primary + 2 degradations - and refuses the 4th).
// Read-only on the gate; the count advances only via the caller's
// attempts++ after a successful dispatch decision.
func (g *tierGate) nextHopBlocked(providerID, modelID string, dispatched int, cause error) error {
	if g == nil {
		return nil
	}
	// dispatched == members already dispatched (the loop's `attempts`
	// counter). The hop about to run is member dispatched+1, i.e.
	// degradation number `dispatched` (member 1 = 0 degradations, member
	// N = N-1 degradations). The hop is legal while the total member
	// count dispatched+1 stays within limit+1 (primary + limit
	// degradations), i.e. dispatched <= limit; blocked once the chain
	// has already served limit degradations.
	if dispatched <= g.limit {
		return nil
	}
	return &TierFloorError{
		ProviderID: providerID,
		ModelID:    modelID,
		Depth:      dispatched,
		Limit:      g.limit,
		Capability: g.capability,
		Cause:      cause,
	}
}

// stampMetadata writes the tier signaling keys (Contract C3) into the
// response's metadata map: llm_tier = "provider/model-id",
// llm_tier_degraded = depth > 0, llm_tier_depth = degradations from the
// primary. Called on every completed call when Enabled && SignalDegraded.
// degraded is the number of degradations that preceded the serving member
// (attempt index in the rotation loop). Nil-gate/no-op safe.
func (g *tierGate) stampMetadata(resp *Response, providerID, modelID string, degraded int) {
	if g == nil || !g.signals || resp == nil {
		return
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any, 3)
	}
	resp.Metadata[MetaKeyLLMTier] = providerID + "/" + modelID
	resp.Metadata[MetaKeyLLMTierDegraded] = degraded > 0
	resp.Metadata[MetaKeyLLMTierDepth] = degraded
}
