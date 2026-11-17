# Integration, Docs, and Topology - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Dispatch implementation agents, review, track completion. Do NOT implement
> code yourself. Full detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 leaves (test gate, wiring, docs/graphs)
- **Scope:** Wire buses at daemon startup, regenerate topology, update docs

## Goal

After branches 01-03 land, connect their `SetBus` seams to `c.msgBus` in
`NewComponents`, regenerate the connectivity graphs, and update the two docs
that currently say bots are dead.

## Architecture

`internal/daemon/components.go` is the only file in this tree allowed to call
`SetBus` on the runners and goal loops. It already has `c.msgBus` in scope.
Wiring is nil-safe. The generator `scripts/gen-connectivity-graph.py` reads bus
topics from source; `make graphs` regenerates `docs/generated/*` and
`make graphs-check` verifies freshness. Docs: `docs/workflows/bots.md` (currently
says deprecated, commands removed) and `docs/workflows/inter-agent-coordination.md`
(add both tiers to the convergence list).

## Interface Contracts

### Wiring points

```
bot.Manager.SetBus(c.msgBus)        // at components.go:5350 construction
each GoalLoop.SetBus(c.msgBus)      // at goal-loop construction in components.go
```
No new interface types. Both seams are C3 (owner: branch 02).

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | 00T-wiring-tests.md | leaf (test) | none | 30K | A |
| 01 | 01-daemon-wiring.md | leaf | 00T, branches 01-03 | 40K | A |
| 02 | 02-docs-and-graphs.md | leaf | 01 | 35K | B |

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch `00T-wiring-tests.md`. Validate RED against F7. Commit; set TESTS_VALIDATED.

### Phase 1-2: Wiring then Docs

Dispatch 01 after all prior branches REVIEWED. Then 02 after 01 REVIEWED.
Only these leaves edit `components.go` and `docs/generated/*`.

### Phase 3: Full Integration

Run all commands in master Integration Test Plan. Set COMPLETE.

## Review Checklist

- [ ] SetBus wired for bot manager and every goal loop
- [ ] `make graphs-check` green; BOTH `bot.result` and `employee.result` in topology JSON/MD
- [ ] Docs state both tiers are supported and emit on the same surface
- [ ] AGENTS.md package table unchanged (no new package)

## Coding Conventions

- Go 1.27; gofmt; nil-safe wiring
- Docs use lowercase UI text; no new generated-file edits by hand
- Leaf agents do NOT commit and do NOT run git add

## Requirement Traceability

| Req | Source | Plan element | Verified by |
|-----|--------|--------------|-------------|
| R3 | WS/topology consumable | 01 + 02 | graphs-check + e2e |
| R6 | e2e hermetic | 01 + 02 | e2e suite green |

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-wiring-tests | PENDING | 0 | |
| 01-daemon-wiring | PENDING | 0 | |
| 02-docs-and-graphs | PENDING | 0 | |

## Integration Test Plan

`make test`, `make e2e-fast`, `make graphs && make graphs-check`, `make lint-ci`.

## Test Gate

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F7 | Daemon wires bus onto emitters | 01-daemon-wiring.md | TestComponents_WiresExecutionBus | all e2e scenarios | PENDING |
| F8 | Topology + docs freshness | 02-docs-and-graphs.md | n/a | `make graphs-check` | PENDING |

Delta log: none yet.

## Open Questions

- Resolved: operator confirmed separate result topics. Topology and docs must
  show both `bot.result` and `employee.result`; no `kind` discriminator.

## Notes

This branch is the only one that may touch `internal/daemon/components.go`
besides branch 02 leaf 03 (trigger handler). Coordinate: leaf 03 edits the
router line (:5349); this branch's 01 edits the SetBus line near it. Serialize
if both are in flight; branch 04 runs after branch 02 is REVIEWED.

