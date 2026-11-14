# 04/03-security-bind-pairing

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 04-sidecar-gateway (Orchestrator)
- Dependencies: 01-foundation (C1, C5)

## Scope
Implement secure pairing and binding policies for the sidecar (C7).
- **Binding Policy (C7):** 
  - Default to loopback (`127.0.0.1`).
  - If `[kanban] sidecar_bind` is non-loopback, log a loud boot warning and require explicit config.
- **Pairing Flow (C7):**
  - Implement `/kanban/pair/exchange` (POST) in the daemon.
  - Use a single-use `crypto/rand` code (pattern from `internal/comm/http/pairing.go`).
  - Code must be printed to the daemon's console on generation.
  - Exchange the code for a stored bearer token in the daemon.
- **Bearer Token Store:**
  - Store bearer tokens mapping to a grant set (C5) in a file under `$MEEPT_HOME`.
  - File permissions must be `0600`.
- **Security:**
  - Implement CORS/CSRF origin checks for all sidecar requests.
  - Support `X-Forwarded-For` trust via `internal/comm/http/server.go:1913`.
  - Re-use existing rate limiter (C4/C5/C7).
- **Invariant:** The sidecar binary must contain NO TLS and NO provider keys.

## Estimated Context (80K)
- `internal/comm/http/pairing.go`
- `internal/auth/users.go` and `internal/auth/store.go`
- `internal/config/config.go`

## Interface Contract (exact signatures exposed to siblings)
- `POST /kanban/pair/exchange` (Daemon side - exchanged via pairing code)
- `Auth Middleware` (Validates bearer token against stored grant set)

## Tasks
- [ ] TDD: Write `internal/kanbansvc/pairing_test.go` verifying the single-use exchange and token storage (kanban pairing lives in the sidecar gateway package, NOT the daemon's `internal/comm/http/pairing.go` — that is precedent only: read, do not edit).
- [ ] Implementation: Implement the daemon-side pairing handler and token management.
- [ ] Implementation: Implement the sidecar's bearer token middleware.
- [ ] Verify: `go test ./internal/comm/http/...` and `go build ./cmd/meept-kanban`

## Self-Verification Checklist
- [ ] Pairing code is printed to console only once.
- [ ] Bearer token file is `0600` and located under `$MEEPT_HOME`.
- [ ] Non-loopback bind triggers the loud boot warning.

## Review Checklist
- [ ] Sidecar binary has no TLS or provider keys.
- [ ] Pairing code is single-use and `crypto/rand` based.
