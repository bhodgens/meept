# 01 Quality Dimension + Selection Chain - Implementation Leaf

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
- **Scope:** Add the per-model `quality` dimension (Contract C2) and the pluggable pre-dispatch selection chain (Contract C1) that every later filter leaf appends to. Make `ResolveForSkill` and `FindCheapest` quality-aware: sort by `(cost, -quality, latency)` and gate on a floor derived from the classified difficulty.
- **Dependencies:** 00T gate tests committed (RED)
- **Estimated Context:** 70K
- **Concurrency Group:** A (wave 1)
- **Source reference:** Sovereign Router `src/gateway.py:183-200` (`_selection_quality`), `:700-703` (quality floor skip), `:746-759` (sort key); registry `quality` shape from `/tmp/SovereignRouter/src/registry.json`

## Goal

Today `ResolveForSkill` (`internal/llm/resolver.go:324-397`) and `FindCheapest` (`resolver.go:500-513`) sort candidates by `TotalCost()` alone — an unweighted `input_cost + output_cost` sum (`models.go:410-412`). A 3x-cheaper model that produces malformed tool calls therefore wins every capability escalation. This leaf adds the missing quality axis and, critically, the **chain abstraction** that leaves 02, 05, 06, and 08 extend without touching resolver internals again.

Sovereign Router's own rule governs the seed values: **"leaderboards nominate, our own probes score."** No quality score may be invented from vendor documentation. Ship `config/models.json5` with the `quality` key documented but **unset on every model** (Q1 in the master), so the feature is structurally present and behaviorally inert until an operator measures.

## Context

- `internal/llm/models.go:229-300` - `ModelConfig`. Carries `CostPerMillionInput/Output`, `ContextLimit`, `Capabilities map[string]bool`, `ProviderID`, `MaxTokens`, `ToolChoice`, etc. Gains two fields.
- `internal/llm/models.go:410-412` - `TotalCost()` = `CostPerMillionInput + CostPerMillionOutput`. **Do not change this method** - it is used elsewhere and changing it silently reorders existing aliases. Add a new comparator instead.
- `internal/llm/providers.go:73-74` - `ModelDef`, the `models.json5` per-model schema (`type ModelDef struct` opens at :74). This is where new JSON keys are declared.
- `internal/llm/providers.go:736-740` - the single site that maps `ModelDef` -> `ModelConfig` (`CostPerMillionInput: modelDef.InputCost` at :738, `CostPerMillionOutput` at :739). Add the new fields here.
- `internal/llm/resolver.go:324-397` - `ResolveForSkill`: capability match, then `sort.Slice` by `TotalCost()`, take `[0]`, log `capability_escalation`, write a `RoutingDecision`. The sort and the floor go here.
- `internal/llm/resolver.go:500-513` - `FindCheapest`: the other cost-ordered selection. Only non-test caller is `resolver.go:290`.
- `internal/llm/resolver.go:528` - `ResolveForAlias`: the alias rotation path. This leaf does NOT reorder alias rotation - it only calls `runSelectionFilters` after a model is chosen, so later leaves compose. **Do not change alias precedence.**
- `internal/llm/resolver.go:660-661` - the frozen precedence: quota blocks > endpoint cooldowns > alias blocks > stickiness. Do not disturb.
- `internal/llm/errors_tier.go:15-45` - `ErrTierFloor` / `TierFloorError`: the exact pattern for a new terminal sentinel (`Error()`, `NonRetryable() bool`, `Unwrap() []error` when wrapping).
- `internal/llm/routing_log.go:91-105` - `routing_decisions` table. `Reason` is a free-text column; no schema change needed for new reasons.
- `config/models.json5` - the shipped template. The `providers.<id>.models.<name>` blocks are where `quality` will eventually live.

## Verified Anchors

- `internal/llm/models.go:229` - `type ModelConfig struct {`
- `internal/llm/models.go:410-412` - `func (m *ModelConfig) TotalCost() float64`
- `internal/llm/providers.go:73` - `type ModelDef struct {`
- `internal/llm/providers.go:737-740` - `ModelConfig` construction (CostPerMillionInput/Output)
- `internal/llm/resolver.go:324` - `ResolveForSkill`
- `internal/llm/resolver.go:376-383` - `sort.Slice(candidates, ...)` by `TotalCost()`
- `internal/llm/resolver.go:500` - `FindCheapest`
- `internal/llm/resolver.go:528` - `ResolveForAlias`
- `internal/llm/errors_tier.go:15-45` - sentinel + typed error pattern
- `internal/llm/routing_log.go:98` - `Reason` column
- `e2e/suites/sovereign-parity/quality_test.go` - the gate tests (T2, T3, T4)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm/quality.go
type QualityClass string
const (
    QualityChat        QualityClass = "chat"
    QualityCode        QualityClass = "code"
    QualityReasoning   QualityClass = "reasoning"
    QualityTooling     QualityClass = "tooling"
    QualityLongContext QualityClass = "long-context"
)

type Quality struct {
    Chat        int
    Code        int
    Reasoning   int
    Tooling     int
    LongContext int
}

func (q Quality) QualityFor(c QualityClass) int
func QualityFloorFor(difficulty int, capability string) int
func (q Quality) IsZero() bool

// internal/llm/errors_quality.go
var ErrQualityFloor = errors.New("llm: quality floor not met")
type QualityFloorError struct {
    Capability string
    Difficulty int
    Floor      int
    Best       int
    Candidates int
}
func (e *QualityFloorError) Error() string
func (e *QualityFloorError) NonRetryable() bool { return true }

// internal/llm/selection.go  (Contract C1 - created here, extended by 02/05/06/08)
type Candidate struct { Model *ModelConfig; Alias, Capability string; EstInput, EstOutput int }
type FilterCtx struct { SessionID, Capability string; EstInput, EstOutput, Difficulty int; Sensitive bool; Now time.Time }
type Verdict int
const ( VerdictAllow Verdict = iota; VerdictSkip; VerdictFloor )
type Filter interface { Name() string; Filter(*Candidate, *FilterCtx) Verdict; Reason() string }

type qualityFilter struct{}
func (qualityFilter) Name() string { return "quality" }
func (qualityFilter) Filter(c *Candidate, fc *FilterCtx) Verdict
func (qualityFilter) Reason() string

func (r *Resolver) runSelectionFilters(c *Candidate, fc *FilterCtx) (Verdict, string)
func (r *Resolver) registerSelectionFilters()   // called from NewResolver
```

### What This Leaf Consumes

- `ModelConfig` / `ModelDef` - extended in place.
- `Resolver.mu` - `runSelectionFilters` is called with the lock held on the alias path and WITHOUT the lock on the skill path; it must not lock internally and must not touch shared mutable state.

### Frozen contracts (do not paraphrase - these are the integration surface)

`QualityFloorFor` table, verbatim:

```
difficulty <= 0  -> 0      // unclassified: feature inert
difficulty 1..2  -> 1
difficulty 3     -> 2
difficulty 4     -> 3
difficulty 5     -> 4
capability not in the five classes -> 0
```

Sort comparator, verbatim (matches `gateway.py:746-759` shape, adapted to Go):

```
less(i, j) = ci.TotalCost() != cj.TotalCost() ? ci.TotalCost() < cj.TotalCost()
           : quality(j) != quality(i) ? quality(j) > quality(i)      // higher quality first
           : latency(i) < latency(j)                                  // lower latency first
```

`latency` is `ModelConfig` - there is no per-model latency field today. Use `0` for every model in v1 (a stable tiebreak that changes nothing) and leave a comment naming `registry.json latency_p50_ms` as the future source. **Do not invent a latency config key in this leaf.**

Float determinism (repo pitfall 26): compare costs with an epsilon, never `!=` on floats.

```
const costEpsilon = 1e-9
less(i,j): if math.Abs(ci-cj) > costEpsilon { return ci < cj }
           ...quality, then latency
```

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Add `Quality` struct + `QualityClass` constants + `QualityFor` + `IsZero` + `QualityFloorFor` (frozen table) | Create: `internal/llm/quality.go`; Test: gate `quality_test.go` (T2-T4) | `go test -p 2 ./internal/llm/ -run TestQualityFloorFor` | RED (undefined) | GREEN | Given the frozen table; When called with 0..5 and an unknown capability; Then it returns 0/1/1/2/3/4 and 0 |
| T2 | Add `ErrQualityFloor` + `QualityFloorError` with `NonRetryable()`; assert `errors.Is` works through a wrap and the bare pointer matches on exhaustion | Create: `internal/llm/errors_quality.go`; Modify: `internal/llm/quality.go` | `go test -p 2 ./internal/llm/ -run TestQualityFloor_Error` | RED (undefined) | GREEN | Given a floor error; When wrapped once; Then `errors.Is(err, ErrQualityFloor)` and `errors.As` yields `*QualityFloorError` |
| T3 | Extend `ModelDef` with `Quality Quality` (`json:"quality"`), `QualitySource string` (`json:"quality_source"`), `QualityVerifiedAt string` (`json:"quality_verified_at"`) | Modify: `internal/llm/providers.go:73-110` | `go build ./internal/llm/` | RED (compile) | GREEN | Given a json5 block with `"quality": {...}`; When loaded; Then `ModelDef.Quality` carries the values |
| T4 | Extend `ModelConfig` with `Quality Quality` and map it at the construction site | Modify: `internal/llm/models.go:229-300`, `internal/llm/providers.go:737-740` | `go build ./internal/llm/` | RED | GREEN | Given a `ModelDef` with quality; When a `ModelConfig` is built; Then `cfg.Quality` equals it |
| T5 | Create `internal/llm/selection.go`: `Candidate`, `FilterCtx`, `Verdict` constants, `Filter` interface, `qualityFilter` implementation, `Resolver.selectionFilters []Filter`, `registerSelectionFilters`, `runSelectionFilters` | Create: `internal/llm/selection.go`; Modify: `internal/llm/resolver.go:113` (`NewResolver`) | `go test -p 2 ./internal/llm/ -run 'TestSelection_'` | RED (undefined) | GREEN | Given two filters where the first skips; When run; Then it returns `(VerdictSkip, first.Reason())` and never calls the second |
| T6 | Make `ResolveForSkill` quality-aware: apply the floor over the candidate set, then sort with the frozen comparator, then log `capability_escalation` with the quality in `CandidatesJSON` | Modify: `internal/llm/resolver.go:324-397` | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestQuality_CodePrefersHigherScore'` | RED | GREEN | Given cheap/score-1 and dear/score-5; When a code skill resolves; Then dear serves |
| T7 | Floor enforcement in `ResolveForSkill`: when every candidate scores below `QualityFloorFor(difficulty, capability)`, return `*QualityFloorError` (never the best-scoring sub-floor model) | Modify: `internal/llm/resolver.go:324-397` | `-run TestQuality_FloorBlocks` | RED | GREEN | Given all candidates below floor; When resolve; Then `errors.Is(err, ErrQualityFloor)` and `err` is `*QualityFloorError` |
| T8 | Inertness when unseeded: `Quality.IsZero()` for every candidate means `QualityFloorFor` is never consulted and the comparator's quality term is skipped (pure cost order = today) | Modify: `internal/llm/resolver.go:376-383` | `-run TestQuality_AbsentIsInert` | RED | GREEN | Given no quality blocks; When resolve; Then selection is byte-identical to the pre-change cost order |
| T9 | Make `FindCheapest` use the same comparator | Modify: `internal/llm/resolver.go:500-513` | `go test -p 2 ./internal/llm/ -run TestFindCheapest` | RED | GREEN | Given equal-cost candidates with different quality; When FindCheapest; Then the higher-quality one wins |
| T10 | Wire `runSelectionFilters` into `ResolveForAlias` AFTER the model is chosen and BEFORE return, so leaves 02/05/06/08 compose without re-editing the rotation | Modify: `internal/llm/resolver.go:528-700` (one call site, after the rotation block resolves `mc`) | `go build ./internal/llm/` + `go test -p 2 ./internal/llm/ -run TestResolveForAlias` | n/a (inert until filters exist) | GREEN | Given no filters registered beyond quality; When a normal alias resolves; Then behavior is unchanged (quality floor only binds when difficulty > 0) |
| T11 | Document the new keys in `config/models.json5` - a commented block showing the shape, with NO model actually setting it, and a note citing Q1 + "probes score, not leaderboards" | Modify: `config/models.json5` (comment block near the provider models) | `./bin/meept config get llm` or config-drift-check | n/a (comment) | docs present, no model seeded | Given the template; When read; Then the shape is documented and inert |
| T12 | Verify the flag-off control: with no quality configured, existing `internal/llm` tests pass unchanged | - | `go test -p 2 ./internal/llm/ -count=1` | n/a | GREEN, zero modified assertions | Given the tree; When the full package suite runs; Then no existing test needed an edit |

## Self-Verification Checklist

- [ ] T1-T12 complete; gate tests T2-T4 GREEN.
- [ ] `TotalCost()` is UNCHANGED. No existing caller's ordering shifts.
- [ ] `ResolveForSkill` and `FindCheapest` share ONE comparator (no duplicated sort logic).
- [ ] `runSelectionFilters` takes no lock and touches no shared mutable state.
- [ ] `registerSelectionFilters` is called from `NewResolver`; the registration order comment names the order contract.
- [ ] Float comparison uses `costEpsilon`; no bare `!=` on float64 anywhere in the new code.
- [ ] `QualityFloorError` implements `NonRetryable()` and is reachable via `errors.Is` through one wrap.
- [ ] `config/models.json5` documents `quality` but seeds NO model (Q1).
- [ ] No new bus topic, no new WS type, no `internal/config` import.
- [ ] `go build ./...` and `go test -p 2 ./internal/llm/` green.
- [ ] No `_ = f()`, no bare `panic`, no ignored error.

## Review Checklist

- [ ] Frozen `QualityFloorFor` table matches master.md C2 digit for digit.
- [ ] Sort comparator order is `(cost, -quality, latency)` and matches `gateway.py:746-759` semantics.
- [ ] Alias rotation, cooldowns, endpoint blocks, quota blocks, and sticky pins are untouched (`git diff` shows no change to those functions' logic).
- [ ] `ResolveForAlias` gained exactly one call, positioned after model selection.
- [ ] Quality values come from measurement; the leaf ships no invented scores.
- [ ] Comment style names the WHY (the Sovereign Router selection rule), not the mechanism.
- [ ] `docs/workflows/llm-routing-quality.md` exists and maps `internal/llm/quality.go` + `selection.go`.
- [ ] No secrets or prompt text in any log line added.

## Do NOT commit

The orchestrator commits this leaf after review.