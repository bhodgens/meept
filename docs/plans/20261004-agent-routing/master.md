# Agent Routing Improvements - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none
- **Children:** 5 leaves (01-05)
- **Scope:** Implement five agent-routing improvements in the tactical scheduler and queue: worker wake-up, quota-aware agent selection, successor hints, honest claim pinning docs, and routing telemetry.

## Goal

The tactical scheduler (internal/agent/tactical.go) routes steps to executor
agents through a static hint table and workers poll the queue on a 1s timer.
Five improvements identified in the 2026-10-04 routing review:

1. Event-driven worker wake-up: workers poll every 1s even when the bus just
   told them a job was enqueued. Add a wake channel.
2. Quota-aware agent selection: selectAgent ignores endpoint park/cooldown
   state, so code hints still route to a parked coder.
3. Successor hints: agents can only suggest the next agent through the heavy
   handoff tool. Add an advisory `suggested_next_hint` field.
4. Claim-pinning documentation: `job.AgentID` is a soft preference, not a
   lock; the code does not say so.
5. Routing telemetry: no record of hint-table misses, explicit overrides, or
   handoff acceptance.

## Architecture

All five leaves touch the tactical scheduler path but each owns a disjoint
file/region set. Leaf 01 owns internal/queue and internal/worker only.
Leaf 02 owns the selectAgent region plus a new internal/agent file.
Leaf 03 owns the step-job envelope and a step field.
Leaf 04 owns comments in internal/queue only (no code change).
Leaf 05 owns a new internal/agent file plus two small publish-site additions.

Concurrency groups: A = {01, 03, 04}, B = {02, 05}. Group B waits for
group A only if leaf 03's field lands in a file leaf 02 touches — it does
not, so B can also run parallel with A. For safety: dispatch A first,
then B.

## Interface Contracts

### Contract 1: queue wake signal (Owner: 01; Consumers: none in-tree)

```
// File: internal/queue/queue.go
// PersistentQueue gains an unexported wake channel + waiter registration:

// WakeWaiter registers a channel that receives a non-blocking notification
// on every Enqueue. Multiple waiters allowed. Returns an unregister func.
func (q *PersistentQueue) WakeWaiter(ch chan<- struct{}) (unregister func())

// On Enqueue: after store.Insert succeeds, send struct{}{} to every
// registered channel in a non-blocking select (channel full = waiter
// already awake, drop the signal).

// File: internal/worker/worker.go
// Worker.run: wait on (ctx.Done, timer, wakeCh). After a processed=true
// job, drain the wake channel non-blocking, then immediately retry claim
// without sleeping (already the behavior via backoff reset — keep it).

// Worker Config gains: WakeCh <-chan struct{}  (nil = poll-only, legacy)
// Pool.AddWorker creates the channel per worker and registers it with
// q.(WakeNotifier) when the queue implements the optional interface:

type WakeNotifier interface {
    WakeWaiter(ch chan<- struct{}) (unregister func())
}
```

### Contract 2: quota-aware agent selection (Owner: 02)

```
// File: internal/agent/agent_quota_aware.go (NEW)
package agent

// AgentHealthSource reports whether an agent's provider endpoint is
// currently parked or in cooldown. Implemented by whatever llm exposes;
// if no live source exists, the scheduler's setter stays nil and routing
// is unchanged (byte-identical legacy).
type AgentHealthSource interface {
    AgentParkedOrCooling(agentID string) bool
}

// File: internal/agent/tactical.go (selectAgent region ONLY)
func (ts *TacticalScheduler) selectAgent(step *task.TaskStep) string {
    if agentID, ok := config.ToolHintAgent(step.ToolHint); ok {
        // NEW: if ts.health != nil && ts.health.AgentParkedOrCooling(agentID),
        // do NOT change the route (hint table remains authoritative for
        // capability matching) but publish a routing.warning event:
        // {step_id, agent_id, reason: "endpoint_parked_or_cooling"}.
        return agentID
    }
    return config.AgentIDChat
}

// TacticalScheduler gains unexported field: health AgentHealthSource
// + setter SetAgentHealthSource(src AgentHealthSource) with nil guard.
// Wiring stays OUT of this leaf if it requires daemon changes; report the
// wiring line and let the orchestrator add it (one line in components.go).
```

### Contract 3: successor hints (Owner: 03)

```
// File: internal/task/step.go — TaskStep gains one field:
SuggestedNextHint string `json:"suggested_next_hint,omitempty"`

// File: internal/agent/tactical.go — stepJobPayloadFromStep gains the
// field in BOTH directions (step -> payload on schedule; payload -> step
// on retry reconstruction).

// File: internal/daemon/components.go — buildStepResult (the step-job
// envelope) extracts `suggested_next_hint` from the finished turn's
// final report JSON if present. On OnJobCompleted (tactical.go), after
// evidence extraction, copy payload field onto step and persist.

// SEMANTICS: advisory only. Nothing reads it for routing in this tree.
// A later tree may feed it to selectAgent as a tiebreaker. Add a
// routing.telemetry event publish when the field is non-empty:
// {step_id, suggested_next_hint} so usage is measurable from day one.
```

### Contract 4: claim-pinning docs (Owner: 04)

```
// No code. Two comment blocks:
// internal/queue/store.go ClaimNextForAgent — above the query: explain
// agent_id is a soft preference (ORDER BY pinned-first) not exclusivity;
// unpinned workers' query has no agent filter and may claim pinned jobs;
// persona rides the job payload so this is safe.
// internal/queue/job.go WithAgentID — same statement, shorter.
```

### Contract 5: routing telemetry (Owner: 05)

```
// File: internal/agent/routing_telemetry.go (NEW)
package agent

// RoutingTelemetry records hint-table routing decisions as bus events
// and (when wired) metrics rows. Fire-and-forget: never blocks routing,
// never fails a step.
type RoutingTelemetry struct{ /* bus + optional metrics store */ }

func NewRoutingTelemetry(bus *bus.MessageBus) *RoutingTelemetry
func (rt *RoutingTelemetry) RecordHintMiss(stepID, toolHint string)
func (rt *RecordHintRoute(stepID, agentID, source string)) // source: "hint_table"|"explicit"|"chat_fallback"
func (rt *RoutingTelemetry) RecordHandoff(req TaskID, fromStepID, toAgentID string, accepted bool)

// Topics: "routing.hint_miss", "routing.decision", "routing.handoff_outcome".
// Wire points (small, in leaf):
//   selectAgent: chat-fallback branch + hint-hit branch call RecordHintRoute.
//   assignStepAgent: explicit-AgentID branch calls RecordHintRoute source="explicit".
//   HandleHandoff: after amendment/direct creation success/failure, RecordHandoff.
// TacticalScheduler gains field telemetry *RoutingTelemetry, constructed
// in NewTacticalScheduler when bus != nil. Nil-guard every call site.
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-worker-wakeup.md | leaf | none | 45K | A |
| 02 | 02-quota-aware-selection.md | leaf | none | 40K | B |
| 03 | 03-successor-hints.md | leaf | none | 40K | A |
| 04 | 04-claim-pinning-docs.md | leaf | none | 10K | A |
| 05 | 05-routing-telemetry.md | leaf | none | 40K | B |

**Concurrency groups:** A = {01, 03, 04}, B = {02, 05}. Groups are
file-disjoint. Dispatch group A first (3 agents), then group B (2 agents).

## Dispatch Protocol

For each concurrency group, in order:

### Phase 1: Dispatch Group A (01, 03, 04)

Dispatch three `delegate_task` calls in parallel:

- Goal: "Implement all tasks from docs/plans/20261004-agent-routing/<NN>-<name>.md"
- Context: the full leaf text + the matching Interface Contract above +
  coding conventions + these inlined anchors:
  - Leaf 01: internal/queue/queue.go:147-168 (Enqueue), :182-267 (Claim),
    internal/worker/worker.go:165-215 (run loop), :62-89 (Config/NewWorker),
    internal/worker/pool.go:169-194 (AddWorker).
  - Leaf 03: internal/task/step.go TaskStep struct,
    internal/agent/tactical.go stepJobPayloadFromStep,
    internal/daemon/components.go buildStepResult region.
  - Leaf 04: internal/queue/store.go:380-423, internal/queue/job.go:120-123.
- Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
- Include: "Never feed read_file output into write_file. Write once and stop."
- Include: "Commit explicit paths only; do not sweep sibling files."

### Phase 2: Review and Commit Each Child

Orchestrator reviews in-session per leaf:

1. `go build ./...` on touched packages; `go test -p 2 -short -count=1` on
   the leaf's package.
2. Read the diff against the leaf spec and the contract.
3. On gaps: re-dispatch with specific feedback (max 3 cycles).
4. On pass: `git add <exact paths> && git commit` per leaf, conventional
   message `feat(agent-routing): <leaf name>` / `docs(queue): ...` for 04.
   Tracking table -> REVIEWED.

### Phase 3: Dispatch Group B (02, 05)

Same protocol as Phase 1 with the leaf 02/05 contexts. Review + commit
identically.

### Phase 4: Integration Review

1. `go build ./...` full module.
2. `go test -p 2 -short -count=1 ./internal/queue/... ./internal/worker/... ./internal/agent/... ./internal/task/... ./internal/daemon/...`
3. `gofmt -l` on all touched files (must be empty).
4. `make graphs` freshness: if bus topics changed, run `make graphs` and
   commit docs/generated updates with the last leaf.
5. AGENTS.md review per repo rule: bus topics added -> confirm
   docs/generated regeneration covers them; no new packages, so package
   table unchanged.

## Review Checklist

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues
- [ ] No debug artifacts: no print/stdout debugging, no TODOs, no placeholder values, no commented-out code
- [ ] No line-number corruption: no `     N|` prefixes baked into source files

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go (module github.com/caimlas/meept), stdlib + existing deps only.
- **Naming:** exported PascalCase, unexported camelCase; receivers `q`, `w`, `ts`, `rt`.
- **Error handling:** wrap with %w; no ignored errors (pre-commit enforced); no bare panic.
- **Concurrency:** never hold a mutex across channel ops or I/O (mutexio analyzer enforces).
- **ID generation:** never time.Now().UnixNano()/math/rand — pkg/id.Generate only.
- **Testing:** table-driven stdlib tests, `_test.go` alongside; hermetic (no live providers).
- **Formatting:** gofmt before reporting; no reformat of untouched code.
- **Typed-nil:** every Set* / setter gets a nil guard (setters_test.go enforces).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-worker-wakeup | PENDING | 0 | |
| 02-quota-aware-selection | PENDING | 0 | |
| 03-successor-hints | PENDING | 0 | |
| 04-claim-pinning-docs | PENDING | 0 | |
| 05-routing-telemetry | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` — clean.
2. `go test -p 2 -short -count=1 ./internal/queue/... ./internal/worker/... ./internal/agent/... ./internal/task/... ./internal/daemon/...` — all ok.
3. Wake-up path: leaf 01's test proves Enqueue signals a registered waiter
   within milliseconds (no poll interval).
4. Telemetry path: leaf 05's test proves selectAgent chat-fallback and
   handoff publish routing.* events on a test bus.
5. `make graphs` + `make graphs-check` — fresh (new routing.* topics registered).

## Structural Completeness Check

Dispatch Protocol, Interface Contracts, Review Checklist, Coding
Conventions, Completion Tracking Table, and Integration Test Plan are all
present above.

## Notes

- The 2026-10-04 review found four additional bugs (claim-window
  starvation, stale-completion guard, cascade log, 8s backoff cap). They
  are FIXED IN PARALLEL by separate fixer subagents and are NOT in this
  tree's scope. Leaves must not touch: Store.Complete guard, Claim slow
  path window logic, failBlockedDependents log line, Retry backoff
  constants — the fix wave owns those regions. If a leaf's edit collides
  spatially, STOP and report; the orchestrator serializes.
- internal/agent tests take ~60s; run package tests with `-short` and
  targeted `-run` filters during iteration, full package once at review.
