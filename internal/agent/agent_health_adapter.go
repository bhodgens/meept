package agent

import (
	"github.com/caimlas/meept/internal/llm"
)

// AgentModelLookup resolves an agent ID to the model it would serve with:
// Config is the concrete *llm.ModelConfig the resolver produced for the
// agent's model ref (nil when the ref does not resolve), and Alias is the
// agent's dedicated alias name ("" when the agent rides the default model).
// The producer wiring in internal/daemon/components.go mirrors the registry's
// model selection: explicit spec.Model wins, else spec.ID when the resolver
// has a matching alias, else the default model ref with no alias.
type AgentModelLookup func(agentID string) (AgentModelBinding, bool)

// AgentModelBinding is the per-agent model info the health adapter needs.
type AgentModelBinding struct {
	Config *llm.ModelConfig
	Alias  string
}

// HealthResolver is the narrow slice of *llm.Resolver the adapter reads.
// Naming it locally keeps the adapter testable with a fake and decouples it
// from resolver internals (quota blocks, endpoint cooldowns live there).
type HealthResolver interface {
	// EndpointBlocked reports whether the model's endpoint (EndpointKey:
	// base-URL host + credential) is under a timeout cooldown (tree 02
	// leaf 04, D10). A nil cfg reports false.
	EndpointBlocked(cfg *llm.ModelConfig) bool
	// HasHealthyModels reports whether the alias has at least one model
	// that can serve a request right now (not cooldown-, quota- or
	// endpoint-blocked). An unknown alias reports false.
	HasHealthyModels(aliasName string) bool
}

// AgentHealthAdapter is the producer for AgentHealthSource (agent-routing
// tree leaf 02 deferred follow-up): it answers TacticalScheduler's
// AgentParkedOrCooling(agentID) probe from the quota/endpoint health state
// that already lives on the llm.Resolver, which is per-MODEL (EndpointBlocked)
// and per-ALIAS (HasHealthyModels), not per-agent — this adapter bridges the
// agentID → model-config gap via an AgentModelLookup.
//
// Semantics: unknown agent → false (never parked); unresolvable model or nil
// resolver → false (conservative: routing proceeds unchanged); endpoint
// blocked → true; alias with no healthy models → true. An agent with no
// dedicated alias is judged on endpoint state only — "" never queries
// HasHealthyModels, so a default-model agent is not spuriously parked by an
// unrelated alias name.
type AgentHealthAdapter struct {
	lookup   AgentModelLookup
	resolver HealthResolver
}

// compile-time: the adapter satisfies the consumer's contract.
var _ AgentHealthSource = (*AgentHealthAdapter)(nil)

// NewAgentHealthAdapter builds the adapter. Both arguments are used as-is;
// nil-safety is enforced per-call, so a nil resolver (or nil adapter) simply
// reports false and leaves tactical routing on the legacy path.
func NewAgentHealthAdapter(lookup AgentModelLookup, resolver HealthResolver) *AgentHealthAdapter {
	return &AgentHealthAdapter{lookup: lookup, resolver: resolver}
}

// AgentParkedOrCooling implements AgentHealthSource.
func (a *AgentHealthAdapter) AgentParkedOrCooling(agentID string) bool {
	if a == nil || a.resolver == nil || a.lookup == nil || agentID == "" {
		return false
	}
	binding, ok := a.lookup(agentID)
	if !ok || binding.Config == nil {
		return false
	}
	if a.resolver.EndpointBlocked(binding.Config) {
		return true
	}
	if binding.Alias != "" && !a.resolver.HasHealthyModels(binding.Alias) {
		return true
	}
	return false
}
