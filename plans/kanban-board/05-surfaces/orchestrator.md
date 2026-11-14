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
| 05/01-rpc-handlers | leaf | 03 | 75K | `internal/rpc/kanban.go` |
| 05/02-cli | leaf | 06 | 55K | `cmd/meept/kanban.go` |
| 05/03-tui-board | leaf | 03 | 90K | `internal/tui/kanban_view.go` |
| 05/04-flutter-bridge | leaf | 01, 05/05 | 70K | `ui/flutter_ui/lib/services/kanban_service.dart` |
| 05/05-flutter-board-view | leaf | 03, 05/04 | 95K | `ui/flutter_ui/lib/features/kanban/board_view.dart` |
| 05/06-parity-inventory | leaf | 03, 05/03 | 45K | `internal/tui/testdata/gui_inventory.json` |
| 05/07-ws-event-class | leaf | 03, 05/05 | 60K | `internal/comm/wsclass/wsclass.go` |

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
| 05/01 | PENDING | |
| 05/02 | PENDING | |
| 05/03 | PENDING | |
| 05/04 | PENDING | |
| 05/05 | PENDING | |
| 05/06 | PENDING | |
| 05/07 | PENDING | |

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
