package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// RuntimeManager manages local LLM runtime lifecycle.
type RuntimeManager struct {
	configs        map[string]*RuntimeConfig
	processes      map[string]*RuntimeProcess
	healthCheckers map[string]*HealthChecker
	mu             sync.Mutex
	logger         *slog.Logger
}

// NewRuntimeManager creates a new manager.
func NewRuntimeManager(logger *slog.Logger) *RuntimeManager {
	return &RuntimeManager{
		configs:        make(map[string]*RuntimeConfig),
		processes:      make(map[string]*RuntimeProcess),
		healthCheckers: make(map[string]*HealthChecker),
		logger:         logger,
	}
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

// GetHealthChecker returns the health checker for a provider.
func (m *RuntimeManager) GetHealthChecker(providerID string) (*HealthChecker, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hc, ok := m.healthCheckers[providerID]
	return hc, ok
}
