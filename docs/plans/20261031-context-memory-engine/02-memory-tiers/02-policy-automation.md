# Policy Automation: Automated Eviction + Compression - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** New `internal/memory/tierpolicy` package: policy engine
  producing evict/compress actions over tier states (contract C5),
  config plumbing (all opt-in, default false), daemon background tick
  wiring, and the default-on flip proposal doc.
- **Dependencies:** 01-working-tier-store.md
- **Estimated Context:** 85K
- **Concurrency Group:** C
- **Audit references:** R6, R10

## Goal

Automate what today only the manual consolidation tick does: continuously
(bounded-interval background tick, NEVER the chat path) evaluate tier
token loads against ceilings and emit eviction/compression actions.
Eviction reuses PlanUsefulEviction ranking (internal/memory/consolidation.
go:537-566); compression reuses the existing distill pipeline (queue jobs
memory.l1_extract / memory.l2_scene, internal/memory/distill_async.go:
31-33) by enqueueing - never running LLM work inline. Everything opt-in
(memory.tiers.enabled default false); the PROPOSAL to make automated
eviction default-on is written up as a product decision with rationale
(docs/agents/opt-in-defaults.md rule), NOT implemented as default-on.

## Context

- internal/memory/consolidation.go:537-566 - PlanUsefulEviction usage -
  reuse for eviction ranking via the same inputs (memories + net votes).
- internal/memory/distill_async.go:31-33 - queue job types - compression
  enqueues these; never blocks the tick on LLM work.
- internal/config/schema.go:1593-1597 - l0_enabled opt-in pattern - copy
  the gendoc comment style for new keys.
- internal/memory/staleness.go - staleness scoring input.
- internal/daemon/ - wiring site (grep for memory manager construction);
  typed-nil guards per internal/daemon/AGENTS.md; construct after deps.
- docs/agents/opt-in-defaults.md - the rule this leaf's config follows.

## Verified Anchors

- internal/memory/consolidation.go:537-566 - PlanUsefulEviction - eviction ranking to reuse
- internal/memory/distill_async.go:31-33 - MemoryJobL1Extract/MemoryJobL2Scene - compression enqueue targets
- internal/config/schema.go:1593-1597 - L0Enabled opt-in - pattern to copy
- internal/memory/consolidation.go:60,96 - single-start Consolidator - must not be duplicated
- internal/config/schema.go:3489,3533 - L0 retention 30d / access expiry 90d prior art - cite, do not duplicate

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/memory/tierpolicy/engine.go  (contract C5, verbatim from parent)
package tierpolicy

type TierState struct { Tier string; Tokens, Items, Ceiling int }
type Action struct { Kind string; TargetIDs []string; Reason string }
type Deps struct{ /* usefulness-vote lookup, staleness lookup, distill enqueuer, episodic/semantic delete - interfaces defined in-package */ }
type Config struct {
    WorkingConfig     working.Config
    EvictOverRatio    float64
    CompressOverRatio float64
    Interval          time.Duration
    Enabled           bool // default FALSE
}
func New(cfg Config, deps Deps) *Engine
func (e *Engine) Evaluate(ctx context.Context, states []TierState) ([]Action, error)

// internal/memory/tierpolicy/execute.go
func (e *Engine) Execute(ctx context.Context, actions []Action) (ExecutionReport, error)
// Execute performs deletes/enqueues against Deps; called ONLY from the
// background tick owner, never from chat-path code.

// internal/config/schema.go (modified): [memory.tiers] enabled (default
// false), evict_over_ratio, compress_over_ratio, interval_seconds.
```

### What This Leaf Consumes

- C4 working.Store (../01-working-tier-store.md) - working-tier state +
  flush-queue pressure as TierState inputs
- internal/memory existing: PlanUsefulEviction inputs, queue enqueue API
  (grep internal/queue for the enqueue signature used by distill_async.go)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Engine core: Evaluate over TierStates; over EvictOverRatio*Ceiling => evict actions ranked by usefulness (Deps lookup wrapping PlanUsefulEviction inputs); over CompressOverRatio => compress actions (enqueue targets) for the middle band (compress band inside evict band: compress first, evict only what compression cannot fit) | Create: internal/memory/tierpolicy/engine.go; Test: internal/memory/tierpolicy/engine_test.go | `go test -p 2 ./internal/memory/tierpolicy/ -run TestEvaluate` | package absent | bands correct on table-driven states; noop under ceiling; Reasons machine-readable | Given state Tokens=95 Ceiling=100 EvictOver=0.9 CompressOver=0.8; When Evaluate; Then compress band first, evict remainder, deterministic order |
| T2 | Execute: applies evictions via Deps delete + compression via queue enqueue (memory.l1_extract-style job payloads); dry-run mode flag; ExecutionReport counts | Modify: engine.go, execute.go; Test: execute_test.go | `go test -p 2 ./internal/memory/tierpolicy/ -run TestExecute` | Execute absent | stub Deps record calls; dry-run applies nothing but reports; enqueue never blocks on LLM | Given 2 evict + 1 compress actions and dry-run; When Execute; Then report counts 3, zero Deps mutations |
| T3 | Config plumbing: [memory.tiers] block, Enabled default false, gendoc comments (copy l0_enabled style); config template updated | Modify: internal/config/schema.go, config/meept.json5 (template if memory block exists there - grep); Test: internal/config/schema_tiers_test.go | `go test -p 2 ./internal/config/ -run TestTiersDefault` | keys absent | zero-value => disabled; ratios/interval have sane defaults when enabled but unset | Given fresh config; When read; Then Enabled false; Given Enabled=true unset ratios; When resolved; Then defaults 0.9/0.8/300s applied |
| T4 | Daemon background tick wiring: gated on config; constructs Engine AFTER memory manager + queue deps; typed-nil guards; single-start protection (a started engine must not double-start); tick loop stops on ctx cancel | Modify: internal/daemon/ (wiring site from grep); Test: internal/daemon/tiers_wiring_test.go (or in-package tierpolicy lifecycle test if daemon test harness resists) | `go test -p 2 ./internal/daemon/ -run TestTiersWiring` | no wiring | disabled config => no goroutine; enabled => tick respects Interval, ctx cancel stops cleanly, second Start returns error (single-start) | Given enabled config and 50ms interval; When run 150ms then cancel; Then >=2 evaluations, clean stop, no double-start |
| T5 | Opt-in flip proposal: product-decision doc for defaulting automated eviction ON | Create: docs/agents/opt-in-defaults.md APPEND section (or docs/plans/20261031-context-memory-engine/02-memory-tiers/flip-proposal.md if the doc forbids appends - choose by reading its header) | `grep -l 'tiers' docs/agents/opt-in-defaults.md` | doc lacks section | proposal states: what flips, rationale (findings R-Bx citations), risk, rollback, decision owner | Given the rule doc; When read; Then flip is clearly a PROPOSAL not a behavior change |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2 ./internal/memory/tierpolicy/ ./internal/config/ ./internal/daemon/`)
- [ ] Contract C5 satisfied exactly; Execute OFF the chat path
- [ ] Consolidator not duplicated; PlanUsefulEviction reused not forked
- [ ] All config defaults false; flip proposal present
- [ ] ASCII only; gofmt clean; typed-nil guards on Start/Set*
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines of go test output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Contract C5 matches exactly; no chat-path execution
- [ ] Single-start engine; typed-nil guards; clean ctx cancel
- [ ] Config defaults false; flip proposal is proposal-only
- [ ] No debug artifacts, no scope creep, no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

Cite findings-memory-tiers.md R-Bx IDs for band ratios and compress-vs-
evict precedence in comments. The engine MUST NOT import internal/agent
and MUST NOT construct a Consolidator (Deps interfaces only). If the
queue enqueue API shape differs from expectation, adapt to the REAL API
found by grep and record the delta in the completion report.
