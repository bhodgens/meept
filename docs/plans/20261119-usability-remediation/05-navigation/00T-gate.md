# 05-NAVIGATION Test Gate - Implementation Leaf (TESTS ONLY)

> **For the implementing agent:** Write tests ONLY for this branch's three
> implementation leaves. No implementation code. Every test fails RED for a
> missing-implementation reason. Do NOT commit. Do NOT git add. Explore
> with search_files/terminal cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Gate suite for F12a/F12b/F12c (branch Test Gate matrix)
- **Dependencies:** none
- **Estimated Context:** 65K
- **Concurrency Group:** A
- **Audit references:** W14/W19/W20; N1-N7; observability 2.1/1.1/1.2/1.3

## Goal

Pin reply-to-task correlation, navigation reachability, trace/evidence
surfaces, /plan fix, restore alignment, and queue parked visibility BEFORE
implementation. Go (TUI) and Dart (GUI) tests are authored as two halves
with the C1 field contract pinned in both.

## Context

- internal/tui/models/chat_turn.go:389-396 - DeliverTurnTerminal keeps only
  conv/reply/status/error/duration; turn.go:66-74 turnTerminalMsg no TaskID;
  chat.go:74-87 ChatMessage no TaskID.
- internal/tui/command_handler.go:1632 - /plan calls session.set_mode
  (unregistered daemon-side); dispatcher.go:984-1030 working /plan route.
- internal/tui/events.go:248-251 - dropped counter discarded.
- ui/flutter_ui/lib/providers/chat_provider.dart:719-725 - reply bubble
  built without taskId; models/api_models.dart:399-409 ChatMessage fields.
- ui/flutter_ui/lib/features/tasks/tasks_tab.dart:10,22 - activeTaskProvider
  never written, TasksList without onTaskSelected; tasks_detail.dart:104-125.
- ui/flutter_ui/lib/core/deep_link.dart:112-117 - /activity?session=<id>
  param dropped (no queryParameters reader).
- internal/session/session.go:2124-2148 - messages.get (include_tool_calls
  added by 03-context/01; tests here consume the C4 shape).
- internal/rpc/queue.go:140-146 - queue_status zeros for parked/planning.

## Verified Anchors

- internal/tui/models/chat_turn.go:389 - DeliverTurnTerminal field keep-list
- internal/tui/command_handler.go:1632 - session.set_mode call
- ui/flutter_ui/lib/features/tasks/tasks_tab.dart:10 - activeTaskProvider
- ui/flutter_ui/lib/providers/chat_provider.dart:719 - bubble construction

## Interface Contracts (From Parent)

### What This Leaf Exposes
Gate tests per branch matrix (Go + Dart halves).

### What This Leaf Consumes
- C1 (task_id/intent_type/handler_case/classified_by on terminal),
  C4 (include_tool_calls), C5 (dispatch-trace route), C6 (dropped counters).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | TUI correlation pin (Go) | Create: internal/tui/models/chat_turn_correlation_test.go | `go test ./internal/tui/ -run TestTurnTerminalKeepsTaskID -count=1` | fields dropped today | GREEN after 01 | Given terminal with task_id/intent/handler_case; Then message model keeps them; stale "no task_id" comments removed |
| T2 | TUI nav + /plan pins (Go) | Create: internal/tui/models/chat_nav_test.go; command_handler_plan_test.go | `go test ./internal/tui/ -run 'TestTaskChipNav|TestPlanSlashForwards|TestDroppedNotice' -count=1` | no chip; /plan broken; dropped discarded | GREEN after 01 | Given completed task reply; Then chip navigates to task detail; /plan forwards to chat.submit (dispatcher route); dropped>0 renders notice |
| T3 | GUI correlation pin (Dart) | Create: ui/flutter_ui/test/providers/chat_correlation_test.dart | `cd ui/flutter_ui && flutter test test/providers/chat_correlation_test.dart` | taskId dropped today | GREEN after 02 | Given TurnTerminalEvent.taskId; Then ChatMessage carries taskId/intentType/handlerCase |
| T4 | GUI detail+trace+popover pins (Dart) | Create: ui/flutter_ui/test/features/tasks_detail_test.dart; trace_expand_test.dart; dispatch_popover_test.dart | `flutter test test/features/tasks_detail_test.dart` | detail unreachable; no trace; no popover | GREEN after 02 | Given task selected; Then detail pane shows steps; trace expand renders persisted tool calls; popover fetches dispatch-trace rows |
| T5 | GUI deep-link pin (Dart) | Create: ui/flutter_ui/test/core/deep_link_param_test.dart | `flutter test test/core/deep_link_param_test.dart` | param dropped today | GREEN after 02 | Given meept://session/<id>; Then activity view filtered to that session (queryParameters consumed) |
| T6 | Restore + queue pins (Go+Dart) | Create: internal/tui/models/restore_test.go; ui/flutter_ui/test/providers/restore_test.dart; internal/rpc/queue_status_parked_test.go | `go test ./internal/tui/ ./internal/rpc/ -run 'TestRestorePolicyAligned|TestQueueStatusParked' -count=1` | policies diverge; parked invisible | GREEN after 03 | Given relaunch; Then both clients restore last session (or explicit empty); queue_status reports parked/planning task states |
| T7 | e2e extensions | Modify: e2e/suites/tui-flows/, tui-gui-consistency/; e2e/manifest.json | `go test -tags e2e ./e2e/suites/tui-flows/... -count=1` | scenarios absent | GREEN after branch | Given headless TUI driver; When reply->chip->detail; Then navigation assertions hold |

## Self-Verification Checklist

- [ ] Every test RED for documented reason (record output)
- [ ] No implementation code; no existing tests weakened
- [ ] C1/C4/C5 shapes exact in both language halves
- [ ] manifest rows complete; matrix F12a-c covered

**DO NOT COMMIT.** Orchestrator handles git after review.

## Completion Report Format

1. Files touched. 2. RED output per task. 3. Deviations.

## Review Checklist (For Review Agent)

- [ ] Tests-only; RED reasons = current defect/missing fields
- [ ] Dart tests use the existing widget-test harness (no new infra)
- [ ] Late-terminal + copyWith regression pins included
- [ ] No artifacts / corruption

Output: APPROVED or gaps with file+line.

## Notes

- GUI pixel-level claims are operator-verified (Q3); widget tests are the
  agent floor. Do not claim visual verification from widget tests alone.
