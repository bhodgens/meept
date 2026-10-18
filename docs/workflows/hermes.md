# Hermes interop (native HTTP client)

> Status: leaf 01 of the hermes-interop tree
> (`docs/plans/hermes-interop/`). Leaf 02 adds the `hermes` builtin tool;
> leaf 03 finalizes this doc's verb reference. The upstream endpoint
> contract is frozen in `docs/plans/hermes-interop/master.md`.

## Overview

Meept can natively drive a running **Hermes gateway API Server** over HTTP:
meept is the CLIENT, Hermes is the remote agent. The client lives in
`internal/hermes` (plain `net/http`, typed sentinel errors checked via
`errors.Is`, mirroring `internal/llm`'s error style).

This page documents the config section and the client package (leaf 01).
The user-facing surface is the `hermes` builtin tool (leaf 02).

## Problem

Without native interop, consulting a Hermes instance from meept requires
manual curl orchestration. The `hermes` integration makes a running gateway
a first-class capability: status probes, one-shot sync chat, async runs
with steer/stop, and session reads — all from meept agents.

## Behavior

### Client (`internal/hermes`)

| Method | Upstream route | Notes |
|--------|----------------|-------|
| `Health(ctx)` | `GET /health` | reachability probe |
| `Capabilities(ctx)` | `GET /v1/capabilities` | feature flags + endpoint table; **60s TTL cache** — one probe, not per call |
| `ChatSync(ctx, sessionID, input, opts)` | `POST /api/sessions/{id}/chat` | one synchronous turn; response object `hermes.session.chat.completion` with `message.content` |
| `CreateRun(ctx, input, opts)` | `POST /v1/runs` | 202 → `run_id` |
| `RunStatus(ctx, runID)` | `GET /v1/runs/{id}` | poll-based; no SSE |
| `SteerRun(ctx, runID, message)` | `POST /v1/runs/{id}/steer` | mid-run guidance at next tool boundary |
| `StopRun(ctx, runID)` | `POST /v1/runs/{id}/stop` | |
| `ListSessions(ctx, limit, offset)` | `GET /api/sessions` | paginated; `offset=0` omitted (gateway default) |
| `SessionMessages(ctx, sessionID, limit)` | `GET /api/sessions/{id}/messages` | paginated |

Non-goals (this tree): no SSE/streaming — sync chat uses the non-stream
endpoint and runs poll via `GET` status. Gateway SSE keepalives
(`: keepalive` every 10s) therefore never reach this client.

### Typed errors

| Sentinel | Trigger | Caller contract |
|----------|---------|-----------------|
| `ErrHermesBusy` | HTTP 429 (concurrent-run cap, default 10) | back off, never error-loop |
| `ErrHermesSessionForeign` | HTTP 409 `permission_denied` (session owned by another Hermes surface) | surface as readable message, **never retry** |
| `ErrHermesAuth` | HTTP 401 (bad/missing key) | check the env-configured key |
| `*hermes.APIError` | other non-2xx / classified 5xx | carries endpoint + status + truncated body |

Check with `errors.Is`. Network failures wrap the transport error and name
the endpoint.

### Security

- The API key travels ONLY as `Authorization: Bearer …` on each request.
- The key is NEVER included in any error string: `classifyError` strips an
  echoed key from upstream error bodies as belt-and-braces, and no log
  path in the package handles the key at all.
- Config stores only the env-var NAME (`api_key_env`), never the key.

## Configuration

`[hermes]` section in `~/.meept/meept.json5` (schema:
`config.HermesConfig`):

```json5
{
  "hermes": {
    "enabled": false,                     // master switch; default false
    "base_url": "http://127.0.0.1:8642",  // Hermes gateway default
    "api_key_env": "HERMES_API_SERVER_KEY", // env indirection ONLY
    "timeout_seconds": 300,               // sync chat turns are long
  },
}
```

Hermes side (its own `.env`): `API_SERVER_ENABLED=true` +
`API_SERVER_KEY=…`, then export the same key value as
`HERMES_API_SERVER_KEY` in meept's environment.

## Edge Cases

- **429 busy**: the gateway's concurrent-run cap is an expected condition,
  not a failure — callers back off; the tool surfaces a readable busy
  message.
- **409 foreign session**: requests naming a session owned by another
  Hermes surface are rejected BEFORE admission and must never be retried.
- **Capabilities degradation**: `Capabilities().HasFeature(...)` returns
  false for unknown flags; callers degrade per-flag.
- **Empty pagination values**: `limit <= 0` / `offset == 0` omit the query
  param so the gateway default applies.
- **Error-body truncation**: upstream error bodies are read capped (4 KiB)
  and a read failure still classifies by status.

## Observability

The package is quiet by design: no logging on success paths, and the only
diagnostic (`hermesDebugLog`) prints to stderr exclusively when
`MEEPT_HERMES_DEBUG` is set — endpoint names and error text only, never
configuration values. Request-level visibility comes from the caller
(leaf 02's tool surfaces typed errors as tool output).
