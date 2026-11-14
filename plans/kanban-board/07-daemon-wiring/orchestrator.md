# 07-daemon-wiring

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Goal
Wire the kanban subsystem into the live daemon: open the store, start the
projector, register the RPC handlers, and gate everything on
`[kanban].enabled`. Without this branch the feature compiles but is never
user-reachable (the repo's wiring-requirement red flag).

## Parent and Dependencies
- Parent: `master.md` (forest root)
- Dependencies: 01-foundation (store, config), 03-board-projection
  (projector, human commands), 05-surfaces/01 (rpc handler file)

## Interface Contracts
- Inherited: master.md C1-C10.
- Local: registration pattern follows `internal/daemon/intake_status_rpc.go`
  (daemon-side RPC registration file) + `internal/daemon/components.go`
  (Components struct wiring). The store opens under the daemon's existing
  SQLite home with `MaxOpenConns(1)` — never query while Rows/Tx open
  (drain+close first), per the repo's SQLite store invariant.

## Child Index

| Child | Type | Deps | Est ctx | Files |
|---|---|---|---|---|
| 07/01-daemon-wiring | Leaf | 01, 03, 05/01 | 75K | internal/daemon/kanban_wiring.go, internal/daemon/components.go, internal/daemon/kanban_wiring_test.go |

## Dispatch Protocol
1. `delegate_task` per leaf with pinned contracts C1-C10.
2. Review in-session by main model; commit per leaf.

## Coding Conventions
- Go stdlib + existing deps only; errors wrapped with `%w`.
- `components.go` is SHARED — patch-only on unique anchors, never
  write_file the whole file.

## Completion Tracking Table

| Task | Status | Notes |
|---|---|---|
| 07/01-daemon-wiring | PENDING | |

## Integration Test Plan
- e2e master case (g): boot the real daemon with `[kanban].enabled=true`;
  assert `kanban.board.list` answers over the Unix socket.
- With `enabled=false` (default): no store open, no projector goroutine,
  no RPC registration — byte-identical boot.

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] Every new Set*/setter has nil guard
- [ ] No I/O under mutex
- [ ] `enabled=false` path compiles and is byte-identical
- [ ] make graphs re-run after RPC registration change
