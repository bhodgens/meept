# ACP Emit Branch - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 3 (1 test gate + 2 implementation leaves)
- **Scope:** Emit turn state + stop reasons from the delegate and pair surfaces (02-01) and from the ACP bridge (02-02: stopReason plumbing + plan/thought/commands updates).

## Goal

Make the producing surfaces speak the vocabulary from branch 01: delegate children emit `agent.turn.state` transitions on spawn/settle and carry a `StopReason` on `DelegateResult`; the ACP bridge stops discarding `stopReason` and forwards the remaining session/update variants (plan, agent_thought_chunk, available_commands_update). Pair phase transitions publish the same TurnStateEvent.

## Architecture

- 02-01 owns: `internal/agent/delegate.go` (DelegateResult + resultFromRecord + settle/publishResult), `internal/agent/pair_orchestrator.go` (phase transitions), and the new emitter helper `internal/agent/turnstate.go` (a small `publishTurnState` helper shared by delegate + pair; 02-02 calls it too).
- 02-02 owns: `internal/acp/session.go` (Prompt/Cancel/onNotice) and `internal/acp/protocol.go` (AgentProgressUpdate type).
- FILE SERIALIZATION: 02-01 and 02-02 both touch `internal/acp/session.go` — 02-01 only adds a `publishTurnState` call at the Prompt boundary; 02-02 rewrites `onNotice` and `Prompt`. 02-02 dispatches AFTER 02-01 is REVIEWED and committed. They are NOT parallel.
- All new emissions are best-effort (`bus.PublishT`, non-blocking) except where the leaf says otherwise.

## Interface Contracts

### C1 (from 01-01): TurnState, StopReason, TurnStateEvent, TopicTurnState

Consumed as-is. `turnstate.go` (02-01) is the only new shared helper:

```go
// Owner: 02-01
// internal/agent/turnstate.go
func publishTurnState(bus *bus.MessageBus, ev TurnStateEvent) // sets Timestamp if zero, PublishT(TopicTurnState, "agent", ev)
// Consumers: 02-01 (delegate, pair), 02-02 (ACP)
```

### C2 (from master): DelegateResult gains StopReason

```go
// Owner: 02-01 (delegate.go)
type DelegateResult struct {
    AgentID    string     `json:"agent_id"`
    Success    bool       `json:"success"`
    Response   string     `json:"response,omitempty"`
    Error      string     `json:"error,omitempty"`
    ChildID    string     `json:"child_id,omitempty"`
    Status     string     `json:"status,omitempty"`
    StopReason StopReason `json:"stop_reason,omitempty"` // NEW
}
```

### C3 (from master): ACP turn result carries stopReason

```go
// Owner: 02-02 (session.go)
// Session.Send returns (output string, stopReason StopReason, err error).
// SessionEvent gains Kind "turn_end" carrying the wire stopReason text.
```

### C5 (from master): AgentProgressUpdate

```go
// Owner: 02-02 (protocol.go)
type AgentProgressUpdate struct {
    SessionUpdate string          `json:"sessionUpdate"`
    MessageID     string          `json:"messageId,omitempty"`
    PlanID        string          `json:"planId,omitempty"`
    Content       json.RawMessage `json:"content,omitempty"`
    Title         string          `json:"title,omitempty"`
}
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 00T | [00T-unit-tests.md](00T-unit-tests.md) | leaf (test gate) | 01-01 | 35K | A |
| 01 | [01-delegate-pair-emit.md](01-delegate-pair-emit.md) | leaf | 00T, 01-01 | 60K | A |
| 02 | [02-acp-updates.md](02-acp-updates.md) | leaf | 01 (REVIEWED), 01-01 | 55K | B |

02-02 is concurrency group B: it dispatches only after 02-01 is REVIEWED (shared file session.go).

## Dispatch Protocol

### Phase 0: Test Gate

Dispatch 00T-unit-tests.md. Validate in-session; commit; set both impl leaves TESTS_VALIDATED.

### Phase 1: Dispatch 02-01

Full brief: leaf text + C1/C2 contracts inlined + conventions + no-commit rules + file-serialization note (02-01 owns Prompt() boundary call in session.go but must NOT touch onNotice — that is 02-02's).

### Phase 2: Review 02-01

In-session review. Pass: commit. Gaps: re-dispatch.

### Phase 3: Dispatch 02-02

Only after 02-01 committed. Full brief: leaf text + C1/C3/C5 contracts + explicit "02-01 already landed the Prompt() turn-state call; do not duplicate it; you own onNotice and the stopReason plumbing".

### Phase 4: Review + Integration

Review 02-02; run branch suite; set COMPLETE; report to parent.

## Review Checklist

- [ ] C1 helper `publishTurnState` exists and is used by both leaves
- [ ] DelegateResult has StopReason; resultFromRecord maps correctly
- [ ] ACP Prompt no longer discards stopReason; onNotice handles plan/thought/commands
- [ ] Gate tests pass; gate-test diff empty
- [ ] `go build ./internal/agent/... ./internal/acp/...` clean
- [ ] No scope creep; no line-number corruption

## Coding Conventions

- Go; snake_case JSON; stdlib testing; `go fmt`; orchestrator commits only
- New emissions use `bus.PublishT` (non-blocking) via the 02-01 helper

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 00T-unit-tests | PENDING | 0 | |
| 01-delegate-pair-emit | PENDING | 0 | |
| 02-acp-updates | PENDING | 0 | |

## Integration Test Plan

1. `go test ./internal/agent/ -run 'TurnState|DelegateResult|ResultFromRecord|Pair' -v`
2. `go test ./internal/acp/ -run 'StopReason|SessionUpdate|OnNotice' -v`
3. `go build ./internal/agent/... ./internal/acp/...`

## Test Gate

Test leaves: 00T-unit-tests.md - dispatched Phase 0.

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Delegate emits turn state running→idle on settle | 02-01 T2 | TestDelegate_EmitsTurnState | (root 00T) | PENDING |
| F2 | DelegateResult.StopReason mapping table | 02-01 T3 | TestResultFromRecord_StopReason | (root 00T) | PENDING |
| F3 | ACP stopReason plumbed through Session.Send | 02-02 T3 | TestACPPrompt_StopReason | n/a (unit) | PENDING |
| F4 | ACP plan/thought/commands updates emitted | 02-02 T2 | TestACPSession_UpdateKinds | (root 00T) | PENDING |
| F5 | Turn state emitted on ACP boundaries | 02-02 T4 | TestACPTurn_EmitsState | (root 00T) | PENDING |
| F6 | Pair phase transitions emit TurnStateEvent | 02-01 T4 | TestPair_EmitsTurnState | n/a (unit) | PENDING |

Statuses: PENDING | DELTA | VALIDATED
Delta log: [date, feature ID, direction, one-line reason]

## Frozen Documents

Leaves freeze at dispatch; corrections ride dispatch context as dated addenda.

## Open Questions

- Q-02: When a delegate child fails, is idle or requires_action the right terminal state? Rec: requires_action when the failure is a refusal/validation (needs human/lead input); idle otherwise. Impact: 02-01's settle mapping.
- Q-03: ACP session needs a bus for turn-state emission; sessions are built in the manager without one. Rec: nil-safe SetTurnStatePublisher (02-02); daemon wires it (04-01). Impact: sessions without a publisher no-op (byte-identical).

## Notes

- File serialization is the load-bearing constraint: 02-01 then 02-02. Do not parallelize.