# Typed Agent Events & Hook System

> **Status:** tentative -- needs review
> **Date:** 2026-05-11
> **For Claude:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** Add a typed event system with type-safe hooks to the agent loop that coexists with the existing message bus. Typed events serve agent-internal lifecycle concerns (turn boundaries, tool execution, context transforms); the bus continues to serve system-wide pub/sub (daemon, scheduler, RPC, TUI).

**Architecture:** Bridge pattern -- an `EventEmitter` publishes typed events to in-process subscribers and also translates them into bus messages for system-wide consumers. Hooks are typed interfaces registered on the `AgentLoop` that can intercept and influence agent behavior (block tool calls, override results, transform context, force exit).

**Tech Stack:** Go 1.24, existing `internal/bus` (untouched), new `internal/agent/events.go` + `internal/agent/hooks.go`.

---

## 1. Problem Statement

Meept's event system is the message bus (`internal/bus/`) -- a generic pub/sub with string topics. Events are published as `bus.Event` (actually `models.BusMessage`) with a `json.RawMessage` payload. This is flexible but:

1. **Untyped:** Every consumer must `json.Unmarshal` the raw payload and type-assert. There is no compile-time guarantee that a payload matches a topic.
2. **Fire-and-forget:** `Publish()` returns an `int` (delivery count). Handlers cannot return values to influence the publisher.
3. **No lifecycle ordering:** Bus subscribers are unordered goroutines. There is no guarantee that, say, a metrics collector finishes before the next iteration begins.
4. **No hook system:** The agent loop cannot be influenced by external code. Tool calls, context transforms, and loop termination are hard-coded. Extensions like the security orchestrator are bolted in via direct field references rather than a general hook mechanism.

This matters because:
- The TUI, metrics collector, and shadow trainer all subscribe to `agent.progress` and independently unmarshal the same ad-hoc struct.
- The security orchestrator (`securityOrch`) is wired as a concrete field on `AgentLoop`. Adding a new interceptor (e.g., audit logging, cost gating) requires modifying `AgentLoop` itself.
- There is no way for an external component to say "transform the messages before this LLM call" or "override this tool result" without modifying `reasoningCycle`.

---

## 2. Current Architecture

### Message Bus (`internal/bus/bus.go`)

```go
type MessageBus struct {
    subscribers map[string][]*Subscriber  // topic -> subscribers
    bufferSize  int
    closed      bool
}

func (b *MessageBus) Publish(topic string, msg *models.BusMessage) int
func (b *MessageBus) Subscribe(id, topic string) *Subscriber
func (b *MessageBus) Request(ctx context.Context, topic string, msg *models.BusMessage) (*models.BusMessage, error)
```

`BusMessage` carries a `json.RawMessage` payload. There is no type safety between publisher and subscriber.

### Current Bus Topics (agent-internal)

| Topic | Publisher | Payload Shape |
|-------|-----------|---------------|
| `agent.progress` | `AgentLoop.reasoningCycle` | `{conversation_id, iteration, stage, detail, token_count, timestamp}` |
| `agent.action` | `AgentLoop.publishAction` | `{conversation_id, iteration, tool_calls: [{name, arguments}]}` |
| `agent.result` | `AgentLoop.publishResult` | `{conversation_id, iteration, results: [{tool_call_id, success, content}]}` |
| `llm.tokens.used` | `AgentLoop.publishTokenUsage` | `{conversation_id, total_tokens}` |
| `chat.response` | `ChatHandler` | `{reply, conversation_id, error}` |

### Agent Loop Structure (`internal/agent/loop.go`)

`reasoningCycle` is a `for` loop over iterations. Each iteration:
1. Publishes `agent.progress` with `stage="thinking"`
2. Builds messages, resolves model alias
3. Calls `chatWithFailover` (LLM call)
4. If tool calls: publishes `agent.action`, calls `executeToolCalls`, publishes `agent.result`, continues
5. If text response: publishes `agent.progress` with `stage="complete"`, returns

There are no hooks or interceptors. The security orchestrator is called directly before/after the LLM call. Shadow training captures are launched as fire-and-forget goroutines.

### The Gap

- **No typed events:** Consumers unmarshal ad-hoc structs from `json.RawMessage`.
- **No return-path hooks:** Nothing can block a tool call, override a result, or force loop exit from outside.
- **No settlement:** Bus publishes are async; the loop does not wait for subscribers to finish processing.
- **Tight coupling:** Adding a new cross-cutting concern (audit, cost gating, context rewriting) requires modifying `AgentLoop` fields and `reasoningCycle`.

---

## 3. Proposed Architecture

### Design Principles

1. **Coexistence, not replacement.** The bus remains the system-wide event backbone. Typed events are an agent-internal layer that bridges to the bus for system-wide subscribers.
2. **Type-safe events.** Each event is a Go struct. Subscribers receive the concrete type. No unmarshaling.
3. **Hooks return values.** Hook interfaces can block, override, or transform. The loop checks hook return values and acts on them.
4. **Settlement.** `WaitForIdle()` blocks until all async event listeners finish. The loop calls this at natural pause points.
5. **Clear naming convention.** Bus topics: `agent.*` (system-wide). Agent events: `AgentEventType` enum (agent-internal).

### Component Overview

```
internal/agent/
  events.go          -- AgentEventType enum, event structs, EventListener interface
  hooks.go           -- Hook interfaces, HookRegistry
  emitter.go         -- EventEmitter (typed pub/sub + bus bridge)
  emitter_test.go    -- Tests for emitter
  hooks_test.go      -- Tests for hook registry
  loop.go            -- Modified: uses EventEmitter and hooks in reasoningCycle
```

### Data Flow

```
reasoningCycle
    |
    +--> emitter.Emit(TurnStartEvent{})
    |       |
    |       +--> typed listeners (sync): hooks, metrics
    |       +--> bus bridge: Publish("agent.event.turn_start", busMsg)
    |
    +--> hookRegistry.RunBeforeToolCall(toolCall) --> BlockResult
    |       |
    |       +--> returns BlockResult{Block: true, Reason: "..."} --> skip tool
    |
    +--> executor.Execute(toolCall)
    |
    +--> hookRegistry.RunAfterToolCall(result) --> OverrideResult
    |       |
    |       +--> returns OverrideResult{Override: true, Result: newResult} --> use newResult
    |
    +--> emitter.Emit(ToolExecutionEndEvent{})
    |       +--> typed listeners (sync)
    |       +--> bus bridge: Publish("agent.event.tool_execution_end", busMsg)
    |
    +--> emitter.WaitForIdle()  // wait for async listeners
    |
    +--> hookRegistry.RunShouldStopAfterTurn(ctx) --> bool
            |
            +--> returns true --> break loop
```

---

## 4. Pros/Cons Analysis

### Pi Agent's Approach (typed events as PRIMARY communication)

Pi Agent uses typed events as the sole communication channel between the agent loop and everything else. The loop is a pure-function reducer: events go in, state comes out. Hooks (beforeToolCall, afterToolCall, etc.) are the mechanism for external influence.

**Advantages:**
- Single event system, no confusion about "which system to use"
- Pure-function loop is easy to test (events in, state out)
- Strong ordering guarantees since events drive the loop

**Disadvantages:**
- All system-wide communication must go through agent events, even daemon-level concerns (scheduler, RPC)
- Tight coupling between agent lifecycle and system infrastructure
- Harder to add system-wide subscribers that don't care about agent internals

### Meept's Approach (bus PRIMARY, typed events as ADDITION)

Meept keeps the bus as the system-wide backbone and adds typed events as an agent-internal layer.

**Advantages:**
- Bus continues to serve daemon, scheduler, RPC, TUI -- proven, works, no migration needed
- Typed events are scoped to agent concerns -- clear separation of audiences
- Hooks can be added incrementally without touching system-wide consumers
- The bridge pattern means existing bus subscribers (`agent.progress`, `agent.action`) continue to work unchanged during migration

**Disadvantages:**
- Two event systems could confuse developers
- Risk of duplication (publishing the same information in both systems)
- Slight overhead from the bridge (one extra publish per event)

### Why Coexistence Is Better for Meept

1. **Different audiences.** The bus has ~40 subscribers across daemon, scheduler, RPC, metrics, memory, tasks, skills. These don't need typed agent events. The agent loop has ~5 internal concerns (security, shadow, hallucination, metrics, context firewall). These need typed events and hooks.

2. **Incremental migration.** We can add typed events alongside existing bus topics. The bridge publishes to both. Existing subscribers keep working. New subscribers use typed events. Old bus topics can be deprecated over time.

3. **Bus serves inter-component communication.** The orchestrator, tactical scheduler, review manager, and escalation system all communicate via bus topics like `orchestrator.plan`, `task.amend.request`. These are not agent-lifecycle events and don't belong in a typed agent event system.

### Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Developer confusion about which system to use | Naming convention: bus topics for system-wide (`agent.progress`), agent events for loop internals (`AgentEventTurnStart`). Documentation in events.go package comment. |
| Duplication of events | Bridge pattern: typed events are the source of truth. Bus messages are derived from typed events. Never publish to bus directly from the loop for agent-lifecycle events. |
| Performance overhead | Bridge publishes are non-blocking (same as current bus.Publish). Typed listener dispatch is sync and fast (direct function calls, no serialization). |
| Hook ordering | Hooks are ordered by priority. Registry sorts by priority on registration. |

---

## 5. Event Type Definitions

All event types live in `internal/agent/events.go`.

### Event Type Enum

```go
// AgentEventType identifies the type of agent lifecycle event.
type AgentEventType string

const (
    // Session lifecycle
    AgentEventSessionStart      AgentEventType = "session_start"
    AgentEventSessionEnd        AgentEventType = "session_end"

    // Agent lifecycle
    AgentEventAgentStart        AgentEventType = "agent_start"
    AgentEventAgentEnd          AgentEventType = "agent_end"

    // Turn lifecycle
    AgentEventTurnStart         AgentEventType = "turn_start"
    AgentEventTurnEnd           AgentEventType = "turn_end"

    // Message lifecycle
    AgentEventMessageStart      AgentEventType = "message_start"
    AgentEventMessageUpdate     AgentEventType = "message_update"
    AgentEventMessageEnd        AgentEventType = "message_end"

    // Tool execution
    AgentEventToolExecutionStart  AgentEventType = "tool_execution_start"
    AgentEventToolExecutionUpdate AgentEventType = "tool_execution_update"
    AgentEventToolExecutionEnd    AgentEventType = "tool_execution_end"

    // Context management
    AgentEventSessionBeforeCompact AgentEventType = "session_before_compact"
    AgentEventSessionCompact       AgentEventType = "session_compact"
    AgentEventSessionBeforeTree    AgentEventType = "session_before_tree"
    AgentEventSessionTree          AgentEventType = "session_tree"

    // Provider interaction
    AgentEventBeforeProviderRequest  AgentEventType = "before_provider_request"
    AgentEventBeforeProviderPayload  AgentEventType = "before_provider_payload"
    AgentEventAfterProviderResponse  AgentEventType = "after_provider_response"

    // Model selection
    AgentEventModelSelect         AgentEventType = "model_select"
    AgentEventThinkingLevelSelect AgentEventType = "thinking_level_select"

    // Resource updates
    AgentEventResourcesUpdate     AgentEventType = "resources_update"
    AgentEventQueueUpdate         AgentEventType = "queue_update"

    // Checkpointing
    AgentEventSavePoint           AgentEventType = "save_point"
    AgentEventAbort               AgentEventType = "abort"
    AgentEventSettled             AgentEventType = "settled"
)
```

### Event Structs

```go
// AgentEvent is the envelope for all typed agent events.
// Type is the discriminating field. Data holds the event-specific payload.
type AgentEvent struct {
    Type          AgentEventType
    Timestamp     time.Time
    AgentID       string
    ConversationID string
    Iteration     int
    Data          AgentEventData // interface, concrete type depends on Type
}

// AgentEventData is the interface all event payloads implement.
type AgentEventData interface {
    agentEventData() // seal the interface
}

// --- Session Events ---

type SessionStartData struct {
    SessionID  string
    Input      string
    AgentSpec  string // agent type/role
}

type SessionEndData struct {
    SessionID     string
    Outcome       string // "success", "error", "cancelled", "max_iterations"
    Duration      time.Duration
    TotalTokens   int
    TotalIter     int
    Error         string
}

// --- Agent Lifecycle Events ---

type AgentStartData struct {
    AgentID   string
    AgentType string // "chat", "coder", "debugger", etc.
    ModelRef  string
}

type AgentEndData struct {
    AgentID  string
    Reason   string
    Duration time.Duration
}

// --- Turn Events ---

type TurnStartData struct {
    TurnNumber     int
    TotalTokensSoFar int
    MessagesCount  int
    ToolCount      int
}

type TurnEndData struct {
    TurnNumber       int
    HadToolCalls     bool
    ToolCallCount    int
    ResponseTokens   int
    StoppedBy        string // "", "max_iterations", "convergence", "budget", "hook"
}

// --- Message Events ---

type MessageStartData struct {
    Role      string
    TokenCount int
}

type MessageUpdateData struct {
    Role      string
    Delta     string // partial content delta (streaming)
    TokenCount int
}

type MessageEndData struct {
    Role      string
    Content   string
    TokenCount int
    ToolCalls int // number of tool calls in this message
}

// --- Tool Execution Events ---

type ToolExecutionStartData struct {
    ToolCallID string
    ToolName   string
    Arguments  string // raw JSON
}

type ToolExecutionUpdateData struct {
    ToolCallID string
    ToolName   string
    Status     string // "running", "waiting", "permission_check"
    Detail     string
}

type ToolExecutionEndData struct {
    ToolCallID string
    ToolName   string
    Success    bool
    Result     string
    Error      string
    Cached     bool
    Duration   time.Duration
    Blocked    bool   // true if blocked by a BeforeToolCallHook
    BlockReason string
}

// --- Context Management Events ---

type SessionBeforeCompactData struct {
    MessageCount int
    TokenCount   int
    Reason       string // "budget", "limit", "manual"
}

type SessionCompactData struct {
    MessageCountBefore int
    MessageCountAfter  int
    TokensSaved        int
    Method             string // "truncate", "summarize", "hierarchical"
}

type SessionBeforeTreeData struct {
    NodeCount int
    Depth     int
}

type SessionTreeData struct {
    Nodes     int
    Depth     int
    TokenSize int
}

// --- Provider Interaction Events ---

type BeforeProviderRequestData struct {
    ModelID   string
    Messages  []llm.ChatMessage
    Tools     []llm.ToolDefinition
    TokenCount int
}

type BeforeProviderPayloadData struct {
    ModelID  string
    Payload  string // serialized request body
    Endpoint string
}

type AfterProviderResponseData struct {
    ModelID     string
    StatusCode  int
    ResponseTokens int
    Latency     time.Duration
    Cached      bool
    Error       string
}

// --- Model Selection Events ---

type ModelSelectData struct {
    Alias    string
    ModelID  string
    Provider string
    Reason   string // "alias_resolution", "rotation", "manual"
}

type ThinkingLevelSelectData struct {
    Level  string // "none", "low", "medium", "high"
    Reason string
}

// --- Resource Events ---

type ResourcesUpdateData struct {
    TokensUsed     int
    TokensBudget   int
    IterationsUsed int
    IterationsMax  int
}

type QueueUpdateData struct {
    QueueDepth   int
    ActiveJobs   int
    CompletedJobs int
}

// --- Checkpoint Events ---

type SavePointData struct {
    Reason string
    State  map[string]any // serializable loop state snapshot
}

type AbortData struct {
    Reason string
    Iteration int
}

type SettledData struct {
    ListenerCount int
    Duration      time.Duration
}
```

---

## 6. Hook Interface Definitions

All hook interfaces live in `internal/agent/hooks.go`.

### Core Hooks

```go
// BeforeToolCallHook is called before a tool is executed.
// Return BlockResult with Block=true to prevent execution.
type BeforeToolCallHook interface {
    BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) BlockResult
}

// BlockResult is returned by BeforeToolCallHook.
type BlockResult struct {
    Block  bool
    Reason string // human-readable reason if blocked
}

// AfterToolCallHook is called after a tool executes.
// Return OverrideResult to replace the tool's output.
type AfterToolCallHook interface {
    AfterToolCall(ctx context.Context, toolCall llm.ToolCall, result *ExecutionResult) OverrideResult
}

// OverrideResult is returned by AfterToolCallHook.
type OverrideResult struct {
    Override bool
    Result   *ExecutionResult // replacement result (if Override=true)
    Reason   string
}

// PrepareNextTurnHook is called between turns.
// It can swap the context (messages), model, or inference parameters.
type PrepareNextTurnHook interface {
    PrepareNextTurn(ctx context.Context, state TurnState) TurnModification
}

// TurnState provides read-only access to the current turn state.
type TurnState struct {
    ConversationID string
    Iteration      int
    Messages       []llm.ChatMessage
    ModelRef       string
    TotalTokens    int
    LastResponse   string
}

// TurnModification requests changes to the next turn.
type TurnModification struct {
    Modified       bool
    ExtraMessages  []llm.ChatMessage // prepended before next LLM call
    ModelOverride  string            // if non-empty, switch model for next call
    SkipTools      bool              // if true, don't send tool definitions next call
    Reason         string
}

// ShouldStopAfterTurnHook is called after each turn.
// Return true to force the loop to exit.
type ShouldStopAfterTurnHook interface {
    ShouldStopAfterTurn(ctx context.Context, state TurnState) StopDecision
}

// StopDecision is returned by ShouldStopAfterTurnHook.
type StopDecision struct {
    Stop   bool
    Reason string // used in the "wrap up" message
}

// TransformContextHook is called before each LLM call.
// It can modify the messages sent to the LLM.
type TransformContextHook interface {
    TransformContext(ctx context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform
}

// ContextTransform is returned by TransformContextHook.
type ContextTransform struct {
    Messages     []llm.ChatMessage // replacement messages
    ToolDefs     []llm.ToolDefinition // replacement tool definitions (nil = keep original)
    Modified     bool
    Reason       string
}
```

### Hook Registry

```go
// HookPriority defines ordering for hook execution.
// Lower values run first.
type HookPriority int

const (
    HookPriorityCritical HookPriority = 0  // security, must run first
    HookPriorityHigh     HookPriority = 10 // audit, cost gating
    HookPriorityNormal   HookPriority = 50 // default
    HookPriorityLow      HookPriority = 90 // logging, metrics
    HookPriorityMonitor  HookPriority = 100 // monitoring, shadow training
)

// HookRegistration holds a registered hook with its priority.
type HookRegistration struct {
    Name     string
    Priority HookPriority
    Hook     any // concrete hook interface
}

// HookRegistry manages all agent hooks.
type HookRegistry struct {
    mu                sync.RWMutex
    beforeToolCalls   []HookRegistration
    afterToolCalls    []HookRegistration
    prepareNextTurns  []HookRegistration
    shouldStopAfter   []HookRegistration
    transformContexts []HookRegistration
    logger            *slog.Logger
}

func NewHookRegistry(logger *slog.Logger) *HookRegistry

// Registration methods -- each sorts by priority after insertion.
func (r *HookRegistry) RegisterBeforeToolCall(name string, priority HookPriority, hook BeforeToolCallHook)
func (r *HookRegistry) RegisterAfterToolCall(name string, priority HookPriority, hook AfterToolCallHook)
func (r *HookRegistry) RegisterPrepareNextTurn(name string, priority HookPriority, hook PrepareNextTurnHook)
func (r *HookRegistry) RegisterShouldStopAfterTurn(name string, priority HookPriority, hook ShouldStopAfterTurnHook)
func (r *HookRegistry) RegisterTransformContext(name string, priority HookPriority, hook TransformContextHook)

// Unregistration
func (r *HookRegistry) Unregister(name string)

// Execution methods -- run all hooks of a given type in priority order.
// Short-circuit on first block/override/stop/modify.
func (r *HookRegistry) RunBeforeToolCalls(ctx context.Context, toolCall llm.ToolCall) BlockResult
func (r *HookRegistry) RunAfterToolCalls(ctx context.Context, toolCall llm.ToolCall, result *ExecutionResult) OverrideResult
func (r *HookRegistry) RunPrepareNextTurn(ctx context.Context, state TurnState) TurnModification
func (r *HookRegistry) RunShouldStopAfterTurn(ctx context.Context, state TurnState) StopDecision
func (r *HookRegistry) RunTransformContext(ctx context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform
```

### Short-Circuit Semantics

Hook execution is ordered by priority (lowest first). For hooks that can short-circuit:

| Hook | Short-circuit on |
|------|-----------------|
| `BeforeToolCall` | First `BlockResult{Block: true}` -- remaining hooks skipped |
| `AfterToolCall` | First `OverrideResult{Override: true}` -- remaining hooks skipped |
| `ShouldStopAfterTurn` | First `StopDecision{Stop: true}` -- remaining hooks skipped |
| `PrepareNextTurn` | First `TurnModification{Modified: true}` -- remaining hooks skipped |
| `TransformContext` | First `ContextTransform{Modified: true}` -- remaining hooks skipped |

---

## 7. Implementation Phases

### Phase 1: Event Types and Event Emitter (foundation, no behavior change)

**Goal:** Define all event types and build the EventEmitter with bus bridge. No changes to the agent loop yet.

**Files to create:**
- `internal/agent/events.go` -- AgentEventType enum, all event data structs, AgentEvent envelope
- `internal/agent/emitter.go` -- EventEmitter with typed listener registration and bus bridge
- `internal/agent/emitter_test.go` -- tests for EventEmitter

**Files to modify:**
- None (pure addition)

**emitter.go API:**

```go
// EventListener is a callback for typed agent events.
type EventListener func(ctx context.Context, event AgentEvent)

// EventEmitter publishes typed agent events to in-process listeners
// and bridges them to the message bus for system-wide subscribers.
type EventEmitter struct {
    mu         sync.RWMutex
    listeners  map[AgentEventType][]listenerEntry // event type -> ordered listeners
    allListeners []listenerEntry                   // receives all events
    bus        *bus.MessageBus
    agentID    string
    logger     *slog.Logger

    // Settlement tracking
    pending    sync.WaitGroup
    settled    bool
}

type listenerEntry struct {
    name     string
    callback EventListener
    async    bool // if true, runs in goroutine (tracked for settlement)
}

func NewEventEmitter(agentID string, bus *bus.MessageBus, logger *slog.Logger) *EventEmitter

// On registers a listener for a specific event type.
func (e *EventEmitter) On(eventType AgentEventType, name string, listener EventListener)

// OnAsync registers an async listener (runs in a goroutine, tracked for settlement).
func (e *EventEmitter) OnAsync(eventType AgentEventType, name string, listener EventListener)

// OnAll registers a listener that receives all events.
func (e *EventEmitter) OnAll(name string, listener EventListener)

// Emit publishes a typed event to all matching listeners and bridges to the bus.
// Sync listeners run inline. Async listeners run in goroutines.
func (e *EventEmitter) Emit(ctx context.Context, eventType AgentEventType, data AgentEventData)

// EmitWithFields publishes an event with explicit metadata fields.
func (e *EventEmitter) EmitWithFields(ctx context.Context, event AgentEvent)

// WaitForIdle blocks until all async listeners from the most recent Emit calls
// have finished processing. Called at natural pause points in the loop.
func (e *EventEmitter) WaitForIdle(ctx context.Context) error

// Off removes a listener by name.
func (e *EventEmitter) Off(name string)

// BusTopic returns the bus topic for a given agent event type.
// Convention: "agent.event.<type>"
func BusTopic(eventType AgentEventType) string
```

**Bridge pattern details:**

The `Emit` method:
1. Creates an `AgentEvent` envelope.
2. Dispatches to sync listeners (direct function call).
3. Dispatches to async listeners (goroutine, increments `pending` WaitGroup).
4. If bus is non-nil, serializes the event to `json.RawMessage` and calls `bus.Publish(BusTopic(eventType), busMsg)`.
5. Does NOT block on bus delivery (same semantics as current `Publish`).

Bus topic mapping: `AgentEventTurnStart` -> `"agent.event.turn_start"`. The full event struct (including `Type` discriminator) is the bus payload, so bus subscribers can unmarshal once and switch on `Type`.

**Tests:**
- Emit with no listeners -- no panic
- Emit with sync listener -- called inline
- Emit with async listener -- called in goroutine, WaitForIdle blocks until done
- Emit with bus bridge -- bus receives message with correct topic and payload
- Emit with nil bus -- no panic, only typed listeners called
- WaitForIdle with no pending -- returns immediately
- Off removes listener by name

### Phase 2: Hook Registry (foundation, no behavior change)

**Goal:** Define hook interfaces and build the HookRegistry. No changes to the agent loop yet.

**Files to create:**
- `internal/agent/hooks.go` -- all hook interfaces, result types, HookRegistry
- `internal/agent/hooks_test.go` -- tests for HookRegistry

**Files to modify:**
- None (pure addition)

**Tests:**
- Register and run single BeforeToolCall hook -- receives call, returns result
- Register multiple hooks with different priorities -- run in priority order
- Short-circuit on Block=true -- remaining hooks not called
- Register and run AfterToolCall hook with override
- Register and run TransformContext hook
- Unregister removes hook by name
- Empty registry -- all Run methods return zero-value results

### Phase 3: Wire EventEmitter into AgentLoop (incremental, backward compatible)

**Goal:** Add EventEmitter and HookRegistry to AgentLoop. Begin emitting typed events alongside existing bus publishes. No existing bus topics removed.

**Files to modify:**
- `internal/agent/loop.go` -- add `emitter` and `hooks` fields, emit events in reasoningCycle
- `internal/agent/loop_test.go` -- test event emission

**Changes to AgentLoop struct:**

```go
type AgentLoop struct {
    // ... existing fields ...

    // Typed event system (Phase 3)
    emitter *EventEmitter
    hooks   *HookRegistry
}
```

**New LoopOptions:**

```go
func WithEventEmitter(emitter *EventEmitter) LoopOption
func WithHookRegistry(hooks *HookRegistry) LoopOption
```

**Changes to reasoningCycle:**

The loop emits typed events at each lifecycle point. Existing `publishProgress`, `publishAction`, `publishResult`, `publishTokenUsage` continue to work (backward compat). New typed events are emitted in addition.

```
Before loop:
    emitter.Emit(AgentEventAgentStart, AgentStartData{...})

Each iteration:
    emitter.Emit(AgentEventTurnStart, TurnStartData{...})

    Before LLM call:
        emitter.Emit(AgentEventBeforeProviderRequest, BeforeProviderRequestData{...})
        hooks.RunTransformContext(ctx, messages, toolDefs)  // <-- NEW: can modify messages
        emitter.Emit(AgentEventBeforeProviderPayload, ...)

    After LLM response:
        emitter.Emit(AgentEventAfterProviderResponse, ...)

    If tool calls:
        For each tool call:
            blockResult := hooks.RunBeforeToolCalls(ctx, toolCall)  // <-- NEW: can block
            if blockResult.Block:
                emit ToolExecutionEnd with Blocked=true
                continue
            emitter.Emit(AgentEventToolExecutionStart, ...)
            result := executor.Execute(ctx, toolCall)
            overrideResult := hooks.RunAfterToolCalls(ctx, toolCall, result)  // <-- NEW: can override
            if overrideResult.Override:
                result = overrideResult.Result
            emitter.Emit(AgentEventToolExecutionEnd, ...)

    Before context truncation:
        emitter.Emit(AgentEventSessionBeforeCompact, ...)

    After context truncation:
        emitter.Emit(AgentEventSessionCompact, ...)

    End of iteration:
        stopDecision := hooks.RunShouldStopAfterTurn(ctx, state)  // <-- NEW: can force exit
        if stopDecision.Stop:
            break

    emitter.Emit(AgentEventTurnEnd, ...)

    emitter.WaitForIdle(ctx)  // <-- NEW: settle async listeners

After loop:
    emitter.Emit(AgentEventAgentEnd, AgentEndData{...})
    emitter.Emit(AgentEventSessionEnd, SessionEndData{...})
```

**Backward compatibility:**
- `publishProgress`, `publishAction`, `publishResult`, `publishTokenUsage` remain and continue to publish to bus
- The emitter's bus bridge publishes to NEW topics (`agent.event.*`), not existing topics
- Existing bus subscribers (`agent.progress`, `agent.action`, etc.) continue to work
- Over time, bus subscribers can be migrated to typed events and old bus topics deprecated

### Phase 4: Migrate Security Orchestrator to Hooks

**Goal:** Move the security orchestrator from a direct field reference to a hook implementation. This proves the hook system works for real cross-cutting concerns.

**Files to create:**
- `internal/security/agent_hooks.go` -- security hook implementations

**Files to modify:**
- `internal/agent/loop.go` -- remove direct `securityOrch` calls from reasoningCycle (keep field for backward compat, but hooks are primary)
- `internal/daemon/components.go` -- register security hooks on agent loop

**Hook implementations:**

```go
// SecurityBeforeToolCall implements BeforeToolCallHook.
// Runs Tirith scan on shell commands before execution.
type SecurityBeforeToolCall struct {
    orchestrator *Orchestrator
}

func (s *SecurityBeforeToolCall) BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) BlockResult {
    if toolCall.Function.Name != "shell" {
        return BlockResult{}
    }
    // Run Tirith scan
    blocked, reason := s.orchestrator.ScanToolCall(toolCall.Function.Arguments)
    if blocked {
        return BlockResult{Block: true, Reason: reason}
    }
    return BlockResult{}
}

// SecurityTransformContext implements TransformContextHook.
// Runs input sanitization on user messages.
type SecurityTransformContext struct {
    orchestrator *Orchestrator
}

func (s *SecurityTransformContext) TransformContext(ctx context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform {
    // Sanitize user messages
    for i, msg := range messages {
        if msg.Role == llm.RoleUser {
            cleaned, blocked, _ := s.orchestrator.SanitizeInput(msg.Content)
            if blocked {
                return ContextTransform{
                    Messages: []llm.ChatMessage{{
                        Role:    llm.RoleAssistant,
                        Content: "I cannot process that request due to security concerns.",
                    }},
                    Modified: true,
                    Reason:   "input blocked by security",
                }
            }
            messages[i].Content = cleaned
        }
    }
    return ContextTransform{Messages: messages, Modified: true, Reason: "security sanitization"}
}
```

### Phase 5: Migrate Existing Bus Publishers to Emitter Bridge

**Goal:** Replace direct `bus.Publish` calls in the agent loop with `emitter.Emit` calls. The emitter's bus bridge handles forwarding to bus topics. This centralizes event publication.

**Files to modify:**
- `internal/agent/loop.go` -- replace `publishProgress`, `publishAction`, `publishResult`, `publishTokenUsage` with emitter calls
- `internal/agent/handler.go` -- replace direct bus publishes with emitter calls (or keep as-is if handler is outside agent scope)

**Migration strategy:**
1. The emitter bridge publishes to both new topics (`agent.event.*`) AND legacy topics (`agent.progress`, `agent.action`, etc.) during transition.
2. Add a `LegacyTopicMap` to the emitter that maps event types to legacy bus topics.
3. Once all subscribers are migrated to typed events, remove the legacy topic map.

```go
// In emitter.go
var legacyTopicMap = map[AgentEventType]string{
    AgentEventTurnStart:            "agent.progress",  // stage="thinking"
    AgentEventToolExecutionStart:   "agent.action",
    AgentEventToolExecutionEnd:     "agent.result",
    AgentEventAfterProviderResponse: "llm.tokens.used",
}
```

### Phase 6: Settlement and Observability

**Goal:** Add `WaitForIdle` calls at natural pause points in the loop. Wire up metrics and TUI to typed events.

**Files to modify:**
- `internal/agent/loop.go` -- add `emitter.WaitForIdle()` after tool execution and after LLM response
- `internal/metrics/collector.go` -- subscribe to typed events instead of bus topics
- `internal/tui/viz/canvas.go` -- subscribe to typed events for progress rendering

**Settlement points in reasoningCycle:**

```
After tool execution (before continuing loop):
    emitter.WaitForIdle(ctx)

After LLM response (before deciding tool vs. text):
    emitter.WaitForIdle(ctx)

At loop exit (before return):
    emitter.WaitForIdle(ctx)
```

`WaitForIdle` uses a `sync.WaitGroup` that is incremented for each async listener invocation and decremented when the listener goroutine completes. If the context expires before settlement, it returns `ctx.Err()`.

---

## 8. Bridge Pattern Details

### Event-to-Bus Translation

The `EventEmitter.Emit` method translates typed events to bus messages:

```go
func (e *EventEmitter) Emit(ctx context.Context, eventType AgentEventType, data AgentEventData) {
    event := AgentEvent{
        Type:          eventType,
        Timestamp:     time.Now().UTC(),
        AgentID:       e.agentID,
        ConversationID: ctx.Value(convIDKey).(string), // extracted from context
        Data:          data,
    }

    // 1. Dispatch to typed listeners (sync)
    e.mu.RLock()
    listeners := e.listeners[eventType]
    allListeners := e.allListeners
    e.mu.RUnlock()

    for _, entry := range listeners {
        if entry.async {
            e.pending.Add(1)
            go func(cb EventListener) {
                defer e.pending.Done()
                cb(ctx, event)
            }(entry.callback)
        } else {
            entry.callback(ctx, event)
        }
    }

    for _, entry := range allListeners {
        if entry.async {
            e.pending.Add(1)
            go func(cb EventListener) {
                defer e.pending.Done()
                cb(ctx, event)
            }(entry.callback)
        } else {
            entry.callback(ctx, event)
        }
    }

    // 2. Bridge to bus
    if e.bus != nil {
        payload, err := json.Marshal(event)
        if err != nil {
            e.logger.Warn("Failed to marshal event for bus bridge", "error", err, "type", eventType)
            return
        }
        msg := &models.BusMessage{
            ID:        generateID(),
            Type:      models.MessageTypeEvent,
            Source:    "agent:" + e.agentID,
            Timestamp: event.Timestamp,
            Payload:   payload,
        }

        topic := BusTopic(eventType)
        delivered := e.bus.Publish(topic, msg)

        // Legacy topic bridge (Phase 5)
        if legacyTopic, ok := legacyTopicMap[eventType]; ok {
            e.bus.Publish(legacyTopic, msg)
        }

        if delivered == 0 {
            e.logger.Debug("Event published to bus (no subscribers)", "topic", topic)
        }
    }
}
```

### Bus Topic Convention

| Event Type | Bus Topic |
|-----------|-----------|
| `agent_start` | `agent.event.agent_start` |
| `turn_start` | `agent.event.turn_start` |
| `tool_execution_start` | `agent.event.tool_execution_start` |
| `tool_execution_end` | `agent.event.tool_execution_end` |
| `after_provider_response` | `agent.event.after_provider_response` |
| `session_end` | `agent.event.session_end` |
| (all others) | `agent.event.<type>` |

### Backward Compatibility

During migration (Phases 3-5), the emitter publishes to BOTH new typed topics and legacy topics. A subscriber that listens on `agent.progress` continues to receive events. A new subscriber can listen on `agent.event.turn_start` and get the typed struct.

After all subscribers are migrated (future work), the legacy topic map is removed and the old `publishProgress`/`publishAction`/`publishResult` methods are deleted.

---

## Summary of Files

| File | Action | Phase |
|------|--------|-------|
| `internal/agent/events.go` | Create | 1 |
| `internal/agent/emitter.go` | Create | 1 |
| `internal/agent/emitter_test.go` | Create | 1 |
| `internal/agent/hooks.go` | Create | 2 |
| `internal/agent/hooks_test.go` | Create | 2 |
| `internal/agent/loop.go` | Modify (add emitter, hooks, emit calls) | 3, 5, 6 |
| `internal/agent/loop_test.go` | Modify (test event emission) | 3 |
| `internal/security/agent_hooks.go` | Create | 4 |
| `internal/daemon/components.go` | Modify (register security hooks) | 4 |
| `internal/metrics/collector.go` | Modify (typed event subscriber) | 6 |
| `internal/tui/viz/canvas.go` | Modify (typed event subscriber) | 6 |

**Total new files:** 6
**Total modified files:** 5
**Estimated phases:** 6 (each independently testable)
