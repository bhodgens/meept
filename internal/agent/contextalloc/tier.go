package contextalloc

import (
	"context"
	"reflect"
	"sort"
)

// TierName identifies a memory tier.
type TierName string

const (
	// TierWorking is the short-lived scratch tier (conversation-buffer
	// facts); branch B's working-tier store feeds it (master contract C1).
	TierWorking TierName = "working"
	// TierEpisodic is the existing episodic memory tier.
	TierEpisodic TierName = "episodic"
	// TierSemantic is the distilled semantic memory tier.
	TierSemantic TierName = "semantic"
)

// RecallFunc returns candidate memory strings for query, at most limit
// items. Implementations MUST NOT synchronously run consolidation and MUST
// be safe for concurrent use. The allocator never calls Recall itself; it
// only carries the handle for the assembly layer (leaf 03).
type RecallFunc func(ctx context.Context, query string, limit int) []string

// TierBudget is one tier's allocation for the current request.
// Ceiling is the tier's configured cap hint in tokens (0 = allocator
// decides). Recall is nil when the tier is unavailable/disabled.
type TierBudget struct {
	Tier    TierName
	Ceiling int
	Recall  RecallFunc
}

// TierBudgetProvider supplies per-tier budgets and recall handles for the
// allocator's memory split. Slice order is insignificant: the allocator
// canonicalizes by tier name before splitting.
//
// Seam rule (master C1): a nil provider -- including a TYPED-nil interface
// value -- is treated as absent and restores undivided behavior exactly
// (MemoryWorking == Memory, episodic/semantic 0). See isAbsentProvider.
type TierBudgetProvider interface {
	TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget
}

// MinTierTokens is the documented per-tier minimum: once a tier is present
// in the provider's response, its split never drops below this many tokens
// while the Memory section can fund it (256 tokens/tier; the section-level
// analogue of the R-A4 memory floor of 512 tokens). When the Memory section
// is too small to fund every tier at this minimum, the minimum is skipped
// rather than inventing tokens.
const MinTierTokens = 256

// tierOrder is the canonical processing order. Determinism requirement:
// largest-remainder ties break by index, so the tier split sorts input into
// this fixed order and the result is independent of the provider's slice
// order (contract C1: "slice order is insignificant").
var tierOrder = []TierName{TierWorking, TierEpisodic, TierSemantic}

// splitMemoryTiers divides the Memory section across the provider's tiers.
//
// Semantics (leaf T5, findings R-A4/R-A5):
//   - no provider (nil or typed-nil): Memory stays undivided --
//     MemoryWorking == Memory, episodic/semantic 0 (today's behavior);
//   - tiers with Ceiling > 0 receive exactly Ceiling (the cap hint);
//   - tiers with Ceiling == 0 equal-split the remainder ("allocator
//     decides"); with no uncapped tier, surplus above the caps is
//     distributed proportionally to the ceilings themselves -- caps are
//     hints and the section must still sum to Memory exactly;
//   - when the ceilings alone exceed Memory, they shrink proportionally
//     (largest-remainder, same graceful-degradation rule as section
//     floors) and uncapped tiers receive 0;
//   - every present tier is then floored at MinTierTokens when the section
//     can fund it (deducted from the largest sibling tier);
//   - unknown tier names are ignored; duplicate names keep the first entry;
//   - a provider that returns no usable tiers behaves as absent.
//
// The split always sums EXACTLY to memory.
func (a *Allocator) splitMemoryTiers(ctx context.Context, windowTokens int, req Request, memory int) (working, episodic, semantic int) {
	if memory <= 0 {
		return 0, 0, 0
	}
	provider := a.cfg.TierProvider
	if isAbsentProvider(provider) {
		return memory, 0, 0
	}
	listed := provider.TierBudgets(ctx, req.Query, windowTokens)

	// Dedupe first-wins; unknown names are dropped entirely.
	byTier := make(map[TierName]TierBudget, len(tierOrder))
	for _, tb := range listed {
		if !knownTier(tb.Tier) {
			continue
		}
		if _, dup := byTier[tb.Tier]; dup {
			continue
		}
		byTier[tb.Tier] = tb
	}

	// Canonicalize to a fixed order so slice order cannot change results.
	present := make([]TierBudget, 0, len(tierOrder))
	for _, name := range tierOrder {
		if tb, ok := byTier[name]; ok {
			present = append(present, tb)
		}
	}
	if len(present) == 0 {
		return memory, 0, 0
	}

	allocs := make([]int, len(present))
	capSum := 0
	freeCount := 0
	for _, tb := range present {
		if tb.Ceiling > 0 {
			capSum += tb.Ceiling
		} else {
			freeCount++
		}
	}

	weights := make([]float64, len(present))
	if capSum >= memory {
		// Ceilings alone overflow the section: shrink them proportionally;
		// uncapped tiers get nothing this round.
		for i, tb := range present {
			if tb.Ceiling > 0 {
				weights[i] = float64(tb.Ceiling)
			}
		}
		allocs = splitLR(memory, weights)
	} else {
		for i, tb := range present {
			if tb.Ceiling > 0 {
				allocs[i] = tb.Ceiling
			}
		}
		remainder := memory - capSum
		if freeCount > 0 {
			freeWeights := make([]float64, len(present))
			for i := range present {
				if present[i].Ceiling <= 0 {
					freeWeights[i] = 1
				}
			}
			for i, share := range splitLR(remainder, freeWeights) {
				allocs[i] += share
			}
		} else {
			// Every tier capped and caps under-fill Memory: distribute the
			// surplus proportionally to the ceilings (caps are hints; the
			// section never wastes budget).
			for i, tb := range present {
				weights[i] = float64(tb.Ceiling)
			}
			allocs = splitLR(memory, weights)
		}
	}

	allocs = applyTierMinimum(present, allocs, memory)

	for i, tb := range present {
		switch tb.Tier {
		case TierWorking:
			working = allocs[i]
		case TierEpisodic:
			episodic = allocs[i]
		case TierSemantic:
			semantic = allocs[i]
		}
	}
	return working, episodic, semantic
}

// applyTierMinimum raises any tier below MinTierTokens to it, funded by the
// largest sibling tier, when the Memory section can fund every tier at the
// minimum. It preserves the exact sum.
func applyTierMinimum(present []TierBudget, allocs []int, memory int) []int {
	if memory < MinTierTokens*len(present) {
		// Section too small to fund the minimum for every tier: skip rather
		// than distort the proportional split.
		return allocs
	}
	out := make([]int, len(allocs))
	copy(out, allocs)
	for i := range out {
		if out[i] >= MinTierTokens {
			continue
		}
		deficit := MinTierTokens - out[i]
		donor := -1
		for j := range out {
			if j == i {
				continue
			}
			if out[j] > MinTierTokens && (donor == -1 || out[j] > out[donor]) {
				donor = j
			}
		}
		if donor == -1 || out[donor] < deficit+MinTierTokens && out[donor] < deficit+out[i]+1 {
			// No tier can give up the deficit; leave the split as-is.
			continue
		}
		give := deficit
		if out[donor]-give < MinTierTokens {
			continue
		}
		out[donor] -= give
		out[i] += give
	}
	return out
}

// knownTier reports whether name is one of the three contract tiers.
func knownTier(name TierName) bool {
	switch name {
	case TierWorking, TierEpisodic, TierSemantic:
		return true
	}
	return false
}

// isAbsentProvider treats both a bare nil and a TYPED-nil interface value
// (e.g. (*myProvider)(nil)) as absent -- the typed-nil guard convention
// from the master contract C1 seam rule.
func isAbsentProvider(p TierBudgetProvider) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan,
		reflect.Func, reflect.UnsafePointer:
		return v.IsNil()
	}
	return false
}

// splitLR divides total into len(weights) non-negative parts proportional
// to the weights using the largest-remainder method: every part equals
// floor(total*w/sumW) or that plus one, and the parts sum EXACTLY to total.
// Fractional-remainder ties break by index order, so callers pass weights
// in a canonical order to stay deterministic. All-zero (or negative) weight
// vectors split equally. No map iteration and no float accumulation across
// iterations of varying order: identical inputs always produce identical
// parts.
func splitLR(total int, weights []float64) []int {
	parts := make([]int, len(weights))
	if total <= 0 || len(weights) == 0 {
		return parts
	}
	sumW := 0.0
	for _, w := range weights {
		if w > 0 {
			sumW += w
		}
	}
	if sumW <= 0 {
		base := total / len(weights)
		rem := total - base*len(weights)
		for i := range parts {
			parts[i] = base
			if i < rem {
				parts[i]++
			}
		}
		return parts
	}
	fracs := make([]float64, len(weights))
	used := 0
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		exact := float64(total) * w / sumW
		parts[i] = int(exact) // floor; weights are non-negative
		fracs[i] = exact - float64(parts[i])
		used += parts[i]
	}
	rem := total - used
	idx := make([]int, 0, len(weights))
	for i := range weights {
		if weights[i] > 0 {
			idx = append(idx, i)
		}
	}
	// Stable sort with a strict > comparison keeps index order on ties.
	sort.SliceStable(idx, func(a, b int) bool {
		return fracs[idx[a]] > fracs[idx[b]]
	})
	for k := 0; k < rem && k < len(idx); k++ {
		parts[idx[k]]++
	}
	return parts
}
