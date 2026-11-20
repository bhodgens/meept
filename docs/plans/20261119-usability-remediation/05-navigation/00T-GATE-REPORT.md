# 05-navigation TEST-GATE Report (orchestrator-authored; child 429d)

Status: TESTS WRITTEN (T1-T6 on disk; T7 restore e2e PENDING). Child sa-1-949f7d92
ran 4.7h, wrote 20 files, died on upstream HTTP 429 before report/finish.
Orchestrator verified on-disk RED state and authored this report. T7 re-dispatch
needed (small).

## Gate files on disk (RED for missing C1/correlation/nav impl)
- T1 internal/tui/models/chat_turn_correlation_test.go -- terminal reply keeps
  task_id/turn_id/plan_id correlation fields (F-ORCH-04 TUI half).
- T2 internal/tui/models/chat_nav_test.go -- task-chip navigation from transcript.
- T3 ui/flutter_ui/test/providers/chat_correlation_test.dart -- GUI terminal reply
  keeps taskId. Orchestrator verified RED: "Some tests failed",
  TestChatMessageTaskID "C1 field" missing (expected: task-77, actual null).
- T4 internal/tui/command_handler_plan_test.go -- /plan slash forwards (not
  swallowed). Orchestrator verified RED: TestPlanSlashForwards FAIL.
  TestDroppedNotice -- dropped-message notice surfaced.
- T5 ui/flutter_ui/test/features/tasks_detail_test.dart + trace_expand_test.dart
  + dispatch_popover_test.dart -- reachable task detail, trace expand, dispatch
  popover (behind-the-scenes navigable).
- T6 ui/flutter_ui/test/core/deep_link_param_test.dart -- deep-link param routing.
- T7 restore policy alignment + queue parked visibility e2e -- PENDING (child
  died before writing). internal/rpc/queue_status_parked_test.go on disk
  (orchestrator verified RED: TestQueueStatusParked FAIL).

## RED evidence (orchestrator spot-checks)
- flutter test chat_correlation_test.dart: FAIL (taskId null, C1 field missing).
- go test ./internal/tui/ -run TestPlanSlashForwards: FAIL.
- go test ./internal/rpc/ -run TestQueueStatusParked: FAIL.
- Go agent-package gates blocked by sibling compile-RED seams (01-safety leaves
  02/07); verify via go -overlay at impl time.

## Deviations
- Child died on 429 before report; orchestrator authored from on-disk files +
  PROGRESS.md + spot-check RED runs.
- T7 restore e2e not written; re-dispatch a small finisher for T7 only.

## Gate verdict
TESTS_WRITTEN (T1-T6 + queue_status_parked). T7 PENDING re-dispatch. Impl leaves
05-navigation/01-tui-nav, 02-gui-nav, 03-restore pending dispatch.