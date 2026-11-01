# Context Memory Engine - Implementation Orchestrator (Root)

> **For the executing agent:** You are the orchestrator for this tree.
> Your job: (1) dispatch branch orchestrators / leaf agents, (2) review their
> work, (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 2 branches (01-context-assembler, 02-memory-tiers) + 1 root leaf (04-e2e)
- **Scope:** Close two approved gaps: a unified per-request token-budgeting
  context assembler, and a 3-tier memory engine (working/episodic/semantic)
  with automated eviction + compression.

## Goal

Today the agent's context budgeting is scattered across static constants
(`IterationTokenBudget=30000`, `ToolResultMaxTokens=3000`,
`DefaultConversationTokenBudget=50000`, `MaxMemoryContextTokens=2000`) with
per-call-site enforcement, and CLAUDE.md is injected verbatim and UNBOUNDED
(internal/agent/artifact_integration.go:293-296). Branch A replaces the
scattered statics with ONE allocator that splits the RESOLVED context window
(from ContextDiscovery) across memory + tools + docs + history as
window-fractions, closes the CLAUDE.md hole, and preserves the
AssembleOrdered stable/unstable classification byte-exactly (a stable-prefix
hash drift makes every provider KV cache miss from token 0).

Today memory has episodic storage, async L1/L2 distillation, and usefulness
eviction, but no true working tier (the conversation buffer is not integrated
with memory), no automated tier-wide eviction/compression policy, and no
semantic-to-episodic demotion path. Branch B builds the working tier, the
policy machinery, and the demotion path -- all OPT-IN (default false) per
docs/agents/opt-in-defaults.md, with the default-on flip documented as a
product decision.

The branches meet at ONE shared contract: the tier-budget interface
(Contract C1) that branch B's stores feed and branch A's allocator consumes
through an optional seam (nil = today's behavior).

## Architecture

```
                       resolved window (ContextDiscovery context_limit)
                                        |
                       +----------------v----------------+
                       | contextalloc.Allocator (branch A)|  one split per request:
                       | window-fractions, floors, caps   |  system-stable / docs /
                       +---+----------+----------+--------+  memory(per-tier) / tools /
                           |          |          |             history / output reserve
                    doc budget   tier budgets  tool+history budgets
                           |          |
              artifact_integration  TierBudgetProvider seam (C1, nil = today)
                           |          |
                     CLAUDE.md     internal/memory working/episodic/semantic
                     bounded       stores + tierpolicy eviction/compression +
                                   demotion (branch B, opt-in)
```

Branches are otherwise independent: branch A compiles without branch B
(seam nil), branch B never imports branch A's loop wiring.

## Interface Contracts

### C1: TierBudget + TierBudgetProvider (SHARED, pinned here and in BOTH branch orchestrators)

```
// File: internal/agent/contextalloc/tier.go
// Owner: 01-context-assembler/01-allocator-core.md
// Provided by: 02-memory-tiers stores (adapter authored in
//              01-context-assembler/03-integration.md)
package contextalloc

// TierName identifies a memory tier.
type TierName string

const (
    TierWorking  TierName = "working"
    TierEpisodic TierName = "episodic"
    TierSemantic TierName = "semantic"
)

// RecallFunc returns candidate memory strings for query, at most limit
// items. Implementations MUST NOT synchronously run consolidation and MUST
// be safe for concurrent use.
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
// allocator's memory split. Slice order is insignificant.
type TierBudgetProvider interface {
    TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget
}
```

- Owner leaf: 01-context-assembler/01-allocator-core.md (defines the types)
- Producer: branch B store leaves feed an adapter; the adapter that maps
  branch B stores onto TierBudgetProvider is authored in
  01-context-assembler/03-integration.md (so branch B never imports branch A
  wiring and stays compilable on its own).
- Seam rule: `SetTierBudgetProvider(nil)` (including a TYPED-nil interface
  value) MUST restore today's behavior exactly. Typed-nil guards are
  mandatory per internal/daemon/AGENTS.md.

### C2: Allocation struct (branch A internal; cited by 04-e2e)

```
// File: internal/agent/contextalloc/allocator.go
type Allocation struct {
    WindowTokens       int // resolved context limit minus output reserve
    OutputReserve      int
    SystemStable       int // stable prompt sections (prefix cache)
    SystemDynamicDocs  int // CLAUDE.md / README / skills / agents section
    MemoryWorking      int
    MemoryEpisodic     int
    MemorySemantic     int
    Tools              int // tool-declaration overhead + per-result cap
    History            int
    IterationBudget    int
    ConversationBudget int
}
```

### C3: e2e suite mapping (root leaf)

```
e2e/suites/context-memory-engine/context_memory_engine_test.go  (build tag: e2e)
e2e/manifest.json: path_map "internal/agent/contextalloc/" -> ["context-memory-engine"],
                   path_map "internal/memory/working/"   -> ["context-memory-engine"],
                   path_map "internal/memory/tierpolicy/"-> ["context-memory-engine"],
                   suites[] entry "context-memory-engine" (status implemented, hermetic runner)
```

## Invariants (binding on every leaf)

- Wiki/trace stores NEVER reach inference-path prompt builders.
- Consolidation stays OFF the chat path; Consolidator remains single-start
  (internal/memory/consolidation.go:60,96).
- Typed-nil guards on all seam Set* wiring.
- Opt-in defaults rule (docs/agents/opt-in-defaults.md): every new
  `memory.working_*` / `memory.tiers_*` config defaults to false. Making any
  of them default-on is a product decision: ship the machinery opt-in and
  record the flip proposal in the leaf's notes + docs/agents/opt-in-defaults.md.
- AssembleOrdered stable/unstable classification is byte-exact; the
  stablePrefixHash (internal/agent/prompt.go:468) MUST NOT drift across
  requests within a session.
- Tests: `go test -p 2` always (macOS ephemeral-port exhaustion).
- Do NOT git commit / git add from leaf agents.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| A0 | 01-context-assembler/00-research-budgeting.md | leaf | none | 30K | A |
| A1 | 01-context-assembler/01-allocator-core.md | leaf | A0 | 80K | B |
| A2 | 01-context-assembler/02-doc-budgets.md | leaf | A1 | 75K | C |
| A3 | 01-context-assembler/03-integration.md | leaf | A1, A2, B1, B2, B3 | 90K | D |
| B0 | 02-memory-tiers/00-research-tiers.md | leaf | none | 30K | A |
| B1 | 02-memory-tiers/01-working-tier-store.md | leaf | B0 | 85K | B |
| B2 | 02-memory-tiers/02-policy-automation.md | leaf | B1 | 85K | C |
| B3 | 02-memory-tiers/03-demotion.md | leaf | B2 | 70K | C |
| E1 | 04-e2e.md | leaf | A3, B3 | 85K | E |

Concurrency groups: A (research, parallel), B (core stores, parallel),
C (policies/docs, parallel), D (integration, serial), E (e2e, serial).

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

This tree runs an IN-LEAF TDD gate, not separate 00T leaves (tree charter:
new feature tests live in the hermetic e2e tier + in-package unit tests;
each leaf's task table is RED-first). The per-branch `## Test Gate` matrices
are validated in-session by the control agent at REVIEW time: a leaf may not
move past REVIEWED until its matrix rows are VALIDATED against the
delivered tests. On delta, the LESSER artifact updates to the GREATER.

### Phase 1: Dispatch Concurrency Group A

Dispatch 01-context-assembler/00-research-budgeting.md and
02-memory-tiers/00-research-tiers.md simultaneously via `delegate_task`:
goal = one-line imperative; context = full leaf text + conventions +
no-commit/no-git-add rules. Research leaves produce pinned findings docs
their siblings cite.

### Phase 2: Dispatch Groups B, C, D in order

Group B (A1 + B1) in parallel after A0/B0 COMPLETE; group C (A2, B2, B3)
after their parents; group D (A3) after A1, A2 and B1-B3. Dispatch shape:
`tasks: [{goal, context: <full leaf text + C1 contract + conventions +
relevant source INLINED + no-commit rule>}]`.

### Phase 3: Review and Commit Each Child

Review in-session (main model -- never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run the leaf's verify commands; gate-test
diff must be empty. Pass: commit the leaf's exact files + status REVIEWED.
Gaps: re-dispatch with findings, max 3 cycles, then escalate.

### Phase 4: Integration Review + Group E

After all branch children REVIEWED: run branch Integration Test Plans, then
dispatch 04-e2e.md. Pass criteria for tree COMPLETE: full e2e suite green
(`make e2e-fast-area AREA=context-memory-engine`), manifest entry present,
`go test -p 2 ./...` green, no import cycles, stable-prefix hash tests
green.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (C1/C2/C3 signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed); verify commands green
- [ ] Invariants respected (see Invariants section)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] ASCII only in all new/modified files

Output: APPROVED or a list of specific gaps.

## Coding Conventions

Passed to every implementation agent in dispatch context:

- **Language:** Go (module github.com/nousresearch/meept layout; internal/*
  packages). gofmt clean; imports grouped stdlib/external/internal.
- **Error handling:** wrap with %w, return early; no ignored errors; no bare
  panic(err); no I/O under mutex (mutexio analyzer).
- **IDs:** never time.Now().UnixNano() or math/rand; use pkg/id.Generate().
- **Maps:** two-value assertion when presence matters.
- **Set* methods:** nil guard ALWAYS (typed-nil interface trap), enforced by
  internal/tools/builtin/setters_test.go pattern.
- **Tests:** in-package `*_test.go` for units; hermetic only (no live
  network); table-driven where natural; run with -p 2. New feature e2e tests
  in e2e/suites/<name>/ with `e2e` build tag + manifest entry.
- **ASCII only** in all new files and docs.
- **Exploration:** use grep/search, never feed read_file line numbers back
  into writes; write each file once.
- **No git commit / git add** from leaf agents; orchestrator commits after
  review.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element | Verified by |
|-----|---------------------------------------------|--------------|-------------|
| R1 | unified per-request token budgeting (context assembler) | A1 allocator-core, C2 Allocation | contextalloc unit tests |
| R2 | window-fractions not static constants | A1 task T3-T5 | contextalloc unit tests |
| R3 | closing the unbounded-CLAUDE.md hole | A2 doc-budgets | doc budget unit tests |
| R4 | stable-prefix hash must not drift | A2 invariant tasks | prompt stable-hash tests |
| R5 | 3-tier memory engine (working/episodic/semantic) | B1 working-tier-store over existing episodic/semantic | working tier unit tests |
| R6 | automated eviction + compression | B2 policy-automation | tierpolicy unit tests |
| R7 | demotion path semantic->episodic | B3 demotion | demotion unit tests |
| R8 | per-tier token budgets exposed to branch A's allocator | C1 TierBudgetProvider, A3 integration | seam unit tests |
| R9 | optional seam, nil = today's behavior | A3 integration, C1 seam rule | typed-nil test |
| R10 | automated eviction default-on flagged as product decision | B2 opt-in + flip proposal doc | docs check |
| R11 | research findings pinned, siblings cite | A0, B0 findings docs | findings doc exists + cited |
| R12 | hermetic e2e tier + manifest entry | 04-e2e, C3 | make e2e-fast-area AREA=context-memory-engine |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-context-assembler/00-research-budgeting | PENDING | 0 | |
| 01-context-assembler/01-allocator-core | PENDING | 0 | |
| 01-context-assembler/02-doc-budgets | PENDING | 0 | |
| 01-context-assembler/03-integration | PENDING | 0 | |
| 02-memory-tiers/00-research-tiers | PENDING | 0 | |
| 02-memory-tiers/01-working-tier-store | PENDING | 0 | |
| 02-memory-tiers/02-policy-automation | PENDING | 0 | |
| 02-memory-tiers/03-demotion | PENDING | 0 | |
| 04-e2e | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

After all children REVIEWED, from repo root:

```
go build ./...
go vet ./internal/agent/contextalloc/... ./internal/memory/working/... ./internal/memory/tierpolicy/...
go test -p 2 ./internal/agent/... ./internal/memory/... ./internal/llm/...
go test -p 2 -race ./internal/agent/contextalloc/... ./internal/memory/working/... ./internal/memory/tierpolicy/...
make e2e-fast-area AREA=context-memory-engine
make graphs-check   # only if bus/RPC surfaces changed (they should not)
```

Expected: all green. Cross-boundary checks: C1 seam compiles with provider
nil AND with a stub provider; CLAUDE.md over doc budget truncates
deterministically; stable-prefix hash unchanged vs pre-change baseline test.

## Test Gate

In-leaf TDD gate (tree charter; see Dispatch Protocol Phase 0). Coverage
matrix validated at REVIEW time in-session.

| # | Feature (from plan) | Source (leaf/spec) | Unit tests | E2E tests | Status |
|---|---------------------|--------------------|-----------|-----------|--------|
| F1 | window-fraction allocation, deterministic | A1 T3-T5 | contextalloc allocator tests | context-memory-engine (budget probes) | PENDING |
| F2 | doc budget bounds CLAUDE.md/README | A2 | artifact_integration budget tests | context-memory-engine | PENDING |
| F3 | stable prefix hash preservation | A2 | prompt ordered-assembly tests | context-memory-engine | PENDING |
| F4 | working tier store + recall bound | B1 | internal/memory/working tests | context-memory-engine | PENDING |
| F5 | automated eviction + compression policies | B2 | tierpolicy tests | context-memory-engine | PENDING |
| F6 | semantic->episodic demotion | B3 | demotion tests | context-memory-engine | PENDING |
| F7 | tier budgets into allocator, nil seam = today | A3 | seam + typed-nil tests | context-memory-engine | PENDING |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: (empty at authoring)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Verified anchors were grep-confirmed at authoring time (2026-10-31); two
  brief drifts corrected: ContextFirewall lives in internal/llm (not
  internal/agent); SetCompactor wired at internal/agent/loop.go:2486;
  EnforceMemoryItemBudget wired at loop.go:6884; WrapToolOutput LIVE at
  loop.go:5362 (the older "never emitted" claim is stale).
- Estimated total: branch A ~600-1000 LOC, branch B medium-large; 4-6 days
  with branches in parallel.
- The known statics (loop.go:4169-4184, conversation.go:22,33) remain as
  FALLBACKS when no model context limit is discovered; the allocator
  supersedes them when ContextDiscovery resolves a real window.
