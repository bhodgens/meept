package agent

// AgentHealthSource reports whether an agent's provider endpoint is
// currently parked or in cooldown (agent-routing tree leaf 02). Producer
// wiring is OPTIONAL: an unwired (nil) source keeps selectAgent
// byte-identical to the legacy routing path.
//
// This leaf defines the consumer side. The park/cooldown state itself
// lives in internal/llm (Resolver.endpointBlocks, alias cooldowns, the
// agent TurnParker); the per-agent producer is AgentHealthAdapter
// (agent_health_adapter.go), which bridges agentID → model config via an
// AgentModelLookup and reads that Resolver state, and
// internal/daemon/components.go wires it with SetAgentHealthSource.
type AgentHealthSource interface {
	AgentParkedOrCooling(agentID string) bool
}
