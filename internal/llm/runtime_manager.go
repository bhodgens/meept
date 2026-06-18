package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// MetricsRecorder records runtime-related metrics.
type MetricsRecorder interface {
	RecordRuntimeHealth(providerID string, healthy bool)
	RecordRuntimeSpawn(providerID string, duration time.Duration, success bool)
	RecordRuntimeRestart(providerID string, attempt int, success bool)
}

// restartState tracks auto-restart attempts for a provider.
type restartState struct {
	attempts    int
	lastRestart time.Time
	lastFailure time.Time
}

// RuntimeManager manages local LLM runtime lifecycle.
type RuntimeManager struct {
	configs        map[string]*RuntimeConfig
	processes      map[string]*RuntimeProcess
	healthCheckers map[string]*HealthChecker
	restartStates  map[string]*restartState
	mu             sync.Mutex
	logger         *slog.Logger
	metrics        MetricsRecorder
}

// NewRuntimeManager creates a new manager.
func NewRuntimeManager(logger *slog.Logger) *RuntimeManager {
	return &RuntimeManager{
		configs:        make(map[string]*RuntimeConfig),
		processes:      make(map[string]*RuntimeProcess),
		healthCheckers: make(map[string]*HealthChecker),
		restartStates:  make(map[string]*restartState),
		logger:         logger,
	}
}

// SetMetricsRecorder sets the metrics recorder for runtime events.
func (m *RuntimeManager) SetMetricsRecorder(rec MetricsRecorder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = rec
}

// RegisterConfig registers a runtime configuration.
func (m *RuntimeManager) RegisterConfig(providerID string, cfg *RuntimeConfig, baseURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.configs[providerID] = cfg

	// Create process
	proc := NewRuntimeProcess(cfg)
	m.processes[providerID] = proc

	// Create health checker
	hc := NewHealthChecker(cfg, baseURL)
	m.healthCheckers[providerID] = hc

	// Wire auto-restart callback
	if cfg.RestartEnabled {
		m.restartStates[providerID] = &restartState{}
		pid := providerID // capture for closure
		hc.OnHealthChange(func(healthy bool) {
			if !healthy {
				go m.attemptAutoRestart(pid)
			} else {
				m.mu.Lock()
				if rs, ok := m.restartStates[pid]; ok {
					if !rs.lastFailure.IsZero() && time.Since(rs.lastFailure) > m.configs[pid].RestartResetAfter {
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

// StartAll starts all registered runtimes with auto_start=true.
func (m *RuntimeManager) StartAll(ctx context.Context) error {
	// Snapshot configs/processes/checkers under lock, then release
	// before doing any I/O (subprocess spawn, HTTP health polling).
	type startItem struct {
		providerID string
		cfg        *RuntimeConfig
		proc       *RuntimeProcess
		hc         *HealthChecker
	}

	m.mu.Lock()
	var items []startItem
	for providerID, cfg := range m.configs {
		if !cfg.AutoStart {
			continue
		}
		items = append(items, startItem{
			providerID: providerID,
			cfg:        cfg,
			proc:       m.processes[providerID],
			hc:         m.healthCheckers[providerID],
		})
	}
	m.mu.Unlock()

	for _, item := range items {
		m.logger.Info("Starting runtime", "provider", item.providerID)
		if err := item.proc.Start(ctx); err != nil {
			return fmt.Errorf("failed to start runtime %s: %w", item.providerID, err)
		}

		// Start health checker
		item.hc.Start(ctx)

		// Wait for healthy
		if err := item.hc.WaitForHealthy(ctx, item.cfg.SpawnTimeout); err != nil {
			item.proc.Stop(ctx)
			return fmt.Errorf("runtime %s did not become healthy: %w", item.providerID, err)
		}

		m.logger.Info("Runtime started and healthy", "provider", item.providerID)
	}

	return nil
}

// StopAll stops all running runtimes that have auto_stop_on_exit=true.
func (m *RuntimeManager) StopAll(ctx context.Context) error {
	// Snapshot processes/configs/checkers under lock, then release
	// before doing I/O (subprocess termination).
	type stopItem struct {
		providerID string
		autoStop   bool
		proc       *RuntimeProcess
		hc         *HealthChecker
	}

	m.mu.Lock()
	var items []stopItem
	for providerID, proc := range m.processes {
		cfg := m.configs[providerID]
		items = append(items, stopItem{
			providerID: providerID,
			autoStop:   cfg.AutoStop,
			proc:       proc,
			hc:         m.healthCheckers[providerID],
		})
	}
	m.mu.Unlock()

	for _, item := range items {
		if !item.autoStop {
			m.logger.Debug("Skipping runtime stop (auto_stop disabled)", "provider", item.providerID)
			continue
		}
		m.logger.Info("Stopping runtime", "provider", item.providerID)
		if err := item.proc.Stop(ctx); err != nil {
			m.logger.Error("Failed to stop runtime", "provider", item.providerID, "error", err)
		}
	}

	// Stop health checkers
	for _, item := range items {
		if item.hc != nil {
			item.hc.Stop()
			m.logger.Debug("Health checker stopped", "provider", item.providerID)
		}
	}

	return nil
}

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

// StartProvider starts a specific provider's runtime.
func (m *RuntimeManager) StartProvider(ctx context.Context, providerID string) error {
	// Snapshot under lock, then release for I/O.
	m.mu.Lock()
	cfg, ok := m.configs[providerID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("provider %s not registered", providerID)
	}

	proc := m.processes[providerID]
	hc := m.healthCheckers[providerID]
	m.mu.Unlock()

	m.logger.Info("Starting runtime", "provider", providerID)
	start := time.Now()
	if err := proc.Start(ctx); err != nil {
		m.recordSpawn(providerID, time.Since(start), false)
		return fmt.Errorf("failed to start runtime %s: %w", providerID, err)
	}
	m.recordSpawn(providerID, time.Since(start), true)

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
	// Snapshot under lock, then release for I/O.
	m.mu.Lock()
	proc, ok := m.processes[providerID]
	hc := m.healthCheckers[providerID]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("provider %s not registered", providerID)
	}

	m.logger.Info("Stopping runtime", "provider", providerID)
	if err := proc.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop runtime %s: %w", providerID, err)
	}

	// Stop health checker
	if hc != nil {
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

// GetHealthChecker returns the health checker for a provider.
func (m *RuntimeManager) GetHealthChecker(providerID string) (*HealthChecker, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hc, ok := m.healthCheckers[providerID]
	return hc, ok
}

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

	if rs.attempts >= cfg.RestartMaxAttempts {
		m.logger.Error("Auto-restart max attempts reached, giving up",
			"provider", providerID, "attempts", rs.attempts)
		m.mu.Unlock()
		return
	}

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
		m.recordRestart(providerID, rs.attempts, false)
	} else {
		m.logger.Info("Auto-restart succeeded", "provider", providerID, "attempt", rs.attempts)
		m.recordRestart(providerID, rs.attempts, true)
	}
}

// recordSpawn records a spawn metric.
//
// Callers that already hold m.mu must use recordSpawnLocked instead to avoid
// a self-deadlock (Go's sync.Mutex is non-reentrant).
func (m *RuntimeManager) recordSpawn(providerID string, duration time.Duration, success bool) {
	m.mu.Lock()
	rec := m.metrics
	m.mu.Unlock()
	if rec != nil {
		rec.RecordRuntimeSpawn(providerID, duration, success)
	}
}

// recordSpawnLocked is the lock-free variant for callers already holding m.mu.
// The Record* call happens after the caller has released the lock to avoid
// performing I/O (or callbacks) under the mutex — see StartProvider which
// snapshots the recorder before calling out. When the caller cannot easily
// defer the call past its unlock, it is acceptable to invoke rec.Record*
// under the lock because the metrics backend is expected to be non-blocking.
func (m *RuntimeManager) recordSpawnLocked(rec MetricsRecorder, providerID string, duration time.Duration, success bool) {
	if rec != nil {
		rec.RecordRuntimeSpawn(providerID, duration, success)
	}
}

func (m *RuntimeManager) recordRestart(providerID string, attempt int, success bool) {
	m.mu.Lock()
	rec := m.metrics
	m.mu.Unlock()
	if rec != nil {
		rec.RecordRuntimeRestart(providerID, attempt, success)
	}
}
