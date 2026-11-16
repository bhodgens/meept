# ACP Emit Unit Tests - Test Leaf

> **For the implementing agent:** Write tests ONLY. Do NOT write or modify implementation files. Tests must fail RED for missing-implementation reasons. Do NOT commit. Do NOT git add.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Unit tests for the delegate/pair emitters (02-01) and the ACP updates (02-02). Gates both implementation leaves.
- **Dependencies:** 01-01 (contract committed)
- **Estimated Context:** 35K
- **Concurrency Group:** A

## Goal

Write the RED gate tests for F1-F6 in the parent Test Gate matrix.

## Context

- Test files: `internal/agent/turnstate_emit_test.go` (delegate/pair), `internal/acp/session_updates_test.go` (ACP).
- stdlib testing; table-driven; no testify.
- The 02-01 helper `publishTurnState` and `DelegateResult.StopReason` do not exist yet; `Session.Send` still returns 2 values; `onNotice` handles 2 kinds. Tests reference the target API and fail to compile until implemented — that is the RED state.

## Verified Anchors

- `internal/agent/delegate.go:822-874` — settle + publishResult (payload map).
- `internal/agent/delegate.go:847` — resultFromRecord.
- `internal/agent/pair_orchestrator.go:199` — runPairConversation (phase transitions at :206, :226, :238, :269).
- `internal/acp/session.go:296-319` — Prompt (stopReason discarded).
- `internal/acp/session.go:392-403` — onNotice (2 kinds).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// Test files only. Reference (from contracts):
//   agent.TurnStateEvent, agent.TopicTurnState, agent.TurnState*, agent.StopReason*
//   agent.DelegateResult{... StopReason StopReason}
//   agent.publishTurnState (02-01 helper)
//   acp.Session.Send(ctx, text) (string, agent.StopReason, error)  // signature change
//   acp.AgentProgressUpdate
```

### What This Leaf Consumes

- C1 (01-01), C2/C3/C5 (parent 02-acp-emit).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | DelegateResult carries StopReason + resultFromRecord mapping | Create: `internal/agent/turnstate_emit_test.go` | `go test ./internal/agent/ -run 'TestResultFromRecord_StopReason' -v` | FAIL: DelegateResult has no StopReason field | PASS: table maps completed→end_turn, interrupted→cancelled, failed→error, refusal marker→refusal, max_iterations→max_iterations | Given a ChildRecord; When resultFromRecord; Then StopReason matches table |
| T2 | Delegate settle emits turn state | Modify: `internal/agent/turnstate_emit_test.go` | `go test ./internal/agent/ -run 'TestDelegate_EmitsTurnState' -v` | FAIL: no publishTurnState / no emission | PASS: spawn publishes running; settle publishes idle (or requires_action when failed needs input) | Given a DelegateManager with a bus; When a child settles; Then TopicTurnState received with state idle + stop_reason |
| T3 | Pair phase transitions emit TurnStateEvent | Modify: `internal/agent/turnstate_emit_test.go` | `go test ./internal/agent/ -run 'TestPair_EmitsTurnState' -v` | FAIL: no emission | PASS: actor_turn→running, reviewer_turn→running, completed→idle, failed→idle+error | Given a PairOrchestrator with a bus; When phases advance; Then TurnStateEvent per phase |
| T4 | ACP Session.Send returns stopReason | Create: `internal/acp/session_updates_test.go` | `go test ./internal/acp/ -run 'TestACPPrompt_StopReason' -v` | FAIL: Send returns 2 values | PASS: fake transport returns stopReason "end_turn"; Send returns it | Given a fake ACP transport replying {"stopReason":"end_turn"}; When Send; Then stopReason==end_turn |
| T5 | onNotice handles plan/thought/commands | Modify: `internal/acp/session_updates_test.go` | `go test ./internal/acp/ -run 'TestACPSession_UpdateKinds' -v` | FAIL: only 2 kinds handled | PASS: session/update with sessionUpdate=plan emits SessionEvent{Kind:"plan"}; agent_thought_chunk→"thought"; available_commands_update→"commands" | Given a session with a scripted notification; When onNotice; Then event kind matches |
| T6 | ACP turn emits TurnStateEvent at boundaries | Modify: `internal/acp/session_updates_test.go` | `go test ./internal/acp/ -run 'TestACPTurn_EmitsState' -v` | FAIL: no emission | PASS: prompt start→running, end→idle+stopReason | Given a session with a bus; When Send completes; Then TurnStateEvent running then idle |

**T1 expansion:** table of {record ChildRecord, want StopReason}. Include: completed+no err→end_turn; completed+refusal marker in result→refusal; interrupted→cancelled; failed→error; failed+max iterations marker→max_iterations.

**T2 expansion:** use a fake bus (capture published messages); spawn a child with a stub run func that returns immediately; assert two TurnStateEvents (running at spawn, idle at settle).

**T4 expansion:** fake transport via the existing `testdata/fakeagent` or a stub `tr` implementation; the fake's Call returns `{"stopReason":"end_turn"}`.

## Self-Verification Checklist

- [ ] All 6 test funcs written; RED (compile fail or assertion fail) until implemented
- [ ] Tests assert behavior (events, mappings), not implementation
- [ ] No implementation files touched

**DO NOT COMMIT.**

## Completion Report Format

1. **Files touched:** [paths]
2. **Verify command + output:** [last 20 lines]
3. **RED confirmation:** [failures observed]
4. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Every F1-F6 row has a test
- [ ] Tests fail RED for missing-implementation reasons only
- [ ] No implementation code in test files

Output: APPROVED or gaps.