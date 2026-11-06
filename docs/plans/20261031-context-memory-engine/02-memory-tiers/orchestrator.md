# Memory Tiers: Working/Episodic/Semantic Engine - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch node.
> Your job: (1) dispatch leaf agents, (2) review their work, (3) re-dispatch
> if incomplete, (4) track completion. Do NOT implement code yourself. All
> implementation happens in leaf agents. Full operational detail: skill
> `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../../master.md
- **Children:** 4 leaves (00-research-tiers, 01-working-tier-store,
  02-policy-automation, 03-demotion)
- **Scope:** A true working tier integrated with the conversation buffer,
  automated eviction + compression policy machinery, and a
  semantic-to-episodic demotion path - all opt-in, feeding per-tier
  budgets to branch A's allocator through contract C1.

## Goal

Meept has episodic storage (internal/memory/episodic.go, FTS5), async L1
claims / L2 scene distillation (internal/memory/distill_async.go:31-33,
config/schema.go:1593-1597 L0Enabled opt-in), usefulness eviction
(consolidation.go:537-566 PlanUsefulEviction), and a single-start
Consolidator (:60,96) - but no WORKING tier (the conversation buffer is
not memory-integrated), no automated cross-tier eviction/compression
policy (eviction runs only on the consolidation tick), and no demotion
path when semantic entries go stale. This branch builds:

1. `internal/memory/working` - a bounded conversation-buffer-backed
   working tier (store + recall), opt-in via config.
2. `internal/memory/tierpolicy` - automated eviction/compression policies
   over the tiers (size/token triggers, compression = distill-to-claim
   reuse, eviction = PlanUsefulEviction reuse), OFF the chat path.
3. Demotion: semantic -> episodic when confidence/staleness thresholds
   trip (internal/memory/staleness.go prior art).

Everything is OPT-IN (default false) per docs/agents/opt-in-defaults.md;
default-on eviction/compression is flagged as a product decision with a
written flip proposal. Per-tier token budgets are exposed through contract
C1 (TierBudgetProvider) - the adapter that presents them to branch A's
allocator is authored in branch A's 03-integration.md so this branch never
imports internal/agent.

## Architecture

```
conversation turns (chat path, read-only mirror)
        |  (append hook, bounded ring, token-accounted)
        v
internal/memory/working.Store  --Recall--> C1 RecallFunc
        | flush on session end / overflow        ^
        v                                        | adapter (branch A leaf 03)
episodic (existing, FTS5) <---- demotion ---- semantic (L1 claims, existing)
        ^                                        |
        |        internal/memory/tierpolicy.PolicyEngine (background tick,
        +-------- reuse Consolidator single-start; eviction via
                  PlanUsefulEviction; compression via distill pipeline)
```

The working tier mirrors conversation state; the policy engine runs on a
background tick (never inline in the chat path); recall is synchronous
read-only and safe for concurrent use.

## Interface Contracts

### C1: TierBudget + TierBudgetProvider (SHARED -- full definition in ../../master.md)

```
// File: internal/agent/contextalloc/tier.go (owned by branch A leaf 01)
type TierName string // "working" | "episodic" | "semantic"
type RecallFunc func(ctx context.Context, query string, limit int) []string
type TierBudget struct { Tier TierName; Ceiling int; Recall RecallFunc }
type TierBudgetProvider interface {
    TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget
}
```

THIS branch provides the underlying stores whose recall functions the
branch-A adapter wraps. Required store surfaces (pinned; branch A's
adapter compiles against these):

### C4: Working tier store (owner: 01-working-tier-store)

```
// internal/memory/working/store.go
package working

type Config struct {
    MaxTokens   int           // ring capacity in tokens (heuristic count)
    MaxItems    int           // hard item cap
    TTL         time.Duration // per-item expiry
}
type Store struct{ /* ... */ }
func New(cfg Config, tk TokenCounter) *Store
func (s *Store) Append(ctx context.Context, turn Turn) error      // evicts to fit
func (s *Store) Recall(ctx context.Context, query string, limit int) []string
func (s *Store) Snapshot() []Turn                                  // read-only copy
func (s *Store) Flush(ctx context.Context, dst EpisodicSink) error // hand off to episodic
type Turn struct { ID, SessionID, Role, Content string; At time.Time }
type TokenCounter interface{ CountTokens(string) int }             // satisfied by llm tokenizers
type EpisodicSink interface{ AppendTurns(ctx context.Context, turns []Turn) error }
```

- Consumers: 02-policy-automation (capacity triggers), 03-demotion (sink
  behavior), branch A 03-integration (Recall via adapter).
- Recall MUST be safe for concurrent use; Append MUST NOT do I/O (pure
  memory + eviction); Flush does the only I/O.

### C5: Policy engine (owner: 02-policy-automation)

```
// internal/memory/tierpolicy/engine.go
package tierpolicy

type TierState struct {
    Tier    string            // "working"|"episodic"|"semantic"
    Tokens  int               // current estimated token load
    Items   int
    Ceiling int               // configured budget hint (0 = unmanaged)
}
type Action struct {
    Kind        string   // "evict"|"compress"|"demote"|"noop"
    TargetIDs   []string // memory IDs (episodic/semantic) or turn IDs (working)
    Reason      string   // machine-readable: "over_ceiling"|"stale"|"low_usefulness"|...
}
type Engine struct{ /* ... */ }
func New(cfg Config, deps Deps) *Engine
func (e *Engine) Evaluate(ctx context.Context, states []TierState) ([]Action, error)
// Deps carries lookups (usefulness votes, staleness) - NEVER the
// Consolidator directly; the engine REUSES PlanUsefulEviction
// (internal/memory/consolidation.go:537-566) for eviction ranking.
// Execution of Actions runs on the caller's tick (daemon background
// job), never inside Evaluate, never on the chat path.
type Config struct {
    WorkingConfig  working.Config
    EvictOverRatio float64 // e.g. 0.9 of Ceiling
    CompressOverRatio float64
    Interval       time.Duration
    Enabled        bool    // config default FALSE (opt-in)
}
```

### C6: Demotion path (owner: 03-demotion)

```
// internal/memory/tierpolicy/demote.go
func (e *Engine) PlanDemotions(ctx context.Context, semanticIDs []string) ([]Action, error)
// semantic -> episodic: copies the claim content into the episodic store
// (Lossless at the episodic tier), marks the semantic entry demoted
// (metadata flag, not delete), driven by staleness.go thresholds.
// Idempotent: re-planning an already-demoted ID returns noop.
```

## Invariants (binding on every leaf)

- Consolidation OFF the chat path; Consolidator single-start preserved
  (internal/memory/consolidation.go:60,96) - the policy engine runs as a
  separate background tick and MUST NOT construct a second consolidator.
- Wiki/trace stores (internal/memory/trace_store.go, knowledge graph
  internals) NEVER feed recall or prompt builders.
- Typed-nil guards on every Set*/registration.
- Opt-in defaults: every new config key default false; default-on flip is
  a product decision documented in docs/agents/opt-in-defaults.md per
  docs/agents/opt-in-defaults.md rules (ship machinery opt-in + flip
  proposal).
- No import of internal/agent from internal/memory (direction enforced:
  branch A's adapter lives in internal/agent/contextalloc).
- Tests: `go test -p 2`; hermetic; ASCII only.
- Do NOT git commit / git add from leaf agents.

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00 | 00-research-tiers.md | leaf | none | 30K | A |
| 01 | 01-working-tier-store.md | leaf | 00 | 85K | B |
| 02 | 02-policy-automation.md | leaf | 01 | 85K | C |
| 03 | 03-demotion.md | leaf | 02 | 70K | C |

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

In-leaf TDD gate (tree charter -- hermetic e2e coverage lands in
../../04-e2e.md; in-package unit tests are RED-first per task tables).
The `## Test Gate` matrix is validated in-session at REVIEW time; on
delta the LESSER artifact updates to the GREATER.

### Phase 1: Dispatch Concurrency Group A

Dispatch 00-research-tiers.md. It pins
`findings-memory-tiers.md` in this directory; leaves 01-03 cite it.

### Phase 2: Groups B, C

Dispatch 01 (group B); then 02 and 03 (group C) after 01 REVIEWED.
delegate_task shape: goal = one-line imperative; context = full leaf text
+ contracts C1/C4/C5/C6 + conventions (../../master.md) + relevant source
INLINED + "Do NOT commit. Do NOT run git add."

### Phase 3: Review and Commit Each Child

Review in-session (main model -- never a delegated reviewer): leaf tasks,
contracts, review checklist, verify commands green (`go test -p 2`).
Commit the leaf's exact files; status -> REVIEWED. Max 3 re-dispatch
cycles, then escalate to ../../master.md.

### Phase 4: Integration Review

After 03 REVIEWED: run the Integration Test Plan; confirm opt-in config
surface (config get works, default false), no chat-path consolidation,
import direction clean. Report COMPLETE to ../../master.md. Note for the
parent: branch A leaf 03 consumes C4/C5/C6 through its adapter; coordinate
dispatch of ../../01-context-assembler/03-integration.md (group D) after
this branch's 01-03 are REVIEWED.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Contracts C1/C4/C5/C6 satisfied exactly (signatures, types, paths)
- [ ] Policy execution OFF the chat path; Consolidator single-start intact
- [ ] Wiki/trace stores absent from recall paths
- [ ] All new config keys default false; flip proposal present where required
- [ ] Tests passing (`go test -p 2`); no import of internal/agent
- [ ] No scope creep; no debug artifacts; no line-number corruption
- [ ] ASCII only

Output: APPROVED or specific gaps.

## Coding Conventions

See ../../master.md `## Coding Conventions` (binding; passed verbatim in
every dispatch context). Branch-specific additions:

- internal/memory/working and internal/memory/tierpolicy are NEW
  packages: import stdlib, internal/llm (tokenizer), internal/memory
  (types, staleness, consolidation helpers only) - never internal/agent.
- Token counting via a TokenCounter interface (C4); tests use a fixed
  chars/4 stub for determinism (no dependence on HeuristicTokenizer
  drift).
- Eviction/compression reuse existing machinery (PlanUsefulEviction,
  distill pipeline) - do not fork parallel implementations.
- Every `Set*` or registration function: typed-nil guard + test.

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R5 | 3-tier memory engine (working/episodic/semantic) | 01 working-tier-store over existing tiers | working unit tests |
| R6 | automated eviction + compression | 02 policy-automation | tierpolicy unit tests |
| R7 | demotion path semantic->episodic | 03 demotion | demotion unit tests |
| R8 | per-tier budgets exposed to allocator | C4/C5 surfaces + branch A adapter | branch A seam tests |
| R10 | default-on flagged as product decision | 02 opt-in + flip proposal | docs check |
| R11 | pinned research findings | 00 research | findings doc cited by 01-03 |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00-research-tiers | COMPLETE | 1 | landed b8b138c6 (findings doc, reviewed) |
| 01-working-tier-store | COMPLETE | 1 | landed 14b21b5e (issue 81) |
| 02-policy-automation | COMPLETE | 1 | landed f8d2bfd6 (issue 81) |
| 03-demotion | COMPLETE | 1 | landed 9872de89 (issue 81) |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

```
go build ./...
go vet ./internal/memory/working/... ./internal/memory/tierpolicy/...
go test -p 2 ./internal/memory/...
go test -p 2 -race ./internal/memory/working/... ./internal/memory/tierpolicy/...
go list -deps ./internal/memory/working ./internal/memory/tierpolicy | grep -c internal/agent   # expect 0
```

Cross-boundary: Append-overflow-Flush round-trips turns into the episodic
store (existing FTS5 store via a test sink and via the real episodic
store in-package); Engine.Evaluate on an over-ceiling state yields
evict/compress actions whose IDs exist; PlanDemotions is idempotent; with
Enabled=false nothing runs (no ticks, no actions).

## Test Gate

In-leaf TDD gate; matrix validated at REVIEW time in-session.

| # | Feature (from plan) | Source (leaf/spec) | Unit tests | E2E tests | Status |
|---|---------------------|--------------------|-----------|-----------|--------|
| F4 | working tier store + recall bound | 01 | internal/memory/working tests | context-memory-engine | VALIDATED |
| F5 | automated eviction + compression policies | 02 | tierpolicy tests | context-memory-engine | VALIDATED |
| F6 | semantic->episodic demotion | 03 | demotion tests | context-memory-engine | VALIDATED |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (empty at authoring)

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Notes

- Verified anchors (grep-confirmed 2026-10-31): internal/config/schema.go:
  1593-1597 L0Enabled (l0_enabled) opt-in + retention note; internal/
  memory/distill_async.go:31-33 MemoryJobL1Extract/MemoryJobL2Scene queue
  jobs; internal/memory/types.go:13-29 MemoryType constants; internal/
  memory/episodic.go FTS5 episodic store; internal/memory/consolidation.go
  :96 Run single pass, :60 single-start note, :537-566 PlanUsefulEviction
  usage; internal/memory/staleness.go staleness scoring; internal/memory/
  manager.go manager surface; internal/memory/dual_store.go dual-store
  routing.
- Existing retention prior art: L0 daily-file prune 30d, access expiry
  90d (config/schema.go:3489, :3533 region) - cite, do not duplicate.
