# 03-board-projection/01-projector

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
Parent: 03-board-projection
Dependencies: 01-foundation, 02-workflow-authority

## Scope
Implement a real-time projector that subscribes to work-item state-change bus events and projects them into Kanban board columns.

## Estimated Context
90K

## Interface Contract
`NewProjector(bus Bus, store KanbanStore, rules ProjectionRules) (*Projector, error)`

## Tasks
- [ ] Implement `internal/kanban/projector.go`: Subscribe to `kanban.event.step.state`, `.goal.state`, `.job.state`, `.task.state`, `.plan.state` topics.
- [ ] Implement `internal/kanban/projector.go`: Map events via `kanban_projections` rules to `Card` objects. Fill `Card` provenance (`GoalID`, `EmployeeID`, `PlanID`, `SessionID`, `TriggerRef`) by reading the source work-item row keyed by `StateEvent.ID` (C1 provenance rule) — inject a read-only row-fetcher interface so the projector does not import each machine package directly.
- [ ] Implement `internal/kanban/projector.go`: Handle bursts via coalescence; never block the publisher.
- [ ] Write `internal/kanban/projector_test.go`: Verify projection logic against event streams.
- [ ] Verify: `go test -v -run TestProjector/coalescence ./internal/kanban/...`

## Self-Verification Checklist
- [ ] No `os.Getwd()` used.
- [ ] All errors wrapped with `%w`.
- [ ] No I/O inside any mutex.
- [ ] `projector_test.go` uses `$TMPDIR` for SQLite.

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] Every new Set*/setter has nil guard
- [ ] No I/O under mutex
- [ ] IDs from `pkg/id.Generate()` only
- [ ] `enabled=false` path compiles and is byte-identical
- [ ] `make graphs` re-run after topic/RPC/WS change
