package llm

// Price groups + paid spillway — sovereign-router-parity tree, leaf 09,
// Contract C10. Mirrors Sovereign Router's build_spillway_candidates
// (src/gateway.py:765-800) and its sort key (gateway.py:746-759): "free
// first, paid only on exhaustion."
//
// C10 freeze: the spillway NEVER reorders alias rotation, NEVER touches a
// sticky pin, NEVER mutates AliasHealth or endpointBlocks, and NEVER
// resolves an untagged alias. It is opt-in at the call site — a caller
// that names a groups step gets tier stepping; default single-alias
// resolution is byte-identical. This is tier ordering, NOT a multi-tenant
// gate: no per-identity policy, no premium gate, no per-client lanes.
//
// ResolveSpillway is exported but has no mandatory internal call site for
// v1 — it is an API surface for future consumers (a daemon chat option or
// CLI flag); available-but-uncalled is an explicit contract decision
// (leaf 09 T8), not dead code.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// PriceGroup is the routing tier an alias belongs to. The empty value is
// "untagged": legacy behavior — the alias resolves only when explicitly
// selected and is invisible to every spillway step.
type PriceGroup string

const (
	PriceGroupFree    PriceGroup = "free"
	PriceGroupPaid    PriceGroup = "paid"
	PriceGroupPremium PriceGroup = "premium"
)

// ParsePriceGroup maps a config string to its PriceGroup. "" and any
// unrecognized value map to the zero value (untagged) — an unknown tag
// must never silently become eligible for a paid tier (C10).
func ParsePriceGroup(s string) PriceGroup {
	switch PriceGroup(strings.ToLower(strings.TrimSpace(s))) {
	case PriceGroupFree:
		return PriceGroupFree
	case PriceGroupPaid:
		return PriceGroupPaid
	case PriceGroupPremium:
		return PriceGroupPremium
	default:
		return ""
	}
}

// ErrNoEligibleSpillwayGroup is returned by ResolveSpillway when no alias
// in the requested groups is both tagged into the groups and fully
// eligible under the selection filters. It joins the ErrAll*Blocked
// sentinel family (ErrAllModelsQuotaBlocked, ErrAllEndpointsBlocked,
// ErrTierFloor): a classified terminal condition, checked with errors.Is,
// never string matching.
var ErrNoEligibleSpillwayGroup = errors.New("llm: no eligible alias in the requested spillway price groups")

// spillwayCostEpsilon is the float compare tolerance for the frozen sort key
// (leaf 09: "never bare != on floats", repo pitfall 26).
const spillwayCostEpsilon = 1e-9

// aliasPriceGroup resolves an alias's configured price group from the
// immutable providers config. r.aliases (*AliasEntry) carries no tier
// field — the tag lives on ModelAliasEntry (declared inert by leaf 01),
// so the runtime lookup goes through r.config.ModelAliases. An alias
// missing from the config map (or an absent config) parses to untagged,
// which is invisible to the spillway.
func (r *Resolver) aliasPriceGroup(name string) PriceGroup {
	if r.config == nil {
		return ""
	}
	entry, ok := r.config.ModelAliases[name]
	if !ok {
		return ""
	}
	return ParsePriceGroup(entry.PriceGroup)
}

// AliasesByPriceGroup returns the names of aliases whose configured
// PriceGroup parses to g, sorted by name for cross-run determinism (T9;
// raw alias-map iteration order is randomized). An empty g returns nil:
// untagged aliases never participate in a spillway step.
//
// The map read happens under r.mu with the snapshot released before the
// sort — no lock is held across filtering, sorting, or any caller code.
// r.mu is a sync.Mutex today (the C10 "RLock" wording assumes an RWMutex
// the tree does not have); the critical section is map reads only.
func (r *Resolver) AliasesByPriceGroup(g PriceGroup) []string {
	if g == "" {
		return nil
	}
	r.mu.Lock()
	names := make([]string, 0, len(r.aliases))
	for name := range r.aliases {
		if r.aliasPriceGroup(name) == g {
			names = append(names, name)
		}
	}
	r.mu.Unlock()
	sort.Strings(names)
	return names
}

// spillwayCandidate is a fully-filtered alias plus the cost/quality pair
// the frozen sort key orders by.
type spillwayCandidate struct {
	alias   string
	model   *ModelConfig // the alias's min-TotalCost member (the alias's cost basis)
	cost    float64
	quality int
}

// spillwayAliasCost returns the min TotalCost over the alias's member
// models — the alias's cost basis for the frozen
// (TotalCost asc, quality desc) sort key. Callers must hold no lock; the
// entry is an immutable snapshot taken under r.mu.
func spillwayBestMember(entry *AliasEntry) *ModelConfig {
	var best *ModelConfig
	for _, m := range entry.Models {
		if m == nil {
			continue
		}
		if best == nil || m.TotalCost() < best.TotalCost()-spillwayCostEpsilon {
			best = m
		}
	}
	return best
}

// ResolveSpillway selects the cheapest capable alias across the given
// groups ("free", "paid", "premium"), mirroring Sovereign Router's
// build_spillway_candidates (gateway.py:765-800) with the frozen sort key
// (gateway.py:746-759): (TotalCost asc, quality desc), epsilon float
// compare, stable. The alias's cost basis is its min-TotalCost member;
// the quality basis is that member's QualityFor(fc.Capability) — Quality
// is zero for absent quality blocks (feature inert, never an error).
//
// Candidate eligibility (both required):
//   - the alias's configured PriceGroup parses to one of `groups`;
//   - ALL member models pass runSelectionFilters == VerdictAllow for fc
//     (posture, quality, admission, budget — the C1 chain, registration
//     order fixed by the master contract).
//
// groups is one tier step: the caller implements free-first-then-paid by
// invoking ResolveSpillway(ctx, []string{"free"}, fc) and, on
// ErrNoEligibleSpillwayGroup (or attempt exhaustion/failure downstream),
// re-invoking with []string{"paid", "premium"}. Within one call the
// groups form a single candidate pool ordered purely by the frozen sort
// key — group order is not a sub-priority inside a step.
//
// UNtagged aliases (empty PriceGroup) are invisible to every spillway
// step. Selection never mutates rotation state: no CurrentIndex, no
// sticky pins, no AliasHealth, no endpointBlocks — the map is snapshotted
// under r.mu (map reads only), the lock is released, and filtering/sorting
// run on the snapshot. Returns the min-cost member *ModelConfig (shared
// pointer; callers must not mutate), the alias name, or
// ErrNoEligibleSpillwayGroup when nothing in the groups is eligible.
//
// Available-but-uncalled in v1 (leaf 09 T8): no internal call site invokes
// this yet; wiring a daemon chat option or CLI flag is a future consumer's
// decision.
func (r *Resolver) ResolveSpillway(ctx context.Context, groups []string, fc *FilterCtx) (*ModelConfig, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if fc == nil {
		// A nil FilterCtx must not panic inside the filters (no bare
		// panics, ever): degrade to a zero-value context, under which
		// posture passes (nothing to scan), the quality floor is inert
		// (difficulty 0), and admission allows (no estimate).
		fc = &FilterCtx{}
	}

	// Parse the group list once. Unknown/empty group strings parse to
	// untagged, which is invisible — a typo'd group silently yields zero
	// candidates rather than accidentally selecting an untagged alias.
	wanted := make(map[PriceGroup]struct{}, len(groups))
	for _, g := range groups {
		if p := ParsePriceGroup(g); p != "" {
			wanted[p] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil, "", fmt.Errorf("%w: groups %q (none parse to a price group)", ErrNoEligibleSpillwayGroup, strings.Join(groups, ","))
	}

	// Snapshot alias names + entries under r.mu (map reads only), then
	// release before any filtering. Sorting the names first makes the
	// stable sort fully deterministic cross-run (T9): equal (cost,
	// quality) candidates order by alias name.
	r.mu.Lock()
	names := make([]string, 0, len(r.aliases))
	for name := range r.aliases {
		names = append(names, name)
	}
	snapshot := make(map[string]*AliasEntry, len(r.aliases))
	groupsByName := make(map[string]PriceGroup, len(r.aliases))
	for _, name := range names {
		snapshot[name] = r.aliases[name]
		groupsByName[name] = r.aliasPriceGroup(name)
	}
	r.mu.Unlock()
	sort.Strings(names)

	candidates := make([]spillwayCandidate, 0, len(names))
	for _, name := range names {
		g := groupsByName[name]
		if _, ok := wanted[g]; !ok {
			continue // untagged or a group outside this step — invisible (C10)
		}
		entry := snapshot[name]
		if entry == nil {
			continue
		}
		// Every member must pass the selection chain; one failing member
		// disqualifies the whole alias for this step (C10: "whose members
		// all pass runSelectionFilters").
		eligible := true
		for _, m := range entry.Models {
			if m == nil {
				eligible = false
				break
			}
			verdict, _ := r.runSelectionFilters(&Candidate{
				Model:      m,
				Alias:      name,
				Capability: fc.Capability,
				EstInput:   fc.EstInput,
				EstOutput:  fc.EstOutput,
			}, fc)
			if verdict != VerdictAllow {
				eligible = false
				break
			}
		}
		if !eligible {
			continue
		}
		best := spillwayBestMember(entry)
		if best == nil {
			continue
		}
		candidates = append(candidates, spillwayCandidate{
			alias:   name,
			model:   best,
			cost:    best.TotalCost(),
			quality: best.Quality.QualityFor(QualityClass(fc.Capability)),
		})
	}

	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("%w: groups %q", ErrNoEligibleSpillwayGroup, strings.Join(groups, ","))
	}

	// Frozen sort key (gateway.py:746-759, leaf 09): cost asc (epsilon
	// compare), quality desc, stable — input order (alias name asc, from
	// the snapshot sort above) breaks remaining ties deterministically.
	sort.SliceStable(candidates, func(i, j int) bool {
		ci, cj := candidates[i].cost, candidates[j].cost
		if math.Abs(ci-cj) > spillwayCostEpsilon {
			return ci < cj
		}
		return candidates[i].quality > candidates[j].quality
	})

	winner := candidates[0]
	return winner.model, winner.alias, nil
}
