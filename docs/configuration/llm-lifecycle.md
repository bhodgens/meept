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
| `model_path` | string | yes | Path to the model file (supports `~` expansion) |
| `auto_start` | bool | no | Auto-start on daemon startup (default: false) |
| `auto_stop_on_exit` | bool | no | Stop on daemon shutdown (default: true) |
| `pid_file` | string | yes | Path to PID file for process tracking |
| `spawn_command` | array | yes | Command and arguments to spawn the runtime |
| `spawn_timeout_seconds` | int | no | Timeout waiting for runtime to become healthy (default: 60) |
| `health_check.endpoint` | string | no | Health check endpoint (default: /health) |
| `health_check.interval_seconds` | int | no | Health check polling interval (default: 10) |
| `health_check.timeout_seconds` | int | no | HTTP request timeout (default: 5) |
| `health_check.unhealthy_threshold` | int | no | Consecutive failures before marking unhealthy (default: 3) |

## Variable Expansion

The `spawn_command` array supports environment variable expansion:

- `${MODEL_PATH}` - Expanded to the configured `model_path`
- `${VAR_NAME}` - Expanded from environment variables

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

1. **Daemon Startup**: The daemon scans all providers for `lifecycle` configurations. For each with `auto_start: true`, it spawns the runtime process.

2. **Health Monitoring**: A background health checker polls the runtime's HTTP endpoint every N seconds. If the runtime becomes unhealthy, it's marked but NOT automatically restarted (manual intervention required).

3. **PID File Management**: The runtime PID is stored in a file for cross-restart tracking. Stale PID files (from crashes) are automatically cleaned up on next startup.

4. **Graceful Shutdown**: On daemon exit, runtimes with `auto_stop_on_exit: true` receive SIGTERM, then SIGKILL if they don't exit within the timeout.

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
