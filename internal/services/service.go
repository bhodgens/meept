// Package services provides the core business logic for meept operations.
// Both RPC and HTTP transports call these services to ensure consistency.
package services

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/selfimprove"
)

// TaskService handles task operations.
type TaskService struct{}

// QueueService wraps the queue.Queue interface.
type QueueService struct {
	queue queue.Queue
}

// SessionService handles session operations.
type SessionService struct{}

// WorkerService handles worker operations.
type WorkerService struct{}

// PipelineService handles pipeline status.
type PipelineService struct{}

// SkillsService handles skills operations.
type SkillsService struct {
	registry *skills.Registry
	executor *skills.Executor
}

// SelfImproveService handles self-improvement operations.
type SelfImproveService struct {
	controller *selfimprove.Controller
}

// CacheService handles token cache operations.
type CacheService struct{}

// SecurityService handles security operations.
type SecurityService struct{}

// SchedulerService handles scheduler operations.
type SchedulerService struct{}

// BusService handles bus subscription operations.
type BusService struct {
	bus *bus.MessageBus
}

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
