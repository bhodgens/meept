package daemon

import (
	"github.com/caimlas/meept/internal/agent/contextalloc"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/working"
)

// Daemon wiring for the context assembler's TierBudgetProvider adapter
// (context-memory-engine leaf 01-context-assembler/03-integration.md,
// task T6): constructs the branch-B store bundle and registers it on the
// agent loop AFTER the memory manager (and any working store) exist --
// the wiring-ordering rule from internal/daemon/AGENTS.md (construct
// evolver-adjacent components after their dependencies).
//
// Compile independence (the leaf's CRITICAL seam rule): the adapter lives
// in internal/agent/contextalloc and is the ONLY importer of
// internal/memory here; this file merely hands over the (possibly nil)
// store fields. Every nil store degrades to an absent tier; an all-nil
// bundle degrades to today's undivided memory path without a second
// guard. The loop setter carries the typed-nil guard (SetTierBudgetProvider),
// so wiring is unconditional at this site.
//
// The working store is constructed lazily HERE (the daemon owns its
// lifecycle) with the shipped ring defaults from branch B leaf 01; a
// future [memory.tiers] config merge can resize it without touching this
// file's shape.
func WireContextAssembler(c *Components, cfg *config.Config) {
	if c == nil || c.AgentLoop == nil || cfg == nil {
		return
	}
	// Opt-in gate (docs/agents/opt-in-defaults.md): config only arms the
	// seam; the zero value leaves the loop untouched.
	if !cfg.Agent.ContextAssembler.Enabled {
		return
	}

	// Branch B stores. All may be nil (typed-nil guard on the loop
	// setter): the episodic and semantic handles come from the memory
	// manager, which is nil when the manager failed to initialize
	// (non-fatal at boot) -- the adapter then simply reports fewer tiers.
	var work *working.Store
	if workCfg := assemblerWorkConfig(); workCfg != nil {
		tk := llm.HeuristicTokenizer{}
		if s, err := working.New(*workCfg, &tk); err == nil {
			work = s
		}
		// Construction failure is non-fatal: the working tier is simply
		// absent and recall degrades to the episodic/semantic stores.
	}

	var epi *memory.EpisodicMemory
	if c.MemoryManager != nil {
		epi = c.MemoryManager.Episodic()
	}

	prov := contextalloc.NewTierStoreProvider(
		work, 0, // working ceiling: allocator decides (R-A4)
		epi, 0, // episodic ceiling: allocator decides
		c.MemoryManager, 0, // semantic recall via Manager.SearchSemantic
		nil, // tokenizer: the loop resolves its serving model's tokenizer
	)

	// SetTierBudgetProvider carries the typed-nil guard; a provider over
	// all-nil stores normalizes to "no seam" inside the loop.
	c.AgentLoop.SetTierBudgetProvider(prov)

	// The [agent.context_assembler] block itself (loop-side arm gate).
	// Set AFTER the provider so a config flip can never observe a
	// half-wired seam (construct-after-dependencies ordering).
	c.AgentLoop.SetContextAssemblerConfig(cfg.Agent.ContextAssembler)
}

// assemblerWorkConfig returns the working-ring configuration. Returns a
// value (never nil) today; the pointer shape exists so a config merge can
// later disable the ring by returning nil.
func assemblerWorkConfig() *working.Config {
	cfg := working.Config{
		MaxTokens: 8192, // ring capacity in tokens (branch B leaf 01 default)
		MaxItems:  200,  // hard item cap
	}
	return &cfg
}
