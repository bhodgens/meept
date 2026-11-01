package agent

import (
	"context"

	"github.com/caimlas/meept/internal/agent/contextalloc"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// Loop seam plumbing for the context assembler (context-memory-engine
// leaf 01-context-assembler/03-integration.md, tasks T3/T4/T5).
//
// Every helper here is nil-seam safe: a loop whose contextSeam is nil (or
// disabled) takes today's code path with byte-identical outputs. The seam
// arms ONLY through SetContextAssemblerConfig with
// agent.context_assembler.enabled=true AND a non-nil TierBudgetProvider
// wired by the daemon (SetTierBudgetProvider).

// SetContextAssemblerConfig wires the [agent.context_assembler] settings.
// A disabled (or zero-value) config leaves any existing seam untouched --
// mirroring SetContextFirewallConfig's field-assignment shape -- but never
// ARMS one: enabled=false degrades every query helper to today's path.
// Safe to call multiple times; later calls replace the config.
func (l *AgentLoop) SetContextAssemblerConfig(cfg config.ContextAssemblerConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.contextAssemblerCfg = cfg
	l.contextAssemblerSet = true
	// A config flip does not rebuild the allocator; the seam materializes
	// lazily in contextSeamIfEnabled() so the TierBudgetProvider (wired
	// independently by the daemon) can arrive in either order.
}

// SetTierBudgetProvider wires the C1 provider adapter (branch B stores).
// Typed-nil guard (internal/daemon/AGENTS.md wiring rule): a nil or
// typed-nil provider stores as an absent seam -- the query helpers then
// report today's path, never a half-armed seam. The provider may be wired
// before OR after SetContextAssemblerConfig.
func (l *AgentLoop) SetTierBudgetProvider(prov contextalloc.TierBudgetProvider) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Store through the interface only when the concrete value is usable;
	// a typed-nil stays a typed-nil here and is re-checked by the
	// reflect-based isAbsentProvider in contextalloc when the seam
	// materializes. Never assign a non-nil interface holding a nil
	// concrete pointer where absence is meant: NewLoopSeam normalizes it.
	l.tierProvider = prov
}

// contextSeamIfEnabled materializes the loop's LoopSeam when
// agent.context_assembler.enabled is set. Returns nil when the feature is
// off (config absent/zero) or the provider is absent -- in BOTH cases the
// caller takes today's path. The materialized seam is cached on the loop;
// a later config or provider change rebuilds it on the next call.
func (l *AgentLoop) contextSeamIfEnabled() *contextalloc.LoopSeam {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.contextAssemblerSet || !l.contextAssemblerCfg.Enabled {
		return nil
	}
	if l.tierProvider == nil {
		return nil
	}
	if l.contextSeam != nil && l.contextSeam.Enabled() {
		return l.contextSeam
	}

	cfg := contextalloc.DefaultConfig()
	alloc, err := contextalloc.New(cfg)
	if err != nil {
		// DefaultConfig validates by construction; this is unreachable
		// today. Fail closed: no seam, today's path.
		return nil
	}
	tk := l.seamTokenizer()
	l.contextSeam = contextalloc.NewLoopSeam(alloc, l.tierProvider, tk)
	if !l.contextSeam.Enabled() {
		// Typed-nil provider: normalize to no seam so Enabled() checks
		// downstream stay single-question.
		l.contextSeam = nil
		return nil
	}
	return l.contextSeam
}

// seamTokenizer resolves the loop's serving-model tokenizer for budget
// counting (doc budgets, memory estimates). Falls back to nil -- callers
// then use the heuristic -- when no concrete client is attached.
func (l *AgentLoop) seamTokenizer() llm.Tokenizer {
	if l.llmClient == nil {
		return nil
	}
	mc := l.llmClient.Config()
	if mc == nil || mc.ModelID == "" {
		return nil
	}
	return llm.NewTokenizerForModel(mc.ModelID)
}

// contextAssemblerWindow resolves the per-model context window for the
// allocator (T4): the loop's ModelContextLimit override (the
// SetContextFirewallConfig lane) first, then the served model's
// discovered ContextLimit, then the firewall's model limit, then 0 (=
// unresolved; callers fall back to the statics). This mirrors
// requestIsSmallVersusLimit's precedence with the override on top.
func (l *AgentLoop) contextAssemblerWindow() int {
	l.mu.RLock()
	firewall := l.contextFirewall
	client := l.llmClient
	modelContextLimit := l.config.ModelContextLimit
	l.mu.RUnlock()

	if modelContextLimit > 0 {
		return modelContextLimit
	}
	if client != nil {
		if mc := client.Config(); mc != nil && mc.ContextLimit > 0 {
			return mc.ContextLimit
		}
	}
	if firewall != nil {
		if mc := firewall.Config(); mc != nil && mc.ContextLimit > 0 {
			return mc.ContextLimit
		}
	}
	return 0
}

// memoryContextViaSeam runs the enabled-path memory injection (T3):
// tier-budgeted recall through the seam, rendered as "- " bullets per
// tier, then whole-item budgeted per tier budget. Returns nil when the
// seam is disabled (caller takes today's path) or nothing was recalled.
func (l *AgentLoop) memoryContextViaSeam(ctx context.Context, query string) []string {
	seam := l.contextSeamIfEnabled()
	if seam == nil {
		return nil
	}
	window := l.contextAssemblerWindow()
	if window <= 0 {
		// No discovered window: the allocator's R-A2 floor still bounds
		// the reserve math, but the documented posture (leaf 01) is that
		// statics supersede the allocator when no real window resolves.
		return nil
	}
	items, budgets, ok := seam.MemoryContext(ctx, query, window)
	if !ok || len(items) == 0 {
		return nil
	}

	tk := seam.Tokenizer()
	estimate := llm.EstimateTokenCountHeuristic
	if tk != nil {
		estimate = tk.CountTokens
	}

	// Render tier by tier in working/episodic/semantic order, each tier
	// whole-item budgeted at its allocator share (findings R-A4/R-A5:
	// per-tier floors, whole-item drops).
	var out []string
	for _, tier := range []contextalloc.TierName{
		contextalloc.TierWorking, contextalloc.TierEpisodic, contextalloc.TierSemantic,
	} {
		tierItems, present := items[tier]
		if !present || len(tierItems) == 0 {
			continue
		}
		budget := budgets[tier]
		bulleted := make([]string, len(tierItems))
		for i, part := range tierItems {
			bulleted[i] = "- " + part
		}
		out = append(out, EnforceMemoryItemBudget(bulleted, budget, estimate)...)
	}
	return out
}

// docBudgetViaSeam resolves the dynamic-docs budget for the artifact
// section (T5): the Allocation.SystemDynamicDocs share when the seam is
// enabled and a window resolved; 0 (= BuildFullArtifactContextBudgeted's
// own default) otherwise. A 0 return with seamEnabled=false means
// "today's unbounded builder".
func (l *AgentLoop) docBudgetViaSeam(ctx context.Context) (budget int, seamEnabled bool) {
	seam := l.contextSeamIfEnabled()
	if seam == nil {
		return 0, false
	}
	window := l.contextAssemblerWindow()
	if window <= 0 {
		return 0, false
	}
	alloc := seam.Allocator().Allocate(ctx, window, contextalloc.Request{})
	if alloc.SystemDynamicDocs <= 0 {
		return 0, false
	}
	return alloc.SystemDynamicDocs, true
}

// iterationBudgetViaSeam resolves the per-iteration token budget (T3/T4):
// Allocation.IterationBudget (window-derived, capped at the History
// share) when the seam is enabled; 0 = use IterationTokenBudget static.
func (l *AgentLoop) iterationBudgetViaSeam(ctx context.Context) int {
	seam := l.contextSeamIfEnabled()
	if seam == nil {
		return 0
	}
	window := l.contextAssemblerWindow()
	if window <= 0 {
		return 0
	}
	alloc := seam.Allocator().Allocate(ctx, window, contextalloc.Request{})
	return alloc.IterationBudget
}

// conversationBudgetViaSeam resolves the conversation budget (T3/T4):
// Allocation.ConversationBudget when the seam is enabled; 0 = use
// conversationTokenBudget()'s static path.
func (l *AgentLoop) conversationBudgetViaSeam(ctx context.Context) int {
	seam := l.contextSeamIfEnabled()
	if seam == nil {
		return 0
	}
	window := l.contextAssemblerWindow()
	if window <= 0 {
		return 0
	}
	alloc := seam.Allocator().Allocate(ctx, window, contextalloc.Request{})
	return alloc.ConversationBudget
}
