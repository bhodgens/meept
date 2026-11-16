# 09 Price Groups + Paid Spillway - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST and are
> committed - your job is to make them pass; do NOT delete, skip, weaken,
> or rewrite any gate test (report suspected-wrong gate tests to the
> orchestrator instead). Do NOT commit - the orchestrator handles all git
> operations after review. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. After writing a file, do NOT
> read it back - write once and stop.

## Meta

- **Parent:** master.md
- **Scope:** Implement the alias price-group tier (Contract C10): `ModelAliasEntry.PriceGroup` (field declared in leaf 01) + `ResolveSpillway`, a caller-selected free-first / paid-on-exhaustion resolution that mirrors Sovereign Router's `build_spillway_candidates`.
- **Dependencies:** 00T gate tests committed (RED); 01-quality-dimension.md COMPLETE (declares `PriceGroup` on `ModelAliasEntry` and provides the `Filter` chain); 07-sticky-difficulty.md COMPLETE (this leaf reads `resolver.go` but must not edit it)
- **Estimated Context:** 45K
- **Concurrency Group:** A5 (wave 5 - reads the A-chain output, writes only `internal/llm/pricegroup.go`)
- **Source reference:** Sovereign Router `src/gateway.py:765-800` `build_spillway_candidates` and `:746-759` the sort key. Registry `lane` values `paid` / `premium`.

## Goal

Sovereign Router's single highest-value routing rule is "free first, paid only on exhaustion" — the **paid spillway** (`ROUTING-PRINCIPLES.md` rule 2, `docs/incidents/2026-11-09-rate-limit-cascade.md` is the incident that motivated the lane discipline). meept has no way to express that a group of aliases is a *cheap tier* and another a *fallback tier*.

This leaf adds two things:

1. A **price-tier label on aliases**: `price_group: "free" | "paid" | "premium"`. This is a routing concern, so it belongs on `ModelAliasEntry` (`providers.go:174`), not on individual models. Untagged (default `""`) means legacy: the alias resolves only when explicitly selected and never participates in a spillway step.
2. A **spillway selector**: `ResolveSpillway(groups, fc)` walks the groups in order — e.g. `["free"]`, then on no-eligible/attempt-failure `["paid","premium"]` — selecting the cheapest capable alias among the eligible group, applying the same posture/quality/admission filters as the live path.

This is **opt-in at the call site**: default callers that resolve a single alias are byte-identical. Only a caller that explicitly invokes the spillway with a group list gets tier stepping.

### What this is NOT

This is NOT the Sovereign Router multi-tenant surface. There are no per-identity clients, no per-client keys, no per-client lanes, no per-client daily targets, no premium gate. meept's identity unit is the session; `internal/auth/quota.go:10-23` is an undecided no-op. This leaf adds only the portable mechanism — tier ordering. The distinction is pinned in the master's Non-goals.

## Context

- `internal/llm/providers.go:174-182` - `ModelAliasEntry`. Leaf 01 adds `PriceGroup string json:"price_group,omitempty"`. This leaf reads it but does NOT edit providers.go.
- `internal/llm/resolver.go:113-200` - `NewResolver` builds `r.aliases` from `cfg.ModelAliases`. This leaf adds a lookup over that map.
- `internal/llm/selection.go` - from leaf 01: `Filter`, `Candidate`, `FilterCtx`, `runSelectionFilters`. Spillway candidates pass the same filters.
- `internal/llm/resolver.go:500` - `FindCheapest`: cost-ascending selection this leaf mirrors for tier stepping.
- Sovereign Router `gateway.py:746-759` sort key — `(estimate_cost, -selection_quality, latency_p50)` — the ordering this leaf replicates for the cheapest-capable step.

## Verified Anchors

- `internal/llm/providers.go:174` - `ModelAliasEntry` (leaf 01 adds `PriceGroup`)
- `internal/llm/resolver.go:113` - `NewResolver`, where `r.aliases` is populated
- `internal/llm/resolver.go:500` - `FindCheapest` (cost-ascending pattern)
- `internal/llm/selection.go` - `Filter` / `FilterCtx` / `Verdict` from leaf 01
- `internal/llm/errors_quota.go` - the `ErrAll*Blocked` sentinel family this leaf's `ErrNoEligibleSpillwayGroup` joins
- `e2e/suites/sovereign-parity/` - pricegroup_test.go (T24, T25)

## Interface Contracts (From Parent)

### What This Leaf Exposes (C10)

```
// internal/llm/pricegroup.go
type PriceGroup string
const (
    PriceGroupFree    PriceGroup = "free"
    PriceGroupPaid    PriceGroup = "paid"
    PriceGroupPremium PriceGroup = "premium"
)

// ParsePriceGroup maps a config string; "" and unknown -> PriceGroup("")
// (untagged = legacy; this alias never participates in a spillway step).
func ParsePriceGroup(s string) PriceGroup

// AliasesByPriceGroup returns the names of aliases whose PriceGroup == g,
// in config (alias-map) iteration order. Used by ResolveSpillway.
func (r *Resolver) AliasesByPriceGroup(g PriceGroup) []string

// ResolveSpillway selects the cheapest capable alias across the given
// groups, in group order. groups=["free"] then ["paid","premium"] (caller
// re-invokes after exhaustion/failure) implements free-first-then-paid.
//   candidate = an alias whose PriceGroup is in groups AND whose member
//               models all pass runSelectionFilters(VerdictAllow)
//   order    = (TotalCost asc, quality desc) over the eligible aliases
// Returns (*ModelConfig, aliasName, error). ErrNoEligibleSpillwayGroup when
// a group has zero eligible aliases.
func (r *Resolver) ResolveSpillway(ctx context.Context, groups []string, fc *FilterCtx) (*ModelConfig, string, error)
```

Freeze (C10): spillway NEVER reorders alias rotation, NEVER touches a sticky pin, NEVER mutates `AliasHealth`. It reads `r.aliases` under `RLock` and returns. An untagged alias is invisible to every spillway step. Selecting a single explicit alias is untouched.

### What This Leaf Consumes

- `r.aliases` (the resolved `AliasEntry` map) — read-only.
- `runSelectionFilters` / `Filter` / `FilterCtx` from leaf 01.
- A cost/quality comparator. Reuse the comparator leaf 01 added to `ResolveForSkill`/`FindCheapest` if it is exported; otherwise replicate it in this leaf (do not change the shared one).

### Frozen sort key (replicating `gateway.py:746-759`)

```
less(i,j) = cost_i != cost_j ? cost_i < cost_j
          : quality_i != quality_j ? quality_i > quality_j
          : 0  // stable; alias-map iteration order breaks the tie
cost = aliases' member model with the min TotalCost() over each spillway step
```

Float determinism: epsilon compare (`costEpsilon = 1e-9`), never bare `!=` — repo pitfall 26.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `PriceGroup`, the constants, `ParsePriceGroup` (`""`/unknown -> untagged) | Create: `internal/llm/pricegroup.go` | `go test -p 2 ./internal/llm/ -run TestParsePriceGroup` | RED (undefined) | GREEN | Given "", "free", "paid", "premium", "garbage"; When parsed; Then untagged, Free, Paid, Premium, untagged |
| T2 | Implement `AliasesByPriceGroup(g)` over `r.aliases` | Modify: `internal/llm/pricegroup.go` | `go test -p 2 ./internal/llm/ -run TestAliasesByPriceGroup` | RED | GREEN | Given aliases tagged free/paid/none; When asked; Then the free set, in alias-map order |
| T3 | Implement `ResolveSpillway` — filter each candidate alias, sort by the frozen key, return the cheapest eligible | Modify: `internal/llm/pricegroup.go` | `go test -p 2 ./internal/llm/ -run 'TestResolveSpillway_'` | RED | GREEN | Given free A (cheap) and free B (dear); When spillway(["free"]); Then A wins |
| T4 | Tier stepping: `groups=["free"]` yields free; re-invoke `groups=["paid"]` after exhaustion yields the paid fallback | Modify: `internal/llm/pricegroup.go` (verify, not a second function) | `go test -p 2 ./internal/llm/ -run TestResolveSpillway_TierStep` | RED | GREEN | Given free dead and paid healthy; When [paid]; Then paid serves |
| T5 | Filters apply: an alias whose member fails posture/quality/admission is invisible to every spillway step | Modify: `internal/llm/pricegroup.go` | `-run TestResolveSpillway_HonorsFilters'` | RED | GREEN | Given an over-context or non-private-eligible free alias; When spillway(["free"]); Then it is skipped |
| T6 | No mutation: spillway leaves `AliasHealth`, `CurrentIndex`, sticky pins, and `endpointBlocks` untouched | Modify: `internal/llm/pricegroup.go` (verify) | `-run TestResolveSpillway_NoMutation` | RED | GREEN | Given a mid-rotation alias pool; When spillway; Then the health snapshots match |
| T7 | `ResolveSpillway` is opt-in: default single-alias resolution is byte-identical (flag-off gate R9) | - | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestPriceGroup_'` | RED | GREEN | Given untagged aliases; When a normal turn resolves; Then the decision equals `main` |
| T8 | Wire the spillway as a **public** resolver method (no internal call site required for v1) — it is an API future consumers (a daemon chat option or CLI) call. Document it as available-but-uncalled | Modify: `internal/llm/pricegroup.go` | `go build ./...` | n/a | compiles; exported; documented | Given `ResolveSpillway`; When built; Then it is reachable from outside `internal/llm` |
| T9 | Sanitize the alias-map iteration order for determinism: sort alias names before `AliasesByPriceGroup` returns them | Modify: `internal/llm/pricegroup.go` | `go test -p 2 ./internal/llm/ -run TestAliasesByPriceGroup_Deterministic` | RED | GREEN | Given an unsorted map; When called; Then the names are stable across runs |

## Self-Verification Checklist

- [ ] T1-T9 complete; gate tests T24, T25 GREEN.
- [ ] Spillway reads under `RLock`, never `Lock`; no health/pin/rotation mutation.
- [ ] Untagged aliases are invisible to every spillway step.
- [ ] Default single-alias resolution is byte-identical (flag-off R9).
- [ ] `ErrNoEligibleSpillwayGroup` joins the `ErrAll*Blocked` sentinel family, checked with `errors.Is`.
- [ ] Float sort uses `costEpsilon`; deterministic tie-break.
- [ ] No new bus topic, no config surface beyond the alias `price_group` field, no `internal/config` import.
- [ ] No per-identity/multi-tenant concept introduced (this is tier ordering, not gatekeeping).
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] C10 freeze holds: no reorder, no pin touch, no `AliasHealth` mutation.
- [ ] Sort key matches `gateway.py:746-759` semantics.
- [ ] `ResolveSpillway` is exported but has no mandatory internal call site for v1; the doc comment says so (available-but-uncalled is an explicit API surface decision).
- [ ] Untagged = legacy, proven by the flag-off e2e.
- [ ] The reviewer confirms this is NOT a multi-tenant gate — no per-identity policy was added.
- [ ] `docs/workflows/llm-price-groups.md` exists and maps `pricegroup.go` + the alias `price_group` key.

## Do NOT commit

The orchestrator commits this leaf after review.