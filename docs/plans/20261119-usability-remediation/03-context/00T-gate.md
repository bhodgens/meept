# 03-CONTEXT Test Gate - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Write tests ONLY for this branch's two
> implementation leaves. No implementation code. Every test fails RED for a
> missing-implementation reason. Do NOT commit. Do NOT git add. Explore
> with search_files/terminal cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Gate suite for F9-F10 (branch Test Gate matrix)
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Audit references:** W10/W11; F-ORCH-16/22/24/29; session-steering A1/A7

## Goal

Pin session identity validation, conversation-only history durability,
tool-call read surface, and recall fidelity BEFORE implementation.

## Context

- internal/rpc/chat_submit.go:149-159 - resolves conv id only when empty;
  unknown ids accepted, new conv fabricated; no both-ids cross-check.
- internal/agent/handler.go:1543 persistExchange - FK 787 on conv-only
  sessions (no sessions row); call sites 1412/2921/3407.
- internal/session/session.go:2124-2148 handleGetMessages - messages only;
  store.go:12-27 Message has no ToolCalls; store.go:134
  GetToolCallsForMessages exists (engine-side only).
- internal/agent/recall_answer.go:41-92 - last-task-only digest; :86-90
  default branch treats failed as in-progress.
- internal/agent/dispatcher.go buildSessionContextDigestExcluding ~3875.
- Frozen probe facts: p2 codeword ZEBRA-4417 + alpha_notes.txt; p1 hello.txt.

## Verified Anchors

- internal/rpc/chat_submit.go:149 - conv resolution branch
- internal/agent/handler.go:1543 - persistExchange
- internal/session/store.go:134 - GetToolCallsForMessages
- internal/agent/recall_answer.go:86 - state switch default

## Interface Contracts (From Parent)

### What This Leaf Exposes
Gate tests per branch matrix; e2e scenario extensions.

### What This Leaf Consumes
- C4 (../master.md) validation + upsert + include_tool_calls wire shape.
- C1 reply_source key (recall answers tagged).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Submit validation pins | Create: internal/rpc/chat_submit_validate_test.go | `go test ./internal/rpc/ -run 'TestUnknownIDRejected|TestBothIDMismatch' -count=1` | unknown ids accepted today | GREEN after 01 | Given unknown session_id; When submit; Then accepted:false + actionable note, NO conv fabricated; mismatched both-ids corrected/rejected |
| T2 | History roundtrip pin | Create: internal/agent/handler_history_test.go | `go test ./internal/agent/ -run TestConvOnlyHistoryRoundtrip -count=1` | FK 787 today | GREEN after 01 | Given conv-only submit; When turn completes + messages.get; Then user+assistant rows present; concurrent same-conv submits race-safe |
| T3 | Tool-call read pin | Create: internal/session/messages_toolcalls_test.go | `go test ./internal/session/ -run TestIncludeToolCalls -count=1` | messages.get omits tool calls | GREEN after 01 | Given persisted tool calls; When messages.get include_tool_calls=true; Then rows joined by message id; default false byte-identical legacy |
| T4 | Recall state pins | Create: internal/agent/recall_answer_test.go | `go test ./internal/agent/ -run 'TestRecallFailedState|TestRecallTaskSet' -count=1` | failed reads as in-progress; last-task-only | GREEN after 02 | Given failed prior task; Then answer says failed plainly (never "still in progress"); plural artifact question covers the session task set |
| T5 | Recall fact routing pin | Create: internal/agent/recall_fact_test.go | `go test ./internal/agent/ -run TestRecallFactToMemory -count=1` | codeword question decomposes into task_get | GREEN after 02 | Given planted session fact; When "what was the codeword"; Then memory_search lane answers ZEBRA-4417 (no task_get plumbing) |
| T6 | e2e extensions | Modify: e2e/suites/session-storage/, naive-user-chat/; e2e/manifest.json | `go test -tags e2e ./e2e/suites/session-storage/... -count=1` | scenarios absent | GREEN after branch | Given harness stack; When conv-only dialog + recall turns; Then durability+continuity assertions hold |

## Self-Verification Checklist

- [ ] Every test RED for documented reason (record output)
- [ ] No implementation code; no existing tests weakened
- [ ] manifest rows complete; matrix F9-F10 covered

**DO NOT COMMIT.** Orchestrator handles git after review.

## Completion Report Format

1. Files touched. 2. RED output per task. 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Tests-only; RED reasons = current defect/missing implementation
- [ ] C4 wire shape exact; legacy default byte-identical pin present
- [ ] No artifacts / corruption

Output: APPROVED or gaps with file+line.

## Notes

- T2 must reproduce the actual FK 787 error text on current code (mutation
  discipline) before the fix.
