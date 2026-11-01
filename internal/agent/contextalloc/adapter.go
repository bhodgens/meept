package contextalloc

import (
	"context"
	"reflect"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/working"
)

// tierStores bundles the branch B store surfaces the adapter maps onto
// TierBudget{Ceiling, Recall} (context-memory-engine leaf
// 03-integration.md T6; contract C1).
//
// EVERY field is optional: a nil store means that tier is absent from the
// provider response (never a panic, never a zero-value tier slot), which
// is how the tree builds and runs before branch B is wired. Nil-pointer
// stores are tolerated via typed-nil handling in TierBudgets, so daemon
// wiring can pass its (possibly nil) fields directly.
type tierStores struct {
	// Working is branch B's working-tier ring store (C4). Its Recall is
	// used directly (RecallFunc-compatible signature, pinned by the
	// working package's own doc).
	Working *working.Store
	// WorkingCeiling overrides the allocator-decided working share when
	// > 0 (cap hint semantics from C1; 0 = allocator decides).
	WorkingCeiling int
	// Episodic is the existing FTS5 episodic store (nil = absent).
	Episodic *memory.EpisodicMemory
	// EpisodicCeiling overrides the episodic share when > 0.
	EpisodicCeiling int
	// Semantic recalls distilled claims through the memory manager's
	// semantic path (vector when configured, keyword fallback otherwise).
	Semantic *memory.Manager
	// SemanticCeiling overrides the semantic share when > 0.
	SemanticCeiling int
}

// TierStoreProvider is the concrete TierBudgetProvider the daemon wires
// (leaf T6: the adapter lives HERE in internal/agent/contextalloc and is
// the ONLY importer of internal/memory in this package -- internal/memory
// never imports this package, preserving the direction rule).
type TierStoreProvider struct {
	stores tierStores
	tk     llm.Tokenizer
}

// compile-time proof the adapter satisfies the C1 seam.
var _ TierBudgetProvider = (*TierStoreProvider)(nil)

// NewTierStoreProvider builds the adapter over the given branch B stores.
func NewTierStoreProvider(work *working.Store, workCeiling int, epi *memory.EpisodicMemory, epiCeiling int, sem *memory.Manager, semCeiling int, tk llm.Tokenizer) *TierStoreProvider {
	p := &TierStoreProvider{
		stores: tierStores{
			Working:         work,
			WorkingCeiling:  workCeiling,
			Episodic:        epi,
			EpisodicCeiling: epiCeiling,
			Semantic:        sem,
			SemanticCeiling: semCeiling,
		},
		tk: tk,
	}
	if p.absent() {
		// Return a typed-nil-adjacent shape: the provider is present as a
		// value but reports no tiers, which the allocator's seam rule
		// already treats as absent. Keeping the concrete struct (not nil)
		// lets wiring stay unconditional.
		return &TierStoreProvider{tk: tk}
	}
	return p
}

// absent reports whether no tier store is wired (typed-nil safe: a nil
// *working.Store etc. counts as absent for that tier, and a struct with
// every tier nil is absent overall).
func (p *TierStoreProvider) absent() bool {
	if p == nil {
		return true
	}
	s := p.stores
	workAbsent := s.Working == nil || isNilPointer(s.Working)
	epiAbsent := s.Episodic == nil || isNilPointer(s.Episodic)
	semAbsent := s.Semantic == nil || isNilPointer(s.Semantic)
	return workAbsent && epiAbsent && semAbsent
}

// Enabled reports whether at least one tier store is wired.
func (p *TierStoreProvider) Enabled() bool {
	return !p.absent()
}

// isNilPointer reports whether v is a nil pointer behind an any-typed
// value (the typed-nil guard used on every concrete store field). All
// store fields are concrete pointer types, so the reflect check is the
// belt-and-braces wrapper used by the tierStores accessors.
func isNilPointer(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan,
		reflect.Func, reflect.UnsafePointer:
		return rv.IsNil()
	}
	return false
}

// TierBudgets maps the wired stores onto TierBudget entries. Order is
// insignificant to the allocator (it canonicalizes), but the emission
// order here is fixed working/episodic/semantic for readability of
// traces. A nil (or typed-nil) store is skipped entirely; a store whose
// recall errors returns no entry rather than a half-broken one.
func (p *TierStoreProvider) TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget {
	if p.absent() {
		return []TierBudget{}
	}
	s := p.stores
	out := make([]TierBudget, 0, 3)
	if s.Working != nil && !isNilPointer(s.Working) {
		store := s.Working
		out = append(out, TierBudget{
			Tier:    TierWorking,
			Ceiling: s.WorkingCeiling,
			Recall:  store.Recall,
		})
	}
	if s.Episodic != nil && !isNilPointer(s.Episodic) {
		epi := s.Episodic
		out = append(out, TierBudget{
			Tier:    TierEpisodic,
			Ceiling: s.EpisodicCeiling,
			Recall: func(ctx context.Context, q string, limit int) []string {
				return episodicRecall(ctx, epi, q, limit)
			},
		})
	}
	if s.Semantic != nil && !isNilPointer(s.Semantic) {
		mgr := s.Semantic
		out = append(out, TierBudget{
			Tier:    TierSemantic,
			Ceiling: s.SemanticCeiling,
			Recall: func(ctx context.Context, q string, limit int) []string {
				return semanticRecall(ctx, mgr, q, limit)
			},
		})
	}
	return out
}

// episodicRecall adapts EpisodicMemory.Search to RecallFunc. Errors and
// zero-hit queries return an empty (non-nil-safe) result: recall is
// advisory and never fails a turn.
func episodicRecall(ctx context.Context, epi *memory.EpisodicMemory, query string, limit int) []string {
	if epi == nil || limit <= 0 {
		return nil
	}
	results, err := epi.Search(ctx, query, limit)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Memory.Content)
	}
	return out
}

// semanticRecall adapts Manager.SearchSemantic to RecallFunc. Same
// advisory contract as episodicRecall.
func semanticRecall(ctx context.Context, mgr *memory.Manager, query string, limit int) []string {
	if mgr == nil || limit <= 0 {
		return nil
	}
	results, err := mgr.SearchSemantic(ctx, query, limit)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Memory.Content)
	}
	return out
}
