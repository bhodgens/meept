# WS relay registration for both result topics - Implementation Leaf

> **For the implementing agent:** Implement all tasks. Gate tests exist and are
> committed; make them pass. Do NOT edit gate tests. Do NOT commit. Do NOT use
> read_file; explore with search_files/terminal cat. Write once, then verify.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** Register one decoder and TWO WS subscriptions for result topics
- **Dependencies:** 01-result-event-type.md
- **Estimated Context:** 35K
- **Concurrency Group:** A

## Goal

Make BOTH `bot.result` and `employee.result` frames reachable over WS and
classified as `agent_progress` through the typed-payload marker path. Never
`chat_message`.

## Context

- `internal/comm/http/server_wsclass.go` - typedPayloadDecoders map
- `internal/comm/http/server.go:740-805` - WS subscription topic list
- `internal/comm/AGENTS.md` - classification invariant

## Verified Anchors

- `internal/comm/http/server_wsclass.go:85-97` - typedPayloadDecoders map
- `internal/comm/http/server.go:760-765` - `"agent.turn.state"` explicit subscription
- `internal/agent/coordination.go:53` - `TurnStateEvent.WSClass()` precedent

## Interface Contracts (From Parent)

### What This Leaf Exposes

- ONE `decodeExecutionResultWSClass` function registered under BOTH topic names
- `"bot.result"` AND `"employee.result"` in the WS subscription list

### What This Leaf Consumes

- C1 `agent.TopicBotResult`, `agent.TopicEmployeeResult` (owner: 01-result-event-type.md)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|----------------|
| T1 | Add decoder for both topics | Modify: `internal/comm/http/server_wsclass.go`; Test: `internal/comm/http/server_wsclass_test.go` (gate) | `go test -p 2 ./internal/comm/http/ -run TestDecodeExecutionResultWSClass -count=1` | decoder undefined | gate passes | Given raw JSON; When decoder runs; Then WSClass()==WSProgress |
| T2 | Add both subscriptions | Modify: `internal/comm/http/server.go` | `go build ./internal/comm/http/ && grep -nE '"(bot|employee).result"' internal/comm/http/server.go` | subscriptions absent | both entries present | Given WS relay start; When it subscribes; Then both 2-segment result topics are in the pattern list |

T1 steps:
1. Confirm gate RED.
2. Add ONE `decodeExecutionResultWSClass` beside `decodeTurnStateWSClass`,
   decoding `agent.ExecutionResultEvent`.
3. Add TWO map entries:
   `agent.TopicBotResult.Name: decodeExecutionResultWSClass,`
   `agent.TopicEmployeeResult.Name: decodeExecutionResultWSClass,`
4. T2: insert BOTH `"bot.result"` and `"employee.result"` next to
   `"agent.turn.state"` with a short comment naming issue #140 and the
   `agent_progress` classification.
5. `gofmt -w`; run both commands.

## Self-Verification Checklist

- [ ] ONE decoder; TWO map entries; TWO subscriptions
- [ ] No `chat_message` case added; no `exhaustive` switch changes needed
- [ ] `go build ./internal/comm/http/` passes

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify command + last 20 lines
3. Deviations

## Review Checklist (For Review Agent)

- [ ] Classification stays `agent_progress` for both topics
- [ ] Both subscription patterns are exact 2-segment topics
- [ ] No changes to turn.terminal / turn.state / kanban decoders

Output: APPROVED or specific gaps.

## Notes

The single-segment `*` wildcard does not match `bot.result` or `employee.result`
(`matchWildcard` compares segment count), hence the explicit entries.
