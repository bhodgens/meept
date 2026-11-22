# 05-navigation / 02-gui-navigation -- PROGRESS

Base: worktree HEAD b161d027. Gate tests committed (f326aef8). NOT committing.

## RED status at start (confirmed by running each file)
- test/providers/chat_correlation_test.dart: 5 failing (C1 keys dropped on
  bubble + model; mode chip missing)
- test/features/tasks_detail_test.dart: 2 failing (detail unreachable; steps
  never hydrated)
- test/features/trace_expand_test.dart: 2 failing (no trace affordance; no
  include_tool_calls; tool_calls stringified)
- test/features/dispatch_popover_test.dart: 2 failing (no route affordance;
  no getDispatchTrace)
- test/core/deep_link_param_test.dart: 1 failing of 4 (session param dropped
  in the /activity view)

## Task log
- [x] Read leaf doc + all 5 gate files
- [x] T1 C1 correlation fields on ChatMessage (+intent_type/handler_case/
      classified_by/suggested_mode freezed regen), chat_provider bubble
      carries event.intentType/handlerCase/classifiedBy, mode/task chips
      in chat_message_bubble (chat_correlation: 5/5 GREEN)
- [x] T2 task detail reachable: TasksTab wires TasksList(onTaskSelected ->
      activeTaskProvider); TasksDetail -> StatefulWidget hydrating via
      getTask (bounded per id), renders steps (tasks_detail: 2/2 GREEN)
- [x] T3 trace expand + route popover: bubble trace/route chips; C4 fetch
      via getMessagesPage(includeToolCalls) ; C5 via new
      getDispatchTrace('/api/v1/sessions/{id}/dispatch-trace');
      ToolCallRecord structured rows + map codec in toJson (trace_expand
      2/2, dispatch_popover 2/2 GREEN)
- [x] T4 deep-link session param: ActivityTab reads
      GoRouterState.uri.queryParameters['session'] in the tab seam,
      ActivityList(sessionFilter) view-filters records; activity tile tap
      -> activeTaskProvider + goNamed('tasks') (deep_link_param 4/4 GREEN)
- [x] T5 flutter build web exit 0 (W21 rename already applied at HEAD:
      platform_open_web defines openExternal, no stale openExternalWeb);
      dart format clean (270 files, 0 changed); full suite: 13 unique
      failures, byte-identical to the base stash-check (approval_card 5,
      seal_surface 5, chat_submit_flow parked 1, restore 2) -- ZERO new
      failures; 3 test-harness stub signatures/types (chat_input,
      chat_pagination, chat_message_bubble_test) updated for the SDK
      signature + toolCalls type change (compatibility, not gate edits);
      mode_chip_test (06-planning gate) + all 4 leaf gates green
      together (21/21)
- [ ] 02-REPORT.md

## Notes / decisions
- Activity stub pins getActivity()'s exact named-parameter list -- adding a
  session param to the SdkApiClient signature would break the gate compile.
  Session filtering therefore happens VIEW-side (filter on record
  session_id), state.records stays unfiltered (gate precondition: 2 records).
- ChatMessage.toolCalls changes List<String> -> List<ToolCallRecord>
  (structured, frozen C4 keys). No UI consumers of toolCalls today (grep).
