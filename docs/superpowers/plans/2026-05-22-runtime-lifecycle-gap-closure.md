# Runtime Lifecycle Gap Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close all identified gaps in the local LLM runtime lifecycle management system: auto-restart, HTTP/RPC API, metrics, daemon status integration, AutoStop bug, health transitions, individual provider control, CLI health wait, and code quality fixes.

**Architecture:** Extend `RuntimeManager` with individual start/stop/restart methods and auto-restart logic. Create a `RuntimeService` in the service layer for HTTP/RPC transport parity. Add health transition logging and metrics recording. Wire runtime status into the daemon status response. Fix the AutoStop bug in `StopAll`.

**Tech Stack:** Go (service layer, HTTP handlers, RPC handlers, metrics), JSON5 configuration.

---

## File Structure

**New files:**
- `internal/services/runtime_service.go` - Service layer for runtime operations
- `internal/rpc/runtime.go` - RPC handlers for runtime management
- `internal/llm/runtime_manager_test_extended.go` - Tests for new RuntimeManager methods
- `internal/services/runtime_service_test.go` - Tests for RuntimeService

**Modified files:**
- `internal/llm/runtime_manager.go` - Add individual start/stop/restart, auto-restart, status, AutoStop fix
- `internal/llm/health_checker.go` - Add transition logging and callback hooks
- `internal/llm/runtime_config.go` - Add RestartPolicy to config, fix health endpoint default
- `config/models.json5` - Add restart policy config
- `internal/daemon/daemon.go` - Wire RuntimeService into service registry, add runtime status to daemon status, add metrics getters
- `internal/daemon/components.go` - Add RuntimeManager to Components field (already exists)
- `internal/comm/http/server.go` - Add runtime HTTP API endpoints
- `internal/comm/http/unified_http_test.go` - Add runtime HTTP endpoint tests
- `cmd/meept/runtime.go` - Add --wait flag, --format json, fix status side effect
- `docs/configuration/llm-lifecycle.md` - Document new features

---

## Task 1: Fix AutoStop Bug and Add Individual Provider Methods

**Files:**
- Modify: `internal/llm/runtime_manager.go`
- Test: `internal/llm/runtime_manager_test.go`

The `StopAll` method currently stops ALL runtimes regardless of `auto_stop_on_exit` config. Fix this and add individual start/stop/restart methods.

- [ ] **Step 1: Add RuntimeStatus type and Status method**

Add to `internal/llm/runtime_manager.go`:

```go
// RuntimeStatus describes the current state of a managed runtime.
type RuntimeStatus struct {
	ProviderID string `json:"provider_id"`
	Runtime    string `json:"runtime"`
	Healthy    bool   `json:"healthy"`
	Running    bool   `json:"running"`
	PID        int    `json:"pid,omitempty"`
	ModelPath  string `json:"model_path"`
}

// Status returns the status of all registered runtimes.
func (m *RuntimeManager) Status() []RuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	var statuses []RuntimeStatus
	for providerID, cfg := range m.configs {
		status := RuntimeStatus{
			ProviderID: providerID,
			Runtime:    string(cfg.Type),
			ModelPath:  cfg.ModelPath,
		}
		if proc, ok := m.processes[providerID]; ok {
			status.Running = proc.IsRunning()
			status.PID = proc.PID()
		}
		if hc, ok := m.healthCheckers[providerID]; ok {
			status.Healthy = hc.IsHealthy()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// StatusForProvider returns the status of a specific provider.
func (m *RuntimeManager) StatusForProvider(providerID string) (RuntimeStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[providerID]
	if !ok {
		return RuntimeStatus{}, false
	}

	status := RuntimeStatus{
		ProviderID: providerID,
		Runtime:    string(cfg.Type),
		ModelPath:  cfg.ModelPath,
	}
	if proc, ok := m.processes[providerID]; ok {
		status.Running = proc.IsRunning()
		status.PID = proc.PID()
	}
	if hc, ok := m.healthCheckers[providerID]; ok {
		status.Healthy = hc.IsHealthy()
	}
	return status, true
}
```

- [ ] **Step 2: Fix StopAll to respect AutoStop**

Replace the existing `StopAll` method:

```go
// StopAll stops all running runtimes that have auto_stop_on_exit=true.
func (m *RuntimeManager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for providerID, proc := range m.processes {
		cfg := m.configs[providerID]
		if !cfg.AutoStop {
			m.logger.Debug("Skipping runtime stop (auto_stop disabled)", "provider", providerID)
			continue
		}
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
```

- [ ] **Step 3: Add individual Start/Stop/RestartProvider methods**

Add to `internal/llm/runtime_manager.go`:

```go
// StartProvider starts a specific provider's runtime.
func (m *RuntimeManager) StartProvider(ctx context.Context, providerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, ok := m.configs[providerID]
	if !ok {
		return fmt.Errorf("provider %s not registered", providerID)
	}

	proc := m.processes[providerID]
	hc := m.healthCheckers[providerID]

	m.logger.Info("Starting runtime", "provider", providerID)
	if err := proc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start runtime %s: %w", providerID, err)
	}

	hc.Start(ctx)

	if err := hc.WaitForHealthy(ctx, cfg.SpawnTimeout); err != nil {
		proc.Stop(ctx)
		return fmt.Errorf("runtime %s did not become healthy: %w", providerID, err)
	}

	m.logger.Info("Runtime started and healthy", "provider", providerID)
	return nil
}

// StopProvider stops a specific provider's runtime.
func (m *RuntimeManager) StopProvider(ctx context.Context, providerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	proc, ok := m.processes[providerID]
	if !ok {
		return fmt.Errorf("provider %s not registered", providerID)
	}

	m.logger.Info("Stopping runtime", "provider", providerID)
	if err := proc.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop runtime %s: %w", providerID, err)
	}

	// Stop health checker
	if hc, ok := m.healthCheckers[providerID]; ok {
		hc.Stop()
	}

	return nil
}

// RestartProvider restarts a specific provider's runtime.
func (m *RuntimeManager) RestartProvider(ctx context.Context, providerID string) error {
	if err := m.StopProvider(ctx, providerID); err != nil {
		m.logger.Warn("Stop failed during restart", "provider", providerID, "error", err)
	}
	return m.StartProvider(ctx, providerID)
}
```

- [ ] **Step 4: Write tests for new methods**

Add to `internal/llm/runtime_manager_test.go`:

```go
func TestRuntimeManager_Status(t *testing.T) {
	mgr := llm.NewRuntimeManager(slog.Default())

	pidFile := filepath.Join(t.TempDir(), "status.pid")
	cfg := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile,
		AutoStart:       false,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  1 * time.Second,
		HealthTimeout:   2 * time.Second,
		HealthThreshold: 1,
	}

	err := mgr.RegisterConfig("test-status", cfg, "http://localhost:8080")
	if err != nil {
		t.Fatalf("RegisterConfig error: %v", err)
	}

	statuses := mgr.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].ProviderID != "test-status" {
		t.Errorf("expected provider test-status, got %s", statuses[0].ProviderID)
	}
	if statuses[0].Runtime != "llama-cpp" {
		t.Errorf("expected runtime llama-cpp, got %s", statuses[0].Runtime)
	}

	// Test single provider lookup
	status, ok := mgr.StatusForProvider("test-status")
	if !ok {
		t.Fatal("expected to find provider test-status")
	}
	if status.ProviderID != "test-status" {
		t.Errorf("expected provider test-status, got %s", status.ProviderID)
	}

	// Test nonexistent provider
	_, ok = mgr.StatusForProvider("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent provider")
	}
}

func TestRuntimeManager_StopAll_RespectsAutoStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := llm.NewRuntimeManager(slog.Default())

	// Provider with AutoStop=true
	pidFile1 := filepath.Join(t.TempDir(), "autostop.pid")
	cfg1 := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile1,
		AutoStart:       true,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "0.1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	// Provider with AutoStop=false
	pidFile2 := filepath.Join(t.TempDir(), "noautostop.pid")
	cfg2 := &llm.RuntimeConfig{
		Type:            llm.RuntimeMLX,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile2,
		AutoStart:       true,
		AutoStop:        false,
		SpawnCommand:    []string{"sleep", "0.1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	mgr.RegisterConfig("stop-me", cfg1, server.URL)
	mgr.RegisterConfig("keep-me", cfg2, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.StartAll(ctx); err != nil {
		t.Fatalf("StartAll error: %v", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := mgr.StopAll(stopCtx); err != nil {
		t.Fatalf("StopAll error: %v", err)
	}

	// autostop PID file should be removed
	if _, err := os.Stat(pidFile1); !os.IsNotExist(err) {
		t.Error("autostop PID file should be removed")
	}

	// noautostop PID file should still exist
	if _, err := os.Stat(pidFile2); os.IsNotExist(err) {
		t.Error("noautostop PID file should still exist")
	}

	// Clean up leftover process
	mgr.StopProvider(stopCtx, "keep-me")
}

func TestRuntimeManager_StartStopProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mgr := llm.NewRuntimeManager(slog.Default())

	pidFile := filepath.Join(t.TempDir(), "individual.pid")
	cfg := &llm.RuntimeConfig{
		Type:            llm.RuntimeLlamaCpp,
		ModelPath:       createTempModelFile(t),
		PIDFile:         pidFile,
		AutoStart:       false,
		AutoStop:        true,
		SpawnCommand:    []string{"sleep", "0.1"},
		SpawnTimeout:    2 * time.Second,
		HealthEndpoint:  "/health",
		HealthInterval:  100 * time.Millisecond,
		HealthTimeout:   1 * time.Second,
		HealthThreshold: 1,
	}

	mgr.RegisterConfig("individual", cfg, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start individual provider
	if err := mgr.StartProvider(ctx, "individual"); err != nil {
		t.Fatalf("StartProvider error: %v", err)
	}

	status, _ := mgr.StatusForProvider("individual")
	if !status.Running {
		t.Error("expected running after StartProvider")
	}

	// Stop individual provider
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()

	if err := mgr.StopProvider(stopCtx, "individual"); err != nil {
		t.Fatalf("StopProvider error: %v", err)
	}

	status, _ = mgr.StatusForProvider("individual")
	if status.Running {
		t.Error("expected not running after StopProvider")
	}

	// Test nonexistent provider
	err := mgr.StartProvider(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/llm/... -run "RuntimeManager" -v -count=1
```

Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/llm/runtime_manager.go internal/llm/runtime_manager_test.go
git commit -m "feat(llm): add individual start/stop/restart, status, fix AutoStop in RuntimeManager"
```

---

## Task 2: Add Restart Policy and Auto-Restart on Unhealthy

**Files:**
- Modify: `internal/llm/runtime_config.go`
- Modify: `internal/llm/health_checker.go`
- Modify: `internal/llm/runtime_manager.go`
- Modify: `config/models.json5`

- [ ] **Step 1: Add RestartPolicy to config types**

Add to `internal/llm/runtime_config.go` after the `HealthCheckConfig` struct:

```go
// RestartPolicyConfig holds restart policy configuration.
type RestartPolicyConfig struct {
	Enabled            bool `json:"enabled"`              // Enable auto-restart on unhealthy
	MaxAttempts        int  `json:"max_attempts"`         // Max restart attempts (default: 3)
	CooldownSeconds    int  `json:"cooldown_seconds"`     // Seconds between restart attempts (default: 30)
	ResetAfterSeconds  int  `json:"reset_after_seconds"`  // Reset failure count after this many seconds of healthy (default: 300)
}
```

Add `RestartPolicy` field to `RuntimeLifecycleConfig`:

```go
type RuntimeLifecycleConfig struct {
	Runtime        string               `json:"runtime"`
	ModelPath      string               `json:"model_path"`
	AutoStart      bool                 `json:"auto_start"`
	AutoStopOnExit bool                 `json:"auto_stop_on_exit"`
	PIDFile        string               `json:"pid_file"`
	SpawnCommand   []string             `json:"spawn_command"`
	SpawnTimeout   int                  `json:"spawn_timeout_seconds"`
	HealthCheck    HealthCheckConfig    `json:"health_check"`
	RestartPolicy  RestartPolicyConfig  `json:"restart_policy,omitempty"`
}
```

Add to `RuntimeConfig` struct:

```go
type RuntimeConfig struct {
	Type             RuntimeType
	ModelPath        string
	PIDFile          string
	AutoStart        bool
	AutoStop         bool
	SpawnCommand     []string
	SpawnTimeout     time.Duration
	HealthEndpoint   string
	HealthInterval   time.Duration
	HealthTimeout    time.Duration
	HealthThreshold  int
	// Restart policy
	RestartEnabled      bool
	RestartMaxAttempts  int
	RestartCooldown     time.Duration
	RestartResetAfter   time.Duration
}
```

Add defaults in `ValidateAndNormalize`:

```go
// Set restart policy defaults
restartMaxAttempts := 3
if cfg.RestartPolicy.MaxAttempts > 0 {
	restartMaxAttempts = cfg.RestartPolicy.MaxAttempts
}
restartCooldown := 30 * time.Second
if cfg.RestartPolicy.CooldownSeconds > 0 {
	restartCooldown = time.Duration(cfg.RestartPolicy.CooldownSeconds) * time.Second
}
restartResetAfter := 300 * time.Second
if cfg.RestartPolicy.ResetAfterSeconds > 0 {
	restartResetAfter = time.Duration(cfg.RestartPolicy.ResetAfterSeconds) * time.Second
}
```

And in the return statement add:

```go
RestartEnabled:     cfg.RestartPolicy.Enabled,
RestartMaxAttempts: restartMaxAttempts,
RestartCooldown:    restartCooldown,
RestartResetAfter:  restartResetAfter,
```

- [ ] **Step 2: Add health transition callbacks to HealthChecker**

Add to `internal/llm/health_checker.go`. Add callback fields to the struct and invoke them on state transitions:

```go
// HealthChangeCallback is invoked when health state changes.
type HealthChangeCallback func(healthy bool)

type HealthChecker struct {
	config         *RuntimeConfig
	client         *http.Client
	baseURL        string
	healthy        bool
	unhealthyCount int
	mu             sync.RWMutex
	stopCh         chan struct{}
	stopped        bool
	onHealthChange HealthChangeCallback
	logger         *slog.Logger
}
```

Update `NewHealthChecker` to accept optional logger:

```go
func NewHealthChecker(cfg *RuntimeConfig, baseURL string) *HealthChecker {
	return &HealthChecker{
		config:  cfg,
		client:  &http.Client{Timeout: cfg.HealthTimeout},
		baseURL: baseURL,
		stopCh:  make(chan struct{}),
		logger:  slog.Default().With("component", "health-checker"),
	}
}

// OnHealthChange sets a callback invoked on health state transitions.
func (h *HealthChecker) OnHealthChange(cb HealthChangeCallback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onHealthChange = cb
}
```

Update `checkOnce` to log transitions and fire callback:

```go
func (h *HealthChecker) checkOnce() {
	h.mu.Lock()
	defer h.mu.Unlock()

	wasHealthy := h.healthy

	url := h.baseURL + h.config.HealthEndpoint
	resp, err := h.client.Get(url)
	if err != nil {
		h.unhealthyCount++
		if h.unhealthyCount >= h.config.HealthThreshold {
			h.healthy = false
		}
		h.notifyTransition(wasHealthy)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		h.unhealthyCount = 0
		h.healthy = true
	} else {
		h.unhealthyCount++
		if h.unhealthyCount >= h.config.HealthThreshold {
			h.healthy = false
		}
	}
	h.notifyTransition(wasHealthy)
}

func (h *HealthChecker) notifyTransition(wasHealthy bool) {
	if wasHealthy == h.healthy {
		return
	}
	if h.healthy {
		h.logger.Info("Runtime became healthy")
	} else {
		h.logger.Warn("Runtime became unhealthy", "consecutive_failures", h.unhealthyCount)
	}
	if h.onHealthChange != nil {
		// Call outside lock to prevent deadlock — copy callback ref
		cb := h.onHealthChange
		go cb(h.healthy)
	}
}
```

- [ ] **Step 3: Add auto-restart logic to RuntimeManager**

Add auto-restart state tracking and logic to `internal/llm/runtime_manager.go`:

```go
// restartState tracks auto-restart attempts for a provider.
type restartState struct {
	attempts     int
	lastRestart  time.Time
	lastFailure  time.Time
}

// RuntimeManager manages local LLM runtime lifecycle.
type RuntimeManager struct {
	configs        map[string]*RuntimeConfig
	processes      map[string]*RuntimeProcess
	healthCheckers map[string]*HealthChecker
	restartStates  map[string]*restartState
	mu             sync.Mutex
	logger         *slog.Logger
}
```

Update `NewRuntimeManager`:

```go
func NewRuntimeManager(logger *slog.Logger) *RuntimeManager {
	return &RuntimeManager{
		configs:        make(map[string]*RuntimeConfig),
		processes:      make(map[string]*RuntimeProcess),
		healthCheckers: make(map[string]*HealthChecker),
		restartStates:  make(map[string]*restartState),
		logger:         logger,
	}
}
```

Update `RegisterConfig` to wire the health change callback:

```go
func (m *RuntimeManager) RegisterConfig(providerID string, cfg *RuntimeConfig, baseURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.configs[providerID] = cfg

	proc := NewRuntimeProcess(cfg)
	m.processes[providerID] = proc

	hc := NewHealthChecker(cfg, baseURL)
	m.healthCheckers[providerID] = hc

	// Wire auto-restart callback
	if cfg.RestartEnabled {
		m.restartStates[providerID] = &restartState{}
		providerID := providerID // capture for closure
		hc.OnHealthChange(func(healthy bool) {
			if !healthy {
				go m.attemptAutoRestart(providerID)
			} else {
				// Reset failure count if healthy for long enough
				m.mu.Lock()
				if rs, ok := m.restartStates[providerID]; ok {
					if !rs.lastFailure.IsZero() && time.Since(rs.lastFailure) > m.configs[providerID].RestartResetAfter {
						rs.attempts = 0
					}
				}
				m.mu.Unlock()
			}
		})
	}

	m.logger.Info("Registered runtime config", "provider", providerID, "runtime", cfg.Type, "auto_restart", cfg.RestartEnabled)
	return nil
}
```

Add the `attemptAutoRestart` method:

```go
func (m *RuntimeManager) attemptAutoRestart(providerID string) {
	m.mu.Lock()
	cfg, ok := m.configs[providerID]
	if !ok || !cfg.RestartEnabled {
		m.mu.Unlock()
		return
	}

	rs := m.restartStates[providerID]
	if rs == nil {
		m.mu.Unlock()
		return
	}

	// Check max attempts
	if rs.attempts >= cfg.RestartMaxAttempts {
		m.logger.Error("Auto-restart max attempts reached, giving up",
			"provider", providerID, "attempts", rs.attempts)
		m.mu.Unlock()
		return
	}

	// Check cooldown
	if !rs.lastRestart.IsZero() && time.Since(rs.lastRestart) < cfg.RestartCooldown {
		m.mu.Unlock()
		return
	}

	rs.attempts++
	rs.lastRestart = time.Now()
	rs.lastFailure = time.Now()
	m.mu.Unlock()

	m.logger.Warn("Attempting auto-restart",
		"provider", providerID,
		"attempt", rs.attempts,
		"max", cfg.RestartMaxAttempts)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.SpawnTimeout)
	defer cancel()

	if err := m.RestartProvider(ctx, providerID); err != nil {
		m.logger.Error("Auto-restart failed", "provider", providerID, "error", err)
	} else {
		m.logger.Info("Auto-restart succeeded", "provider", providerID, "attempt", rs.attempts)
	}
}
```

- [ ] **Step 4: Update config/models.json5**

Add restart policy to the local provider lifecycle section:

```json5
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
  },
  "restart_policy": {
    "enabled": true,
    "max_attempts": 3,
    "cooldown_seconds": 30,
    "reset_after_seconds": 300
  }
}
```

- [ ] **Step 5: Run tests and build**

```bash
go build ./...
go test ./internal/llm/... -v -count=1
```

- [ ] **Step 6: Commit**

```bash
git add internal/llm/runtime_config.go internal/llm/health_checker.go internal/llm/runtime_manager.go config/models.json5
git commit -m "feat(llm): add auto-restart policy with configurable attempts and cooldown"
```

---

## Task 3: Create RuntimeService (Service Layer)

**Files:**
- Create: `internal/services/runtime_service.go`
- Create: `internal/services/runtime_service_test.go`

- [ ] **Step 1: Implement RuntimeService**

Create `internal/services/runtime_service.go`:

```go
package services

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
)

// RuntimeService provides runtime management operations through the service layer.
type RuntimeService struct {
	manager *llm.RuntimeManager
}

// NewRuntimeService creates a runtime service.
func NewRuntimeService(manager *llm.RuntimeManager) *RuntimeService {
	return &RuntimeService{manager: manager}
}

// RuntimeStatusResponse is the response for runtime status queries.
type RuntimeStatusResponse struct {
	Runtimes []llm.RuntimeStatus `json:"runtimes"`
}

// Status returns the status of all managed runtimes.
func (s *RuntimeService) Status(ctx context.Context) (*RuntimeStatusResponse, error) {
	if s.manager == nil {
		return nil, fmt.Errorf("runtime manager not available")
	}
	statuses := s.manager.Status()
	return &RuntimeStatusResponse{Runtimes: statuses}, nil
}

// StatusForProvider returns the status of a specific provider.
func (s *RuntimeService) StatusForProvider(ctx context.Context, providerID string) (*llm.RuntimeStatus, error) {
	if s.manager == nil {
		return nil, fmt.Errorf("runtime manager not available")
	}
	status, ok := s.manager.StatusForProvider(providerID)
	if !ok {
		return nil, fmt.Errorf("provider %s not found", providerID)
	}
	return &status, nil
}

// StartProvider starts a specific provider's runtime.
func (s *RuntimeService) StartProvider(ctx context.Context, providerID string) error {
	if s.manager == nil {
		return fmt.Errorf("runtime manager not available")
	}
	return s.manager.StartProvider(ctx, providerID)
}

// StopProvider stops a specific provider's runtime.
func (s *RuntimeService) StopProvider(ctx context.Context, providerID string) error {
	if s.manager == nil {
		return fmt.Errorf("runtime manager not available")
	}
	return s.manager.StopProvider(ctx, providerID)
}

// RestartProvider restarts a specific provider's runtime.
func (s *RuntimeService) RestartProvider(ctx context.Context, providerID string) error {
	if s.manager == nil {
		return fmt.Errorf("runtime manager not available")
	}
	return s.manager.RestartProvider(ctx, providerID)
}
```

- [ ] **Step 2: Write tests**

Create `internal/services/runtime_service_test.go`:

```go
package services

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/services"
)

func TestRuntimeService_Status_NilManager(t *testing.T) {
	svc := services.NewRuntimeService(nil)
	_, err := svc.Status(context.Background())
	if err == nil {
		t.Fatal("expected error with nil manager")
	}
}

func TestRuntimeService_StatusForProvider_NilManager(t *testing.T) {
	svc := services.NewRuntimeService(nil)
	_, err := svc.StatusForProvider(context.Background(), "local")
	if err == nil {
		t.Fatal("expected error with nil manager")
	}
}

func TestRuntimeService_Start_NilManager(t *testing.T) {
	svc := services.NewRuntimeService(nil)
	err := svc.StartProvider(context.Background(), "local")
	if err == nil {
		t.Fatal("expected error with nil manager")
	}
}

func TestRuntimeService_Status(t *testing.T) {
	mgr := llm.NewRuntimeManager(nil)
	svc := services.NewRuntimeService(mgr)

	resp, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// No runtimes registered, so empty slice
	if len(resp.Runtimes) != 0 {
		t.Errorf("expected 0 runtimes, got %d", len(resp.Runtimes))
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/services/runtime_service_test.go ./internal/services/runtime_service.go -v -count=1
```

Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/services/runtime_service.go internal/services/runtime_service_test.go
git commit -m "feat(services): add RuntimeService for HTTP/RPC transport parity"
```

---

## Task 4: Wire RuntimeService into Service Registry and Daemon

**Files:**
- Modify: `internal/services/service.go`
- Modify: `internal/daemon/components.go`
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Add RuntimeService to ServiceRegistry**

In `internal/services/service.go`, add to the `ServiceRegistry` struct:

```go
Runtime *RuntimeService
```

Add to the `Config` struct:

```go
RuntimeManager *llm.RuntimeManager
```

Add to `NewRegistry` after the Calendar block:

```go
if cfg.RuntimeManager != nil {
	reg.Runtime = NewRuntimeService(cfg.RuntimeManager)
}
```

- [ ] **Step 2: Pass RuntimeManager to service registry in daemon.go**

In `internal/daemon/daemon.go`, find the `services.NewRegistry(services.Config{...})` call and add:

```go
RuntimeManager: components.RuntimeManager,
```

- [ ] **Step 3: Add runtime health to daemon status RPC response**

In `internal/daemon/daemon.go`, find the `registerBuiltinHandlers` function and locate the `status` handler (the one that builds the `DaemonStatusResponse`). After the existing status map construction, add runtime health data:

```go
// Add runtime health info if available
if components != nil && components.RuntimeManager != nil {
	runtimeStatuses := components.RuntimeManager.Status()
	if len(runtimeStatuses) > 0 {
		runtimeInfo := make(map[string]any)
		for _, rs := range runtimeStatuses {
			runtimeInfo[rs.ProviderID] = map[string]any{
				"running": rs.Running,
				"healthy": rs.Healthy,
				"pid":     rs.PID,
			}
		}
		status["runtimes"] = runtimeInfo
	}
}
```

- [ ] **Step 4: Build and verify**

```bash
go build ./...
```

Expected: Clean build

- [ ] **Step 5: Commit**

```bash
git add internal/services/service.go internal/daemon/components.go internal/daemon/daemon.go
git commit -m "feat(daemon): wire RuntimeService into service registry and daemon status"
```

---

## Task 5: Add HTTP API Endpoints for Runtime Management

**Files:**
- Modify: `internal/comm/http/server.go`

- [ ] **Step 1: Add runtime HTTP handlers**

Add to `internal/comm/http/server.go`. First add the route registrations in `setupRESTRoutes`:

```go
// Runtime management endpoints
mux.HandleFunc("GET /api/v1/runtime/status", s.handleRuntimeStatus)
mux.HandleFunc("GET /api/v1/runtime/status/{provider}", s.handleRuntimeStatusProvider)
mux.HandleFunc("POST /api/v1/runtime/start/{provider}", s.handleRuntimeStart)
mux.HandleFunc("POST /api/v1/runtime/stop/{provider}", s.handleRuntimeStop)
mux.HandleFunc("POST /api/v1/runtime/restart/{provider}", s.handleRuntimeRestart)
```

Add the handler methods:

```go
func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	resp, err := s.services.Runtime.Status(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRuntimeStatusProvider(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "local"
	}
	resp, err := s.services.Runtime.StatusForProvider(r.Context(), provider)
	if err != nil {
		s.handleServiceError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRuntimeStart(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "local"
	}
	if err := s.services.Runtime.StartProvider(r.Context(), provider); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (s *Server) handleRuntimeStop(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "local"
	}
	if err := s.services.Runtime.StopProvider(r.Context(), provider); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) handleRuntimeRestart(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Runtime == nil {
		s.writeError(w, http.StatusServiceUnavailable, "runtime service not available")
		return
	}
	provider := r.PathValue("provider")
	if provider == "" {
		provider = "local"
	}
	if err := s.services.Runtime.RestartProvider(r.Context(), provider); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}
```

- [ ] **Step 2: Build and verify**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/comm/http/server.go
git commit -m "feat(http): add runtime management API endpoints"
```

---

## Task 6: Add RPC Handlers for Runtime Management

**Files:**
- Create: `internal/rpc/runtime.go`
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Create RPC handlers**

Create `internal/rpc/runtime.go`:

```go
package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/services"
)

// RuntimeRPCHandler handles runtime-related RPC methods.
type RuntimeRPCHandler struct {
	service *services.RuntimeService
}

// NewRuntimeRPCHandler creates a new runtime RPC handler.
func NewRuntimeRPCHandler(service *services.RuntimeService) *RuntimeRPCHandler {
	return &RuntimeRPCHandler{service: service}
}

// RegisterRuntimeMethods registers runtime RPC methods.
func (h *RuntimeRPCHandler) RegisterRuntimeMethods(server *Server) {
	server.RegisterHandler("runtime.status", h.handleStatus)
	server.RegisterHandler("runtime.start", h.handleStart)
	server.RegisterHandler("runtime.stop", h.handleStop)
	server.RegisterHandler("runtime.restart", h.handleRestart)
}

func (h *RuntimeRPCHandler) handleStatus(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("runtime service not available")
	}

	var req struct {
		Provider string `json:"provider"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &req)
	}

	if req.Provider != "" {
		resp, err := h.service.StatusForProvider(ctx, req.Provider)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			RPCKeyStatus: "ok",
			"runtime":    resp,
		}, nil
	}

	resp, err := h.service.Status(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		RPCKeyStatus: "ok",
		"runtimes":   resp.Runtimes,
	}, nil
}

func (h *RuntimeRPCHandler) handleStart(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("runtime service not available")
	}
	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Provider == "" {
		req.Provider = "local"
	}
	if err := h.service.StartProvider(ctx, req.Provider); err != nil {
		return nil, err
	}
	return map[string]any{
		RPCKeyStatus: "started",
		"provider":   req.Provider,
	}, nil
}

func (h *RuntimeRPCHandler) handleStop(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("runtime service not available")
	}
	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Provider == "" {
		req.Provider = "local"
	}
	if err := h.service.StopProvider(ctx, req.Provider); err != nil {
		return nil, err
	}
	return map[string]any{
		RPCKeyStatus: "stopped",
		"provider":   req.Provider,
	}, nil
}

func (h *RuntimeRPCHandler) handleRestart(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("runtime service not available")
	}
	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Provider == "" {
		req.Provider = "local"
	}
	if err := h.service.RestartProvider(ctx, req.Provider); err != nil {
		return nil, err
	}
	return map[string]any{
		RPCKeyStatus: "restarted",
		"provider":   req.Provider,
	}, nil
}
```

- [ ] **Step 2: Wire RPC handlers in daemon.go**

In `internal/daemon/daemon.go`, find the RPC handler registration section (after other handler registrations) and add:

```go
// Runtime management handlers
if svcRegistry.Runtime != nil {
	runtimeHandler := rpc.NewRuntimeRPCHandler(svcRegistry.Runtime)
	runtimeHandler.RegisterRuntimeMethods(rpcServer)
}
```

- [ ] **Step 3: Build and verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/rpc/runtime.go internal/daemon/daemon.go
git commit -m "feat(rpc): add runtime management RPC handlers"
```

---

## Task 7: Add Metrics Integration

**Files:**
- Modify: `internal/llm/runtime_manager.go`
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Add metrics recording methods to RuntimeManager**

Add to `internal/llm/runtime_manager.go`. Add a `metricsRecorder` field and setter:

```go
// MetricsRecorder records runtime-related metrics.
type MetricsRecorder interface {
	RecordRuntimeHealth(providerID string, healthy bool)
	RecordRuntimeSpawn(providerID string, duration time.Duration, success bool)
	RecordRuntimeRestart(providerID string, attempt int, success bool)
}

// RuntimeManager manages local LLM runtime lifecycle.
type RuntimeManager struct {
	configs        map[string]*RuntimeConfig
	processes      map[string]*RuntimeProcess
	healthCheckers map[string]*HealthChecker
	restartStates  map[string]*restartState
	metrics        MetricsRecorder
	mu             sync.Mutex
	logger         *slog.Logger
}
```

Add setter:

```go
// SetMetricsRecorder sets the metrics recorder for runtime events.
func (m *RuntimeManager) SetMetricsRecorder(rec MetricsRecorder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = rec
}
```

Add `time` to imports if not present.

Add metrics recording calls in `StartProvider`, `StopProvider`, and `attemptAutoRestart`:

In `StartProvider`, after the `proc.Start(ctx)` call, add timing and recording:

```go
start := time.Now()
if err := proc.Start(ctx); err != nil {
	m.recordSpawn(providerID, time.Since(start), false)
	return fmt.Errorf("failed to start runtime %s: %w", providerID, err)
}
m.recordSpawn(providerID, time.Since(start), true)
```

Add helper:

```go
func (m *RuntimeManager) recordSpawn(providerID string, duration time.Duration, success bool) {
	if m.metrics != nil {
		m.metrics.RecordRuntimeSpawn(providerID, duration, success)
	}
}

func (m *RuntimeManager) recordRestart(providerID string, attempt int, success bool) {
	if m.metrics != nil {
		m.metrics.RecordRuntimeRestart(providerID, attempt, success)
	}
}
```

In `attemptAutoRestart`, update the restart call to record metrics:

```go
if err := m.RestartProvider(ctx, providerID); err != nil {
	m.logger.Error("Auto-restart failed", "provider", providerID, "error", err)
	m.recordRestart(providerID, rs.attempts, false)
} else {
	m.logger.Info("Auto-restart succeeded", "provider", providerID, "attempt", rs.attempts)
	m.recordRestart(providerID, rs.attempts, true)
}
```

- [ ] **Step 2: Implement metrics adapter in daemon.go**

In `internal/daemon/daemon.go`, add a metrics adapter after the `metricsStoreWrapper`:

```go
// runtimeMetricsAdapter adapts metrics.Store to implement llm.MetricsRecorder.
type runtimeMetricsAdapter struct {
	store *metrics.Store
}

func (a *runtimeMetricsAdapter) RecordRuntimeHealth(providerID string, healthy bool) {
	if a.store == nil {
		return
	}
	val := 0.0
	if healthy {
		val = 1.0
	}
	a.store.Record("runtime.healthy", val, map[string]string{
		"provider": providerID,
	})
}

func (a *runtimeMetricsAdapter) RecordRuntimeSpawn(providerID string, duration time.Duration, success bool) {
	if a.store == nil {
		return
	}
	tags := map[string]string{"provider": providerID}
	a.store.Record("runtime.spawn.duration", duration.Seconds(), tags)
	if success {
		a.store.Record("runtime.spawn.success", 1, tags)
	} else {
		a.store.Record("runtime.spawn.failure", 1, tags)
	}
}

func (a *runtimeMetricsAdapter) RecordRuntimeRestart(providerID string, attempt int, success bool) {
	if a.store == nil {
		return
	}
	tags := map[string]string{
		"provider": providerID,
	}
	a.store.Record("runtime.restart.attempts", float64(attempt), tags)
	if success {
		a.store.Record("runtime.restart.success", 1, tags)
	} else {
		a.store.Record("runtime.restart.failure", 1, tags)
	}
}
```

Wire the adapter after `RuntimeManager` creation (in `components.go` or `daemon.go` where RuntimeManager is available):

```go
if metricsStore != nil && components.RuntimeManager != nil {
	components.RuntimeManager.SetMetricsRecorder(&runtimeMetricsAdapter{store: metricsStore})
}
```

- [ ] **Step 3: Build and verify**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/llm/runtime_manager.go internal/daemon/daemon.go
git commit -m "feat(metrics): add runtime lifecycle metrics (spawn, health, restart)"
```

---

## Task 8: CLI Improvements

**Files:**
- Modify: `cmd/meept/runtime.go`

- [ ] **Step 1: Add --wait flag to runtime start**

Add a `--wait` flag to `newRuntimeStartCmd`:

```go
func newRuntimeStartCmd() *cobra.Command {
	var provider string
	var wait bool

	cmd := &cobra.Command{
		Use:   "start [provider]",
		Short: "Start the local LLM runtime",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				provider = args[0]
			}
			return runRuntimeStart(cmd.Context(), provider, wait)
		},
	}

	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for runtime to become healthy before returning")

	return cmd
}
```

Update `runRuntimeStart` signature to accept `wait bool`:

```go
func runRuntimeStart(ctx context.Context, provider string, wait bool) error {
	_, pc, err := loadRuntimeConfig(provider)
	if err != nil {
		return err
	}

	rtCfg, err := llm.ValidateAndNormalize(*pc.Lifecycle)
	if err != nil {
		return fmt.Errorf("invalid lifecycle config: %w", err)
	}

	pidFile := rtCfg.PIDFile

	// Check if already running
	if data, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(string(data)); err == nil {
			if checkProcessAlive(pid) {
				return fmt.Errorf("runtime %s is already running (PID: %d)", provider, pid)
			}
		}
		os.Remove(pidFile)
	}

	// Spawn the process
	runtimeProc := llm.NewRuntimeProcess(rtCfg)
	if err := runtimeProc.Start(ctx); err != nil {
		return fmt.Errorf("failed to start runtime: %w", err)
	}

	fmt.Printf("Runtime %s started (PID: %d)\n", provider, runtimeProc.PID())

	if wait {
		baseURL := pc.Options.BaseURL
		healthEndpoint := pc.Lifecycle.HealthCheck.Endpoint
		if healthEndpoint == "" {
			healthEndpoint = "/health"
		}
		hc := llm.NewHealthChecker(rtCfg, baseURL)

		fmt.Printf("Waiting for runtime to become healthy")
		if err := hc.WaitForHealthy(ctx, rtCfg.SpawnTimeout); err != nil {
			fmt.Printf(" - timeout\n")
			return fmt.Errorf("runtime did not become healthy within %v: %w", rtCfg.SpawnTimeout, err)
		}
		fmt.Printf(" - healthy\n")
	}

	return nil
}
```

- [ ] **Step 2: Add --format flag to runtime status**

Add `--format` flag to `newRuntimeStatusCmd`:

```go
func newRuntimeStatusCmd() *cobra.Command {
	var provider string
	var format string

	cmd := &cobra.Command{
		Use:   "status [provider]",
		Short: "Show local LLM runtime status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				provider = args[0]
			}
			return runRuntimeStatusFormatted(cmd.Context(), provider, format)
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")

	return cmd
}
```

Add `runRuntimeStatusFormatted` that delegates to existing `runRuntimeStatus` for text, or outputs JSON:

```go
func runRuntimeStatusFormatted(ctx context.Context, provider, format string) error {
	if provider == "" {
		provider = "local"
	}

	_, pc, err := loadRuntimeConfig(provider)
	if err != nil {
		return err
	}

	pidFile := pidFileFromConfig(pc.Lifecycle)

	data, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		if format == "json" {
			return jsonOutput(map[string]any{
				"provider": provider,
				"running":  false,
				"pid":      nil,
			})
		}
		fmt.Printf("Runtime %s: not running (no PID file)\n", provider)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("invalid PID in file: %w", err)
	}

	running := checkProcessAlive(pid)
	if !running {
		// Note: do NOT remove stale PID file on status query (side effect fix)
		if format == "json" {
			return jsonOutput(map[string]any{
				"provider": provider,
				"running":  false,
				"pid":      pid,
				"note":     "process dead, stale PID file",
			})
		}
		fmt.Printf("Runtime %s: not running (process dead, PID: %d)\n", provider, pid)
		return nil
	}

	baseURL := pc.Options.BaseURL
	healthEndpoint := pc.Lifecycle.HealthCheck.Endpoint
	if healthEndpoint == "" {
		healthEndpoint = "/health"
	}

	if format == "json" {
		return jsonOutput(map[string]any{
			"provider":       provider,
			"running":        true,
			"pid":            pid,
			"health_endpoint": baseURL + healthEndpoint,
			"pid_file":        pidFile,
		})
	}

	fmt.Printf("Runtime %s: running (PID: %d)\n", provider, pid)
	fmt.Printf("  Health endpoint:  %s%s\n", baseURL, healthEndpoint)
	fmt.Printf("  PID file:         %s\n", pidFile)
	return nil
}

func jsonOutput(data any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
```

- [ ] **Step 3: Fix status command side effect**

The old `runRuntimeStatus` had `os.Remove(pidFile)` for stale PID files. The new `runRuntimeStatusFormatted` does NOT remove stale PID files on status queries, fixing the side effect. Add `encoding/json` to imports.

- [ ] **Step 4: Build and verify**

```bash
go build ./cmd/meept/...
```

- [ ] **Step 5: Commit**

```bash
git add cmd/meept/runtime.go
git commit -m "feat(cli): add --wait and --format flags to runtime commands, fix status side effect"
```

---

## Task 9: Add Runtime Status to Daemon Status Endpoint

**Files:**
- Modify: `internal/comm/http/server.go`

- [ ] **Step 1: Add runtime info to handleDaemonStatus**

In `internal/comm/http/server.go`, find `handleDaemonStatus`. After the status map is built, add runtime data:

```go
// Add runtime health info if available
if s.services != nil && s.services.Runtime != nil {
	if resp, err := s.services.Runtime.Status(r.Context()); err == nil && len(resp.Runtimes) > 0 {
		runtimes := make(map[string]any)
		for _, rt := range resp.Runtimes {
			runtimes[rt.ProviderID] = map[string]any{
				"running": rt.Running,
				"healthy": rt.Healthy,
				"pid":     rt.PID,
				"runtime": rt.Runtime,
			}
		}
		status["runtimes"] = runtimes
	}
}
```

- [ ] **Step 2: Build and verify**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/comm/http/server.go
git commit -m "feat(http): include runtime health in daemon status response"
```

---

## Task 10: HTTP Endpoint Tests

**Files:**
- Modify: `internal/comm/http/unified_http_test.go`

- [ ] **Step 1: Add runtime endpoint tests**

Add to `internal/comm/http/unified_http_test.go`:

```go
func TestUnifiedHTTPServer_RuntimeStatus_NoRuntime(t *testing.T) {
	// Server without runtime service — should return 503
	baseURL, cancel := startTestServer(t)
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/runtime/status")
	if err != nil {
		t.Fatalf("runtime status request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusServiceUnavailable {
		t.Errorf("expected 503 without runtime service, got %d", resp.StatusCode)
	}
}

func TestUnifiedHTTPServer_RuntimeStatus_WithManager(t *testing.T) {
	msgBus := bus.New(nil, nil)
	mgr := llm.NewRuntimeManager(nil)

	svcRegistry := &services.ServiceRegistry{
		Bus:     services.NewBusService(msgBus),
		Runtime: services.NewRuntimeService(mgr),
	}

	cfg := http.DefaultServerConfig()
	cfg.Addr = ":0"
	srv := http.NewServer(cfg, nil, nil, nil, svcRegistry, nil)
	if srv == nil {
		t.Fatal("failed to create server")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Start(ctx)

	// Wait for server
	for i := 0; i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", "127.0.0.1"+srv.Addr(), time.Second)
		if err == nil {
			conn.Close()
			break
		}
	}

	addr := srv.Addr()
	host, port, _ := net.SplitHostPort(addr)
	if host == "" || host == "::" {
		host = "127.0.0.1"
	}
	baseURL := "http://" + host + ":" + port

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/runtime/status")
	if err != nil {
		t.Fatalf("runtime status request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
}
```

- [ ] **Step 2: Run all HTTP tests**

```bash
go test ./internal/comm/http/... -v -count=1 -run "Runtime|Unified"
```

- [ ] **Step 3: Commit**

```bash
git add internal/comm/http/unified_http_test.go
git commit -m "test(http): add runtime endpoint tests"
```

---

## Task 11: Update Documentation

**Files:**
- Modify: `docs/configuration/llm-lifecycle.md`

- [ ] **Step 1: Document new features**

Add to `docs/configuration/llm-lifecycle.md` after the "How It Works" section:

```markdown
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
```

- [ ] **Step 2: Commit**

```bash
git add docs/configuration/llm-lifecycle.md
git commit -m "docs: document auto-restart, HTTP API, RPC, metrics, and daemon status"
```

---

## Summary

| Task | Feature | Files |
|------|---------|-------|
| 1 | Fix AutoStop bug, add individual start/stop/restart, status methods | `runtime_manager.go` |
| 2 | Auto-restart on unhealthy with configurable policy | `runtime_config.go`, `health_checker.go`, `runtime_manager.go` |
| 3 | RuntimeService (service layer) | `services/runtime_service.go` |
| 4 | Wire into service registry and daemon status | `service.go`, `components.go`, `daemon.go` |
| 5 | HTTP API endpoints | `server.go` |
| 6 | RPC handlers | `rpc/runtime.go` |
| 7 | Metrics integration | `runtime_manager.go`, `daemon.go` |
| 8 | CLI improvements (--wait, --format, side effect fix) | `cmd/meept/runtime.go` |
| 9 | Runtime info in daemon status HTTP endpoint | `server.go` |
| 10 | HTTP endpoint tests | `unified_http_test.go` |
| 11 | Documentation updates | `llm-lifecycle.md` |
