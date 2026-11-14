# 01/03-board-crud

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run `git add`.

## Parent and Dependencies
- Parent: `01-foundation` (orchestrator)

## Scope
Implement the Kanban board management logic (non-state-transition operations).
1. Implement `internal/kanban/board.go` providing operations for columns and labels.
2. Support column creation, reordering, WIP settings, and labeling on the store.
3. Ensure operations are idempotent where appropriate.

## Estimated Context
55K tokens

## Interface Contract
Exposes the following board management signatures (no state transitions):
- `Board.CreateColumn(ctx context.Context, b Board, c Column) error`
- `Board.ReorderColumn(ctx context.Context, b Board, colID string, pos int) error`
- `Board.SetWIPLimit(ctx context.Context, b Board, colID string, limit int) error`
- `Board.AddLabel(ctx context.Context, cardID string, label string) error`
- `Board.RemoveLabel(ctx context.Context, cardID string, label string) error`

## Tasks
1. **TDD: failing test for column reordering**
   - `go test -v ./internal/kanban/ -run TestBoard_ReorderColumn`
2. **Implementation: board.go**
   - Implement `internal/kanban/board.go` using the store.
   - Handle column reordering and WIP limit logic.
3. **Verification: board_test.go**
   - Test column CRUD, reordering, and labeling.
   - Verify interactions with `Store` methods.

## Self-Verification Checklist
- [ ] No state transitions are implemented here (leave to 02-workflow-authority).
- [ ] Reorder logic maintains column position integrity.
- [ ] Error handling uses `%w`.

## Review Checklist
- [ ] No debug prints or TODOs.
- [ ] Every new Set*/setter has a nil guard.
- [ ] No I/O under mutex.
