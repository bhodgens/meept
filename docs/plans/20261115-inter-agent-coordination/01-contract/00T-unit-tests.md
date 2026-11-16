# Contract Unit Tests - Test Leaf

> **For the implementing agent:** Write tests ONLY. Do NOT write or modify implementation files. Tests must fail RED for missing-implementation reasons. Do NOT commit. Do NOT git add.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Unit tests for the coordination contract (TurnState, StopReason, TurnStateEvent, TopicTurnState) that 01-01 implements.
- **Dependencies:** none (01-01 implements the contract; these tests gate it)
- **Estimated Context:** 30K
- **Concurrency Group:** A
- **Audit references:** F-C1, F-C2, F-C3

## Goal

Write the RED gate tests for `internal/agent/coordination.go`. The tests pin the exact wire strings and the topic name so 01-01 cannot drift.

## Context

- File under test (does not exist yet): `internal/agent/coordination.go`
- Test file to create: `internal/agent/coordination_test.go` (package `agent`)
- The repo uses stdlib `testing` only in `internal/agent` (no testify). Table-driven style: `tests := []struct{...}` + `for _, tc := range tests` + `t.Run`.
- `wsclass.WSClass` is an int enum; `TurnStateEvent.WSClass()` must return `wsclass.WSProgress`.

## Verified Anchors

- `internal/agent/topics.go:14` — `var TopicTurnTerminal = bus.NewTopic[TurnTerminalEvent]("turn.terminal")` — the pattern the new topic mirrors.
- `internal/agent/handler.go:320` — `func (TurnTerminalEvent) WSClass() wsclass.WSClass { return wsclass.WSProgress }` — the marker pattern.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// Test file only. No production symbols.
// Tests reference (from C1, implemented by 01-01):
//   agent.TurnState, agent.TurnStateRunning/Idle/RequiresAction
//   agent.StopReason, agent.StopReasonEndTurn/Cancelled/MaxTokens/Refusal/MaxIterations/Error
//   agent.TurnStateEvent{AgentID, SessionID, ChildID, State, StopReason, Phase, Timestamp}
//   agent.TopicTurnState (bus.Topic[agent.TurnStateEvent])
```

### What This Leaf Consumes

- C1 TurnState/StopReason/TurnStateEvent/TopicTurnState - owning leaf 01-01.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | TurnState/StopReason marshal to exact wire strings | Create: `internal/agent/coordination_test.go` | `go test ./internal/agent/ -run 'TestTurnState_Marshal\|TestStopReason_Marshal' -v` | FAIL: undefined TurnState/StopReason (01-01 not written) | PASS: table asserts "running","idle","requires_action","end_turn","cancelled","max_tokens","refusal","max_iterations","error" | Given the constants from C1; When marshaled to JSON; Then the string values match exactly |
| T2 | TurnStateEvent carries all fields + WSClass returns WSProgress | Modify: `internal/agent/coordination_test.go` | `go test ./internal/agent/ -run 'TestTurnStateEvent_WSClass' -v` | FAIL: undefined TurnStateEvent | PASS: event with all fields set marshals with correct snake_case keys; WSClass() == wsclass.WSProgress | Given a fully-populated TurnStateEvent; When marshaled; Then JSON keys are agent_id/session_id/child_id/state/stop_reason/phase/timestamp; When WSClass() called; Then wsclass.WSProgress |
| T3 | TopicTurnState registered under "agent.turn.state" | Modify: `internal/agent/coordination_test.go` | `go test ./internal/agent/ -run 'TestTopicTurnState_Name' -v` | FAIL: undefined TopicTurnState | PASS: TopicTurnState.Name == "agent.turn.state" | Given TopicTurnState; When .Name read; Then "agent.turn.state" |

**T1 expansion:**
1. Write the table test with all 9 enum values (3 TurnState + 6 StopReason) asserting JSON marshal output.
2. Verify: `go test ./internal/agent/ -run 'TestTurnState_Marshal' -v` — expect FAIL (undefined: TurnState).
3. (01-01 implements; this leaf does NOT implement.)
4. PASS state: all rows green.

**T2 expansion:**
1. Build a TurnStateEvent with AgentID="a", SessionID="s", ChildID="c", State=TurnStateRequiresAction, StopReason=StopReasonRefusal, Phase="reviewer_turn", Timestamp=time.Now().
2. Marshal; assert JSON keys and values (use `json.Marshal` + `strings.Contains` or unmarshal into `map[string]any`).
3. Assert `TurnStateEvent{}.WSClass() == wsclass.WSProgress`.
4. Verify: `go test ./internal/agent/ -run 'TestTurnStateEvent_WSClass' -v` — expect FAIL (undefined).

**T3 expansion:**
1. Assert `TopicTurnState.Name == "agent.turn.state"`.
2. Verify: `go test ./internal/agent/ -run 'TestTopicTurnState_Name' -v` — expect FAIL (undefined).

## Self-Verification Checklist

- [ ] All three test functions written; file compiles ONLY when 01-01's symbols exist (RED until then)
- [ ] Tests assert wire strings and topic name exactly (no tautologies)
- [ ] No implementation files touched
- [ ] `go test ./internal/agent/ -run 'Coordination|...' -v` run once; failures are "undefined" reasons only

**DO NOT COMMIT.** The orchestrator commits gate tests at TESTS_VALIDATED.

## Completion Report Format

1. **Files touched:** [created/modified paths]
2. **Verify command + output:** [last 20 lines]
3. **RED confirmation:** [the undefined-symbol failures observed]
4. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Every task row covered by a test
- [ ] Tests assert behavior (wire strings, topic name, WSClass), not implementation details
- [ ] No tautologies; no implementation code in the test file
- [ ] No line-number corruption

Output: APPROVED or specific gaps.