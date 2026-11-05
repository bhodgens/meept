# Restore With Tool Calls - Implementation Leaf

> **For the implementing agent:** implement, verify, report. Do NOT
> commit. Do NOT git add.

## Meta

- **Parent:** ../master.md
- **Scope:** buildConversationRestoreFn (loop.go:9328-9377) rebuilds
  ChatMessages role+content only, dropping ToolCalls. Re-attach tool
  calls on restore (reader side; the writer landed in 65ad62b1).
- **Dependencies:** none (writer + reader columns exist).
- **Estimated Context:** 40K

## Goal

A session restored after restart loses its tool calls - the model sees
assistant replies with no tool-call record. Join GetToolCallsForMessages
by message_id and re-attach ToolCalls to the assistant ChatMessages
during restore.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | In buildConversationRestoreFn's rebuild loop: for assistant messages, attach ToolCalls from GetToolCallsForMessages results (keyed message_id, seq order). Match the ChatMessage.ToolCalls shape (llm.ToolCalls: id/name/args). Failure to fetch = proceed without (best-effort, Warn). | Modify: internal/agent/loop.go (restore fn only) | `go test -p 2 ./internal/agent/ -run Restore` | restored assistant msg has no ToolCalls | restored msg carries its tool calls | Given a persisted exchange w/ tool calls; When session restored; Then assistant msg ToolCalls non-empty |
| T2 | Proof: save exchange w/ tool calls (SaveMessagesWithToolCalls) -> restore -> assert ToolCalls on the assistant message AND the model sees the tool-call record in the rebuilt context (fake-LLM request contains the tool result echo). Drop the attach -> restore loses calls = FAIL. | Modify: new/existing loop restore test | same | unproven | proven | Given attach removed; When restored; Then ToolCalls empty = FAIL |
| T3 | boundary-hardening e2e spot (restore path touched): suite still green. | - | `go test -p 2 -tags e2e ./e2e/suites/boundary-hardening/` | suite red = regression | green | - |

## Self-Verification Checklist

- [ ] go build ./... green (scoped agent build w/ sibling RED gate mv-aside)
- [ ] restore tests green
- [ ] boundary-hardening e2e green
- [ ] gofmt clean, ASCII only
- [ ] best-effort fetch (Warn, never fail restore)

## Review Checklist (orchestrator)

- [ ] Restore fn only (no writer changes)
- [ ] ToolCalls shape matches llm.ToolCalls
- [ ] Best-effort pinned
- [ ] RED witnessed for T2

## Do NOT commit. The orchestrator stages and commits after review.
