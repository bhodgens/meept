// Package services provides the core business logic for meept operations.
// Both RPC and HTTP transports call these services to ensure consistency.
package services

import (
	"context"
	"log/slog"
)

// ServiceRegistry holds all service instances.
type ServiceRegistry struct {
	Chat        *ChatService
	Memory      *MemoryService
	Task        *TaskService
	Queue       *QueueService
	Session     *SessionService
	Worker      *WorkerService
	Pipeline    *PipelineService
	Skills      *SkillsService
	SelfImprove *SelfImproveService
	Cache       *CacheService
	Security    *SecurityService
	Scheduler   *SchedulerService
	Bus         *BusService
}

// Config holds service configuration.
type Config struct {
	// Dependencies will be added as needed per service
}

// NewRegistry creates all services with their dependencies.
func NewRegistry(cfg Config, logger *slog.Logger) (*ServiceRegistry, error) {
	if logger == nil {
		logger = slog.Default()
	}
	_ = logger // TODO: use logger when services are implemented
	return &ServiceRegistry{}, nil
}

// Start starts all startable services.
func (r *ServiceRegistry) Start(ctx context.Context) error {
	return nil
}

// Stop stops all services gracefully.
func (r *ServiceRegistry) Stop(ctx context.Context) error {
	return nil
}
