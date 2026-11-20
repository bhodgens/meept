# 05-navigation / 02-gui-navigation -- Completion Report

Worktree: .hermes/worktrees/usability-remediation (base HEAD b161d027).
NOT committed (per leaf instruction).

## 1. Files touched

Production (ui/flutter_ui/lib/):
- models/api_models.dart
  - New ToolCallRecord class: C4 frozen row {id,name,args_summary,
    result_summary,at}; fromLegacy accepts C4 maps AND legacy bare-name
    strings.
  - ChatMessage: added C1 fields intent_type / handler_case /
    classified_by (always-emitted, default '') + suggested_mode
    (closes 06-planning gate T4's typed field, coordinated per 04-clutter
    note: turn_id/task_id from that leaf untouched, no duplicate fields).
  - toolCalls type List<String>? -> List<ToolCallRecord>? with a JsonKey
    codec pair so toJson() emits structured MAPS (gate trace_expand T4
    wire-shape pin) and fromJson tolerates legacy strings.
  - fromBackendMessage passes through intent_type/handler_case/
    classified_by/suggested_mode and keeps tool_calls rows structured.
- models/api_models.freezed.dart + models/api_models.g.dart:
  regenerated via `dart run build_runner build`, then dart format
  (no hand edits).
- providers/chat_provider.dart: _consumeTurnTerminal reply bubble now
  carries event.intentType/handlerCase/classifiedBy (the :782 build site
  already kept turnId/taskId from 04-clutter/02).
- features/chat/chat_message_bubble.dart: ConsumerWidget ->
  ConsumerStatefulWidget. Mode-chip TODO closed: correlated assistant
  replies render a task chip (tap -> activeTaskProvider + goNamed
  'tasks'), mapped mode chip (plan/quick/direct/recall; never leaks raw
  suggested_mode enum), handler_case evidence chip, and -- on correlated
  replies (taskId+sessionId) -- 'trace' (C4) + 'route' (C5) expand
  affordances. Trace lazily fetches getMessagesPage(includeToolCalls:
  true); route lazily fetches getDispatchTrace(sessionId) and renders
  input_summary / intent / classifier_method / confidence / handler_case
  / task_id / turn_id rows. One fetch per expand (no per-build RPC).
- services/sdk_client.dart: getMessagesPage gains includeToolCalls param
  (sends include_tool_calls=true; matches the daemon's
  internal/session/session.go:2160 wire field); new getDispatchTrace
  (GET /api/v1/sessions/{id}/dispatch-trace, C5). getActivity signature
  deliberately UNCHANGED -- the deep-link gate stub pins its exact
  parameter list; session filtering is view-side (see Deviations).
- features/tasks/tasks_tab.dart: TasksList wired with onTaskSelected ->
  writes activeTaskProvider (detail now reachable from the list).
- features/tasks/tasks_detail.dart: ConsumerStatefulWidget; hydrates the
  selected task via sdkClient.getTask (bounded: one attempt per selected
  id; getTask seam previously had zero callers), prefers the hydrated
  document, renders steps (description/state/result rows).
- features/activity/activity_tab.dart: consumes the deep-link param
  inside the tab seam -- GoRouterState.of(context).uri.queryParameters
  ['session'] (try/catch; non-router contexts = no filter) -> passed to
  ActivityList as sessionFilter.
- features/activity/activity_list.dart: ActivityList(sessionFilter:)
  view-filters rendered records by record.sessionId (state.records stays
  unfiltered); empty-after-filter shows 'no activity records'; tile tap
  (task_step/step records) writes activeTaskProvider + goNamed('tasks')
  -- GestureDetector (the tile list has no Material ancestor).

Test-harness compatibility (NOT gate files; required to keep the rest of
the suite compiling after the SDK/model changes):
- test/features/chat/chat_input_test.dart,
  test/providers/chat_pagination_test.dart: getMessagesPage overrides
  gained `bool includeToolCalls = false`.
- test/features/chat/chat_message_bubble_test.dart: toolCalls helper
  maps List<String> names -> ToolCallRecord.

## 2. Verify output (real runs)

- flutter test test/providers/chat_correlation_test.dart  -> 5/5 GREEN
- flutter test test/features/tasks_detail_test.dart       -> 2/2 GREEN
- flutter test test/features/trace_expand_test.dart       -> 2/2 GREEN
- flutter test test/features/dispatch_popover_test.dart   -> 2/2 GREEN
- flutter test test/core/deep_link_param_test.dart        -> 4/4 GREEN
- all 4 gate files together                              -> 13/13 GREEN
- flutter test test/features/mode_chip_test.dart (06-planning gate T4,
  shares this surface)                                    -> GREEN
- full `flutter test`: 13 unique failing tests, EXACTLY the base
  pre-existing set (git-stash A/B verified: approval_card 5,
  seal_surface 5, chat_submit_flow 'parked status' 1, restore 2).
  ZERO new failures. (Base context said "32"; measured base failure
  count is 13 unique -- unchanged either way.)
- flutter build web --release -> exit 0, "Built build/web".
- dart format --output=none --set-exit-if-changed lib test -> 0 changed.
- Gate diff empty: git diff HEAD on the 5 gate files shows no changes.
- go build ./... untouched (no Go changes in this leaf).

## 3. Build log

flutter build web --release run inline (27.3s compile, success). No
saved log file; parent can re-run the verify commands above.

## 4. Deviations / decisions

- W21 one-line rename NOT needed: at HEAD platform_open_web.dart no
  longer defines openExternalWeb (already merged); web build green.
- Deep-link session filter is VIEW-side (ActivityList.sessionFilter),
  not an extra getActivity parameter: the gate's stub overrides
  getActivity with a frozen named-parameter list, so adding `session:`
  would break the gate compile; the gate also pins
  activityProvider.state.records hasLength(2) AFTER navigating with
  ?session=, so filtering the notifier state would violate its own
  precondition. The consumed-where-it-lands seam (tab reads
  GoRouterState) matches the gate comment's requirement.
- No GUI-path liveness widget test added (leaf Meta mentions F-NEW-14):
  not pinned by any gate file and the task matrix (T1-T5) omits it;
  adding un-pinned tests risked scope creep. Flagging for orchestrator.
- Pixel-level appearance of chips/panels: operator-pending (Q3); widget
  tests are the agent floor (gate itself says so).
- suggested_mode was added because the bubble chip is now the 06-planning
  T4 gate's implementation seam ('GREEN after 06-planning/03 T2 (+05/02
  chip)'); mode_chip_test verified green.
