package agent

// AgentHealthSource reports whether an agent's provider endpoint is
// currently parked or in cooldown (agent-routing tree leaf 02). Producer
// wiring is OPTIONAL: an unwired (nil) source keeps selectAgent
// byte-identical to the legacy routing path.
//
// This leaf defines only the consumer side. The park/cooldown state itself
// lives in internal/llm (Resolver.endpointBlocks, alias cooldowns, the
// agent TurnParker); no per-agent producer exists there today — see the
// leaf-02 report for wiring options.
type AgentHealthSource interface {
	AgentParkedOrCooling(agentID string) bool
}
