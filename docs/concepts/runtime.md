# Containerized Execution Backend

Meept supports optional containerized execution backends for isolated, reproducible command execution.

## Overview

The runtime package (`internal/runtime`) provides:

- **ExecutionBackend interface**: Abstracts command execution
- **LocalBackend**: Direct shell execution (default, always available)
- **DockerBackend**: Containerized execution with full isolation
- **TestHarness**: Validation pipeline for verifying changes

## When to Use Containerized Backends

✅ **Use Docker backend when:**
- You need reproducible environments across machines
- Tests require specific library versions
- You want isolation from host environment
- Running untrusted code

❌ **Stick with local backend when:**
- Performance is critical (Docker adds ~100ms overhead)
- Access to host-specific resources is needed
- Docker daemon is unavailable

## Configuration

Enable in `~/.meept/meept.json5`:

```json5
{
  runtime: {
    enabled: true,
    default_backend: "local", // or "docker"
    docker: {
      image: "golang:1.24-alpine",
      volume_binds: ["~/.meept/workspaces:/workspaces"],
      timeout_seconds: 300,
      auto_cleanup: true,
    },
  },
}
```

## Test Harness

Configure automatic validation:

```json5
{
  test_harness: {
    enabled: true,
    install_command: "go mod download",
    test_command: "go test ./... -race",
    timeout_seconds: 600,
  }
}
```

Test harness runs after code changes and before review approval.

## Architecture

```
┌─────────────────┐
│  ShellExecute   │
│     Tool        │
└────────┬────────┘
         │
┌────────▼────────┐
│ Runtime Manager │
└────────┬────────┘
         │
    ┌────┴────┐
    │         │
┌───▼──┐  ┌───▼────┐
│Local │  │ Docker │
│Backend│  │Backend │
└──────┘  └────────┘
```

## API

```go
// Create runtime manager
mgr, err := runtime.NewManager(runtime.Config{
    DefaultBackend: "local",
})

// Get backend
backend := mgr.GetDefaultBackend()

// Execute command
result, err := backend.Execute(ctx, runtime.Command{
    Cmd:     "go test ./...",
    Dir:     "/path/to/project",
    Timeout: 5 * time.Minute,
})

fmt.Printf("Exit code: %d\n", result.ExitCode)
fmt.Printf("Output: %s\n", result.Output)
```

## Graceful Degradation

If Docker backend is enabled but Docker daemon is unavailable:
- Manager logs warning
- Falls back to LocalBackend automatically
- No failure - continues with local execution

## Security Considerations

- Container isolates command execution from host
- Volume binds should be explicitly configured
- Network access can be restricted via `network_mode`
- Auto-cleanup prevents container accumulation
