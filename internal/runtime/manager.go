package runtime

import (
	"fmt"
	"log/slog"
	"sync"
)

// Manager provides access to execution backends.
type ContainerManager struct {
	mu            sync.RWMutex
	config        Config
	backends      map[string]ExecutionBackend
	defaultBackend string
	closed        bool
	logger        *slog.Logger
}

// NewManager creates a new runtime manager.
func NewContainerManager(cfg Config, logger *slog.Logger) (*ContainerManager, error) {
	m := &ContainerManager{
		config:   cfg,
		backends: make(map[string]ExecutionBackend),
		logger:   logger,
	}

	// Initialize local backend (always available)
	local := NewLocalBackend()
	m.backends["local"] = local
	m.defaultBackend = "local"

	// Set default backend from config
	if cfg.DefaultBackend != "" {
		if cfg.DefaultBackend != "local" && cfg.DefaultBackend != "docker" {
			return nil, fmt.Errorf("unknown default backend: %s (must be \"local\" or \"docker\")", cfg.DefaultBackend)
		}
		m.defaultBackend = cfg.DefaultBackend
	}

	// Initialize Docker backend if configured
	if cfg.DefaultBackend == "docker" || cfg.Docker.Image != "" {
		if err := m.initDockerBackend(cfg.Docker, logger); err != nil {
			logger.Warn("Docker backend unavailable, falling back to local", "error", err)
			m.defaultBackend = "local"
		}
	}

	return m, nil
}

// GetBackend returns a backend by name, or nil if not available.
func (m *ContainerManager) GetBackend(name string) ExecutionBackend {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil
	}

	return m.backends[name]
}

// GetDefaultBackend returns the default backend.
func (m *ContainerManager) GetDefaultBackend() ExecutionBackend {
	return m.GetBackend(m.defaultBackend)
}

// ListBackends returns names of available backends.
func (m *ContainerManager) ListBackends() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.backends))
	for name := range m.backends {
		names = append(names, name)
	}
	return names
}

// Close shuts down all backends.
func (m *ContainerManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	var lastErr error
	for name, backend := range m.backends {
		if err := backend.Close(); err != nil {
			lastErr = err
			m.logger.Warn("error closing backend", "backend", name, "error", err)
		}
		delete(m.backends, name)
	}

	m.closed = true
	return lastErr
}

// DefaultBackend returns the name of the configured default backend.
func (m *ContainerManager) DefaultBackend() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultBackend
}

// initDockerBackend initializes the Docker backend if Docker is available.
func (m *ContainerManager) initDockerBackend(cfg DockerConfig, logger *slog.Logger) error {
	// Set defaults
	image := cfg.Image
	if image == "" {
		image = "alpine:latest"
	}

	// Create DockerBackend (defined in docker.go); it handles all Docker setup internally
	backend, err := newDockerBackend(cfg, image, logger)
	if err != nil {
		return fmt.Errorf("failed to create Docker backend: %w", err)
	}

	m.mu.Lock()
	m.backends["docker"] = backend
	m.mu.Unlock()

	logger.Info("Docker backend initialized", "image", image)
	return nil
}
