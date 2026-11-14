# 04-sidecar-gateway Orchestrator

## Goal
Establish the network-exposed boundary for the Kanban subsystem. This branch implements the sidecar gateway binary, security pairing, and the supervision mechanism for the spawned process.

## Interface Contracts (inherited: master.md C1-C10 + branch-local only)
- **C5 Grants (Local):** `board.read`, `board.mutate`, `board.takeover`.
- **C7 Sidecar Binary:** No TLS; loopback bind by default; non-loopback requires `[kanban] sidecar_bind` knob and logs a loud warning.
- **C10 Lease Rule:** `ErrLeaseHeld` must include holder and age.

## Child Index
| Child | Type | Deps | Est context | Files |
|---|---|---|---|---|
| 04/01-grants-authz | leaf | 01 | 55K | `internal/kanban/authz.go` |
| 04/02-sidecar-gateway | leaf | 01, 04/01 | 85K | `cmd/meept-kanban/main.go`, `internal/kanbansvc/gateway.go` |
| 04/03-security-bind-pairing | leaf | 01, 04/01 | 80K | `internal/kanbansvc/pairing.go` (+ `internal/kanbansvc/pairing_test.go`; `internal/comm/http/pairing.go` is READ-ONLY precedent, not owned) |
| 04/04-daemon-spawn-supervision | leaf | 01 | 70K | `internal/daemon/kanban_boot.go` |

## Dispatch Protocol
1. `delegate_task` per leaf.
2. Verify `go build ./cmd/meept-kanban` and `go test ./internal/kanban/...`.
3. Check `git status --short` and `git diff --stat HEAD` after each return.
4. Verify C7 boot warnings via log/stdout.
5. Review in-session.

## Coding Conventions
- Go stdlib + existing deps only.
- Error wrapping with `%w`.
- TDD for all leaves.

## Completion Tracking Table
| Leaf | Status | Notes |
|---|---|---|
| 04/01 | PENDING | |
| 04/02 | PENDING | |
| 04/03 | PENDING | |
| 04/04 | PENDING | |

## Integration Test Plan
- Verify sidecar process lifecycle (spawn/SIGTERM).
- Verify pairing flow: `POST /kanban/pair/exchange` -> check daemon console -> use token to call REST.
- Verify non-loopback bind failure/warning.

## Review Checklist
- [ ] Sidecar contains NO SQLite handle or provider keys.
- [ ] `ErrLeaseHeld` includes holder + age.
- [ ] Non-loopback bind requires explicit knob.
