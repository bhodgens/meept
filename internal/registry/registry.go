// Package registry provides component lifecycle management.
package registry

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/caimlas/meept/pkg/models"
)

// Component represents a daemon component that can be started and stopped.
type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Running() bool
}

// Registry manages component lifecycle and dependencies.
//
//nolint:revive // stutter with package name is intentional for API clarity
type Registry struct {
	mu         sync.RWMutex
	components map[string]Component
	order      []string // startup order
	logger     *slog.Logger
}

// New creates a new Registry.
func New(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		components: make(map[string]Component),
		order:      make([]string, 0),
		logger:     logger,
	}
}

// Register adds a component to the registry.
// Components are started in registration order.
func (r *Registry) Register(c Component) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := c.Name()
	if _, exists := r.components[name]; exists {
		return fmt.Errorf("component already registered: %s", name)
	}

	r.components[name] = c
	r.order = append(r.order, name)
	r.logger.Debug("registry: registered component", "name", name)
	return nil
}

// Get retrieves a component by name.
func (r *Registry) Get(name string) (Component, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.components[name]
	return c, ok
}

// StartAll starts all registered components in registration order.
func (r *Registry) StartAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, name := range r.order {
		c := r.components[name]
		r.logger.Info("registry: starting component", "name", name)
		if err := c.Start(ctx); err != nil {
			return fmt.Errorf("failed to start %s: %w", name, err)
		}
	}
	return nil
}

// StopAll stops all components in reverse registration order.
//
// CORE-7 FIX: Previously this method held r.mu.RLock while calling
// component.Stop(ctx), which could deadlock if a component's Stop()
// tried to acquire the same write lock. The fix collects all running
// components under the read lock, releases it, then calls Stop on each
// without holding the lock.
func (r *Registry) StopAll(ctx context.Context) error {
	// Collect components under lock to avoid holding lock during Stop()
	// This prevents deadlock if component's Stop() calls back into registry
	r.mu.RLock()
	type componentEntry struct {
		name      string
		component Component
	}
	toStop := make([]componentEntry, 0, len(r.order))
	for _, v := range slices.Backward(r.order) {
		name := v
		c := r.components[name]
		if c.Running() {
			toStop = append(toStop, componentEntry{name: name, component: c})
		}
	}
	r.mu.RUnlock()

	// Now stop components without holding lock
	var lastErr error
	for _, entry := range toStop {
		r.logger.Info("registry: stopping component", "name", entry.name)
		if err := entry.component.Stop(ctx); err != nil {
			r.logger.Error("registry: failed to stop component",
				"name", entry.name,
				"error", err,
			)
			lastErr = err
		}
	}
	return lastErr
}

// List returns information about all registered components.
func (r *Registry) List() []models.ComponentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]models.ComponentInfo, 0, len(r.components))
	for _, name := range r.order {
		c := r.components[name]
		infos = append(infos, models.ComponentInfo{
			Name:    name,
			Type:    fmt.Sprintf("%T", c),
			Running: c.Running(),
		})
	}
	return infos
}

// Count returns the number of registered components.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.components)
}
