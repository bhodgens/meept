# 04-CLUTTER Test Gate - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Write tests ONLY for this branch's two
> implementation leaves. No implementation code. Every test fails RED for a
> missing-implementation reason. Do NOT commit. Do NOT git add. Explore
> with search_files/terminal cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Gate suite for F11a/F11b (branch Test Gate matrix)
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Audit references:** W3/W15/W18; F-ORCH-4/5/6/15/21/25; notes-server 2.4

## Goal

Pin reply hygiene BEFORE implementation: empty-reply synthesis, envelope
stripping on the relay path, review feedback kept out of chat, side-job
completion noise gated, trivial fast lane, double-render dedupe in both
clients.

## Context

- internal/agent/handler.go:2248-2294 formatTaskCompletedMessage;
  bestSummaryResult ~2481-2501 (envelope strip exists but misses the
  {"status","accomplished","not_done","issues","observations"} shape);
  route_to_agent completion ships reply="" when loop has no report.
- internal/agent/loop.go RunOnceWithParts ~3321 - finalResponse empty after
  tool-only turn; applyReplyGuard region ~3820-3912.
- internal/agent/handler.go - "## review: approved" chat push (review
  feedback relay); orchestrator.go "failed to find step for job" ERROR on
  memory-capture side jobs (empty step_id).
- internal/agent/dispatcher.go:1559 isShortSimpleMessage; :4971 definition.
- internal/tui/models/chat.go:1505-1523 ChatTaskResultMsg card render;
  chat_turn.go:455-460 SuppressTaskCompletedFor (pending-only gate).
- ui/flutter_ui/lib/providers/chat_provider.dart:715-757
  _lastAssistantContentEquals (last-bubble-only dedupe).
- Frozen rig evidence: probes/p6-positive-control.json turn1 (raw envelope),
  probes/p1-naive-continuity.json turn1 (empty reply), probes/tui-2plus2.json
  (double render).

## Verified Anchors

- internal/agent/handler.go:2248 - formatTaskCompletedMessage
- internal/agent/handler.go:2481 - bestSummaryResult
- internal/agent/dispatcher.go:1559 - isShortSimpleMessage call
- internal/tui/models/chat.go:1505 - ChatTaskResultMsg case
- ui/flutter_ui/lib/providers/chat_provider.dart:749 - _lastAssistantContentEquals

## Interface Contracts (From Parent)

### What This Leaf Exposes
Gate tests per branch matrix; frozen reply fixtures from rig probes.

### What This Leaf Consumes
- C1 (reply_source, task_relay marking for dedupe).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Empty-reply synthesis pin | Create: internal/agent/handler_empty_reply_test.go | `go test ./internal/agent/ -run TestEmptyReplySynthesis -count=1` | completed reply="" today | GREEN after 01 | Given tool-only turn (file_write succeeded); When completion; Then reply non-empty, names written path from tool ledger; never canned when evidence exists |
| T2 | Envelope strip pin | Create: internal/agent/relay_strip_test.go | `go test ./internal/agent/ -run TestRelayEnvelopeStrip -count=1` | raw envelope in relay today | GREEN after 01 | Given step result {"status","accomplished",...}; When relay formats; Then prose recovered, no raw JSON keys in reply |
| T3 | Review feedback routing pin | Create: internal/agent/review_chat_test.go | `go test ./internal/agent/ -run TestReviewFeedbackNotChat -count=1` | "## review:" pushed to chat today | GREEN after 01 | Given review approved/rejected; Then feedback lands in task detail/event only, NOT chat.response |
| T4 | Side-job noise pin | Create: internal/agent/orchestrator_sidejob_test.go | `go test ./internal/agent/ -run TestSideJobCompletionQuiet -count=1` | ERROR spam today | GREEN after 01 | Given memory-capture job (empty step_id) completes; Then no ERROR log, no chat event; real step failures still loud |
| T5 | Trivial fast lane pin | Create: internal/agent/dispatcher_trivial_test.go | `go test ./internal/agent/ -run TestTrivialFastLane -count=1` | 2+2 takes quickplan today | GREEN after 02 | Given "What is 2+2? Answer briefly."; When classify; Then direct chat lane, no task, one brief reply; artifact verbs still route to tasks; config off = legacy |
| T6 | TUI dedupe pin | Create: internal/tui/models/chat_dedupe_test.go | `go test ./internal/tui/ -run TestTUITaskCardDedupe -count=1` | card + bubble both render | GREEN after 02 | Given task_completed_relay turn; Then ONE render (card or bubble, not both); late terminal after stalled still renders |
| T7 | GUI dedupe pin | Create: ui/flutter_ui/test/providers/chat_dedupe_test.dart | `cd ui/flutter_ui && flutter test test/providers/chat_dedupe_test.dart` | last-bubble-only dedupe fragile | GREEN after 02 | Given chat_message then turn.terminal with interleaved system message; Then reply rendered once (dedupe by turn/task id, not last-content) |
| T8 | e2e extensions | Modify: e2e/suites/output-filters/, naive-user-chat/; e2e/manifest.json | `go test -tags e2e ./e2e/suites/output-filters/... -count=1` | scenarios absent | GREEN after branch | Given harness fake-LLM; When tool-only + trivial + relay turns; Then hygiene assertions hold |

## Self-Verification Checklist

- [ ] Every test RED for documented reason (record output)
- [ ] No implementation code; no existing tests weakened
- [ ] manifest rows complete; matrix F11a/F11b covered

**DO NOT COMMIT.** Orchestrator handles git after review.

## Completion Report Format

1. Files touched. 2. RED output per task. 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Tests-only; RED reasons = current defect
- [ ] Fixtures verbatim from rig probes (no rewording)
- [ ] Dedupe pins cover late-terminal-after-stalled (no regression)
- [ ] No artifacts / corruption

Output: APPROVED or gaps with file+line.

## Notes

- T7 is Dart - keep the leaf single-language per dispatch: orchestrator may
  split T7 into the GUI half of 02's brief (pinned contract: dedupe key =
  turn_id/task_id from C1).
