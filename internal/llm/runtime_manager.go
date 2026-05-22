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
