# Session Validation + History Durability - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T1/T2/T3) - make them pass; do NOT delete/skip/weaken
> them. Do NOT commit. Do NOT git add. Explore with search_files/terminal
> cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Validated session identity; durable conversation-only history; tool-call read surface
- **Dependencies:** 00T-gate.md
- **Estimated Context:** 80K
- **Concurrency Group:** A
- **Audit references:** W11; C1/C4/C5 report axis-1; F-ORCH-22/29; A1/A7

## Goal

Reject unknown ids at submit (never fabricate phantom sessions); cross-check
both-ids pairs; upsert a sessions row before persistExchange so conversation-
only history survives; expose tool calls via include_tool_calls.

## Context

- internal/rpc/chat_submit.go:149-159 - conv resolution; :219-225 ack shape.
- internal/agent/handler.go:824-834 - twin resolution; :1543 persistExchange
  (FK 787 on missing sessions row); sites 1412/2921/3407.
- internal/session/store_sqlite.go - sessions table + session_messages FK;
  SetDetectionContext precedent; store.go:12-27 Message struct;
  :134 GetToolCallsForMessages; session.go:2124-2148 handleGetMessages.
- internal/session/thread_resolve.go:95 - thread unwrap for id validation.

## Verified Anchors

- internal/rpc/chat_submit.go:149 - resolution branch
- internal/agent/handler.go:1543 - persistExchange
- internal/session/session.go:2124 - handleGetMessages
- internal/session/store.go:134 - GetToolCallsForMessages

## Interface Contracts (From Parent)

### What This Leaf Exposes
- C4: submit validation (accepted:false + note on unknown id; both-ids
  membership check); conv-only sessions row upsert before persistence;
  session.messages.get include_tool_calls bool -> Message.tool_calls
  []{id,name,args_summary,result_summary,at} (additive).

### What This Leaf Consumes
- C1 (reply_source for synthesized history rows, if any).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Submit validation | Modify: internal/rpc/chat_submit.go; internal/agent/handler.go resolution twin | `go test ./internal/rpc/ -run 'TestUnknownIDRejected|TestBothIDMismatch' -count=1` | gate T1 RED | GREEN | Given unknown id; Then accepted:false note "unknown session - create one first"; thread ids unwrapped before validation; mismatched pair corrected to owning conv or rejected |
| T2 | Sessions-row upsert | Modify: internal/agent/handler.go persistExchange; internal/session store (UpsertByConversationID) | `go test ./internal/agent/ -run TestConvOnlyHistoryRoundtrip -count=1` | gate T2 RED (FK 787) | GREEN | Given conv-only submit; Then row upserted idempotently before insert; concurrent submits race-safe (one row); history persists |
| T3 | include_tool_calls | Modify: internal/session/session.go handleGetMessages; store.go Message wire (additive); rpc proxy param passthrough | `go test ./internal/session/ -run TestIncludeToolCalls -count=1` | gate T3 RED | GREEN | Given tool calls persisted; When include_tool_calls=true; Then joined rows; default false byte-identical legacy shape |
| T4 | Resume contract | Modify: internal/session (most-recent + boot-id stamp on tasks if needed for C6 report) | `go test ./internal/session/ -run TestResumeContract -count=1` (add) | stale non-terminal tasks unmarked | GREEN | Given daemon restart; Then stale non-terminal tasks carry interrupted marker (boot id), resume API returns last session + pending state |

## Self-Verification Checklist

- [ ] Gate T1/T2/T3 GREEN; gate diff empty
- [ ] C4 exact; legacy messages.get byte-identical by default
- [ ] Live-gate: rig conv-only turn -> restart -> messages present; unknown id rejected
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Upsert never widens an existing session's project binding
- [ ] Validation never rejects legitimate thread ids
- [ ] tool_calls summaries bounded; no secret leakage (sanitizer precedent)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- Client restore-policy alignment (TUI vs GUI) is 05-navigation/03; T4
  provides the server contract it consumes.
