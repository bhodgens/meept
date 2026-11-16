# Delegate + Pair Turn-State Emitter - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. The gate tests already exist and are committed - make them pass; do NOT delete, skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT git add. Do NOT use read_file on existing source files - explore with search_files or terminal cat. After writing a file, do NOT read it back - write once and stop. Report what you built, files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Add `publishTurnState` helper (internal/agent/turnstate.go); add StopReason to DelegateResult + mapping in resultFromRecord; emit turn state at delegate spawn/settle and pair phase transitions.
- **Dependencies:** 00T-unit-tests (gate tests committed), 01-01 (contract committed)
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **File ownership:** you own `internal/agent/turnstate.go`, `internal/agent/delegate.go`, `internal/agent/pair_orchestrator.go`, AND the Prompt-boundary turn-state call in `internal/acp/session.go`. You must NOT touch `onNotice` in session.go — that belongs to leaf 02-02 which dispatches after you.

## Goal

Every delegate child and pair session publishes `agent.turn.state` events, and every DelegateResult carries a StopReason. This is the producer half of F1/F2/F6.

## Context

- `internal/agent/turnstate.go` — NEW. Helper shared by delegate, pair, and (later) ACP.
- `internal/agent/delegate.go:182-189` — DelegateResult (add StopReason).
- `internal/agent/delegate.go:822-844` — settle (emit idle after publishResult).
- `internal/agent/delegate.go:847-858` — resultFromRecord (map ChildState→StopReason).
- `internal/agent/delegate.go:423-563` — Spawn (emit running after child registered, ~:507).
- `internal/agent/pair_orchestrator.go:199-320` — runPairConversation (emit at each phase transition: :206 actor_turn, :226 reviewer_turn, :269 completed, :243 failed).
- `internal/acp/session.go:296-319` — Prompt (emit running at entry, idle at exit). ONLY this boundary — do not touch onNotice (:376-404).

## Verified Anchors

- `internal/agent/delegate.go:59-72` — ChildState constants (pending/running/completed/interrupted/failed).
- `internal/agent/delegate.go:507` — `State: ChildStateRunning` in Spawn (emit point).
- `internal/agent/delegate.go:822-844` — settle (emit point, outside the mutex).
- `internal/agent/pair_channel.go:54-88` — BusPairSessionState.Phase strings (actor_turn/reviewer_turn/completed/failed).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/turnstate.go
func publishTurnState(b *bus.MessageBus, ev TurnStateEvent) // Timestamp defaults to now; PublishT(TopicTurnState, "agent", ev)

// internal/agent/delegate.go
type DelegateResult struct {
    AgentID    string     `json:"agent_id"`
    Success    bool       `json:"success"`
    Response   string     `json:"response,omitempty"`
    Error      string     `json:"error,omitempty"`
    ChildID    string     `json:"child_id,omitempty"`
    Status     string     `json:"status,omitempty"`
    StopReason StopReason `json:"stop_reason,omitempty"`
}
```

### What This Leaf Consumes

- C1 TurnState/StopReason/TurnStateEvent/TopicTurnState - owning leaf 01-01.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | publishTurnState helper | Create: `internal/agent/turnstate.go` | `go test ./internal/agent/ -run 'TestDelegate_EmitsTurnState' -v` | FAIL: undefined publishTurnState | PASS: helper compiles; test proceeds to emission assertions | Given a bus; When publishTurnState called; Then TopicTurnState receives event with Timestamp set |
| T2 | DelegateResult.StopReason + resultFromRecord mapping | Modify: `internal/agent/delegate.go` | `go test ./internal/agent/ -run 'TestResultFromRecord_StopReason' -v` | FAIL: no StopReason field | PASS: mapping table green | Given ChildRecord; When resultFromRecord; Then StopReason per table |
| T3 | Delegate emits running at spawn + idle at settle | Modify: `internal/agent/delegate.go` | `go test ./internal/agent/ -run 'TestDelegate_EmitsTurnState' -v` | FAIL: no emission | PASS: two TurnStateEvents (running, idle) | Given DelegateManager with bus; When child spawns+settles; Then events observed |
| T4 | Pair phase transitions emit TurnStateEvent | Modify: `internal/agent/pair_orchestrator.go` | `go test ./internal/agent/ -run 'TestPair_EmitsTurnState' -v` | FAIL: no emission | PASS: event per phase | Given PairOrchestrator with bus; When phases advance; Then TurnStateEvent per phase with phase field set |
| T5 | ACP Prompt boundary emits running→idle | Modify: `internal/acp/session.go` (Prompt only) | `go test ./internal/agent/ -run 'TestDelegate_EmitsTurnState' -v` (compile) + `go build ./internal/acp/...` | FAIL: no emission | PASS: Prompt emits running at entry, idle at exit | Given a session with bus; When Send; Then events observed |

**T2 mapping table (implement in resultFromRecord, delegate.go:847):**
```
completed, err==""                       -> StopReasonEndTurn
completed, result contains refusal marker -> StopReasonRefusal   (marker: "I can't help with that" / "I'm unable to")
interrupted                              -> StopReasonCancelled
failed, err contains "max iterations"    -> StopReasonMaxIterations
failed, otherwise                        -> StopReasonError
pending/running                          -> "" (omit)
```

**T3 emit points (outside mutexes):**
- Spawn: after `State: ChildStateRunning` (:507) and after the child entry is added, call `publishTurnState(d.bus, TurnStateEvent{AgentID: req.AgentID, ChildID: childID, State: TurnStateRunning, Phase: "spawn"})`. Guard `if d.bus != nil`.
- settle: after `m.publishResult(rec)` (:838), call with State=TurnStateIdle (or TurnStateRequiresAction when the child failed in a way that needs input — for now: idle for completed/interrupted, requires_action for failed), StopReason=result.StopReason, Phase="settled". Guard bus nil.

**T4 emit points (pair_orchestrator.go):** at each `state.Phase = "X"` transition, also `publishTurnState(po.bus, TurnStateEvent{AgentID: <current agent>, SessionID: sessionID, State: <running|idle>, Phase: "X"})`. Map: actor_turn→running, reviewer_turn→running, completed→idle, failed→idle+StopReasonError. po.bus is `PairOrchestratorDeps.Bus`.

**T5 emit points (session.go Prompt):** needs the session's bus. If `Session` has no bus field, add one via a nil-safe setter `SetTurnStatePublisher(func(TurnStateEvent))` (constructor `NewTurnStatePublisher`); the daemon wiring (04-01) injects it. Emit running at Prompt entry, idle+stopReason at exit. If no publisher wired, no-op.

## Self-Verification Checklist

- [ ] `go build ./internal/agent/... ./internal/acp/...` clean
- [ ] `go vet ./internal/agent/... ./internal/acp/...` clean
- [ ] `go test ./internal/agent/ -run 'TurnState|DelegateResult|ResultFromRecord|Pair' -v` — PASS
- [ ] DelegateResult JSON includes stop_reason when set; omitted when empty
- [ ] Gate tests untouched
- [ ] `go fmt` applied; only the 4 owned files modified

**DO NOT COMMIT.**

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] publishTurnState helper exact per C1
- [ ] DelegateResult.StopReason + mapping table exact
- [ ] Emit points outside mutexes (mutexio gate)
- [ ] Pair phases emit with correct State mapping
- [ ] ACP Prompt emits; onNotice untouched
- [ ] Gate tests untouched; no line-number corruption

Output: APPROVED or specific gaps.