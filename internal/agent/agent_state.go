package agent

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

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
	case StateQuotaWait:
		return "quota_wait"
	case StateCompleted:
		return "completed"
	case StateError:
		return "error"
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

// AgentStateMachine manages agent state transitions.
type AgentStateMachine struct {
	mu           sync.RWMutex
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

	// Don't log redundant same-state transitions
	if from == to {
		return nil
	}

	// Validate transition
	if !m.isValidTransition(from, to) {
		return fmt.Errorf("invalid state transition: %s -> %s (reason: %s)", from, to, reason)
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

	if m.logger != nil {
		m.logger.Debug("State transition",
			"from", from,
			"to", to,
			"reason", reason,
			"metadata", metadata)
	}

	// Notify listeners
	for _, listener := range m.listeners {
		go listener(transition)
	}

	return nil
}

// isValidTransition returns true if the transition is allowed.
//
// The table is intentionally permissive: it favors allowing legitimate
// production transitions over strict enforcement, because the agent loop
// (reasoningCycle in loop.go) drives transitions directly and any rejection
// silently fails via safeTransition (logged at Debug level), leaving the
// machine stuck in a stale state. The concrete impact of an overly-strict
// table is that GetStateSnapshot() always reports "idle", bus events never
// fire, and handleErrorBasedOnState always hits the default branch.
//
// Specifically, reasoningCycle begins in StateIdle (the initial state) and
// immediately transitions to StateThinking. It can also reach terminal
// states directly from Idle when an early-exit condition triggers before the
// first iteration (budget exhausted, completed via cached path, etc.). The
// table therefore permits every state reachable from Idle in practice, while
// still rejecting nonsensical transitions such as terminal -> terminal of a
// different kind, or Completed -> Thinking without going through the Idle
// reset.
func (m *AgentStateMachine) isValidTransition(from, to AgentState) bool {
	// Same-state transitions are no-ops handled earlier by Transition; mirror
	// that here for safety.
	if from == to {
		return true
	}

	// Define allowed transitions. Every defined AgentState appears as a key
	// below; if a future state is added without an entry here the `!ok` fall-
	// through below rejects it (safe-by-default) until the table is updated.
	allowedTransitions := map[AgentState][]AgentState{
		// Idle is the initial/reset state. reasoningCycle starts here and may
		// jump directly to Thinking, or short-circuit to a terminal state for
		// early exits (budget exhausted, error during setup, immediate
		// completion, max-iterations on a zero-iteration path, etc.).
		StateIdle: {
			StateReceivingInput,
			StateClassifying,
			StateThinking,
			StateGeneratingResponse,
			StateToolExecuting,
			StateProcessingResult,
			StateQuotaWait, // quota episode entered while agent idle
			StateError,
			StateCancelled,
			StateMaxIterations,
			StateBudgetExhausted,
			StateCompleted,
		},
		StateReceivingInput:     {StateClassifying, StateThinking, StateError, StateCancelled},
		StateClassifying:        {StateThinking, StateGeneratingResponse, StateError, StateCancelled},
		StateThinking:           {StateToolExecuting, StateToolWaiting, StateGeneratingResponse, StateProcessingResult, StateQuotaWait, StateError, StateBlocked, StateCancelled, StateMaxIterations, StateBudgetExhausted, StateCompleted},
		StateToolExecuting:      {StateToolWaiting, StateProcessingResult, StateGeneratingResponse, StateQuotaWait, StateError, StateBlocked, StateCancelled},
		StateToolWaiting:        {StateProcessingResult, StateToolExecuting, StateQuotaWait, StateError, StateBlocked, StateCancelled},
		StateProcessingResult:   {StateThinking, StateGeneratingResponse, StateToolExecuting, StateCompleted, StateQuotaWait, StateError, StateBlocked, StateMaxIterations, StateCancelled},
		StateGeneratingResponse: {StateCompleted, StateError, StateCancelled, StateQuotaWait, StateThinking},
		StateBlocked:            {StateThinking, StateToolExecuting, StateProcessingResult, StateQuotaWait, StateCancelled, StateError},
		// StateQuotaWait: the agent is parked waiting for a provider quota
		// window to reset (entered from any active state or Idle by the
		// QuotaEpisodeTracker). Exits to Thinking on automatic resume, Idle on
		// quota_cleared, Blocked on the 24h escalation, or Error/Cancelled.
		StateQuotaWait: {StateThinking, StateIdle, StateBlocked, StateError, StateCancelled},
		// Terminal states may only reset to Idle. Error additionally allows
		// transitioning back to Thinking for self-recovery (attemptStateRecovery
		// performs an Error→Idle reset; callers that prefer direct retry can
		// use Error→Thinking). Any other terminal-to-X transition is rejected,
		// including terminal→terminal pivots (e.g., Completed→Cancelled,
		// Error→MaxIterations) which must go through Idle first.
		StateError:           {StateIdle, StateThinking},
		StateCompleted:       {StateIdle},
		StateCancelled:       {StateIdle},
		StateMaxIterations:   {StateIdle},
		StateBudgetExhausted: {StateIdle},
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

// SafeTransition performs a state transition but never panics.
// If the transition is invalid, it logs a warning instead of failing.
func (m *AgentStateMachine) SafeTransition(to AgentState, reason string, metadata map[string]any) {
	if err := m.Transition(to, reason, metadata); err != nil {
		if m.logger != nil {
			// Snapshot from-state under the read lock to avoid a data race
			// with concurrent Transition calls (m.currentState is written
			// under m.mu in Transition).
			m.mu.RLock()
			from := m.currentState
			m.mu.RUnlock()
			m.logger.Warn("State transition failed (ignoring)",
				"from", from,
				"to", to,
				"reason", reason,
				"error", err)
		}
	}
}
