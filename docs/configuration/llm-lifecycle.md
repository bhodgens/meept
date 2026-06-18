# LLM Runtime Lifecycle Management

Meept can automatically manage local LLM runtimes (llama.cpp or MLX), including spawning them on daemon startup, monitoring their health, and gracefully shutting them down on exit.

## Configuration

Add a `lifecycle` section to your provider configuration in `config/models.json5`:

```json5
{
  "local": {
    "api": "openai",
    "options": { "baseURL": "http://127.0.0.1:8080/v1" },
    "lifecycle": {
      "runtime": "llama-cpp",
      "model_path": "~/models/lfm-code.Q8_0.gguf",
      "auto_start": true,
      "auto_stop_on_exit": true,
      "pid_file": "~/.meept/run/llama.pid",
      "spawn_command": ["llama-server", "-m", "${MODEL_PATH}", "--port", "8080"],
      "spawn_timeout_seconds": 60,
      "health_check": {
        "endpoint": "/health",
        "interval_seconds": 10,
        "timeout_seconds": 5,
        "unhealthy_threshold": 3
      }
    }
  }
}
```

## Configuration Options

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `runtime` | string | yes | Runtime type: `llama-cpp` or `mlx` |
| `model_path` | string | see note | Path to a single model file (supports `~` expansion). Required unless `model_paths` is set |
| `model_paths` | object | see note | Map of `modelKey` → model path, for multi-model servers sharing one subprocess. Required unless `model_path` is set |
| `auto_start` | bool | no | Auto-start on daemon startup (default: false) |
| `auto_stop_on_exit` | bool | no | Stop on daemon shutdown (default: true) |
| `pid_file` | string | yes | Path to PID file for process tracking |
| `spawn_command` | array | yes | Command and arguments to spawn the runtime |
| `spawn_timeout_seconds` | int | no | Timeout waiting for runtime to become healthy (default: 60) |
| `health_check.endpoint` | string | no | Health check endpoint (default: /health) |
| `health_check.interval_seconds` | int | no | Health check polling interval (default: 10) |
| `health_check.timeout_seconds` | int | no | HTTP request timeout (default: 5) |
| `health_check.unhealthy_threshold` | int | no | Consecutive failures before marking unhealthy (default: 3) |

### `model_path` vs `model_paths`

- For single-model runtimes, use `model_path` (the legacy form). It is equivalent to `model_paths: { "default": <model_path> }`.
- For multi-model servers (e.g. MLX or llama.cpp serving several models on one port), use `model_paths`:

```json5
"lifecycle": {
  "runtime": "mlx",
  "model_paths": {
    "lfm-code":            "~/models/lfm-code-4bit",
    "lfm-thinking-claude": "~/models/lfm-thinking-claude-4bit"
  },
  "spawn_command": ["mlx_server", "--port", "8080", "--models", "${MODEL_PATHS_JSON}"]
}
```

At least one of the two fields is required. Setting both is allowed; `model_paths` takes precedence.

### Localhost requirement

The provider's `options.baseURL` must point at a loopback address (`localhost`, `127.0.0.1`, `::1`, `0:0:0:0:0:0:0:1`). Any other host is rejected at daemon startup with a warning. This applies to all lifecycle-enabled providers regardless of `auto_start`.

## Variable Expansion

The `spawn_command` array supports these expansions:

| Variable | Expansion |
|----------|-----------|
| `${MODEL_PATH}` | First declared path (backward compat with single-model configs) |
| `${MODEL_PATHS}` | Space-separated list of all paths |
| `${MODEL_PATHS_JSON}` | JSON array string, e.g. `["/path/a","/path/b"]` |
| `${MODEL_PATH:<key>}` | Specific model's path (e.g. `${MODEL_PATH:lfm-code}`) |
| `${VAR_NAME}` | Any other `${VAR}` resolves from the environment |

Example:
```json5
"spawn_command": [
  "llama-server",
  "-m", "${MODEL_PATH}",
  "--port", "8080",
  "--threads", "${LLAMA_THREADS:-4}"
]
```

## Manual Management

Use the `meept runtime` CLI command for manual control:

```bash
# Check runtime status
meept runtime status [provider]

# Start runtime
meept runtime start [provider]

# Stop runtime
meept runtime stop [provider]

# Restart runtime
meept runtime restart [provider]
```

If no provider is specified, `local` is used by default.

## How It Works

1. **Daemon Startup**: The daemon scans all providers for `lifecycle` configurations. For each provider:
   - The `options.baseURL` host must be loopback (`localhost`, `127.0.0.1`, `::1`, `0:0:0:0:0:0:0:1`). Non-loopback providers are skipped with a warning.
   - The validated config is registered against an **endpoint key** of the form `<runtime>:<host>:<port>`. Multiple providers on the same endpoint key merge into a single shared subprocess (first spawn command wins; later providers contribute their model paths).
   - At least one of the provider's models must be in the daemon-wide **in-use set** (referenced by an enabled agent, a model slot, or a model alias). Endpoints with no in-use models are skipped with a debug log.

2. **Health Monitoring**: A background health checker per endpoint polls the runtime's HTTP endpoint every N seconds. Health transitions fan out to every per-model log on the endpoint. If `restart_policy.enabled` is true, unhealthy transitions trigger an auto-restart (see [Auto-Restart Policy](#auto-restart-policy)).

3. **Per-Model Logging**: A structured JSON-line log is written per model at `~/.meept/logs/runtimes/<providerID>-<modelKey>.log`. Events: `register`, `spawn_attempt`, `spawn_success`, `spawn_failure`, `health_transition`, `restart_attempt`, `restart_success`, `restart_failed`, `stop`. Raw subprocess output goes to `~/.meept/logs/runtimes/<host>-<port>.process.log` with `out:`/`err:` line prefixes. Files rotate at 10 MB with one `.1` backup.

4. **PID File Management**: The runtime PID is stored in a file for cross-restart tracking. Stale PID files (from crashes) are automatically cleaned up on next startup. The `pid_file` of the first provider to register an endpoint wins; subsequent providers' `pid_file` values are ignored (debug log if they differ).

5. **Graceful Shutdown**: On daemon exit, each endpoint (not each provider) receives a single SIGTERM, then SIGKILL if it doesn't exit within the timeout. Health checkers are stopped and per-model/per-process log files are closed.

## Troubleshooting

### Runtime fails to start

1. Check that the model file exists at `model_path`
2. Verify the `spawn_command` is correct (try running it manually)
3. Check daemon logs for spawn errors

### Runtime marked unhealthy

1. Verify the health endpoint is accessible: `curl http://localhost:8080/health`
2. Check that the runtime process is still running: `meept runtime status`
3. Review runtime logs for crashes

### PID file errors

If you see "PID file" errors after a crash:
```bash
# Remove stale PID file
rm ~/.meept/run/llama.pid

# Restart the runtime
meept runtime restart
```

## Auto-Restart Policy

When a runtime becomes unhealthy, Meept can automatically attempt to restart it:

```json5
"restart_policy": {
  "enabled": true,
  "max_attempts": 3,
  "cooldown_seconds": 30,
  "reset_after_seconds": 300
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | false | Enable automatic restart on unhealthy |
| `max_attempts` | int | 3 | Maximum restart attempts before giving up |
| `cooldown_seconds` | int | 30 | Minimum seconds between restart attempts |
| `reset_after_seconds` | int | 300 | Reset failure count after this many seconds of healthy operation |

When `enabled: true`, the health checker monitors the runtime and triggers a restart after `unhealthy_threshold` consecutive failures. After `max_attempts` failed restarts, it stops trying and logs an error. The failure counter resets after the runtime stays healthy for `reset_after_seconds`.

## HTTP API

Runtime management is available via the HTTP API when the daemon is running:

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/runtime/status` | Status of all managed runtimes |
| GET | `/api/v1/runtime/status/{provider}` | Status of a specific provider |
| POST | `/api/v1/runtime/start/{provider}` | Start a provider's runtime |
| POST | `/api/v1/runtime/stop/{provider}` | Stop a provider's runtime |
| POST | `/api/v1/runtime/restart/{provider}` | Restart a provider's runtime |

## RPC Methods

Runtime management is also available via RPC:

| Method | Parameters | Description |
|--------|------------|-------------|
| `runtime.status` | `{"provider": "local"}` (optional) | Get runtime status |
| `runtime.start` | `{"provider": "local"}` | Start a runtime |
| `runtime.stop` | `{"provider": "local"}` | Stop a runtime |
| `runtime.restart` | `{"provider": "local"}` | Restart a runtime |

## Daemon Status

Runtime health information is included in the daemon status response (`GET /api/v1/daemon/status`) under the `runtimes` key:

```json
{
  "running": true,
  "runtimes": {
    "local": {
      "running": true,
      "healthy": true,
      "pid": 12345,
      "runtime": "llama-cpp"
    }
  }
}
```

## Metrics

Runtime lifecycle events are recorded to the metrics subsystem:

| Metric | Description |
|--------|-------------|
| `runtime.healthy` | 1.0 if healthy, 0.0 if not (per provider) |
| `runtime.spawn.duration` | Time to spawn a runtime process |
| `runtime.spawn.success` | Count of successful spawns |
| `runtime.spawn.failure` | Count of failed spawns |
| `runtime.restart.attempts` | Restart attempt number |
| `runtime.restart.success` | Count of successful restarts |
| `runtime.restart.failure` | Count of failed restarts |

## Supported Runtimes

### llama.cpp

```json5
{
  "runtime": "llama-cpp",
  "model_path": "~/models/lfm-code.Q8_0.gguf",
  "spawn_command": ["llama-server", "-m", "${MODEL_PATH}", "--port", "8080"]
}
```

### MLX (macOS)

```json5
{
  "runtime": "mlx",
  "model_path": "~/models/lfm-codemlx",
  "spawn_command": ["mlx_lm.server", "--model", "${MODEL_PATH}", "--port", "8080"]
}
```
