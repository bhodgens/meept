# 01-SAFETY Test Gate - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Write tests ONLY for this branch's five
> implementation leaves. No implementation code. Every test must fail RED
> for a missing-implementation reason (undefined symbol, wrong behavior of
> the CURRENT code). Do NOT commit - the orchestrator handles git. Do NOT
> use read_file on existing source; explore with search_files/terminal cat.
> Write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Full unit + e2e gate suite for F1-F5 (branch Test Gate matrix)
- **Dependencies:** none
- **Estimated Context:** 70K
- **Concurrency Group:** A
- **Audit references:** W1-W7, W24-W27; queue-gates #1/#2/#6/#7/#8/#11;
  audit-routing-failures top-10 #2/#3/#4/#5/#8/#9

## Goal

Pin every safety contract BEFORE implementation: reject honesty, queue
lifecycle, critical delivery, watchdog exemption, failure terminalization.
Tests reference the NEW symbols the impl leaves will create (listed per
task); RED reasons must be "undefined: X" or the documented current wrong
behavior - never a fixture bug.

## Context

- internal/rpc/task_approval.go - TaskApprovalHandler{ApproveFunc} only;
  handleReject calls ApproveFunc (the bug). RejectFunc seam will be added.
- internal/agent/strategic.go:2336 - RejectPlan(ctx, taskID, reason) exists,
  zero callers; publishes task.rejected (orphan topic).
- internal/agent/queue.go:400 - Close one-way CAS; loop.go:3513 re-registers
  same queue; loop.go:10107 ConfigSnapshot shares queue pointer.
- internal/agent/registry.go:2078 - GetActiveQueue exact lookup, no remap.
- internal/rpc/proxy.go:443 - MaxEvents:100 drop-oldest trim.
- internal/tui/events.go:151 - subscribe(); poll errors swallowed (~244).
- internal/agent/turn_watchdog.go - reaps stale tracked turns (600s).
- internal/agent/tactical.go:2479-2503 - OnJobFailed pair branch silent.
- internal/agent/pair_manager.go:354-402 - finalizeTask publishes nothing.
- internal/agent/orchestrator.go:369-375 - handlePlanRequest error log-only.
- internal/plan/manager.go:634-645 - Synthesize error stored, no event.
- internal/agent/handler.go:1427-1432 - park detection requires err!=nil;
  handler.go:749 handleRequest no recover.
- e2e harness: e2e/harness (Stack, fake LLM), manifest.json rows required.

## Verified Anchors

- internal/rpc/task_approval.go:21 - ApproveFunc field (add RejectFunc beside)
- internal/rpc/task_approval.go:69 - handleReject calls h.ApproveFunc (bug site)
- internal/agent/strategic.go:2336 - RejectPlan(ctx, taskID, reason string) error
- internal/agent/queue.go:400-403 - Close CAS
- internal/agent/loop.go:10107 - WithMessageQueue(l.queue) in ConfigSnapshot
- internal/agent/registry.go:2078 - GetActiveQueue(conversationID)
- internal/rpc/proxy.go:443 - MaxEvents: 100
- internal/agent/handler.go:2035 - PublishBlockingT(TopicTurnTerminal)
- internal/daemon/daemon.go:2163 - NewTaskApprovalHandler wiring site

## Interface Contracts (From Parent)

### What This Leaf Exposes
Gate tests named in the branch Test Gate matrix; e2e suites registered in
e2e/manifest.json (suite+scenario+path_map rows). Impl leaves make them GREEN.

### What This Leaf Consumes
- C1/C2/C3/C6/C7 (../master.md) - tests assert these exact shapes.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Reject honesty unit pins | Create: internal/rpc/task_approval_reject_test.go | `go test ./internal/rpc/ -run TestReject -count=1` | undefined: RejectFunc / current reject approves | GREEN after 01 | Given a handler with distinct ApproveFunc/RejectFunc spies; When task.reject{reason}; Then RejectFunc called with reason, ApproveFunc NOT called, response status=rejected |
| T2 | Rejected relay terminal pin | Create: internal/agent/handler_rejected_relay_test.go | `go test ./internal/agent/ -run TestRejectedRelay -count=1` | no task.rejected subscription exists | GREEN after 01 | Given a tracked turn attached to a task; When task.rejected{reason} published; Then turn.terminal status=failed handler_case=task_rejected_relay error=reason under the ORIGINATING turn id |
| T3 | Queue lifecycle pins | Create: internal/agent/queue_lifecycle_test.go | `go test ./internal/agent/ -run 'TestFreshQueuePerTurn|TestCloneQueueIsolation|TestSteerFullText' -count=1` | current code reuses closed queue / shares clone queue / injects Summary | GREEN after 02 | Given a cached loop on turn 2; When steer arrives; Then queue open, full original text injected (not 100-char summary); two session clones have distinct queues |
| T4 | Thread remap pins | Create: internal/agent/registry_remap_test.go | `go test ./internal/agent/ -run TestThreadRemap -count=1` | GetActiveQueue exact-only | GREEN after 02 | Given queue registered under conv-X-thread-work-ab12; When lookup conv-X; Then found; ambiguous two-thread match returns explicit error |
| T5 | Critical delivery pins | Create: internal/rpc/proxy_critical_test.go; internal/bus/critical_test.go | `go test ./internal/rpc/ ./internal/bus/ -run 'TestCriticalRing|TestBlockingPublishers' -count=1` | MaxEvents trims terminals; task.completed non-blocking | GREEN after 03 | Given 300 progress events + 1 turn.terminal buffered; When poll; Then terminal present, dropped counts only progress; task.completed/failed+plan terminal events use PublishBlockingT |
| T6 | TUI reconnect pin | Create: internal/tui/events_resubscribe_test.go | `go test ./internal/tui/ -run TestResubscribe -count=1` | poll error swallowed | GREEN after 03 | Given poll returns "subscription not found"; When next poll cycle; Then subscribe() re-run and events flow |
| T7 | Watchdog exemption pins | Create: internal/agent/turn_watchdog_exempt_test.go | `go test ./internal/agent/ -run 'TestApprovalWaitNotReaped|TestSealActivityTouch' -count=1` | watchdog reaps awaiting_approval | GREEN after 04 | Given tracked turn whose task is awaiting_approval, stale 700s; When watchdog tick; Then NOT reaped; draft/seal activity Touches liveness |
| T8 | Failure terminalization pins | Create: internal/agent/failure_terminal_test.go; internal/plan/manager_fail_event_test.go | `go test ./internal/agent/ ./internal/plan/ -run 'TestPairFailureTerminal|TestPlanRequestErrorTerminal|TestSynthesisFailureEvent|TestParkEmptyReply|TestPanicRecovery' -count=1` | pair branch silent; plan error log-only; synthesis no event; park empty reply; no recover | GREEN after 05 | Given each failure injection; When it fires; Then task.failed/plan.failed published and turn.terminal failed with actionable error (never empty completed, never silent) |
| T9 | Evidence review gate pin | Create: internal/agent/review_evidence_gate_test.go | `go test ./internal/agent/ -run TestFileStepZeroEvidenceGate -count=1` | file-shaped step with zero file tools auto-approves today | GREEN after 06 | Given file-edit step, ledger zero file tools; When ReviewStep; Then forced evidence review, NOT auto-approved; with real file ToolEvidence normal approval preserved |
| T11 | Replan storm ceiling pin | Create: internal/agent/ralph_storm_test.go | `go test ./internal/agent/ -run 'TestReplanHardCeiling|TestFailedTaskNeverReplans|TestFailureRelayedOnce' -count=1` | failed task replans 113x today (extension grants unbounded) | GREEN after 07 | Given task failing evidence checks with growing step count; Then replans bounded by MaxIterations+max_replan_extensions; failTaskAtCap latches; ONE failure terminal (no fresh-turn_id re-relay) |
| T12 | Plans typed-nil panic pin | Create: internal/services/plan_typednil_test.go; internal/daemon/evolver_store_nil_test.go | `go test ./internal/services/ ./internal/daemon/ -run 'TestPlansTypedNil|TestNilSafeEvolverStore' -count=1` | GET /plans panics on typed-nil sink store today | GREEN after 07 | Given evolver sink disabled (nil concrete *SQLiteStore boxed into interface); When List/Get; Then no panic, shared-store plans returned; test through REAL wiring function (AGENTS.md typed-nil rule) |
| T10 | e2e suites + manifest | Create: e2e/suites/task-approval/, bus-delivery/, approval-wait/, failure-paths/ (boots-leg tests); Modify: e2e/manifest.json; extend e2e/suites/steering scenario | `go test -tags e2e ./e2e/suites/task-approval/... -count=1` (RED until impl) | suites assert post-fix behavior | GREEN after branch | Given harness fake-LLM stack; When reject/steer/reconnect/wait/failure scenarios run; Then contract assertions hold; manifest completeness tests pass |

## Self-Verification Checklist

- [ ] Every test RED for the documented reason (run each verify command, record output)
- [ ] No implementation code written; no existing tests weakened
- [ ] manifest.json rows complete (suite+scenario+path_map) for new suites
- [ ] Gate matrix F1-F5 rows all covered
- [ ] No deviations (or documented)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. Files touched. 2. Verify command + RED output per task (last 20 lines).
3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Tests-only diff; RED reasons match missing-implementation (not fixture bugs)
- [ ] Contract shapes exact (C1 keys, C2 RPC, C3 resolution order, C6 topics, C7 states)
- [ ] e2e manifest completeness green; no gate-test edits to EXISTING suites beyond additive scenarios
- [ ] No scope creep / artifacts / line-number corruption

Output: APPROVED or specific gaps with file+line.

## Notes

- Mutation discipline: for each guard pin, verify the test fails on the
  CURRENT defect shape (e.g. reject test must show ApproveFunc called today).
- TestRejectDoesNotApprove is the branch's load-bearing pin - W1 acceptance.
