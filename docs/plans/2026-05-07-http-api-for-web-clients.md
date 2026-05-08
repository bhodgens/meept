# HTTP API for Web Clients Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** Expose full meept daemon functionality over HTTP for web/remote clients while preserving existing RPC transport for CLI/TUI, using a shared service layer to eliminate drift.

**Architecture:** Extract business logic from RPC handlers into a service layer (`internal/services/`), then wire both RPC and HTTP transports to call the same service functions. This ensures zero drift — both transports execute identical logic.

**Tech Stack:** Go 1.24, `net/http` with `http.ServeMux`, JSON-RPC 2.0 (RPC), REST+JSON (HTTP), SQLite (existing), message bus (existing).

---

## Design Overview

### Service Layer Structure

```
internal/services/
  service.go          -- ServiceRegistry, common types, errors
  chat_service.go     -- Chat, conversation management
  memory_service.go   -- Query, recent, export
  task_service.go     -- CRUD, linking, steps
  queue_service.go    -- Enqueue, claim, complete, fail, retry, stats
  session_service.go  -- Create, attach, detach, messages
  worker_service.go   -- Add, remove, list, stats, scale
  pipeline_service.go -- Status
  skills_service.go   -- List, get, execute, triage
  selfimprove_service.go -- Detect, analyze, generate, validate, apply, reject
  cache_service.go    -- Stats, clear, invalidate
  security_service.go -- Query log, stats, record override, approve
  scheduler_service.go -- List jobs, add job
  bus_service.go      -- Subscribe, poll, unsubscribe
```

### Transport Wiring

```
┌─────────────┐    ┌──────────────┐    ┌─────────────────┐
│ HTTP Client │───▶│ HTTP Handlers│───▶│ Service Layer   │
└─────────────┘    └──────────────┘    └────────┬────────┘
                                                │
┌─────────────┐    ┌──────────────┐    ┌────────▼────────┐
│ RPC Client  │───▶│ RPC Handlers │───▶│ (same funcs)    │
└─────────────┘    └──────────────┘    └─────────────────┘
```

### Execution Model Choice

**Selected: Direct Service Calls** (not bus proxy, not RPC translation)

Rationale:
- Existing queue/task handlers already call `q.store.*()` and `t.store.*()` directly
- RPC proxy pattern (`internal/rpc/proxy.go`) publishes to bus and waits — adds latency/complexity
- Service layer can optionally use bus internally for notifications (best of both)

---

## Sprint 1: Foundation — Service Layer Skeleton

### Task 1: Create Service Registry and Common Types

**Files:**
- Create: `internal/services/service.go`
- Create: `internal/services/errors.go`
- Test: `internal/services/service_test.go`

**Step 1: Write `internal/services/service.go`**

```go
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
	// Add dependencies as needed per service
	// e.g., DB paths, bus, logger, etc.
}

// NewRegistry creates all services with their dependencies.
func NewRegistry(cfg Config, logger *slog.Logger) (*ServiceRegistry, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Initialize services in dependency order
	// Some services may depend on others (e.g., WorkerService needs QueueService)
	return &ServiceRegistry{
		// Will be populated in subsequent tasks
	}, nil
}

// Start starts all startable services.
func (r *ServiceRegistry) Start(ctx context.Context) error {
	// Start any background processes
	return nil
}

// Stop stops all services gracefully.
func (r *ServiceRegistry) Stop(ctx context.Context) error {
	// Cleanup resources
	return nil
}
```

**Step 2: Run `go build ./...` to verify syntax**

```bash
go build ./internal/services/...
# Expected: Success
```

**Step 3: Commit**

```bash
git add internal/services/service.go
git commit -m "feat(services): add service registry skeleton"
```

---

### Task 2: Define Service Error Types

**Files:**
- Create: `internal/services/errors.go`

**Step 1: Write standard error types**

```go
package services

import "errors"

// Standard service errors for consistent cross-transport handling.
var (
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrInternal      = errors.New("internal error")
	ErrTimeout       = errors.New("operation timed out")
	ErrUnavailable   = errors.New("service unavailable")
)

// ServiceError wraps errors with service context.
type ServiceError struct {
	Service string
	Op      string
	Err     error
}

func (e *ServiceError) Error() string {
	return e.Service + "." + e.Op + ": " + e.Err.Error()
}

func (e *ServiceError) Unwrap() error {
	return e.Err
}

// wrapError wraps an error with service and operation context.
func wrapError(service, op string, err error) error {
	if err == nil {
		return nil
	}
	return &ServiceError{Service: service, Op: op, Err: err}
}
```

**Step 2: Run `go build ./internal/services/...`**

```bash
go build ./internal/services/...
# Expected: Success
```

**Step 3: Commit**

```bash
git add internal/services/errors.go
git commit -m "feat(services): add standard error types"
```

---

## Sprint 2: Chat Service — Core Conversation

### Task 3: Chat Service Implementation

**Files:**
- Create: `internal/services/chat_service.go`
- Test: `internal/services/chat_service_test.go`

**Context:** Read `internal/rpc/proxy.go:52` — chat RPC publishes to `chat.request` and waits on `chat.response`. We'll extract the core logic.

**Step 1: Analyze existing chat flow**

The RPC proxy does NOT implement chat logic directly — it forwards to bus subscribers (agents). The service layer needs to:
1. Accept chat request with message + conversation ID
2. Publish to bus (existing pattern)
3. Wait for response from agent
4. Return response

**Step 2: Write ChatService**

```go
package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// ChatService handles chat operations.
type ChatService struct {
	bus    *bus.MessageBus
	logger *slog.Logger
}

// ChatRequest contains chat input.
type ChatRequest struct {
	Message        string `json:"message"`
	ConversationID string `json:"conversation_id"`
}

// ChatResponse contains chat output.
type ChatResponse struct {
	Reply        string `json:"reply"`
	Model        string `json:"model,omitempty"`
	TokensUsed   int    `json:"tokens_used,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
}

// NewChatService creates a chat service.
func NewChatService(msgBus *bus.MessageBus, logger *slog.Logger) *ChatService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChatService{
		bus:    msgBus,
		logger: logger,
	}
}

// Chat sends a message and waits for a response.
func (s *ChatService) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if req.Message == "" {
		return nil, wrapError("chat", "Chat", ErrInvalidInput)
	}
	if req.ConversationID == "" {
		return nil, wrapError("chat", "Chat", ErrInvalidInput)
	}

	// Create request message
	msgID := fmt.Sprintf("svc-chat-%d", time.Now().UnixNano())
	msg := &models.BusMessage{
		ID:      msgID,
		Type:    models.MessageTypeRequest,
		Topic:   "chat.request",
		Source:  "svc.chat",
		Payload: []byte(req.Message),
		ReplyTo: "chat.response",
	}

	// Create response channel
	respChan := make(chan *models.BusMessage, 1)
	replyTopic := "chat.res." + msgID
	sub := s.bus.Subscribe(msgID, replyTopic)
	defer s.bus.Unsubscribe(sub)

	// Watch for responses (context-aware)
	go func() {
		for {
			select {
			case resp, ok := <-sub.Channel:
				if !ok {
					return
				}
				if resp.ReplyTo == msgID {
					select {
					case respChan <- resp:
					default:
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Publish request
	s.bus.Publish("chat.request", msg)

	// Wait for response
	select {
	case resp := <-respChan:
		var reply struct {
			Reply      string `json:"reply"`
			Model      string `json:"model,omitempty"`
			TokensUsed int    `json:"tokens_used,omitempty"`
		}
		if err := json.Unmarshal(resp.Payload, &reply); err != nil {
			return &ChatResponse{Reply: string(resp.Payload)}, nil
		}
		return &ChatResponse{
			Reply:      reply.Reply,
			Model:      reply.Model,
			TokensUsed: reply.TokensUsed,
		}, nil
	case <-time.After(2 * time.Minute):
		return nil, wrapError("chat", "Chat", ErrTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
```

**Step 3: Run `go build ./...`**

```bash
go build ./...
# Expected: Success
```

**Step 4: Write minimal test**

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

func TestChatService_InvalidInput(t *testing.T) {
	bus := bus.New(nil, nil)
	svc := NewChatService(bus, nil)

	_, err := svc.Chat(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("expected error for empty message")
	}
}

func TestChatService_Timeout(t *testing.T) {
	bus := bus.New(nil, nil)
	svc := NewChatService(bus, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := svc.Chat(ctx, ChatRequest{
		Message:        "test",
		ConversationID: "test-conv",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
```

**Step 5: Run test**

```bash
go test ./internal/services/chat_service_test.go ./internal/services/chat_service.go ./internal/services/errors.go ./internal/services/service.go -v
# Expected: PASS (both tests)
```

**Step 6: Commit**

```bash
git add internal/services/chat_service.go internal/services/chat_service_test.go
git commit -m "feat(services): implement ChatService with bus integration"
```

---

## Sprint 3: Memory Service

### Task 4: Memory Service Implementation

**Files:**
- Create: `internal/services/memory_service.go`
- Test: `internal/services/memory_service_test.go`

**Context:** Memory RPC proxies to `memory.query`, `memory.recent`, `memory.export`. The service should call memory manager directly.

**Step 1: Read existing memory implementation**

```bash
# Check what memory manager looks like
head -100 internal/memory/manager.go
```

**Step 2: Write MemoryService**

```go
package services

import (
	"context"
	"encoding/json"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/pkg/models"
)

// MemoryService handles memory operations.
type MemoryService struct {
	manager *memory.Manager
	bus     *bus.MessageBus
	logger  *slog.Logger
}

// MemoryQueryRequest contains query parameters.
type MemoryQueryRequest struct {
	Query   string `json:"query"`
	Limit   int    `json:"limit,omitempty"`
	Category string `json:"category,omitempty"`
}

// MemoryResult contains retrieved memories.
type MemoryResult struct {
	Memories []*memory.Memory `json:"memories"`
	Count    int              `json:"count"`
}

// NewMemoryService creates a memory service.
func NewMemoryService(mgr *memory.Manager, msgBus *bus.MessageBus, logger *slog.Logger) *MemoryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &MemoryService{
		manager: mgr,
		bus:     msgBus,
		logger:  logger,
	}
}

// Query searches memories.
func (s *MemoryService) Query(ctx context.Context, req MemoryQueryRequest) (*MemoryResult, error) {
	if req.Query == "" {
		return nil, wrapError("memory", "Query", ErrInvalidInput)
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	// Call manager directly
	memories, err := s.manager.Search(ctx, req.Query, req.Limit)
	if err != nil {
		return nil, wrapError("memory", "Query", err)
	}

	return &MemoryResult{
		Memories: memories,
		Count:    len(memories),
	}, nil
}

// Recent gets recent memories.
func (s *MemoryService) Recent(ctx context.Context, limit int) (*MemoryResult, error) {
	if limit <= 0 {
		limit = 10
	}

	memories, err := s.manager.GetRecent(ctx, limit)
	if err != nil {
		return nil, wrapError("memory", "Recent", err)
	}

	return &MemoryResult{
		Memories: memories,
		Count:    len(memories),
	}, nil
}

// Export exports memories in specified format.
func (s *MemoryService) Export(ctx context.Context, format string, category string) ([]byte, error) {
	data, err := s.manager.Export(ctx, format, category)
	if err != nil {
		return nil, wrapError("memory", "Export", err)
	}
	return data, nil
}
```

**Step 3: Commit**

```bash
git add internal/services/memory_service.go
git commit -m "feat(services): implement MemoryService"
```

---

## Sprint 4: Task Service

### Task 5: Task Service Implementation

**Files:**
- Create: `internal/services/task_service.go`
- Test: `internal/services/task_service_test.go`

**Context:** Task RPC proxies to `task.create`, `task.get`, `task.list`, `task.update`, `task.cancel`, `task.delete`, `task.link`, `task.unlink`, `task.steps`.

**Step 1: Read existing task store**

```bash
# Find task store implementation
head -150 internal/task/store.go 2>/dev/null || head -150 internal/agent/task.go 2>/dev/null || find . -name "*.go" -path "*/task*" | head -5
```

**Step 2: Write TaskService**

```go
package services

import (
	"context"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// TaskService handles task operations.
type TaskService struct {
	store  TaskStore
	bus    *bus.MessageBus
	logger *slog.Logger
}

// TaskStore defines the task storage interface.
type TaskStore interface {
	Create(ctx context.Context, task *models.Task) error
	GetByID(ctx context.Context, id string) (*models.Task, error)
	List(ctx context.Context, filter TaskFilter) ([]*models.Task, error)
	Update(ctx context.Context, task *models.Task) error
	Cancel(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	Link(ctx context.Context, parentID, childID string) error
	Unlink(ctx context.Context, parentID, childID string) error
	GetSteps(ctx context.Context, id string) ([]*models.TaskStep, error)
}

// TaskFilter contains list filters.
type TaskFilter struct {
	Status   string
	Assignee string
	Limit    int
	Offset   int
}

// NewTaskService creates a task service.
func NewTaskService(store TaskStore, msgBus *bus.MessageBus, logger *slog.Logger) *TaskService {
	if logger == nil {
		logger = slog.Default()
	}
	return &TaskService{
		store:  store,
		bus:    msgBus,
		logger: logger,
	}
}

// Get retrieves a task by ID.
func (s *TaskService) Get(ctx context.Context, id string) (*models.Task, error) {
	task, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, wrapError("task", "Get", err)
	}
	if task == nil {
		return nil, wrapError("task", "Get", ErrNotFound)
	}
	return task, nil
}

// List returns tasks matching filter.
func (s *TaskService) List(ctx context.Context, filter TaskFilter) ([]*models.Task, error) {
	tasks, err := s.store.List(ctx, filter)
	if err != nil {
		return nil, wrapError("task", "List", err)
	}
	return tasks, nil
}
```

**Step 3: Commit**

```bash
git add internal/services/task_service.go
git commit -m "feat(services): implement TaskService skeleton"
```

---

## Sprint 5: Queue Service

### Task 6: Queue Service Implementation

**Files:**
- Create: `internal/services/queue_service.go`

**Context:** Already has `internal/queue/queue.go` with `Queue` interface. Wrap it.

**Step 1: Write QueueService (thin wrapper)**

```go
package services

import (
	"context"

	"github.com/caimlas/meept/internal/queue"
)

// QueueService wraps the queue.Queue interface.
type QueueService struct {
	queue queue.Queue
}

// NewQueueService creates a queue service.
func NewQueueService(q queue.Queue) *QueueService {
	return &QueueService{queue: q}
}

// Enqueue adds a job.
func (s *QueueService) Enqueue(ctx context.Context, job *queue.Job) error {
	return s.queue.Enqueue(ctx, job)
}

// Claim claims next job for worker.
func (s *QueueService) Claim(ctx context.Context, workerID string, caps []string) (*queue.Job, error) {
	return s.queue.Claim(ctx, workerID, caps)
}

// Complete marks job done.
func (s *QueueService) Complete(ctx context.Context, jobID string, result any) error {
	return s.queue.Complete(ctx, jobID, result)
}

// Fail marks job failed.
func (s *QueueService) Fail(ctx context.Context, jobID string, err error) error {
	return s.queue.Fail(ctx, jobID, err)
}

// Retry retries failed job.
func (s *QueueService) Retry(ctx context.Context, jobID string) error {
	return s.queue.Retry(ctx, jobID)
}

// Get retrieves job by ID.
func (s *QueueService) Get(ctx context.Context, jobID string) (*queue.Job, error) {
	return s.queue.Get(ctx, jobID)
}

// ListByState lists jobs by state.
func (s *QueueService) ListByState(ctx context.Context, state queue.JobState, limit int) ([]*queue.Job, error) {
	return s.queue.ListByState(ctx, state, limit)
}

// Stats returns queue stats.
func (s *QueueService) Stats(ctx context.Context) (*queue.QueueStats, error) {
	return s.queue.Stats(ctx)
}
```

**Step 2: Commit**

```bash
git add internal/services/queue_service.go
git commit -m "feat(services): implement QueueService wrapper"
```

---

## Sprint 6: Remaining Services (Parallelizable)

These services can be implemented in parallel by subagents since they're independent.

### Task 7a: Session Service

**Files:** `internal/services/session_service.go`

```go
// Session CRUD + messages + attach/detach/describe
```

### Task 7b: Worker Service

**Files:** `internal/services/worker_service.go`

```go
// Worker add/remove/list/stats/scale
```

### Task 7c: Skills Service

**Files:** `internal/services/skills_service.go`

```go
// Skills list/get/execute/triage — calls internal/skills.Registry and Executor
```

### Task 7d: SelfImprove Service

**Files:** `internal/services/selfimprove_service.go`

```go
// Calls internal/selfimprove.Controller directly (already done in RPC — copy logic)
```

### Task 7e: Cache Service

**Files:** `internal/services/cache_service.go`

```go
// Wraps token cache — stats/clear/invalidate
```

### Task 7f: Security Service

**Files:** `internal/services/security_service.go`

```go
// Security engine queries, override recording
```

### Task 7g: Scheduler Service

**Files:** `internal/services/scheduler_service.go`

```go
// Cron/schedule job management
```

### Task 7h: Bus Service

**Files:** `internal/services/bus_service.go`

```go
// Bus subscribe/poll/unsubscribe for event streaming
```

### Task 7i: Pipeline Service

**Files:** `internal/services/pipeline_service.go`

```go
// Pipeline status checks
```

---

## Sprint 7: Wire RPC to Service Layer

### Task 8: Update RPC Handlers to Call Services

**Files:**
- Modify: `internal/rpc/proxy.go`
- Modify: `internal/rpc/server.go`
- Modify: `internal/daemon/daemon.go`

**Step 1: Add services to daemon**

Modify `internal/daemon/daemon.go` to create service registry:

```go
// In Daemon struct, add:
services *services.ServiceRegistry

// In New(), after creating components:
svcCfg := services.Config{
    // Pass dependencies
}
svcRegistry, err := services.NewRegistry(svcCfg, d.logger)
if err != nil {
    return nil, fmt.Errorf("failed to create services: %w", err)
}
d.services = svcRegistry
```

**Step 2: Update RPC proxy to use services**

Modify `internal/rpc/proxy.go`:

```go
// Add services field
type ProxyHandler struct {
    bus       *bus.MessageBus
    services  *services.ServiceRegistry  // NEW
    pending   sync.Map
    // ...
}

// Update constructor
func NewProxyHandler(msgBus *bus.MessageBus, svcRegistry *services.ServiceRegistry) *ProxyHandler {
    return &ProxyHandler{
        bus:      msgBus,
        services: svcRegistry,
    }
}
```

**Step 3: Update chat handler to call service**

```go
server.RegisterHandler("chat", func(ctx context.Context, params json.RawMessage) (any, error) {
    var req services.ChatRequest
    if err := json.Unmarshal(params, &req); err != nil {
        return nil, err
    }
    return p.services.Chat.Chat(ctx, req)
})
```

**Step 4: Commit**

```bash
git add internal/daemon/daemon.go internal/rpc/proxy.go internal/rpc/server.go
git commit -m "feat: wire RPC handlers to service layer"
```

---

## Sprint 8: HTTP Transport Implementation

### Task 9: HTTP Server Structure

**Files:**
- Modify: `internal/comm/http/server.go` (existing, extend)
- Create: `internal/comm/http/handlers.go`

**Step 1: Add service registry to HTTP server**

```go
// In Server struct:
services *services.ServiceRegistry

// Update constructor
func NewServer(cfg ServerConfig, configSvc *ConfigService, daemonCtrl DaemonController, metricsSvc MetricsService, svcRegistry *services.ServiceRegistry, logger *slog.Logger) *Server {
    return &Server{
        config:         cfg,
        configService:  configSvc,
        daemonCtrl:     daemonCtrl,
        metricsService: metricsSvc,
        services:       svcRegistry,
        logger:         logger,
    }
}
```

**Step 2: Add REST routes**

Modify `setupRoutes()`:

```go
// Chat endpoints
mux.HandleFunc("POST /api/v1/chat", s.handleChat)
mux.HandleFunc("GET /api/v1/chat/{id}", s.handleGetChat)

// Memory endpoints
mux.HandleFunc("POST /api/v1/memory/query", s.handleMemoryQuery)
mux.HandleFunc("GET /api/v1/memory/recent", s.handleMemoryRecent)
mux.HandleFunc("POST /api/v1/memory/export", s.handleMemoryExport)

// Task endpoints
mux.HandleFunc("POST /api/v1/tasks", s.handleTaskCreate)
mux.HandleFunc("GET /api/v1/tasks", s.handleTaskList)
mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleTaskGet)
mux.HandleFunc("PUT /api/v1/tasks/{id}", s.handleTaskUpdate)
mux.HandleFunc("DELETE /api/v1/tasks/{id}", s.handleTaskDelete)
mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.handleTaskCancel)
mux.HandleFunc("GET /api/v1/tasks/{id}/steps", s.handleTaskSteps)

// Queue endpoints
mux.HandleFunc("POST /api/v1/queue/jobs", s.handleQueueEnqueue)
mux.HandleFunc("GET /api/v1/queue/jobs", s.handleQueueList)
mux.HandleFunc("GET /api/v1/queue/jobs/{id}", s.handleQueueGet)
mux.HandleFunc("POST /api/v1/queue/jobs/{id}/claim", s.handleQueueClaim)
mux.HandleFunc("POST /api/v1/queue/jobs/{id}/complete", s.handleQueueComplete)
mux.HandleFunc("POST /api/v1/queue/jobs/{id}/fail", s.handleQueueFail)
mux.HandleFunc("POST /api/v1/queue/jobs/{id}/retry", s.handleQueueRetry)
mux.HandleFunc("GET /api/v1/queue/stats", s.handleQueueStats)

// Session endpoints
mux.HandleFunc("POST /api/v1/sessions", s.handleSessionCreate)
mux.HandleFunc("GET /api/v1/sessions", s.handleSessionList)
mux.HandleFunc("GET /api/v1/sessions/{id}", s.handleSessionGet)
mux.HandleFunc("POST /api/v1/sessions/{id}/attach", s.handleSessionAttach)
mux.HandleFunc("POST /api/v1/sessions/{id}/detach", s.handleSessionDetach)

// Worker endpoints
mux.HandleFunc("GET /api/v1/workers", s.handleWorkerList)
mux.HandleFunc("POST /api/v1/workers", s.handleWorkerAdd)
mux.HandleFunc("DELETE /api/v1/workers/{id}", s.handleWorkerRemove)
mux.HandleFunc("GET /api/v1/workers/stats", s.handleWorkerStats)

// Skills endpoints
mux.HandleFunc("GET /api/v1/skills", s.handleSkillsList)
mux.HandleFunc("GET /api/v1/skills/{name}", s.handleSkillsGet)
mux.HandleFunc("POST /api/v1/skills/{name}/execute", s.handleSkillsExecute)

// Self-improve endpoints
mux.HandleFunc("POST /api/v1/selfimprove/detect", s.handleSelfImproveDetect)
mux.HandleFunc("POST /api/v1/selfimprove/analyze", s.handleSelfImproveAnalyze)
mux.HandleFunc("POST /api/v1/selfimprove/generate", s.handleSelfImproveGenerate)
mux.HandleFunc("POST /api/v1/selfimprove/validate", s.handleSelfImproveValidate)
mux.HandleFunc("POST /api/v1/selfimprove/apply", s.handleSelfImproveApply)
mux.HandleFunc("POST /api/v1/selfimprove/reject", s.handleSelfImproveReject)
mux.HandleFunc("GET /api/v1/selfimprove/status", s.handleSelfImproveStatus)
mux.HandleFunc("POST /api/v1/selfimprove/cycle", s.handleSelfImproveCycle)

// Scheduler endpoints
mux.HandleFunc("GET /api/v1/scheduler/jobs", s.handleSchedulerList)
mux.HandleFunc("POST /api/v1/scheduler/jobs", s.handleSchedulerAdd)

// Bus endpoints (for TUI streaming)
mux.HandleFunc("POST /api/v1/bus/subscribe", s.handleBusSubscribe)
mux.HandleFunc("POST /api/v1/bus/poll", s.handleBusPoll)
mux.HandleFunc("DELETE /api/v1/bus/subscribe/{id}", s.handleBusUnsubscribe)
```

**Step 3: Commit**

```bash
git add internal/comm/http/server.go
git commit -m "feat(http): add REST routes for all services"
```

---

### Task 10: HTTP Chat Handler

**Files:**
- Create: `internal/comm/http/chat_handler.go`

**Step 1: Write handler**

```go
package http

import (
	"encoding/json"
	"net/http"

	"github.com/caimlas/meept/internal/services"
)

// handleChat handles POST /api/v1/chat.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req services.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := s.services.Chat.Chat(r.Context(), req)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}
```

**Step 2: Commit**

```bash
git add internal/comm/http/chat_handler.go
git commit -m "feat(http): implement chat handler"
```

---

### Task 11-20: HTTP Handlers for Each Service (Parallelizable)

Each subagent can implement handlers for 1-2 services.

**Pattern for each:**
```go
func (s *Server) handleXxx(w http.ResponseWriter, r *http.Request) {
    var req services.XxxRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        s.writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    resp, err := s.services.Xxx.DoThing(r.Context(), req)
    if err != nil {
        s.handleServiceError(w, err)
        return
    }

    s.writeJSON(w, http.StatusOK, resp)
}
```

**Error handling helper:**
```go
func (s *Server) handleServiceError(w http.ResponseWriter, err error) {
    switch {
    case errors.Is(err, services.ErrNotFound):
        s.writeError(w, http.StatusNotFound, err.Error())
    case errors.Is(err, services.ErrInvalidInput):
        s.writeError(w, http.StatusBadRequest, err.Error())
    case errors.Is(err, services.ErrUnauthorized):
        s.writeError(w, http.StatusUnauthorized, err.Error())
    default:
        s.writeError(w, http.StatusInternalServerError, err.Error())
    }
}
```

---

## Sprint 9: Authentication & Security

### Task 21: Add Authentication Middleware

**Files:**
- Create: `internal/comm/http/auth.go`
- Modify: `internal/comm/http/server.go`

**Step 1: API key authentication**

```go
package http

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

type contextKey string
const apiKeyKey contextKey = "api_key"

// APIKeyAuth middleware validates API key from Authorization header.
type APIKeyAuth struct {
	validKeys map[string]bool
}

func NewAPIKeyAuth(keys []string) *APIKeyAuth {
	validKeys := make(map[string]bool)
	for _, key := range keys {
		validKeys[key] = true
	}
	return &APIKeyAuth{validKeys: validKeys}
}

func (a *APIKeyAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}

		// Support "Bearer <key>" or just "<key>"
		key := strings.TrimPrefix(auth, "Bearer ")

		if !a.validKeys[key] {
			// Constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(key), []byte("invalid")) == 0 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		ctx := context.WithValue(r.Context(), apiKeyKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

**Step 2: Wire middleware**

```go
// In setupRoutes or middleware():
mux = a.AuthMiddleware(mux)
```

---

## Sprint 10: Documentation & Testing

### Task 22: Generate OpenAPI Spec

**Files:**
- Create: `docs/reference/http-api/openapi.yaml`

**Step 1: Write OpenAPI 3.0 spec**

Document all endpoints with:
- Path, method, summary
- Request body schema
- Response schemas (200, 400, 401, 404, 500)
- Example requests/responses

---

### Task 23: Integration Tests

**Files:**
- Create: `internal/comm/http/server_test.go` (extend)
- Create: `tests/http_api_test.go`

**Step 1: End-to-end test**

```go
func TestHTTP_ChatEndpoint(t *testing.T) {
    // Start server with test services
    // POST /api/v1/chat
    // Verify response
}
```

---

### Task 24: Update Documentation

**Files:**
- Modify: `docs/concepts/multi-agent.md`
- Create: `docs/reference/http-api.md`

Update CLAUDE.md with new architecture.

---

## Summary: Execution Order

```
Sprint 1: Foundation
  Task 1: Service registry skeleton
  Task 2: Error types

Sprint 2: Chat
  Task 3: ChatService

Sprint 3: Memory
  Task 4: MemoryService

Sprint 4: Task
  Task 5: TaskService

Sprint 5: Queue
  Task 6: QueueService

Sprint 6: Remaining Services (PARALLEL - 9 subagents)
  Task 7a-i: Session, Worker, Skills, SelfImprove, Cache, Security, Scheduler, Bus, Pipeline

Sprint 7: Wire RPC
  Task 8: Update RPC to call services

Sprint 8: HTTP Transport
  Task 9: HTTP routes
  Task 10: Chat handler

Sprint 8b: HTTP Handlers (PARALLEL - 5 subagents)
  Task 11-20: Handlers for each service group

Sprint 9: Security
  Task 21: Auth middleware

Sprint 10: Docs & Tests
  Task 22: OpenAPI spec
  Task 23: Integration tests
  Task 24: Documentation
```

---

**Plan complete.** Two execution options:

1. **Subagent-Driven (this session)** — I dispatch fresh subagent per task/sprint, review between tasks
2. **Parallel Session** — You open new session with `superpowers:executing-plans` to batch-execute

Which approach?
