# Hermes interop (native HTTP client)

> Status: hermes-interop tree COMPLETE (leaves 01–03,
> `docs/plans/hermes-interop/`). This page documents the LANDED surface:
> the `[hermes]` config section (leaf 01), the `internal/hermes` client
> (leaf 01), and the `hermes` builtin tool with 8 verbs (leaf 02). The
> upstream endpoint contract is frozen in
> `docs/plans/hermes-interop/master.md`.

## Overview

Meept can natively drive a running **Hermes gateway API Server** over HTTP:
meept is the CLIENT, Hermes is the remote agent. The client lives in
`internal/hermes` (plain `net/http`, typed sentinel errors checked via
`errors.Is`, mirroring `internal/llm`'s error style). The user-facing
surface is the `hermes` builtin tool
(`internal/tools/builtin/hermes_agent.go`), registered by
`RegisterHermesTools` (`internal/daemon/hermes_register.go`, wired at
`internal/daemon/components.go:2850`) — registered ONLY when
`[hermes].enabled` is true AND the API key resolves from the env var named
by `api_key_env` (nil-safe no-op for a nil registry, disabled config, or
absent key; a missing key is a valid configuration state, logged at Debug).

## Problem

Without native interop, consulting a Hermes instance from meept requires
manual curl orchestration. The `hermes` integration makes a running gateway
a first-class capability: status probes, one-shot sync chat, async runs
with steer/stop, and session reads — all from meept agents.

## Setup

### Hermes side

1. In the Hermes agent's `.env` (usually `~/.hermes/.env`):

   ```bash
   API_SERVER_ENABLED=true
   API_SERVER_KEY=<a long random secret>
   ```

2. Run the Hermes gateway (`hermes gateway run` / however the instance is
   normally started). The API Server listens on the gateway process,
   default **`http://127.0.0.1:8642`**.

### Meept side

1. Export the SAME key value in meept's environment, under the env var
   named by `hermes.api_key_env` (default `HERMES_API_SERVER_KEY`):

   ```bash
   export HERMES_API_SERVER_KEY=<the same API_SERVER_KEY value>
   ```

   The key NEVER lives in `meept.json5` — env indirection only.

2. Enable the section in `~/.meept/meept.json5` (schema:
   `config.HermesConfig`; template shipped in `config/meept.json5`):

   ```json5
   {
     "hermes": {
       "enabled": true,                      // master switch; default false
       "base_url": "http://127.0.0.1:8642",  // Hermes gateway default
       "api_key_env": "HERMES_API_SERVER_KEY", // env indirection ONLY
       "timeout_seconds": 300,               // sync chat turns are long
     },
   }
   ```

3. Restart the daemon. When both conditions hold (enabled + key resolved),
   the `hermes` tool appears in the registry. Verify with `verb=status`.

## Tool verb reference (`hermes`)

One meta-tool, `verb`-shaped like `acp_agent`. Read-only verbs:
`status`, `runs`, `sessions`, `messages` (`IsReadOnly` returns true —
they only read remote state). `chat`, `run`, `steer`, and `stop` mutate
remote state. Every verb is serialized (`IsConcurrencySafe` = false) so
two calls never race the same remote session/run.

| Verb | Args | Effect | Returns |
|------|------|--------|---------|
| `status` | — | health + capabilities summary (read-only) | `reachable`, `base_url`, `features`, `endpoints` (per-flag `capabilities_error` when the probe fails but health passes) |
| `chat` | `session_id` **(required)**, `input`, `provider?`, `model?` | one synchronous turn | `session_id`, `reply`, `usage` |
| `run` | `input`, `provider?`, `model?` | submit async run | `run_id` (+ hint to poll with `runs`) |
| `runs` | `run_id` | poll a run | `status`, `result` (when terminal), `error` |
| `steer` | `run_id`, `message` | mid-run guidance, applied at the next tool boundary | `steered: true` |
| `stop` | `run_id` | cancel a run | `stopped: true` |
| `sessions` | `limit?`, `offset?` | list gateway sessions | `sessions[]` (`id`, `title`, `created_at`) |
| `messages` | `session_id`, `limit?` | read one session's history | `messages[]` (`role`, `content`) |

Every successful result carries `elapsed_ms`. Empty `limit`/`offset` omit
the query param so the gateway default applies.

### v1 limitation: `chat` REQUIRES an explicit session_id

The landed client (leaf 01) has no create-session route, so `chat` cannot
open a new Hermes session: omitting or emptying `session_id` returns
`"session_id is required for this verb"` (validated before any network
call). Discover existing ids with `verb=sessions`, then chat against one.
Auto-create (upstream `POST /api/sessions`) is a recorded follow-up in
`docs/plans/hermes-interop/master.md` (Contract C's original shape:
empty `session_id` → create one, return the id).

## Client method map (`internal/hermes`)

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
(`: keepalive` every 10s) therefore never reach this client. No reverse
direction (Hermes driving meept); no ACP adapter for Hermes; no
credential storage in meept config files.

## Error semantics

The tool translates typed client errors into model-readable messages via
`translateHermesError`; the typed sentinel survives translation (a
`%w:%w` wrap), so `errors.Is` keeps working for programmatic callers.

| Condition | Typed sentinel | Tool message | What the caller should do |
|-----------|----------------|--------------|---------------------------|
| HTTP 429 (concurrent-run cap, gateway default 10) | `hermes.ErrHermesBusy` | `hermes busy (concurrent-run cap), retry shortly` | **back off**, never error-loop |
| HTTP 409 `permission_denied` (session owned by another Hermes surface: CLI/ACP/messaging) | `hermes.ErrHermesSessionForeign` | `session belongs to another hermes surface (CLI/ACP/messaging) — create a new session` | **never retry**; use a different session |
| HTTP 401 (bad/missing key) | `hermes.ErrHermesAuth` | `hermes auth rejected — check HERMES_API_SERVER_KEY (the env var named by hermes.api_key_env)` | fix the env key, restart/re-register |
| other non-2xx / classified 5xx | `*hermes.APIError` (endpoint + status + truncated body) | `hermes gateway <base_url>: …` | inspect status/body |
| network failure | wrapped transport error | `hermes gateway <base_url>: …` | check gateway reachability |

Argument validation runs BEFORE any network call: wrong verb, missing
`input` (chat/run), missing `session_id` (chat/messages), missing
`run_id` (runs/steer/stop), or missing `message` (steer) each return a
specific error string without touching the gateway.

## Security

- The API key travels ONLY as `Authorization: Bearer …` on each request.
- The key is NEVER included in any error string: the client's
  `classifyError` strips an echoed key from upstream error bodies as
  belt-and-braces, no log path in the package handles the key at all, and
  the tool's translated error strings never contain it. Only the base URL
  travels in errors.
- Config stores only the env-var NAME (`api_key_env`), never the key.

> **Remote-agent caution:** the remote Hermes runs its OWN tools under
> its OWN approval policy. Every payload the `hermes` tool returns is
> another agent's report, not ground truth — the calling agent must
> verify before acting on it. (This note is carried in the tool's
> description, on purpose, so the model sees it at call time.)

## Edge Cases

- **429 busy**: the gateway's concurrent-run cap is an expected condition,
  not a failure — callers back off; the tool surfaces a readable busy
  message.
- **409 foreign session**: requests naming a session owned by another
  Hermes surface are rejected BEFORE admission and must never be retried.
- **Capabilities degradation**: `Capabilities().HasFeature(...)` returns
  false for unknown flags; callers degrade per-flag. `status` reports
  `capabilities_error` instead of failing when only the capabilities probe
  fails.
- **Empty pagination values**: `limit <= 0` / `offset == 0` omit the query
  param so the gateway default applies.
- **Error-body truncation**: upstream error bodies are read capped (4 KiB)
  and a read failure still classifies by status.
- **Registration gating**: `[hermes] enabled` without a resolvable key is
  a valid state — the tool family is simply absent (Debug-logged), and the
  tool itself answers `hermes disabled: no client configured` if reached
  without a client.

## Observability

The package is quiet by design: no logging on success paths, and the only
diagnostic (`hermesDebugLog`) prints to stderr exclusively when
`MEEPT_HERMES_DEBUG` is set — endpoint names and error text only, never
configuration values. Request-level visibility comes from the tool, which
surfaces typed errors as tool output and stamps every success with
`elapsed_ms`.

## Follow-ups

- `internal/hermes` client: add `CreateSession` (`POST /api/sessions`) +
  `chat` verb auto-create when `session_id` is empty (removes the v1
  explicit-session_id requirement documented above).
