# meept-kanban (sidecar binary)

`cmd/meept-kanban` builds the sidecar gateway process for the kanban
board. The gateway contract — routes, grants, event relay, static
bundle, known debt — is documented in
[docs/workflows/kanbansvc.md](kanbansvc.md). This page covers running it.

## What it is

A long-lived process you start beside the meept daemon. It serves the
board's HTTP REST surface, the WebSocket event feed, and the Flutter web
bundle. It talks to the daemon only through `internal/transport`
(unix socket RPC or HTTP). It opens no database. It holds no board logic.

## Running

```
bin/meept-kanban -bind 127.0.0.1:8790
```

| Flag | Default | Purpose |
|---|---|---|
| `-bind` | `127.0.0.1:8790` | HTTP listen address (contract C7 loopback default). |
| `-allow-lan` | `false` | Permit a non-loopback bind. You then own TLS termination — bearer tokens ride cleartext HTTP. |
| `-daemon-socket` | `$MEEPT_HOME/meept.sock` | Daemon RPC socket for forwarding. |
| `-daemon-http` | (empty) | Daemon HTTP base URL. Overrides `-daemon-socket` when set. |
| `-webdir` | `$WEBDIR`, else `$MEEPT_HOME/webui` | Flutter bundle root, served at `/web/`. |
| `-allow-origin` | (none) | Extra EXACT browser Origin accepted on the WS relay (repeatable or comma-list). The daemon passes `[kanban].allowed_origins` through; the sidecar itself never reads config (C7). Empty = loopback-only posture. |
| `-pair-store` | `$MEEPT_HOME/kanban/tokens.json` | Bearer-token + pairing-code store file. |

A non-loopback `-bind` without `-allow-lan` refuses to start.

## Tokens (as-built, pairing leaf 04/03)

The gateway authorizes through `TokenStore` (`internal/kanbansvc/pairing.go`):
a bearer token must have been minted by a single-use pairing code
(`TokenStore.GeneratePairCode`, printed once by the operator — or the
daemon, leaf 07/01 — never logged) and exchanged at
`POST /api/v1/pair/exchange`. Until a code is minted and exchanged the
store knows no tokens and every API route answers 401. That refusal is
deliberate: a board reachable without auth is an outage waiting to
happen. The placeholder page at `/` still serves. See
[docs/workflows/kanbansvc.md](kanbansvc.md) for the grant lanes and the
conjunctive takeover rule (310701d5).

## Signals

`SIGINT`/`SIGTERM` shut the listener down with a 5-second HTTP drain
(`cmd/meept-kanban/main.go:116`) and close the daemon connection.

## Verifying it is up

```
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8790/          # 200 (index/placeholder)
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8790/api/v1/board/list  # 401 (no token)
```

## Build

`make build` compiles it with the rest, or:

```
go build -o bin/meept-kanban ./cmd/meept-kanban
```
