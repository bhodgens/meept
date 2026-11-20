# 05-navigation TEST-GATE progress (durability checkpoint)

Worktree: /Users/caimlas/git/meept/.hermes/worktrees/usability-remediation
HEAD: 76b1c0f7. Tests only; no commit, no git add.

## T1 - TUI correlation pin (Go) - DONE, RED verified
- Files: internal/tui/models/chat_turn_correlation_test.go
- Verify: go test ./internal/tui/models/ -run 'TestTurnTerminalKeepsTaskID|TestTurnTerminalStaleCommentsRemoved|TestTurnTerminalCopyWithDiscipline' -count=1
- Status: RED (4 tests). Last lines:
  - TestTurnTerminalKeepsTaskID: "turnTerminalMsg: C1 field TaskID missing (dropped by the keep-list)" (+IntentType/HandlerCase/ClassifiedBy, both models)
  - TestTurnTerminalKeepsTaskIDEarlyBuffer: same C1 missing on early-buffer path (late-terminal pin)
  - TestTurnTerminalStaleCommentsRemoved: "chat_turn.go still claims ... 'carries no task_id'" + ../app.go
  - TestTurnTerminalCopyWithDiscipline: C1 missing on concurrent resolution (pending-turn preservation assertions PASS today)
  - FAIL github.com/caimlas/meept/internal/tui/models 3.453s

## T2 - TUI nav + /plan + dropped pins (Go) - models half DONE RED; tui half written, verify BLOCKED by sibling
- Files: internal/tui/models/chat_nav_test.go (RED verified); internal/tui/command_handler_plan_test.go (written, gofmt clean)
- Verify (models): go test ./internal/tui/models/ -run 'TestTaskChipNav|TestTraceExpand|TestRoutePopover' -count=1
- Status models: RED (4 tests):
  - TestTaskChipNav: "renders no task chip with the task id" + "no task-detail navigation msg carrying task-42"
  - TestTaskChipNavDetailFreshSteps: "without fetching fresh steps via ListTaskSteps (zero callers)" + steps not rendered
  - TestTraceExpand: "never requested session.messages.get with include_tool_calls=true (C4)"
  - TestRoutePopover: "never requested session.dispatch_trace for sess-1 (C5)"
- Verify (tui): go test ./internal/tui/ -run 'TestPlanSlashForwards|TestPlanSlashBareToggleForwards|TestDroppedNotice' -count=1
- Status tui: BLOCKED - sibling 06-planning gate file internal/tui/mode_badge_test.go (untracked, NOT mine) breaks package build: undefined gateSeedSubmitAck/drainGateCmd/models.PlanCountLineForGate/keyPressMsgAlias. Retry after sibling wave stabilizes. My file compiles standalone-checked via gofmt; LSP clean.

## T3 - GUI correlation pin (Dart) - written, verify pending
- Files: ui/flutter_ui/test/providers/chat_correlation_test.dart
- Verify: cd ui/flutter_ui && flutter test test/providers/chat_correlation_test.dart

## T4 - GUI detail+trace+popover (Dart) - written, verify pending
- Files: ui/flutter_ui/test/features/tasks_detail_test.dart, trace_expand_test.dart, dispatch_popover_test.dart
- Verify: flutter test test/features/tasks_detail_test.dart test/features/trace_expand_test.dart test/features/dispatch_popover_test.dart

## T5 - GUI deep-link param (Dart) - written, verify pending
- Files: ui/flutter_ui/test/core/deep_link_param_test.dart
- Verify: flutter test test/core/deep_link_param_test.dart

## T6 - restore + queue parked (Go+Dart) - queue Go written; restore pending
- Files: internal/rpc/queue_status_parked_test.go (written, gofmt clean)
- Verify: go test ./internal/rpc/ -run TestQueueStatusParked -count=1
- NOTE: sibling 01-safety gate internal/rpc/task_approval_reject_test.go references undefined RejectFunc -> package test build currently broken (NOT mine). Retry later.
- Pending: internal/tui/models/restore_test.go, ui/flutter_ui/test/providers/restore_test.dart

## T7 - e2e extensions - pending
- Pending: e2e/suites/tui-flows/ + tui-gui-consistency/ extensions + e2e/manifest.json rows (manifest already modified by sibling waves - merge carefully, append-only)

## Interference log
- 2026-11-19: sibling gate waves landing concurrently in shared worktree (01-safety, 02-routing, 03-context, 04-clutter, 06-planning files present untracked). internal/tui + internal/rpc test builds broken by sibling files mode_badge_test.go / task_approval_reject_test.go. No sibling file touched by this gate.
## STATUS CHECK (finalize order received)
- T1 DONE on disk, RED verified (4 tests, C1 fields missing + stale comments).
- T2 DONE on disk: models half RED verified (4 tests); tui half (command_handler_plan_test.go) written, package build blocked by sibling mode_badge_test.go -> will verify via go -overlay.
- T3 DONE on disk, RED verified (5/5 fail: C1 keys dropped, chip missing).
- T4 DONE on disk, RED verified: tasks_detail 2/2 (activeTaskProvider null, getTask uncalled); trace_expand 2/2 (no affordance, C4 rows stringified); dispatch_popover 2/2 (no affordance).
- T5 on disk: param-drop test RED verified (quartzbeta leaks); resolver precondition GREEN; 'no param' guard test hits a pre-existing ActivityTab sort-row RenderFlex overflow at test font metrics -> fixing harness (ActivityList seam) now.
- T6 PENDING: queue Go file on disk (verify blocked by sibling task_approval_reject_test.go -> overlay); restore Go+Dart files being written NOW.
- T7 PENDING: e2e extensions + manifest rows next.

## T5 DONE - RED verified
- File: ui/flutter_ui/test/core/deep_link_param_test.dart (rewritten: real-font load + known pre-existing sort-row overflow filtered; drives real ActivityTab under blind /activity route)
- Verify: flutter test test/core/deep_link_param_test.dart -> 00:00 +3 -1: Some tests failed.
- RED: TestDeepLinkParam "session param dropped: /activity?session=sess-a still shows sess-b records" (quartzbeta found). GREEN preconditions: resolver param, shipped /activity route, no-param unfiltered guard.
- T3/T4 RED re-confirmed earlier: chat_correlation 0+5-, tasks_detail 0+2-, trace_expand/dispatch_popover fail on missing affordance + stringified C4 rows.
