# 05-NAVIGATION - Reply-to-Task Links, Traces, Evidence, Reconnect UX

> **For the executing agent:** Branch orchestrator. Test gate first,
> validate in-session, then impl leaves; review IN-SESSION; commit after
> review. Do NOT implement code yourself. Worktree:
> .hermes/worktrees/usability-remediation.

## Meta

- **Role:** Branch
- **Parent:** ../master.md
- **Children:** 00T-gate.md + 3 implementation leaves
- **Scope:** Behind-the-scenes is easy to navigate to from any reply.

## Goal

Close W14/W19/W20 + N1-N7 (report axis 4): keep terminal correlation fields
in both clients, tappable task chips, tool-trace read surface, dispatch
evidence popover, GUI task detail wiring, activity tap-through + deep-link
params, TUI /plan fix, restore-policy alignment, dropped-event surfacing.

## Architecture

Three leaves along client seams: TUI correlation+navigation (01,
internal/tui), GUI correlation+navigation (02, ui/flutter_ui), server read
surfaces + restore contract clients (03, session.messages.get tool_calls
already in 03-context/01 - this leaf consumes it; owns GUI/TUI restore
policy + queue-status parked visibility). 01 and 02 are file-disjoint
(group A parallel). 03 touches both clients' session/restore regions -
group B after 01+02. W21 GUI build fix arrives via the parallel
fix/usability-gui-openexternal branch (merge first; 02 verifies SUPERSEDED).

## Interface Contracts

Consumes C1 (terminal fields), C4 (include_tool_calls), C5 (dispatch-trace
HTTP route), C6 (dropped counters, reconnect). Produces: client navigation
contract (task chip -> task detail; trace expand; why-this-route popover;
deep-link param consumption).

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|--------------|--------------|-------------|
| 00T | 00T-gate.md | leaf (tests) | none | 65K | A |
| 01 | 01-tui-navigation.md | leaf | 00T, C1/C4/C5/C6 producers | 85K | A |
| 02 | 02-gui-navigation.md | leaf | 00T, C1/C4/C5 producers, W21 merge | 90K | A |
| 03 | 03-restore-queue-visibility.md | leaf | 00T, 01, 02 | 60K | B |

## Dispatch Protocol

Phase 0: 00T gate RED, validate in-session, commit, TESTS_VALIDATED.
Phase 1: 01 + 02 together (file-disjoint Go vs Dart). Phase 2: 03. Review
in-session; commit exact paths; max 3 cycles then BLOCKED. Briefs: worktree
path, consumed contracts, no-commit/no-git-add/no-read_file/write-once.
GUI live verification: API-level + widget tests; pixel-level claims need
operator browser pass (master Q3) - mark accordingly.

## Review Checklist

- [ ] All leaf tasks implemented; verify commands pass
- [ ] Gate tests GREEN and unmodified
- [ ] task_id/intent/handler_case kept on BOTH message models (C1)
- [ ] Navigation links resolve to real detail (task chip -> task detail;
      trace expand shows persisted calls; popover shows trace rows)
- [ ] W21 build fix present (flutter build web exit 0)
- [ ] No scope creep / artifacts / corruption; gofmt + dart format

## Coding Conventions

Per ../master.md. Branch specifics: TUI bubbletea v2 headless test pattern
(e2e/suites/tui-flows driver); GUI riverpod copyWith discipline (never fresh
ChatState mid-turn - F20 class); conditional-import halves export identical
symbols (W21 class).

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|--------------|
| 00T-gate | PENDING | 0 | |
| 01-tui-navigation | PENDING | 0 | |
| 02-gui-navigation | PENDING | 0 | |
| 03-restore-queue-visibility | PENDING | 0 | |

## Integration Test Plan

1. `go test -p 2 ./internal/tui/...`; `cd ui/flutter_ui && flutter test &&
   flutter build web --release`.
2. e2e tui-flows + tui-gui-consistency suites.
3. Live rig: TUI screenshot - reply shows task chip, enter opens detail;
   GUI API-level - dispatch-trace route returns rows for the turn;
   tool_calls present in messages.get include_tool_calls=true.

## Test Gate

Test leaf: 00T-gate.md. Matrix (root F12):

| # | Feature | Source | Unit tests | E2E tests | Status |
|---|---------|--------|-----------|-----------|--------|
| F12a | TUI correlation + nav + /plan fix | 01 T1-T4 | TestTurnTerminalKeepsTaskID, TestTaskChipNav, TestPlanSlashForwards, TestDroppedNotice | e2e/suites/tui-flows (extend) | PENDING |
| F12b | GUI correlation + detail + traces + popover | 02 T1-T5 | TestChatMessageTaskID, TestTaskDetailReachable, TestTraceExpand, TestDispatchPopover, TestDeepLinkParam | widget tests + gui parity e2e | PENDING |
| F12c | restore policy + queue parked visibility | 03 T1-T2 | TestRestorePolicyAligned, TestQueueStatusParked | e2e tui-gui-consistency | PENDING |

Delta log: (none yet)

## Open Questions

- Q3: pixel-level GUI verification needs operator browser (self-signed cert).
  Widget tests + API checks are the agent-verifiable floor.

## Notes

- 05-navigation/01 adds TaskID to turnTerminalMsg/ChatMessage - 04-clutter/02
  dedupe consumes it defensively; land 01 before 04/02 if possible (root
  wave order C handles this).
