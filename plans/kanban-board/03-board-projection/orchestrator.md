# 03-board-projection

## Goal
Implement the data projection and command handling logic for the Kanban board, enabling real-time visual updates and manual human/agent interventions.

## Interface Contracts (inherited: master.md C1-C10 + branch-local only)
- C1: Go types (`internal/kanban/types.go`)
- C2: Bus topics (`kanban.event.step.state`, etc.)
- C3: Table names (`kanban_boards`, `kanban_columns`, `kanban_cards`, `kanban_projections`)
- C4: RPC methods (`kanban.card.move`, `kanban.card.takeover`, `kanban.card.assign`, `kanban.card.comment`, `kanban.reconcile.now`, etc.)
- C10: Lease rule (ErrLeaseHeld with Holder/Since)

## Child Index

| Child | Type | Deps | Est ctx | Files |
|---|---|---|---|---|
| 01-projector | Leaf | 01, 02 | 90K | internal/kanban/projector.go, internal/kanban/projector_test.go |
| 02-rebuild-reconcile | Leaf | 01, 02 | 65K | internal/kanban/reconcile.go, internal/kanban/reconcile_test.go |
| 03-human-commands | Leaf | 01, 02 | 100K | internal/kanban/human.go, internal/kanban/human_test.go |

## Dispatch Protocol
1. Do NOT commit. Do NOT run git add.
2. Dispatch each leaf with:
   - Leaf path
   - Pinned contracts (C1-C10)
   - Coding Conventions
   - Verification commands (`go test -p 2 ./...`)
3. Upon async completion:
   - Run `git status --short` and `git diff --stat HEAD --`
   - Review in-session by main model
   - Commit per leaf: `feat(kanban): <leaf-name>`
4. Update the Completion Tracking Table.

## Coding Conventions
- Go stdlib + existing deps only.
- Error wrapping with `%w`; typed sentinel errors.
- Table-driven tests; `t.Cleanup` for temp SQLite in `$TMPDIR`.
- Match neighbor file style.

## Completion Tracking Table

| Task | Status | Notes |
|---|---|---|
| 01-projector | PENDING | |
| 02-rebuild-reconcile | PENDING | |
| 03-human-commands | PENDING | |

## Integration Test Plan
(See master.md Integration Test Plan for 03-board-projection specifics)

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] Every new Set*/setter has nil guard
- [ ] No I/O under mutex
- [ ] IDs from `pkg/id.Generate()` only
- [ ] `enabled=false` path compiles and is byte-identical
- [ ] `make graphs` re-run after topic/RPC/WS change
