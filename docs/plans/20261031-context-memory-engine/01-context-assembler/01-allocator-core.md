# Allocator Core: Window-Fraction Context Splitter - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** New `internal/agent/contextalloc` package: deterministic
  window-fraction allocator producing the C2 Allocation split.
- **Dependencies:** 00-research-budgeting.md (cite findings-context-budgeting.md)
- **Estimated Context:** 80K
- **Concurrency Group:** B
- **Audit references:** R1, R2

## Goal

Create the allocator package: `New(Config) *Allocator` and
`Allocate(ctx, windowTokens, Request) Allocation`. Fractions replace static
constants when a resolved window exists; the statics
(internal/agent/loop.go:4169-4184, internal/agent/conversation.go:22,33)
stay untouched as fallbacks. The allocator is PURE and deterministic:
identical inputs -> identical Allocation (bit-identical integers; no
wall-clock, no map-order float accumulation). It defines the shared C1
types (tier.go) that 03-integration wires and branch B feeds.

## Context

- internal/agent/loop.go:4169-4193 - IterationTokenBudget=30000,
  ToolResultMaxTokens=3000, DefaultConversationTokenBudget=50000,
  warn 0.80, conversationTokenBudget() - the statics this package
  supersedes when discovery resolves a window; DO NOT delete them.
- internal/llm/context_discovery.go - resolves the real context_limit per
  model; the windowTokens input comes from there at wiring time (leaf 03).
- internal/llm/context_firewall.go:548-584 - DerivedIterationBudget /
  DerivedConversationBudget (ratio * context_limit, 0.7 small-model
  factor) - prior art; the allocator generalizes this to all sections.
- internal/llm/tokenizer.go - llm.NewTokenizerForModel +
  HeuristicTokenizer (3 chars/token) for any token estimation.
- findings-context-budgeting.md (sibling research leaf) - justify fraction
  defaults with R-Ax citations in code comments.

## Verified Anchors

- internal/agent/loop.go:4169-4184 - budget statics - fallbacks preserved
- internal/agent/loop.go:4476 - `effectiveBudget := max(IterationTokenBudget-toolOverhead, ...)` - current per-iteration budget math
- internal/llm/context_firewall.go:548-584 - Derived budgets + ContextUtilization - ratio prior art
- internal/llm/tokenizer.go:51-63 - HeuristicTokenizer - fallback counting
- internal/tools/interface.go:187-194 - ResultSizer floors - per-tool floors the Tools section must respect at wiring time

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/agent/contextalloc/allocator.go  (contract C3)
func New(cfg Config) *Allocator
func (a *Allocator) Allocate(ctx context.Context, windowTokens int, req Request) Allocation
type Request struct { Query string; ToolCount int; DocBytes int; HistoryTokens int }
type Config struct {
    OutputReserveFraction float64
    Fractions             SectionFractions
    MinSections           MinSections
}
// internal/agent/contextalloc/tier.go  (contract C1)
type TierName string // "working" | "episodic" | "semantic"
type RecallFunc func(ctx context.Context, query string, limit int) []string
type TierBudget struct { Tier TierName; Ceiling int; Recall RecallFunc }
type TierBudgetProvider interface {
    TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget
}
```

Package rules: package `contextalloc`; imports allowed: stdlib,
internal/llm, internal/tools, pkg/*; MUST NOT import internal/agent
(cycle). New(Config) validates: fractions sum to 1.0 within 1e-9, every
fraction in [0,1], MinSections floors are non-negative and not exceeding
their section's maximum plausible value; invalid config returns an error
(New returns (*Allocator, error)).

### What This Leaf Consumes

- C2 Allocation (../../master.md) - you define it here per C2 spec
- findings-context-budgeting.md - R-Ax citations for default fractions

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Package skeleton + C1 types in tier.go with doc comments | Create: internal/agent/contextalloc/tier.go, internal/agent/contextalloc/doc.go (package doc) | `go build ./internal/agent/contextalloc/` | package does not exist | builds; TierName constants exact | Given no package; When created with C1 types; Then `go build` green and constants are "working"/"episodic"/"semantic" |
| T2 | Config validation | Modify: internal/agent/contextalloc/allocator.go; Test: internal/agent/contextalloc/allocator_test.go | `go test -p 2 ./internal/agent/contextalloc/ -run TestNew` | New undefined | rejects fraction-sum != 1.0 (epsilon 1e-9), negative fractions, negative floors; accepts valid | Given Config{Fractions sum 0.9}; When New; Then error mentions fractions |
| T3 | Core Allocate: integer split, largest-remainder so sections sum EXACTLY to usable tokens (window - output reserve); deterministic | Modify: allocator.go; Test: allocator_test.go | `go test -p 2 ./internal/agent/contextalloc/ -run TestAllocateSums` | Allocate undefined | 1000 random-ish windows (fixed seed list in table) each sum exactly; identical inputs -> identical output | Given window=100000, fractions S=.30/D=.10/M=.20/T=.15/H=.25, reserve .15; When Allocate; Then all seven Allocation ints sum to 85000 and repeat calls are equal |
| T4 | Floors and caps: MinSections floors applied BEFORE proportional split; if floors exceed usable tokens, shrink proportionally and never return negative sections | Modify: allocator.go; Test: allocator_test.go | `go test -p 2 ./internal/agent/contextalloc/ -run TestAllocateFloors` | no floor logic | floors honored when they fit; graceful degradation when they do not | Given floor History=50000 with window usable 40000; When Allocate; Then no negative ints and History <= usable |
| T5 | Memory subdivision across tiers: with a TierBudgetProvider present, Memory splits across returned tiers honoring Ceiling hints (floored at a documented minimum) and equal-split otherwise; without provider, Memory stays undivided (MemoryWorking=Memory, others 0) | Modify: allocator.go, tier.go; Test: tier_test.go | `go test -p 2 ./internal/agent/contextalloc/ -run TestTierSplit` | no tier logic | ceiling hints respected; nil provider = undivided; typed-nil provider (e.g. (*myProv)(nil)) treated as absent | Given provider returns 2 tiers, ceilings 6000/0; When Allocate; Then MemoryWorking+MemoryEpisodic+MemorySemantic == Memory, first >= 6000 floored rule applied |
| T6 | Iteration/Conversation derivation: Allocation.IterationBudget = min(History fraction share, prior art ratio rules); ConversationBudget = window * conversation fraction; document relationship to internal/llm/context_firewall.go:548-584 in comments | Modify: allocator.go; Test: allocator_test.go | `go test -p 2 ./internal/agent/contextalloc/ -run TestDerivedBudgets` | fields always 0 | nonzero, monotonic in windowTokens | Given window 100000; When Allocate; Then IterationBudget>0 and increases with window 200000 |

Given/When/Then rows above are the acceptance criteria; every test is
RED-first (undefined symbols / missing behavior) before implementation.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2 ./internal/agent/contextalloc/...`)
- [ ] Contracts C1/C2/C3 satisfied exactly (names, signatures, paths)
- [ ] Package does not import internal/agent
- [ ] Allocate deterministic (no map-order floats; sort keys before accumulation)
- [ ] Statics in internal/agent untouched
- [ ] ASCII only; gofmt clean
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines of go test output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Contracts C1/C2/C3 match exactly
- [ ] Determinism: fixed-input tests, no wall-clock/rand (predid-clean)
- [ ] No import of internal/agent from contextalloc
- [ ] Conventions followed; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

Cite findings-context-budgeting.md R-Ax IDs in comments justifying default
fraction values. Keep the package pure: no logging, no bus, no config
reads. The 0.7 small-model factor (context_firewall.go:556) is prior art -
do not silently copy it; note in comments whether/why the allocator
generalizes or omits it (findings citation expected).
