# GUI Correlation + Navigation - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T3/T4/T5) - make them pass; do NOT delete/skip/weaken
> them. Do NOT commit. Do NOT git add. Explore with search_files/terminal
> cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.
> PREREQUISITE: W21 build fix merged (fix/usability-gui-openexternal). If
> platform_open_web.dart still defines openExternalWeb, apply the one-line
> rename FIRST (verify flutter build web exit 0) and note it in the report.

## Meta

- **Parent:** orchestrator.md
- **Scope:** GUI keeps terminal correlation; reply->task nav; reachable detail; traces; evidence popover; deep-link params
- **Dependencies:** 00T-gate.md, C1/C4/C5 producers, W21 merge
- **Estimated Context:** 90K
- **Concurrency Group:** A
- **Audit references:** W14, W19, W20 (GUI half), W21; N1/N2/N4/N5/N7;
  gui-notes Q2/Q3; F-NEW-14 (GUI-path chat completion never arrived on WS
  within 3.5 min - this leaf's correlation + C6 delivery make it observable;
  add a GUI-path liveness widget test)

## Goal

Keep taskId/intentType/handlerCase/classifiedBy on ChatMessage; tappable
task chip -> reachable task detail with steps; expandable tool trace;
why-this-route popover via dispatch-trace HTTP route; activity tile
tap-through + deep-link session param consumption; mode chip (the existing
TODO) from intent_type/handler_case.

## Context

- ui/flutter_ui/lib/providers/chat_provider.dart:719-725 bubble construction
  (drops taskId); :664-746 _consumeTurnTerminal; models/api_models.dart:
  399-409 ChatMessage; models/ws_events.dart:166-179 TurnTerminalEvent
  (fields parsed already).
- ui/flutter_ui/lib/features/tasks/tasks_tab.dart:10,22 activeTaskProvider
  never written / TasksList without onTaskSelected; tasks_detail.dart:
  104-125; tasks_list.dart:14,170 optional callback; sdk_client.dart:878
  getTask uncalled, :861 list, steps route GET /tasks/{id}/steps.
- ui/flutter_ui/lib/features/chat/chat_message_bubble.dart:130-149 mode-chip
  TODO + MarkdownBody (no onTapLink).
- ui/flutter_ui/lib/core/deep_link.dart:112-117 /activity?session=<id>;
  features/activity/activity_list.dart:46-59 loadActivity (sort only).
- sdk_client.dart - add getDispatchTrace (C5 route), getMessagesPage
  includeToolCalls param (C4).
- Conditional-import halves must export identical symbols (W21 class).

## Verified Anchors

- ui/flutter_ui/lib/providers/chat_provider.dart:719 - bubble construction
- ui/flutter_ui/lib/features/tasks/tasks_tab.dart:10 - activeTaskProvider
- ui/flutter_ui/lib/features/chat/chat_message_bubble.dart:136 - MarkdownBody
- ui/flutter_ui/lib/core/deep_link.dart:112 - session deep link

## Interface Contracts (From Parent)

### What This Leaf Exposes
- GUI ChatMessage carries C1 fields; task chip -> detail (steps hydrated);
  trace expand (C4); route popover (C5); activity session filter; mode chip.

### What This Leaf Consumes
- C1, C4, C5 (../master.md); W21 build fix.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Keep correlation + mode chip | Modify: chat_provider.dart bubble build; api_models.dart ChatMessage (+freezed regen); chat_message_bubble.dart chip | `cd ui/flutter_ui && flutter test test/providers/chat_correlation_test.dart` | gate T3 RED | GREEN | Given terminal taskId/intent/handler; Then ChatMessage carries them; mode chip renders (planned/direct/recall) |
| T2 | Task detail reachable + steps | Modify: tasks_tab.dart (onTaskSelected->activeTaskProvider), tasks_list.dart, tasks_detail.dart (steps via getTask+steps route); sdk_client.dart getTask wiring | `flutter test test/features/tasks_detail_test.dart` | gate T4 RED (unreachable) | GREEN | Given task tap (from chip or list); Then detail pane shows title/state/steps/results; chip navigates here |
| T3 | Trace expand + route popover | Modify: chat_message_bubble.dart (expandable trace), sdk_client.dart (includeToolCalls + getDispatchTrace); new popover widget | `flutter test test/features/trace_expand_test.dart test/features/dispatch_popover_test.dart` | gate T4 RED | GREEN | Given completed reply; Then trace shows persisted calls; popover shows intent/method/confidence rows from C5 |
| T4 | Activity tap-through + deep-link param | Modify: activity_list.dart (tile tap -> detail/filter), deep_link.dart + activity_tab.dart (queryParameters session filter) | `flutter test test/core/deep_link_param_test.dart` | gate T5 RED (param dropped) | GREEN | Given meept://session/<id>; Then activity filtered to session; tile tap opens task/step detail |
| T5 | Build + format gate | Verify: flutter build web --release; dart format | `cd ui/flutter_ui && flutter build web --release` | W21 broken at base | exit 0 | Given worktree HEAD; Then web build green; no conditional-export symbol mismatch |

## Self-Verification Checklist

- [ ] Gate T3/T4/T5 GREEN; gate diff empty
- [ ] C1/C4/C5 shapes exact; copyWith discipline (no fresh ChatState mid-turn)
- [ ] flutter build web exit 0; dart format clean
- [ ] No scope creep / artifacts / corruption

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Build log path. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] freezed/json_serializable regenerated consistently (no hand-edit drift)
- [ ] Detail fetch bounded; no per-build RPC storm
- [ ] Deep-link param never silently dropped; unknown action surfaces error
- [ ] Pixel-level claims marked operator-pending (Q3)
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- GUI live interaction blocked by self-signed cert + loopback browser block;
  widget tests + API checks are the agent floor. Operator browser pass is I4.
