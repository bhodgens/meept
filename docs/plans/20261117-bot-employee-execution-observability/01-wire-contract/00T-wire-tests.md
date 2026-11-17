# Wire contract gate tests - Implementation Leaf

> **For the implementing agent:** Write TESTS ONLY. Do NOT write implementation
> files. Tests must fail RED for missing symbols. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once and stop.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** RED tests for shared payload shape, two topic names, WS decoder
- **Dependencies:** none
- **Estimated Context:** 30K
- **Concurrency Group:** A

## Goal

Author unit gate tests pinning C1's wire shape, the TWO topic names, and C5's
decoder.

## Context

- `internal/agent/coordination.go:42-56` - TurnStateEvent + WSClass precedent
- `internal/comm/http/server_wsclass.go:23-33` - decodeTurnStateWSClass precedent
- `internal/agent/topics.go:14` - topic var precedent

## Verified Anchors

- `internal/agent/coordination.go:42` - `type TurnStateEvent struct`
- `internal/agent/topics.go:14` - `var TopicTurnTerminal`
- `internal/comm/http/server_wsclass.go:23` - `func decodeTurnStateWSClass`

## Interface Contracts (From Parent)

### What This Leaf Exposes

- `internal/agent/execution_events_test.go` (F1)
- `internal/comm/http/server_wsclass_test.go` addition (F2)

### What This Leaf Consumes

- C1 shared `ExecutionResultEvent` + `TopicBotResult` / `TopicEmployeeResult` (owner: 01)
- C5 decoder registration (owner: 02)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Pin JSON shape | Create: `internal/agent/execution_events_test.go` | `go test -p 2 ./internal/agent/ -run TestExecutionResultEvent_JSONShape -count=1` | ExecutionResultEvent undefined | marshals to exact tags | Given an event; When json.Marshal; Then keys are agent_id/status/output/tokens_used/error/duration_ms/stop_reason/trigger_ref/session_id/timestamp, with NO kind key |
| T2 | Pin two topic names | Add to same file | `go test -p 2 ./internal/agent/ -run TestResultTopics_Names -count=1` | topic vars undefined | names exact | Given TopicBotResult/TopicEmployeeResult; When .Name read; Then "bot.result" and "employee.result" respectively |
| T3 | Pin WS class + decoder | Modify: `internal/comm/http/server_wsclass_test.go` | `go test -p 2 ./internal/comm/http/ -run TestDecodeExecutionResultWSClass -count=1` | decoder undefined | decodes + WSProgress | Given raw JSON; When decoder runs; Then result WSClass()==WSProgress |

T1 worked shape:
1. Write a table test constructing
   `agent.ExecutionResultEvent{Status:"completed", StopReason: agent.StopReasonEndTurn}`
   and comparing `json.Marshal` output keys. Assert NO `kind` key exists.
2. Run the verify command. RED: `undefined: agent.ExecutionResultEvent`.
3. Do not implement.

Gate tests are RED by design. Do not weaken assertions.

## Self-Verification Checklist

- [ ] Tests assert frozen tags, both topic names, WS class, no kind field
- [ ] No implementation files written
- [ ] No line-number corruption

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines of output
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Assertions pin exact tags and both topic names from C1
- [ ] Test explicitly rejects a `kind` key
- [ ] No implementation files changed; no gate test weakened

Output: APPROVED or specific gaps with file + line references.

## Notes

None.
