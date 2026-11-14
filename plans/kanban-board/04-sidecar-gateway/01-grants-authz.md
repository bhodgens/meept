# 04/01-grants-authz

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 04-sidecar-gateway (Orchestrator)
- Dependencies: 01-foundation (C1, C5)

## Scope
Implement `internal/kanban/authz.go` to enforce C5 grants.
- Implement `CheckGrant(principal, grant string) bool`.
- Logic:
  - If `multiuser.enabled=false` (default), the Unix socket/dev_key owner holds all three grants: `board.read`, `board.mutate`, `board.takeover`.
  - If `multiuser.enabled=true`, verify principal has the required grant from the user store.
- Implement `GetGrantsForPrincipal(principal string) []string`.

## Estimated Context (55K)
- `internal/kanban/authz.go`
- `internal/kanban/types.go`
- `internal/config/config.go`

## Interface Contract (exact signatures exposed to siblings)
- `func CheckGrant(principal string, grant string) bool`
- `func GetGrantsForPrincipal(principal string) []string`

## Tasks
- [ ] TDD: Write `internal/kanban/authz_test.go` testing both `multiuser=false` (owner-only) and `multiuser=true` (user-specific) modes.
- [ ] Implementation: Implement `internal/kanban/authz.go` with `CheckGrant` and `GetGrantsForPrincipal`.
- [ ] Verify: `go test ./internal/kanban/...`

## Self-Verification Checklist
- [ ] `CheckGrant` returns `true` for all three grants when `multiuser=false`.
- [ ] `CheckGrant` respects user-specific grants when `multiuser=true`.
- [ ] No nil pointer dereferences on empty principal.

## Review Checklist
- [ ] `multiuser.enabled=false` path is byte-identical to legacy behavior.
- [ ] All errors are wrapped with `%w`.
