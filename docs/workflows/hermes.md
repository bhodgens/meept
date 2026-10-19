# Hermes interop (native HTTP client)

> Status: hermes-interop trees COMPLETE — phase 1 (leaves 01–03,
> `docs/plans/hermes-interop/`) and phase 2 (leaves p2-01..p2-05,
> `docs/plans/hermes-interop/phase2/`). This page documents the LANDED
> surface: the `[hermes]` config section (incl. `auto_approve`), the
> `internal/hermes` client (REST + session create/model + approval +
> jobs + SSE streaming), and the `hermes` builtin tool with **12
> verbs**. The upstream endpoint contracts are frozen in
> `docs/plans/hermes-interop/master.md` and `phase2/p2-master.md`.

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
a first-class capability: status probes, sync or streamed chat, async runs
with steer/stop/wait, approval relay, cron jobs, session create/read, and
model locking — all from meept agents.

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
       "auto_approve": false,                // false = `approve` verb asks the
                                             // meept user first (default);
                                             // true = forward decisions directly
     },
   }
   ```

3. Restart the daemon. When both conditions hold (enabled + key resolved),
   the `hermes` tool appears in the registry. Verify with `verb=status`.

## Tool verb reference (`hermes`)

One meta-tool, `verb`-shaped like `acp_agent`, with **12 verbs**.
Read-only verbs (`IsReadOnly` returns true): `status`, `runs`,
`sessions`, `messages`. Everything else mutates remote state: `chat`,
`run`, `steer`, `stop`, `create_session`, `set_model`, `approve`, and
`jobs` (jobs `list`/`get` are read-only per action, but the tool stays
conservatively not read-only for the `jobs` verb). Every verb is
serialized (`IsConcurrencySafe` = false) so two calls never race the
same remote session/run.

Mutating verbs are capability-gated against the gateway's cached
`/v1/capabilities` flags (see [Capability gating](#capability-gating)
below); the gate flag per verb is in the table.

| Verb | Args | Effect | Gate flag | Returns |
|------|------|--------|-----------|---------|
| `status` | — | health + capabilities summary (read-only) | — | `reachable`, `base_url`, `features`, `endpoints` (per-flag `capabilities_error` when the probe fails but health passes) |
| `chat` | `input` **(required)**, `session_id?`, `provider?`, `model?`, `session_key?`, `stream?` | one synchronous turn; **empty `session_id` auto-creates a fresh session**; `stream=true` runs it over SSE (see [Streaming](#streaming-runs-wait--chat-stream)) | `session_chat` | `session_id`, `reply`, `usage` (+ `session_created: true`, `created_session_id` when auto-created; + `progress[]` when streamed) |
| `run` | `input` **(required)**, `session_id?`, `provider?`, `model?`, `session_key?` | submit async run; `session_id` binds it to a session | `run_submission` | `run_id` (+ hint to poll with `runs`) |
| `runs` | `run_id`, `wait?`, `collect_events?` | poll a run; `wait=true` blocks on the run's event stream until terminal | — (read-only) | `run_id`, `status`, `result` (terminal output), `error`, `session_id` (+ `timeline[]` with `wait` + `collect_events`; `{cancelled: true}` when cancelled) |
| `steer` | `run_id`, `message` | mid-run guidance, applied at the next tool boundary | `run_steer` | `steered: true` |
| `stop` | `run_id` | cancel a run | `run_stop` | `stopped: true` |
| `approve` | `run_id`, `decision` (approve\|once\|session\|always\|deny), `reason?`, `confirmed?` | resolve a pending run approval; **two-phase when `auto_approve=false`** (see [Approval relay](#approval-relay--the-auto_approve-security-model)) | `run_approval_response` | `run_id`, `decision`, `sent: true` — or a `requires_confirmation` response on the first (unconfirmed) call |
| `jobs` | `action` (list\|get\|create\|pause\|resume\|run), `job_id?`, `name?`, `schedule?`, `prompt?`/`input?`, `repeat_times?` | gateway cron-job CRUD + pause/resume/run-now | — (ungated, see below) | `jobs[]` (list), job record (get/create), `{job_id, <action>: true}` (pause/resume/run) |
| `create_session` | `session_id?` (explicit id), `title?`, `system_prompt?`, `provider?`, `model?` | create a session row; an explicit id + model is LOCKED (`require_model_lock`) so an unroutable model fails loudly | — | `session_id`, `title`, `model`, `source` |
| `set_model` | `session_id`, `model` and/or `provider` | change a session's model for later turns | `session_model_lock` | `session_id`, `model`, `provider`, `route_source`, `locked` |
| `sessions` | `limit?`, `offset?` | list gateway sessions | — (read-only) | `sessions[]` (`id`, `title`, `created_at`, `model`, `source`) |
| `messages` | `session_id`, `limit?` | read one session's history | — (read-only) | `session_id`, `messages[]` (`role`, `content`) |

Every successful result carries `elapsed_ms`. Empty `limit`/`offset` omit
the query param so the gateway default applies.

### Capability gating

Before any mutating action call, the tool checks the verb's flag against
the cached `/v1/capabilities` probe (60s TTL — one probe, not per call).
A flag the gateway does not advertise returns
`hermes gateway does not advertise the <flag> capability` with **no HTTP
action call**. Flag names are pinned against upstream's
`_STATIC_FEATURE_FLAGS`: `session_chat` (chat), `run_submission` (run),
`run_steer` (steer), `run_stop` (stop), `run_approval_response`
(approve), `session_model_lock` (set_model).

Two deliberate gaps:

- **The gate fails OPEN on a probe error**: an unreachable/failing
  capabilities endpoint must not brick otherwise-working verbs — the
  action call itself will surface the real error. Only a *successful*
  probe that lacks the flag blocks the verb.
- **`jobs` is ungated**: upstream has NO per-route feature flag for the
  `/api/jobs` CRUD routes (the `jobs_admin` flag is false and unrelated
  to them), so gating jobs on it would brick a working surface.

### Session continuity: auto-create, `session_key`, and the 409 rule

`chat` no longer requires an explicit `session_id`: when it is empty, the
tool first calls `POST /api/sessions` (client `CreateSession`, title =
the input's first 40 runes) and runs the turn on the freshly-minted
session — the payload reports the id in use plus `session_created: true`
and `created_session_id`. Explicit ids keep the old path byte-identical.
Session creation happens BEFORE any stream call, so `stream=true` with an
empty `session_id` works too.

`chat` and `run` accept an optional **`session_key`** — a client-declared
memory scope sent as the `X-Hermes-Session-Key` header (never sent as an
empty value). It lets the caller pin a stable remote memory scope across
meept-side sessions.

**409 foreign-session rule:** a request naming a session owned by another
Hermes surface (CLI/ACP/messaging) is rejected BEFORE admission
(`ErrHermesSessionForeign`). Never retry it — create a new session
instead (e.g. just call `chat` with an empty `session_id`).

### Session creation and model changes

`create_session` exposes session creation directly (optional `title`,
`system_prompt`, `provider`/`model`). When an explicit `session_id` arrives
together with a `model`/`provider`, the tool sends
`require_model_lock: true`: the gateway refuses an unroutable selection
(409 `model_lock_unavailable`) instead of silently falling back to its
global model.

`set_model` changes a session's model for subsequent turns via
`POST /api/sessions/{id}/model` (upstream `session_model_lock` endpoint).
It is capability-gated as described above: when the cached probe succeeds
and the gateway does not advertise `session_model_lock`, the tool returns
the readable capability error without any mutating call.

### Approval relay + the `auto_approve` security model

`verb=approve` resolves a run approval that is pending on the REMOTE
gateway (`POST /v1/runs/{id}/approval`). Because forwarding a decision
lets the remote agent run its own tools, the verb is confirmation-gated
by `hermes.auto_approve` (default **false**):

- **`auto_approve=false` (default):** the first call — before ANY HTTP,
  including the capabilities probe — returns the standard phase-1
  `requires_confirmation` response (`confirm_arg: "confirmed"`, details
  carry `run_id`/`decision`/`reason`), which the TUI ConfirmationInterceptor
  / meept user sees and confirms. Only the `confirmed=true` re-invoke
  forwards the decision to the gateway.
- **`auto_approve=true`:** decisions go straight through with no
  meept-side confirmation (headless/trusted-gateway setups only).

Decisions are `approve | once | session | always | deny`, validated
locally; the client normalizes `approve` (and `approved`/`allow`) to the
canonical upstream choice `once`. The wire body is
`{"choice": "once"|"session"|"always"|"deny"}` (pinned against upstream
`_handle_run_approval`); an optional `reason` rides the body for audit
even though upstream currently ignores it. A 409 (approval not pending /
run not active) classifies via the shared conflict path.

The `approve` verb is itself gated on the `run_approval_response`
capability flag — but only on the confirmed re-invoke (or when
`auto_approve` is on); an unconfirmed call never touches the network.

### Streaming (`runs wait`, `chat stream`)

Phase 2 added an SSE reader (`internal/hermes/sse.go`):
`StreamRunEvents` (`GET /v1/runs/{id}/events`) and `StreamChat`
(`POST /api/sessions/{id}/chat/stream`). Streams are NOT bounded by the
sync-turn timeout — the caller's ctx is the only bound — and `: keepalive`
comment lines (upstream emits them every 10s) are skipped.

Tool-level usage:

- **`runs` with `wait=true`** blocks on the run's event stream until the
  terminal `run.*` event instead of polling once. `collect_events=true`
  additionally returns a `timeline` array of the run's
  `tool.started`/`tool.completed`(/`tool.failed`) events in stream order
  plus the terminal event.
- **`chat` with `stream=true`** runs the turn over SSE: `assistant.delta`
  text is aggregated into `reply` and the tool timeline rides a `progress`
  array. The result shape otherwise equals the sync chat payload.
- Without `wait`/`stream`, the legacy poll/sync paths are untouched
  (byte-identical results).

Terminal-event semantics (both verbs):

| Terminal event | Result |
|----------------|--------|
| `run.completed` | success (`runs wait`: final status/result re-polled from the run document, degrading to the event payload if the follow-up poll fails) |
| `run.failed` | **error** carrying the payload's `message` (fallback `error` field) |
| `run.cancelled` | **NOT an error**: a success result with `status: "cancelled"` and `cancelled: true` |

A stream that ends without a terminal event (and no ctx cancel) is
reported as an error; a ctx cancel mid-stream surfaces the ctx error.

### Jobs verbs

`verb=jobs` drives the gateway's cron surface (`/api/jobs` CRUD +
pause/resume/run-now) via `action`:

- `list` — all jobs; `get` — one by `job_id` (both read-only).
- `create` — requires `name`, `schedule`, and a prompt (`prompt` arg, or
  `input` as alias); optional `repeat_times` (omit = repeat forever).
  `schedule` accepts interval text (`"30m"`), natural language
  (`"every day at 9am"`), a cron expression, or an ISO timestamp.
- `pause` / `resume` / `run` — require `job_id`; `run` triggers an
  immediate run.

Job records carry `id`, `name`, `prompt`, `schedule` (flattened display
text), `status` (upstream `state`: scheduled/paused/completed/error),
`enabled`, and — when present — `next_run_at`, `last_run_at`,
`last_status`.

## Client method map (`internal/hermes`)

| Method | Upstream route | Notes |
|--------|----------------|-------|
| `Health(ctx)` | `GET /health` | reachability probe |
| `CreateSession(ctx, opts)` | `POST /api/sessions` | create session row; opts: id/title/system_prompt/provider/model/require_model_lock; 201 -> `SessionDetail` |
| `SetSessionModel(ctx, sessionID, provider, model)` | `POST /api/sessions/{id}/model` | model lock for later turns; always sends `require_model_lock: true` |
| `Capabilities(ctx)` | `GET /v1/capabilities` | feature flags + endpoint table; **60s TTL cache** — one probe, not per call |
| `ChatSync(ctx, sessionID, input, opts)` | `POST /api/sessions/{id}/chat` | one synchronous turn; response object `hermes.session.chat.completion` with `message.content`; `opts.SessionKey` -> `X-Hermes-Session-Key` |
| `StreamChat(ctx, sessionID, input, opts)` | `POST /api/sessions/{id}/chat/stream` | SSE streaming turn; `<-chan Event` (sse.go) |
| `CreateRun(ctx, input, opts)` | `POST /v1/runs` | 202 → `run_id`; `opts.SessionKey` -> `X-Hermes-Session-Key` |
| `RunStatus(ctx, runID)` | `GET /v1/runs/{id}` | poll-based status |
| `StreamRunEvents(ctx, runID)` | `GET /v1/runs/{id}/events` | SSE run lifecycle stream (sse.go); closes after the terminal `run.*` event or on ctx cancel |
| `SteerRun(ctx, runID, message)` | `POST /v1/runs/{id}/steer` | mid-run guidance at next tool boundary |
| `StopRun(ctx, runID)` | `POST /v1/runs/{id}/stop` | |
| `RunApproval(ctx, runID, decision, reason)` | `POST /v1/runs/{id}/approval` | body `{"choice": ...}`; decision normalized (`approve` -> `once`) and validated locally |
| `ListSessions(ctx, limit, offset)` | `GET /api/sessions` | paginated; `offset=0` omitted (gateway default) |
| `SessionMessages(ctx, sessionID, limit)` | `GET /api/sessions/{id}/messages` | paginated |
| `ListJobs(ctx)` | `GET /api/jobs` | cron jobs; schedule dict flattened to display text |
| `CreateJob(ctx, j)` | `POST /api/jobs` | |
| `GetJob(ctx, id)` | `GET /api/jobs/{id}` | |
| `PauseJob(ctx, id)` | `POST /api/jobs/{id}/pause` | |
| `ResumeJob(ctx, id)` | `POST /api/jobs/{id}/resume` | |
| `RunJob(ctx, id)` | `POST /api/jobs/{id}/run` | trigger an immediate run |

Non-goals: no bus-event relay of Hermes progress into meept WS clients
(streaming results carry a tool timeline in the tool result only — live
relay is a future tree); no reverse direction (Hermes driving meept); no
`admin_config_rw` / `memory_write_api` verbs (upstream flags are false);
no ACP adapter for Hermes; no credential storage in meept config files.

## Error semantics

The tool translates typed client errors into model-readable messages via
`translateHermesError`; the typed sentinel survives translation (a
`%w:%w` wrap), so `errors.Is` keeps working for programmatic callers.

| Condition | Typed sentinel | Tool message | What the caller should do |
|-----------|----------------|--------------|---------------------------|
| HTTP 429 (concurrent-run cap, gateway default 10) | `hermes.ErrHermesBusy` | `hermes busy (concurrent-run cap), retry shortly` | **back off**, never error-loop |
| HTTP 409 `permission_denied` (session owned by another Hermes surface: CLI/ACP/messaging) | `hermes.ErrHermesSessionForeign` | `session belongs to another hermes surface (CLI/ACP/messaging) — create a new session` | **never retry**; use a different session |
| HTTP 409 `session_exists` (explicit id already taken at create) | `hermes.ErrHermesSessionExists` | `hermes session id already exists — pick another id or omit it to auto-mint one` | retry with a different (or absent) id |
| HTTP 401 (bad/missing key) | `hermes.ErrHermesAuth` | `hermes auth rejected — check HERMES_API_SERVER_KEY (the env var named by hermes.api_key_env)` | fix the env key, restart/re-register |
| other non-2xx / classified 5xx | `*hermes.APIError` (endpoint + status + truncated body) | `hermes gateway <base_url>: …` | inspect status/body |
| network failure | wrapped transport error | `hermes gateway <base_url>: …` | check gateway reachability |

Argument validation runs BEFORE any network call: wrong verb, missing
`input` (chat/run), missing `session_id` (messages/set_model), missing
`run_id` (runs/steer/stop/approve), missing `message` (steer), missing
`model`+`provider` (set_model), an unknown `decision` (approve), or bad
jobs args (unknown `action`, missing `job_id` for get/pause/resume/run,
missing `name`/`schedule`/prompt for create) each return a specific
error string without touching the gateway. So does the approve
confirmation gate: with `auto_approve=false` and no `confirmed=true`,
the `requires_confirmation` response is returned before ANY HTTP.

## Security

- The API key travels ONLY as `Authorization: Bearer …` on each request.
- The key is NEVER included in any error string: the client's
  `classifyError` strips an echoed key from upstream error bodies as
  belt-and-braces, no log path in the package handles the key at all, and
  the tool's translated error strings never contain it. Only the base URL
  travels in errors.
- Config stores only the env-var NAME (`api_key_env`), never the key.
- **Approval relay is confirmation-gated by default**: with
  `hermes.auto_approve=false` (the shipped default) the `approve` verb
  cannot forward a decision to the remote gateway without an explicit
  meept-side user confirmation (`requires_confirmation` ->
  `confirmed=true` re-invoke). Flipping `auto_approve=true` is a
  deliberate trust decision: it lets the calling agent resolve remote
  approvals — and thereby let the REMOTE agent run its tools — with no
  human in the loop. See
  [Approval relay](#approval-relay--the-auto_approve-security-model).

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
- **SSE streams are ctx-bounded only**: the stream client has no HTTP
  timeout (a long run would be killed mid-flight); `: keepalive` comment
  lines are skipped and single lines are capped at 4 MiB. A cancelled
  run is a result (`cancelled: true`), never an error.
- **Capability probe degradation**: the verb gate fails OPEN on a probe
  error — only a successful probe missing the flag blocks a verb (see
  Capability gating).
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

- (landed, phase 2) approval relay (`approve` + `auto_approve`), SSE
  streaming (`runs wait`/`collect_events`, `chat stream`), jobs verbs,
  capability gating, `session_key` continuity, `CreateSession` + `chat`
  auto-create, `SetSessionModel` + `set_model` — the v1
  explicit-session_id limitation is removed.
- Future tree: live relay of Hermes stream progress into meept WS
  clients (bus events), beyond the tool-result timeline shipped here.
