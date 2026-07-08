# Issue: MLX Runtime Receives SIGKILL Immediately After Health Check Passes

**Date:** 2026-07-07
**Severity:** High
**Component:** `internal/llm/runtime_manager.go`, `internal/llm/runtime_process.go`, `internal/llm/health_checker.go`

## Summary

The MLX runtime process (`mlx_lm server`) successfully starts, passes health checks, but is then immediately killed with `SIGKILL` (signal 9). This occurs consistently ~10 seconds after the server starts.

## Observed Behavior

```
time=2026-07-07T15:17:41.013-06:00 level=INFO msg="Runtime started and healthy" endpoint_key=mlx:127.0.0.1:8080
MLX process exited: exitCode=-1 err=signal: killed
```

Process log shows:
```
err: Starting httpd at 127.0.0.1 on port 8080...
err: 127.0.0.1 - - [07/Jul/2026 15:17:40] "GET /health HTTP/1.1" 200 -
<process exits immediately after>
```

## Fixes Implemented (Not Sufficient)

1. **Health Checker URL Fix** (`health_checker.go`): Fixed health checks to hit `/health` instead of `/v1/health`
2. **Missing StartAll Call** (`daemon/components.go`): Added `ContainerManager.StartAll()` to actually start runtimes
3. **Zombie Prevention** (`runtime_process.go`): Added `waitBackground()` goroutine to reap child processes
4. **Stdin Handling** (`runtime_process.go`): Set `p.cmd.Stdin = nil` to prevent stdin inheritance issues

Despite these fixes, the SIGKILL persists.

## Investigation Findings

### MLX Server Works When Started Manually

```bash
mlx_lm server --model /Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-MLX-4bit --port 8080
# Server runs indefinitely, health checks succeed
```

### SIGKILL Characteristics

- `SIGKILL` (signal 9) cannot be caught or ignored
- Only sent by: kernel (OOM), userspace (kill -9), or parent process
- System memory is fine (56% free, no OOM conditions)
- No explicit `Kill()` call in the daemon code path after successful startup

### Potential Root Causes

1. **Context Cancellation:** `exec.CommandContext(ctx, ...)` kills child if ctx is cancelled. Daemon context should remain valid, but investigation needed.

2. **Stdout/Stderr Pipe Closure:** When `StartAll()` returns, local `stdout`/`stderr` variables go out of scope. If `ProcessLogger` writers are GC'd or closed, child receives SIGPIPE/EOF.

3. **External Process Monitor:** macOS activity monitor or other external tool may be killing the process.

4. **Process Group Signaling:** `Setpgid=true` creates new process group. If daemon sends signal to its group, child shouldn't be affected, but edge cases possible.

5. **File Descriptor Inheritance:** Shared file descriptors between daemon and child may cause issues when daemon continues.

## Temporary Workaround

**Disable MLX runtime, use llama.cpp server as default.**

In `~/.meept/models.json5`:

```json5
{
  "small_model": "ollama/llama3.2",  // Changed from "local/lfm-8b-4bit"

  "providers": {
    "local": {
      "lifecycle": {
        "runtime": "llama",  // Changed from "mlx"
        "model_path": "/path/to/llama.cpp/model.gguf",
        "auto_start": false,  // Disable auto-start for now
        // ...
      }
    }
  }
}
```

Or disable the local provider entirely:

```json5
{
  "disabled_providers": ["gala-mlx", "gala-llama", "local"]
}
```

## Required Fixes

### High Priority

- [ ] Trace SIGKILL source using `dtruss` or similar macOS debugging tools
- [ ] Add detailed logging around `Start()` and `Stop()` calls
- [ ] Test with `Setpgid=false` to rule out process group issues
- [ ] Verify stdout/stderr writers remain valid after `StartAll()` returns
- [ ] Check if daemon parent process is sending signals

### Medium Priority

- [ ] Add retry logic for runtime startup failures
- [ ] Implement better error messages when runtime dies unexpectedly
- [ ] Add runtime health monitoring with automatic recovery
- [ ] Document working MLX configuration for macOS

## Related Files

- `internal/llm/runtime_manager.go` - Runtime lifecycle management
- `internal/llm/runtime_process.go` - Process spawning and waiting
- `internal/llm/health_checker.go` - Health check implementation
- `internal/llm/runtime_logs.go` - Process logging infrastructure
- `internal/daemon/components.go` - Daemon component wiring

## Workaround Status

**MLX runtime is currently disabled by default.** Users who want to use MLX must:

1. Manually edit `models.json5` to enable the local provider
2. Ensure they have sufficient system resources
3. Be prepared to debug SIGKILL issues

Default model configuration now uses remote providers (z.ai) or Ollama for local inference.

## Updates

### 2026-07-07

- Identified SIGKILL as the cause of MLX runtime failures
- Implemented health checker URL fix (was hitting `/v1/health` → 404)
- Added `ContainerManager.StartAll()` call (was missing)
- Added zombie process prevention with `waitBackground()` goroutine
- Issue persists despite fixes; root cause remains unknown
- Temporary workaround: disable MLX, use llama.cpp or remote providers
