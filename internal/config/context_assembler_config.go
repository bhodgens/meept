package config

// ContextAssemblerConfig configures the context assembler
// ([agent.context_assembler], context-memory-engine leaf
// 01-context-assembler/03-integration.md).
//
// Opt-in (docs/agents/opt-in-defaults.md): Enabled defaults FALSE. The
// zero value keeps every loop budget path byte-identical to the
// pre-assembler behavior: statics (IterationTokenBudget /
// DefaultConversationTokenBudget / MaxMemoryContextTokens) and the
// unbounded artifact-doc builder stay authoritative until this switch is
// flipped AND a real context window is discovered.
//
// Orchestrator merge note (sibling owns schema.go): attach this struct to
// AgentConfig with the single mechanical field
//
//	ContextAssembler ContextAssemblerConfig `json:"context_assembler" toml:"context_assembler"`
//
// so the config key agent.context_assembler.enabled resolves through the
// generic `meept config get` surface.
//
//gendoc:desc Context assembler: window-derived per-section budget allocation for the agent prompt (opt-in; disabled by default).
type ContextAssemblerConfig struct {
	// Enabled turns on allocator-derived budgets (memory tiers,
	// iteration, conversation, dynamic docs) resolved from the model's
	// discovered context window. Default false.
	Enabled bool `json:"enabled" toml:"enabled"`
}
