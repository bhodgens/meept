# 05-surfaces Orchestrator

## Goal
Implement the user-facing interaction layer for the Kanban board across CLI, TUI, and Flutter GUI.

## Interface Contracts (inherited: master.md C1-C10 + branch-local only)
- **C4 RPC Methods:** `kanban.board.list`, `kanban.board.create`, `kanban.column.list`, `kanban.column.set_wip`, `kanban.card.list`, `kanban.card.get`, `kanban.card.create`, `kanban.card.move`, `kanban.card.takeover`, `kanban.card.assign`, `kanban.card.comment`, `kanban.card.timeline`, `kanban.reconcile.now`.
- **C6 CLI:** `meept kanban list|show|board|create|move|takeover|wip|reconcile`.
- **C8 WS Event Class:** `WSBoard` in `internal/comm/wsclass`.
- **TUI/GUI Parity:** Features (status bars, command palette, keyboard shortcuts, session semantics) must match.

## Child Index
| Child | Type | Deps | Est context | Files |
|---|---|---|---|---|
| 05/01-rpc-handlers | leaf | 03 | 75K | `internal/rpc/kanban.go` (+ `kanban_test.go`; daemon-side registration is 07/01) |
| 05/02-cli | leaf | 03 | 55K | `cmd/meept/kanban.go` (+ `kanban_test.go`; uses the existing `transport.Client` from `connectDaemon()`, `cmd/meept/main.go:302` — there is no `internal/rpc/client.go`) |
| 05/03-tui-board | leaf | 03 | 90K | `internal/tui/kanban_view.go` (+ test; uses the `internal/tui/tableutil` package — a package dir, not `tableutil.go`) |
| 05/04-flutter-bridge | leaf | 01 | 70K | `ui/flutter_ui/lib/services/kanban_service.dart`, `ui/flutter_ui/lib/models/kanban_models.dart` |
| 05/05-flutter-board-view | leaf | 03, 05/04 | 95K | `ui/flutter_ui/lib/features/kanban/board_view.dart` |
| 05/06-parity-inventory | leaf | 03, 05/03 | 45K | `internal/tui/testdata/gui_inventory.json`, `internal/tui/parity_test.go` (UPDATE — file exists), `internal/tui/kanban_view.go` |
| 05/07-ws-event-class | leaf | 03, 05/05 | 60K | `internal/comm/wsclass/wsclass.go` (+ tests), `ui/flutter_ui/lib/models/kanban_models.dart` |

Leaf order: 05/01, 05/02, 05/03 parallel; then 05/04; then 05/05; then
05/06 and 05/07 (parallel). The old cycle (05/04 depending on 05/05) is
removed — the 04→05→07 chain is strict.

## Dispatch Protocol
1. `delegate_task` per leaf.
2. Verify `make graphs-check` and `make lint-ci` after all leaves land.
3. Check `git status --short` and `git diff --stat HEAD` after each return.
4. Verify TUI table sizing with `internal/tui/tableutil.Size`.
5. Review in-session.

## Coding Conventions
- Go stdlib + existing deps only.
- Dart: lowercase UI text, `kIsWeb` guards, no `dart:io` in shared code.
- TDD for all leaves.

## Completion Tracking Table
| Leaf | Status | Notes |
|---|---|---|
| 05/01 | DONE | b43b9ea6 (kanban.* RPC handlers — all 13 C4 methods) |
| 05/02 | DONE | 126c1eb5 (meept kanban command tree) |
| 05/03 | DONE | 5f649948 (TUI board view) |
| 05/04 | DONE | 45b19b90 (GUI service bridge) |
| 05/05 | DONE | fe03c6e5 (GUI board view) |
| 05/06 | DONE | 1a2ade63 (parity verified + lowercase fix; Go pin added 2026-11-16 in internal/tui/parity_kanban_test.go, issue #144) |
| 05/07 | DONE | f715909d (WSBoard event class for the kanban feed) |

> Reconciliation note (2026-11-16, issue #156): rows above were under-reporting merged work; statuses updated from git (each SHA verified an ancestor of main). Tables were last refreshed d492867c (2026-11-14).

## Integration Test Plan
- CLI `meept kanban` command suite.
- TUI board navigation and keybindings.
- Flutter board view (Web + Desktop) drag-and-drop simulation.
- WS `WSBoard` event propagation to Flutter.

## Review Checklist
- [ ] Parity between TUI and Flutter GUI.
- [ ] TUI tables sized on both axes.
- [ ] `kIsWeb` guards in Flutter shared code.
- [ ] `make graphs` updates.
