# 03-board-projection/02-rebuild-reconcile

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
Parent: 03-board-projection
Dependencies: 01-foundation, 02-workflow-authority

## Scope
Implement a reconciliation mechanism to ensure the Kanban board can be fully rebuilt from source tables and maintain consistency.

## Estimated Context
65K

## Interface Contract
`ReconcileNow(ctx context.Context) error` (updates `kanban_projections` via full scan)

## Tasks
- [ ] Implement `internal/kanban/reconcile.go`: Full rebuild from source tables (`kanban_boards`, `kanban_columns`, etc.).
- [ ] Implement `internal/kanban/reconcile.go`: Periodic rebuild trigger.
- [ ] Implement `internal/kanban/reconcile.go`: `ReconcileNow()` implementation for on-demand refresh.
- [ ] Write `internal/kanban/reconcile_test.go`: Verify equality (board == source) after rebuild.
- [ ] Test: Kill the projector mid-stream, restart, and assert equality in `reconcile_test.go`.

## Self-Verification Checklist
- [ ] No `os.Getwd()` used.
- [ ] All errors wrapped with `%w`.
- [ ] No I/O inside any mutex.
- [ ] `reconcile_test.go` uses `$TMPDIR` for SQLite.

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] Every new Set*/setter has nil guard
- [ ] No I/O under mutex
- [ ] IDs from `pkg/id.Generate()` only
- [ ] `enabled=false` path compiles and is byte-identical
- [ ] `make graphs` re-run after topic/RPC/WS change
