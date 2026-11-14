# Kanban Sidecar Gateway (`internal/kanbansvc` + `cmd/meept-kanban`)

Spec: `plans/kanban-board/master.md` (leaf `04-sidecar-gateway/02`).

## What it is

`meept-kanban` is the only network-exposed process of the kanban feature.
It is a thin sidecar gateway: an HTTP server that forwards board requests
to the meept daemon and relays board events to browsers. It holds **no
board logic and no database handle**. All state lives in the daemon
(`internal/kanban` store + the work-item machines it projects).

## How it talks to the daemon

The gateway uses `internal/transport.Client` — the same RPC/HTTP client
the CLI uses. Two dial modes (mirrors `transport.Config`):

- unix socket (`-daemon-socket`, default `$MEEPT_HOME/meept.sock`), transport
  `"rpc"`;
- HTTP base URL (`-daemon-http`) for a daemon with `transport.http`.

Every REST route maps 1:1 to a `kanban.*` RPC method (contract C4). The
route table lives in `restRoutes` in `gateway.go`; `TestGateway_EveryRouteForwardsToItsRPC`
walks it and fails if any path forwards to the wrong method.

## Authorization (contract C5)

Three grants: `board.read`, `board.mutate`, `board.takeover`. Every
route — reads included — requires its grant. No anonymous board access.

| Condition | Status |
|---|---|
| no bearer token | 401 |
| token unknown to the grant store | 401 |
| token known, grant missing | 403 (body names the grant) |
| wrong HTTP verb on a known path | 405 (`Allow` header) |
| malformed JSON body | 400 (never forwarded) |
| daemon says "unknown method" | 404 |
| any other RPC error | 502 (body names the rpc) |

Tokens resolve through `GrantChecker`. This leaf ships
`StaticGrantStore` (an in-memory map, `Grant`/`Revoke`, safe for
concurrent use). Runtime pairing (leaf `04/03`) populates it from the
daemon's pairing handshake; until then the sidecar authorizes nothing an
operator has not granted explicitly.

## Token transport

`bearerToken()` reads the `Authorization: Bearer <t>` header first, then
the WebSocket subprotocol lane (`bearer.<token>`). The subprotocol lane
exists because browsers cannot set headers on a WS upgrade. The gateway
**echoes the offered subprotocol back** in the upgrade response —
without the echo the browser aborts the connection after accept
(`TestGateway_WS` pins this with a real gorilla dialer).

## Event relay

`/api/v1/events` upgrades to WebSocket (origin-checked against the
daemon hub's allow-list: same-origin or no-Origin clients). The gateway
subscribes to the daemon through `EventSource` and pumps frames to the
client. `IdleSource` is the no-event placeholder used until the bus
bridge leaf lands; it closes its channel on context cancel so a relay
never outlives its connection.

## Static web bundle

`-webdir` (default `$WEBDIR`, else `$MEEPT_HOME/webui`)
serves the Flutter bundle under `/web/` via the canonical
`http.StripPrefix + http.FileServer` pair — path cleanup and traversal
rejection stay inside `net/http`, which owns that contract.
No bundle: `/web/*` answers 503 with JSON, `/` answers a plain-text
explanation. With a bundle: `/` 302s to `/web/`, `/web` 301s to `/web/`.

## Known debt (tracked follow-up)

`cmd/meept-kanban` links `modernc.org/sqlite` transitively:
`internal/transport` → `rpc_adapter.go` → `internal/tui` (the socket
`RPCClient` implementation lives in `internal/tui/rpc.go`). The sidecar
never opens a DB handle — the code is linked, not used — but it breaks
the "network-exposed process holds no database dependency" goal on the
binary level. The fix belongs to a transport-split leaf (move or
re-home `tui.RPCClient`); until it lands:

- **never import `internal/tui` from `internal/kanban` or
  `internal/kanbansvc`** (the packages themselves are verified clean:
  `go list -deps ./internal/kanbansvc | grep sqlite` is empty);
- do not treat the binary's SQLite linkage as authorization to store
  board state sidecar-side.

## Verification

```
go test -p 2 -race -count=1 ./internal/kanbansvc/...
go build -o bin/meept-kanban ./cmd/meept-kanban
```
