# Dispatch Evidence Surface - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T3) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Routing decisions queryable by either id, joined to turns, with input text; HTTP route for clients
- **Dependencies:** 00T-gate.md, 01-veto-and-notice.md (dispatcher.go region serialization)
- **Estimated Context:** 65K
- **Concurrency Group:** B
- **Audit references:** W9; F-ORCH-3; audit-routing R3; observability 1.3/top-10 #7

## Goal

dispatch_log gains turn_id + populated input_summary; session.dispatch_trace
accepts session_id OR conversation_id (dual-key, thread-aware); new HTTP
route GET /api/v1/sessions/{id}/dispatch-trace for the GUI popover (05/02).

## Context

- internal/metrics/store.go:315-328 - dispatch_log schema; RecordDispatch
  ~1092; QueryDispatchLogBySession.
- internal/agent/dispatcher.go:4678-4780 - RecordDispatch call; turn_no
  COUNT(*)+1 racy; req.TurnID available in dispatch context.
- internal/rpc/dispatch_trace.go:14-42 - session_id-only handler.
- internal/session - Get/GetByConversationID dual resolution pattern
  (chat_submit.go:149-156 precedent); thread_resolve.go:95 unwrap.
- internal/comm/http/server.go - route registration; activity_handlers.go
  precedent for read-only session-scoped GET.

## Verified Anchors

- internal/metrics/store.go:315 - dispatch_log CREATE TABLE
- internal/rpc/dispatch_trace.go:20 - registration
- internal/agent/dispatcher.go:4779 - turn_no computation (audit base; re-grep)

## Interface Contracts (From Parent)

### What This Leaf Exposes
- C5: dual-key trace, turn_id + input_summary columns, HTTP route.

### What This Leaf Consumes
- C1 (terminal turn_id source).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Schema + record turn_id/input | Modify: internal/metrics/store.go (idempotent ALTER + INSERT/SELECT sites); internal/agent/dispatcher.go RecordDispatch | `go test ./internal/metrics/ -run TestTraceTurnID -count=1` | gate T3 RED (no column) | GREEN | Given dispatch with req.TurnID; Then row carries turn_id + input_summary (bounded 200 chars); roundtrip pin incl. legacy rows (COALESCE) |
| T2 | Dual-key RPC | Modify: internal/rpc/dispatch_trace.go | `go test ./internal/rpc/ -run TestDualKeyTrace -count=1` | gate T3 RED (primary id returns 0) | GREEN | Given rows under conv id; When query by primary session id or thread id; Then resolved to owning conv, rows returned |
| T3 | HTTP route | Modify: internal/comm/http/server.go + handler file | `go test ./internal/comm/http/ -run TestDispatchTraceRoute -count=1` | route absent | GREEN | Given GET /api/v1/sessions/{id}/dispatch-trace; Then 200 JSON entries; unknown id empty list (not 500) |

## Self-Verification Checklist

- [ ] Gate T3 GREEN; gate diff empty
- [ ] C5 exact; migration idempotent on existing rig DBs
- [ ] Live-gate: rig turn -> trace by session_id returns the turn with input text
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live-gate evidence. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] turn_no race not worsened (turn_id is the join key now)
- [ ] input_summary bounded; never leaks secrets (sanitizer precedent)
- [ ] Dual-key resolution never cross-returns another session's rows
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- GUI popover consuming this route is 05-navigation/02.
