# Local LLM Runtime Lifecycle Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement automatic lifecycle management for local LLM runtimes (llama.cpp/MLX) so the classifier agent starts automatically when needed and stops cleanly on daemon exit.

**Architecture:** Extend `models.json5` provider configuration with lifecycle metadata. The daemon's `NewComponents` function reads this config and spawns/stops runtimes via a new `RuntimeManager` component that handles health checks, PID file management, and graceful shutdown.

**Tech Stack:** Go (daemon lifecycle), exec package (process management), HTTP health checks, SQLite (optional PID tracking), JSON5 configuration.

---

## File Structure

**New files:**
- `internal/llm/runtime_manager.go` - Core lifecycle management (spawn, stop, health check)
- `internal/llm/runtime_config.go` - Configuration types for runtime lifecycle
- `internal/llm/runtime_process.go` - Process wrapper with PID file management
- `internal/llm/health_checker.go` - HTTP health check with configurable intervals
- `cmd/meept/runtime_check.go` - CLI command for testing runtime status
- `test/llm/runtime_manager_test.go` - Unit tests for lifecycle management

**Modified files:**
- `config/models.json5` - Add lifecycle config to "local" provider
- `internal/config/schema.go` - Add LifecycleConfig types
- `internal/daemon/components.go` - Wire RuntimeManager into component initialization
- `internal/daemon/daemon.go` - Add RuntimeManager to shutdown sequence
- `docs/configuration/llm.md` - Document lifecycle configuration options

---

## Configuration Design

The `models.json5` local provider gets extended with:

```json5
"local": {
  "api": "openai",
  "options": { "baseURL": "http://127.0.0.1:8080/v1" },
  "lifecycle": {
    "runtime": "llama-cpp",  // or "mlx"
    "model_path": "~/models/lfm-code.Q8_0.gguf",
    "auto_start": true,
    "auto_stop_on_exit": true,
    "pid_file": "~/.meept/run/llama.pid",
    "health_check": {
      "endpoint": "/health",
      "interval_seconds": 10,
      "timeout_seconds": 5,
      "unhealthy_threshold": 3
    },
    "spawn_command": ["llama-server", "-m", "${MODEL_PATH}", "--port", "8080"],
    "spawn_timeout_seconds": 60
  }
}
```

---

## Task 1: Configuration Schema

**Files:**
- Modify: `internal/config/schema.go`
- Create: `internal/llm/runtime_config.go`
- Test: `internal/llm/runtime_config_test.go`

- [ ] **Step 1: Add LifecycleConfig to schema.go**

Add the following types to `internal/config/schema.go`:

```go
// RuntimeLifecycleConfig holds configuration for local LLM runtime management.
type RuntimeLifecycleConfig struct {
    Runtime          string           `json:"runtime"`            // "llama-cpp" or "mlx"
    ModelPath        string           `json:"model_path"`         // Path to model file
    AutoStart        bool             `json:"auto_start"`         // Auto-start on daemon startup
    AutoStopOnExit   bool             `json:"auto_stop_on_exit"`  // Stop on daemon shutdown
    PIDFile          string           `json:"pid_file"`           // Path to PID file
    SpawnCommand     []string         `json:"spawn_command"`      // Command and args to spawn runtime
    SpawnTimeout     int              `json:"spawn_timeout_seconds"`
    HealthCheck      HealthCheckConfig `json:"health_check"`
}

// HealthCheckConfig holds health check configuration.
type HealthCheckConfig struct {
    Endpoint           string `json:"endpoint"`
    IntervalSeconds    int    `json:"interval_seconds"`
    TimeoutSeconds     int    `json:"timeout_seconds"`
    UnhealthyThreshold int    `json:"unhealthy_threshold"`
}
```

- [ ] **Step 2: Add LifecycleConfig to ProviderConfig**

In `internal/config/schema.go`, modify `ProviderConfig`:

```go
type ProviderConfig struct {
    API       string                 `json:"api"`
    Options   ProviderOptionsConfig  `json:"options"`
    Models    map[string]ModelDef    `json:"models"`
    Lifecycle *RuntimeLifecycleConfig `json:"lifecycle,omitempty"`  // Add this
}
```

- [ ] **Step 3: Create runtime_config.go**

Create `internal/llm/runtime_config.go`:

```go
package llm

import (
    "os"
    "path/filepath"
    "time"

    "github.com/caimlas/meept/internal/pathutil"
)

// RuntimeType represents a supported LLM runtime.
type RuntimeType string

const (
    RuntimeLlamaCpp RuntimeType = "llama-cpp"
    RuntimeMLX      RuntimeType = "mlx"
)

// RuntimeConfig holds validated runtime configuration.
type RuntimeConfig struct {
    Type           RuntimeType
    ModelPath      string
    PIDFile        string
    AutoStart      bool
    AutoStop       bool
    SpawnCommand   []string
    SpawnTimeout   time.Duration
    HealthEndpoint string
    HealthInterval time.Duration
    HealthTimeout  time.Duration
    HealthThreshold int
}

// ValidateAndNormalize validates the config and expands paths.
func ValidateAndNormalize(cfg RuntimeLifecycleConfig) (*RuntimeConfig, error) {
    // Validate runtime type
    var rt RuntimeType
    switch cfg.Runtime {
    case "llama-cpp":
        rt = RuntimeLlamaCpp
    case "mlx":
        rt = RuntimeMLX
    default:
        return nil, fmt.Errorf("unsupported runtime: %s", cfg.Runtime)
    }

    // Expand model path
    modelPath := pathutil.ExpandPath(cfg.ModelPath)
    if _, err := os.Stat(modelPath); err != nil {
        return nil, fmt.Errorf("model file not found: %s: %w", modelPath, err)
    }

    // Expand PID file path
    pidFile := pathutil.ExpandPath(cfg.PIDFile)
    if err := os.MkdirAll(filepath.Dir(pidFile), 0o700); err != nil {
        return nil, fmt.Errorf("failed to create PID directory: %w", err)
    }

    // Build spawn command with variable expansion
    spawnCmd := make([]string, len(cfg.SpawnCommand))
    for i, part := range cfg.SpawnCommand {
        spawnCmd[i] = os.Expand(part, func(key string) string {
            if key == "MODEL_PATH" {
                return modelPath
            }
            return os.Getenv(key)
        })
    }

    // Set defaults
    spawnTimeout := 60 * time.Second
    if cfg.SpawnTimeout > 0 {
        spawnTimeout = time.Duration(cfg.SpawnTimeout) * time.Second
    }

    healthInterval := 10 * time.Second
    if cfg.HealthCheck.IntervalSeconds > 0 {
        healthInterval = time.Duration(cfg.HealthCheck.IntervalSeconds) * time.Second
    }

    healthTimeout := 5 * time.Second
    if cfg.HealthCheck.TimeoutSeconds > 0 {
        healthTimeout = time.Duration(cfg.HealthCheck.TimeoutSeconds) * time.Second
    }

    healthThreshold := 3
    if cfg.HealthCheck.UnhealthyThreshold > 0 {
        healthThreshold = cfg.HealthCheck.UnhealthyThreshold
    }

    return &RuntimeConfig{
        Type:            rt,
        ModelPath:       modelPath,
        PIDFile:         pidFile,
        AutoStart:       cfg.AutoStart,
        AutoStop:        cfg.AutoStopOnExit,
        SpawnCommand:    spawnCmd,
        SpawnTimeout:    spawnTimeout,
        HealthEndpoint:  cfg.HealthCheck.Endpoint,
        HealthInterval:  healthInterval,
        HealthTimeout:   healthTimeout,
        HealthThreshold: healthThreshold,
    }, nil
}
```

- [ ] **Step 4: Write unit tests for runtime_config.go**

Create `internal/llm/runtime_config_test.go`:

```go
package llm

import (
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestValidateAndNormalize_ValidConfig(t *testing.T) {
    tmpDir := t.TempDir()
    modelPath := filepath.Join(tmpDir, "model.gguf")
    if err := os.WriteFile(modelPath, []byte("dummy"), 0o600); err != nil {
        t.Fatal(err)
    }

    cfg := RuntimeLifecycleConfig{
        Runtime:       "llama-cpp",
        ModelPath:     "~/test-model.gguf", // Will be normalized
        PIDFile:       filepath.Join(tmpDir, "test.pid"),
        AutoStart:     true,
        AutoStopOnExit: true,
        SpawnCommand:  []string{"llama-server", "-m", "${MODEL_PATH}", "--port", "8080"},
        SpawnTimeout:  30,
        HealthCheck: HealthCheckConfig{
            Endpoint:           "/health",
            IntervalSeconds:    15,
            TimeoutSeconds:     10,
            UnhealthyThreshold: 5,
        },
    }

    result, err := ValidateAndNormalize(cfg)
    if err != nil {
        t.Fatalf("ValidateAndNormalize() error = %v", err)
    }

    if result.Type != RuntimeLlamaCpp {
        t.Errorf("Type = %v, want %v", result.Type, RuntimeLlamaCpp)
    }
    if result.ModelPath != modelPath {
        t.Errorf("ModelPath = %v, want %v", result.ModelPath, modelPath)
    }
    if result.AutoStart != true {
        t.Errorf("AutoStart = %v, want true", result.AutoStart)
    }
    if result.AutoStop != true {
        t.Errorf("AutoStop = %v, want true", result.AutoStop)
    }
    if result.SpawnTimeout != 30*time.Second {
        t.Errorf("SpawnTimeout = %v, want %v", result.SpawnTimeout, 30*time.Second)
    }
    if result.HealthInterval != 15*time.Second {
        t.Errorf("HealthInterval = %v, want %v", result.HealthInterval, 15*time.Second)
    }
}

func TestValidateAndNormalize_InvalidRuntime(t *testing.T) {
    tmpDir := t.TempDir()
    modelPath := filepath.Join(tmpDir, "model.gguf")
    if err := os.WriteFile(modelPath, []byte("dummy"), 0o600); err != nil {
        t.Fatal(err)
    }

    cfg := RuntimeLifecycleConfig{
        Runtime:   "invalid-runtime",
        ModelPath: modelPath,
        PIDFile:   filepath.Join(tmpDir, "test.pid"),
    }

    _, err := ValidateAndNormalize(cfg)
    if err == nil {
        t.Fatal("ValidateAndNormalize() expected error for invalid runtime")
    }
}

func TestValidateAndNormalize_ModelNotFound(t *testing.T) {
    tmpDir := t.TempDir()

    cfg := RuntimeLifecycleConfig{
        Runtime:   "llama-cpp",
        ModelPath: filepath.Join(tmpDir, "nonexistent.gguf"),
        PIDFile:   filepath.Join(tmpDir, "test.pid"),
    }

    _, err := ValidateAndNormalize(cfg)
    if err == nil {
        t.Fatal("ValidateAndNormalize() expected error for missing model")
    }
}
```

- [ ] **Step 5: Run tests**

```bash
cd /Users/caimlas/git/meept
go test ./internal/llm/runtime_config_test.go ./internal/llm/runtime_config.go -v
```

Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/config/schema.go internal/llm/runtime_config.go internal/llm/runtime_config_test.go
git commit -m "feat: add RuntimeLifecycleConfig types for local LLM management"
```

---

## Task 2: Health Checker

**Files:**
- Create: `internal/llm/health_checker.go`
- Test: `internal/llm/health_checker_test.go`

- [ ] **Step 1: Implement HealthChecker**

Create `internal/llm/health_checker.go`:

```go
package llm

import (
    "context"
    "fmt"
    "net/http"
    "sync"
    "time"
)

// HealthChecker performs periodic health checks on a runtime.
type HealthChecker struct {
    config      *RuntimeConfig
    client      *http.Client
    baseURL     string
    healthy     bool
    unhealthyCount int
    mu          sync.RWMutex
    stopCh      chan struct{}
    stopped     bool
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(cfg *RuntimeConfig, baseURL string) *HealthChecker {
    return &HealthChecker{
        config:   cfg,
        client:   &http.Client{Timeout: cfg.HealthTimeout},
        baseURL:  baseURL,
        stopCh:   make(chan struct{}),
    }
}

// Start begins periodic health checks.
func (h *HealthChecker) Start(ctx context.Context) {
    go h.run(ctx)
}

func (h *HealthChecker) run(ctx context.Context) {
    ticker := time.NewTicker(h.config.HealthInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-h.stopCh:
            return
        case <-ticker.C:
            h.checkOnce()
        }
    }
}

func (h *HealthChecker) checkOnce() {
    h.mu.Lock()
    defer h.mu.Unlock()

    url := h.baseURL + h.config.HealthEndpoint
    resp, err := h.client.Get(url)
    if err != nil {
        h.unhealthyCount++
        h.healthy = false
        return
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusOK {
        h.unhealthyCount = 0
        h.healthy = true
    } else {
        h.unhealthyCount++
        h.healthy = h.unhealthyCount >= h.config.HealthThreshold
    }
}

// Stop stops the health checker.
func (h *HealthChecker) Stop() {
    h.mu.Lock()
    defer h.mu.Unlock()

    if !h.stopped {
        close(h.stopCh)
        h.stopped = true
    }
}

// IsHealthy returns true if the runtime is healthy.
func (h *HealthChecker) IsHealthy() bool {
    h.mu.RLock()
    defer h.mu.RUnlock()
    return h.healthy
}

// WaitForHealthy blocks until the runtime is healthy or timeout.
func (h *HealthChecker) WaitForHealthy(ctx context.Context, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if h.IsHealthy() {
            return nil
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(500 * time.Millisecond):
            // Poll
        }
    }
    return fmt.Errorf("timeout waiting for runtime to become healthy")
}
```

- [ ] **Step 2: Write tests**

Create `internal/llm/health_checker_test.go` with tests for `NewHealthChecker`, `Start`, `Stop`, `IsHealthy`, and `WaitForHealthy`.

- [ ] **Step 3: Run tests and commit**

```bash
go test ./internal/llm/health_checker_test.go ./internal/llm/health_checker.go -v
git add internal/llm/health_checker.go internal/llm/health_checker_test.go
git commit -m "feat: add HealthChecker for periodic runtime health monitoring"
```

---

## Task 3: Runtime Process Manager

**Files:**
- Create: `internal/llm/runtime_process.go`
- Test: `internal/llm/runtime_process_test.go`

- [ ] **Step 1: Implement RuntimeProcess**

Create `internal/llm/runtime_process.go`:

```go
package llm

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "syscall"
    "time"
)

// RuntimeProcess manages a spawned LLM runtime process.
type RuntimeProcess struct {
    config  *RuntimeConfig
    cmd     *exec.Cmd
    pid     int
    pidFile string
}

// NewRuntimeProcess creates a new process manager.
func NewRuntimeProcess(cfg *RuntimeConfig) *RuntimeProcess {
    return &RuntimeProcess{
        config:  cfg,
        pidFile: cfg.PIDFile,
    }
}

// Start spawns the runtime process.
func (p *RuntimeProcess) Start(ctx context.Context) error {
    // Check if already running via PID file
    if pid, err := p.readPIDFile(); err == nil && pid > 0 {
        if p.isProcessRunning(pid) {
            return nil // Already running
        }
        // Stale PID file
        os.Remove(p.pidFile)
    }

    // Spawn the process
    if len(p.config.SpawnCommand) == 0 {
        return fmt.Errorf("no spawn command configured")
    }

    name := p.config.SpawnCommand[0]
    args := p.config.SpawnCommand[1:]

    p.cmd = exec.CommandContext(ctx, name, args...)
    p.cmd.Stdout = os.Stdout
    p.cmd.Stderr = os.Stderr
    p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

    if err := p.cmd.Start(); err != nil {
        return fmt.Errorf("failed to spawn runtime: %w", err)
    }

    p.pid = p.cmd.Process.Pid

    // Write PID file
    if err := p.writePIDFile(p.pid); err != nil {
        p.cmd.Process.Kill()
        return fmt.Errorf("failed to write PID file: %w", err)
    }

    return nil
}

// Stop gracefully terminates the runtime process.
func (p *RuntimeProcess) Stop(ctx context.Context) error {
    if p.cmd == nil || p.cmd.Process == nil {
        // Try to read from PID file
        if pid, err := p.readPIDFile(); err == nil && pid > 0 {
            proc, err := os.FindProcess(pid)
            if err != nil {
                return nil
            }
            p.cmd = &exec.Cmd{}
            p.cmd.Process = proc
        } else {
            return nil // Not running
        }
    }

    // Send SIGTERM for graceful shutdown
    if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
        // Already dead
        os.Remove(p.pidFile)
        return nil
    }

    // Wait for process to exit
    done := make(chan error, 1)
    go func() {
        done <- p.cmd.Wait()
    }()

    select {
    case <-ctx.Done():
        // Force kill
        p.cmd.Process.Kill()
    case err := <-done:
        _ = err // Ignored
    }

    os.Remove(p.pidFile)
    return nil
}

// PID returns the process ID.
func (p *RuntimeProcess) PID() int {
    return p.pid
}

// IsRunning checks if the process is still alive.
func (p *RuntimeProcess) IsRunning() bool {
    if p.pid == 0 {
        return false
    }
    return p.isProcessRunning(p.pid)
}

func (p *RuntimeProcess) isProcessRunning(pid int) bool {
    proc, err := os.FindProcess(pid)
    if err != nil {
        return false
    }
    err = proc.Signal(syscall.Signal(0))
    return err == nil
}

func (p *RuntimeProcess) writePIDFile(pid int) error {
    dir := filepath.Dir(p.pidFile)
    if err := os.MkdirAll(dir, 0o700); err != nil {
        return err
    }
    return os.WriteFile(p.pidFile, []byte(strconv.Itoa(pid)), 0o600)
}

func (p *RuntimeProcess) readPIDFile() (int, error) {
    data, err := os.ReadFile(p.pidFile)
    if err != nil {
        return 0, err
    }
    return strconv.Atoi(string(data))
}
```

- [ ] **Step 2: Write tests**

Create `internal/llm/runtime_process_test.go`.

- [ ] **Step 3: Commit**

```bash
git add internal/llm/runtime_process.go internal/llm/runtime_process_test.go
git commit -m "feat: add RuntimeProcess for spawn/stop lifecycle"
```

---

## Task 4: RuntimeManager Integration

**Files:**
- Create: `internal/llm/runtime_manager.go`
- Modify: `internal/daemon/components.go`
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Implement RuntimeManager**

Create `internal/llm/runtime_manager.go`:

```go
package llm

import (
    "context"
    "fmt"
    "log/slog"
    "sync"
)

// RuntimeManager manages local LLM runtime lifecycle.
type RuntimeManager struct {
    configs []*RuntimeConfig
    processes map[string]*RuntimeProcess
    healthCheckers map[string]*HealthChecker
    mu sync.Mutex
    logger *slog.Logger
}

// NewRuntimeManager creates a new manager.
func NewRuntimeManager(logger *slog.Logger) *RuntimeManager {
    return &RuntimeManager{
        configs:        make([]*RuntimeConfig, 0),
        processes:      make(map[string]*RuntimeProcess),
        healthCheckers: make(map[string]*HealthChecker),
        logger:         logger,
    }
}

// RegisterConfig registers a runtime configuration.
func (m *RuntimeManager) RegisterConfig(providerID string, cfg *RuntimeConfig, baseURL string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.configs = append(m.configs, cfg)

    // Create process
    proc := NewRuntimeProcess(cfg)
    m.processes[providerID] = proc

    // Create health checker
    hc := NewHealthChecker(cfg, baseURL)
    m.healthCheckers[providerID] = hc

    m.logger.Info("Registered runtime config", "provider", providerID, "runtime", cfg.Type)
    return nil
}

// StartAll starts all registered runtimes with auto_start=true.
func (m *RuntimeManager) StartAll(ctx context.Context) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    for providerID, cfg := range m.configs {
        if !cfg.AutoStart {
            continue
        }

        proc := m.processes[providerID]
        hc := m.healthCheckers[providerID]

        m.logger.Info("Starting runtime", "provider", providerID)
        if err := proc.Start(ctx); err != nil {
            return fmt.Errorf("failed to start runtime %s: %w", providerID, err)
        }

        // Start health checker
        hc.Start(ctx)

        // Wait for healthy
        if err := hc.WaitForHealthy(ctx, cfg.SpawnTimeout); err != nil {
            proc.Stop(ctx)
            return fmt.Errorf("runtime %s did not become healthy: %w", providerID, err)
        }

        m.logger.Info("Runtime started and healthy", "provider", providerID)
    }

    return nil
}

// StopAll stops all running runtimes.
func (m *RuntimeManager) StopAll(ctx context.Context) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    for providerID, proc := range m.processes {
        m.logger.Info("Stopping runtime", "provider", providerID)
        if err := proc.Stop(ctx); err != nil {
            m.logger.Error("Failed to stop runtime", "provider", providerID, "error", err)
        }
    }

    // Stop health checkers
    for providerID, hc := range m.healthCheckers {
        hc.Stop()
        m.logger.Debug("Health checker stopped", "provider", providerID)
    }

    return nil
}

// GetHealthChecker returns the health checker for a provider.
func (m *RuntimeManager) GetHealthChecker(providerID string) (*HealthChecker, bool) {
    m.mu.Lock()
    defer m.mu.Unlock()
    hc, ok := m.healthCheckers[providerID]
    return hc, ok
}
```

- [ ] **Step 2: Wire into components.go**

In `internal/daemon/components.go`, find `NewComponents` and add RuntimeManager initialization.

- [ ] **Step 3: Wire into daemon.go shutdown**

In `internal/daemon/daemon.go`, add RuntimeManager to the Components struct and shutdown sequence.

- [ ] **Step 4: Commit**

---

## Task 5: Update models.json5 Configuration

**Files:**
- Modify: `config/models.json5`
- Test: Manual testing

- [ ] **Step 1: Add lifecycle config to local provider**

Edit `config/models.json5` and add the lifecycle section to the "local" provider.

- [ ] **Step 2: Test configuration loading**

```bash
go build -o bin/meept ./cmd/meept
./bin/meept status
```

- [ ] **Step 3: Commit**

```bash
git add config/models.json5
git commit -m "config: add lifecycle config for local LLM runtime"
```

---

## Task 6: CLI Tool for Runtime Management

**Files:**
- Create: `cmd/meept/runtime.go`
- Test: Manual testing

- [ ] **Step 1: Implement runtime CLI commands**

```bash
meept runtime status    # Show runtime status
meept runtime start     # Start a runtime
meept runtime stop      # Stop a runtime
meept runtime restart   # Restart a runtime
```

- [ ] **Step 2: Commit**

---

## Task 7: Documentation

**Files:**
- Create: `docs/configuration/llm-lifecycle.md`
- Modify: `docs/configuration/llm.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Write configuration documentation**

- [ ] **Step 2: Update CLAUDE.md with architecture notes**

- [ ] **Step 3: Commit**

```bash
git add docs/configuration/
git commit -m "docs: add runtime lifecycle management documentation"
```

---

## Summary

This plan implements Approach #1 from the brainstorming session: **Provider Health-Check + Auto-Spawn Pattern**. The key design decisions:

1. **Configuration-driven**: Lifecycle settings live in `models.json5` alongside provider config
2. **Explicit runtime type**: `runtime: "llama-cpp"` or `runtime: "mlx"` - no magic detection
3. **Health checks**: HTTP polling with configurable thresholds
4. **PID file management**: Clean shutdown/restart, stale PID detection
5. **Graceful shutdown**: SIGTERM with timeout, then force kill
