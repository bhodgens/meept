# Trivial Fast Lane + Client Render Dedupe - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T5/T6/T7) - make them pass; do NOT delete/skip/weaken
> them. Do NOT commit. Do NOT git add. Explore with search_files/terminal
> cat; write once, never read back. Worktree:
> .hermes/worktrees/usability-remediation.
> DUAL-LANGUAGE LEAF: dispatched as two halves (Go T1/T2, Dart T3) with the
> dedupe contract pinned in both briefs. Finish the Go half first.

## Meta

- **Parent:** orchestrator.md
- **Scope:** Trivial questions answer directly; task replies render once in both clients
- **Dependencies:** 00T-gate.md, 02-routing (dispatcher.go serialization)
- **Estimated Context:** 80K (Go half 50K, Dart half 30K)
- **Concurrency Group:** C
- **Audit references:** W18; U2/U7; F-ORCH-5; notes-server 2.4

## Goal

Add a config-gated trivial-question fast lane (no task machinery for simple
interrogatives); eliminate the card+terminal double render in TUI and make
GUI dedupe robust (keyed by turn/task id, not last-bubble content); honest
pending-state copy per handler_case.

## Context

- internal/agent/dispatcher.go:1559 isShortSimpleMessage call; :4971 def;
  quickplan upgrade gate session_state_gate.go (fast lane must not bypass
  security sanitization or session binding).
- internal/config/schema.go - orchestrator section (add trivial_fast_lane).
- internal/tui/models/chat.go:1505-1523 ChatTaskResultMsg card;
  chat_turn.go:455-460 SuppressTaskCompletedFor; turnTerminalMsg lacks
  TaskID (05-navigation/01 adds it - coordinate via C1).
- ui/flutter_ui/lib/providers/chat_provider.dart:715-757
  _lastAssistantContentEquals; ChatMessage has no taskId (05/01 adds).
- DEDUPE CONTRACT (pinned both halves): a task reply renders ONCE keyed by
  (turn_id, task_id) from C1; the card and the terminal bubble are the same
  logical reply - whichever arrives first wins, the second is suppressed;
  late terminal after stalled still renders (never suppressed by dedupe).

## Verified Anchors

- internal/agent/dispatcher.go:1559 - isShortSimpleMessage
- internal/tui/models/chat.go:1505 - card render case
- ui/flutter_ui/lib/providers/chat_provider.dart:749 - dedupe helper

## Interface Contracts (From Parent)

### What This Leaf Exposes
- orchestrator.trivial_fast_lane bool (default true, Q2 ratified at dispatch);
  dedupe key (turn_id, task_id) honored by both clients; pending-state copy
  per handler_case (parked != quota unless quota).

### What This Leaf Consumes
- C1 (task_id/intent_type/handler_case on terminal - 05/01 adds fields;
  this leaf reads them defensively until then).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Trivial fast lane (Go) | Modify: internal/agent/dispatcher.go isShortSimpleMessage region; internal/config/schema.go | `go test ./internal/agent/ -run TestTrivialFastLane -count=1` | gate T5 RED | GREEN | Given "What is 2+2? Answer briefly."; Then direct chat reply, no task row; artifact verbs still task-route; flag off = legacy; sanitization+session binding intact |
| T2 | TUI dedupe + pending copy (Go) | Modify: internal/tui/models/chat.go card case; chat_turn.go suppression; pending-line copy | `go test ./internal/tui/ -run TestTUITaskCardDedupe -count=1` | gate T6 RED | GREEN | Given relay turn; Then ONE render; late-after-stalled renders; parked copy reflects handler_case (quota text only for quota) |
| T3 | GUI dedupe (Dart half) | Modify: ui/flutter_ui/lib/providers/chat_provider.dart dedupe; models/api_models.dart ChatMessage (read taskId defensively) | `cd ui/flutter_ui && flutter test test/providers/chat_dedupe_test.dart` | gate T7 RED | GREEN | Given chat_message + terminal with interleaved system message; Then rendered once by (turn_id,task_id); copyWith discipline preserved |

## Self-Verification Checklist

- [ ] Gate T5/T6/T7 GREEN; gate diff empty
- [ ] Dedupe contract identical in both halves; late-terminal regression pin green
- [ ] Live-gate: rig "What is 2+2?" TUI screenshot shows ONE brief answer
- [ ] No scope creep / artifacts / corruption; gofmt + dart format

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched (per half). 2. Verify output. 3. Live-gate screenshot path. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] Fast lane never bypasses sanitization/session binding/security fence
- [ ] Regression suite routing tasks green (no over-fast-laning)
- [ ] Dedupe never suppresses a late real terminal
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- If 05-navigation/01 has not landed TaskID fields yet, T2/T3 key on
  turn_id alone (defensive) and switch to (turn_id,task_id) when present -
  pin both shapes in tests.
