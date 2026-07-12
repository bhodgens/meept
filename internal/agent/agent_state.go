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
func (m *AgentStateMachine) isValidTransition(from, to AgentState) bool {
	// Define allowed transitions
	allowedTransitions := map[AgentState][]AgentState{
		StateIdle:               {StateReceivingInput, StateCancelled},
		StateReceivingInput:     {StateClassifying, StateCancelled},
		StateClassifying:        {StateThinking, StateError, StateCancelled},
		StateThinking:           {StateToolExecuting, StateGeneratingResponse, StateError, StateCancelled, StateMaxIterations, StateBudgetExhausted},
		StateToolExecuting:      {StateToolWaiting, StateProcessingResult, StateError, StateBlocked},
		StateToolWaiting:        {StateProcessingResult, StateError, StateBlocked},
		StateProcessingResult:   {StateThinking, StateCompleted, StateError, StateMaxIterations},
		StateGeneratingResponse: {StateCompleted, StateError},
		StateBlocked:            {StateThinking, StateToolExecuting, StateCancelled, StateError},
		StateError:              {StateIdle, StateCancelled}, // Reset or cancel
		StateCompleted:          {StateIdle},                  // Reset
		StateCancelled:          {StateIdle},                  // Reset
		StateMaxIterations:      {StateIdle},                  // Reset
		StateBudgetExhausted:    {StateIdle},                  // Reset
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
			m.logger.Warn("State transition failed (ignoring)",
				"from", m.currentState,
				"to", to,
				"reason", reason,
				"error", err)
		}
	}
}
