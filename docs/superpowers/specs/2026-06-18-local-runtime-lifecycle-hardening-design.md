# Local Runtime Lifecycle Hardening — Design Spec

**Date:** 2026-06-18
**Status:** Draft (pending user review)
**Goal:** Harden and extend the existing local LLM runtime lifecycle management to enforce a localhost gate, only auto-start runtimes for models actually used by configured agents, emit per-model logs under `~/.meept`, share one server process across multiple models on the same port, and surface the new options in `meept config`.

---

## Context

The daemon already spawns/stops local LLM runtimes (llama.cpp, MLX) at daemon startup/shutdown. The mechanism is implemented in `internal/llm/runtime_{config,process,manager}.go`, wired into `internal/daemon/components.go` and `internal/daemon/daemon.go`, and configured via `models.json5` provider `lifecycle` blocks.

This spec does **not** redesign that mechanism — it adds four targeted behaviors and a config UI update.

### Existing pieces this spec builds on

| Component | Path | Role |
|---|---|---|
| `RuntimeLifecycleConfig` | `internal/llm/runtime_config.go:13` | Per-provider lifecycle config |
| `RuntimeConfig` | `internal/llm/runtime_config.go:50` | Validated runtime config |
| `ValidateAndNormalize` | `internal/llm/runtime_config.go:69` | Path expansion + validation |
| `RuntimeProcess` | `internal/llm/runtime_process.go:14` | Subprocess spawn/stop, PID file |
| `RuntimeManager.StartAll/StopAll` | `internal/llm/runtime_manager.go` | Per-provider lifecycle orchestration |
| Daemon wiring (register) | `internal/daemon/components.go:503-524` | Scans providers for `lifecycle` blocks |
| Daemon wiring (start) | `internal/daemon/daemon.go:744-752` | Background `StartAll` |
| Daemon wiring (stop) | `internal/daemon/daemon.go:908` | `StopAll` on shutdown |
| Config UI drilldown | `internal/configui/sections_models.go:24-43` | Provider fields (currently omits lifecycle) |

---

## Requirements

Four behaviors plus a config UI update. Each maps to one user requirement.

1. **Localhost enforcement**: only activate lifecycle when the provider's `baseURL` host is a loopback address.
2. **Agent-gated startup**: only spawn at daemon start when at least one of the provider's models is referenced by an existing agent definition, a `models.json5` slot, or a `model_aliases` target.
3. **Per-model logging**: emit one structured log per model under `~/.meept/logs/runtimes/`, plus one raw-subprocess log per spawned process.
4. **Shared process per port**: multiple models on the same `(runtime, host, port)` triplet share a single server process; new `model_paths` config supports multi-model spawn commands.
5. **`meept config` coverage**: surface all lifecycle fields in the provider drilldown; surface "in-use" status in `meept runtime status`.

---

## Section 1: Localhost Enforcement

### Behavior

When scanning providers in `internal/daemon/components.go:503-524`, before calling `RegisterConfig`, validate the provider's `baseURL` host:

- Parse `baseURL` with `net/url`.
- Accept exactly these hosts (case-insensitive): `localhost`, `127.0.0.1`, `::1`, `0:0:0:0:0:0:0:1`.
- Reject everything else: link-local, private ranges (`192.168.*`, `10.*`, etc.), public hostnames, public IPs, empty/missing baseURL.

### On rejection

Match the existing pattern for invalid runtime configs (`components.go:514-516`):

```go
logger.Warn("Skipping runtime config: baseURL is not loopback",
    "provider", providerID, "baseURL", provider.Options.BaseURL)
continue
```

Daemon startup continues. This is a warning, not a hard failure — a misconfigured lifecycle should not prevent the daemon from running.

### Implementation

New function in `internal/llm/runtime_config.go`:

```go
// IsLoopbackBaseURL reports whether the baseURL's host is a loopback address.
// Returns false for empty or unparseable URLs.
func IsLoopbackBaseURL(baseURL string) bool
```

- Uses `net/url.Parse`.
- Resolves `localhost` to loopback (any of the accepted literal forms above).
- Pure function, table-testable.

### Tests (`internal/llm/runtime_config_test.go`, append)

- `localhost`, `127.0.0.1`, `::1`, all scheme/port/path variants → true
- `0.0.0.0`, `192.168.1.5`, `api.example.com`, `8.8.8.8`, `""`, `"not a url"` → false
- IPv6 with brackets `[::1]` → true
- IPv6 link-local `fe80::1` → false

---

## Section 2: Agent-Gated Startup

### "In use" set

Collected at daemon startup, before `ContainerManager.StartAll` is called. The set contains fully-qualified model IDs in `provider/model` form (e.g. `local/lfm-code`).

Sources, in order:

1. **Agent definitions** loaded from `agents/*.toml` (`config.AgentDefinition.Model`). Only `Enabled: true` agents count.
2. **Model slots** in `models.json5`: `model`, `small_model`, `classifier_model`, `summarizer_model`.
3. **Alias expansion**: for any value above that names an alias in `model_aliases`, add every model in that alias's `models` list. Nested aliases are not chased (single-level expansion — sufficient for the current config).
4. **Disabled providers filter**: any model whose provider is in `disabled_providers` is excluded (matches existing behavior in `provider_registry.go`).

Aliased models are normalized to `provider/model` form by splitting on the first `/`. Values without a slash (rare misconfiguration) are skipped with a debug log.

### Where the gate runs

`RuntimeManager.StartAll` currently iterates all registered configs. Change: it accepts an "in-use set" and skips providers whose models are not in it.

Two options for plumbing:

- **(A)** Add a parameter: `StartAll(ctx, inUseModels map[string]struct{}) error`.
- **(B)** Setter on the manager: `SetModelsInUse(set)` called before `StartAll`.

I recommend **(B)** — it doesn't break the existing method signature, and the manager already has setters (`SetMetricsRecorder`). The daemon calls `SetModelsInUse` then `StartAll(ctx)`.

### What "models" a provider has

A provider declares its models via `models` (a `map[string]ModelDef` in `ProviderConfig`). The in-use check is: for the provider being considered, does any of its model keys, when joined as `providerID/modelKey`, appear in the in-use set?

For shared-process providers (Section 4), the gate is per-registered provider, not per-process. At spawn time, the manager iterates registered providers and checks the in-use set for each. A shared process spawns iff at least one of its participating providers passes the gate. Providers that fail the gate are debug-logged as skipped; they do not cause the shared process to fail if another participating provider passes.

### Behavior when skipped

`logger.Debug("Skipping runtime start: no model in use", "provider", providerID)`. The runtime remains registered (so `meept runtime status` reports it as "configured, not started"), but no subprocess spawns.

### Implementation

- New file `internal/llm/inuse.go` with `BuildModelsInUse(agents []*AgentDefinition, slots ModelSlots, aliases map[string]AliasDef, disabled []string) map[string]struct{}`.
- Daemon: in `components.go` after agents are loaded and before `ContainerManager.StartAll`, compute the set and call `c.ContainerManager.SetModelsInUse(set)`.
- `RuntimeManager.StartAll`: add an `inUseModels` field; skip providers not in use.

### Tests

- Unit: `BuildModelsInUse` with sample agent defs, slots, aliases, disabled list.
- Integration: `StartAll` with two providers, only one in the in-use set; verify only one spawn happens.

---

## Section 3: Per-Model Logging

### File layout

```
~/.meept/logs/runtimes/
  local-lfm-code.log                 # per-model structured events
  local-lfm-thinking-claude.log      # per-model structured events
  127.0.0.1-8080.process.log         # per-process raw subprocess output
  127.0.0.1-8080.process.log.1       # rotated backup (when present)
```

The per-model file is keyed by `<providerID>-<modelKey>` because provider IDs are unique in `models.json5` and model keys are unique within a provider. The process file is keyed by `<host>-<port>` to match the process grouping in Section 4.

### Per-model log format

Structured `log/slog` JSON lines, one per event. The model field is always present:

```json
{"ts":"2026-06-18T14:03:11Z","level":"info","model":"lfm-code","provider":"local","event":"spawn_success","pid":12345}
{"ts":"2026-06-18T14:03:21Z","level":"info","model":"lfm-code","provider":"local","event":"health_transition","healthy":true}
{"ts":"2026-06-18T14:30:00Z","level":"warn","model":"lfm-code","provider":"local","event":"restart_attempt","attempt":1}
```

Events logged:
- `register` (when the manager registers the config)
- `spawn_attempt`, `spawn_success`, `spawn_failure`
- `health_transition` (healthy → unhealthy or vice versa)
- `restart_attempt`, `restart_success`, `restart_failed`
- `stop`

For shared processes, a single process-level event (e.g. `spawn_success`) is written to **each** per-model log served by that process. Health transitions are similarly fanned out.

### Process log format

Raw subprocess stdout + stderr, line-prefixed:

```
out: 2026-06-18 14:03:10 INFO  server init complete
err: 2026-06-18 14:03:11 WARN  cache miss
```

Append mode. **Truncated** at the moment the shared process is first spawned (i.e. when `RuntimeProcess.Start` actually launches a new subprocess, not when each participating provider registers). Subsequent daemon restarts that spawn a fresh process also truncate. Merging additional providers into an already-running process does not truncate — they simply start appending to the same file.

### Rotation

Size cap per file: **10 MB**. On exceeding, rotate: `foo.log` → `foo.log.1` (replacing any existing `.1`), then truncate `foo.log`. One backup max — older history is discarded. Simple, deterministic, no third-party rotation library.

A rotation check runs in the writer wrapper after each write; if size exceeds cap, perform the rename-and-truncate.

### Implementation

- New file `internal/llm/runtime_logs.go`:
  - `type ModelLogger struct { *slog.Logger; file *os.File }`
  - `type ProcessLogger struct { out *rotatingWriter; err *rotatingWriter }` — both wrap the same underlying file with the line-prefix tagger
  - `OpenModelLogger(providerID, modelKey string) (*ModelLogger, error)`
  - `OpenProcessLogger(host, port string) (*ProcessLogger, error)`
  - `rotatingWriter` struct: `io.Writer` that checks size on each write and rotates when over cap.
- `RuntimeProcess.Start` gains two parameters: `stdout, stderr io.Writer`. Currently hardcoded to `os.Stdout`/`os.Stderr` at `runtime_process.go:49-50`.
- `RuntimeManager`:
  - On `RegisterConfig`, open one `ModelLogger` per model under the provider.
  - On process spawn, open the `ProcessLogger` for the endpoint key and pass its writers to `RuntimeProcess.Start`.
  - On `StopAll`, close all loggers.
- `RuntimeManager.attemptAutoRestart`, `HealthChecker.OnHealthChange` callbacks: write events to the relevant `ModelLogger`s.

### Backward compat

If log files can't be opened (permission denied, disk full), the daemon falls back to `os.Stdout`/`os.Stderr` and emits a daemon-level warning. A broken logger must not break runtime startup.

### Tests

- `OpenModelLogger` creates a file at the expected path.
- `rotatingWriter` rotates at 10MB and keeps exactly one `.1` backup.
- `RuntimeProcess.Start` honors passed-in writers (mock writers in tests).
- Per-model event fan-out: simulated health transition on a shared process writes to both per-model logs.

---

## Section 4: Shared Process Per Port

### Behavior

Multiple providers (or multiple models within one provider) targeting the same `(runtime, host, port)` triplet share **one** spawned process. The first provider registered for an endpoint wins; later registrations merge their models into the existing process and contribute their model paths to the spawn command.

### Config changes

Add `model_paths` as a map form alongside the legacy `model_path`:

```json5
"local": {
  "api": "openai",
  "options": { "baseURL": "http://127.0.0.1:8080/v1" },
  "lifecycle": {
    "runtime": "mlx",
    "auto_start": true,
    "auto_stop_on_exit": true,
    "pid_file": "~/.meept/run/mlx-8080.pid",
    "spawn_command": ["mlx_server", "--port", "8080", "--models", "${MODEL_PATHS_JSON}"],
    "model_paths": {
      "lfm-code":            "~/models/lfm-code-4bit",
      "lfm-thinking-claude": "~/models/lfm-thinking-claude-4bit"
    },
    "health_check": { ... },
    "restart_policy": { ... }
  },
  "models": {
    "lfm-code": { ... },
    "lfm-thinking-claude": { ... }
  }
}
```

### Backward compat

`model_path` (singular) is still supported. If `model_paths` is absent and `model_path` is set, the validation layer treats it as `model_paths: { <provider's first model key>: <model_path> }`. Existing `config/models.json5` keeps working unchanged.

### Spawn-command variable expansion

Extended in `ValidateAndNormalize` (currently only `${MODEL_PATH}` at `runtime_config.go:96-102`):

| Variable | Expansion |
|---|---|
| `${MODEL_PATH}` | First declared path (backward compat) |
| `${MODEL_PATHS}` | Space-separated list of all paths |
| `${MODEL_PATHS_JSON}` | JSON array string, e.g. `["/path/a","/path/b"]` |
| `${MODEL_PATH:<key>}` | Specific model's path |

Other `${VAR}` continues to resolve from environment.

### Process grouping

`RuntimeManager` currently keys processes by `providerID`. Change to key by `endpointKey` = `<runtime>:<host>:<port>`. The endpoint key is derived from the validated runtime config and the provider's baseURL.

`RegisterConfig` logic:

1. Compute the endpoint key for the new provider.
2. If no process exists for this key, create one (this provider "owns" the spawn).
3. If a process exists, **merge**:
   - Append this provider's `(providerID, modelKey, modelPath)` entries to the existing process's model list.
   - If `spawn_command` differs from the existing one, log a warning and keep the first one. (The user is responsible for ensuring merged providers use a spawn command that loads all declared models.)
   - Skip creating a duplicate `RuntimeProcess`.

### Health check interaction

One health checker per endpoint key (per process), unchanged. Health transitions fan out to every per-model log associated with the endpoint (Section 3).

### PID file naming

Per endpoint key, not per provider. The `pid_file` in config is used as-is for the first provider that registers the endpoint; subsequent providers' `pid_file` values are ignored (with a debug log if they differ).

### Status reporting

`RuntimeManager.Status()` returns one entry per registered provider (so `meept runtime status` shows all configured models), but each entry's `PID` field reflects the shared process's PID. A new field, `process_group`, identifies the shared process using the full endpoint key (e.g. `"llama-cpp:127.0.0.1:8080"` — `<runtime>:<host>:<port>`). Including the runtime type prefix disambiguates between runtime types (llama-cpp vs mlx) that may share a host:port.

### Implementation

- `RuntimeLifecycleConfig` gains `ModelPaths map[string]string` field.
- `ValidateAndNormalize`:
  - Accepts either `model_path` or `model_paths`; errors if both are empty.
  - Builds a normalized `map[modelKey]resolvedPath` in the returned `RuntimeConfig`.
  - Variable expansion handles the new variables.
- `RuntimeConfig` gains `EndpointKey string`, `ModelPaths map[string]string`.
- `RuntimeManager.processes` keyed by `endpointKey`.
- `RuntimeManager.RegisterConfig` rewritten with merge logic.
- `StopAll` iterates by endpoint key (not provider), so the shared process is stopped once.

### Tests

- Single-provider legacy config (`model_path` only): spawns one process, one PID file, health check passes.
- Two providers targeting the same port: only one process spawns; both per-model logs see `spawn_success`.
- Conflicting `spawn_command` on shared port: warning logged, first wins.
- `${MODEL_PATHS_JSON}` expands to the correct JSON string.
- `StopAll` sends exactly one SIGTERM per endpoint key.

---

## Section 5: `meept config` and Runtime Status Updates

### 5a. Lifecycle fields in provider drilldown

`internal/configui/sections_models.go:buildProviderItems` currently exposes only `api`, `baseURL`, `apiKey`, `timeout`. Extend the per-provider drilldown to include lifecycle fields when a lifecycle block is present, and the ability to add one when absent.

Fields to add (using existing dot-notation keypath support):

| Field key | Type | Notes |
|---|---|---|
| `lifecycle.runtime` | select | `llama-cpp` \| `mlx` |
| `lifecycle.auto_start` | boolean | |
| `lifecycle.auto_stop_on_exit` | boolean | |
| `lifecycle.model_path` | text | Legacy single-model form |
| `lifecycle.model_paths` | text | JSON5 map string for multi-model |
| `lifecycle.spawn_command` | text | Space-joined for display; raw edit supported |
| `lifecycle.pid_file` | text | |
| `lifecycle.spawn_timeout_seconds` | number | |
| `lifecycle.health_check.endpoint` | text | |
| `lifecycle.health_check.interval_seconds` | number | |
| `lifecycle.health_check.timeout_seconds` | number | |
| `lifecycle.health_check.unhealthy_threshold` | number | |
| `lifecycle.restart_policy.enabled` | boolean | |
| `lifecycle.restart_policy.max_attempts` | number | |
| `lifecycle.restart_policy.cooldown_seconds` | number | |
| `lifecycle.restart_policy.reset_after_seconds` | number |

No new TUI primitives needed — the drilldown model already supports nested keypaths (`options.baseURL` is precedent).

Writer (`internal/configui/writers.go`) must handle the `model_paths` map serialization (write as JSON5 map; read from text field with JSON5 parsing).

### 5b. In-use status in `meept runtime status`

Extend the existing `meept runtime status` output (`cmd/meept/runtime.go`) to show, for each provider with lifecycle enabled:

- The shared process group (endpoint key)
- Which of the provider's models are "in use" by agents/slots/aliases
- A `would_start` boolean reflecting the Section 2 gate

This makes "why didn't my model start?" debuggable without re-reading the config by hand.

### 5c. Documentation

- Update `docs/configuration/llm.md` with the new `model_paths` field and the localhost requirement.
- Update `docs/reference/cli.md` with the new `meept runtime status` fields.
- Update `CLAUDE.md` Configuration section with a brief note on lifecycle gating.

---

## Architecture Summary

```
Daemon startup
  ↓
Load agent definitions, models.json5 slots, model_aliases
  ↓
BuildModelsInUse() → set of "provider/model" strings
  ↓
ContainerManager.SetModelsInUse(set)
  ↓
For each provider:
  ├─ IsLoopbackBaseURL(baseURL)?            ← Section 1
  │    no → warn, skip
  │    yes ↓
  ├─ ValidateAndNormalize(lifecycle)
  │    → ModelPaths, EndpointKey, spawn vars ← Section 4
  ├─ RegisterConfig(providerID, cfg, baseURL)
  │    → merge by EndpointKey                ← Section 4
  │    → open per-model loggers              ← Section 3
  ↓
ContainerManager.StartAll(ctx)
  ├─ for each endpointKey:
  │    any model in use?                     ← Section 2
  │      no → debug, skip
  │      yes ↓
  │    spawn process → process logger        ← Section 3
  │    wait for health
  ↓
Daemon runs...
  ↓
Shutdown
  ↓
ContainerManager.StopAll(ctx)
  ├─ for each endpointKey: SIGTERM, wait, kill
  ├─ close per-model loggers
```

---

## Out of Scope

These were considered and explicitly rejected to keep this change focused:

- **Lazy start on first request** (spawn only when an agent actually invokes the provider) — possible future enhancement, but requires hooking the LLM client's request path. Defer.
- **Per-model stdin** — not useful; servers don't accept per-model stdin.
- **Cgroup/cgroups-based cleanup** — over-engineered for a single-user daemon. `Setpgid: true` + PID file + SIGTERM/SIGKILL on shutdown is sufficient.
- **Port conflict detection** (two lifecycle configs targeting the same port with different runtimes) — current warning-on-different-spawn-command behavior covers this well enough.
- **Containerization** of spawned runtimes — orthogonal; the existing `internal/runtime/` package handles containerized execution for tools, not LLM servers.

---

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Breaking existing `config/models.json5` | `model_path` singular still works (Section 4 backward compat). All current configs continue to function. |
| Shared-process model mismatches (server doesn't support multi-model) | Warn on conflicting spawn_command; documentation explains the requirement. No silent failures. |
| Per-model log explosion | 10MB rotation cap, one backup file. Worst case ~20MB per file across the runtimes directory. |
| Stale in-use set after config reload | Daemon reload re-computes `BuildModelsInUse` and calls `SetModelsInUse` again. (Existing reload path already re-runs `NewComponents`.) |
| PID file race between merged providers | First provider's `pid_file` wins; subsequent ones ignored with debug log. Documented. |
| Localhost check rejects valid IPv6 loopback variants | Test suite covers all canonical forms (`::1`, `0:0:0:0:0:0:0:1`); non-canonical forms are normalized via `net.ParseIP`. |

---

## Open Questions

None. All design decisions are settled pending user review of this spec.

---

## Success Criteria

1. A provider with `lifecycle` block whose `baseURL` is not loopback is **not** spawned, and a warning is logged.
2. A provider with `lifecycle` block whose models are not in the in-use set is **not** spawned, and a debug log is emitted; `meept runtime status` shows `would_start: false` with reason.
3. After daemon startup with at least one in-use local model, `~/.meept/logs/runtimes/<provider>-<model>.log` exists and contains at least a `register` and `spawn_success` event.
4. Two providers targeting the same `(runtime, host, port)` result in exactly one spawned subprocess; both per-model logs see the spawn event; `StopAll` sends exactly one SIGTERM.
5. `meept config` (TUI) shows lifecycle fields in the provider drilldown; saving a change writes back to `models.json5` correctly.
6. `meept runtime status` shows the in-use state and process group for each lifecycle-enabled provider.
7. All existing tests pass; new tests cover each behavior above.
