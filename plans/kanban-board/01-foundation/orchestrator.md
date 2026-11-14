# 01-foundation

## Goal
Implement the core data structures and storage for the Kanban board, establishing the ground truth for board, column, and card entities as defined in the master plan.

## Interface Contracts
- Inherited: master.md C1-C10
- Local: 
    - C1: Go types for `Board`, `Column`, `WorkRef`, `Card`, `StateEvent`, `CardMoved` (as defined in `internal/kanban/types.go`).
    - C3: SQLite table names: `kanban_boards`, `kanban_columns`, `kanban_cards`, `kanban_projections`.

## Child Index

| Child | Type | Deps | Est context | Files |
|---|---|---|---|---|
| 01/01-store-schema | Leaf | — | 80K | types.go, store.go, store_test.go |
| 01/02-config-surface | Leaf | — | 45K | config.go |
| 01/03-board-crud | Leaf | 01 | 55K | board.go, board_test.go |

## Dispatch Protocol
1. `delegate_task` per leaf.
2. Review in-session by main model.
3. Do NOT commit. Do NOT run `git add`.
4. `git status --short` and `git diff --stat HEAD` after each async return.
5. Commit per leaf after review pass.
6. Update tracking table in `master.md`.

## Coding Conventions
- Go stdlib + existing deps only.
- Match neighbor file style; cite invariant docs.
- Use table-driven tests and `t.Cleanup` for SQLite.

## Completion Tracking Table
| Leaf | Status | Notes |
|---|---|---|
| 01/01-store-schema | DONE | 322b6682 (parent fixes: FK pragma, provenance INSERT, card-events API, 13 tests) |
| 01/02-config-surface | DONE | f93a017f (hand-recovered after subagent context blowout) |
| 01/03-board-crud | DONE | 83ed8f56 (Manager; parent moved raw SQL into store, fixed ListColumns ORDER BY, +unknown-col/clamp/idempotence tests) |

## Integration Test Plan
- Verify SQLite migration logic for `kanban_` tables.
- Ensure `internal/kanban/types.go` exactly matches master.md C1.
- Test CRUD operations on columns and cards.

## Review Checklist
- [ ] No leaf owns >3 changed files.
- [ ] Contract IDs C1-C10 used verbatim.
- [ ] Every new Set*/setter has a nil guard.
- [ ] No I/O under mutex.
- [ ] `enabled=false` path compiles and is byte-identical.
