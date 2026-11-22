# TUI Correlation + Navigation - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks using TDD. Gate tests
> ALREADY EXIST (00T T1/T2) - make them pass; do NOT delete/skip/weaken them.
> Do NOT commit. Do NOT git add. Explore with search_files/terminal cat;
> write once, never read back. Worktree: .hermes/worktrees/usability-remediation.

## Meta

- **Parent:** orchestrator.md
- **Scope:** TUI keeps terminal correlation fields; reply->task navigation; /plan works; dropped surfaced
- **Dependencies:** 00T-gate.md, C1/C4/C5/C6 producers (01-safety, 02-routing, 03-context)
- **Estimated Context:** 85K
- **Concurrency Group:** A
- **Audit references:** W14, W19, W20 (TUI half); N1/N2/N3/N5/N6; observability top-10 #1/#2/#4/#6

## Goal

Keep task_id/intent_type/handler_case/classified_by on TUI turn messages;
render a tappable task chip that opens task detail; expandable tool trace
under replies (include_tool_calls); why-this-route popover (dispatch-trace);
fix /plan to forward to chat.submit; surface dropped counters; detail modal
Escape closes; tasks-view counters labeled.

## Context

- internal/tui/models/chat_turn.go:389-396 DeliverTurnTerminal keep-list;
  turn.go:66-74 turnTerminalMsg; chat.go:74-87 ChatMessage; stale comments
  chat_turn.go:449-450 + app.go:1762-1763 ("no task_id" - false).
- internal/tui/models/tasks.go:452-461 detail modal; renderTaskDetailModal
  ~1144; connection.go:40,43 GetTask/ListTaskSteps (zero callers - wire).
- internal/tui/command_handler.go:1632 /plan -> session.set_mode (broken);
  dispatcher.go:984-1030 working route via chat.submit text.
- internal/tui/events.go:248-251 dropped discard; app.go:1532-1535 reconnect.
- internal/session messages.get include_tool_calls (C4, 03-context/01);
  GET /api/v1/sessions/{id}/dispatch-trace (C5, 02-routing/02) - TUI uses
  RPC session.dispatch_trace (dual-key now).

## Verified Anchors

- internal/tui/models/chat_turn.go:389 - DeliverTurnTerminal
- internal/tui/models/turn.go:66 - turnTerminalMsg struct
- internal/tui/command_handler.go:1632 - session.set_mode call
- internal/tui/models/tasks.go:452 - detail modal entry

## Interface Contracts (From Parent)

### What This Leaf Exposes
- TUI message models carry C1 fields; task chip -> detail navigation;
  trace expand (C4); route popover (C5); /plan forwards to chat.submit.

### What This Leaf Consumes
- C1, C4, C5, C6 (../master.md).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Keep correlation fields | Modify: internal/tui/models/chat_turn.go, turn.go, chat.go (models); remove stale comments | `go test ./internal/tui/ -run TestTurnTerminalKeepsTaskID -count=1` | gate T1 RED | GREEN | Given terminal payload; Then TaskID/IntentType/HandlerCase/ClassifiedBy on message; render task chip when TaskID set |
| T2 | Task chip nav + detail freshness | Modify: internal/tui/models/chat.go (chip render+key), tasks.go (detail via GetTask/ListTaskSteps), modal Escape close, counter labels | `go test ./internal/tui/ -run TestTaskChipNav -count=1` | gate T2 RED | GREEN | Given reply chip; When enter/click; Then task detail modal with fresh steps; Escape closes; steps counter labeled (completed/total) |
| T3 | Trace + route popover | Modify: internal/tui/models/chat.go (expand), rpc.go (messages.get include_tool_calls + dispatch_trace) | `go test ./internal/tui/ -run 'TestTraceExpand|TestRoutePopover' -count=1` (add) | no read surface wired | GREEN | Given completed reply; Then expand shows persisted tool calls; route popover shows intent/method/confidence rows |
| T4 | /plan fix + dropped notice | Modify: internal/tui/command_handler.go:1632 region; events.go dropped handling; chat.go:3140 card hint | `go test ./internal/tui/ -run 'TestPlanSlashForwards|TestDroppedNotice' -count=1` | gate T2 RED | GREEN | Given /plan text; Then forwarded to chat.submit (dispatcher handles); card hint says "open plans tab, press a"; dropped>0 system notice once per episode |

## Self-Verification Checklist

- [ ] Gate T1/T2 GREEN; gate diff empty
- [ ] C1 fields exact; navigation resolves to real detail (live TUI screenshot)
- [ ] /plan never calls unregistered RPC; late-terminal regression green
- [ ] No scope creep / artifacts / corruption; gofmt clean

**DO NOT COMMIT.** Orchestrator commits after review.

## Completion Report Format

1. Files touched. 2. Verify output. 3. Live TUI screenshot path. 4. Deviations.

## Review Checklist (For Review Agent)

- [ ] copyWith-equivalent state discipline (no pending-turn loss on render)
- [ ] Detail fetch bounded (no per-render RPC storm)
- [ ] Stale comments removed; no new false claims
- [ ] Gate untouched; conventions; no artifacts

Output: APPROVED or gaps with file+line.

## Notes

- 04-clutter/02 dedupe consumes TaskID from this leaf - land T1 early in
  the leaf so the sibling can switch from turn_id-only to (turn_id,task_id).
