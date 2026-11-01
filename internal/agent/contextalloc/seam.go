package contextalloc

import (
	"context"
	"reflect"

	"github.com/caimlas/meept/internal/llm"
)

// LoopSeam is the loop-facing integration handle (leaf
// 01-context-assembler/03-integration.md T2): it binds an Allocator, an
// optional TierBudgetProvider, and a tokenizer into the single object the
// agent loop consults per turn.
//
// Nil / typed-nil rules (the seam contract): a seam constructed from a nil
// *Allocator -- or from a typed-nil provider (var p *myProv;
// NewLoopSeam(a, p, nil)) -- is DISABLED: MemoryContext reports ok=false
// and every caller takes today's path byte-for-byte. The typed-nil guard
// follows isAbsentProvider in tier.go (reflect-based, per the
// internal/daemon/AGENTS.md typed-nil wiring rule).
type LoopSeam struct {
	alloc    *Allocator
	provider TierBudgetProvider
	token    llm.Tokenizer
}

// NewLoopSeam wires the seam. Any disabled component (nil allocator,
// nil/typed-nil provider) disables the WHOLE seam: callers check
// Enabled()/the ok flag rather than partial state, so a half-wired
// configuration can never split the prompt path.
func NewLoopSeam(a *Allocator, prov TierBudgetProvider, tk llm.Tokenizer) *LoopSeam {
	if a == nil || isAbsentProvider(prov) {
		return &LoopSeam{}
	}
	return &LoopSeam{alloc: a, provider: prov, token: tk}
}

// Enabled reports whether the seam will act. A nil receiver is disabled
// (every method is nil-safe for the same reason).
func (s *LoopSeam) Enabled() bool {
	return s != nil && s.alloc != nil && !isAbsentProvider(s.provider)
}

// Allocator exposes the underlying allocator for window resolution
// (Allocate). Nil on a disabled seam; callers must check Enabled first.
func (s *LoopSeam) Allocator() *Allocator {
	if s == nil {
		return nil
	}
	return s.alloc
}

// Tokenizer returns the wired tokenizer (nil = caller falls back to the
// heuristic).
func (s *LoopSeam) Tokenizer() llm.Tokenizer {
	if s == nil {
		return nil
	}
	return s.token
}

// MemoryContext resolves the tier budgets and recall handles for one
// request through the C1 provider seam.
//
//   - ok=false: seam disabled (nil/typed-nil anywhere) -- the caller takes
//     today's undivided memory path (MaxMemoryContextTokens). No provider
//     call happens, no allocation of recall.
//   - ok=true: budgets carries a non-empty, allocator-derived token
//     budget per tier present in the provider response, and items are the
//     recalled candidates per tier (nil where the tier has no Recall or
//     recalled nothing). The caller renders items under budgets
//     tier-by-tier.
func (s *LoopSeam) MemoryContext(ctx context.Context, query string, windowTokens int) (
	items map[TierName][]string, budgets map[TierName]int, ok bool,
) {
	if !s.Enabled() {
		return nil, nil, false
	}
	req := Request{Query: query}
	alloc := s.allocate(ctx, windowTokens, req)

	listed := s.provider.TierBudgets(ctx, query, windowTokens)
	if len(listed) == 0 {
		return nil, nil, false
	}

	sectionByTier := map[TierName]int{
		TierWorking:  alloc.MemoryWorking,
		TierEpisodic: alloc.MemoryEpisodic,
		TierSemantic: alloc.MemorySemantic,
	}
	// The three named tiers only; unknown names cannot be budgeted.
	budgets = make(map[TierName]int, 3)
	items = make(map[TierName][]string, 3)
	for _, tb := range listed {
		share, known := sectionByTier[tb.Tier]
		if !known {
			continue
		}
		if _, dup := budgets[tb.Tier]; dup {
			continue
		}
		budgets[tb.Tier] = share
		if tb.Recall != nil {
			items[tb.Tier] = tb.Recall(ctx, query, recallItemCount(share))
		} else {
			items[tb.Tier] = nil
		}
	}
	if len(budgets) == 0 {
		return nil, nil, false
	}
	return items, budgets, true
}

// allocate runs the section split with the seam's OWN provider wired
// into the allocator config, so the tier budgets the loop reads back
// (MemoryWorking/Episodic/Semantic) reflect the same provider this seam
// queries for recall. An allocator already carrying the same provider is
// used as-is.
func (s *LoopSeam) allocate(ctx context.Context, windowTokens int, req Request) Allocation {
	if sameProvider(s.alloc.cfg.TierProvider, s.provider) {
		return s.alloc.Allocate(ctx, windowTokens, req)
	}
	cfg := s.alloc.cfg
	cfg.TierProvider = s.provider
	derived, err := New(cfg)
	if err != nil {
		// The config came from an already-validated allocator, so
		// re-validation cannot fail. Fall back to the original allocator
		// rather than guessing if it ever does.
		return s.alloc.Allocate(ctx, windowTokens, req)
	}
	return derived.Allocate(ctx, windowTokens, req)
}

// sameProvider reports whether two provider interface values hold the
// same underlying object (or are both absent).
func sameProvider(a, b TierBudgetProvider) bool {
	if isAbsentProvider(a) && isAbsentProvider(b) {
		return true
	}
	if isAbsentProvider(a) != isAbsentProvider(b) {
		return false
	}
	ra, rb := reflect.ValueOf(a), reflect.ValueOf(b)
	if ra.Kind() != reflect.Pointer || rb.Kind() != reflect.Pointer {
		return a == b
	}
	return ra.Pointer() == rb.Pointer()
}

// recallItemCount derives the per-tier recall candidate cap from the
// tier's token budget under the repo's 3-chars/token heuristic: one item
// is worth roughly a rendered paragraph (~300 runes ~= 100 tokens), so
// the cap is budget/100 clamped to [1, 20]. Deterministic; no I/O.
func recallItemCount(budgetTokens int) int {
	if budgetTokens <= 0 {
		return 0
	}
	n := budgetTokens / 100
	if n < 1 {
		n = 1
	}
	if n > 20 {
		n = 20
	}
	return n
}
