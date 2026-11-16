# Contract Branch - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 2 (1 test gate leaf + 1 implementation leaf)
- **Scope:** Define the shared inter-agent coordination vocabulary (TurnState, StopReason, TurnStateEvent, TopicTurnState) in one file, `internal/agent/coordination.go`.

## Goal

Every other branch consumes the contract types defined here. This branch is the single source of truth for the vocabulary: TurnState (running/idle/requires_action), StopReason (end_turn/cancelled/max_tokens/refusal/max_iterations/error), the TurnStateEvent payload, and the agent.turn.state topic. No other leaf defines these types.

## Architecture

One new file, `internal/agent/coordination.go`, package `agent`. It imports `internal/bus` (for the typed topic) and `internal/comm/wsclass` (for the WSClass marker). No changes to any existing file except the two registration touch-points in 04-01 (WS relay + graphs), which are explicitly deferred. The file compiles standalone; the topic has no publisher until 02-01/02-02/03-01 land, which is fine — a typed topic with no publisher is not an orphan-check failure until 04-01 wires the subscriber.

## Interface Contracts

### C1: TurnState + StopReason + TurnStateEvent + TopicTurnState

```go
// Owner: 01-01-coordination-contract.md (this branch)
type TurnState string
const (
    TurnStateRunning        TurnState = "running"
    TurnStateIdle           TurnState = "idle"
    TurnStateRequiresAction TurnState = "requires_action"
)

type StopReason string
const (
    StopReasonEndTurn       StopReason = "end_turn"
    StopReasonCancelled     StopReason = "cancelled"
    StopReasonMaxTokens     StopReason = "max_tokens"
    StopReasonRefusal       StopReason = "refusal"
    StopReasonMaxIterations StopReason = "max_iterations"
    StopReasonError         StopReason = "error"
)

type TurnStateEvent struct {
    AgentID    string    `json:"agent_id"`
    SessionID  string    `json:"session_id,omitempty"`
    ChildID    string    `json:"child_id,omitempty"`
    State      TurnState `json:"state"`
    StopReason StopReason `json:"stop_reason,omitempty"`
    Phase      string    `json:"phase,omitempty"`
    Timestamp  time.Time `json:"timestamp"`
}
func (TurnStateEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }

var TopicTurnState = bus.NewTopic[TurnStateEvent]("agent.turn.state")
// Consumers: 02-01, 02-02, 03-01, 04-01
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | [00T-unit-tests.md](00T-unit-tests.md) | leaf (test gate) | none | 30K | A |
| 01 | [01-coordination-contract.md](01-coordination-contract.md) | leaf | 00T | 55K | A |

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch 00T-unit-tests.md. Brief: tests only; fail RED for missing-implementation reasons; do NOT commit. Validate in-session against the Test Gate matrix; commit gate tests; set 01-01 to TESTS_VALIDATED.

### Phase 1: Dispatch

Dispatch 01-coordination-contract.md. Brief: full leaf text + C1 contract INLINED + conventions + "Do NOT commit. Do NOT git add. Do NOT use read_file on existing source files; explore with search_files or terminal cat. Write each file once and stop."

### Phase 2: Review and Commit

Review in-session (main model). Check: contract signatures exact, WSClass marker present, topic declared, tests green, gate-test diff empty. Pass: commit + REVIEWED. Gaps: re-dispatch, max 3 cycles.

### Phase 3: Integration Review

After 01 REVIEWED: run `go build ./internal/agent/...`, `go vet ./internal/agent/...`. Pass: set COMPLETE, report COMPLETE to parent. This branch is the gate for all other branches — do not report COMPLETE until the contract file is committed.

## Review Checklist

- [ ] All tasks from the leaf document are implemented
- [ ] C1 contract satisfied exactly (type names, constants, JSON tags, WSClass, topic name)
- [ ] File at exact path `internal/agent/coordination.go`
- [ ] Gate tests pass GREEN; gate-test diff empty
- [ ] `go build ./internal/agent/...` clean; `go vet` clean
- [ ] No scope creep; no debug artifacts; no line-number corruption

Output: APPROVED or specific gaps.

## Coding Conventions

- Go; exported PascalCase; JSON snake_case; bus topic lowercase dot-separated
- stdlib testing; table-driven; gate test file `coordination_test.go` (package agent)
- `go fmt` before reporting
- Orchestrator commits only

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-unit-tests | PENDING | 0 | |
| 01-coordination-contract | PENDING | 0 | |

## Integration Test Plan

1. `go test ./internal/agent/ -run 'Coordination|TurnState|StopReason' -v` — unit green.
2. `go build ./internal/agent/...` — contract file compiles.
3. Contract check: `grep -c "TurnStateRunning\|StopReasonEndTurn\|TopicTurnState" internal/agent/coordination.go` — 3+ hits.

## Test Gate

Test leaves: 00T-unit-tests.md - dispatched Phase 0, validated in-session.

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F-C1 | TurnState/StopReason marshal to exact wire strings | 01-01 T1 | TestTurnState_Marshal, TestStopReason_Marshal | n/a (vocabulary) | PENDING |
| F-C2 | TurnStateEvent carries all fields + WSClass | 01-01 T2 | TestTurnStateEvent_WSClass | n/a (vocabulary) | PENDING |
| F-C3 | TopicTurnState registered under "agent.turn.state" | 01-01 T3 | TestTopicTurnState_Name | n/a (vocabulary) | PENDING |

Statuses: PENDING | DELTA | VALIDATED
Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as dated addenda.

## Open Questions

- Q-B1: Does the pair driver have a bus handle today? If not, 02-01 adds one via PairOrchestratorDeps (already carries Bus). Impact: none — the deps struct exists (pair_orchestrator.go:39-43).
- Q-B2: Should pair `phase` reuse the existing strings or map to TurnState? Rec: reuse strings in the `phase` field; map to State separately (actor_turn→running).

## Notes

- This branch is the hard dependency for 02 and 03. Do not dispatch anything else until 01-01 is committed.