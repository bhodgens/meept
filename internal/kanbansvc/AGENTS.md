# internal/kanbansvc/AGENTS.md

Guidance for AI agents working in `internal/kanbansvc/` (and by contract
`cmd/meept-kanban`, its only production host). Referenced from the root
AGENTS.md; the full sidecar-gateway contract lives here. Update both in
the same commit per the root maintenance rule. User-facing docs:
`docs/workflows/kanbansvc.md` (gateway contract), `docs/workflows/meept-kanban.md`
(running the binary), `docs/workflows/kanban.md` (feature + TLS-proxy
operation). Plan tree: `plans/kanban-board/` (C4–C7 below are verbatim
contract ids from `plans/kanban-board/master.md`).

## Forward-only gateway: REST to RPC, zero board logic

The gateway is an HTTP front that maps every REST route 1:1 onto a
`kanban.*` RPC method through `restRoutes` (`gateway.go:82-100`), and
`TestGateway_EveryRouteForwardsToItsRPC` fails if any path forwards to
the wrong method. It holds **no board logic and no store handle**: all
lease/WIP/timeline/move semantics live in `internal/kanban` behind
`internal/rpc/kanban.go` (`rpc/kanban.go:9-13` — "the sidecar gateway
stays a 401/403 REST front that forwards INTO these methods, never a
re-implementation"). Do not add validation, retry, caching, or
state-deriving behavior here; a new route is a `restRoutes` entry plus
the grant lane, nothing more.

Daemon access is the `RPCCaller` seam (`gateway.go:44-49`) satisfied only
by `internal/transport.Client` — the same RPC/HTTP client the CLI uses.
The package imports exactly `internal/config` (pairing.go:49, home-dir
resolution) plus stdlib; it must NEVER import `internal/kanban`,
`internal/rpc`, `internal/daemon`, or a machine package.

## No database

`internal/kanbansvc` carries ZERO direct sqlite imports —
`go list -deps ./internal/kanbansvc | grep sqlite` must stay empty
(guarded claim, `docs/workflows/kanbansvc.md` "Known debt").
**Transitive link debt:** the `meept-kanban` BINARY links
`modernc.org/sqlite` via `internal/transport` → `transport/rpc_adapter`
→ `internal/tui` (the socket `RPCClient` lives in `internal/tui/rpc.go`).
The code is linked, not used; the fix belongs to a transport-split leaf.
Until it lands: never import `internal/tui` from `internal/kanban` or
`internal/kanbansvc`, and never treat the binary's SQLite linkage as
authorization to store board state sidecar-side (root AGENTS.md Kanban
row carries the same rule).

## No TLS, no provider keys, loopback by default (C7)

- **No TLS in this process.** TLS terminates at an operator proxy —
  worked Caddy/nginx examples live in `docs/workflows/kanban.md`
  ("Exposing the sidecar"). `cmd/meept-kanban/main.go:7`.
- **No provider/daemon secrets.** The sidecar holds no LLM keys and no
  dev key; its only credential is the bearer token the CLIENT presents,
  resolved through the `GrantChecker` seam without ever logging it
  (pairing.go header: code and minted token are "never logged").
- **Loopback default + bind guard.** Default bind `127.0.0.1:8790`
  (`cmd/meept-kanban/main.go:34`). `guardBind` (`main.go:128-140`)
  REFUSES a non-loopback bind without the explicit `-allow-lan` flag and
  logs a loud boot warning when it passes: exposing the board is never
  accidental, and `-allow-lan` transfers TLS ownership to the operator —
  bearer tokens then ride cleartext HTTP.
- The daemon spawns this child ONLY when `[kanban].sidecar_enabled`
  (default false) — supervision contract in
  `docs/workflows/kanban.md` ("Sidecar spawn supervision", leaf 04/04,
  `internal/daemon/kanban_boot.go`).

## Grants (C5): conjunctive takeover

`board.read` / `board.mutate` / `board.takeover` (`gateway.go:34-36`).
A route's `grants` list is CONJUNCTIVE: the token must hold ALL of them
(`gateway.go:74-75`, `authorize` `gateway.go:273-288`). Takeover needs
`board.mutate` AND `board.takeover` — a takeover-only token cannot move
cards in (commit 310701d5). Status-code posture is part of the client
contract (Dart `kanban_errors.dart`): no/unknown token → 401, known
token missing a grant → 403 naming the lane, wrong verb on a known path
→ 405 + `Allow` (`gateway.go:182-183`), malformed JSON → 400 never
forwarded, daemon "unknown method" → 404, any other RPC error → 502 with
the frozen refusal text riding the body (`gateway.go:370-376`).

## Bearer token lane + pairing (leaf 04/03)

- `bearerToken()` reads `Authorization: Bearer *** first, then the WS
  subprotocol lane (browsers cannot set headers on a WS upgrade).
- `TokenStore` (`pairing.go`) is the production `GrantChecker`: token
  file at `$MEEPT_HOME/kanban/tokens.json`, **0600 in a 0700 dir, load
  REFUSED if any group/other bit is set** (`pairing.go:286`); a fresh
  256-bit `crypto/rand` bearer token (`pairing.go:18`, `tokenBytes=32`
  `pairing.go:77`).
- Pairing codes: `GeneratePairCode` mints a single-use `crypto/rand`
  code (`pairing.go:200-205`); `POST /api/v1/pair/exchange`
  (`gateway.go:217-246`) is the deliberately UNGRANTED route — the code
  in the body IS the credential. Exchange compares constant-time
  (`subtle.ConstantTimeCompare` `pairing.go:248`); reuse, wrong, and
  missing codes all answer the SAME 401 `ErrInvalidPairCode` message
  ("invalid or already-used") — no store-state oracle (`pairing.go:84-88`,
  `gateway.go:213`). Codes live in memory only: a restart re-mints.
- **Empty-store boot state is 401-everything**: until a code is minted
  and exchanged the store knows no tokens and every API route refuses —
  deliberate; "a board reachable without auth is an outage waiting to
  happen" (`cmd/meept-kanban/main.go:71-79`). Do not add a default/
  anonymous token.

## WS relay: subprotocol echo is load-bearing

`/api/v1/events` upgrades to WebSocket (`gateway.go:392`). The token may
ride `Sec-WebSocket-Protocol` as `bearer.<token>` (`gateway.go:11`,
`wsBearer` `gateway.go:319-323`); auth happens IN THE HANDSHAKE (bad
token never completes an upgrade, `gateway.go:390-404`). The server MUST
echo the offered subprotocol back (`pickSubprotocol` `gateway.go:330-346`,
`gateway.go:415-417`) — without the echo the browser aborts the
connection after accept (`TestGateway_WS` pins it with a real gorilla
dialer). Origin check mirrors the daemon hub's allow-list: same-origin
or no-Origin (non-browser) clients allowed, bearer still required
(`gateway.go`). `[kanban].allowed_origins` (repeated `-allow-origin`
flags from the daemon boot; `SetAllowedOrigins`) adds EXACT
scheme://host[:port] entries for the TLS-proxy deployment — no wildcard,
no suffix matching; the empty list keeps the loopback-only posture byte-
identical. The event feed comes through the `EventSource`
seam (`gateway.go:54-56`); the production source is `BusPump`
(`pump.go`): it bridges the daemon's `bus.subscribe`/`bus.poll` RPC pair
(the seven concrete kanban topics, over the SAME `RPCCaller` client the
REST forwards ride) and delivers daemon-shaped `board_event` envelopes.
`IdleSource` remains only as an inert embedder/test placeholder
(`gateway.go`).

Any operator proxy in front of the sidecar MUST pass
`Sec-WebSocket-Protocol` through unchanged — the bearer lane rides it —
see the TLS-termination section in `docs/workflows/kanban.md`.

## Deadcode-stock status

`.githooks/deadcode-baseline.txt` has ZERO `internal/kanbansvc` entries —
the gateway and `cmd/meept-kanban` are self-contained binaries/tests, not
unwired stock. The daemon-side kanban wiring (fetchers, LeaseAuthority,
RPC registration) remains leaf 07/01 pending; see
`internal/kanban/AGENTS.md`.

## Verification

```
go test -p 2 -race -count=1 ./internal/kanbansvc/...
go build -o bin/meept-kanban ./cmd/meept-kanban
go list -deps ./internal/kanbansvc | grep sqlite          # must be empty
```
e2e: suite `kanban-board` (manifest maps `internal/kanbansvc/` and
`cmd/meept-kanban/` to it; drives the real sidecar binary).
