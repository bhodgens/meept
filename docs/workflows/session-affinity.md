# Session affinity and upstream teardown

Session affinity lets affinity-aware inference backends (llama-server and
similar KV-cache servers) pin one conversation to one GPU/slot so cached
prompt state stays warm. Meept sends a stable per-session identifier with
every LLM request, prefers the endpoint that last served a session during
failover, and asks the backend to release that session's cache when the
session ends.

## The problem

A KV cache (the model's per-conversation token cache on the server) is
only useful while requests for the same conversation keep landing on the
same server process. Without an identifier on the wire:

- load-balanced gateways round-robin every request, so a llama-server's
  cached tokens are discarded and every turn re-processes the full prompt;
- alias failover silently moves a session to a different endpoint even
  when its usual endpoint is healthy again;
- nothing ever tells the backend "this conversation is done, release the
  slot", so caches linger until the server restarts.

## Configuration: `llm.session_affinity`

All knobs live in `meept.json5` under `llm.session_affinity` (defaults
shown):

```json5
llm: {
  session_affinity: {
    enabled: true,            // send session headers on all clients
    header: "session_id",     // OpenAI-compatible header name
    anthropic_header: "session_id", // Anthropic header name
    idle_release: "30m",      // no LLM call for this long => teardown
    teardown_timeout: "5s",   // per-teardown HTTP call timeout
  },
}
```

Setting `enabled: false` turns everything off. Setting `header` or
`anthropic_header` to `""` disables that one client's header while keeping
teardown active.

## What is actually sent

- **OpenAI-compatible clients** set the configured header on every
  streaming and non-streaming request when the request carries a session
  scope.
- **Anthropic clients** do the same with `anthropic_header`; a
  `${session_id}` placeholder in a model's `extra_headers` is replaced
  with the real session id.
- **Codex** keeps its wire-mandated `session_id` header (pre-existing
  behavior).

The header value is the turn's **accounting session id** — for
interactive turns that is the session's conversation id (`conv-…`); it is
stable across every call of one turn, which is what a backend needs.

**Empty scope sends no header.** Internal calls that carry no session
scope (startup probes, unscoped utilities) omit the header entirely — it
is never sent empty.

## Sticky rotation

When a request succeeds, the serving endpoint (host + credential
identity) is remembered per session (in memory only, capped at 1024
sessions). During alias resolution a **healthy** remembered endpoint is
tried first, so a brief failover does not permanently move a session off
its warm endpoint. Precedence is fixed:

```
quota blocks > endpoint cooldowns > alias blocks > stickiness
```

The preference is overwritten on every success; restarts clear it.

## Upstream teardown

When a session's upstream resources should be released — the user
archives or deletes the session, the session goes idle past
`idle_release`, or the daemon shuts down — meept POSTs the verified
llama-server slot-erase route:

```
POST <base-url>/slots/0?action=erase
```

- Teardown is **best-effort**: an erase failure is logged and never fails
  a turn, an archive, or a shutdown.
- Endpoints shared by several live sessions are erased only when the
  last session on them goes away (reference-counted).
- **Cloud providers are a documented no-op**: there is no upstream
  session API to call, so the cloud adapter exists in code to make that
  decision visible, and fires nothing.

### `--slot-save-path` caveat

llama-server only implements slot save/restore/erase when it was started
with `--slot-save-path`; without that flag the erase route answers
**501 NOT_SUPPORTED** and meept logs the failure (best-effort, never
fatal). Meept's managed spawn commands do not set `--slot-save-path`
today, so operators running managed local runtimes should either add the
flag to `spawn_command` themselves or expect the (harmless) 501 in the
daemon log.

## Limits

- **Affinity is a request, not a guarantee.** Meept sends the header and
  prefers the remembered endpoint; the backend (or any gateway in
  between) decides whether to honor either. Unknown headers are ignored
  by gateways that do not support them, which is why sending is safe by
  default.
- The sticky map is in-memory: a daemon restart forgets all
  preferences (the first call re-arms them).
- Idle teardown depends on the idle reaper's 1-minute check interval; a
  short `idle_release` still fires within about a minute of crossing the
  threshold.
- Session IDs and endpoint keys never appear in Info-level logs.

## Test coverage

- `e2e/suites/session-affinity/` — headers carry the right session on
  every scoped request (two sessions, header stub), empty-scope calls
  send no header, and the sticky-rotation proof runs for real: fail
  member A, the session moves to B; fail B, the session moves back to
  the recovered A (the memory follows the serving endpoint on every
  success and never pins past a failure).
- `e2e/suites/session-teardown/` — archive fires exactly one erase,
  the daemon-stop sweep fires one erase per active session (no
  reference count at shutdown), and a cloud-configured provider fires
  none. The adapter/coordinator unit behavior is covered in
  `internal/llm`'s teardown tests, and idle once-per-episode semantics
  in `internal/session`'s lifecycle tests.
