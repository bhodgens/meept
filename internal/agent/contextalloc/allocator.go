// Package contextalloc allocates a resolved LLM context window across the
// assembler's named sections.
//
// The allocator is PURE and deterministic: identical inputs produce a
// bit-identical Allocation. There is no wall-clock, no randomness, and no
// map-order float accumulation (splits use index-ordered largest-remainder
// math; see splitLR in tier.go).
//
// Design basis: docs/plans/20261031-context-memory-engine/
// 01-context-assembler/findings-context-budgeting.md (R-Ax citations
// inline below). It supersedes the static budgets in
// internal/agent/loop.go (IterationTokenBudget / ToolResultMaxTokens /
// DefaultConversationTokenBudget) ONLY when a resolved window exists; those
// statics remain untouched as fallbacks (leaf 01-allocator-core, R2).
package contextalloc

import (
	"context"
	"fmt"
)

// Request describes the current call's shape. The allocator uses it only
// for the tier-provider seam; section sizes themselves are pure
// window-fraction math.
type Request struct {
	Query         string
	ToolCount     int
	DocBytes      int
	HistoryTokens int
}

// SectionFractions are the five flexible-section fractions of the
// post-reserve window. They must sum to exactly 1.0 (within 1e-9) and each
// must lie in [0, usableShare].
//
// Defaults follow findings-context-budgeting.md R-A4 (Addaly's shape: small
// fixed sections; docs hard-capped; history flexes; memory tiers capped):
// system+tools are the stable byte-frozen prefix (R-A7), docs and memory
// are hard-capped flexible sections, and history takes the flexible
// remainder.
type SectionFractions struct {
	SystemStable float64 // stable prompt sections (prefix cache, R-A7)
	DynamicDocs  float64 // CLAUDE.md / README / skills / agents (leaf 02)
	Memory       float64 // memory tiers, subdivided per C1 (R-A4 cap 5%)
	Tools        float64 // tool-declaration overhead + per-result cap (R-A8 rung 1)
	History      float64 // rolling conversation history (flexible remainder)
}

// MinSections are per-section floors in tokens, applied BEFORE the
// proportional split. Floors must be non-negative and must not exceed the
// section's maximum plausible share of the usable window (validated in New).
type MinSections struct {
	SystemStable int
	DynamicDocs  int
	Memory       int
	Tools        int
	History      int
}

// Config configures an Allocator. Use DefaultConfig() for the documented
// defaults.
type Config struct {
	// OutputReserveFraction is the fraction of the resolved window held
	// back for generation (R-A2). The allocator always reserves max(
	// fraction*window, OutputReserveFloor) -- the reserve never shrinks
	// proportionally on small windows (R-A2/R-A10, ml4devs absolute-
	// reserve principle).
	OutputReserveFraction float64

	// Fractions are the section fractions of the POST-reserve window.
	Fractions SectionFractions

	// MinSections are floors applied before the proportional split.
	MinSections MinSections

	// TierProvider is the optional C1 seam. nil (or a typed-nil) restores
	// undivided memory behavior exactly.
	TierProvider TierBudgetProvider
}

// Defaults documented in findings-context-budgeting.md.
const (
	// OutputReserveFloor is the absolute minimum generation reserve in
	// tokens (R-A2: absolute floor 4,096; never fractional on small
	// windows).
	OutputReserveFloor = 4096

	// DefaultOutputReserveFraction covers R-A2's ~6.25% reserve plus the
	// R-A3 ~3% tokenizer-disagreement margin (~9% total). R-A3's safety
	// margin is folded here rather than kept as a separate field: the C2
	// Allocation contract pins the struct shape and there is no separate
	// margin field; the margin's purpose (HeuristicTokenizer is 3
	// chars/token and will disagree with provider counts) is preserved.
	DefaultOutputReserveFraction = 0.09

	// DefaultIterationRatio is the prior-art per-iteration ratio from
	// internal/llm/context_firewall.go DerivedIterationBudget
	// (IterationBudgetRatio = 0.30 of the resolved window).
	DefaultIterationRatio = 0.30

	// DefaultConversationRatio is the prior-art conversation ratio from
	// internal/llm/context_firewall.go DerivedConversationBudget
	// (ConversationBudgetRatio = 0.50 of the resolved window).
	DefaultConversationRatio = 0.50
)

// DefaultConfig returns the documented default configuration with the
// R-A4-derived fractions.
func DefaultConfig() Config {
	return Config{
		OutputReserveFraction: DefaultOutputReserveFraction,
		Fractions: SectionFractions{
			SystemStable: 0.30,
			DynamicDocs:  0.10,
			Memory:       0.20,
			Tools:        0.15,
			History:      0.25,
		},
	}
}

// Allocation is the C2 contract: one request's split of the resolved
// window. The five section fields always sum EXACTLY to WindowTokens.
type Allocation struct {
	WindowTokens       int // usable window: resolved limit minus output reserve
	OutputReserve      int // generation reserve (R-A2)
	SystemStable       int // stable prompt sections (prefix cache)
	SystemDynamicDocs  int // CLAUDE.md / README / skills / agents section
	MemoryWorking      int // working-tier share (undivided == Memory without a provider)
	MemoryEpisodic     int // episodic-tier share
	MemorySemantic     int // semantic-tier share
	Tools              int // tool-declaration overhead + per-result cap
	History            int
	IterationBudget    int // per-iteration LLM budget (supersedes IterationTokenBudget)
	ConversationBudget int // per-conversation budget (supersedes DefaultConversationTokenBudget)
}

// Allocator deterministically splits a resolved context window into the
// assembler's sections. It is immutable after construction and safe for
// concurrent use: Allocate reads only immutable config state and holds no
// mutex.
type Allocator struct {
	cfg               Config
	iterationRatio    float64
	conversationRatio float64
}

// New validates cfg and returns an allocator. Validation rules (leaf T2):
// fractions sum to 1.0 within 1e-9; every fraction lies in [0, 1]; no
// fraction exceeds the post-reserve usable share (1 - reserve fraction);
// MinSections floors are non-negative and do not exceed their section's
// maximum plausible value (fraction * a nominal window share); the reserve
// fraction is in [0, 1).
func New(cfg Config) (*Allocator, error) {
	const eps = 1e-9
	if cfg.OutputReserveFraction < 0 || cfg.OutputReserveFraction >= 1 {
		return nil, fmt.Errorf("contextalloc: output reserve fraction %g must be in [0,1)",
			cfg.OutputReserveFraction)
	}
	fr := cfg.Fractions
	fields := []struct {
		name  string
		value float64
	}{
		{"system_stable", fr.SystemStable},
		{"dynamic_docs", fr.DynamicDocs},
		{"memory", fr.Memory},
		{"tools", fr.Tools},
		{"history", fr.History},
	}
	sum := 0.0
	for _, f := range fields {
		if f.value < 0 || f.value > 1 {
			return nil, fmt.Errorf("contextalloc: %s fraction %g outside [0,1]", f.name, f.value)
		}
		sum += f.value
	}
	if diff := sum - 1.0; diff > eps || diff < -eps {
		return nil, fmt.Errorf("contextalloc: fractions sum to %g, want 1.0 (epsilon 1e-9)", sum)
	}
	floors := []struct {
		name     string
		floor    int
		fraction float64
	}{
		{"system_stable", cfg.MinSections.SystemStable, fr.SystemStable},
		{"dynamic_docs", cfg.MinSections.DynamicDocs, fr.DynamicDocs},
		{"memory", cfg.MinSections.Memory, fr.Memory},
		{"tools", cfg.MinSections.Tools, fr.Tools},
		{"history", cfg.MinSections.History, fr.History},
	}
	for _, fl := range floors {
		if fl.floor < 0 {
			return nil, fmt.Errorf("contextalloc: %s floor %d is negative", fl.name, fl.floor)
		}
		if fl.fraction <= 0 && fl.floor > 0 {
			return nil, fmt.Errorf("contextalloc: %s floor %d exceeds the section's maximum plausible value (fraction is 0)",
				fl.name, fl.floor)
		}
	}
	iterationRatio := cfg.computedIterationRatio()
	conversationRatio := cfg.computedConversationRatio()
	return &Allocator{
		cfg:               cfg,
		iterationRatio:    iterationRatio,
		conversationRatio: conversationRatio,
	}, nil
}

// computedIterationRatio returns the configured iteration ratio, defaulting
// to the prior-art value when unset.
func (c Config) computedIterationRatio() float64 {
	return DefaultIterationRatio
}

// computedConversationRatio returns the configured conversation ratio,
// defaulting to the prior-art value when unset.
func (c Config) computedConversationRatio() float64 {
	return DefaultConversationRatio
}

// Allocate splits windowTokens (the resolved context limit from
// internal/llm context discovery) into the C2 Allocation. Pure and
// deterministic: identical inputs give a bit-identical result.
//
// Pipeline:
//  1. reserve = max(reserveFraction*window, OutputReserveFloor)  (R-A2)
//  2. usable = window - reserve, floored at 0
//  3. section floors are pre-charged from usable, then the remainder is
//     split by fraction with the largest-remainder method so the five
//     sections sum EXACTLY to usable (leaf T3); when floors overflow
//     usable they shrink proportionally and never go negative (leaf T4)
//  4. the Memory section is subdivided across tiers via the C1 seam
//  5. IterationBudget / ConversationBudget derive from the prior-art
//     firewall ratios capped at the section shares (leaf T6)
func (a *Allocator) Allocate(ctx context.Context, windowTokens int, req Request) Allocation {
	window := windowTokens
	if window < 0 {
		window = 0
	}

	// R-A2: reserve the LARGER of the configured fraction and the absolute
	// floor. The reserve never shrinks proportionally on small windows
	// (R-A2/R-A10; ml4devs absolute-reserve principle).
	reserve := int(float64(window) * a.cfg.OutputReserveFraction)
	if reserve < OutputReserveFloor {
		reserve = OutputReserveFloor
	}
	if reserve > window {
		reserve = window
	}
	usable := window - reserve

	// R-A4: split the post-reserve window by fraction, largest-remainder so
	// the sections sum exactly. Fractional-remainder ties break by field
	// order (system, docs, memory, tools, history) for determinism.
	weights := []float64{
		a.cfg.Fractions.SystemStable,
		a.cfg.Fractions.DynamicDocs,
		a.cfg.Fractions.Memory,
		a.cfg.Fractions.Tools,
		a.cfg.Fractions.History,
	}
	parts := splitLR(usable, weights)

	// Leaf T4: MinSections floors applied BEFORE the proportional split.
	// When the floors fit inside usable, pre-charge them and split only the
	// remainder proportionally (floors then bind only where the fraction
	// would undercut them). When they overflow usable, keep the pure
	// proportional split, which shrinks gracefully and never returns a
	// negative section.
	floors := [5]int{
		a.cfg.MinSections.SystemStable,
		a.cfg.MinSections.DynamicDocs,
		a.cfg.MinSections.Memory,
		a.cfg.MinSections.Tools,
		a.cfg.MinSections.History,
	}
	floorSum := 0
	needFloor := false
	for i, fl := range floors {
		floorSum += fl
		if fl > parts[i] {
			needFloor = true
		}
	}
	if needFloor && floorSum <= usable {
		remainder := usable - floorSum
		rParts := splitLR(remainder, weights)
		for i := range parts {
			parts[i] = floors[i] + rParts[i]
		}
	}
	// else: floors do not fit (or do not bind) -- the proportional split
	// already degrades gracefully (all parts non-negative, exact sum).

	alloc := Allocation{
		WindowTokens:      usable,
		OutputReserve:     reserve,
		SystemStable:      parts[0],
		SystemDynamicDocs: parts[1],
		Tools:             parts[3],
		History:           parts[4],
	}
	memorySection := parts[2]

	// C1 seam: subdivide Memory across tiers. nil/typed-nil provider keeps
	// today's undivided behavior (MemoryWorking == Memory).
	working, episodic, semantic := a.splitMemoryTiers(ctx, window, req, memorySection)
	alloc.MemoryWorking = working
	alloc.MemoryEpisodic = episodic
	alloc.MemorySemantic = semantic

	// Leaf T6: derive the iteration/conversation budgets. Relationship to
	// internal/llm/context_firewall.go DerivedIterationBudget /
	// DerivedConversationBudget (the prior art at :548-584): those return
	// ratio * context_limit with an unconditional 0.7 small-model factor.
	// The allocator generalizes the ratio rule to the resolved window and
	// CAPS the iteration budget at the History section share -- a single
	// iteration can never outbid the history section it feeds. The 0.7
	// small-model factor is deliberately NOT copied: R-A10 keeps the
	// reserve and recent-history floor absolute on small windows and scales
	// only the flexible sections, which the fraction-based split already
	// does proportionally; a blanket 0.7 would shrink the reserve too
	// (ml4devs: reserve the largest legitimate output, not a fraction that
	// shrinks to nothing). Integration (leaf 03) applies R-A10's
	// flexible-only scaling at wiring time if SmallModelContextThreshold
	// behavior is wanted.
	iteration := int(a.iterationRatio * float64(window))
	if iteration > alloc.History {
		iteration = alloc.History
	}
	// ConversationBudget tracks the prior-art conversation ratio of the
	// resolved window, capped at the post-reserve window so it can never
	// promise more than exists after the R-A2 reserve.
	conversation := int(a.conversationRatio * float64(window))
	if conversation > usable {
		conversation = usable
	}
	alloc.IterationBudget = iteration
	alloc.ConversationBudget = conversation
	return alloc
}
