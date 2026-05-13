# Plan: Steering and Follow-Up Message Queues for Agent Loop (v2)

**Date:** 2026-05-11 (revised 2026-05-11)
**Scope:** `internal/agent/loop.go`, `internal/agent/dispatcher.go`, `internal/agent/registry.go`, `internal/tui/`, `internal/bus/`, `internal/services/`, `internal/memory/`
**Status:** v2 - addresses review concerns, ready for implementation
**Influence:** Pi Agent's steering and follow-up queue design
**Changes from v1:** Generation counter for lifecycle safety, single-message drain for steering, clarified conversation state, agent lifecycle events, explicit steering heuristic table, hybrid persistence layer

---

## 1. Problem Statement

Meept's agent loop is fire-and-forget. Once `reasoningCycle()` begins, it runs up to 25 iterations calling the LLM and executing tools with no mechanism for external input. The user has two pain points:

1. **No mid-task intervention.** If the agent is 15 iterations into a long coding task and the user realizes the approach is wrong, they must wait for it to finish (or hit `ctrl+c` to abort). There is no way to say "stop, do X instead" and have the agent incorporate that feedback.

2. **No follow-up chaining.** When the agent finishes (no more tool calls), the user might want to say "also do Y." Currently this requires a new `Chat()` RPC round-trip, losing the agent's accumulated context window and tool-use momentum.

Pi Agent (TypeScript) solves this with two first-class queues:
- **Steering queue**: injects a user message after the current tool batch completes, causing the LLM to see it on the next turn. This interrupts the current flow.
- **Follow-up queue**: queues messages delivered only when the agent would naturally stop (no more tool calls, no steering pending). This extends the agent's work.

Meept needs equivalent functionality adapted to Go's concurrency model.

---

## 2. Current Architecture

### Agent Loop (`internal/agent/loop.go`)

The `AgentLoop` struct (line 375) owns an LLM client, tool registry, message bus, and conversation store. It exposes two entry points:

- **`RunOnce(ctx, userMessage, conversationID)`** (line 816): synchronous, single-turn. Adds the user message to the conversation, calls `reasoningCycle()`, returns the final text response. Used by the dispatcher's `RouteToAgent()`.

- **`Run(ctx, messages <-chan, responses chan<-)`** (line 2617): channel-based, long-lived. Reads `AgentMessage` structs from an input channel, calls `RunOnce` for each, writes `AgentResponse` structs to an output channel.

The core loop is `reasoningCycle()` (line 1307):

```
for iteration := 1..MaxIterations:
    check context cancellation
    check token budget
    check multi-turn budget tracker
    call LLM with conversation history
    if LLM returned tool calls:
        execute tool calls
        add tool results to conversation
        continue  // next iteration
    else:
        return response.Content  // done
return "max iterations reached" error
```

Key observation: the loop has natural yield points between iterations. After tool calls are executed (line 1505-1541) and before the next LLM call, there is a `continue` statement. This is where steering messages should be injected.

### Dispatcher (`internal/agent/dispatcher.go`)

`ClassifyAndRoute()` classifies intent and produces a `DispatchResult` with an `AgentID`. `RouteToAgent()` (line 543) looks up the agent in the registry and calls `agent.RunOnce()`. The dispatcher has no back-channel to the agent loop once execution begins.

### TUI (`internal/tui/`)

The TUI sends chat via `ChatModel.sendMessage()` which calls `rpc.Chat(text, conversationID)` -- a synchronous JSON-RPC call to the daemon. The daemon processes it through `ChatService.Chat()` which publishes to `chat.request` on the bus and waits for a reply. While waiting, the TUI shows a loading spinner. There is no way to send a second message while the first is in-flight.

### Message Bus (`internal/bus/bus.go`)

Channel-based pub/sub. Supports topic wildcards. The `SubscriptionHandler` manages lifecycle. No existing mechanism for per-conversation message queuing.

---

## 3. Proposed Architecture (v2 - Revised)

### 3.1 QueuedMessage Type

```go
// internal/agent/queue.go

// QueueType distinguishes steering from follow-up messages.
type QueueType string

const (
    QueueTypeSteer    QueueType = "steer"
    QueueTypeFollowUp QueueType = "follow_up"
)

// DrainMode controls how many messages are consumed from a queue at once.
type DrainMode string

const (
    DrainOne DrainMode = "one"  // consume one message per check
    DrainAll DrainMode = "all"  // consume all messages at once
)

// QueuedMessage represents a user message waiting to be injected.
type QueuedMessage struct {
    ID        string    `json:"id"`
    Content   string    `json:"content"`
    QueueType QueueType `json:"queue_type"`
    Timestamp time.Time `json:"timestamp"`
    Source    string    `json:"source"` // e.g. "tui", "dispatcher", "api"
}

// QueueConfig controls drain behavior for each queue type.
type QueueConfig struct {
    Steering      DrainMode `json:"steering_drain"`      // always "one" for steering
    FollowUp      DrainMode `json:"followup_drain"`      // "one" or "all"
    MaxSteering   int       `json:"max_steering"`        // max queued steering messages
    MaxFollowUp   int       `json:"max_followup"`        // max queued follow-up messages
    PersistFollowUp bool    `json:"persist_followup"`    // persist follow-ups to disk
}
```

**Changed from v1:** Added `MaxSteering`, `MaxFollowUp`, and `PersistFollowUp` to config for overflow control and persistence.

### 3.2 MessageQueue (thread-safe, channel-backed, generation-tracked)

```go
// internal/agent/queue.go

// MessageQueue is a thread-safe queue that supports external goroutine injection.
// Uses a mutex-protected slice rather than a channel so we can inspect length,
// peek, and drain selectively.
type MessageQueue struct {
    mu       sync.Mutex
    wg       sync.WaitGroup

    // Steering queue - single message at a time, always DrainOne
    steeringQueue []QueuedMessage

    // Follow-up queue - supports multiple messages, configurable drain
    followUpQueue []QueuedMessage

    notifyCh chan struct{} // signaled on enqueue, read non-blocking

    // Lifecycle management - generation counter prevents zombie injection
    generation   uint64      // incremented on register, checked on deregister
    closed       atomic.Bool // true when queue is no longer active

    config   QueueConfig
    bus      *bus.MessageBus
    agentID  string
    logger   *slog.Logger

    // Persistence layer for follow-up messages (hybrid approach)
    // Follow-ups are persisted only when queue depth > 0 AND agent is idle
    persister *QueuePersister
}

// QueueStatus is a snapshot of queue state for UI display.
type QueueStatus struct {
    SteeringDepth   int  `json:"steering_depth"`
    FollowUpDepth   int  `json:"followup_depth"`
    IsActive        bool `json:"is_active"`       // false if closed
    Generation      uint64 `json:"generation"`     // for version-checking
}

// Steer injects a message into the steering queue.
// Returns ErrQueueClosed if the queue is no longer active.
// Returns ErrQueueFull if max_steering is exceeded.
func (q *MessageQueue) Steer(ctx context.Context, content, source string) error {
    q.mu.Lock()
    defer q.mu.Unlock()

    // Check closed state first - defense in depth
    if q.closed.Load() {
        return ErrQueueClosed
    }

    // Check overflow - reject if at limit
    if len(q.steeringQueue) >= q.config.MaxSteering {
        return ErrQueueFull
    }

    // Steering queue always has max 1 message - append or replace
    msg := QueuedMessage{
        ID:        uuid.NewString(),
        Content:   content,
        QueueType: QueueTypeSteer,
        Timestamp: time.Now(),
        Source:    source,
    }

    // Replace existing steering message (only keep latest)
    q.steeringQueue = []QueuedMessage{msg}

    // Signal agent loop
    q.notifyNonBlocking()

    // Publish bus event
    q.publishSteerAdded(msg)

    return nil
}

// FollowUp injects a message into the follow-up queue.
// Returns ErrQueueClosed if the queue is no longer active.
// Returns ErrQueueFull if max_followup is exceeded.
func (q *MessageQueue) FollowUp(ctx context.Context, content, source string) error {
    q.mu.Lock()
    defer q.mu.Unlock()

    if q.closed.Load() {
        return ErrQueueClosed
    }

    if len(q.followUpQueue) >= q.config.MaxFollowUp {
        return ErrQueueFull
    }

    msg := QueuedMessage{
        ID:        uuid.NewString(),
        Content:   content,
        QueueType: QueueTypeFollowUp,
        Timestamp: time.Now(),
        Source:    source,
    }

    q.followUpQueue = append(q.followUpQueue, msg)

    // Persist follow-up asynchronously if enabled
    // Only persist when: (1) feature enabled, (2) queue has messages
    if q.config.PersistFollowUp && q.persister != nil {
        q.wg.Add(1)
        go func() {
            defer q.wg.Done()
            // Don't persist immediately - batch on idle or shutdown
            // Persister handles debouncing internally
            q.persister.EnqueueAsync(msg)
        }()
    }

    q.notifyNonBlocking()
    q.publishFollowUpAdded(msg)

    return nil
}

// DrainSteering returns AT MOST ONE message from the steering queue.
// Steering is always DrainOne by design - user sends one redirection at a time.
// Returns empty slice if no steering messages pending.
func (q *MessageQueue) DrainSteering() []QueuedMessage {
    q.mu.Lock()
    defer q.mu.Unlock()

    if len(q.steeringQueue) == 0 {
        return nil
    }

    // Take only the first (and typically only) message
    msg := q.steeringQueue[0]
    q.steeringQueue = nil

    return []QueuedMessage{msg}
}

// DrainFollowUp returns messages from the follow-up queue per DrainMode.
// Returns empty slice if no follow-up messages pending.
func (q *MessageQueue) DrainFollowUp() []QueuedMessage {
    q.mu.Lock()
    defer q.mu.Unlock()

    if len(q.followUpQueue) == 0 {
        return nil
    }

    var drained []QueuedMessage
    switch q.config.FollowUp {
    case DrainOne:
        drained = []QueuedMessage{q.followUpQueue[0]}
        q.followUpQueue = q.followUpQueue[1:]
    case DrainAll:
        drained = make([]QueuedMessage, len(q.followUpQueue))
        copy(drained, q.followUpQueue)
        q.followUpQueue = nil
    }

    return drained
}

// HasSteering returns true if the steering queue is non-empty.
func (q *MessageQueue) HasSteering() bool {
    q.mu.Lock()
    defer q.mu.Unlock()
    return len(q.steeringQueue) > 0
}

// HasFollowUp returns true if the follow-up queue is non-empty.
func (q *MessageQueue) HasFollowUp() bool {
    q.mu.Lock()
    defer q.mu.Unlock()
    return len(q.followUpQueue) > 0
}

// IsClosed returns true if the queue is no longer active.
// Used by dispatcher to avoid injecting into exited loops.
func (q *MessageQueue) IsClosed() bool {
    return q.closed.Load()
}

// GetGeneration returns the current generation counter.
// Used for version-checking in registry lookups.
func (q *MessageQueue) GetGeneration() uint64 {
    return q.generation
}

// Close marks the queue as closed and persists pending follow-ups.
// Called by AgentRegistry on loop exit.
func (q *MessageQueue) Close() error {
    q.closed.Store(true)

    // Persist all pending follow-ups synchronously
    if q.config.PersistFollowUp && q.persister != nil {
        q.mu.Lock()
        followUps := q.followUpQueue
        q.followUpQueue = nil
        q.mu.Unlock()

        for _, msg := range followUps {
            q.persister.PersistSync(msg)
        }
    }

    // Wait for any in-flight persistence goroutines
    q.wg.Wait()

    return nil
}

// Status returns a snapshot of both queues for UI display.
func (q *MessageQueue) Status() QueueStatus {
    q.mu.Lock()
    defer q.mu.Unlock()

    return QueueStatus{
        SteeringDepth:   len(q.steeringQueue),
        FollowUpDepth:   len(q.followUpQueue),
        IsActive:        !q.closed.Load(),
        Generation:      q.generation,
    }
}

func (q *MessageQueue) notifyNonBlocking() {
    select {
    case q.notifyCh <- struct{}{}:
    default:
        // Channel already has pending notification
    }
}
```

**Changed from v1:**
- Added `generation uint64` counter for lifecycle version-checking
- Added `closed atomic.Bool` flag for immediate rejection of zombie injections
- Steering queue always drains one message at a time (simplified design)
- Added `Close()` method for cleanup and sync persistence
- Added `persister *QueuePersister` for hybrid persistence

### 3.3 QueuePersister (NEW - Hybrid Persistence Layer)

```go
// internal/agent/queue_persister.go

// QueuePersister handles async persistence of follow-up messages.
// Uses a write-behind strategy to minimize disk I/O:
// - Messages are batched in memory
// - Flush occurs on: (1) daemon shutdown, (2) queue close, (3) idle timeout
// - On daemon startup, pending follow-ups are loaded and re-injected
type QueuePersister struct {
    db       *sql.DB    // SQLite connection (shared with episodic memory)
    bus      *bus.MessageBus
    logger   *slog.Logger

    // Write-behind buffer
    mu        sync.Mutex
    pending   []QueuedMessage
    flushTimer *time.Timer
    flushDelay time.Duration  // default 5 seconds

    // Conversation ID for scoping persistence
    conversationID string
}

// NewQueuePersister creates a persister with write-behind batching.
func NewQueuePersister(db *sql.DB, bus *bus.MessageBus, conversationID string) *QueuePersister {
    return &QueuePersister{
        db:             db,
        bus:            bus,
        conversationID: conversationID,
        logger:         slog.Default(),
        flushDelay:     5 * time.Second,
    }
}

// EnqueueAsync adds a message to the write-behind buffer.
// Flush occurs after flushDelay or on explicit Flush().
func (p *QueuePersister) EnqueueAsync(msg QueuedMessage) {
    p.mu.Lock()
    defer p.mu.Unlock()

    p.pending = append(p.pending, msg)

    // Reset or start flush timer
    if p.flushTimer != nil {
        p.flushTimer.Stop()
    }
    p.flushTimer = time.AfterFunc(p.flushDelay, p.flushLocked)
}

// PersistSync immediately persists a message to disk.
// Used for high-priority messages or on queue close.
func (p *QueuePersister) PersistSync(msg QueuedMessage) error {
    query := `
        INSERT INTO queued_followups
        (conversation_id, message_id, content, queue_type, source, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT (message_id) DO UPDATE SET
            content = excluded.content,
            updated_at = excluded.updated_at
    `

    _, err := p.db.ExecContext(context.Background(), query,
        p.conversationID,
        msg.ID,
        msg.Content,
        msg.QueueType,
        msg.Source,
        msg.Timestamp,
    )

    if err != nil {
        p.logger.Error("Failed to persist follow-up",
            "conversation", p.conversationID,
            "error", err,
        )
    }

    return err
}

// Flush writes all pending messages to disk.
func (p *QueuePersister) Flush() error {
    p.mu.Lock()
    defer p.mu.Unlock()
    return p.flushLocked()
}

func (p *QueuePersister) flushLocked() error {
    if len(p.pending) == 0 {
        return nil
    }

    tx, err := p.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()

    query := `
        INSERT INTO queued_followups
        (conversation_id, message_id, content, queue_type, source, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT (message_id) DO NOTHING
    `

    for _, msg := range p.pending {
        if _, err := tx.ExecContext(context.Background(), query,
            p.conversationID, msg.ID, msg.Content, msg.QueueType, msg.Source, msg.Timestamp,
        ); err != nil {
            return err
        }
    }

    if err := tx.Commit(); err != nil {
        return err
    }

    p.pending = nil
    p.logger.Debug("Flushed pending follow-ups",
        "conversation", p.conversationID,
        "count", len(p.pending),
    )

    return nil
}

// LoadPending returns all follow-ups persisted for this conversation.
// Called on daemon startup to restore queued messages.
func (p *QueuePersister) LoadPending() ([]QueuedMessage, error) {
    query := `
        SELECT message_id, content, queue_type, source, created_at
        FROM queued_followups
        WHERE conversation_id = ?
        ORDER BY created_at ASC
    `

    rows, err := p.db.QueryContext(context.Background(), query, p.conversationID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var messages []QueuedMessage
    for rows.Next() {
        var msg QueuedMessage
        if err := rows.Scan(&msg.ID, &msg.Content, &msg.QueueType, &msg.Source, &msg.Timestamp); err != nil {
            return nil, err
        }
        messages = append(messages, msg)
    }

    return messages, rows.Err()
}

// ClearPending removes all persisted follow-ups after they've been consumed.
func (p *QueuePersister) ClearPending() error {
    _, err := p.db.ExecContext(context.Background(),
        `DELETE FROM queued_followups WHERE conversation_id = ?`,
        p.conversationID,
    )
    return err
}
```

**Database schema addition:**

```sql
-- internal/memory/schema.go

CREATE TABLE IF NOT EXISTS queued_followups (
    conversation_id TEXT NOT NULL,
    message_id      TEXT PRIMARY KEY,
    content         TEXT NOT NULL,
    queue_type      TEXT NOT NULL,
    source          TEXT NOT NULL,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_conversation (conversation_id)
);
```

**Why hybrid persistence?**
- Follow-ups are user-intended actions that should survive restarts
- Write-behind batching minimizes disk I/O (flush every 5s or on close)
- Steering messages are NOT persisted (transient, time-sensitive)
- On daemon startup, pending follow-ups are loaded and the user is notified

### 3.4 Integration into AgentLoop

Add a `queue *MessageQueue` field to `AgentLoop`. Modify `reasoningCycle()`:

```go
func (l *AgentLoop) reasoningCycle(ctx context.Context, conv *Conversation, conversationID string) (string, error) {
    // ... existing setup ...

    for iteration := 1; iteration <= l.config.MaxIterations; iteration++ {
        // ... existing context/budget checks ...

        // === STEERING CHECK: Before LLM call ===
        // Steering messages interrupt the current flow after tool results
        if l.queue != nil {
            if steerMsgs := l.queue.DrainSteering(); len(steerMsgs) > 0 {
                for _, sm := range steerMsgs {
                    conv.AddUserMessage(sm.Content)
                    l.logger.Info("Steering message injected",
                        "conversation", conversationID,
                        "source", sm.Source,
                        "iteration", iteration,
                    )
                }
                l.publishSteeringInjected(conversationID, steerMsgs)
            }
        }

        // ... existing LLM call, tool execution ...

        // (existing `continue` after tool calls -- loop back to top,
        //  where steering check runs again before next LLM call)

        // === FOLLOW-UP CHECK: Before returning final response ===
        // At this point, LLM returned text (no tool calls) - natural stopping point
        if l.queue != nil {
            if followMsgs := l.queue.DrainFollowUp(); len(followMsgs) > 0 {
                // CRITICAL: Add the assistant's response BEFORE the follow-up
                // This ensures the LLM sees its own output when processing follow-ups
                conv.AddAssistantMessage(response.Content)

                for _, fm := range followMsgs {
                    conv.AddUserMessage(fm.Content)
                    l.logger.Info("Follow-up message injected",
                        "conversation", conversationID,
                        "source", fm.Source,
                        "iteration", iteration,
                    )
                }
                l.publishFollowUpInjected(conversationID, followMsgs)
                continue // Loop back for another LLM turn
            }
        }

        return response.Content, nil
    }

    // Max iterations -- same as before
}
```

**Clarified from v1:**
- The `response.Content` is the LLM's answer. At the follow-up check point, this response has NOT been added to the conversation yet. Adding it before follow-ups ensures conversation continuity - the LLM sees "its own answer + follow-up question" as context for the next turn.

### 3.5 AgentRegistry Changes (v2 - Generation Counter)

```go
// internal/agent/registry.go

// QueueEntry wraps a MessageQueue with generation tracking for version-checking.
type QueueEntry struct {
    Queue      *MessageQueue
    Generation uint64
}

type AgentRegistry struct {
    // ... existing fields ...
    activeQueues   map[string]*QueueEntry // conversationID -> entry
    activeQueuesMu sync.RWMutex
    nextGen        uint64                 // monotonic counter
}

// RegisterActiveQueue associates a queue with a running conversation.
// Returns the generation number for this registration.
func (r *AgentRegistry) RegisterActiveQueue(conversationID string, q *MessageQueue) uint64 {
    r.activeQueuesMu.Lock()
    defer r.activeQueuesMu.Unlock()

    r.nextGen++
    entry := &QueueEntry{
        Queue:      q,
        Generation: r.nextGen,
    }

    // Set generation on the queue itself for cross-checking
    // (This would be done via a SetGeneration method on MessageQueue)

    r.activeQueues[conversationID] = entry
    return r.nextGen
}

// UnregisterActiveQueue removes the queue when the loop exits.
// Also closes the queue to reject any in-flight injection attempts.
func (r *AgentRegistry) UnregisterActiveQueue(conversationID string) {
    r.activeQueuesMu.Lock()
    defer r.activeQueuesMu.Unlock()

    entry, exists := r.activeQueues[conversationID]
    if !exists {
        return
    }

    // Close the queue - marks it as closed, persists pending follow-ups
    entry.Queue.Close()

    delete(r.activeQueues, conversationID)
}

// GetActiveQueue returns the queue for a running conversation, or nil.
// Also returns the generation counter for version-checking.
func (r *AgentRegistry) GetActiveQueue(conversationID string) (*MessageQueue, uint64) {
    r.activeQueuesMu.RLock()
    defer r.activeQueuesMu.RUnlock()

    entry, exists := r.activeQueues[conversationID]
    if !exists {
        return nil, 0
    }

    return entry.Queue, entry.Generation
}

// GetQueueWithVersion performs a version-check after lookup.
// Returns ErrQueueClosed or ErrGenerationMismatch if the queue is stale.
func (r *AgentRegistry) GetQueueWithVersion(conversationID string, expectedGen uint64) (*MessageQueue, error) {
    r.activeQueuesMu.RLock()
    defer r.activeQueuesMu.RUnlock()

    entry, exists := r.activeQueues[conversationID]
    if !exists {
        return nil, ErrQueueNotFound
    }

    if entry.Generation != expectedGen {
        return nil, ErrGenerationMismatch
    }

    if entry.Queue.IsClosed() {
        return nil, ErrQueueClosed
    }

    return entry.Queue, nil
}
```

**Changed from v1:**
- Wrapped queue in `QueueEntry` with generation counter
- `UnregisterActiveQueue` now calls `queue.Close()` before deletion
- Added `GetQueueWithVersion()` for defense-in-depth version checking
- Generation counter increments on every registration (not just per-conversation)

### 3.6 Dispatcher Integration (v2 - Steering Heuristic)

```go
// internal/agent/dispatcher.go

// SteeringHeuristicTable defines which intent types should steer vs follow-up.
// This is a clear decision table for routing behavior.
var SteeringHeuristicTable = map[IntentType]bool{
    // HIGH URGENCY - Steer (interrupt immediately)
    IntentCode:       true,  // User is redirecting coding approach
    IntentDebug:      true,  // User spotted a bug mid-execution
    IntentSecurity:   true,  // Security concern needs immediate attention
    IntentToolUse:    true,  // Explicit tool redirection

    // MEDIUM URGENCY - Context-dependent (default to follow-up)
    IntentChat:       false, // General chat can wait
    IntentRecall:     false, // Memory recall is not urgent
    IntentResearch:   false, // Research extensions follow naturally

    // LOW URGENCY - Always Follow-Up (wait for natural stop)
    IntentReport:     false, // Reporting status/information
    IntentPlatform:   false, // Platform events are informational
    IntentStatus:     false, // Status inquiries

    // UNKNOWN - Default to follow-up (safer)
    IntentUnknown:    false,
}

// shouldSteer determines if a message should interrupt the current flow.
// Returns true for steering, false for follow-up.
func shouldSteer(result *DispatchResult, explicitSteerMode bool) bool {
    // Explicit user override (ctrl+s) always wins
    if explicitSteerMode {
        return true
    }

    // Intent-based heuristic
    if shouldSteer, exists := SteeringHeuristicTable[result.Intent.Type]; exists {
        return shouldSteer
    }

    // Default: follow-up (safer, less disruptive)
    return false
}

// RouteToAgent checks if there's an active loop for this conversation.
// If so, it queues appropriately; otherwise it runs normally.
func (d *Dispatcher) RouteToAgent(ctx context.Context, result *DispatchResult, conversationID string) (string, error) {
    queue, generation := d.registry.GetActiveQueue(conversationID)
    if queue != nil {
        // Defense in depth: check queue closed state
        if queue.IsClosed() {
            d.logger.Info("Queue is closed, running new agent",
                "conversation", conversationID,
            )
            // Fall through to normal execution
        } else {
            d.logger.Info("Steering active agent",
                "conversation", conversationID,
                "agent", result.AgentID,
                "generation", generation,
            )

            // Determine steering vs follow-up
            isSteer := shouldSteer(result, result.ExplicitSteerMode)

            if isSteer {
                if err := queue.Steer(ctx, result.Intent.Summary, "dispatcher"); err != nil {
                    if errors.Is(err, ErrQueueClosed) || errors.Is(err, ErrQueueFull) {
                        d.logger.Warn("Queue injection failed, starting new agent",
                            "conversation", conversationID,
                            "error", err,
                        )
                        // Fall through to new agent
                    } else {
                        return "", err
                    }
                } else {
                    return "message queued (steer)", nil
                }
            } else {
                if err := queue.FollowUp(ctx, result.Intent.Summary, "dispatcher"); err != nil {
                    if errors.Is(err, ErrQueueClosed) || errors.Is(err, ErrQueueFull) {
                        d.logger.Warn("Queue injection failed, starting new agent",
                            "conversation", conversationID,
                            "error", err,
                        )
                        // Fall through to new agent
                    } else {
                        return "", err
                    }
                } else {
                    return "message queued (follow-up)", nil
                }
            }
        }
    }

    // No active loop, or queue closed/full -- run normally
    // ... existing code ...
}
```

**Changed from v1:**
- Added explicit `SteeringHeuristicTable` with all intent types mapped
- Added `explicitSteerMode` parameter for TUI override (ctrl+s)
- Graceful fallback: if queue is closed/full, start a new agent instead of erroring
- Generation counter logged for debugging

### 3.7 Agent Lifecycle Events (NEW)

Add new bus events to signal when agent loops start and exit:

```go
// internal/bus/events.go

// Agent lifecycle events
const (
    EventAgentStarted   = "agent.lifecycle.started"
    EventAgentEnded     = "agent.lifecycle.ended"
    EventAgentIteration = "agent.iteration.completed"  // Per-iteration
)

// AgentLifecyclePayload is emitted on agent start/end.
type AgentLifecyclePayload struct {
    ConversationID string `json:"conversation_id"`
    AgentID        string `json:"agent_id"`
    Generation     uint64 `json:"generation"`  // For version-checking
    Reason         string `json:"reason,omitempty"`  // e.g. "completed", "cancelled", "max_iterations"
}
```

Publish these events in `RunOnce()`:

```go
func (l *AgentLoop) RunOnce(ctx context.Context, userMessage, conversationID string) (string, error) {
    // ... existing setup ...

    // Publish started event
    l.bus.Publish(EventAgentStarted, AgentLifecyclePayload{
        ConversationID: conversationID,
        AgentID:        l.agentID,
        Generation:     generation,
    })

    // Register queue for external access
    if l.registry != nil && l.queue != nil {
        generation = l.registry.RegisterActiveQueue(conversationID, l.queue)
        defer func() {
            l.registry.UnregisterActiveQueue(conversationID)
            // Publish ended event after cleanup
            l.bus.Publish(EventAgentEnded, AgentLifecyclePayload{
                ConversationID: conversationID,
                AgentID:        l.agentID,
                Generation:     generation,
                Reason:         reason,
            })
        }()
    }

    // ... rest of RunOnce ...
}
```

**TUI Integration:**
- Listen for `agent.lifecycle.started` → set `agentActive = true`, show spinner
- Listen for `agent.lifecycle.ended` → set `agentActive = false`, hide spinner
- Listen for `agent.iteration.completed` → optional progress indicator

---

## 4. Steering Heuristic Decision Table

| Intent Type | Default Behavior | Rationale |
|-------------|------------------|-----------|
| `IntentCode` | **Steer** | User spotted wrong approach mid-execution |
| `IntentDebug` | **Steer** | Bug fix direction needs immediate injection |
| `IntentSecurity` | **Steer** | Security concern cannot wait |
| `IntentToolUse` | **Steer** | Explicit tool redirection |
| `IntentChat` | Follow-up | General conversation extends naturally |
| `IntentRecall` | Follow-up | Memory recall is not time-sensitive |
| `IntentResearch` | Follow-up | Research extensions build on completion |
| `IntentReport` | Follow-up | Status reports are informational |
| `IntentPlatform` | Follow-up | Platform events don't redirect |
| `IntentStatus` | Follow-up | Status inquiries wait for natural stop |
| `IntentUnknown` | Follow-up | Unknown intent → safer default |
| **Explicit (ctrl+s)** | **Steer** | User override always wins |

---

## 5. Conversation State Clarification

The conversation state at key points in `reasoningCycle()`:

```
┌─────────────────────────────────────────────────────────────┐
│ Point A: Top of loop, before LLM call                       │
│ - Steering messages (if any) are added as UserMessage       │
│ - LLM sees: [history + steering]                            │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│ Point B: After tool execution, before continue              │
│ - Tool results are added as ToolMessage                     │
│ - Loop continues to Point A                                 │
└─────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│ Point C: LLM returned text (no tool calls)                  │
│ - AT THIS POINT: response.Content holds the LLM answer      │
│ - response.Content has NOT been added to conversation yet   │
│ - Check follow-up queue                                     │
│   - If follow-ups exist:                                    │
│     1. Add response.Content as AssistantMessage             │
│     2. Add follow-ups as UserMessage(s)                     │
│     3. continue to Point A (another LLM turn)               │
│   - If no follow-ups:                                       │
│     1. Return response.Content                              │
│     2. Caller is responsible to add it to conversation      │
└─────────────────────────────────────────────────────────────┘
```

**Why this design?**
- The assistant message must be added BEFORE follow-ups so the LLM sees "answer → follow-up question" as a coherent exchange
- If no follow-ups exist, returning the response (without adding to conversation) lets the caller (`RunOnce`) handle persistence and bus events
- This matches the existing behavior when queues are nil

---

## 6. Implementation Phases

### Phase 1: Core Queue Infrastructure

**Files to create:**
- `internal/agent/queue.go` -- `MessageQueue`, `QueuedMessage`, `QueueConfig`, `QueueStatus`, drain logic, bus event publishing, generation counter, close handling
- `internal/agent/queue_persister.go` -- `QueuePersister` with write-behind buffering
- `internal/agent/queue_errors.go` -- Error types (`ErrQueueClosed`, `ErrQueueFull`, `ErrQueueNotFound`, `ErrGenerationMismatch`)

**Files to modify:**
- `internal/agent/loop.go` -- Add `queue *MessageQueue` field, add `WithMessageQueue(q *MessageQueue) LoopOption`, modify `reasoningCycle()` for steering/follow-up checks, add lifecycle event publishing

**Tests:**
- `internal/agent/queue_test.go` -- Concurrent enqueue/drain, generation counter, close behavior, drain modes, overflow rejection
- `internal/agent/queue_persister_test.go` -- Write-behind buffering, persistence, recovery

**Risk:** Low. Queue is standalone; loop changes are additive.

### Phase 2: Registry with Generation Tracking

**Files to modify:**
- `internal/agent/registry.go` -- Add `QueueEntry` wrapper, `activeQueues` map, generation counter, `RegisterActiveQueue()`, `UnregisterActiveQueue()` (with close), `GetQueueWithVersion()`

**Files to modify:**
- `internal/agent/loop.go` -- Register/unregister queue in `RunOnce()`, publish `agent.lifecycle.started` and `agent.lifecycle.ended` events

**Tests:**
- Update existing registry tests for queue registration
- Test generation counter uniqueness
- Test close-on-unregister behavior

**Risk:** Low. Standard map-with-mutex pattern.

### Phase 3: Dispatcher with Steering Heuristic

**Files to modify:**
- `internal/agent/dispatcher.go` -- Add `SteeringHeuristicTable`, `shouldSteer()` function, modify `RouteToAgent()` for queue lookup with graceful fallback, add `SteerActiveAgent()` and `FollowUpActiveAgent()` methods

**Files to modify:**
- `internal/agent/intent.go` -- Add `IntentUnknown` constant if not present

**Tests:**
- Unit tests for steering heuristic table coverage
- Unit tests for graceful fallback when queue is closed/full
- Integration test: router with active vs inactive queues

**Risk:** Medium. Routing logic needs testing for edge cases.

### Phase 4: Persistence Layer

**Files to modify:**
- `internal/memory/schema.go` -- Add `queued_followups` table
- `internal/memory/task_memory.go` (or new `internal/memory/queue_store.go`) -- Add `QueuePersister` integration

**Files to modify:**
- `internal/agent/queue_persister.go` -- (created in Phase 1) Wire up to actual SQLite connection

**Files to modify:**
- `cmd/meept-daemon/main.go` -- On startup, load pending follow-ups and notify user (via bus event `agent.queue.followup.restored`)

**Tests:**
- Persistence integration tests
- Recovery tests (daemon restart scenario)

**Risk:** Low. Standard SQL CRUD operations.

### Phase 5: Service Layer

**Files to modify:**
- `internal/services/chat_service.go` -- Add `Steer()`, `FollowUp()`, `GetQueueStatus()` methods

**Files to modify:**
- `internal/services/registry.go` -- Register new service methods

**Files to modify:**
- `internal/rpc/dev.go` -- Add RPC handlers for `chat.steer`, `chat.followup`, `chat.queue_status`, `chat.queue.restore`

**Files to modify:**
- `internal/comm/http/api_handlers.go` -- Add HTTP endpoints:
  - `POST /api/v1/chat/steer`
  - `POST /api/v1/chat/followup`
  - `POST /api/v1/chat/steer-explicit` (for ctrl+s equivalent)
  - `GET /api/v1/chat/queue/:conversation_id`

**Tests:**
- Service layer unit tests
- RPC integration tests
- HTTP endpoint tests

**Risk:** Low. Standard service/RPC/HTTP plumbing.

### Phase 6: TUI Integration

**Files to modify:**
- `internal/tui/rpc.go` -- Add `Steer()`, `FollowUp()`, `GetQueueStatus()`, `RestorePendingFollowUps()` RPC methods

**Files to modify:**
- `internal/tui/events.go` -- Add event handlers for:
  - `agent.lifecycle.started` → `agentActive = true`
  - `agent.lifecycle.ended` → `agentActive = false`
  - `agent.queue.steer.injected` → Show system message
  - `agent.queue.followup.injected` → Show system message
  - `agent.queue.followup.restored` → Show "N pending messages restored" toast

**Files to modify:**
- `internal/tui/models/chat.go` -- Add `agentActive`, `queueStatus`, `steerMode` fields; modify `Update()` to handle queue state; add queue indicator rendering

**Files to modify:**
- `internal/tui/app.go` -- Wire up:
  - `ctrl+s` key binding → toggle `steerMode` (with toast feedback: "Steer mode: ON/OFF")
  - Enter key → check `steerMode`, send to appropriate queue if agent is active

**Files to modify:**
- `internal/tui/styles.go` -- Add styles for:
  - Steer badge (urgent color when `steerMode` is true)
  - Follow-up badge (subtle color)
  - System message style for injected messages
  - Inactive/spinner state styling

**Files to modify:**
- `internal/tui/views/chat_view.go` -- Render queue indicators, handle `ctrl+s` toggle, show system messages for injections

**Tests:**
- TUI model tests for state transitions
- Manual TUI testing for key bindings and indicator visibility

**Risk:** Medium. TUI state management requires careful testing.

### Phase 7: Configuration

**Files to modify:**
- `internal/config/config.go` -- Add `QueueConfig` to `AgentConfig`

**Files to modify:**
- `config/meept.json5` -- Add default queue configuration:

```json5
{
  agent: {
    queues: {
      steering_drain: "one",        // Always "one" for steering
      followup_drain: "one",        // "one" or "all"
      max_steering: 5,              // Max queued steering (low - they're urgent)
      max_followup: 20,             // Max queued follow-ups
      persist_followup: true,       // Enable hybrid persistence
      flush_delay_ms: 5000,         // Write-behind flush delay
    },
  },
}
```

**Risk:** Low.

---

## 7. Testing Strategy (Updated)

- **Unit tests for `MessageQueue`**:
  - Concurrent enqueue/drain with race detector
  - Generation counter uniqueness
  - Close behavior (rejects after close)
  - Steering always drains one
  - Follow-up respects DrainMode
  - Overflow rejection with proper errors

- **Unit tests for `QueuePersister`**:
  - Write-behind buffering
  - Flush on timer
  - Flush on close
  - Recovery after "restart"

- **Unit tests for `reasoningCycle` modifications**:
  - Steering injected after tool batch
  - Follow-up injected at natural stop
  - Assistant message added before follow-up
  - Nil queue (no change to behavior)

- **Integration tests**:
  - Dispatcher routes to active queue when running
  - Graceful fallback when queue is closed
  - Graceful fallback when queue is full
  - Registry unregisters on loop exit

- **Persistence tests**:
  - Follow-ups survive "daemon restart" (simulated)
  - Steering messages are NOT persisted
  - Pending follow-ups are loaded and notified

- **TUI tests**:
  - Agent active state reflects lifecycle events
  - Queue indicators render correctly
  - ctrl+s toggles steer mode
  - System messages appear on injection

- **Race detector**: Run ALL tests with `-race`

---

## 8. Open Questions (Resolved in v2)

| Question | Resolution |
|----------|------------|
| Steering vs follow-up default? | Follow-up is default; steering requires high-urgency intent or explicit ctrl+s |
| Queue depth limits behavior? | Reject with error, return to client/RPC, TUI shows toast notification |
| Steering message ordering? | Always one at a time (DrainOne). Latest steering message replaces prior (only keep most recent). |
| Cross-agent steering? | Not supported in initial implementation. Message always queues on current conversation's agent. Agent change requires new conversation. |
| Persistence? | Hybrid: Follow-ups persisted with write-behind batching. Steering NOT persisted. |
| Token budget interaction? | Steering/follow-ups count normally toward token budget. Existing tracking handles this. |
| Max iterations with follow-ups? | Follow-ups extend the session, but max iterations still applies. Existing `ErrMaxIterationsReached` handling summarizes and stops. |
| Conversation state at follow-up? | Assistant message is added BEFORE follow-up injection, ensuring LLM sees its own answer. |

---

## 9. Success Criteria

- User can type a message while agent is running → queued as follow-up, injected at natural stop
- User can press `ctrl+s` → toggles steer mode, next message interrupts after current tool batch
- Bus events published for:
  - `agent.lifecycle.started`, `agent.lifecycle.ended`
  - `agent.queue.steer.added`, `agent.queue.followup.added`
  - `agent.queue.steer.injected`, `agent.queue.followup.injected`
  - `agent.queue.followup.restored` (on daemon startup)
- TUI shows:
  - Agent active indicator (spinner) driven by lifecycle events
  - Queue depth badges: `[steer: 1] [follow-up: 3]`
  - Steer mode indicator when `ctrl+s` is active
  - System messages for injected steering/follow-ups
- No regressions in `RunOnce()` behavior when queue is nil
- Pending follow-ups survive daemon restart (persisted to SQLite, loaded on startup)
- All tests pass with `-race`

---

## 10. Rollback Plan

If implementation issues arise:

1. **Queue feature toggle**: Add `queues.enabled: false` config to disable entire feature
2. **Graceful degradation**:
   - If persister fails → continue with in-memory only (log warning)
   - If queue injection fails → start new agent (existing behavior)
3. **Backward compatibility**: When `queue` is nil, `reasoningCycle()` behaves identically to pre-queue implementation

---

## 11. Migration Notes

No schema migration required for existing data. The `queued_followups` table is new and starts empty.

Users upgrading from pre-queue versions:
- Feature is opt-in via config (`persist_followup: true`)
- No existing conversations are affected
- Steering/follow-up only works for NEW conversations started after upgrade
