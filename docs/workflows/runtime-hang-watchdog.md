# Runtime Hang Watchdog (canary + self-watchdog)

Local LLM runtimes (llama-server, mlx) can enter a state where the process
is alive, the socket is held, and `/health` answers 200 — but inference
never completes. A `/health`-only checker reports such a runtime as
forever healthy (observed live 2026-10-25: a wedged 8B runtime served
`/health` for 2h15m while every completion timed out). The
llama-hang-watchdog layer closes that gap with two independent mechanisms
in `internal/llm`:

1. **Inference canary** (`health_checker.go`): after the first post-spawn
   `/health` 200 (cold-load guard), every `canary.interval` the checker
   issues `POST {server-root}/v1/chat/completions` with `max_tokens: 1`
   and a fixed prompt, on its OWN plain `http.Client`. Success = HTTP 200
   AND a parseable body with a non-empty `choices` array. The canary
   client never touches the Resolver, the slot gate, or the call ledger —
   it writes no `llm_calls` row (it is infrastructure, not a turn).
2. **Checker self-watchdog** (`runtime_manager.go`): a 60s manager ticker
   asserts each endpoint's health run is still ticking (`RunTicks()`
   monotonic counter). No advance across two consecutive samples with the
   process alive and the checker not stopped = ERROR log `health run
   stalled`, re-arm via `hc.Start()`, and a `runtime.health`
   verdict=unhealthy detail=`watchdog re-arm` event.

## Verdict tri-state

`llm.Verdict` (`health_checker.go`) is the checker's result, reported via
`OnHealthVerdict` and `VerdictNow()`:

| Verdict | Meaning |
|---------|---------|
| `VerdictHealthy` | `/health` answering and no canary failure streak |
| `VerdictUnhealthy` | `/health` failing or the spawned process dead (classic semantics) |
| `VerdictHung` | `/health` ANSWERING but the canary failing >= threshold — the hang class |

HUNG is its own state: entering it does NOT flip the healthy flag, and it
fires exactly one `false` on the legacy bool callback so the existing
auto-restart path (`attemptAutoRestart`) runs unchanged. A post-recovery
flap guard (1s) prevents restart storms on a flapping canary: the failure
streak keeps counting inside the window, but no new HUNG transition fires
until it expires.

## Configuration surface

In the provider's `lifecycle` block in `config/models.json5` (see
`docs/configuration/llm-lifecycle.md` for the surrounding keys):

```json5
"health_check": {
  // existing fields unchanged: interval_seconds, timeout_seconds, unhealthy_threshold
  "canary": {
    "enabled": true,           // default: true for loopback llama-cpp/mlx runtimes, false otherwise
    "interval_seconds": 60,    // canary cadence; 0 => 60
    "timeout_seconds": 15,     // per-probe budget; 0 => 15
    "threshold": 3,            // consecutive canary failures => HUNG; 0 => 3
    "prompt": "ping"           // fixed completion prompt; "" => "ping"
  }
}
```

Default rule: an explicit `enabled` always wins; the ABSENT (nil) key
resolves to true iff the endpoint address is loopback AND a lifecycle
block exists (re-resolved from the stamped absolute baseURL at
registration — the lifecycle block alone often only carries a relative
`/health`, which is not loopback-decidable). Cloud endpoints and
non-loopback runtimes are untouched.

## Recovery behavior

HUNG drives recovery ONLY through the existing restart machinery: the
bool callback's single `false` edge triggers `attemptAutoRestart` →
`RestartProvider` (stop the endpoint's process group via the supervisor
path, then respawn the same `spawn_command`) under the endpoint's
`restart_policy` (enabled/max_attempts/cooldown). No hang-specific kill or
respawn path exists — the canary's only addition is knowing WHEN to fire
the existing one. The e2e proof is `e2e/suites/runtime-hang/` (hermetic
fake llama-server: healthy `/health`, wedged completions → hung event →
restart with a new pid → healthy recovery event).

## Bus events

Topic `runtime.health`, raw Publish, multi-shape payload with exactly
these keys (WS classification: `agent_progress`, never `chat_message`):

| Key | Type | Notes |
|-----|------|-------|
| `endpoint_key` | string | `<runtime>:<host>:<port>` |
| `verdict` | string | `healthy` \| `unhealthy` \| `hung` |
| `model` | string | resolved model path (may be empty) |
| `detail` | string | operator-facing reason (canary failure count/error, `watchdog re-arm`, ...) |
| `ts` | int64 | unix seconds |

Events fire on: HUNG entry (`hung`), HUNG recovery (`healthy`), and
self-watchdog re-arm (`unhealthy` + detail `watchdog re-arm`). The classic
unhealthy/crash edge deliberately does NOT publish, keeping the pre-tree
crash path byte-identical.

The manager has NO bus dependency: the daemon wires
`RuntimeManager.SetHealthEventPublisher` (nil-safe setter; publishing is a
no-op until wired) with a closure over its raw `MessageBus.Publish` — see
`internal/daemon/components.go` where `ContainerManager` is constructed.

## Operator runbook

Symptom: completions hang/timeout but the runtime "looks up"
(`meept runtime status` healthy, port listening).

What to grep for, in the daemon log (`~/.meept/meept.log`):

- `Runtime hung: /health answering but inference canary failing` — the
  ERROR that declares HUNG (carries `endpoint_key`,
  `consecutive_failures`, `last_canary_error`).
- `canary probe failing at threshold` — WARN just before the HUNG edge.
- `Runtime recovered from hung state` — INFO on the healthy recovery.
- `health run stalled` — ERROR from the self-watchdog: the checker run
  itself died/stalled and was re-armed.
- `Attempting auto-restart` / `Auto-restart succeeded` / `Auto-restart
  failed` — the restart the HUNG edge drives.

Per-model structured log (`~/.meept/logs/runtimes/<providerID>-<modelKey>.log`):
events `runtime_hung`, `runtime_hung_recovered`, and `restart_*` carry the
same detail strings.

Counters/visibility:

- Bus topic `runtime.health` (subscribe via `bus.subscribe` RPC, WS
  `agent_progress`, or HTTP bus bridge) for live hang/recovery events.
- `RuntimeManager.HangRestartCount(endpointKey)` — per-endpoint count of
  observed HUNG verdicts (in-memory; resets on daemon restart). It is a
  programmatic accessor today; no status surface or metrics counter
  exports it yet.

## Known limitations

- The publisher seam is daemon-wired (`SetHealthEventPublisher` in
  `internal/daemon/components.go`); any embedding that constructs a
  `RuntimeManager` WITHOUT calling the setter gets detection + restarts +
  logs but no bus events (nil-safe no-op by design).
- The canary consumes one real inference slot per probe — keep the
  interval generous (default 60s) on small servers with few slots.
- HUNG recovery requires the respawned runtime to answer BOTH `/health`
  and the canary; a server that comes back but keeps wedging completions
  will re-enter HUNG after the 1s flap-guard window and consume
  `restart_policy.max_attempts` before giving up (loud ERROR:
  `Auto-restart max attempts reached`).
