# 07/01-daemon-wiring

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: `07-daemon-wiring` (orchestrator)
- Dependencies: 01-foundation/01 (store), 01-foundation/02 (config),
  03-board-projection/01 (projector), 05-surfaces/01 (rpc handlers)

## Scope
Wire the kanban subsystem into daemon startup, gated on `[kanban].enabled`.
1. Create `internal/daemon/kanban_wiring.go`: when `cfg.Kanban.Enabled`,
   open `internal/kanban` Store (SQLite under the daemon home;
   `MaxOpenConns(1)`; drain+close before any new query), construct the
   Projector (03/01) and subscribe it to the five `kanban.event.*` topics,
   and register the `kanban.*` RPC handlers (05/01) following the
   `internal/daemon/intake_status_rpc.go` registration pattern.
2. Patch `internal/daemon/components.go` — SHARED FILE: patch-only on
   unique anchors, never write_file the whole file. Add the kanban wiring
   call after the intake wiring block.
3. When `enabled=false` (default): no store open, no goroutine, no RPC
   registration — byte-identical boot.
4. Periodic reconcile: when `[kanban].reconcile_interval > 0`, schedule
   `ReconcileNow` on a ticker tied to the daemon stop context.

## Estimated Context
75K tokens

## Interface Contract
- `func wireKanban(d *Daemon, cfg *config.Config) error` (internal, called
  from components.go)
- Exposes the live-daemon guarantee: `kanban.board.list` answers over the
  Unix socket when enabled.

## Tasks
1. **TDD: failing test for unregistered methods when disabled**
   - `go test -v ./internal/daemon/ -run TestKanbanWiring_Disabled`
2. **Implementation: kanban_wiring.go**
   - Store open + projector start + RPC registration behind the enabled gate.
3. **Wiring: components.go patch**
   - Single anchored patch; do not reorder neighboring blocks.
4. **Verification**
   - `go test -p 2 ./internal/daemon/ -run TestKanbanWiring`
   - `go build ./...`

## Self-Verification Checklist
- [ ] `enabled=false` boot is byte-identical (no store, no goroutine).
- [ ] Store uses `MaxOpenConns(1)`; no query while Rows/Tx open.
- [ ] components.go patched on unique anchor only.
- [ ] All errors wrapped with `%w`; no I/O under mutex.

## Review Checklist
- [ ] No debug prints or TODOs.
- [ ] Contract IDs C1-C10 used verbatim.
- [ ] make graphs re-run (RPC registration is a graphs surface).
