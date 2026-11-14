# 01/01-store-schema

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run `git add`.

## Parent and Dependencies
- Parent: `01-foundation` (orchestrator)

## Scope
Implement the storage layer for the Kanban board in `internal/kanban`.
1. Create `internal/kanban/types.go` with exact Go types from master.md C1.
2. Create `internal/kanban/store.go` implementing CRUD for `Board`, `Column`, and `Card` using the SQLite pattern (tables `kanban_boards`, `kanban_columns`, `kanban_cards`, `kanban_projections` per master.md C3).
3. Create `internal/kanban/store_test.go` with table-driven tests.

## Estimated Context
80K tokens

## Interface Contract
Exposes the following CRUD signatures:
- `Store.ListBoards(ctx context.Context) ([]Board, error)`
- `Store.CreateBoard(ctx context.Context, b Board) error`
- `Store.ListColumns(ctx context.Context, boardID string) ([]Column, error)`
- `Store.CreateColumn(ctx context.Context, c Column) error`
- `Store.ListCards(ctx context.Context, columnID string) ([]Card, error)`
- `Store.CreateCard(ctx context.Context, c Card) error`
- `Store.UpdateCard(ctx context.Context, c Card) error`
- `Store.DeleteCard(ctx context.Context, cardID string) error`
- `Store.MoveCard(ctx context.Context, cardID, fromCol, toCol string) error`
- `Store.GetCard(ctx context.Context, id string) (Card, error)`

## Tasks
1. **TDD: failing test for Store.CreateBoard**
   - `go test -v ./internal/kanban/ -run TestStore_CreateBoard`
2. **Implementation: types.go and store.go**
   - Implement `internal/kanban/types.go` (C1 verbatim)
   - Implement `internal/kanban/store.go` with SQLite migrations and CAS for card movement
   - Include the `kanban_card_events` migration (C3: append-only comments/timeline).
3. **Verification: store_test.go**
   - Run `go test -p 2 -v ./internal/kanban/`

## Self-Verification Checklist
- [ ] `internal/kanban/types.go` matches master.md C1 exactly.
- [ ] SQLite tables `kanban_` match master.md C3.
- [ ] `Store.MoveCard` uses a CAS pattern for the order/column updates.
- [ ] All errors are wrapped with `%w`.

## Review Checklist
- [ ] No debug prints or TODOs.
- [ ] Every new Set*/setter has a nil guard.
- [ ] No I/O under mutex.
- [ ] `enabled=false` path compiles and is byte-identical.
