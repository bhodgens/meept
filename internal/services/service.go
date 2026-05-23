// Package services provides the core business logic for meept operations.
// Both RPC and HTTP transports call these services to ensure consistency.
package services

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/calendar"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/skills"
	"github.com/caimlas/meept/internal/templates"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/worker"
	"github.com/caimlas/meept/pkg/security"
)

// ServiceRegistry holds all service instances.
//nolint:revive // stutter with package name is intentional for API clarity
type ServiceRegistry struct {
	Chat        *ChatService
	Memory      *MemoryService
	Task        *TaskService
	Queue       *QueueService
	Session     *SessionService
	SessionStore session.Store  // Exposed for MCP handler access
	Worker      *WorkerService
	Pipeline    *PipelineService
	Skills      *SkillsService
	SelfImprove *SelfImproveService
	Cache       *CacheService
	Security    *SecurityService
	Scheduler   *SchedulerService
	Bus         *BusService
	Templates   *TemplatesService
	Daemon      *DaemonService
	Model       *ModelService
	Calendar    *CalendarService
	Runtime     *RuntimeService
}

// Config holds dependencies for service instantiation.
// All fields are optional; services whose dependencies are nil will not be created.
type Config struct {
	Bus            *bus.MessageBus
	AgentRegistry  *agent.AgentRegistry
	Queue          queue.Queue
	MemoryManager  *memory.Manager
	TaskRegistry   *task.Registry
	SessionStore   session.Store
	WorkerPool     *worker.Pool
	SkillRegistry   *skills.Registry
	SkillExecutor   *skills.Executor
	TemplateRegistry *templates.Registry
	SelfImprove    *selfimprove.Controller
	TokenCache     *llm.TokenCacheCoordinator
	SecurityChecker *security.PermissionChecker
	Scheduler      *scheduler.Scheduler
	CalendarClient *calendar.Client
	DaemonController DaemonController
	RuntimeManager *llm.RuntimeManager
	PidFile        string
	StateDir       string
	BinPath        string
}

// NewRegistry creates all services with their dependencies.
// Services whose required dependencies are nil in cfg are left as nil
// in the returned registry, allowing HTTP handlers to return 503 gracefully.
func NewRegistry(cfg Config, logger *slog.Logger) (*ServiceRegistry, error) {
	if logger == nil {
		logger = slog.Default()
	}

	reg := &ServiceRegistry{
		// ChatService is always created if Bus is available;
		// it gracefully handles nil AgentRegistry (Steer/FollowUp return ErrUnavailable).
		Chat:     NewChatService(cfg.Bus, cfg.AgentRegistry, logger),
		Pipeline: NewPipelineService(),
		Bus:      NewBusService(cfg.Bus),
	}

	if cfg.Queue != nil {
		reg.Queue = NewQueueService(cfg.Queue)
	}
	if cfg.MemoryManager != nil {
		reg.Memory = NewMemoryService(cfg.MemoryManager)
	}
	if cfg.TaskRegistry != nil {
		reg.Task = NewTaskService(cfg.TaskRegistry)
	}
	if cfg.SessionStore != nil {
		reg.Session = NewSessionService(cfg.SessionStore)
		reg.SessionStore = cfg.SessionStore
	}
	if cfg.WorkerPool != nil {
		reg.Worker = NewWorkerService(cfg.WorkerPool)
	}
	if cfg.SkillRegistry != nil {
		reg.Skills = NewSkillsService(cfg.SkillRegistry, cfg.SkillExecutor)
	}
	if cfg.TemplateRegistry != nil {
		reg.Templates = NewTemplatesService(cfg.TemplateRegistry, cfg.SkillExecutor)
	}
	if cfg.SelfImprove != nil {
		reg.SelfImprove = NewSelfImproveService(cfg.SelfImprove)
	}
	if cfg.TokenCache != nil {
		reg.Cache = NewCacheService(cfg.TokenCache)
	}
	if cfg.SecurityChecker != nil {
		reg.Security = NewSecurityService(cfg.SecurityChecker)
	}
	if cfg.Scheduler != nil {
		reg.Scheduler = NewSchedulerService(cfg.Scheduler)
	}
	if cfg.DaemonController != nil || cfg.PidFile != "" {
		reg.Daemon = NewDaemonService(cfg.PidFile, cfg.StateDir, cfg.BinPath, cfg.DaemonController)
	}
	// ModelService is always available if stateDir is set (for credential store)
	reg.Model, _ = NewModelService("", cfg.StateDir)

	// CalendarService is available if client is configured
	if cfg.CalendarClient != nil {
		reg.Calendar = NewCalendarService(cfg.CalendarClient)
	}

	// RuntimeService is available if runtime manager is configured
	if cfg.RuntimeManager != nil {
		reg.Runtime = NewRuntimeService(cfg.RuntimeManager)
	}

	return reg, nil
}

// Start starts all startable services.
func (r *ServiceRegistry) Start(ctx context.Context) error {
	return nil
}

// Stop stops all services gracefully.
func (r *ServiceRegistry) Stop(ctx context.Context) error {
	return nil
}
