# HTTP API for Web Clients - Complete Implementation Plan

> **For Claude:** Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** Expose full meept daemon functionality over HTTP for web clients with feature parity to the CLI, while preserving RPC transport for CLI/TUI.

**Architecture:** Service layer shared between RPC and HTTP transports eliminates drift. Both call identical service functions.

**Tech Stack:** Go 1.24, `net/http`, JSON-RPC 2.0 (RPC), REST+JSON (HTTP), SSE for streaming, SQLite, message bus.

**Excluded (per requirements):** Q Agent, Shadow Training - these are advanced/meta features not needed for core web UI.

---

## Feature Priority Tiers

| Tier | Purpose | Services |
|------|---------|----------|
| **P0 (MVP)** | Core chat functionality | Chat, Session, Memory (query) |
| **P1 (Core UX)** | Task orchestration, real-time updates | Task, Queue, Worker, Skills, Templates, Branch/Session navigation |
| **P2 (Admin)** | Configuration, monitoring | Daemon, Models, Cache, Scheduler, Calendar |
| **P3 (Nice-to-have)** | Advanced features | (Shadow, Q Agent - excluded) |

---

## Service Layer Structure

```
internal/services/
  # Core (P0)
  service.go              -- ServiceRegistry, common types, errors
  chat_service.go         -- Chat, conversation management
  session_service.go      -- Sessions, attach/detach, message history
  memory_service.go       -- Query, recent, export

  # Task Orchestration (P1)
  task_service.go         -- CRUD, linking, steps
  queue_service.go        -- Enqueue, claim, complete, fail, retry, stats
  worker_service.go       -- List, scale, capabilities

  # Skills & Templates (P1)
  skills_service.go       -- List, get, execute
  templates_service.go    -- List, get, invoke, clear

  # Session Navigation (P1)
  branch_service.go       -- List, navigate, fork, tree

  # Admin & Config (P2)
  daemon_service.go       -- Start, stop, restart, status
  model_service.go        -- List providers/models, add, remove, set-default, credentials
  cache_service.go        -- Stats, clear, invalidate, inspect
  scheduler_service.go    -- List jobs, add/remove jobs

  # Integrations (P2)
  calendar_service.go     -- OAuth, events

  # Shared
  bus_service.go          -- SSE streaming, subscribe, poll
```

---

## Sprint 0: Foundation

### Task 0.1: Service Registry Skeleton

**Files:** `internal/services/service.go`, `internal/services/errors.go`

**Purpose:** Central registry holding all service instances with shared error types.

```go
// Package services provides the core business logic for meept operations.
package services

import (
    "context"
    "log/slog"
)

// ServiceRegistry holds all service instances.
type ServiceRegistry struct {
    // P0 - Core
    Chat    *ChatService
    Session *SessionService
    Memory  *MemoryService

    // P1 - Task Orchestration
    Task     *TaskService
    Queue    *QueueService
    Worker   *WorkerService
    Skills   *SkillsService
    Templates *TemplatesService
    Branch   *BranchService

    // P2 - Admin
    Daemon    *DaemonService
    Model     *ModelService
    Cache     *CacheService
    Scheduler *SchedulerService

    // Shared
    Bus *BusService
}

// NewRegistry creates all services with their dependencies.
func NewRegistry(cfg Config, logger *slog.Logger) (*ServiceRegistry, error) {
    if logger == nil {
        logger = slog.Default()
    }
    // Initialize services in dependency order
    return &ServiceRegistry{}, nil
}
```

```go
// Standard service errors for consistent cross-transport handling.
var (
    ErrNotFound      = errors.New("not found")
    ErrAlreadyExists = errors.New("already exists")
    ErrInvalidInput  = errors.New("invalid input")
    ErrUnauthorized  = errors.New("unauthorized")
    ErrInternal      = errors.New("internal error")
    ErrTimeout       = errors.New("operation timed out")
)

// ServiceError wraps errors with service context.
type ServiceError struct {
    Service string
    Op      string
    Err     error
}
```

**Acceptance:**
- [x] `go build ./internal/services/...` succeeds
- [x] Unit tests pass for error types

---

### Task 0.2: Common Types and Pagination

**Files:** `internal/services/types.go`

```go
// Pagination params for list operations.
type ListOptions struct {
    Limit  int    `json:"limit,omitempty"`
    Offset int    `json:"offset,omitempty"`
    Filter string `json:"filter,omitempty"`
}

// Paginated response wrapper.
type PaginatedResponse[T any] struct {
    Items      []T `json:"items"`
    Total      int `json:"total"`
    HasMore    bool `json:"has_more"`
    NextOffset int `json:"next_offset,omitempty"`
}
```

---

## Sprint 1: P0 - Core Chat Functionality

### Task 1.1: ChatService

**Files:** `internal/services/chat_service.go`, `chat_service_test.go`

**Methods:**
```go
type ChatService struct {
    bus    *bus.MessageBus
    logger *slog.Logger
}

type ChatRequest struct {
    Message        string `json:"message"`
    ConversationID string `json:"conversation_id"`
    Model          string `json:"model,omitempty"`
}

type ChatResponse struct {
    Reply      string `json:"reply"`
    Model      string `json:"model,omitempty"`
    TokensUsed int    `json:"tokens_used,omitempty"`
    DurationMs int64  `json:"duration_ms,omitempty"`
}

func (s *ChatService) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
```

**Key Behavior:**
- Publishes to `chat.request` topic on message bus
- Waits for agent response with timeout (2 min default)
- Returns structured response with token usage

---

### Task 1.2: SessionService

**Files:** `internal/services/session_service.go`

**Methods:**
```go
type SessionService struct {
    store  SessionStore
    bus    *bus.MessageBus
}

func (s *SessionService) Create(ctx context.Context, name string) (*Session, error)
func (s *SessionService) Get(ctx context.Context, id string) (*Session, error)
func (s *SessionService) List(ctx context.Context, opts ListOptions) (*PaginatedResponse[Session], error)
func (s *SessionService) GetMostRecent(ctx context.Context) (*Session, error)
func (s *SessionService) GetMessages(ctx context.Context, id string, opts ListOptions) ([]Message, error)
func (s *SessionService) AttachTask(ctx context.Context, sessionID, taskID string) error
func (s *SessionService) DetachTask(ctx context.Context, sessionID, taskID string) error
func (s *SessionService) Describe(ctx context.Context, id string) (*SessionDetails, error)
```

---

### Task 1.3: MemoryService

**Files:** `internal/services/memory_service.go`

**Methods:**
```go
type MemoryService struct {
    manager *memory.Manager
}

type MemoryQueryRequest struct {
    Query    string `json:"query"`
    Limit    int    `json:"limit,omitempty"`
    Category string `json:"category,omitempty"`
}

type MemoryResult struct {
    Memories []*memory.Memory `json:"memories"`
    Count    int              `json:"count"`
}

func (s *MemoryService) Query(ctx context.Context, req MemoryQueryRequest) (*MemoryResult, error)
func (s *MemoryService) Recent(ctx context.Context, limit int) (*MemoryResult, error)
func (s *MemoryService) Export(ctx context.Context, format, category string) ([]byte, error)
```

---

## Sprint 2: P1 - Task Orchestration

### Task 2.1: TaskService

**Files:** `internal/services/task_service.go`

**Methods:**
```go
type TaskService struct {
    store TaskStore
}

func (s *TaskService) Create(ctx context.Context, name, description string) (*Task, error)
func (s *TaskService) Get(ctx context.Context, id string) (*Task, error)
func (s *TaskService) List(ctx context.Context, filter TaskFilter) (*PaginatedResponse[Task], error)
func (s *TaskService) Update(ctx context.Context, task *Task) error
func (s *TaskService) Delete(ctx context.Context, id string) error
func (s *TaskService) Cancel(ctx context.Context, id string) error
func (s *TaskService) LinkSession(ctx context.Context, taskID, sessionID string) error
func (s *TaskService) UnlinkSession(ctx context.Context, taskID, sessionID string) error
func (s *TaskService) GetSteps(ctx context.Context, id string) ([]TaskStep, error)
```

---

### Task 2.2: QueueService

**Files:** `internal/services/queue_service.go`

**Methods:**
```go
type QueueService struct {
    queue queue.Queue
}

func (s *QueueService) Enqueue(ctx context.Context, job *queue.Job) error
func (s *QueueService) Get(ctx context.Context, jobID string) (*queue.Job, error)
func (s *QueueService) List(ctx context.Context, state queue.JobState, limit int) ([]*queue.Job, error)
func (s *QueueService) Retry(ctx context.Context, jobID string) error
func (s *QueueService) Stats(ctx context.Context) (*queue.QueueStats, error)
```

---

### Task 2.3: WorkerService

**Files:** `internal/services/worker_service.go`

**Methods:**
```go
type WorkerService struct {
    pool *worker.Pool
}

func (s *WorkerService) List(ctx context.Context) ([]WorkerInfo, error)
func (s *WorkerService) Stats(ctx context.Context) (*WorkerPoolStats, error)
func (s *WorkerService) Scale(ctx context.Context, count int) error
```

---

## Sprint 3: P1 - Skills, Templates, Branches

### Task 3.1: SkillsService

**Files:** `internal/services/skills_service.go`

**Methods:**
```go
type SkillsService struct {
    registry *skills.Registry
    executor *skills.Executor
}

type SkillInfo struct {
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Tags        []string `json:"tags"`
    RiskLevel   string   `json:"risk_level"`
}

func (s *SkillsService) List(ctx context.Context, filterTag string) ([]SkillInfo, error)
func (s *SkillsService) Get(ctx context.Context, name string) (*SkillDetails, error)
func (s *SkillsService) Execute(ctx context.Context, name, input string) (*SkillResult, error)
```

---

### Task 3.2: TemplatesService

**Files:** `internal/services/templates_service.go`

**Methods:**
```go
type TemplatesService struct {
    registry *templates.Registry
}

func (s *TemplatesService) List(ctx context.Context) ([]TemplateInfo, error)
func (s *TemplatesService) Get(ctx context.Context, name string) (*TemplateDetails, error)
func (s *TemplatesService) Invoke(ctx context.Context, name string, args []string) (*TemplateResult, error)
func (s *TemplatesService) Clear(ctx context.Context, conversationID, name string) ([]string, error)
```

---

### Task 3.3: BranchService

**Files:** `internal/services/branch_service.go`

**Methods:**
```go
type BranchService struct {
    store SessionStore
}

type BranchInfo struct {
    ID           string `json:"id"`
    MessageCount int    `json:"message_count"`
    Summary      string `json:"summary,omitempty"`
    IsCurrent    bool   `json:"is_current"`
}

func (s *BranchService) List(ctx context.Context, sessionID string) ([]BranchInfo, error)
func (s *BranchService) Summary(ctx context.Context, sessionID string) ([]BranchInfo, error)
func (s *BranchService) Navigate(ctx context.Context, sessionID string, messageID int64) (*NavigateResult, error)
func (s *BranchService) Fork(ctx context.Context, sessionID string, messageID int64, name string) (*ForkResult, error)
func (s *BranchService) GetTree(ctx context.Context, sessionID string) (*ConversationTree, error)
```

---

## Sprint 4: P2 - Admin & Configuration

### Task 4.1: DaemonService

**Files:** `internal/services/daemon_service.go`

**Methods:**
```go
type DaemonService struct {
    pidFile string
    binPath string
    stateDir string
}

type DaemonStatus struct {
    Status      string  `json:"status"`
    PID         int     `json:"pid,omitempty"`
    UptimeSecs  float64 `json:"uptime_seconds,omitempty"`
    Model       string  `json:"model,omitempty"`
    TokensUsed  int     `json:"tokens_used"`
    TokensRemaining int `json:"tokens_remaining"`
    BudgetUsed  float64 `json:"budget_used"`
    Methods     int     `json:"registered_methods"`
}

func (s *DaemonService) Status(ctx context.Context) (*DaemonStatus, error)
func (s *DaemonService) Start(ctx context.Context) error
func (s *DaemonService) Stop(ctx context.Context) error
func (s *DaemonService) Restart(ctx context.Context) error
```

---

### Task 4.2: ModelService

**Files:** `internal/services/model_service.go`

**Methods:**
```go
type ModelService struct {
    configPath string
}

type ModelInfo struct {
    Provider     string   `json:"provider"`
    Model        string   `json:"model"`
    FullName     string   `json:"full_name"`
    BaseURL      string   `json:"base_url"`
    ContextLimit int      `json:"context_limit"`
    MaxOutput    int      `json:"max_output"`
    Capabilities []string `json:"capabilities"`
    IsDefault    bool     `json:"is_default"`
}

func (s *ModelService) List(ctx context.Context) ([]ModelInfo, error)
func (s *ModelService) Providers(ctx context.Context) ([]ProviderInfo, error)
func (s *ModelService) GetDefault(ctx context.Context) (*ModelInfo, error)
func (s *ModelService) SetDefault(ctx context.Context, provider, model string) error
func (s *ModelService) Remove(ctx context.Context, provider, model string) error
func (s *ModelService) GetCredential(ctx context.Context, providerID string) (string, error)
func (s *ModelService) SetCredential(ctx context.Context, providerID, credential string) error
func (s *ModelService) DeleteCredential(ctx context.Context, providerID string) error
```

---

### Task 4.3: CacheService

**Files:** `internal/services/cache_service.go`

**Methods:**
```go
type CacheService struct {
    cache *llm.TokenCache
}

type CacheStats struct {
    L1Entries   int     `json:"l1_entries"`
    L1Hits      int     `json:"l1_hits"`
    L1Misses    int     `json:"l1_misses"`
    L2Entries   int     `json:"l2_entries"`
    L2Hits      int     `json:"l2_hits"`
    L2Misses    int     `json:"l2_misses"`
    TotalHits   int     `json:"total_hits"`
    HitRate     float64 `json:"hit_rate"`
}

type CacheEntry struct {
    ModelID   string            `json:"model_id"`
    Source    string            `json:"source"`
    CreatedAt string            `json:"created_at"`
    ExpiresAt string            `json:"expires_at"`
    HitCount  int               `json:"hit_count"`
    Response  string            `json:"response,omitempty"`
    FileHashes map[string]string `json:"file_hashes,omitempty"`
}

func (s *CacheService) Stats(ctx context.Context) (*CacheStats, error)
func (s *CacheService) Clear(ctx context.Context) error
func (s *CacheService) Invalidate(ctx context.Context, filePath string) error
func (s *CacheService) Inspect(ctx context.Context, promptHash string) ([]CacheEntry, error)
```

---

### Task 4.4: SchedulerService

**Files:** `internal/services/scheduler_service.go`

**Methods:**
```go
type SchedulerService struct {
    scheduler *scheduler.Scheduler
}

type ScheduledJob struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Schedule    string `json:"schedule"`
    NextRunTime string `json:"next_run_time"`
    Paused      bool   `json:"paused"`
}

func (s *SchedulerService) List(ctx context.Context) ([]ScheduledJob, error)
func (s *SchedulerService) Add(ctx context.Context, name, schedule, command string) error
func (s *SchedulerService) Remove(ctx context.Context, id string) error
func (s *SchedulerService) Pause(ctx context.Context, id string) error
func (s *SchedulerService) Resume(ctx context.Context, id string) error
```

---

## Sprint 5: P2 - Calendar Integration

### Task 5.1: CalendarService

**Files:** `internal/services/calendar_service.go`

**Methods:**
```go
type CalendarService struct {
    client *calendar.Client
}

type CalendarEvent struct {
    ID        string `json:"id"`
    Summary   string `json:"summary"`
    Start     string `json:"start"`
    End       string `json:"end"`
    Location  string `json:"location,omitempty"`
}

func (s *CalendarService) AuthURL(ctx context.Context) (string, error)
func (s *CalendarService) ExchangeCode(ctx context.Context, code string) error
func (s *CalendarService) GetToday(ctx context.Context) ([]CalendarEvent, error)
```

---

## Sprint 6: HTTP Transport Implementation

### Task 6.1: HTTP Server with SSE Support

**Files:** `internal/comm/http/server.go`

**Structure:**
```go
type Server struct {
    services *services.ServiceRegistry
    logger   *slog.Logger
}

func NewServer(cfg ServerConfig, services *services.ServiceRegistry, logger *slog.Logger) *Server
func (s *Server) Serve(addr string) error
```

**SSE Streaming Endpoint:**
```go
// handleSSE streams bus events via Server-Sent Events.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("Access-Control-Allow-Origin", "*")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "SSE not supported", http.StatusInternalServerError)
        return
    }

    // Subscribe to bus topic
    sub := s.services.Bus.Subscribe(r.Context(), "chat.response")
    defer s.services.Bus.Unsubscribe(sub)

    for {
        select {
        case msg := <-sub.Channel:
            fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
            flusher.Flush()
        case <-r.Context().Done():
            return
        }
    }
}
```

---

### Task 6.2: HTTP Routes

**File:** `internal/comm/http/routes.go`

```go
func (s *Server) setupRoutes(mux *http.ServeMux) {
    // P0 - Core Chat
    mux.HandleFunc("POST /api/v1/chat", s.handleChat)
    mux.HandleFunc("POST /api/v1/sessions", s.handleSessionCreate)
    mux.HandleFunc("GET /api/v1/sessions", s.handleSessionList)
    mux.HandleFunc("GET /api/v1/sessions/{id}", s.handleSessionGet)
    mux.HandleFunc("GET /api/v1/sessions/{id}/messages", s.handleSessionMessages)
    mux.HandleFunc("GET /api/v1/sessions/most-recent", s.handleSessionMostRecent)
    mux.HandleFunc("GET /api/v1/sse", s.handleSSE)

    // P0 - Memory
    mux.HandleFunc("POST /api/v1/memory/query", s.handleMemoryQuery)
    mux.HandleFunc("GET /api/v1/memory/recent", s.handleMemoryRecent)
    mux.HandleFunc("POST /api/v1/memory/export", s.handleMemoryExport)

    // P1 - Tasks
    mux.HandleFunc("POST /api/v1/tasks", s.handleTaskCreate)
    mux.HandleFunc("GET /api/v1/tasks", s.handleTaskList)
    mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleTaskGet)
    mux.HandleFunc("PUT /api/v1/tasks/{id}", s.handleTaskUpdate)
    mux.HandleFunc("DELETE /api/v1/tasks/{id}", s.handleTaskDelete)
    mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.handleTaskCancel)
    mux.HandleFunc("GET /api/v1/tasks/{id}/steps", s.handleTaskSteps)
    mux.HandleFunc("POST /api/v1/tasks/{id}/link-session", s.handleTaskLinkSession)
    mux.HandleFunc("POST /api/v1/tasks/{id}/unlink-session", s.handleTaskUnlinkSession)

    // P1 - Queue
    mux.HandleFunc("POST /api/v1/queue/jobs", s.handleQueueEnqueue)
    mux.HandleFunc("GET /api/v1/queue/jobs", s.handleQueueList)
    mux.HandleFunc("GET /api/v1/queue/jobs/{id}", s.handleQueueGet)
    mux.HandleFunc("POST /api/v1/queue/jobs/{id}/retry", s.handleQueueRetry)
    mux.HandleFunc("GET /api/v1/queue/stats", s.handleQueueStats)

    // P1 - Workers
    mux.HandleFunc("GET /api/v1/workers", s.handleWorkerList)
    mux.HandleFunc("POST /api/v1/workers/scale", s.handleWorkerScale)
    mux.HandleFunc("GET /api/v1/workers/stats", s.handleWorkerStats)

    // P1 - Skills
    mux.HandleFunc("GET /api/v1/skills", s.handleSkillsList)
    mux.HandleFunc("GET /api/v1/skills/{name}", s.handleSkillsGet)
    mux.HandleFunc("POST /api/v1/skills/{name}/execute", s.handleSkillsExecute)

    // P1 - Templates
    mux.HandleFunc("GET /api/v1/templates", s.handleTemplatesList)
    mux.HandleFunc("GET /api/v1/templates/{name}", s.handleTemplatesGet)
    mux.HandleFunc("POST /api/v1/templates/{name}/invoke", s.handleTemplatesInvoke)
    mux.HandleFunc("DELETE /api/v1/templates/{name}", s.handleTemplatesClear)

    // P1 - Branches
    mux.HandleFunc("GET /api/v1/sessions/{sid}/branches", s.handleBranchList)
    mux.HandleFunc("GET /api/v1/sessions/{sid}/branches/summary", s.handleBranchSummary)
    mux.HandleFunc("POST /api/v1/sessions/{sid}/navigate", s.handleBranchNavigate)
    mux.HandleFunc("POST /api/v1/sessions/{sid}/fork", s.handleBranchFork)
    mux.HandleFunc("GET /api/v1/sessions/{sid}/tree", s.handleBranchTree)

    // P2 - Daemon
    mux.HandleFunc("GET /api/v1/daemon/status", s.handleDaemonStatus)
    mux.HandleFunc("POST /api/v1/daemon/start", s.handleDaemonStart)
    mux.HandleFunc("POST /api/v1/daemon/stop", s.handleDaemonStop)
    mux.HandleFunc("POST /api/v1/daemon/restart", s.handleDaemonRestart)

    // P2 - Models
    mux.HandleFunc("GET /api/v1/models", s.handleModelList)
    mux.HandleFunc("GET /api/v1/models/providers", s.handleModelProviders)
    mux.HandleFunc("POST /api/v1/models/default", s.handleModelSetDefault)
    mux.HandleFunc("DELETE /api/v1/models/{provider}/{model}", s.handleModelRemove)
    mux.HandleFunc("GET /api/v1/models/credentials/{provider}", s.handleModelGetCredential)
    mux.HandleFunc("PUT /api/v1/models/credentials/{provider}", s.handleModelSetCredential)

    // P2 - Cache
    mux.HandleFunc("GET /api/v1/cache/stats", s.handleCacheStats)
    mux.HandleFunc("POST /api/v1/cache/clear", s.handleCacheClear)
    mux.HandleFunc("POST /api/v1/cache/invalidate", s.handleCacheInvalidate)
    mux.HandleFunc("GET /api/v1/cache/inspect", s.handleCacheInspect)

    // P2 - Scheduler
    mux.HandleFunc("GET /api/v1/scheduler/jobs", s.handleSchedulerList)
    mux.HandleFunc("POST /api/v1/scheduler/jobs", s.handleSchedulerAdd)
    mux.HandleFunc("DELETE /api/v1/scheduler/jobs/{id}", s.handleSchedulerRemove)
    mux.HandleFunc("POST /api/v1/scheduler/jobs/{id}/pause", s.handleSchedulerPause)
    mux.HandleFunc("POST /api/v1/scheduler/jobs/{id}/resume", s.handleSchedulerResume)

    // P2 - Calendar
    mux.HandleFunc("GET /api/v1/calendar/auth-url", s.handleCalendarAuthURL)
    mux.HandleFunc("POST /api/v1/calendar/callback", s.handleCalendarCallback)
    mux.HandleFunc("GET /api/v1/calendar/today", s.handleCalendarToday)
}
```

---

### Task 6.3: HTTP Handlers Pattern

**File:** `internal/comm/http/handlers.go`

```go
// Helper for consistent JSON responses.
func (s *Server) writeJSON(w http.ResponseWriter, status int, data any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}

// Helper for error responses.
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
    s.writeJSON(w, status, map[string]string{"error": message})
}

// Helper for service error mapping.
func (s *Server) handleServiceError(w http.ResponseWriter, err error) {
    switch {
    case errors.Is(err, services.ErrNotFound):
        s.writeError(w, http.StatusNotFound, err.Error())
    case errors.Is(err, services.ErrInvalidInput):
        s.writeError(w, http.StatusBadRequest, err.Error())
    case errors.Is(err, services.ErrUnauthorized):
        s.writeError(w, http.StatusUnauthorized, err.Error())
    case errors.Is(err, services.ErrTimeout):
        s.writeError(w, http.StatusRequestTimeout, err.Error())
    default:
        s.writeError(w, http.StatusInternalServerError, err.Error())
    }
}
```

---

### Task 6.4: Authentication Middleware

**File:** `internal/comm/http/auth.go`

```go
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
        key := strings.TrimPrefix(auth, "Bearer ")
        if !a.validKeys[key] {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

---

## Sprint 7: Wiring & Integration

### Task 7.1: Wire RPC to Services

**Files:** Modified `internal/rpc/proxy.go`, `internal/daemon/daemon.go`

- Update daemon to create service registry on startup
- Update RPC handlers to delegate to services instead of direct implementation

---

### Task 7.2: Wire HTTP to Services

**Files:** Modified `internal/comm/http/server.go`

- Inject service registry into HTTP server
- Ensure all handlers call service methods

---

## Sprint 8: Testing & Documentation

### Task 8.1: Integration Tests

**Files:** `tests/http_api_test.go`

```go
func TestHTTP_ChatEndpoint(t *testing.T) {
    // Start test server with mocked services
    // POST /api/v1/chat with message
    // Verify 200 OK with chat response
}

func TestHTTP_TaskCRUD(t *testing.T) {
    // Create task -> GET task -> Update -> Delete
}

func TestHTTP_AuthMiddleware(t *testing.T) {
    // Test unauthenticated request -> 401
    // Test valid API key -> 200
}
```

---

### Task 8.2: OpenAPI Specification

**File:** `docs/reference/http-api/openapi.yaml`

Generate from Go structs using https://github.com/swaggo/swag or similar:

```bash
swag init --dir internal/comm/http,Internal/services
```

---

### Task 8.3: TypeScript Client SDK

**File:** `web-client/src/api/client.ts`

Generate from OpenAPI:

```bash
openapi-typescript docs/reference/http-api/openapi.yaml -o web-client/src/api/types.ts
openapi-typesgen docs/reference/http-api/openapi.yaml -o web-client/src/api/client.ts
```

---

## Execution Order Summary

```
Sprint 0: Foundation
  - Task 0.1: Service registry + errors
  - Task 0.2: Common types (pagination)

Sprint 1: P0 Core Chat
  - Task 1.1: ChatService
  - Task 1.2: SessionService
  - Task 1.3: MemoryService

Sprint 2: P1 Task Orchestration
  - Task 2.1: TaskService
  - Task 2.2: QueueService
  - Task 2.3: WorkerService

Sprint 3: P1 Skills/Templates/Branches
  - Task 3.1: SkillsService
  - Task 3.2: TemplatesService
  - Task 3.3: BranchService

Sprint 4: P2 Admin
  - Task 4.1: DaemonService
  - Task 4.2: ModelService
  - Task 4.3: CacheService
  - Task 4.4: SchedulerService

Sprint 5: P2 Integrations
  - Task 5.1: CalendarService

Sprint 6: HTTP Transport
  - Task 6.1: HTTP Server + SSE
  - Task 6.2: Routes
  - Task 6.3: Handlers
  - Task 6.4: auth middleware

Sprint 7: Wiring
  - Task 7.1: RPC -> Services
  - Task 7.2: HTTP -> Services

Sprint 8: Testing & Docs
  - Task 8.1: Integration tests
  - Task 8.2: OpenAPI spec
  - Task 8.3: TypeScript SDK
```

---

## Client Implementation Checklist

After backend implementation, verify web client can:

### Core (P0)
- [x] Send chat messages and receive streaming responses
- [x] Create/list/get sessions
- [x] View session message history
- [x] Search memories
- [x] Subscribe to SSE events for real-time updates

### Task Management (P1)
- [x] Create tasks with description
- [x] List tasks with filters (state, limit)
- [x] View task details and steps
- [x] Link/unlink sessions to tasks
- [x] Cancel tasks
- [x] View queue status
- [x] Retry failed jobs
- [x] View/scale workers

### Skills & Templates (P1)
- [x] List available skills with tag filter
- [x] View skill details
- [x] Execute skills with input
- [x] List/invoke templates
- [x] Clear session templates

### Session Navigation (P1)
- [x] List branches in a session
- [x] View branch summaries
- [x] Navigate to prior messages (new branch)
- [x] Fork sessions from a message
- [x] View conversation tree

### Admin (P2)
- [x] View daemon status
- [x] Start/stop/restart daemon
- [x] List models and providers
- [x] Set default model
- [x] Manage API credentials
- [x] View cache stats, clear, invalidate
- [x] View/manage scheduled jobs
- [x] Google Calendar auth, view today's events
