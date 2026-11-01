# Context Assembler: Unified Per-Request Token Budgeting - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch node.
> Your job: (1) dispatch leaf agents, (2) review their work, (3) re-dispatch
> if incomplete, (4) track completion. Do NOT implement code yourself. All
> implementation happens in leaf agents. Full operational detail: skill
> `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 4 leaves (00-research-budgeting, 01-allocator-core,
  02-doc-budgets, 03-integration)
- **Scope:** One allocator that splits the resolved context window across
  memory, tools, docs, and history as window-fractions per request,
  closing the unbounded-CLAUDE.md hole and preserving the stable prefix.

## Goal

Replace the scattered static token budgets with a single per-request
allocator in a new `internal/agent/contextalloc` package. The allocator
takes the RESOLVED window (ContextDiscovery's real context_limit,
internal/llm/context_discovery.go; fallback to the existing statics when no
model is discovered), subtracts an output reserve, and distributes the rest
across: stable system sections, dynamic doc sections, per-tier memory
(via the C1 seam; nil = today's behavior), tool overhead + per-result
caps, and conversation history. It closes the CLAUDE.md hole
(artifact_integration.go:293-296 injects RawContent verbatim, unbounded;
README has only a 4KB char cap at :330) by budgeting doc sections from the
allocation. Statics (loop.go:4169-4184, conversation.go:22,33) remain as
fallbacks so behavior without discovery is unchanged.

HARD INVARIANT: AssembleOrdered (internal/agent/prompt.go:461-525) and its
stablePrefixHash (sha256 at :468,496) classify sections stable/unstable
byte-exactly. Every consumer of the new budgets MUST preserve the
stable-first ordering and never move a stable section after a dynamic one
within a session, or provider KV caches miss from token 0.

## Architecture

`contextalloc.Allocator` is pure: `Allocate(windowTokens, opts) Allocation`
plus `Apply` hooks that existing call sites consult. Wiring order per
request: loop resolves model -> ContextDiscovery gave ContextLimit ->
allocator splits -> budgets flow to (a) memory injection
(EnforceMemoryItemBudget, loop.go:6884/7230, replacing the flat
MaxMemoryContextTokens=2000 when the seam is live), (b) doc section
builder (BuildFullArtifactContext), (c) iteration/conversation budgets
(loopt loop.go:4176-4193, firewall DerivedIterationBudget/
DerivedConversationBudget internal/llm/context_firewall.go:548-584 keep
their fallback roles). The integration leaf (03) is the only leaf that
touches loop.go wiring and the seam.

## Interface Contracts

### C1: TierBudget + TierBudgetProvider (SHARED -- full definition in ../../master.md)

```
// File: internal/agent/contextalloc/tier.go
type TierName string // TierWorking | TierEpisodic | TierSemantic
type RecallFunc func(ctx context.Context, query string, limit int) []string
type TierBudget struct { Tier TierName; Ceiling int; Recall RecallFunc }
type TierBudgetProvider interface {
    TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget
}
// Consumed by: 03-integration.md. Seam: nil (incl. typed-nil) = today.
```

### C2: Allocation struct (branch A internal -- full definition in ../../master.md)

```
// File: internal/agent/contextalloc/allocator.go
type Allocation struct {
    WindowTokens, OutputReserve int
    SystemStable, SystemDynamicDocs int
    MemoryWorking, MemoryEpisodic, MemorySemantic int
    Tools, History int
    IterationBudget, ConversationBudget int
}
```

### C3: Allocator public surface (owner: 01-allocator-core)

```
func New(cfg Config) *Allocator
func (a *Allocator) Allocate(ctx context.Context, windowTokens int, req Request) Allocation
type Request struct { Query string; ToolCount int; DocBytes int; HistoryTokens int }
type Config struct {
    OutputReserveFraction float64 // e.g. 0.15
    Fractions             SectionFractions // must sum to 1.0 +- epsilon
    MinSections           MinSections      // hard floors; validated
}
// SectionFractions fields: SystemStable, SystemDynamicDocs, Memory, Tools,
// History. Memory is subdivided across tiers present in the provider
// result (equal-split default; provider Ceiling hints honored, floored).
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00 | 00-research-budgeting.md | leaf | none | 30K | A |
| 01 | 01-allocator-core.md | leaf | 00 | 80K | B |
| 02 | 02-doc-budgets.md | leaf | 01 | 75K | C |
| 03 | 03-integration.md | leaf | 01, 02 | 90K | D |

03-integration ALSO requires branch B leaves 01-03 COMPLETE (it consumes
their stores through the seam), but must COMPILE without them via the nil
seam. Dispatch D only after both branches' implementation leaves land.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

In-leaf TDD gate (tree charter -- new feature tests in hermetic e2e tier +
in-package unit tests; leaf task tables are RED-first). The `## Test Gate`
matrix below is validated in-session at REVIEW time; on delta the LESSER
artifact updates to the GREATER.

### Phase 1: Dispatch Concurrency Group A

Dispatch 00-research-budgeting.md. It pins
`findings-context-budgeting.md` in this directory; leaves 01-03 cite it.

### Phase 2: Groups B, C, D

Dispatch 01 (group B); then 02 (group C); 03 (group D) last, after branch
B's 01-03 are REVIEWED. delegate_task shape: goal = one-line imperative;
context = full leaf text + contracts C1-C3 + conventions (from
../../master.md) + relevant source INLINED + "Do NOT commit. Do NOT run
git add."

### Phase 3: Review and Commit Each Child

Review in-session (main model -- never a delegated reviewer): leaf tasks,
contracts, review checklist, verify commands green. Commit the leaf's
exact files; status -> REVIEWED. Max 3 re-dispatch cycles, then escalate
to ../../master.md.

### Phase 4: Integration Review

After 03 REVIEWED: run the Integration Test Plan below; confirm branch B
dependency handling (compiles + tests green with the seam nil); confirm no
stable-prefix drift vs the pre-change baseline test. Report COMPLETE to
../../master.md.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Contracts C1-C3 satisfied exactly (signatures, types, paths)
- [ ] Stable/unstable section classification preserved byte-exactly;
      stablePrefixHash tests green
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD, -p 2)
- [ ] Wiki/trace stores absent from any prompt-builder path
- [ ] No scope creep; no debug artifacts; no line-number corruption
- [ ] ASCII only

Output: APPROVED or specific gaps.

## Coding Conventions

See ../../master.md `## Coding Conventions` (binding; passed verbatim in
every dispatch context). Branch-specific additions:

- contextalloc is a NEW package: no import of internal/agent (avoid
  cycles); it may import internal/llm and pkg-level utilities only.
- Pure functions first: Allocate MUST be deterministic for identical
  inputs (no wall-clock, no map-iteration-order-dependent math; sort keys
  before float accumulation).
- Existing statics stay untouched as fallbacks; do not delete
  IterationTokenBudget/ToolResultMaxTokens/DefaultConversationTokenBudget/
  MaxMemoryContextTokens.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R1 | unified per-request token budgeting | 01 allocator-core | contextalloc unit tests |
| R2 | window-fractions not static constants | 01 T3-T5 | unit tests |
| R3 | close unbounded-CLAUDE.md hole | 02 doc-budgets | doc budget tests |
| R4 | stable-prefix hash must not drift | 02 invariant tasks | stable-hash tests |
| R8 | per-tier budgets into allocator | 03 integration | seam tests |
| R9 | nil seam = today's behavior | 03 integration | typed-nil test |
| R11 | pinned research findings | 00 research | findings doc cited by 01-03 |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00-research-budgeting | PENDING | 0 | |
| 01-allocator-core | PENDING | 0 | |
| 02-doc-budgets | PENDING | 0 | |
| 03-integration | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

```
go build ./...
go vet ./internal/agent/contextalloc/...
go test -p 2 ./internal/agent/contextalloc/... ./internal/agent/ -run 'Alloc|Budget|StablePrefix|ClaudeMD|DocBudget'
go test -p 2 -race ./internal/agent/contextalloc/...
```

Cross-boundary: allocation sums to window; CLAUDE.md 1MB fixture truncates
to the doc budget with a truncation notice; stable-prefix hash identical
before/after a dynamic doc change; seam nil reproduces
MaxMemoryContextTokens=2000 behavior bit-for-bit in a golden test.

## Test Gate

In-leaf TDD gate; matrix validated at REVIEW time in-session.

| # | Feature (from plan) | Source (leaf/spec) | Unit tests | E2E tests | Status |
|---|---------------------|--------------------|-----------|-----------|--------|
| F1 | window-fraction allocation, deterministic | 01 T3-T5 | contextalloc tests | context-memory-engine | PENDING |
| F2 | doc budgets bound CLAUDE.md/README | 02 | artifact budget tests | context-memory-engine | PENDING |
| F3 | stable prefix hash preservation | 02 | prompt ordered tests | context-memory-engine | PENDING |
| F7 | tier budgets into allocator, nil = today | 03 | seam + typed-nil tests | context-memory-engine | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (empty at authoring)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Verified anchors (grep-confirmed 2026-10-31):
  internal/agent/conversation.go:22 DefaultMaxMessages=200, :33
  MaxMemoryContextTokens=2000; internal/agent/loop.go:4169
  IterationTokenBudget=30000, :4173 ToolResultMaxTokens=3000, :4178
  DefaultConversationTokenBudget=50000, :4184 warning ratio 0.80, :6884
  EnforceMemoryItemBudget wiring, :7230 definition; internal/agent/
  prompt.go:461-525 AssembleOrdered + sha256; internal/agent/
  artifact_integration.go:293-296 CLAUDE.md verbatim, :315-334 README 4KB;
  internal/llm/context_firewall.go:548-584 Derived budgets +
  ContextUtilization; internal/llm/context_discovery.go real context_limit;
  internal/llm/tokenizer.go llm.NewTokenizerForModel +
  HeuristicTokenizer (3 chars/token); internal/tools/interface.go:187-194
  ResultSizer floors; internal/agent/loop.go:5362 WrapToolOutput LIVE.
