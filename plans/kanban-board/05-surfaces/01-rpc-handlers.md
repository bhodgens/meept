# 05/01-rpc-handlers

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 05-surfaces (Orchestrator)
- Dependencies: 03-board-projection (C4)

## Scope
Implement the `kanban` RPC handlers in `internal/rpc/kanban.go`.
- **C4 Methods Implementation:**
  - `kanban.board.list` (C4)
  - `kanban.board.create` (C4)
  - `kanban.column.list` (C4)
  - `kanban.column.set_wip` (C4)
  - `kanban.card.list` (C4)
  - `kanban.card.get` (C4)
  - `kanban.card.create` (C4)
  - `kanban.card.move` (C4)
  - `kanban.card.takeover` (C4)
  - `kanban.card.assign` (C4)
  - `kanban.card.comment` (C4)
  - `kanban.card.timeline` (C4)
  - `kanban.reconcile.now` (C4)
- **Authorization (C5):** All mutation methods (anything modifying cards/columns/boards) must be guarded by `board.mutate` and `board.takeover` as appropriate. Read methods must be guarded by `board.read`.
- **Implementation Detail:** All handlers must be registered in the RPC server (follow `internal/rpc/intake_status.go` pattern). NOTE: this leaf ships the handler + registration code as a registerable unit; the LIVE daemon registration (calling it from components/daemon wiring) is owned by 07/01-daemon-wiring. Until 07 lands, the handlers exist but the live daemon does not serve them.

## Estimated Context (75K)
- `internal/rpc/kanban.go`
- `internal/kanban/authz.go`
- `internal/rpc/types.go`

## Interface Contract (exact signatures exposed to siblings)
- All `kanban.*` RPC methods as defined in C4.

## Tasks
- [ ] TDD: Write `internal/rpc/kanban_test.go` testing all C4 methods with valid and unauthorized calls.
- [ ] Implementation: Implement `internal/rpc/kanban.go` with all C4 methods and `board.mutate` guards.
- [ ] Verify: `go test -v ./internal/rpc/...`

## Self-Verification Checklist
- [ ] All mutation methods are protected by `board.mutate` or `board.takeover`.
- [ ] Read methods are protected by `board.read`.
- [ ] RPC methods follow the `RegisterHandler` pattern.

## Review Checklist
- [ ] RPC methods match C4 exact strings.
- [ ] No logic duplication with `internal/kanbansvc/gateway.go`.
