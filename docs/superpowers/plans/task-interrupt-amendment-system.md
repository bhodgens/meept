# Task Interrupt & Amendment System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement comprehensive task interrupt, cancellation, and amendment capabilities allowing users to modify, cancel, or inject new direction into running tasks mid-flight.

**Architecture:** Three-layer system:
1. **InterruptToken** - Per-task cancellation context with reason tracking
2. **Amendment Protocol** - Bus-based amendment requests with structured handlers
3. **Conversational Override** - Live context injection into agent loops

**Tech Stack:** Go 1.24+, SQLite, message bus pub/sub, context cancellation

---

## Phase 1: Core Infrastructure (InterruptToken & Cancellation)

### Task 1: InterruptToken Type and Store Integration

**Files:**
- Create: `internal/task/interrupt.go`
- Modify: `internal/task/store.go:54-114` (migration)
- Modify: `internal/task/registry.go` (cancel handler)

```go
// interrupt.go
package task

import (
    "context"
    "sync"
    "time"
)

// InterruptReason indicates why a task was interrupted.
type InterruptReason string

const (
    ReasonUserCancelled  InterruptReason = "user_cancelled"
    ReasonUserAmended    InterruptReason = "user_amended"
    ReasonSuperseded     InterruptReason = "superseded"
    ReasonResourceLimit  InterruptReason = "resource_limit"
    ReasonDependencyFail InterruptReason = "dependency_failed"
)

// InterruptToken represents a cancellable context for a task.
type InterruptToken struct {
    mu        sync.RWMutex
    ctx       context.Context
    cancel    context.CancelFunc
    taskID    string
    triggered bool
    reason    InterruptReason
    message   string
    triggeredAt time.Time
}

// NewInterruptToken creates a new interrupt token.
func NewInterruptToken(taskID string) *InterruptToken {
    ctx, cancel := context.WithCancel(context.Background())
    return &InterruptToken{
        ctx:    ctx,
        cancel: cancel,
        taskID: taskID,
    }
}

// Context returns the underlying context for cancellation checking.
func (t *InterruptToken) Context() context.Context {
    return t.ctx
}

// Trigger cancels the context with a reason.
func (t *InterruptToken) Trigger(reason InterruptReason, message string) {
    t.mu.Lock()
    defer t.mu.Unlock()
    if t.triggered {
        return // Already triggered
    }
    t.triggered = true
    t.reason = reason
    t.message = message
    t.triggeredAt = time.Now().UTC()
    t.cancel()
}

// IsTriggered returns true if the interrupt has been triggered.
func (t *InterruptToken) IsTriggered() bool {
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.triggered
}

// Reason returns the interrupt reason.
func (t *InterruptToken) Reason() InterruptReason {
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.reason
}

// Message returns the interrupt message.
func (t *InterruptToken) Message() string {
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.message
}

// Reset clears the interrupt for task reuse.
func (t *InterruptToken) Reset() {
    t.mu.Lock()
    defer t.mu.Unlock()
    if t.triggered {
        t.ctx, t.cancel = context.WithCancel(context.Background())
        t.triggered = false
        t.reason = ""
        t.message = ""
        t.triggeredAt = time.Time{}
    }
}
```

- [x] **Step 1: Write the failing test**

```go
// interrupt_test.go
package task

import (
    "context"
    "testing"
    "time"
)

func TestInterruptToken_Trigger(t *testing.T) {
    tok := NewInterruptToken("task-1")

    // Should not be triggered initially
    if tok.IsTriggered() {
        t.Fatal("token should not be triggered initially")
    }

    // Trigger cancellation
    tok.Trigger(ReasonUserCancelled, "User changed their mind")

    // Should be triggered now
    if !tok.IsTriggered() {
        t.Fatal("token should be triggered after Trigger()")
    }

    // Reason and message should be set
    if tok.Reason() != ReasonUserCancelled {
        t.Errorf("got reason %v, want %v", tok.Reason(), ReasonUserCancelled)
    }
    if tok.Message() != "User changed their mind" {
        t.Errorf("got message %q, want %q", tok.Message(), "User changed their mind")
    }
}

func TestInterruptToken_ContextCancellation(t *testing.T) {
    tok := NewInterruptToken("task-1")
    ctx := tok.Context()

    // Context should not be done initially
    select {
    case <-ctx.Done():
        t.Fatal("context should not be done initially")
    default:
    }

    // Trigger cancellation
    tok.Trigger(ReasonUserAmended, "New direction provided")

    // Context should be done now
    select {
    case <-ctx.Done():
        // Expected
    case <-time.After(100 * time.Millisecond):
        t.Fatal("context should be cancelled")
    }
}

func TestInterruptToken_DoubleTrigger(t *testing.T) {
    tok := NewInterruptToken("task-1")

    tok.Trigger(ReasonUserCancelled, "First reason")
    tok.Trigger(ReasonSuperseded, "Second reason") // Should be ignored

    if tok.Reason() != ReasonUserCancelled {
        t.Errorf("first trigger should win, got %v", tok.Reason())
    }
    if tok.Message() != "First reason" {
        t.Errorf("first message should win, got %q", tok.Message())
    }
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/task -run TestInterruptToken -v`
Expected: FAIL with "file does not exist"

- [x] **Step 3: Write interrupt.go implementation**

Create the file with the code above.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/task -run TestInterruptToken -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/task/interrupt.go internal/task/interrupt_test.go
git commit -m "feat: add InterruptToken for task cancellation"
```

---

### Task 2: InterruptManager for Task-Level Token Registry

**Files:**
- Create: `internal/task/interrupt_manager.go`
- Create: `internal/task/interrupt_manager_test.go`

```go
// interrupt_manager.go
package task

import (
    "context"
    "log/slog"
    "sync"
)

// InterruptManager manages interrupt tokens for all active tasks.
type InterruptManager struct {
    mu      sync.RWMutex
    tokens  map[string]*InterruptToken
    logger  *slog.Logger
}

// NewInterruptManager creates a new interrupt manager.
func NewInterruptManager(logger *slog.Logger) *InterruptManager {
    if logger == nil {
        logger = slog.Default()
    }
    return &InterruptManager{
        tokens: make(map[string]*InterruptToken),
        logger: logger,
    }
}

// GetOrCreate returns an existing token or creates a new one.
func (m *InterruptManager) GetOrCreate(taskID string) *InterruptToken {
    m.mu.Lock()
    defer m.mu.Unlock()

    if tok, ok := m.tokens[taskID]; ok {
        return tok
    }

    tok := NewInterruptToken(taskID)
    m.tokens[taskID] = tok
    m.logger.Debug("Created interrupt token", "task_id", taskID)
    return tok
}

// Get returns a token if it exists.
func (m *InterruptManager) Get(taskID string) (*InterruptToken, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    tok, ok := m.tokens[taskID]
    return tok, ok
}

// Trigger triggers a task's interrupt token.
func (m *InterruptManager) Trigger(taskID string, reason InterruptReason, message string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    tok, ok := m.tokens[taskID]
    if !ok {
        // Create token and trigger immediately
        tok = NewInterruptToken(taskID)
        m.tokens[taskID] = tok
    }

    tok.Trigger(reason, message)
    m.logger.Info("Task interrupted",
        "task_id", taskID,
        "reason", reason,
        "message", message,
    )
    return nil
}

// Remove removes a token (called when task completes).
func (m *InterruptManager) Remove(taskID string) {
    m.mu.Lock()
    defer m.mu.Unlock()

    delete(m.tokens, taskID)
    m.logger.Debug("Removed interrupt token", "task_id", taskID)
}

// ListActive returns all active task IDs with interrupt tokens.
func (m *InterruptManager) ListActive() []string {
    m.mu.RLock()
    defer m.mu.RUnlock()

    ids := make([]string, 0, len(m.tokens))
    for id := range m.tokens {
        ids = append(ids, id)
    }
    return ids
}

// Close shuts down the manager.
func (m *InterruptManager) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    for _, tok := range m.tokens {
        tok.Trigger(ReasonResourceLimit, "InterruptManager closed")
    }
    m.tokens = make(map[string]*InterruptToken)
    return nil
}
```

- [x] **Step 1: Write the failing test**

```go
// interrupt_manager_test.go
package task

import (
    "log/slog"
    "os"
    "testing"
)

func TestInterruptManager_GetOrCreate(t *testing.T) {
    logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
    mgr := NewInterruptManager(logger)

    // GetOrCreate should create new token
    tok1 := mgr.GetOrCreate("task-1")
    if tok1 == nil {
        t.Fatal("GetOrCreate should return non-nil token")
    }

    // Second call should return same token
    tok2 := mgr.GetOrCreate("task-1")
    if tok1 != tok2 {
        t.Fatal("GetOrCreate should return same token")
    }

    // Different task should get different token
    tok3 := mgr.GetOrCreate("task-2")
    if tok1 == tok3 {
        t.Fatal("different tasks should have different tokens")
    }
}

func TestInterruptManager_Trigger(t *testing.T) {
    logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
    mgr := NewInterruptManager(logger)

    // Trigger non-existent task (should create and trigger)
    err := mgr.Trigger("task-1", ReasonUserCancelled, "test")
    if err != nil {
        t.Fatalf("Trigger failed: %v", err)
    }

    tok, ok := mgr.Get("task-1")
    if !ok {
        t.Fatal("token should exist after Trigger")
    }
    if !tok.IsTriggered() {
        t.Fatal("token should be triggered")
    }
}

func TestInterruptManager_Remove(t *testing.T) {
    logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
    mgr := NewInterruptManager(logger)

    mgr.GetOrCreate("task-1")
    mgr.Remove("task-1")

    _, ok := mgr.Get("task-1")
    if ok {
        t.Fatal("token should be removed")
    }
}

func TestInterruptManager_ListActive(t *testing.T) {
    logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
    mgr := NewInterruptManager(logger)

    mgr.GetOrCreate("task-1")
    mgr.GetOrCreate("task-2")

    ids := mgr.ListActive()
    if len(ids) != 2 {
        t.Fatalf("expected 2 active IDs, got %d", len(ids))
    }
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/task -run TestInterruptManager -v`
Expected: FAIL with "file does not exist"

- [x] **Step 3: Write interrupt_manager.go implementation**

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/task -run TestInterruptManager -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/task/interrupt_manager.go internal/task/interrupt_manager_test.go
git commit -m "feat: add InterruptManager for tracking task tokens"
```

---

### Task 3: Wire InterruptManager into Task Registry

**Files:**
- Modify: `internal/task/registry.go:15-23`, `26-44`, `621-657`

- [x] **Step 1: Add InterruptManager to Registry struct**

```go
// registry.go (line 15-23)
type Registry struct {
    store           *Store
    bus             *bus.MessageBus
    logger          *slog.Logger
    interruptMgr    *InterruptManager  // ADD THIS
    mu              sync.RWMutex
    closed          bool
}
```

- [x] **Step 2: Initialize InterruptManager in NewRegistry**

```go
// registry.go (NewRegistry function)
func NewRegistry(dbPath string, msgBus *bus.MessageBus, logger *slog.Logger) (*Registry, error) {
    // ... existing code ...

    reg := &Registry{
        store:        store,
        bus:          msgBus,
        logger:       logger,
        interruptMgr: NewInterruptManager(logger.With("component", "interrupt-mgr")),  // ADD THIS
    }

    // ... rest ...
}
```

- [x] **Step 3: Update handleCancel to use InterruptManager**

```go
// registry.go (handleCancel function - line 621+)
func (h *Handler) handleCancel(ctx context.Context, msg *models.BusMessage) (any, error) {
    var params struct {
        ID      string `json:"id"`
        Reason  string `json:"reason,omitempty"`
        Message string `json:"message,omitempty"`
    }
    if err := json.Unmarshal(msg.Payload, &params); err != nil {
        return nil, err
    }
    if params.ID == "" {
        return nil, fmt.Errorf("task id is required")
    }

    // Trigger interrupt token
    reason := InterruptReason(params.Reason)
    if reason == "" {
        reason = ReasonUserCancelled
    }
    msgText := params.Message
    if msgText == "" {
        msgText = "Cancelled by user"
    }

    //触发 interrupt
    if err := h.registry.interruptMgr.Trigger(params.ID, reason, msgText); err != nil {
        return nil, err
    }

    // Update task state
    task, err := h.registry.Get(ctx, params.ID)
    if err != nil {
        return nil, err
    }
    if task == nil {
        return nil, fmt.Errorf("task not found: %s", params.ID)
    }

    if task.State.IsTerminal() {
        return map[string]any{
            "status":  "noop",
            "state":   string(task.State),
            "message": "task already in terminal state",
        }, nil
    }

    task.SetState(StateCancelled)
    if err := h.registry.Update(ctx, task); err != nil {
        return nil, err
    }

    // Publish cancellation event
    h.registry.publishEvent("task.cancelled", map[string]any{
        "task_id": params.ID,
        "reason":  reason,
        "message": msgText,
    })

    return map[string]any{
        "status": "cancelled",
        "state":  string(task.State),
        "reason": reason,
    }, nil
}
```

- [x] **Step 4: Add InterruptManager getter**

```go
// Add after StepStore() method
func (r *Registry) InterruptManager() *InterruptManager {
    return r.interruptMgr
}
```

- [x] **Step 5: Run tests**

Run: `go test ./internal/task/... -v`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/task/registry.go
git commit -m "feat: wire InterruptManager into task cancel handler"
```

---

## Phase 2: Amendment Protocol (Bus-Based)

### Task 4: Amendment Types and Bus Topics

**Files:**
- Create: `internal/task/amendment.go`

```go
// amendment.go
package task

import (
    "encoding/json"
    "time"
)

// AmendmentType represents the type of amendment.
type AmendmentType string

const (
    AmendmentInjectContext AmendmentType = "inject_context"  // Add context/message to agent
    AmendmentReprioritize  AmendmentType = "reprioritize"    // Change step priorities
    AmendmentSkipStep      AmendmentType = "skip_step"       // Skip a step
    AmendmentAddStep       AmendmentType = "add_step"        // Insert new step
    AmendmentChangeAgent   AmendmentType = "change_agent"    // Reassign step to different agent
)

// AmendmentRequest represents a user's amendment request.
type AmendmentRequest struct {
    ID          string          `json:"id"`
    TaskID      string          `json:"task_id"`
    Type        AmendmentType   `json:"type"`
    StepID      string          `json:"step_id,omitempty"`       // For step-specific amendments
    Content     string          `json:"content"`                 // The amendment content
    Metadata    json.RawMessage `json:"metadata,omitempty"`      // Type-specific metadata
    Status      AmendmentStatus `json:"status"`
    CreatedAt   time.Time       `json:"created_at"`
    ProcessedAt time.Time       `json:"processed_at,omitempty"`
}

// AmendmentStatus represents the status of an amendment.
type AmendmentStatus string

const (
    AmendmentPending   AmendmentStatus = "pending"
    AmendmentApplied   AmendmentStatus = "applied"
    AmendmentRejected  AmendmentStatus = "rejected"
    AmendmentIgnored   AmendmentStatus = "ignored"  // For amendments no longer relevant
)

// AmendmentReply is the response to an amendment request.
type AmendmentReply struct {
    RequestID  string          `json:"request_id"`
    Success    bool            `json:"success"`
    Message    string          `json:"message,omitempty"`
    Metadata   json.RawMessage `json:"metadata,omitempty"`
}

// NewAmendmentRequest creates a new amendment request.
func NewAmendmentRequest(taskID string, typ AmendmentType, content string) *AmendmentRequest {
    return &AmendmentRequest{
        ID:        fmt.Sprintf("amend-%s-%d", taskID, time.Now().UnixNano()),
        TaskID:    taskID,
        Type:      typ,
        Content:   content,
        Status:    AmendmentPending,
        CreatedAt: time.Now().UTC(),
    }
}
```

- [x] **Step 1: Write tests**

```go
// amendment_test.go
package task

import (
    "testing"
    "time"
)

func TestNewAmendmentRequest(t *testing.T) {
    req := NewAmendmentRequest("task-1", AmendmentInjectContext, "skip the tests")

    if req.TaskID != "task-1" {
        t.Errorf("wrong task ID: %s", req.TaskID)
    }
    if req.Type != AmendmentInjectContext {
        t.Errorf("wrong type: %v", req.Type)
    }
    if req.Content != "skip the tests" {
        t.Errorf("wrong content: %s", req.Content)
    }
    if req.Status != AmendmentPending {
        t.Errorf("wrong status: %v", req.Status)
    }
    if req.ID == "" {
        t.Error("ID should not be empty")
    }
    if req.CreatedAt.IsZero() {
        t.Error("CreatedAt should be set")
    }
}
```

- [x] **Step 2: Run test, implement, verify**

- [x] **Step 3: Commit**

```bash
git add internal/task/amendment.go internal/task/amendment_test.go
git commit -m "feat: add AmendmentRequest types for task amendments"
```

---

### Task 5: AmendmentManager with Bus Integration

**Files:**
- Create: `internal/task/amendment_manager.go`

```go
// amendment_manager.go
package task

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "sync"

    "github.com/caimlas/meept/internal/bus"
    "github.com/caimlas/meept/pkg/models"
)

// AmendmentHandlerFunc handles an amendment request.
type AmendmentHandlerFunc func(context.Context, *AmendmentRequest) (*AmendmentReply, error)

// AmendmentManager manages amendment requests and routing.
type AmendmentManager struct {
    mu          sync.RWMutex
    bus         *bus.MessageBus
    logger      *slog.Logger
    handlers    map[AmendmentType]AmendmentHandlerFunc
    pending     map[string]*AmendmentRequest  // requestID -> request
    taskIndex   map[string][]string           // taskID -> []requestID
}

// NewAmendmentManager creates a new amendment manager.
func NewAmendmentManager(msgBus *bus.MessageBus, logger *slog.Logger) *AmendmentManager {
    if logger == nil {
        logger = slog.Default()
    }
    mgr := &AmendmentManager{
        bus:       msgBus,
        logger:    logger,
        handlers:  make(map[AmendmentType]AmendmentHandlerFunc),
        pending:   make(map[string]*AmendmentRequest),
        taskIndex: make(map[string][]string),
    }

    // Start subscription goroutine
    mgr.subscribe()

    return mgr
}

// RegisterHandler registers a handler for an amendment type.
func (m *AmendmentManager) RegisterHandler(typ AmendmentType, handler AmendmentHandlerFunc) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.handlers[typ] = handler
    m.logger.Debug("Registered amendment handler", "type", typ)
}

// Submit submits an amendment request.
func (m *AmendmentManager) Submit(ctx context.Context, req *AmendmentRequest) error {
    m.mu.Lock()
    m.pending[req.ID] = req
    m.taskIndex[req.TaskID] = append(m.taskIndex[req.TaskID], req.ID)
    m.mu.Unlock()

    // Publish to bus
    payload, _ := json.Marshal(req)
    msg := &models.BusMessage{
        ID:        req.ID,
        Type:      models.MessageTypeRequest,
        Topic:     "task.amend.request",
        Source:    "amendment-manager",
        Timestamp: time.Now().UTC(),
        Payload:   payload,
    }

    m.bus.Publish("task.amend.request", msg)

    m.logger.Info("Amendment submitted",
        "request_id", req.ID,
        "task_id", req.TaskID,
        "type", req.Type,
    )

    return nil
}

// GetPending returns a pending request by ID.
func (m *AmendmentManager) GetPending(requestID string) (*AmendmentRequest, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    req, ok := m.pending[requestID]
    return req, ok
}

// GetPendingForTask returns all pending requests for a task.
func (m *AmendmentManager) GetPendingForTask(taskID string) []*AmendmentRequest {
    m.mu.RLock()
    defer m.mu.RUnlock()

    requestIDs := m.taskIndex[taskID]
    var requests []*AmendmentRequest
    for _, id := range requestIDs {
        if req, ok := m.pending[id]; ok && req.Status == AmendmentPending {
            requests = append(requests, req)
        }
    }
    return requests
}

// Process applies a handler to a pending request.
func (m *AmendmentManager) Process(ctx context.Context, requestID string) (*AmendmentReply, error) {
    m.mu.Lock()
    req, ok := m.pending[requestID]
    if !ok {
        m.mu.Unlock()
        return nil, fmt.Errorf("request not found: %s", requestID)
    }
    handler, ok := m.handlers[req.Type]
    m.mu.Unlock()

    if handler == nil {
        reply := &AmendmentReply{
            RequestID: requestID,
            Success:   false,
            Message:   fmt.Sprintf("no handler for amendment type: %s", req.Type),
        }
        req.Status = AmendmentRejected
        return reply, nil
    }

    // Call handler
    reply, err := handler(ctx, req)
    if err != nil {
        req.Status = AmendmentRejected
        return nil, err
    }

    if reply.Success {
        req.Status = AmendmentApplied
        req.ProcessedAt = time.Now().UTC()
        m.publishEvent("task.amend.applied", req)
    } else {
        req.Status = AmendmentRejected
        m.publishEvent("task.amend.rejected", req)
    }

    return reply, nil
}

func (m *AmendmentManager) subscribe() {
    sub := m.bus.Subscribe("amendment-manager", "task.amend.request")
    go func() {
        for msg := range sub.Channel {
            var req AmendmentRequest
            if err := json.Unmarshal(msg.Payload, &req); err != nil {
                m.logger.Error("Failed to parse amendment request", "error", err)
                continue
            }

            // Auto-process if handler registered
            m.logger.Debug("Received amendment request", "id", req.ID, "type", req.Type)
        }
    }()
}

func (m *AmendmentManager) publishEvent(topic string, data any) {
    payload, _ := json.Marshal(data)
    msg := &models.BusMessage{
        ID:        fmt.Sprintf("amend-%d", time.Now().UnixNano()),
        Type:      models.MessageTypeEvent,
        Topic:     topic,
        Source:    "amendment-manager",
        Timestamp: time.Now().UTC(),
        Payload:   payload,
    }
    m.bus.Publish(topic, msg)
}
```

- [x] **Step 1-4: Test and implement**

- [x] **Step 5: Commit**

```bash
git add internal/task/amendment_manager.go
git commit -m "feat: add AmendmentManager with bus integration"
```

---

### Task 6: Amendment Handlers for Each Type

**Files:**
- Create: `internal/task/amendment_handlers.go`

```go
// amendment_handlers.go
package task

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/caimlas/meept/internal/queue"
)

// AmendmentHandlers provides built-in handlers for amendment types.
type AmendmentHandlers struct {
    registry    *Registry
    queue       queue.Queue
    stepStore   *StepStore
}

// NewAmendmentHandlers creates amendment handlers.
func NewAmendmentHandlers(registry *Registry, q queue.Queue) *AmendmentHandlers {
    return &AmendmentHandlers{
        registry:  registry,
        queue:     q,
        stepStore: registry.StepStore(),
    }
}

// RegisterAll registers all built-in handlers.
func (h *AmendmentHandlers) RegisterAll(mgr *AmendmentManager) {
    mgr.RegisterHandler(AmendmentInjectContext, h.handleInjectContext)
    mgr.RegisterHandler(AmendmentSkipStep, h.handleSkipStep)
    mgr.RegisterHandler(AmendmentAddStep, h.handleAddStep)
    mgr.RegisterHandler(AmendmentReprioritize, h.handleReprioritize)
    mgr.RegisterHandler(AmendmentChangeAgent, h.handleChangeAgent)
}

// handleInjectContext injects context into active agent loops.
func (h *AmendmentHandlers) handleInjectContext(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
    // Get task
    task, err := h.registry.Get(ctx, req.TaskID)
    if err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("task not found: %v", err),
        }, nil
    }

    // Inject context by adding to task's context query or memory refs
    // This will be picked up by agent loops on next iteration
    if task.ContextQuery != "" {
        task.ContextQuery += "\n" + req.Content
    } else {
        task.ContextQuery = req.Content
    }

    if err := h.registry.Update(ctx, task); err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("failed to update task: %v", err),
        }, nil
    }

    return &AmendmentReply{
        RequestID: req.ID,
        Success:   true,
        Message:   "Context injected successfully",
    }, nil
}

// handleSkipStep marks a step as skipped.
func (h *AmendmentHandlers) handleSkipStep(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
    if req.StepID == "" {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   "step_id required for skip_step",
        }, nil
    }

    step, err := h.stepStore.GetByID(req.StepID)
    if err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("step not found: %v", err),
        }, nil
    }

    if err := h.stepStore.SetState(req.StepID, StepSkipped); err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("failed to skip step: %v", err),
        }, nil
    }

    // Promote newly unblocked steps
    h.stepStore.PromoteReadySteps(req.TaskID)
    h.registry.StepStore().PromoteReadySteps(req.TaskID)

    return &AmendmentReply{
        RequestID: req.ID,
        Success:   true,
        Message:   fmt.Sprintf("Step %s skipped", req.StepID),
    }, nil
}

// handleAddStep adds a new step to a task.
func (h *AmendmentHandlers) handleAddStep(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
    var metadata struct {
        Description string   `json:"description"`
        ToolHint    string   `json:"tool_hint,omitempty"`
        DependsOn   []string `json:"depends_on,omitempty"`
    }
    if err := json.Unmarshal(req.Metadata, &metadata); err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("invalid metadata: %v", err),
        }, nil
    }

    // Get existing steps to determine sequence
    steps, _ := h.stepStore.ListByTaskID(req.TaskID)
    sequence := len(steps) + 1

    step := NewTaskStep(req.TaskID, metadata.Description, sequence)
    step.ToolHint = metadata.ToolHint
    step.DependsOn = metadata.DependsOn

    if err := h.stepStore.Create(step); err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("failed to create step: %v", err),
        }, nil
    }

    // Update task total jobs
    task, _ := h.registry.Get(ctx, req.TaskID)
    if task != nil {
        task.TotalJobs++
        h.registry.Update(ctx, task)
    }

    return &AmendmentReply{
        RequestID: req.ID,
        Success:   true,
        Message:   fmt.Sprintf("Step %s added", step.ID),
        Metadata:  json.RawMessage(fmt.Sprintf(`{"step_id":%q}`, step.ID)),
    }, nil
}

// handleReprioritize changes step sequence/priority.
func (h *AmendmentHandlers) handleReprioritize(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
    var metadata struct {
        StepIDs    []string `json:"step_ids"`  // New order
    }
    if err := json.Unmarshal(req.Metadata, &metadata); err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("invalid metadata: %v", err),
        }, nil
    }

    // Re-sequence steps
    for i, stepID := range metadata.StepIDs {
        step, err := h.stepStore.GetByID(stepID)
        if err != nil {
            continue
        }
        step.Sequence = i
        h.stepStore.Update(step)
    }

    return &AmendmentReply{
        RequestID: req.ID,
        Success:   true,
        Message:   "Steps reprioritized",
    }, nil
}

// handleChangeAgent reassigns a step to a different agent.
func (h *AmendmentHandlers) handleChangeAgent(ctx context.Context, req *AmendmentRequest) (*AmendmentReply, error) {
    var metadata struct {
        StepID  string `json:"step_id"`
        AgentID string `json:"agent_id"`
    }
    if err := json.Unmarshal(req.Metadata, &metadata); err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("invalid metadata: %v", err),
        }, nil
    }

    step, err := h.stepStore.GetByID(metadata.StepID)
    if err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("step not found: %v", err),
        }, nil
    }

    step.AgentID = metadata.AgentID
    if err := h.stepStore.Update(step); err != nil {
        return &AmendmentReply{
            RequestID: req.ID,
            Success:   false,
            Message:   fmt.Sprintf("failed to update step: %v", err),
        }, nil
    }

    return &AmendmentReply{
        RequestID: req.ID,
        Success:   true,
        Message:   fmt.Sprintf("Step %s reassigned to %s", metadata.StepID, metadata.AgentID),
    }, nil
}
```

- [x] **Step 1-4: Test and implement**

- [x] **Step 5: Commit**

```bash
git add internal/task/amendment_handlers.go
git commit -m "feat: add built-in amendment handlers"
```

---

## Phase 3: Job Queue Cancellation

### Task 7: Interrupt-Token-Aware Queue Claim

**Files:**
- Modify: `internal/queue/queue.go`
- Modify: `internal/queue/store.go` (if exists)

- [x] **Step 1: Add interrupt check to Claim**

```go
// queue.go - Modify Claim to check for task cancellation
func (q *PersistentQueue) Claim(ctx context.Context, workerID string, caps []string,
    interruptMgr *task.InterruptManager) (*Job, error) {

    job, err := q.store.ClaimNext(workerID, caps)
    if err != nil {
        return nil, err
    }
    if job == nil {
        return nil, nil
    }

    // Check if task is cancelled
    if interruptMgr != nil && job.TaskID != "" {
        if tok, ok := interruptMgr.Get(job.TaskID); ok {
            if tok.IsTriggered() {
                q.logger.Info("Skipping cancelled task",
                    "job_id", job.ID,
                    "task_id", job.TaskID,
                    "reason", tok.Reason(),
                )
                // Re-queue or skip
                return q.Claim(ctx, workerID, caps, interruptMgr)
            }
        }
    }

    return job, nil
}
```

- [x] **Step 2: Update worker to observe cancellation**

- [x] **Step 3: Tests and commit**

---

## Phase 4: TUI Integration

### Task 8: TUI Slash Commands for Interrupt/Amendment

**Files:**
- Modify: `internal/tui/slash_autocomplete.go`
- Modify: `internal/tui/app.go`

- [x] **Step 1: Add slash commands**

```go
// New slash commands:
// /cancel <task-id> [reason]
// /amend <task-id> <context|skip|add|reprioritize> [content]
// /interrupt <task-id> [message]
// /tasks - list active tasks
```

- [x] **Step 2: Implement command handlers**

- [x] **Step 3: Tests and commit**

---

## Summary

This plan implements 3 independent capabilities in parallelizable tasks:

1. **InterruptToken** - Cancellation context per task
2. **Amendment Protocol** - Structured mid-flight modifications
3. **Conversational Override** - Context injection into agent loops
4. **Queue Integration** - Job-level cancellation observation
5. **TUI Integration** - User-facing commands
