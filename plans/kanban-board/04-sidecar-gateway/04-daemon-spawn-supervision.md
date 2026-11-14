# 04/04-daemon-spawn-supervision

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 04-sidecar-gateway (Orchestrator)
- Dependencies: 01-foundation (C1)

## Scope
Implement the daemon-side lifecycle management for the Kanban sidecar (`internal/daemon/kanban_boot.go`).
- **Spawn Logic:** 
  - Spawn the `meept-kanban` process if `[kanban] sidecar_enabled` is true.
  - Use the `voice-child` pattern (daemon-spawned, confined process, Unix socket for RPC).
- **Supervision:**
  - Monitor process health; automatically restart on exit.
  - Handle `SIGTERM` for graceful shutdown.
- **Startup Hygiene:**
  - Integrate with `StartupOrphanSweep` to reap any leaked `meept-kanban` processes from previous failed starts.
  - Follow the voice child pattern: `internal/voicechild/child.go` (spawn/confine) and `internal/daemon/voice_plain_boot.go` (boot gate).

## Estimated Context (70K)
- `internal/daemon/kanban_boot.go`
- `internal/voicechild/child.go`, `internal/daemon/voice_plain_boot.go`
- `internal/daemon/AGENTS.md`

## Interface Contract (exact signatures exposed to siblings)
- `func (d *Daemon) StartKanbanSidecar(ctx context.Context)`

## Tasks
- [ ] TDD: Write `internal/daemon/kanban_boot_test.go` simulating process crashes and restarts.
- [ ] Implementation: Implement `internal/daemon/kanban_boot.go` with supervised process management.
- [ ] Implementation: Integrate `StartupOrphanSweep` logic for `meept-kanban`.
- [ ] Verify: `go test ./internal/daemon/...`

## Self-Verification Checklist
- [ ] Sidecar is spawned as a child process.
- [ ] Process is automatically restarted upon unexpected exit.
- [ ] `SIGTERM` is handled gracefully by both daemon and sidecar.

## Review Checklist
- [ ] No orphaned `meept-kanban` processes remain after daemon exit.
- [ ] Supervision logic uses existing `voice-child` machinery.
