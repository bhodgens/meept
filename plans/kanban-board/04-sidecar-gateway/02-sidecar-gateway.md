# 04/02-sidecar-gateway

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
- Parent: 04-sidecar-gateway (Orchestrator)
- Dependencies: 01-foundation (C1), 04/01-grants-authz

## Scope
Implement the sidecar gateway binary `cmd/meept-kanban/main.go` and its service layer `internal/kanbansvc/gateway.go`.
- **cmd/meept-kanban/main.go**: 
  - Bind to default `127.0.0.1:8790` (C7).
  - Implement non-loopback bind requirement with loud boot warning (C7).
  - Serve static WEBDIR bundle (C7).
  - Expose REST endpoints mapping 1:1 to C4 RPC methods (C4).
  - Implement WS relay of board events (C8).
  - Implement Unix-socket RPC client to communicate with the daemon.
  - NO SQLite import/handle (assert with `go list -deps` in tests).
- **internal/kanbansvc/gateway.go**:
  - Implements the REST/WS interface for the sidecar.
  - Proxies requests to the daemon via Unix socket.

## Estimated Context (85K)
- `cmd/meept-kanban/main.go`
- `internal/kanbansvc/gateway.go`
- `internal/comm/http/server.go`
- `internal/rpc/types.go`

## Interface Contract (exact signatures exposed to siblings)
- `GET /api/v1/board/list` (corresponds to `kanban.board.list`)
- `POST /api/v1/board/create` (corresponds to `kanban.board.create`)
- `POST /api/v1/card/move` (corresponds to `kanban.card.move`)
- `WS /api/v1/events` (WSBoard event stream)

## Tasks
- [ ] TDD: Write `internal/kanbansvc/gateway_test.go` verifying REST-to-RPC mapping and WS event relay.
- [ ] Implementation: Implement `cmd/meept-kanban/main.go` with loopback/non-loopback logic and static asset serving.
- [ ] Implementation: Implement `internal/kanbansvc/gateway.go` with the Unix-socket proxy.
- [ ] Verify: `go test ./internal/kanbansvc/...` and `go build ./cmd/meept-kanban`

## Self-Verification Checklist
- [ ] Sidecar binary does NOT import `modernc.org/sqlite` or any SQLite driver.
- [ ] REST endpoints handle authentication via bearer token (from pairing).
- [ ] WebSocket connection successfully relays `WSBoard` events.

## Review Checklist
- [ ] No SQLite handle or provider keys in the sidecar binary.
- [ ] REST methods match C4 RPC method names.
