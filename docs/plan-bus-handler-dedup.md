# Plan: Bus Handler Deduplication

## Overview

Three packages (`internal/task/registry.go`, `internal/queue/queue.go`, `internal/session/session.go`) implement nearly identical bus subscription handlers with duplicated boilerplate code.

**Source:** `plan-missing-implementation.md` §3.2

## Current State

### Duplicated Code
All three packages define:
```go
type Handler struct {
    bus    *bus.MessageBus
    topics []string
    ctx    context.Context
    cancel context.CancelFunc
    wg     sync.WaitGroup
}

func (h *Handler) Start(ctx context.Context) {
    for _, topic := range h.topics {
        h.wg.Add(1)
        go h.handleTopic(topic)
    }
}

func (h *Handler) handleTopic(topic string) {
    defer h.wg.Done()
    // Subscribe to topic
    // Wait for messages in loop
    // Pass to callback
}

func (h *Handler) Stop() {
    h.cancel()
    h.wg.Wait()
}
```

| Package | File | Lines | Purpose |
|---------|------|-------|---------|
| task | `registry.go` | 337-369 | Task CRUD + step events |
| queue | `queue.go` | 286-317 | Job lifecycle events |
| session | `session.go` | 379-417 | Session tracking events |

**Similarity:** ~90% (only callback implementations differ)

## Solution: Generic `SubscriptionHandler`

Extract a reusable base type that handles subscribe/dispatch/teardown.

### Type Design

```go
// internal/bus/handler.go (NEW)

// MessageCallback is invoked when a message arrives on a subscribed topic.
type MessageCallback func(ctx context.Context, topic string, message json.RawMessage)

// SubscriptionHandler manages bus subscriptions with automatic lifecycle
type SubscriptionHandler struct {
    bus       *MessageBus
    callbacks map[string]MessageCallback  // topic → callback
    ctx       context.Context
    cancel    context.CancelFunc
    wg        sync.WaitGroup
    logger    *slog.Logger
}

// NewSubscriptionHandler creates a new handler
func NewSubscriptionHandler(bus *MessageBus, logger *slog.Logger) *SubscriptionHandler

// Subscribe adds a topic→callback mapping
func (h *SubscriptionHandler) Subscribe(topic string, callback MessageCallback)

// Start begins listening to all subscribed topics
func (h *SubscriptionHandler) Start(parentCtx context.Context)

// Stop gracefully shuts down all subscription goroutines
func (h *SubscriptionHandler) Stop()
```

### Migration Steps

#### Phase 1: Create Generic Handler
1. Create `internal/bus/handler.go` with `SubscriptionHandler`
2. Implement generic subscription loop with per-topic callbacks
3. Add proper lifecycle management (Start/Stop with `sync.WaitGroup`)

**Effort:** ~80 lines

#### Phase 2: Migrate Task Registry
```go
// Before (internal/task/registry.go)
type Handler struct {
    bus  *bus.MessageBus
    // ... 30 lines of identical fields
}
func (h *Handler) Start(ctx context.Context) { /* boilerplate */ }
func (h *Handler) Stop() { /* boilerplate */ }

// After
type Handler struct {
    handler *bus.SubscriptionHandler
    store   *Store
}

func NewHandler(bus *bus.MessageBus, store *Store) *Handler {
    h := &Handler{
        handler: bus.NewSubscriptionHandler(logger.With("component", "task-handler")),
        store:   store,
    }
    h.handler.Subscribe("task.create", h.handleTaskCreate)
    h.handler.Subscribe("task.update", h.handleTaskUpdate)
    // ...
    return h
}

func (h *Handler) Start(ctx context.Context) {
    h.handler.Start(ctx)  // Delegates to generic handler
}

func (h *Handler) Stop() {
    h.handler.Stop()  // Delegates to generic handler
}
```

**Effort:** ~30 lines (removed ~40 lines of boilerplate)

#### Phase 3: Migrate Queue Handler
Same pattern - replace embedded handler with `*bus.SubscriptionHandler`, subscribe topics, delegate Start/Stop.

**Effort:** ~30 lines (removed ~40 lines)

#### Phase 4: Migrate Session Handler
Same pattern.

**Effort:** ~30 lines (removed ~40 lines)

#### Phase 5: Tests
1. Unit tests for `SubscriptionHandler` generic type
2. Verify task handler tests still pass
3. Verify queue handler tests still pass
4. Verify session handler tests still pass

## Implementation Phases

### Phase 1: Generic Handler (40%)
1. Create `internal/bus/handler.go`
2. Implement `SubscriptionHandler` with topic→callback map
3. Lifecycle management (Start/Stop with goroutine cleanup)

### Phase 2: Migrate Task Handler (20%)
1. Replace duplicated handler with generic SubscriptionHandler
2. Update topic subscriptions to use callback pattern
3. Delegate Start/Stop to generic handler

### Phase 3: Migrate Queue Handler (15%)
Same pattern as task handler.

### Phase 4: Migrate Session Handler (15%)
Same pattern.

### Phase 5: Polish (10%)
1. Add documentation
2. Verify no behavioral changes
3. Ensure all existing tests pass
4. Update CLAUDE.md

## Files to Create

| File | Purpose |
|------|---------|
| `internal/bus/handler.go` | Generic `SubscriptionHandler` with lifecycle management |
| `internal/bus/handler_test.go` | Unit tests for generic handler |

## Files to Modify

| File | Change |
|------|--------|
| `internal/task/registry.go` | Replace duplicated handler with generic SubscriptionHandler |
| `internal/queue/queue.go` | Replace duplicated handler with generic SubscriptionHandler |
| `internal/session/session.go` | Replace duplicated handler with generic SubscriptionHandler |
| `CLAUDE.md` | Document generic handler pattern in architecture section |

## Risks & Mitigation

| Risk | Severity | Mitigation |
|------|----------|------------|
| Callback closure captures wrong state | Medium | Test each handler thoroughly |
| Goroutine leak on Stop() | High | Verify `wg.Done()` always called via defer |
| Topic name changes | Low | Use string constants for topics |
| Callback error handling | Medium | Keep existing error logging in each callback |

## Success Criteria

- ✅ All three handlers use `bus.SubscriptionHandler` internally
- ✅ No behavioral changes (same topics, same callbacks)
- ✅ All existing tests pass
- ✅ ~100 lines of duplicated code removed
- ✅ Build passes cleanly

## Open Questions

1. **Should callbacks return errors?** Could add error handling to generic handler, but keeping it simple (let each callback log errors) is better for now.

2. **Should handler support dynamic subscribe/unsubscribe?** Not needed - all topics are known at construction time.

3. **Should there be a callback registry interface?** Overkill - simple function callbacks are sufficient.
