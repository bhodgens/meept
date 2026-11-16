# Kanban Sidecar Gateway (`internal/kanbansvc` + `cmd/meept-kanban`)

Spec: `plans/kanban-board/master.md` (leaf `04-sidecar-gateway/02`).
Package invariants for agents: `internal/kanbansvc/AGENTS.md`.

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
route — reads included — requires its grant. Takeover
(`/api/v1/card/takeover`) is conjunctive: it requires BOTH
`board.mutate` AND `board.takeover`, and a token missing either one
gets 403. No anonymous board access.

| Condition | Status |
|---|---|
| no bearer token | 401 |
| token unknown to the grant store | 401 |
| token known, grant missing | 403 (body names the grant) |
| wrong HTTP verb on a known path | 405 (`Allow` header) |
| malformed JSON body | 400 (never forwarded) |
| daemon says "unknown method" | 404 |
| any other RPC error | 502 (body names the rpc) |

Tokens resolve through `GrantChecker`. The production implementation is
`TokenStore` (pairing, leaf `04/03`): a file-backed store at
`$MEEPT_HOME/kanban/tokens.json` (0600 in a 0700 dir, refused at load if
any group/other bit is set) that survives restarts and mints bearer
tokens through single-use pairing codes. `StaticGrantStore` remains the
in-process helper (tests, embedding hosts). With no token exchanged yet
the sidecar authorizes nothing an operator has not granted explicitly.

## Pairing (`POST /api/v1/pair/exchange`)

The sidecar boots with pairing wired: `meept-kanban` loads the
`TokenStore` (flag `-pair-store`, default
`$MEEPT_HOME/kanban/tokens.json`) and both consults it for every
bearer-authenticated route and arms the exchange route.

`POST /api/v1/pair/exchange` with body `{"code": "..."}` is the one
deliberately **ungranted** route — the single-use pairing code in the
body IS the credential. Flow:

1. `TokenStore.GeneratePairCode(board.read, ...)` mints a single-use
   `crypto/rand` code; the caller (operator, or the daemon — leaf
   `07/01`) prints it once to the console. Never logged.
2. The client exchanges it at `/api/v1/pair/exchange` and receives
   `{"token": "...", "grants": [...]}` — a fresh 256-bit bearer token
   carrying the grants the code was minted with.
3. The code is consumed on first use: reuse, wrong, and missing codes
   all answer 401 with the same message (no store-state oracle).

The route is exempt from the authorize check (like health-style
endpoints), requires no bearer token, and returns 503 when the sidecar
was built without a pairing store. There is no rate limiter — the
pairing layer's defenses are single-use consumption and constant-time
code comparison; none is invented at the gateway layer.

## Token transport

`bearerToken()` reads the `Authorization: Bearer <t>` header first, then
the WebSocket subprotocol lane (`bearer.<token>`). The subprotocol lane
exists because browsers cannot set headers on a WS upgrade. The gateway
**echoes the offered subprotocol back** in the upgrade response —
without the echo the browser aborts the connection after accept
(`TestGateway_WS` pins this with a real gorilla dialer).

## Event relay

`/api/v1/events` upgrades to WebSocket. The origin gate mirrors the
daemon hub's allow-list shape but is a LOOPBACK allow-list:
`localhost`/`127.0.0.1`/`::1` origins pass, a missing Origin (non-browser
client) passes because the bearer token still gates the connection, and
any other origin is refused by default (`checkWSOrigin`) —
relevant when fronting the sidecar with a TLS proxy, see
[docs/workflows/kanban.md](kanban.md) "Exposing the sidecar".
`[kanban].allowed_origins` (delivered as repeated `-allow-origin` flags
by the daemon boot; `SetAllowedOrigins`) extends the gate with EXACT
`scheme://host[:port]` entries — no wildcard, no suffix matching; an
empty list keeps the loopback-only posture byte-identical. The gateway
subscribes to the daemon through `EventSource` and pumps frames to the
client. The production source is `BusPump` (`pump.go`), which the
binary boots with (`cmd/meept-kanban/main.go`): it subscribes the seven
concrete kanban topics over the daemon's `bus.subscribe`/`bus.poll` RPC
pair — riding the SAME transport client the REST forwards use — polls on
a short interval, tolerates daemon disconnects with bounded retry, and
delivers daemon-shaped `board_event` envelopes. `Close` stops the pump
goroutine and closes every subscriber channel; per-relay context cancel
closes only that relay's channel, so a relay never outlives its
connection. `IdleSource` remains only as an inert embedder/test
placeholder. Any change to the relay's frame shape must stay compatible
with the Flutter `KanbanBoardEvent` decoder (`ui/flutter_ui/lib/features/kanban/`).

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
