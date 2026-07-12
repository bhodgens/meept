# Plan: Agent State Machine Formalization

**Status:** Proposed
**Created:** 2026-07-10
**Priority:** Medium
**Risk:** Low (additive changes, no breaking changes to existing flow)

---

## Summary

Formalize Meept's agent loop state management by introducing an explicit state machine with well-defined states, transitions, and observable hooks. This improves:

1. **Observability** - Clear visibility into agent state at any point
2. **Debugging** - Easier to trace state transitions when issues occur
3. **Escalation logic** - State-aware error handling and recovery
4. **Testing** - State-based testing for edge cases
5. **User feedback** - More accurate progress reporting to users

---

## Current State Analysis

### Existing State Patterns

**Location:** Various files with ad-hoc state tracking

```go
// conversation.go: TurnBudgetTracker has implicit state
type TurnBudgetTracker struct {
    warningZone     bool //隐式状态: budget nearly depleted
    wrapUpRequested bool //隐式状态：must wrap up
}

// loop.go: reasoningCycle uses iteration-based state
for iteration := 1; iteration <= l.config.MaxIterations; iteration++ {
    // State is implicit in the iteration number and local variables
    inWarningZone := false  // Local state flag
    hadToolCalls = false    // Local state flag
}

// watchdog.go: WorkerState tracks agent execution state
type WorkerState struct {
    LastUpdate   time.Time
    Iteration    int
    Stage        string  // "thinking", "executing", "completed"
    conversation string
}

// hooks.go: TurnState captures per-turn data
type TurnState struct {
    TurnID       string
    Request      llm.ChatCompletionRequest
    Response     *llm.ChatCompletionResponse
    ToolCalls    []llm.ToolCall
    ToolResults  []*ExecutionResult
    TokensUsed   int
    Duration     time.Duration
}
```

### Strengths
- ✅ `Watchdog` tracks high-level execution stages
- ✅ `TurnBudgetTracker` has explicit budget state flags
- ✅ `TurnState` captures structured per-turn data

### Gaps
- ❌ No unified agent state machine - state is scattered across components
- ❌ State transitions are implicit (no explicit `Transition()` calls)
- ❌ No state validation (can't verify legal transitions)
- ❌ Limited observability - external systems can't easily query agent state
- ❌ No state persistence - can't resume from specific states after restart

---

## Objectives

| Objective | Success Metric |
|-----------|----------------|
| **O1: Explicit State Machine** | All agent states and transitions are formally defined |
| **O2: State Observability** | External systems can query current agent state via API/hooks |
| **O3: State-Aware Error Handling** | Error recovery logic uses state context |
| **O4: Transition Logging** | All state transitions are logged for auditing |
| **O5: Backward Compatibility** | Existing code continues to work without changes |

---

## Implementation Phases

### Phase 1: Define State Machine Types

**Goal:** Create the core state machine types and definitions.

#### 1.1: Define AgentState Enum

**File:** `internal/agent/agent_state.go` (new)

```go
package agent

// AgentState represents the current state of an agent loop.
type AgentState uint8

const (
    // StateIdle: Agent is waiting for input (initial state)
    StateIdle AgentState = iota

    // StateReceivingInput: Agent is receiving user input
    StateReceivingInput

    // StateClassifying: Dispatcher is classifying intent
    StateClassifying

    // StateThinking: Agent is processing with LLM (before tool calls)
    StateThinking

    // StateToolExecuting: Agent is executing tool calls
    StateToolExecuting

    // StateToolWaiting: Agent is waiting for external tool result (e.g., HTTP, shell)
    StateToolWaiting

    // StateProcessingResult: Agent is processing tool results
    StateProcessingResult

    // StateGeneratingResponse: Agent is generating final response
    StateGeneratingResponse

    // StateBlocked: Agent is blocked (awaiting approval, rate limited, etc.)
    StateBlocked

    // StateError: Agent encountered an error
    StateError

    // StateCompleted: Agent completed the task successfully
    StateCompleted

    // StateCancelled: Agent was cancelled by user or system
    StateCancelled

    // StateMaxIterations: Agent hit max iteration limit
    StateMaxIterations

    // StateBudgetExhausted: Agent exhausted token budget
    StateBudgetExhausted
)

// String returns a human-readable name for the state.
func (s AgentState) String() string {
    switch s {
    case StateIdle:
        return "idle"
    case StateReceivingInput:
        return "receiving_input"
    case StateClassifying:
        return "classifying"
    case StateThinking:
        return "thinking"
    case StateToolExecuting:
        return "tool_executing"
    case StateToolWaiting:
        return "tool_waiting"
    case StateProcessingResult:
        return "processing_result"
    case StateGeneratingResponse:
        return "generating_response"
    case StateBlocked:
        return "blocked"
    case StateError:
        return "error"
    case StateCompleted:
        return "completed"
    case StateCancelled:
        return "cancelled"
    case StateMaxIterations:
        return "max_iterations"
    case StateBudgetExhausted:
        return "budget_exhausted"
    default:
        return "unknown"
    }
}

// IsTerminal returns true if the state is a terminal state (no further transitions except Reset).
func (s AgentState) IsTerminal() bool {
    switch s {
    case StateCompleted, StateError, StateCancelled, StateMaxIterations, StateBudgetExhausted:
        return true
    default:
        return false
    }
}

// IsActive returns true if the state indicates active processing.
func (s AgentState) IsActive() bool {
    switch s {
    case StateThinking, StateToolExecuting, StateToolWaiting, StateProcessingResult, StateGeneratingResponse:
        return true
    default:
        return false
    }
}
```

#### 1.2: Define State Transition Type

```go
// StateTransition represents a state change event.
type StateTransition struct {
    // From is the previous state.
    From AgentState `json:"from"`
    // To is the new state.
    To AgentState `json:"to"`
    // Reason is the reason for the transition (e.g., "tool_call_received", "error_occurred")
    Reason string `json:"reason"`
    // Metadata is optional key-value data associated with the transition.
    Metadata map[string]any `json:"metadata,omitempty"`
    // Timestamp is when the transition occurred.
    Timestamp time.Time `json:"timestamp"`
    // TurnID is the current turn/iteration number.
    TurnID int `json:"turn_id,omitempty"`
}

// StateTransitionListener is a callback for state transitions.
type StateTransitionListener func(transition StateTransition)
```

#### 1.3: Define AgentStateMachine

```go
// AgentStateMachine manages agent state transitions.
type AgentStateMachine struct {
    mu          sync.RWMutex
    currentState AgentState
    history      []StateTransition
    maxHistory   int
    listeners    []StateTransitionListener
    logger       *slog.Logger
}

// NewAgentStateMachine creates a new state machine.
func NewAgentStateMachine(logger *slog.Logger) *AgentStateMachine {
    return &AgentStateMachine{
        currentState: StateIdle,
        history:      make([]StateTransition, 0),
        maxHistory:   100,
        listeners:    make([]StateTransitionListener, 0),
        logger:       logger,
    }
}

// CurrentState returns the current state.
func (m *AgentStateMachine) CurrentState() AgentState {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.currentState
}

// Transition performs a state transition with validation.
func (m *AgentStateMachine) Transition(to AgentState, reason string, metadata map[string]any) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    from := m.currentState

    // Validate transition
    if !m.isValidTransition(from, to) {
        return fmt.Errorf("invalid state transition: %s -> %s (reason: %s)", from, to, reason)
    }

    // Don't log redundant same-state transitions
    if from == to {
        return nil
    }

    transition := StateTransition{
        From:      from,
        To:        to,
        Reason:    reason,
        Metadata:  metadata,
        Timestamp: time.Now(),
    }

    m.currentState = to
    m.history = append(m.history, transition)

    // Trim history
    if len(m.history) > m.maxHistory {
        m.history = m.history[len(m.history)-m.maxHistory:]
    }

    m.logger.Debug("State transition",
        "from", from,
        "to", to,
        "reason", reason,
        "metadata", metadata)

    // Notify listeners
    for _, listener := range m.listeners {
        go listener(transition)
    }

    return nil
}

// isValidTransition returns true if the transition is allowed.
func (m *AgentStateMachine) isValidTransition(from, to AgentState) bool {
    // Define allowed transitions
    allowedTransitions := map[AgentState][]AgentState{
        StateIdle:              {StateReceivingInput, StateCancelled},
        StateReceivingInput:    {StateClassifying, StateCancelled},
        StateClassifying:       {StateThinking, StateError, StateCancelled},
        StateThinking:          {StateToolExecuting, StateGeneratingResponse, StateError, StateCancelled, StateMaxIterations, StateBudgetExhausted},
        StateToolExecuting:     {StateToolWaiting, StateProcessingResult, StateError, StateBlocked},
        StateToolWaiting:       {StateProcessingResult, StateError, StateBlocked},
        StateProcessingResult:  {StateThinking, StateCompleted, StateError, StateMaxIterations},
        StateGeneratingResponse: {StateCompleted, StateError},
        StateBlocked:           {StateThinking, StateToolExecuting, StateCancelled, StateError},
        StateError:             {StateIdle, StateCancelled}, // Reset or cancel
        StateCompleted:         {StateIdle},                 // Reset
        StateCancelled:         {StateIdle},                 // Reset
        StateMaxIterations:     {StateIdle},                 // Reset
        StateBudgetExhausted:   {StateIdle},                 // Reset
    }

    allowed, ok := allowedTransitions[from]
    if !ok {
        return false
    }

    for _, s := range allowed {
        if s == to {
            return true
        }
    }

    return false
}

// OnTransition registers a listener for state transitions.
func (m *AgentStateMachine) OnTransition(listener StateTransitionListener) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.listeners = append(m.listeners, listener)
}

// History returns recent state transitions.
func (m *AgentStateMachine) History() []StateTransition {
    m.mu.RLock()
    defer m.mu.RUnlock()
    result := make([]StateTransition, len(m.history))
    copy(result, m.history)
    return result
}

// IsTerminal returns true if the current state is terminal.
func (m *AgentStateMachine) IsTerminal() bool {
    return m.CurrentState().IsTerminal()
}

// IsActive returns true if the agent is actively processing.
func (m *AgentStateMachine) IsActive() bool {
    return m.CurrentState().IsActive()
}
```

#### 1.4: Write Unit Tests

**File:** `internal/agent/agent_state_test.go`

```go
func TestAgentStateMachine_InitialState(t *testing.T) {
    sm := NewAgentStateMachine(nil)
    if sm.CurrentState() != StateIdle {
        t.Errorf("expected initial state Idle, got %v", sm.CurrentState())
    }
}

func TestAgentStateMachine_ValidTransition(t *testing.T) {
    sm := NewAgentStateMachine(nil)
    err := sm.Transition(StateReceivingInput, "input_received", nil)
    if err != nil {
        t.Errorf("valid transition failed: %v", err)
    }
    if sm.CurrentState() != StateReceivingInput {
        t.Errorf("expected StateReceivingInput, got %v", sm.CurrentState())
    }
}

func TestAgentStateMachine_InvalidTransition(t *testing.T) {
    sm := NewAgentStateMachine(nil)
    err := sm.Transition(StateCompleted, "forced", nil)
    if err == nil {
        t.Error("invalid transition should have failed")
    }
}

func TestAgentStateMachine_History(t *testing.T) {
    sm := NewAgentStateMachine(nil)
    sm.Transition(StateReceivingInput, "input", nil)
    sm.Transition(StateClassifying, "classify", nil)

    history := sm.History()
    if len(history) != 2 {
        t.Errorf("expected 2 transitions, got %d", len(history))
    }
}

func TestAgentStateMachine_IsTerminal(t *testing.T) {
    sm := NewAgentStateMachine(nil)
    if sm.IsTerminal() {
        t.Error("Idle should not be terminal")
    }

    sm.Transition(StateError, "error", nil)
    if !sm.IsTerminal() {
        t.Error("Error should be terminal")
    }
}
```

**Verification:**
- [ ] `go test ./internal/agent -run TestAgentStateMachine -v` passes
- [ ] All transition validation tests pass
- [ ] History tracking works correctly

---

### Phase 2: Integrate State Machine into AgentLoop

**Goal:** Wire the state machine into the agent loop lifecycle.

#### 2.1: Add State Machine to AgentLoop

**File:** `internal/agent/loop.go`

Add field to `AgentLoop` struct:

```go
type AgentLoop struct {
    // ... existing fields ...

    // State machine for explicit state tracking
    stateMachine *AgentStateMachine

    // stateMu protects state-dependent operations during transitions
    stateMu sync.Mutex
}
```

#### 2.2: Initialize State Machine in Constructor

**File:** `internal/agent/loop.go`

In `NewAgentLoop()`:

```go
loop := &AgentLoop{
    // ... existing initialization ...
    stateMachine: NewAgentStateMachine(logger),
}
```

#### 2.3: Add State Transitions to reasoningCycle()

**File:** `internal/agent/loop.go`

Update `reasoningCycle()` to use state transitions:

```go
func (l *AgentLoop) reasoningCycle(ctx context.Context, conv *Conversation, conversationID string) (string, error) {
    // Transition to thinking state
    if err := l.stateMachine.Transition(StateThinking, "reasoning_cycle_start", map[string]any{
        "conversation_id": conversationID,
    }); err != nil {
        l.logger.Warn("State transition failed", "error", err)
    }

    for iteration := 1; iteration <= l.config.MaxIterations; iteration++ {
        // Update state with iteration info
        l.stateMachine.Transition(StateThinking, "iteration_start", map[string]any{
            "iteration": iteration,
        })

        // ... existing thinking logic ...

        // After LLM call, check for tool calls
        if response.HasToolCalls() {
            // Transition to tool executing
            l.stateMachine.Transition(StateToolExecuting, "tool_calls_received", map[string]any{
                "tool_count": len(response.ToolCalls),
            })

            // Execute tools
            results := l.executeToolCalls(ctx, response.ToolCalls)

            // Check for blocked state (permission denied)
            for _, result := range results {
                if !result.Success && strings.Contains(result.Error, "permission denied") {
                    l.stateMachine.Transition(StateBlocked, "permission_denied", map[string]any{
                        "tool": result.ToolCallID,
                        "error": result.Error,
                    })
                    // ... existing approval logic ...
                }
            }

            // Transition to processing result
            l.stateMachine.Transition(StateProcessingResult, "tools_completed", map[string]any{
                "success_count": countSuccess(results),
                "failure_count": countFailures(results),
            })
        }

        // Check for max iterations
        if iteration >= l.config.MaxIterations {
            l.stateMachine.Transition(StateMaxIterations, "iteration_limit_reached", map[string]any{
                "max_iterations": l.config.MaxIterations,
            })
            return exhaustMsg, ErrMaxIterationsReached
        }

        // Check for budget exhausted
        if l.budgetTracker != nil && l.budgetTracker.IsWrapUpRequested() {
            l.stateMachine.Transition(StateBudgetExhausted, "budget_exhausted", map[string]any{
                "used": l.budgetTracker.UsedBudget,
                "total": l.budgetTracker.totalBudget,
            })
            // ... existing wrap-up logic ...
        }
    }

    // Transition to completed
    l.stateMachine.Transition(StateCompleted, "reasoning_cycle_completed", map[string]any{
        "iterations": l.config.MaxIterations,
    })

    return response.Content, nil
}
```

#### 2.4: Add Error State Transitions

```go
// In reasoningCycle(), after LLM call or tool execution:
response, err := l.chatWithFailover(ctx, messages, chatOpts...)
if err != nil {
    l.stateMachine.Transition(StateError, "llm_call_failed", map[string]any{
        "error": err.Error(),
        "iteration": iteration,
    })
    return "", fmt.Errorf("LLM call failed: %w", err)
}
```

**Verification:**
- [ ] `go build ./...` succeeds
- [ ] State transitions are logged during agent execution
- [ ] Existing tests pass without modification

---

### Phase 3: State Observability Hooks

**Goal:** Expose agent state to external systems.

#### 3.1: Add State Query API

**File:** `internal/agent/loop.go`

Add public methods to `AgentLoop`:

```go
// GetState returns the current agent state.
func (l *AgentLoop) GetState() AgentState {
    return l.stateMachine.CurrentState()
}

// GetStateHistory returns recent state transitions.
func (l *AgentLoop) GetStateHistory() []StateTransition {
    return l.stateMachine.History()
}

// IsAgentActive returns true if the agent is actively processing.
func (l *AgentLoop) IsAgentActive() bool {
    return l.stateMachine.IsActive()
}

// GetStateSnapshot returns a full snapshot of agent state.
func (l *AgentLoop) GetStateSnapshot() AgentStateSnapshot {
    l.stateMu.Lock()
    defer l.stateMu.Unlock()

    return AgentStateSnapshot{
        CurrentState:    l.stateMachine.CurrentState().String(),
        IsTerminal:      l.stateMachine.IsTerminal(),
        IsActive:        l.stateMachine.IsActive(),
        ConversationID:  l.sessionID,
        AgentID:         l.agentID,
        CurrentTurn:     l.turnCounter,
        History:         l.stateMachine.History(),
        Timestamp:       time.Now(),
    }
}

// AgentStateSnapshot is a serializable snapshot of agent state.
type AgentStateSnapshot struct {
    CurrentState   string            `json:"current_state"`
    IsTerminal     bool              `json:"is_terminal"`
    IsActive       bool              `json:"is_active"`
    ConversationID string            `json:"conversation_id"`
    AgentID        string            `json:"agent_id"`
    CurrentTurn    int               `json:"current_turn"`
    History        []StateTransition `json:"history,omitempty"`
    Timestamp      time.Time         `json:"timestamp"`
}
```

#### 3.2: Wire into HTTP API

**File:** `internal/comm/http/server.go`

Add endpoint for state queries:

```go
// GET /api/v1/agents/{agent_id}/state
func (s *Server) handleGetAgentState(w http.ResponseWriter, r *http.Request) {
    agentID := mux.Vars(r)["agent_id"]

    agent := s.registry.Get(agentID)
    if agent == nil {
        http.Error(w, "agent not found", http.StatusNotFound)
        return
    }

    snapshot := agent.GetStateSnapshot()

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(snapshot)
}
```

#### 3.3: Wire into Bus Events

**File:** `internal/agent/loop.go`

Emit bus events on state transitions:

```go
// In NewAgentStateMachine, add a listener that publishes to bus:
stateMachine.OnTransition(func(t StateTransition) {
    if bus != nil {
        bus.Publish("agent.state.changed", AgentStateEvent{
            AgentID:        l.agentID,
            ConversationID: l.sessionID,
            Transition:     t,
        })
    }
})
```

**Verification:**
- [ ] HTTP endpoint returns correct state JSON
- [ ] Bus events are published on transitions
- [ ] External systems can query agent state

---

### Phase 4: State-Aware Error Handling

**Goal:** Use state context for smarter error recovery.

#### 4.1: State-Based Retry Logic

**File:** `internal/agent/loop.go`

```go
// handleErrorBasedOnState handles errors differently based on current state.
func (l *AgentLoop) handleErrorBasedOnState(err error) error {
    state := l.stateMachine.CurrentState()

    switch state {
    case StateThinking:
        // LLM call failed - try model failover
        return l.handleLLMError(err)

    case StateToolExecuting:
        // Tool execution failed - check if retryable
        return l.handleToolError(err)

    case StateBlocked:
        // Agent is blocked - don't retry, wait for user action
        return err

    case StateBudgetExhausted, StateMaxIterations:
        // Terminal states - wrap up gracefully
        return l.gracefulWrapUp(err)

    default:
        // Generic error handling
        return err
    }
}
```

#### 4.2: State-Aware Recovery

```go
// attemptStateRecovery tries to recover the agent to a valid state.
func (l *AgentLoop) attemptStateRecovery(err error) error {
    state := l.stateMachine.CurrentState()

    switch state {
    case StateError:
        // Try to reset to idle
        l.stateMachine.Transition(StateIdle, "recovery_reset", nil)
        return nil

    case StateBlocked:
        // Notify user and wait
        l.notifyUserBlocked()
        return errBlocked{err}

    case StateToolWaiting:
        // Timeout waiting for tool - mark as failed and continue
        l.stateMachine.Transition(StateProcessingResult, "tool_timeout", map[string]any{
            "error": err.Error(),
        })
        return nil

    default:
        return err
    }
}
```

**Verification:**
- [ ] Error handling respects agent state
- [ ] Recovery logic works for each error type
- [ ] Tests verify state-aware behavior

---

### Phase 5: State Persistence (Optional Enhancement)

**Goal:** Persist agent state for crash recovery.

#### 5.1: Define State Persistence Interface

```go
// AgentStatePersister persists agent state to durable storage.
type AgentStatePersister interface {
    // Save saves the agent state.
    Save(ctx context.Context, agentID string, snapshot AgentStateSnapshot) error
    // Load loads the agent state.
    Load(ctx context.Context, agentID string) (*AgentStateSnapshot, error)
    // Delete removes the agent state.
    Delete(ctx context.Context, agentID string) error
}
```

#### 5.2: SQLite Implementation

**File:** `internal/agent/state_persist_sqlite.go`

```go
type SQLiteStatePersister struct {
    db *sql.DB
}

func (p *SQLiteStatePersister) Save(ctx context.Context, agentID string, snapshot AgentStateSnapshot) error {
    data, _ := json.Marshal(snapshot)
    _, err := p.db.ExecContext(ctx, `
        INSERT INTO agent_states (agent_id, state_data, updated_at)
        VALUES (?, ?, ?)
        ON CONFLICT(agent_id) DO UPDATE SET state_data = ?, updated_at = ?
    `, agentID, data, snapshot.Timestamp, data, snapshot.Timestamp)
    return err
}

func (p *SQLiteStatePersister) Load(ctx context.Context, agentID string) (*AgentStateSnapshot, error) {
    var data []byte
    err := p.db.QueryRowContext(ctx, `
        SELECT state_data FROM agent_states WHERE agent_id = ?
    `, agentID).Scan(&data)
    if err != nil {
        return nil, err
    }
    var snapshot AgentStateSnapshot
    return &snapshot, json.Unmarshal(data, &snapshot)
}
```

**Verification:**
- [ ] State persists across daemon restarts
- [ ] Recovery from saved state works correctly

---

## Testing Strategy

### Unit Tests

| Test Case | File | Description |
|-----------|------|-------------|
| `TestAgentStateMachine_InitialState` | `agent_state_test.go` | Verify initial state is Idle |
| `TestAgentStateMachine_ValidTransitions` | `agent_state_test.go` | All valid transitions work |
| `TestAgentStateMachine_InvalidTransitions` | `agent_state_test.go` | Invalid transitions are rejected |
| `TestAgentStateMachine_History` | `agent_state_test.go` | History is correctly maintained |
| `TestAgentStateMachine_Listeners` | `agent_state_test.go` | Listeners are notified |
| `TestAgentStateSnapshot_Serialization` | `agent_state_test.go` | Snapshot JSON serialization |

### Integration Tests

| Test Case | Description |
|-----------|-------------|
| `TestReasoningCycle_StateTransitions` | Full reasoning cycle produces correct state sequence |
| `TestStateAwareErrorHandling` | Errors handled differently based on state |
| `TestHTTPStateEndpoint` | API returns correct state |

---

## Rollback Plan

If issues arise:

1. **Immediate**: State machine is additive - existing code continues to work
2. **Disable**: Set `agent.state_tracking.enabled = false` in config to skip transitions
3. **Revert**: Remove state machine integration from `reasoningCycle()`

---

## Configuration Changes

**File:** `config/agent.json5`

```json5
{
  "agent": {
    "state_tracking": {
      "enabled": true,
      "max_history": 100,
      "persist": false,
      "emit_events": true
    }
  }
}
```

---

## Metrics to Track

| Metric | Description | Alert Threshold |
|--------|-------------|-----------------|
| `agent.state.transitions` | Count of state transitions per session | > 100 (looping) |
| `agent.state.time_in_state` | Time spent in each state | Thinking > 60s |
| `agent.state.error_rate.by_state` | Error rate per state | Any state > 20% |
| `agent.state.invalid_transitions` | Count of rejected transitions | > 0 |

---

## Success Criteria

- [ ] **Phase 1**: State machine types defined and tested
- [ ] **Phase 2**: AgentLoop integrated with state machine
- [ ] **Phase 3**: State query API and HTTP endpoint working
- [ ] **Phase 4**: State-aware error handling implemented
- [ ] **Phase 5**: State persistence (optional) working
- [ ] **Overall**: No regression in agent behavior; improved observability and debugging
