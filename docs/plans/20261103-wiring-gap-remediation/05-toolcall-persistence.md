# Tool-Call Persistence - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** Restore the runtime writer for session_tool_calls (purged
  in 9edc4c85): persist tool calls keyed to the assistant message.
- **Dependencies:** none. Unlocks the golden-capture first-choice path
  (92fa3350) and future restore-with-tool-calls (read side is a
  documented follow-up, NOT this leaf).
- **Estimated Context:** 70K

## Goal

SaveToolCalls/ChatMessagesToToolCalls (internal/session) have zero
production callers since 9edc4c85. The chat exchange persist path
(persistChatExchange, handler.go:1405-1441) writes user+assistant rows
but cannot reach tool calls: SaveMessages returns no IDs and the loop's
final ChatMessage list (with ToolCalls) is not threaded into the persist.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Store: SaveMessages path returns inserted message IDs (or add SaveMessagesWithToolCalls taking the assistant messages + their tool calls in one transaction). Read store_sqlite.go SaveMessages + SaveToolCalls first; prefer extending SaveMessages' return over a new method if the interface change is small (handler.go:41 messageSaver interface may widen). | Modify: internal/session/store.go, store_sqlite.go | `go test -p 2 ./internal/session/ -run Save` | IDs not returned | roundtrip: save exchange w/ tool calls -> GetToolCallsForMessages returns them keyed by message_id | Given a chat exchange with 2 tool calls; When saved; Then session_tool_calls rows exist with correct message_id + seq |
| T2 | Thread the loop's final messages (with ToolCalls) into persistChatExchange: handler.go persist path gains the message list (the loop reply-finalization point already holds it; find the exact hand-off by reading persistChatExchange callers). Write ChatMessagesToToolCalls output via SaveToolCalls per assistant message. Best-effort: a persistence failure logs Warn and NEVER fails the turn. | Modify: internal/agent/handler.go (persist path), possibly loop.go reply-finalization hand-off | `go test -p 2 ./internal/agent/ -run Persist` (new wiring test) | zero rows for a tool-call turn | turn with tool calls -> rows appear | Given a scripted turn with a tool call; When completed; Then session_tool_calls non-empty |
| T3 | Wiring-failure property: comment the persist call -> e2e/unit proves zero rows; restore -> rows return. Pin in the T2 test. | Modify: internal/agent/handler_test.go or the new test file | same as T2 | - | deletion proven to break | Given the write removed; When turn completes; Then zero session_tool_calls rows |
| T4 | Golden-capture note: docs/workflows/trajectory-grading.md gains one line - the DB-first capture path now activates automatically (FakeLLM echo fallback stays as fallback). | Modify: docs/workflows/trajectory-grading.md | grep shows the note | - | doc notes activation | - |

## Constraints

- Read side OUT of scope: buildConversationRestoreFn dropping ToolCalls
  (loop.go:9328-9377) is a documented follow-up - do not wire it here.
- Best-effort persistence only: never fail a turn on a write error
  (repo chat-path contract).
- No schema changes: session_tool_calls columns already exist
  (trace_id/span_id landed with the tracing tree).

## Self-Verification Checklist

- [ ] go build ./... green
- [ ] session + agent tests green
- [ ] e2e suites touching chat paths still green (spot: task-state)
- [ ] gofmt clean, ASCII only
- [ ] best-effort semantics pinned (write failure != turn failure)

## Review Checklist (orchestrator)

- [ ] SaveMessages return-shape change audited for all callers
- [ ] persistChatExchange hand-off point named in report
- [ ] Wiring-failure proof demonstrated
- [ ] trajectory-grading.md note present

## Do NOT commit. The orchestrator stages and commits after review.
