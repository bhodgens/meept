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
    // Sandbox-aware backend selection (see below).
    sandbox: {
      sandbox_backend_order: "auto", // auto | bwrap | docker | local
      require_sandbox: false,        // true = fail closed when no sandbox available
    },
  },
}
```

## Sandbox Backend Selection (sandbox_backend_order / require_sandbox)

`sandbox.sandbox_backend_order` controls which execution backend the daemon
resolves at startup:

| Order     | Behavior                                                                    |
| --------- | --------------------------------------------------------------------------- |
| `"auto"`  | Prefer the strongest confinement available: docker > bwrap > local          |
| `"bwrap"` | Use the bubblewrap backend only                                             |
| `"docker"`| Use the Docker backend only                                                 |
| `"local"` | Deliberately run unsandboxed on the host (no fallback warning is emitted)   |

### Fail-closed semantics

- `require_sandbox: false` (default): if no qualifying backend (docker,
  bwrap) is available, the daemon falls back to **unsandboxed local exec**
  and logs a loud `UNSANDBOXED local fallback` warning.
- `require_sandbox: true`: if no qualifying backend is available, the shell
  tool is wired to a **refusing backend** — every command fails with an error
  wrapping `runtime.ErrSandboxRequired`. The daemon never silently degrades
  to unsandboxed execution.

> **Posture distinction:** with `[runtime] enabled = false`, behavior is
> entirely unchanged from before sandbox resolution existed: no runtime
> manager is built and the shell tool uses direct local exec. That is a
> deliberate configuration posture, not a silent degrade; only *enabled*
> runtimes participate in fail-closed sandbox enforcement.

A backend name *qualifies* as a sandbox (`runtime.Qualifies`) when it provides
OS-level confinement: `bwrap` and `docker` qualify; `local` never does.

## Bubblewrap (bwrap) Backend

On Linux, meep can jail agent commands via [bubblewrap](https://github.com/containers/bubblewrap)
user namespaces using the external `bwrap` binary:

```bash
# Debian/Ubuntu
sudo apt-get install bubblewrap
# Fedora
sudo dnf install bubblewrap
# Arch
sudo pacman -S bubblewrap
```

The backend execs the installed binary — no code is vendored. Each command
runs in a jail assembled as:

```
bwrap --ro-bind /usr /usr --ro-bind /bin /bin \
      --ro-bind /lib /lib --ro-bind /lib64 /lib64   # each only if present
      --bind <workdir> <workdir>                    # writable workspace
      --dev /dev --proc /proc [--tmpfs /tmp]
      <extra_args...>
      -- sh -c <command>
```

Optional tuning under `[runtime]`:

```json5
{
  runtime: {
    bwrap: {
      binary_path: "bwrap",            // custom binary location
      extra_args: ["--unshare-net"],   // e.g. cut network access
      tmpfs_dirs: ["/tmp"],            // default tmpfs mounts
    },
  },
}
```

Child environments are filtered through the same `[runtime].env_policy`
(allowlist by default) as every other backend.

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
│ContainerManager │
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
mgr, err := runtime.NewContainerManager(runtime.Config{
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
- ContainerManager logs warning
- Falls back to LocalBackend automatically
- No failure - continues with local execution

## Security Considerations

- Container isolates command execution from host
- Volume binds should be explicitly configured
- Network access can be restricted via `network_mode`
- Auto-cleanup prevents container accumulation
