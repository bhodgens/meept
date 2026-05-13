# Plan: Steering and Follow-Up Message Queues for Agent Loop

**Date:** 2026-05-11
**Scope:** `internal/agent/loop.go`, `internal/agent/dispatcher.go`, `internal/tui/`, `internal/bus/`, `internal/services/`
**Status:** tentative / needs-review
**Influence:** Pi Agent's steering and follow-up queue design

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

## 3. Proposed Architecture

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
    Steering DrainMode `json:"steering_drain"`
    FollowUp DrainMode `json:"followup_drain"`
}
```

### 3.2 MessageQueue (thread-safe, channel-backed)

```go
// internal/agent/queue.go

// MessageQueue is a thread-safe queue that supports external goroutine injection.
// Uses a mutex-protected slice rather than a channel so we can inspect length,
// peek, and drain selectively.
type MessageQueue struct {
    mu       sync.Mutex
    items    []QueuedMessage
    notifyCh chan struct{} // signaled on enqueue, read non-blocking
    config   QueueConfig
    bus      *bus.MessageBus
    agentID  string
    logger   *slog.Logger
}

// Steer injects a message into the steering queue.
// The agent loop will see this message after the current tool batch completes.
func (q *MessageQueue) Steer(ctx context.Context, content, source string) error { ... }

// FollowUp injects a message into the follow-up queue.
// The agent loop will see this message only when it would otherwise stop.
func (q *MessageQueue) FollowUp(ctx context.Context, content, source string) error { ... }

// DrainSteering returns messages from the steering queue (respects DrainMode).
// Returns empty slice if no steering messages pending.
func (q *MessageQueue) DrainSteering() []QueuedMessage { ... }

// DrainFollowUp returns messages from the follow-up queue (respects DrainMode).
// Returns empty slice if no follow-up messages pending.
func (q *MessageQueue) DrainFollowUp() []QueuedMessage { ... }

// HasSteering returns true if the steering queue is non-empty.
func (q *MessageQueue) HasSteering() bool { ... }

// HasFollowUp returns true if the follow-up queue is non-empty.
func (q *MessageQueue) HasFollowUp() bool { ... }

// Status returns a snapshot of both queues for UI display.
func (q *MessageQueue) Status() QueueStatus { ... }
```

The `notifyCh` is a buffered channel (size 1) used as a signaling mechanism. `Steer()` and `FollowUp()` do a non-blocking send to `notifyCh` after acquiring the mutex. The agent loop can `select` on `notifyCh` to short-circuit any waiting (though in practice the loop is always busy, so it will check at the next iteration boundary).

### 3.3 Integration into AgentLoop

Add a `queue *MessageQueue` field to `AgentLoop`. Modify `reasoningCycle()`:

```go
func (l *AgentLoop) reasoningCycle(ctx context.Context, conv *Conversation, conversationID string) (string, error) {
    // ... existing setup ...

    for iteration := 1; iteration <= l.config.MaxIterations; iteration++ {
        // ... existing context/budget checks ...

        // === NEW: check steering queue before LLM call ===
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
                // Publish bus event
                l.publishSteeringInjected(conversationID, steerMsgs)
            }
        }

        // ... existing LLM call, tool execution ...

        // (existing `continue` after tool calls -- loop back to top,
        //  where steering check runs again before next LLM call)

        // === NEW: before returning final response, check follow-up ===
        // (at the point where we would return response.Content)
        if l.queue != nil {
            if followMsgs := l.queue.DrainFollowUp(); len(followMsgs) > 0 {
                for _, fm := range followMsgs {
                    conv.AddUserMessage(fm.Content)
                    l.logger.Info("Follow-up message injected",
                        "conversation", conversationID,
                        "source", fm.Source,
                        "iteration", iteration,
                    )
                }
                // Add the original response as assistant message so the LLM
                // sees its own answer before the follow-up
                conv.AddAssistantMessage(response.Content)
                l.publishFollowUpInjected(conversationID, followMsgs)
                continue // loop back for another LLM turn
            }
        }

        return response.Content, nil
    }

    // Max iterations -- same as before
}
```

**Steering check placement**: Before the LLM call, at the top of the loop. This means a steering message injected while the LLM is thinking or tools are executing will be picked up at the start of the next iteration, after tool results have been added. The LLM sees the steering message alongside the tool results.

**Follow-up check placement**: After the LLM returns a text response (no tool calls), before returning. This means follow-ups extend the conversation naturally -- the agent was about to stop, but now has more to do.

### 3.4 Dispatcher Integration

The dispatcher needs a reference to the currently running agent's message queue. Two approaches:

**Option A: Registry-held queue map.** The `AgentRegistry` maintains a `map[string]*MessageQueue` keyed by conversation ID. When the dispatcher receives a new message for a conversation that already has an active agent loop, it injects into that queue instead of creating a new `RunOnce` call.

```go
// internal/agent/dispatcher.go

// RouteToAgent checks if there's an active loop for this conversation.
// If so, it steers; otherwise it runs normally.
func (d *Dispatcher) RouteToAgent(ctx context.Context, result *DispatchResult, conversationID string) (string, error) {
    // Check if there's an active agent loop for this conversation
    if queue := d.registry.GetActiveQueue(conversationID); queue != nil {
        d.logger.Info("Steering active agent",
            "conversation", conversationID,
            "agent", result.AgentID,
        )
        // Determine if this is a steering or follow-up message
        // based on intent type or explicit flag
        if shouldSteer(result) {
            if err := queue.Steer(ctx, result.Intent.Summary, "dispatcher"); err != nil {
                return "", err
            }
        } else {
            if err := queue.FollowUp(ctx, result.Intent.Summary, "dispatcher"); err != nil {
                return "", err
            }
        }
        return "message queued", nil
    }

    // No active loop -- run normally (existing code)
    // ...
}
```

**Steering vs follow-up routing heuristic:**
- Messages with high-confidence tool-use intent (`IntentCode`, `IntentDebug`) -> **steer** (the user wants to redirect)
- Messages with low urgency or clarification intent (`IntentChat`, `IntentRecall`) -> **follow-up** (extend when done)
- Messages with `IntentReport` or `IntentPlatform` -> **follow-up** (informational, can wait)
- The TUI can also expose an explicit toggle: `ctrl+s` to force steering, default is follow-up

### 3.5 AgentRegistry Changes

```go
// internal/agent/registry.go

type AgentRegistry struct {
    // ... existing fields ...
    activeQueues   map[string]*MessageQueue // conversationID -> queue
    activeQueuesMu sync.RWMutex
}

// RegisterActiveQueue associates a queue with a running conversation.
func (r *AgentRegistry) RegisterActiveQueue(conversationID string, q *MessageQueue) { ... }

// UnregisterActiveQueue removes the queue when the loop exits.
func (r *AgentRegistry) UnregisterActiveQueue(conversationID string) { ... }

// GetActiveQueue returns the queue for a running conversation, or nil.
func (r *AgentRegistry) GetActiveQueue(conversationID string) *MessageQueue { ... }
```

The `RunOnce` method registers/unregisters the queue around the `reasoningCycle` call:

```go
func (l *AgentLoop) RunOnce(ctx context.Context, userMessage, conversationID string) (string, error) {
    // ... existing setup ...

    // Register queue for external access
    if l.registry != nil && l.queue != nil {
        l.registry.RegisterActiveQueue(conversationID, l.queue)
        defer l.registry.UnregisterActiveQueue(conversationID)
    }

    // ... rest of RunOnce ...
}
```

### 3.6 Service Layer

New methods on `ChatService`:

```go
// internal/services/chat_service.go

// SteerRequest sends a steering message to an active agent loop.
type SteerRequest struct {
    Message        string `json:"message"`
    ConversationID string `json:"conversation_id"`
    Source         string `json:"source,omitempty"`
}

// Steer injects a message into the steering queue.
func (s *ChatService) Steer(ctx context.Context, req SteerRequest) error { ... }

// FollowUpRequest sends a follow-up message to an active agent loop.
type FollowUpRequest struct {
    Message        string `json:"message"`
    ConversationID string `json:"conversation_id"`
    Source         string `json:"source,omitempty"`
}

// FollowUp injects a message into the follow-up queue.
func (s *ChatService) FollowUp(ctx context.Context, req FollowUpRequest) error { ... }

// QueueStatusRequest queries the current queue state.
type QueueStatusRequest struct {
    ConversationID string `json:"conversation_id"`
}

// GetQueueStatus returns the current queue state for a conversation.
func (s *ChatService) GetQueueStatus(ctx context.Context, req QueueStatusRequest) (*QueueStatusResponse, error) { ... }
```

### 3.7 Bus Events

New event types published when queues change:

| Topic | Payload | When |
|-------|---------|------|
| `agent.queue.steer.added` | `QueueEventPayload` | Steering message enqueued |
| `agent.queue.followup.added` | `QueueEventPayload` | Follow-up message enqueued |
| `agent.queue.steer.injected` | `QueueInjectedPayload` | Steering messages drained into conversation |
| `agent.queue.followup.injected` | `QueueInjectedPayload` | Follow-up messages drained into conversation |
| `agent.queue.status` | `QueueStatusResponse` | Periodic status (for TUI polling) |

```go
// QueueEventPayload is emitted when a message is enqueued.
type QueueEventPayload struct {
    ConversationID string    `json:"conversation_id"`
    QueueType      QueueType `json:"queue_type"`
    MessageID      string    `json:"message_id"`
    ContentPreview string    `json:"content_preview"` // first 100 chars
    Source         string    `json:"source"`
    QueueDepth     int       `json:"queue_depth"`     // messages remaining after this one
}

// QueueInjectedPayload is emitted when messages are drained.
type QueueInjectedPayload struct {
    ConversationID string      `json:"conversation_id"`
    QueueType      QueueType   `json:"queue_type"`
    Count          int         `json:"count"`
    MessageIDs     []string    `json:"message_ids"`
    Iteration      int         `json:"iteration"`
}
```

### 3.8 TUI Changes

#### 3.8.1 RPC Client Methods

```go
// internal/tui/rpc.go

// Steer sends a steering message to an active conversation.
func (c *RPCClient) Steer(message, conversationID string) error { ... }

// FollowUp sends a follow-up message to an active conversation.
func (c *RPCClient) FollowUp(message, conversationID string) error { ... }

// GetQueueStatus returns the current queue state for a conversation.
func (c *RPCClient) GetQueueStatus(conversationID string) (*QueueStatusResponse, error) { ... }
```

#### 3.8.2 ChatModel Changes

The `ChatModel` needs to track whether the agent is currently processing (to know if steering/follow-up is possible) and display pending queue indicators.

```go
// internal/tui/models/chat.go

type ChatModel struct {
    // ... existing fields ...
    agentActive     bool              // true while agent is processing
    queueStatus     *QueueStatusResponse // latest queue state (nil if agent idle)
    steerMode       bool              // when true, next message is a steer (ctrl+s toggle)
}
```

#### 3.8.3 Input Behavior

When the agent is active (spinner showing):
- Normal enter sends a **follow-up** message (queued, does not interrupt)
- `ctrl+s` then enter sends a **steering** message (interrupts after current tool batch)
- A small indicator near the input shows: `steer [2]` or `queued [1]` with queue depths

When the agent is idle:
- Normal behavior (synchronous `Chat()` call, no queuing)

#### 3.8.4 Visual Indicators

In the chat view, when messages are queued:
- A status line below the input field: `[steer: 2] [follow-up: 1]`
- When a steering message is injected, a system message appears: `[steering] message injected at iteration 7`
- When a follow-up is injected: `[follow-up] extending session with queued message`

The sidebar should show an indicator dot on the chat tab when queues are non-empty (similar to existing `tabFlash` mechanism).

#### 3.8.5 Event Stream Subscription

Add new topics to `DefaultEventStreamConfig`:

```go
Topics: []string{
    "agent.*",
    "agent.queue.*",  // NEW
    "task.*",
    // ...
}
```

Handle `agent.queue.*` events in the TUI update loop to refresh `queueStatus` and flash indicators.

---

## 4. Pros/Cons Analysis

### 4.1 Pi Agent's Approach (pure-function callbacks)

Pi Agent uses `getSteeringMessages()` and `getFollowUpMessages()` callbacks passed to the agent runner:

```typescript
// Pi Agent (TypeScript)
const result = await runAgent({
  getSteeringMessages: () => steeringQueue.drain(),
  getFollowUpMessages: () => followUpQueue.drain(),
});
```

**Pros:**
- Simple, testable -- pass a mock function in tests
- No state on the agent runner itself
- Functional, predictable

**Cons:**
- Requires the caller to hold the queue and provide messages synchronously
- In JS's single-threaded event loop, "external goroutines" do not exist -- the only way to inject is via the callback
- Does not support multiple producers (only the caller can provide messages)
- No way for a third party (e.g., a scheduler or another agent) to inject

### 4.2 Meept's Approach (methods on the loop struct)

```go
// Meept (Go)
loop.Steer(ctx, "stop doing X, do Y instead", "tui")
loop.FollowUp(ctx, "also handle edge case Z", "dispatcher")
```

**Pros:**
- Any goroutine with a reference to the loop (or its queue) can inject messages
- Fits Go's concurrency model: goroutines communicate via shared memory with synchronization
- The dispatcher, TUI, HTTP API, scheduler, and other agents can all inject
- Thread-safe by design (mutex-protected queue)
- Can publish bus events on enqueue for observability

**Cons:**
- The agent loop now has mutable state beyond the conversation
- Requires careful lifecycle management (register/unregister with registry)
- Harder to test in isolation (need to mock or use a real queue)
- Potential for race conditions if the loop exits between enqueue and check (mitigated by registry cleanup)

### 4.3 Why Method-Based is Better for Meept

1. **Multi-agent architecture**: The dispatcher, scheduler, and other agents are separate goroutines that need to inject messages into running loops. Pi's callback approach requires all injection to flow through a single caller, which does not map to Meept's architecture.

2. **Go's concurrency model**: Go goroutines share memory explicitly (unlike JS workers which communicate via message passing). A mutex-protected slice is idiomatic Go. Channels would also work but make inspection (length, peek) harder.

3. **Bus integration**: Methods on the loop can publish bus events, enabling the TUI and other consumers to react to queue changes without polling.

4. **Service layer**: The RPC/HTTP layers can call `Steer()`/`FollowUp()` through the service layer, providing a clean API boundary.

---

## 5. Implementation Phases

### Phase 1: Core Queue Infrastructure

**Files to create:**
- `internal/agent/queue.go` -- `MessageQueue`, `QueuedMessage`, `QueueConfig`, `QueueStatus`, drain logic, bus event publishing

**Files to modify:**
- `internal/agent/loop.go` -- Add `queue *MessageQueue` field to `AgentLoop`, add `WithMessageQueue(q *MessageQueue) LoopOption`, modify `reasoningCycle()` to check steering before LLM call and follow-up before return, add `publishSteeringInjected()` and `publishFollowUpInjected()` helper methods

**Tests:**
- `internal/agent/queue_test.go` -- Unit tests for enqueue, drain, drain modes, concurrent access, status

**Risk:** Low. The queue is a standalone component. The loop changes are additive (if queue is nil, behavior is unchanged).

### Phase 2: Registry Integration

**Files to modify:**
- `internal/agent/registry.go` -- Add `activeQueues` map, `RegisterActiveQueue()`, `UnregisterActiveQueue()`, `GetActiveQueue()` methods

**Files to modify:**
- `internal/agent/loop.go` -- Register/unregister queue in `RunOnce()` (defer pattern)

**Tests:**
- Update existing registry tests for queue registration

**Risk:** Low. Map with RWMutex, standard pattern.

### Phase 3: Dispatcher Integration

**Files to modify:**
- `internal/agent/dispatcher.go` -- Modify `RouteToAgent()` to check for active queues, add `shouldSteer()` heuristic, inject via `Steer()` or `FollowUp()`
- `internal/agent/dispatcher.go` -- Add `SteerActiveAgent()` and `FollowUpActiveAgent()` public methods for direct use

**Files to modify:**
- `internal/agent/intent.go` (or wherever `IntentType` methods live) -- Add `IsSteerable()` method on `IntentType` for the routing heuristic

**Tests:**
- Unit tests for routing logic with active vs inactive queues

**Risk:** Medium. The dispatcher routing logic needs careful testing to avoid double-processing or message loss.

### Phase 4: Service Layer

**Files to modify:**
- `internal/services/chat_service.go` -- Add `Steer()`, `FollowUp()`, `GetQueueStatus()` methods
- `internal/services/registry.go` -- Register new service methods

**Files to modify:**
- `internal/rpc/dev.go` -- Add RPC handler methods for `chat.steer`, `chat.followup`, `chat.queue_status`

**Files to modify:**
- `internal/comm/http/api_handlers.go` -- Add HTTP endpoints `POST /api/v1/chat/steer`, `POST /api/v1/chat/followup`, `GET /api/v1/chat/queue/:conversation_id`

**Tests:**
- Service layer tests
- RPC integration tests

**Risk:** Low. Standard service/RPC/HTTP plumbing.

### Phase 5: TUI Integration

**Files to modify:**
- `internal/tui/rpc.go` -- Add `Steer()`, `FollowUp()`, `GetQueueStatus()` RPC client methods
- `internal/tui/events.go` -- Add `agent.queue.*` to default topics, handle queue events
- `internal/tui/models/chat.go` -- Add `agentActive`, `queueStatus`, `steerMode` fields, modify `Update()` to handle `QueueStatusMsg`, add queue indicator rendering
- `internal/tui/app.go` -- Wire up `ctrl+s` key binding for steer mode toggle, handle queue event messages in `Update()`
- `internal/tui/styles.go` -- Add styles for queue indicators (steer badge, follow-up badge, injected system message)

**Tests:**
- TUI model tests for queue state transitions

**Risk:** Medium. TUI state management needs careful handling to avoid flickering or stale indicators.

### Phase 6: Configuration

**Files to modify:**
- `internal/config/config.go` -- Add `QueueConfig` to `AgentConfig` or top-level config
- `config/meept.json5` -- Add default queue configuration

```json5
{
  agent: {
    queues: {
      steering_drain: "one",     // "one" or "all"
      followup_drain: "one",     // "one" or "all"
      max_steering: 10,          // max queued steering messages
      max_followup: 20,          // max queued follow-up messages
    },
  },
}
```

**Risk:** Low.

---

## 6. Open Questions

1. **Steering vs follow-up default**: Should the TUI default to steering or follow-up when the agent is active? Steering is more useful but also more disruptive. Consider making follow-up the default and requiring an explicit `ctrl+s` for steering.

2. **Queue depth limits**: What happens when queues are full? Options: reject with error, drop oldest, block. Recommend reject with error and a toast notification in the TUI.

3. **Cross-agent steering**: If agent A is running and the user's message should go to agent B, should we (a) queue it on agent A's follow-up, (b) abort agent A and start agent B, or (c) start agent B in parallel? Recommend (a) for now, with (b) as a future enhancement.

4. **Persistence**: Should queued messages survive a daemon restart? Probably not for the initial implementation -- queues are in-memory only. The user can re-send after restart.

5. **Token budget interaction**: Do steering/follow-up messages count against the conversation token budget? Yes -- they are added to the conversation like any other user message. The existing budget tracking handles this naturally.

6. **Max iterations interaction**: If follow-ups keep extending the session, we could hit max iterations. The existing `ErrMaxIterationsReached` handling is appropriate -- the agent will summarize and stop.

---

## 7. Testing Strategy

- **Unit tests for `MessageQueue`**: concurrent enqueue/drain, drain modes, overflow rejection
- **Unit tests for `reasoningCycle` modifications**: inject steering after tool batch, inject follow-up at natural stop, nil queue (no change to behavior)
- **Integration test**: dispatcher routes to active queue when loop is running, falls back to normal when idle
- **TUI test**: simulate agent-active state, verify queue indicators render correctly
- **Race detector**: run all tests with `-race` since queue access is concurrent

---

## 8. Success Criteria

- User can type a message while the agent is running and have it queued as a follow-up
- User can press `ctrl+s` to toggle steer mode and interrupt the agent mid-task
- Bus events are emitted for all queue operations
- TUI shows queue depth indicators and injected-message system messages
- No regressions in existing `RunOnce` behavior when queue is nil
- All tests pass with `-race`
