# Native Hermes HTTP Interop — Master Plan

Tree: `docs/plans/hermes-interop/` (master.md + leaves 01–03).

## Goal

Meept natively drives a running Hermes gateway API Server over HTTP:
meept agents get a `hermes` builtin tool to consult/steer a Hermes
instance (status, one-shot chat, async runs with steer/stop, session
reads). Meept is the CLIENT; Hermes is the remote agent.

## Verified upstream contract (Hermes API Server, 2026-10-15)

Source: hermes-agent docs (features/api-server) + `gateway/platforms/api_server.py`
route table on main.

- Transport: HTTP on the gateway process, default `http://127.0.0.1:8642`.
  Enabled by `API_SERVER_ENABLED=true` + `API_SERVER_KEY` in Hermes' `.env`.
- Auth: `Authorization: Bearer <API_SERVER_KEY>` on EVERY route.
- Discovery: `GET /v1/capabilities` → feature flags + endpoint table
  (`endpoints` map: name → {method, path}). The client MUST probe this
  before first use and degrade per-flag.
- Health: `GET /health`, `GET /health/detailed`.
- Sync turn: `POST /api/sessions/{id}/chat` (one synchronous agent turn,
  returns final response + usage) and `POST /api/sessions/{id}/chat/stream`
  (SSE: assistant.delta, tool.started/completed, run.terminal).
- Async runs: `POST /v1/runs` → 202 + run_id; `GET /v1/runs/{id}` status;
  `POST /v1/runs/{id}/steer` (mid-run guidance at next tool boundary);
  `POST /v1/runs/{id}/stop`; `GET /v1/runs/{id}/events` (SSE).
- Session reads: `GET /api/sessions` (paginated), `GET /api/sessions/{id}`,
  `GET /api/sessions/{id}/messages` (paginated).
- Model override per request: `model` + `provider` fields.
- Limits: concurrent-run cap `gateway.api_server.max_concurrent_runs`
  (default 10) → HTTP 429 `Too many concurrent runs (max N)` — client
  must back off, not error-loop.
- Session ownership: API-server sessions are their own; requests naming a
  session owned by another surface (CLI/ACP/messaging) get HTTP 409
  `permission_denied` BEFORE admission. The tool must surface 409 as a
  readable message, never retry it.
- SSE keepalive: all streams emit `: keepalive` comment lines every 10s;
  parsers must skip `:`-prefixed lines.

## Frozen contracts

### Contract A — config (leaf 01 owns)

`internal/config/schema.go` new section, wired through DefaultConfig():

```go
type HermesConfig struct {
    Enabled       bool   `json:"enabled"`         // default false
    BaseURL       string `json:"base_url"`        // default "http://127.0.0.1:8642"
    APIKeyEnv     string `json:"api_key_env"`     // default "HERMES_API_SERVER_KEY"
    TimeoutSeconds int   `json:"timeout_seconds"` // default 300 (sync turns are long)
}
```

Config template entry in `config/meept.json5` with comments. The API key
NEVER lives in meept.json5 — env indirection only.

### Contract B — HTTP client (leaf 01 owns)

New package `internal/hermes/client.go`:

```go
type Client struct { ... } // baseURL, apiKey, http.Client
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client
func (c *Client) Health(ctx) error
func (c *Client) Capabilities(ctx) (*Capabilities, error) // parsed flags+endpoints
func (c *Client) ChatSync(ctx, sessionID, input string, opts) (*ChatResult, error)
func (c *Client) CreateRun(ctx, input string, opts) (runID string, error)   // 202 → run_id
func (c *Client) RunStatus(ctx, runID) (*RunStatus, error)
func (c *Client) SteerRun(ctx, runID, message) error
func (c *Client) StopRun(ctx, runID) error
func (c *Client) ListSessions(ctx, limit, offset) ([]Session, error)
func (c *Client) SessionMessages(ctx, sessionID, limit) ([]Message, error)
```

Rules:
- Bearer header set from the key string; the client NEVER logs the key.
- 429 → typed `ErrHermesBusy` (caller backs off; tool surfaces it as a
  readable busy message). 409 → typed `ErrHermesSessionForeign` (never
  retried). 401 → `ErrHermesAuth`. Network/5xx → wrapped with endpoint.
- No SSE in v1: sync chat uses the non-stream endpoint; runs poll via
  GET status. SSE is a documented non-goal for this tree.
- `Capabilities` result cached with a TTL (60s) — one probe, not per call.
- httptest-based unit tests; no live gateway in unit tier.

### Contract C — builtin tool (leaf 02 owns)

`internal/tools/builtin/hermes_agent.go`, tool name `hermes`, verb-shaped
like `acp_agent`:

- `status` — health + capabilities summary (read-only).
- `chat` — one-shot sync turn: args {session_id?, input, provider?, model?}.
  Empty session_id → create one via POST /api/sessions first, return the id.
- `run` — submit async run: args {input, provider?, model?} → run_id.
- `runs` — poll: args {run_id} → status + result when terminal.
- `steer` — args {run_id, message}.
- `stop` — args {run_id}.
- `sessions` — list (limit/offset).
- `messages` — read a session's messages.

Registration mirrors `RegisterACPTools` (`internal/daemon/acp_register.go`
pattern): a `RegisterHermesTools(reg, cfg)` no-op when disabled or key
unset. Tool description carries the security note: the remote Hermes runs
its OWN tools with its OWN approval policy — output is another agent's
word, verify before acting on it.

### Contract D — docs (leaf 03 owns)

`docs/workflows/hermes-interop.md`: setup (Hermes side env + gateway),
meept side config, tool verb reference, limits (429/409 semantics),
security note. Feature-matrix row update (`docs/feature-comparison-matrix.md`
Hermes interop cell) allowed as a one-line edit.

## Non-goals

- No SSE/streaming in this tree (poll-based runs only).
- No reverse direction (Hermes driving meept) — that is meept's existing
  MCP server or a future ACP server mode.
- No ACP adapter for Hermes.
- No credential storage in meept config files.

## Leaves

- **01** `01-config-and-client.md` — HermesConfig + internal/hermes client
  + tests. Branch: config + new package.
- **02** `02-builtin-tool.md` — `hermes` tool + daemon registration + tests.
  Depends on 01. Branch: tools + daemon.
- **03** `03-docs.md` — workflow doc + matrix cell. Depends on 02 (final
  verb list).

Wave order: 01 → 02 → 03 (serial; 02 consumes 01's API, 03 documents it).

## Tracking table

| Leaf | Status | Commit | Notes |
|------|--------|--------|-------|
| 01 | COMPLETE 2026-10-18 | 90da68e8 (+e7c37a6d gendoc) | HermesConfig + internal/hermes client; 17 client tests; e2e suite hermes-client added |
| 02 | PENDING | — | |
| 03 | PENDING | — | |

## Integration gates

- `go build` both binaries; `go test -p 2 ./internal/config/... ./internal/hermes/... ./internal/tools/... ./internal/daemon/...`
- Pre-commit 17 checks per commit; explicit-path staging only.
