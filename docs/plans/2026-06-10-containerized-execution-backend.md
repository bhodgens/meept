# Containerized Execution Backend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add optional containerized execution backends (Docker-first) for isolated, reproducible shell command execution with test harness validation.

**Architecture:**
- New `internal/runtime` package with `ExecutionBackend` interface
- Backend implementations: `LocalBackend` (current behavior), `DockerBackend` (containerized)
- `ShellExecuteTool` routes commands to configured backend
- Optional test harness integration for validation
- Config-driven: enabled/disabled per-daemon, per-task override

**Tech Stack:** Go 1.24+, Docker Engine API (via `go-dockerclient` or `nerdctl`), JSON5 configuration

---

### Phase 1: Core Interface and Local Backend

### Task 1: Define ExecutionBackend Interface

**Files:**
- Create: `internal/runtime/backend.go`
- Test: `internal/runtime/backend_test.go`

**Step 1: Write interface definition test**

```go
package runtime

import (
    "context"
    "testing"
    "github.com/stretchr/testify/assert"
)

// Test that ExecutionBackend interface is satisfied
func TestExecutionBackend_Interface(t *testing.T) {
    var _ ExecutionBackend = (*LocalBackend)(nil)
    // DockerBackend will be added later
}

func TestExecutionBackend_Execute(t *testing.T) {
    backend := NewLocalBackend()
    result, err := backend.Execute(context.Background(), Command{
        Cmd: "echo hello",
    })
    assert.NoError(t, err)
    assert.Contains(t, result.Output, "hello")
    assert.Equal(t, 0, result.ExitCode)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/runtime/... -v -run TestExecutionBackend
```
Expected: FAIL with "undefined: ExecutionBackend"

**Step 3: Define interface and types**

```go
// Package runtime provides isolated execution backends for shell commands.
package runtime

import (
    "context"
    "io"
    "time"
)

// Command represents a command to execute.
type Command struct {
    Cmd         string            // Shell command to run
    Dir         string            // Working directory
    Env         map[string]string // Environment variables
    Timeout     time.Duration     // Execution timeout (0 = no timeout)
    Interactive bool              // PTY mode for interactive tools
}

// CommandResult holds execution results.
type CommandResult struct {
    Output     string
    ExitCode   int
    Duration   time.Duration
    WasCached  bool // True if result came from cache
}

// ExecutionBackend defines the interface for command execution.
type ExecutionBackend interface {
    // Execute runs a command and returns the result.
    Execute(ctx context.Context, cmd Command) (*CommandResult, error)

    // Name returns the backend identifier (e.g., "local", "docker").
    Name() string

    // Close cleans up backend resources.
    Close() error
}

// Config holds runtime configuration.
type Config struct {
    // DefaultBackend is the default backend to use ("local" or "docker").
    DefaultBackend string `json:"default_backend"`
    // DockerConfig holds Docker-specific settings.
    Docker DockerConfig `json:"docker"`
}

// DockerConfig holds Docker backend configuration.
type DockerConfig struct {
    // Image is the container image to use (e.g., "golang:1.24").
    Image string `json:"image"`
    // Workdir is the working directory inside the container.
    Workdir string `json:"workdir"`
    // VolumeBinds maps host paths to container paths.
    VolumeBinds []string `json:"volume_binds"`
    // NetworkMode sets the container network mode.
    NetworkMode string `json:"network_mode"`
    // Timeout is the default command timeout.
    Timeout time.Duration `json:"timeout"`
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/runtime/... -v -run TestExecutionBackend
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/backend.go internal/runtime/backend_test.go
git commit -m "feat(runtime): define ExecutionBackend interface and base types"
```

---

### Task 2: Implement LocalBackend

**Files:**
- Create: `internal/runtime/local.go`
- Test: `internal/runtime/local_test.go`

**Step 1: Write comprehensive tests**

```go
package runtime

import (
    "context"
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestLocalBackend_Execute_Basic(t *testing.T) {
    backend := NewLocalBackend()
    result, err := backend.Execute(context.Background(), Command{
        Cmd: "echo hello",
    })
    require.NoError(t, err)
    assert.Equal(t, 0, result.ExitCode)
    assert.Contains(t, result.Output, "hello")
}

func TestLocalBackend_Execute_ExitCode(t *testing.T) {
    backend := NewLocalBackend()
    result, err := backend.Execute(context.Background(), Command{
        Cmd: "exit 42",
    })
    require.NoError(t, err)
    assert.Equal(t, 42, result.ExitCode)
}

func TestLocalBackend_Execute_WorkingDir(t *testing.T) {
    backend := NewLocalBackend()
    result, err := backend.Execute(context.Background(), Command{
        Cmd: "pwd",
        Dir: "/tmp",
    })
    require.NoError(t, err)
    assert.Contains(t, result.Output, "/tmp")
}

func TestLocalBackend_Execute_Environment(t *testing.T) {
    backend := NewLocalBackend()
    result, err := backend.Execute(context.Background(), Command{
        Cmd: "echo $MYVAR",
        Env: map[string]string{"MYVAR": "test-value"},
    })
    require.NoError(t, err)
    assert.Contains(t, result.Output, "test-value")
}

func TestLocalBackend_Execute_Timeout(t *testing.T) {
    backend := NewLocalBackend()
    ctx := context.Background()
    result, err := backend.Execute(ctx, Command{
        Cmd:     "sleep 10",
        Timeout: 100 * time.Millisecond,
    })
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "timeout")
    assert.Nil(t, result)
}

func TestLocalBackend_Close(t *testing.T) {
    backend := NewLocalBackend()
    err := backend.Close()
    assert.NoError(t, err)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/runtime/... -v -run TestLocalBackend
```
Expected: FAIL with "undefined: NewLocalBackend"

**Step 3: Implement LocalBackend**

```go
package runtime

import (
    "context"
    "fmt"
    "os/exec"
    "strings"
    "time"
)

// LocalBackend executes commands on the local system using exec.Command.
type LocalBackend struct {
    defaultEnv map[string]string
}

// NewLocalBackend creates a new local execution backend.
func NewLocalBackend() *LocalBackend {
    return &LocalBackend{
        defaultEnv: make(map[string]string),
    }
}

// Name returns the backend identifier.
func (b *LocalBackend) Name() string {
    return "local"
}

// Execute runs a command locally and returns the result.
func (b *LocalBackend) Execute(ctx context.Context, cmd Command) (*CommandResult, error) {
    if cmd.Timeout > 0 {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, cmd.Timeout)
        defer cancel()
    }

    command := exec.CommandContext(ctx, "sh", "-c", cmd.Cmd)

    // Set working directory
    if cmd.Dir != "" {
        command.Dir = cmd.Dir
    }

    // Set environment variables
    env := b.buildEnv(cmd.Env)
    command.Env = env

    start := time.Now()
    output, err := command.CombinedOutput()
    duration := time.Since(start)

    var exitCode int
    if exitErr, ok := err.(*exec.ExitError); ok {
        exitCode = exitErr.ExitCode()
        // Don't return error for non-zero exit codes - caller handles them
        err = nil
    }

    return &CommandResult{
        Output:   string(output),
        ExitCode: exitCode,
        Duration: duration,
    }, err
}

// Close cleans up resources (no-op for local backend).
func (b *LocalBackend) Close() error {
    return nil
}

// buildEnv combines default and command-specific environment.
func (b *LocalBackend) buildEnv(cmdEnv map[string]string) []string {
    // Start with current environment
    env := append([]string{}, execEnv()...)

    // Add defaults
    for k, v := range b.defaultEnv {
        env = append(env, fmt.Sprintf("%s=%s", k, v))
    }

    // Add command-specific (overrides defaults)
    for k, v := range cmdEnv {
        // Remove existing key if present
        for i, existing := range env {
            if strings.HasPrefix(existing, k+"=") {
                env[i] = fmt.Sprintf("%s=%s", k, v)
                goto found
            }
        }
        env = append(env, fmt.Sprintf("%s=%s", k, v))
        found:
    }

    return env
}

// execEnv returns the current process environment.
func execEnv() []string {
    return []string{} // Will use os.Environ() in real impl
}
```

**Step 4: Fix execEnv to actually return environment**

```go
// In internal/runtime/local.go, update execEnv:

import "os"

func execEnv() []string {
    return os.Environ()
}
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/runtime/... -v -run TestLocalBackend
```
Expected: PASS

**Step 6: Commit**

```bash
git add internal/runtime/local.go internal/runtime/local_test.go
git commit -m "feat(runtime): implement LocalBackend with exec.Command"
```

---

### Task 3: Add Runtime Manager

**Files:**
- Create: `internal/runtime/manager.go`
- Test: `internal/runtime/manager_test.go`

**Step 1: Write manager tests**

```go
package runtime

import (
    "context"
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestManager_GetBackend_Default(t *testing.T) {
    mgr, err := NewManager(Config{
        DefaultBackend: "local",
    })
    require.NoError(t, err)
    require.NotNil(t, mgr)

    backend := mgr.GetBackend("local")
    assert.NotNil(t, backend)
    assert.Equal(t, "local", backend.Name())
}

func TestManager_GetBackend_Unknown(t *testing.T) {
    mgr, err := NewManager(Config{
        DefaultBackend: "local",
    })
    require.NoError(t, err)

    backend := mgr.GetBackend("unknown")
    assert.Nil(t, backend)
}

func TestManager_GetDefaultBackend(t *testing.T) {
    mgr, err := NewManager(Config{
        DefaultBackend: "local",
    })
    require.NoError(t, err)

    backend := mgr.GetDefaultBackend()
    assert.NotNil(t, backend)
    assert.Equal(t, "local", backend.Name())
}

func TestManager_Close(t *testing.T) {
    mgr, err := NewManager(Config{
        DefaultBackend: "local",
    })
    require.NoError(t, err)

    err = mgr.Close()
    assert.NoError(t, err)

    // Subsequent GetBackend should return nil
    backend := mgr.GetBackend("local")
    assert.Nil(t, backend)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/runtime/... -v -run TestManager
```
Expected: FAIL with "undefined: NewManager"

**Step 3: Implement Manager**

```go
package runtime

import (
    "fmt"
    "sync"
)

// Manager provides access to execution backends.
type Manager struct {
    mu            sync.RWMutex
    config        Config
    backends      map[string]ExecutionBackend
    defaultBackend string
    closed        bool
}

// NewManager creates a new runtime manager.
func NewManager(cfg Config) (*Manager, error) {
    m := &Manager{
        config:   cfg,
        backends: make(map[string]ExecutionBackend),
    }

    // Initialize local backend (always available)
    local := NewLocalBackend()
    m.backends["local"] = local
    m.defaultBackend = "local"

    // Set default backend
    if cfg.DefaultBackend != "" {
        if cfg.DefaultBackend != "local" && cfg.DefaultBackend != "docker" {
            return nil, fmt.Errorf("unknown default backend: %s", cfg.DefaultBackend)
        }
        m.defaultBackend = cfg.DefaultBackend
    }

    // Initialize Docker backend if configured
    if cfg.DefaultBackend == "docker" || cfg.Docker.Image != "" {
        if err := m.initDockerBackend(); err != nil {
            // Log warning but don't fail - fall back to local
            // Caller can check which backends are available
        }
    }

    return m, nil
}

// GetBackend returns a backend by name, or nil if not available.
func (m *Manager) GetBackend(name string) ExecutionBackend {
    m.mu.RLock()
    defer m.mu.RUnlock()

    if m.closed {
        return nil
    }

    return m.backends[name]
}

// GetDefaultBackend returns the default backend.
func (m *Manager) GetDefaultBackend() ExecutionBackend {
    return m.GetBackend(m.defaultBackend)
}

// ListBackends returns names of available backends.
func (m *Manager) ListBackends() []string {
    m.mu.RLock()
    defer m.mu.RUnlock()

    names := make([]string, 0, len(m.backends))
    for name := range m.backends {
        names = append(names, name)
    }
    return names
}

// Close shuts down all backends.
func (m *Manager) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.closed {
        return nil
    }

    var lastErr error
    for name, backend := range m.backends {
        if err := backend.Close(); err != nil {
            lastErr = err
        }
        delete(m.backends, name)
    }

    m.closed = true
    return lastErr
}

// initDockerBackend initializes the Docker backend if Docker is available.
func (m *Manager) initDockerBackend() error {
    // Will be implemented in Task 4
    // For now, just return an error
    return fmt.Errorf("Docker backend not yet implemented")
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/runtime/... -v -run TestManager
```
Expected: PASS

**Step 5: Commit**

```bash
git add internal/runtime/manager.go internal/runtime/manager_test.go
git commit -m "feat(runtime): add Manager for backend lifecycle and lookup"
```

---

### Phase 2: Docker Backend Implementation

### Task 4: Implement DockerBackend

**Files:**
- Create: `internal/runtime/docker.go`
- Test: `internal/runtime/docker_test.go` (requires Docker daemon)

**Dependencies:**
```bash
go get github.com/fsouza/go-dockerclient
```

**Step 1: Write Docker backend tests (skipped without Docker)**

```go
package runtime

import (
    "context"
    "os"
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// hasDocker checks if Docker daemon is available.
func hasDocker() bool {
    return os.Getenv("TEST_DOCKER") != ""
}

func TestDockerBackend_Execute_Basic(t *testing.T) {
    if !hasDocker() {
        t.Skip("Docker not available, set TEST_DOCKER=1 to run")
    }

    backend, err := NewDockerBackend(DockerConfig{
        Image: "alpine:latest",
    })
    require.NoError(t, err)
    defer backend.Close()

    result, err := backend.Execute(context.Background(), Command{
        Cmd: "echo hello",
    })
    require.NoError(t, err)
    assert.Equal(t, 0, result.ExitCode)
    assert.Contains(t, result.Output, "hello")
}

func TestDockerBackend_Execute_WorkingDir(t *testing.T) {
    if !hasDocker() {
        t.Skip("Docker not available")
    }

    backend, err := NewDockerBackend(DockerConfig{
        Image:   "alpine:latest",
        Workdir: "/tmp",
    })
    require.NoError(t, err)
    defer backend.Close()

    result, err := backend.Execute(context.Background(), Command{
        Cmd: "pwd",
    })
    require.NoError(t, err)
    assert.Contains(t, result.Output, "/tmp")
}
```

**Step 2: Run tests (will skip without Docker)**

```bash
go test ./internal/runtime/... -v -run TestDockerBackend
```
Expected: SKIP (or PASS if Docker available)

**Step 3: Implement DockerBackend**

```go
package runtime

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "time"

    docker "github.com/fsouza/go-dockerclient"
)

// DockerBackend executes commands inside Docker containers.
type DockerBackend struct {
    client      *docker.Client
    config      DockerConfig
    containerID string
    mu          sync.Mutex
}

// NewDockerBackend creates a new Docker execution backend.
func NewDockerBackend(cfg DockerConfig) (*DockerBackend, error) {
    client, err := docker.NewClientFromEnv()
    if err != nil {
        return nil, fmt.Errorf("failed to create Docker client: %w", err)
    }

    // Pull image if not present
    if err := ensureImage(client, cfg.Image); err != nil {
        return nil, fmt.Errorf("failed to ensure Docker image: %w", err)
    }

    // Create container
    container, err := createContainer(client, cfg)
    if err != nil {
        return nil, fmt.Errorf("failed to create container: %w", err)
    }

    return &DockerBackend{
        client:      client,
        config:      cfg,
        containerID: container.ID,
    }, nil
}

// Name returns the backend identifier.
func (b *DockerBackend) Name() string {
    return "docker"
}

// Execute runs a command inside the container.
func (b *DockerBackend) Execute(ctx context.Context, cmd Command) (*CommandResult, error) {
    b.mu.Lock()
    defer b.mu.Unlock()

    timeout := b.config.Timeout
    if cmd.Timeout > 0 {
        timeout = cmd.Timeout
    }

    // Create exec instance
    execOpts := docker.CreateExecOptions{
        AttachStdout: true,
        AttachStderr: true,
        Container:    b.containerID,
        Cmd:          []string{"sh", "-c", cmd.Cmd},
        WorkingDir:   cmd.Dir,
        Env:          b.buildEnv(cmd.Env),
    }

    if timeout > 0 {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, timeout)
        defer cancel()
    }

    exec, err := b.client.CreateExec(execOpts)
    if err != nil {
        return nil, fmt.Errorf("failed to create exec: %w", err)
    }

    // Start exec
    var stdout, stderr bytes.Buffer
    startOpts := docker.StartExecOptions{
        Detach:       false,
        AttachStdout: true,
        AttachStderr: true,
        OutputStream: &stdout,
        ErrorStream:  &stderr,
    }

    if err := b.client.StartExec(exec.ID, startOpts); err != nil {
        return nil, fmt.Errorf("failed to start exec: %w", err)
    }

    // Get exit code
    inspect, err := b.client.InspectExec(exec.ID)
    if err != nil {
        return nil, fmt.Errorf("failed to inspect exec: %w", err)
    }

    output := stdout.String()
    if stderr.Len() > 0 {
        output += stderr.String()
    }

    return &CommandResult{
        Output:   output,
        ExitCode: inspect.ExitCode,
    }, nil
}

// Close removes the container.
func (b *DockerBackend) Close() error {
    b.mu.Lock()
    defer b.mu.Unlock()

    if b.containerID == "" {
        return nil
    }

    // Stop container
    if err := b.client.StopContainer(b.containerID, 5); err != nil {
        // Container may already be stopped
    }

    // Remove container
    opts := docker.RemoveContainerOptions{
        ID:            b.containerID,
        RemoveVolumes: true,
        Force:         true,
    }

    return b.client.RemoveContainer(opts)
}

// buildEnv combines environment variables.
func (b *DockerBackend) buildEnv(cmdEnv map[string]string) []string {
    env := make([]string, 0, len(cmdEnv))
    for k, v := range cmdEnv {
        env = append(env, fmt.Sprintf("%s=%s", k, v))
    }
    return env
}

// ensureImage pulls the image if not present locally.
func ensureImage(client *docker.Client, image string) error {
    // Check if image exists
    _, err := client.InspectImage(image)
    if err == nil {
        return nil // Image already present
    }

    // Pull image
    return client.PullImage(docker.PullImageOptions{
        Repository: image,
    }, docker.AuthConfiguration{})
}

// createContainer creates a container for command execution.
func createContainer(client *docker.Client, cfg DockerConfig) (*docker.Container, error) {
    hostConfig := &docker.HostConfig{
        Binds: cfg.VolumeBinds,
    }

    if cfg.NetworkMode != "" {
        hostConfig.NetworkMode = cfg.NetworkMode
    }

    container, err := client.CreateContainer(docker.CreateContainerOptions{
        Config: &docker.Config{
            Image:    cfg.Image,
            Cmd:      []string{"sleep", "infinity"}, // Keep container running
            OpenStdin: true,
        },
        HostConfig: hostConfig,
    })

    if err != nil {
        return nil, err
    }

    if err := client.StartContainer(container.ID, nil); err != nil {
        return nil, err
    }

    return container, nil
}
```

**Step 4: Add missing import**

```go
// Add to imports in docker.go
import "sync"
```

**Step 5: Run tests to verify they compile**

```bash
go test ./internal/runtime/... -v -run TestDockerBackend
```
Expected: SKIP or PASS (depending on Docker availability)

**Step 6: Commit**

```bash
git add internal/runtime/docker.go internal/runtime/docker_test.go go.mod go.sum
git commit -m "feat(runtime): implement DockerBackend with go-dockerclient"
```

---

### Phase 3: ShellExecuteTool Integration

### Task 5: Update ShellExecuteTool for Backend Routing

**Files:**
- Modify: `internal/tools/builtin/shell.go`
- Test: `internal/tools/builtin/shell_test.go`

**Step 1: Review current ShellExecuteTool**

Read `internal/tools/builtin/shell.go` to understand current structure.

**Step 2: Add runtime dependency to ShellExecuteTool**

```go
// In internal/tools/builtin/shell.go, add to imports:
"github.com/caimlas/meept/internal/runtime"

// Add field to ShellExecuteTool:
type ShellExecuteTool struct {
    backend runtime.ExecutionBackend
    manager *runtime.Manager
    logger  *slog.Logger
}

// Add constructor option:
func NewShellExecuteTool(mgr *runtime.Manager, logger *slog.Logger) *ShellExecuteTool {
    return &ShellExecuteTool{
        manager: mgr,
        backend: mgr.GetDefaultBackend(),
        logger:  logger.With("component", "shell-tool"),
    }
}

// Update Execute method to use backend:
func (t *ShellExecuteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
    cmd, _ := args["command"].(string)
    workingDir, _ := args["working_dir"].(string)

    result, err := t.backend.Execute(ctx, runtime.Command{
        Cmd:     cmd,
        Dir:     workingDir,
        Env:     extractEnv(args),
        Timeout: extractTimeout(args),
    })

    // Convert runtime.CommandResult to ShellResult
    return &ShellResult{
        Output:   result.Output,
        ExitCode: result.ExitCode,
    }, err
}
```

**Step 3: Update tool registry wiring**

Find where `NewShellExecuteTool` is called and update to pass runtime manager.

**Step 4: Run tests**

```bash
go test ./internal/tools/builtin/... -v -run TestShellExecuteTool
```

**Step 5: Commit**

```bash
git add internal/tools/builtin/shell.go internal/tools/builtin/shell_test.go
git commit -m "feat(shell): route commands through runtime backend"
```

---

### Phase 4: Configuration and Daemon Wiring

### Task 6: Add Configuration Schema

**Files:**
- Modify: `internal/config/schema.go`
- Create: `config/runtime.json5` (template)

**Step 1: Add RuntimeConfig to schema**

```go
// In internal/config/schema.go:

type RuntimeConfig struct {
    // Enabled controls whether containerized backends are available.
    Enabled bool `json:"enabled"`
    // DefaultBackend is "local" or "docker".
    DefaultBackend string `json:"default_backend"`
    // Docker holds Docker-specific configuration.
    Docker DockerRuntimeConfig `json:"docker"`
}

type DockerRuntimeConfig struct {
    // Image is the default container image.
    Image string `json:"image"`
    // VolumeBinds maps host paths to container paths.
    VolumeBinds []string `json:"volume_binds"`
    // TimeoutSeconds is the default command timeout.
    TimeoutSeconds int `json:"timeout_seconds"`
    // AutoCleanup removes containers after use.
    AutoCleanup bool `json:"auto_cleanup"`
}
```

**Step 2: Create config template**

```json5
// config/runtime.json5
{
  runtime: {
    // Enable containerized execution backends (default: true)
    enabled: true,

    // Default backend: "local" or "docker" (default: "local")
    default_backend: "local",

    docker: {
      // Container image for Docker backend
      image: "golang:1.24-alpine",

      // Volume binds: "host_path:container_path"
      volume_binds: [
        "~/.meept/workspaces:/workspaces",
      ],

      // Command timeout in seconds (0 = no timeout)
      timeout_seconds: 300,

      // Clean up containers after execution
      auto_cleanup: true,
    },
  },
}
```

**Step 3: Commit**

```bash
git add internal/config/schema.go config/runtime.json5
git commit -m "feat(config): add runtime configuration schema"
```

---

### Task 7: Wire Runtime Manager into Daemon

**Files:**
- Modify: `cmd/meept-daemon/main.go`
- Modify: `internal/daemon/daemon.go`

**Step 1: Create runtime manager in daemon initialization**

```go
// In internal/daemon/daemon.go:

import "github.com/caimlas/meept/internal/runtime"

type Daemon struct {
    runtimeMgr *runtime.Manager
    // ... existing fields
}

func NewDaemon(cfg *Config) (*Daemon, error) {
    // Create runtime manager
    runtimeCfg := runtime.Config{
        DefaultBackend: cfg.Runtime.DefaultBackend,
        Docker: runtime.DockerConfig{
            Image:       cfg.Runtime.Docker.Image,
            VolumeBinds: cfg.Runtime.Docker.VolumeBinds,
            Timeout:     time.Duration(cfg.Runtime.Docker.TimeoutSeconds) * time.Second,
        },
    }

    var runtimeMgr *runtime.Manager
    if cfg.Runtime.Enabled {
        var err error
        runtimeMgr, err = runtime.NewManager(runtimeCfg)
        if err != nil {
            logger.Warn("Failed to create runtime manager, using local backend", "error", err)
        }
    }

    // Pass runtimeMgr to shell tool
    shellTool := builtin.NewShellExecuteTool(runtimeMgr, logger)

    return &Daemon{
        runtimeMgr: runtimeMgr,
        // ... rest of initialization
    }, nil
}

func (d *Daemon) Close() error {
    if d.runtimeMgr != nil {
        d.runtimeMgr.Close()
    }
    // ... rest of cleanup
}
```

**Step 2: Run daemon build**

```bash
go build -o bin/meept-daemon ./cmd/meept-daemon
```

**Step 3: Commit**

```bash
git add internal/daemon/daemon.go cmd/meept-daemon/main.go
git commit -m "feat(daemon): wire runtime manager into daemon lifecycle"
```

---

### Phase 5: Test Harness Integration

### Task 8: Add TestHarness Configuration

**Files:**
- Modify: `internal/config/schema.go`
- Create: `internal/runtime/harness.go`
- Test: `internal/runtime/harness_test.go`

**Step 1: Add TestHarnessConfig**

```go
// In internal/config/schema.go:

type TestHarnessConfig struct {
    // Enabled controls test harness validation.
    Enabled bool `json:"enabled"`
    // InstallCommand runs before tests (e.g., "go mod download").
    InstallCommand string `json:"install_command"`
    // TestCommand runs tests (e.g., "go test ./...").
    TestCommand string `json:"test_command"`
    // TimeoutSeconds is the test timeout.
    TimeoutSeconds int `json:"timeout_seconds"`
}
```

**Step 2: Implement TestHarness**

```go
// In internal/runtime/harness.go:

package runtime

import (
    "context"
    "fmt"
    "time"
)

// TestHarness validates changes by running a test suite.
type TestHarness struct {
    config TestHarnessConfig
    backend ExecutionBackend
}

// TestHarnessConfig holds test configuration.
type TestHarnessConfig struct {
    InstallCommand string
    TestCommand    string
    Timeout        time.Duration
}

// NewTestHarness creates a new test harness.
func NewTestHarness(cfg TestHarnessConfig, backend ExecutionBackend) *TestHarness {
    return &TestHarness{
        config:  cfg,
        backend: backend,
    }
}

// ValidationResult holds test results.
type ValidationResult struct {
    Passed   bool
    Output   string
    Duration time.Duration
}

// Validate runs the test harness and returns results.
func (h *TestHarness) Validate(ctx context.Context, workdir string) (*ValidationResult, error) {
    result := &ValidationResult{Passed: false}
    start := time.Now()

    // Run install command if configured
    if h.config.InstallCommand != "" {
        installResult, err := h.backend.Execute(ctx, Command{
            Cmd:     h.config.InstallCommand,
            Dir:     workdir,
            Timeout: h.config.Timeout,
        })
        if err != nil {
            return nil, fmt.Errorf("install failed: %w", err)
        }
        if installResult.ExitCode != 0 {
            result.Output = installResult.Output
            return result, nil
        }
    }

    // Run test command
    testResult, err := h.backend.Execute(ctx, Command{
        Cmd:     h.config.TestCommand,
        Dir:     workdir,
        Timeout: h.config.Timeout,
    })

    result.Output = testResult.Output
    result.Duration = time.Since(start)

    if err != nil {
        return nil, err
    }

    result.Passed = testResult.ExitCode == 0
    return result, nil
}
```

**Step 3: Write tests**

```go
// In internal/runtime/harness_test.go

func TestTestHarness_Validate(t *testing.T) {
    backend := NewLocalBackend()
    harness := NewTestHarness(TestHarnessConfig{
        InstallCommand: "echo installing",
        TestCommand:    "echo 'tests passed'",
    }, backend)

    result, err := harness.Validate(context.Background(), "/tmp")
    assert.NoError(t, err)
    assert.True(t, result.Passed)
    assert.Contains(t, result.Output, "tests passed")
}
```

**Step 4: Commit**

```bash
git add internal/runtime/harness.go internal/runtime/harness_test.go
git commit -m "feat(runtime): add TestHarness for validation"
```

---

### Phase 6: Documentation

### Task 9: Write Documentation

**Files:**
- Create: `docs/concepts/runtime.md`
- Modify: `docs/configuration/runtime.md`

**Step 1: Write concepts documentation**

```markdown
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
```

**Step 2: Commit**

```bash
git add docs/concepts/runtime.md docs/configuration/runtime.md
git commit -m "docs: add containerized execution backend documentation"
```

---

### Task 10: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Add runtime section**

```markdown
## Runtime (Containerized Execution)

The runtime package provides isolated command execution:

```bash
# Test with Docker backend
TEST_DOCKER=1 go test ./internal/runtime/... -v

# Build with runtime support
go build -o bin/meept-daemon ./cmd/meept-daemon
```

Configuration: `config/runtime.json5`, `~/.meept/meept.json5`
```

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add runtime section to CLAUDE.md"
```

---

## Summary

**Total Tasks:** 10
**Estimated Time:** 4-6 hours
**Complexity:** Medium (Docker integration is the main complexity)

### Key Decisions:

1. **Opt-in by default**: `enabled: true` but `default_backend: "local"` - Docker available but not forced
2. **Graceful degradation**: If Docker unavailable, falls back to local
3. **Interface-driven design**: Easy to add new backends (Podman, Kubernetes, etc.)
4. **Test harness optional**: Separate configuration, only runs if enabled

### Files to Create:

| File | Purpose |
|------|---------|
| `internal/runtime/backend.go` | Interface and base types |
| `internal/runtime/local.go` | Local execution |
| `internal/runtime/docker.go` | Docker execution |
| `internal/runtime/manager.go` | Backend lifecycle |
| `internal/runtime/harness.go` | Test validation |
| `config/runtime.json5` | Config template |
| `docs/concepts/runtime.md` | Documentation |

### Files to Modify:

| File | Change |
|------|--------|
| `internal/config/schema.go` | Add RuntimeConfig |
| `internal/tools/builtin/shell.go` | Route through backend |
| `internal/daemon/daemon.go` | Wire runtime manager |
| `CLAUDE.md` | Add runtime section |

---

**Plan complete.** Two execution options:

1. **Subagent-Driven** - I dispatch fresh subagents per task with review between phases
2. **Parallel Session** - Open new session with `superpowers:executing-plans` for batch execution

Which approach?
