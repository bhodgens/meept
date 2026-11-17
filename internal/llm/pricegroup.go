package llm

import (
	"context"
	"errors"
	"math"
	"sort"
)

// Alias price groups + paid spillway (Contract C10, sovereign-router-parity
// leaf 09; Sovereign Router gateway.py:765-800 build_spillway_candidates,
// :746-759 sort key).
//
// The spillway is a CALLER-SELECTED tier step (["free"], then
// ["paid","premium"] on exhaustion): it NEVER reorders alias rotation,
// NEVER touches a sticky pin, and NEVER mutates AliasHealth or the block
// maps. It reads r.aliases under RLock and returns. An untagged alias
// (PriceGroup "" — the default) is invisible to every spillway step, so
// callers that never invoke ResolveSpillway are byte-identical (opt-in
// gate, R9).

// PriceGroup is the alias routing-tier label (leaf 01 declares the
// ModelAliasEntry.PriceGroup field; this leaf owns the vocabulary).
type PriceGroup string

const (
	PriceGroupFree    PriceGroup = "free"
	PriceGroupPaid    PriceGroup = "paid"
	PriceGroupPremium PriceGroup = "premium"
)

// (costEpsilon for the frozen sort key lives in quality.go — the shared
// comparator's constant; no second declaration.)

// ParsePriceGroup maps a config string to the tier vocabulary. "" and
// unknown map to PriceGroup("") (untagged = legacy: this alias resolves
// only when explicitly selected and never participates in a spillway
// step).
func ParsePriceGroup(s string) PriceGroup {
	switch s {
	case string(PriceGroupFree):
		return PriceGroupFree
	case string(PriceGroupPaid):
		return PriceGroupPaid
	case string(PriceGroupPremium):
		return PriceGroupPremium
	default:
		return PriceGroup("")
	}
}

// ErrNoEligibleSpillwayGroup is returned by ResolveSpillway when a step's
// group has zero eligible aliases (untagged aliases are invisible to
// every step). It joins the ErrAll*Blocked sentinel family — check with
// errors.Is, never string matching.
var ErrNoEligibleSpillwayGroup = errors.New("llm: no eligible alias in any spillway group")

// AliasesByPriceGroup returns the names of aliases whose PriceGroup == g,
// sorted for determinism (leaf 09 T9: Go map iteration is random, so an
// unsorted return would make spillway selection order nondeterministic).
// A nil receiver returns nil.
func (r *Resolver) AliasesByPriceGroup(g PriceGroup) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name, entry := range r.aliases {
		if entry == nil {
			continue
		}
		if PriceGroup(entry.PriceGroup) == g {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// spillwayCandidate is one eligible alias of a spillway step with its
// frozen sort-key facts.
type spillwayCandidate struct {
	name    string
	model   *ModelConfig
	cost    float64
	quality int
}

// ResolveSpillway selects the cheapest capable alias across the given
// groups, in group order: groups=["free"] then, after exhaustion/failure,
// ["paid","premium"] implements free-first-then-paid. For each group the
// eligible aliases are those whose PriceGroup is in groups AND whose
// member models all pass runSelectionFilters (posture + quality +
// admission, the same chain the live path uses) for this request's
// FilterCtx. Eligible aliases sort by the frozen key (cost asc, quality
// desc; epsilon-compare on cost). Returns the chosen config, the alias
// name that produced it, and an error. Read-only: no rotation cursor, no
// pin, no block map, no health field is touched.
func (r *Resolver) ResolveSpillway(ctx context.Context, groups []string, fc *FilterCtx) (*ModelConfig, string, error) {
	if r == nil {
		return nil, "", ErrNoEligibleSpillwayGroup
	}
	if fc == nil {
		fc = &FilterCtx{Now: r.clock()}
	} else if fc.Now.IsZero() {
		fc.Now = r.clock()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, group := range groups {
		g := ParsePriceGroup(group)
		if g == PriceGroup("") {
			continue // untagged step: not a spillway tier
		}
		var cands []spillwayCandidate
		for name, entry := range r.aliases {
			if entry == nil || PriceGroup(entry.PriceGroup) != g || len(entry.Models) == 0 {
				continue
			}
			member, ok := spillwayEligible(r, entry, fc)
			if !ok {
				continue
			}
			cands = append(cands, spillwayCandidate{
				name:    name,
				model:   member,
				cost:    member.TotalCost(),
				quality: spillwayQuality(member),
			})
		}
		if len(cands) == 0 {
			continue
		}
		sort.SliceStable(cands, func(i, j int) bool {
			if math.Abs(cands[i].cost-cands[j].cost) >= costEpsilon {
				return cands[i].cost < cands[j].cost
			}
			return cands[i].quality > cands[j].quality
		})
		return cands[0].model, cands[0].name, nil
	}
	return nil, "", ErrNoEligibleSpillwayGroup
}

// spillwayEligible reports whether EVERY member of the alias passes the
// selection chain for this request, returning the alias's cheapest member
// when eligible (the serving pick — spillway steps an alias, not a model).
// Callers must hold at least r.mu (RLock).
func spillwayEligible(r *Resolver, entry *AliasEntry, fc *FilterCtx) (*ModelConfig, bool) {
	var cheapest *ModelConfig
	for _, m := range entry.Models {
		if m == nil {
			return nil, false
		}
		if v, _ := r.runSelectionFilters(&Candidate{
			Model: m,
		}, fc); v != VerdictAllow {
			return nil, false
		}
		if cheapest == nil || m.TotalCost() < cheapest.TotalCost() {
			cheapest = m
		}
	}
	if cheapest == nil {
		return nil, false
	}
	return cheapest, true
}

// spillwayQuality reads the member's measured quality total for the
// frozen sort key's tiebreak (0 when unmeasured — leaderboards nominate,
// probes score; an invented score must never land in a routing decision).
func spillwayQuality(m *ModelConfig) int {
	q := m.Quality
	return q.Chat + q.Code + q.Reasoning + q.Tooling + q.LongContext
}
