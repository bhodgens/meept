# Wire Branch - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 1 leaf
- **Scope:** Wire the new turn-state topic into the WS relay + orphan-check baseline, regenerate graphs, write the schema doc, and add the hermetic e2e suite + manifest entry (the root 00T e2e leaf is the test gate for this branch's integration claims).

## Goal

Close the loop: the vocabulary is defined (01), emitted (02, 03), and now observable + documented. This branch wires `agent.turn.state` through the WS relay, regenerates `docs/generated/`, writes `docs/workflows/inter-agent-coordination.md`, and adds the e2e suite entry.

## Architecture

Single leaf 04-01 touches: `internal/comm/http/server.go` (relay subscription list + wsclass switch), `internal/comm/wsclass` (if a new class is needed — it is not; WSProgress covers it), `scripts/orphan-baseline.txt` (add agent.turn.state if orphan-check flags it), `docs/generated/*` (via `make graphs`), `docs/workflows/inter-agent-coordination.md` (new), `e2e/manifest.json` + `e2e/suites/inter-agent-coord/` (new suite; the root 00T leaf writes the tests, 04-01 registers the manifest entry + wires the daemon publisher).

## Interface Contracts

### C6 (from master): agent.turn.state topic

```go
var TopicTurnState = bus.NewTopic[TurnStateEvent]("agent.turn.state")
// 04-01: add "agent.turn.state" to the WS relay subscription list (server.go:722-756);
// add the wsclass case (WSProgress — already covered by TurnStateEvent.WSClass marker);
// regenerate graphs; add to orphan baseline if needed.
```

### C8 (from 02-02): ACP turn-state publisher wiring

```go
// 02-02 added SetTurnStatePublisher on acp.Session (nil-safe).
// 04-01: in internal/daemon (acp wiring or components), after the acp.Manager is built:
//   mgr.Sessions... // for each session the daemon creates, call SetTurnStatePublisher(func(id, state, reason, phase string) {
//       agent.PublishTurnState(...)) // or the daemon-local equivalent that constructs agent.TurnStateEvent + bus.PublishT(TopicTurnState)
// The daemon CAN import agent + employee (no cycle at that layer).
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | [01-ws-docs-graphs.md](01-ws-docs-graphs.md) | leaf | 02, 03 | 50K | A |

## Dispatch Protocol

### Phase 0: Test Gate

Covered by the ROOT 00T e2e leaf (dispatched at the root level). This branch's integration is validated by that suite + the commands below. If the root gate is not yet TESTS_VALIDATED, 04-01 still dispatches (it is wiring, not feature logic) — but its e2e claims stay PENDING until the root suite is green.

### Phase 1: Dispatch 04-01

Full brief + C6/C8 + conventions + no-commit + "run `make graphs` and commit the regenerated files with your report (the orchestrator commits)".

### Phase 2: Review (in-session). Commit.

### Phase 3: Integration — run the full Integration Test Plan; COMPLETE; report.

## Review Checklist

- [ ] agent.turn.state in WS relay subscription list
- [ ] `make graphs` run; docs/generated updated
- [ ] orphan-check passes (baseline updated if needed)
- [ ] docs/workflows/inter-agent-coordination.md exists with the schema
- [ ] e2e manifest entry + suite dir present
- [ ] ACP turn-state publisher wired in daemon
- [ ] Gate tests pass (root suite green); no line-number corruption

## Coding Conventions

- Go; snake_case JSON; `go fmt`; orchestrator commits only

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-ws-docs-graphs | PENDING | 0 | |

## Integration Test Plan

1. `make graphs` then `git diff --exit-code docs/generated/` — topology regenerated.
2. `python3 scripts/orphan-check.py` — green (or baseline updated).
3. `make e2e-fast` — new suite green.
4. `grep -c "agent.turn.state" docs/generated/bus-topology.md` — >= 1.

## Test Gate

Covered by root 00T. This branch adds no new test leaf.

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F9 | agent.turn.state in WS relay + graphs + baseline | 04-01 | n/a (integration) | (root 00T turn-state-observed) | PENDING |
| F10 | Schema doc in docs/workflows/ | 04-01 | n/a | docs gate [16] | PENDING |

Statuses: PENDING | DELTA | VALIDATED
Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaves freeze at dispatch; corrections ride dispatch context as dated addenda.

## Open Questions

- Q-04: Does the e2e suite name `inter-agent-coord` collide with an existing manifest entry? Rec: no (checked — no such suite). Impact: none.
- Q-05: Does `make graphs` require mage + gomarkdoc on PATH? If unavailable, the leaf documents the manual regen command and the orchestrator runs it in the worktree. Impact: 04-01 may need a follow-up.

## Notes

- `make graphs` embeds absolute line offsets; a red graphs-check after unrelated edits means "regenerate", not "topology changed" (per AGENTS.md).