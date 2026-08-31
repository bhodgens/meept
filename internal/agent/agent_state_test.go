package agent

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAgentState_String(t *testing.T) {
	tests := []struct {
		state  AgentState
		expect string
	}{
		{StateIdle, "idle"},
		{StateReceivingInput, "receiving_input"},
		{StateClassifying, "classifying"},
		{StateThinking, "thinking"},
		{StateToolExecuting, "tool_executing"},
		{StateToolWaiting, "tool_waiting"},
		{StateProcessingResult, "processing_result"},
		{StateGeneratingResponse, "generating_response"},
		{StateBlocked, "blocked"},
		{StateError, "error"},
		{StateCompleted, "completed"},
		{StateCancelled, "cancelled"},
		{StateMaxIterations, "max_iterations"},
		{StateBudgetExhausted, "budget_exhausted"},
		{AgentState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expect {
				t.Errorf("AgentState(%d).String() = %q, want %q", tt.state, got, tt.expect)
			}
		})
	}
}

func TestAgentState_IsTerminal(t *testing.T) {
	terminal := []AgentState{StateCompleted, StateError, StateCancelled, StateMaxIterations, StateBudgetExhausted}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("expected %s to be terminal", s)
		}
	}

	nonTerminal := []AgentState{StateIdle, StateThinking, StateToolExecuting, StateToolWaiting,
		StateProcessingResult, StateGeneratingResponse, StateBlocked, StateReceivingInput,
		StateClassifying}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("expected %s to NOT be terminal", s)
		}
	}
}

func TestAgentState_IsActive(t *testing.T) {
	active := []AgentState{StateThinking, StateToolExecuting, StateToolWaiting, StateProcessingResult, StateGeneratingResponse}
	for _, s := range active {
		if !s.IsActive() {
			t.Errorf("expected %s to be active", s)
		}
	}

	nonActive := []AgentState{StateIdle, StateReceivingInput, StateClassifying, StateBlocked,
		StateError, StateCompleted, StateCancelled, StateMaxIterations, StateBudgetExhausted}
	for _, s := range nonActive {
		if s.IsActive() {
			t.Errorf("expected %s to NOT be active", s)
		}
	}
}

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

func TestAgentStateMachine_ChainOfTransitions(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	err := sm.Transition(StateReceivingInput, "input", nil)
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}
	err = sm.Transition(StateClassifying, "classify", nil)
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}
	err = sm.Transition(StateThinking, "start_llm", nil)
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}
	if sm.CurrentState() != StateThinking {
		t.Errorf("expected StateThinking, got %v", sm.CurrentState())
	}
}

func TestAgentStateMachine_InvalidTransition(t *testing.T) {
	// Terminal-to-terminal pivots must go through Idle reset.
	// Idle -> Completed is allowed by the permissive production table; pivot
	// from Completed to a different terminal (Error) is rejected.
	sm := NewAgentStateMachine(nil)
	if err := sm.Transition(StateCompleted, "to_completed", nil); err != nil {
		t.Fatalf("Idle -> Completed should be allowed: %v", err)
	}
	err := sm.Transition(StateError, "terminal_pivot", nil)
	if err == nil {
		t.Error("Completed -> Error should be rejected (terminal-to-terminal)")
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
	if history[0].From != StateIdle || history[0].To != StateReceivingInput {
		t.Errorf("first transition: %+v", history[0])
	}
	if history[1].From != StateReceivingInput || history[1].To != StateClassifying {
		t.Errorf("second transition: %+v", history[1])
	}
}

func TestAgentStateMachine_TerminalState(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	if sm.IsTerminal() {
		t.Error("Idle should not be terminal")
	}

	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateError, "error", nil)
	if !sm.IsTerminal() {
		t.Error("Error should be terminal")
	}
}

func TestAgentStateMachine_ActiveState(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	if sm.IsActive() {
		t.Error("Idle should not be active")
	}

	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "start", nil)
	if !sm.IsActive() {
		t.Error("Thinking should be active")
	}
}

func TestAgentStateMachine_SameStateNoOp(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	// Navigate through valid transitions to reach Thinking state
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "think", nil)
	// Same state transition should not fail and should not record history
	err := sm.Transition(StateThinking, "noop", nil)
	if err != nil {
		t.Errorf("same-state transition failed: %v", err)
	}
	// History should only contain the 3 transitions, not the noop
	if len(sm.History()) != 3 {
		t.Errorf("same-state transition should not add to history, got %d entries", len(sm.History()))
	}
}

func TestAgentStateMachine_MaxHistoryTrimming(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.maxHistory = 3
	sm.Transition(StateReceivingInput, "1", nil)
	sm.Transition(StateClassifying, "2", nil)
	sm.Transition(StateThinking, "3", nil)
	sm.Transition(StateToolExecuting, "4", nil)

	history := sm.History()
	if len(history) != 3 {
		t.Errorf("expected 3 history entries (maxHistory=3), got %d", len(history))
	}
	// Should contain the last 3 transitions after trimming:
	// 1. receiving_input -> classifying
	// 2. classifying -> thinking
	// 3. thinking -> tool_executing
	if history[0].From != StateReceivingInput {
		t.Errorf("first entry should be receiving_input->classifying, got %+v", history[0])
	}
	if history[1].To != StateThinking {
		t.Errorf("second entry should end at thinking, got %+v", history[1])
	}
	if history[2].To != StateToolExecuting {
		t.Errorf("last entry should end at tool_executing, got %+v", history[2])
	}
}

func TestAgentStateMachine_ListenerNotified(t *testing.T) {
	sm := NewAgentStateMachine(nil)

	var notified atomic.Bool
	var m mutex

	sm.OnTransition(func(t StateTransition) {
		m.Lock()
		defer m.Unlock()
		notified.Store(true)
	})

	// Navigate through a valid path that triggers transitions
	sm.Transition(StateReceivingInput, "input", nil)

	// Give goroutines a moment to complete
	time.Sleep(50 * time.Millisecond)

	m.Lock()
	if !notified.Load() {
		t.Error("listener should have been notified")
	}
	m.Unlock()
}

// simple mutex wrapper for atomic test
type mutex struct{ sync.Mutex }

func TestAgentStateMachine_ErrorToIdleReset(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateError, "initial_error", nil)
	if sm.CurrentState() != StateError {
		t.Error("expected error state")
	}
	// From Error, can go to Idle (reset)
	sm.Transition(StateIdle, "reset", nil)
	if sm.CurrentState() != StateIdle {
		t.Errorf("expected reset to idle, got %v", sm.CurrentState())
	}
}

func TestAgentStateMachine_CompletedToIdleReset(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "start", nil)
	sm.Transition(StateGeneratingResponse, "response", nil)
	sm.Transition(StateCompleted, "done", nil)
	if !sm.IsTerminal() {
		t.Error("Completed should be terminal")
	}
	// From Completed, can go to Idle (reset)
	sm.Transition(StateIdle, "reset", nil)
	if sm.CurrentState() != StateIdle {
		t.Errorf("expected reset to idle, got %v", sm.CurrentState())
	}
}

func TestAgentStateMachine_TransitionWithErrorMetadata(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	err := sm.Transition(StateReceivingInput, "input", map[string]any{"source": "ui", "count": 42})
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}
	hist := sm.History()
	if len(hist) != 1 {
		t.Fatalf("expected 1 history entry")
	}
	metadata := hist[0].Metadata
	if metadata["source"] != "ui" {
		t.Errorf("metadata source = %v, want ui", metadata["source"])
	}
	if metadata["count"] != 42 {
		t.Errorf("metadata count = %v, want 42", metadata["count"])
	}
}

// TestAgentStateMachine_IdleAllowsProductionStates verifies that the
// permissive Idle transition table permits every state that reasoningCycle
// reaches directly from Idle (the bug that motivated widening the table:
// previously Idle only allowed ReceivingInput/Cancelled, so every
// safeTransition from Idle silently failed and the machine was stuck).
func TestAgentStateMachine_IdleAllowsProductionStates(t *testing.T) {
	allowed := []AgentState{
		StateThinking,
		StateReceivingInput,
		StateClassifying,
		StateGeneratingResponse,
		StateCompleted,
		StateError,
		StateCancelled,
		StateMaxIterations,
		StateBudgetExhausted,
	}
	for _, target := range allowed {
		sm := NewAgentStateMachine(nil)
		if err := sm.Transition(target, "from_idle", nil); err != nil {
			t.Errorf("Idle -> %s should be allowed (permissive production table): %v", target, err)
		}
	}
}

// TestAgentStateMachine_TerminalToTerminalRejected verifies the table still
// enforces the one constraint that matters: a terminal state cannot pivot
// directly to a different terminal state (must reset through Idle first).
func TestAgentStateMachine_TerminalToTerminalRejected(t *testing.T) {
	terminals := []AgentState{StateCompleted, StateError, StateCancelled, StateMaxIterations, StateBudgetExhausted}
	for _, from := range terminals {
		for _, to := range terminals {
			if from == to {
				continue
			}
			sm := NewAgentStateMachine(nil)
			// Reach `from` via Idle (always permitted by the permissive table).
			if err := sm.Transition(from, "to_terminal", nil); err != nil {
				t.Fatalf("setup Idle -> %s failed: %v", from, err)
			}
			if err := sm.Transition(to, "pivot", nil); err == nil {
				t.Errorf("terminal %s -> %s should be rejected (must reset to Idle first)", from, to)
			}
		}
	}
}

// TestAgentStateMachine_CompletedCannotPivotToThinking verifies the one
// non-Idle reset path that remains disallowed: a completed cycle cannot
// resume Thinking without first resetting to Idle.
func TestAgentStateMachine_CompletedCannotPivotToThinking(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	if err := sm.Transition(StateCompleted, "complete", nil); err != nil {
		t.Fatalf("Idle -> Completed should be allowed: %v", err)
	}
	if err := sm.Transition(StateThinking, "pivot_without_reset", nil); err == nil {
		t.Error("Completed -> Thinking should be rejected (must reset to Idle first)")
	}
	// Reset-then-Think is fine.
	if err := sm.Transition(StateIdle, "reset", nil); err != nil {
		t.Fatalf("Completed -> Idle reset failed: %v", err)
	}
	if err := sm.Transition(StateThinking, "fresh_cycle", nil); err != nil {
		t.Errorf("Idle -> Thinking after reset should be allowed: %v", err)
	}
}

func TestAgentStateMachine_MultipleListeners(t *testing.T) {
	sm := NewAgentStateMachine(slog.Default())

	var count atomic.Int32
	sm.OnTransition(func(t StateTransition) { count.Add(1) })
	sm.OnTransition(func(t StateTransition) { count.Add(1) })

	sm.Transition(StateReceivingInput, "test", nil)

	// Give goroutines a moment to complete
	time.Sleep(50 * time.Millisecond)
	if n := count.Load(); n != 2 {
		t.Errorf("expected 2 notifications, got %d", n)
	}
}

func TestAgentStateMachine_CancelledTransitions(t *testing.T) {
	sm := NewAgentStateMachine(nil)

	// From Idle, can go to Cancelled
	err := sm.Transition(StateCancelled, "cancelled", nil)
	if err != nil {
		t.Errorf("allowed idle -> cancelled: %v", err)
	}

	// From Cancelled, can go to Idle (reset)
	err = sm.Transition(StateIdle, "reset", nil)
	if err != nil {
		t.Errorf("allowed cancelled -> idle: %v", err)
	}
}

func TestAgentStateMachine_MaxIterationsFromThinking(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "start", nil)

	err := sm.Transition(StateMaxIterations, "limit_reached", nil)
	if err != nil {
		t.Errorf("allowed thinking -> max_iterations: %v", err)
	}
}

func TestAgentStateMachine_BudgetExhaustedFromThinking(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "start", nil)

	err := sm.Transition(StateBudgetExhausted, "budget_reached", nil)
	if err != nil {
		t.Errorf("allowed thinking -> budget_exhausted: %v", err)
	}
}

func TestAgentStateMachine_TransitionFromToolExecuting(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "start", nil)
	sm.Transition(StateToolExecuting, "tools", nil)

	// From ToolExecuting, can go to ProcessingResult
	err := sm.Transition(StateProcessingResult, "done", nil)
	if err != nil {
		t.Errorf("allowed tool_executing -> processing_result: %v", err)
	}

	// From ProcessingResult, can go to Thinking (for next iteration)
	err = sm.Transition(StateThinking, "next_iteration", nil)
	if err != nil {
		t.Errorf("allowed processing_result -> thinking: %v", err)
	}
}

func TestAgentStateMachine_BlockedTransitions(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "start", nil)
	sm.Transition(StateToolExecuting, "tools", nil)
	sm.Transition(StateBlocked, "blocked", nil)

	// From Blocked, can go back to Thinking (after user approves)
	err := sm.Transition(StateThinking, "unblocked", nil)
	if err != nil {
		t.Errorf("allowed blocked -> thinking: %v", err)
	}

	// From Blocked, can go to Cancelled
	sm2 := NewAgentStateMachine(nil)
	sm2.Transition(StateReceivingInput, "input", nil)
	sm2.Transition(StateClassifying, "classify", nil)
	sm2.Transition(StateThinking, "start", nil)
	sm2.Transition(StateToolExecuting, "tools", nil)
	sm2.Transition(StateBlocked, "blocked", nil)
	err = sm2.Transition(StateCancelled, "cancel", nil)
	if err != nil {
		t.Errorf("allowed blocked -> cancelled: %v", err)
	}
}

func TestAgentStateMachine_ToToolWaiting(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "start", nil)
	sm.Transition(StateToolExecuting, "tools", nil)
	sm.Transition(StateToolWaiting, "waiting", nil)

	// From ToolWaiting, can go to ProcessingResult
	err := sm.Transition(StateProcessingResult, "results", nil)
	if err != nil {
		t.Errorf("allowed tool_waiting -> processing_result: %v", err)
	}
}

func TestAgentSnapshot_Serialization(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)

	snapshot := AgentStateSnapshot{
		CurrentState:   sm.CurrentState().String(),
		IsTerminal:     sm.IsTerminal(),
		IsActive:       sm.IsActive(),
		ConversationID: "test-conv-1",
		AgentID:        "agent-001",
		CurrentTurn:    5,
		History:        sm.History(),
		Timestamp:      time.Now(),
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("failed to marshal snapshot: %v", err)
	}

	var parsed AgentStateSnapshot
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal snapshot: %v", err)
	}

	if parsed.CurrentState != "classifying" {
		t.Errorf("current_state = %q, want %q", parsed.CurrentState, "classifying")
	}
	if parsed.IsTerminal {
		t.Error("should not be terminal")
	}
	if parsed.IsActive {
		t.Error("should not be active")
	}
	if parsed.ConversationID != "test-conv-1" {
		t.Errorf("conversation_id = %q, want %q", parsed.ConversationID, "test-conv-1")
	}
	if parsed.AgentID != "agent-001" {
		t.Errorf("agent_id = %q, want %q", parsed.AgentID, "agent-001")
	}
	if parsed.CurrentTurn != 5 {
		t.Errorf("current_turn = %d, want %d", parsed.CurrentTurn, 5)
	}
	if len(parsed.History) != 2 {
		t.Errorf("history count = %d, want %d", len(parsed.History), 2)
	}
}

func TestAgentStateMachine_BlockedFromToolWaiting(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "start", nil)
	sm.Transition(StateToolExecuting, "tools", nil)
	sm.Transition(StateToolWaiting, "waiting", nil)

	// From ToolWaiting, can go to Blocked
	err := sm.Transition(StateBlocked, "blocked_while_waiting", nil)
	if err != nil {
		t.Errorf("allowed tool_waiting -> blocked: %v", err)
	}
}

func TestAgentStateMachine_ErrorTransitions(t *testing.T) {
	sm := NewAgentStateMachine(nil)

	// From states that have Error in their allowed transitions
	states := []AgentState{
		StateClassifying,
		StateThinking,
		StateToolExecuting,
		StateToolWaiting,
		StateProcessingResult,
		StateBlocked,
		StateGeneratingResponse,
	}

	for _, s := range states {
		t.Run(s.String(), func(t *testing.T) {
			// Build path to target state
			sm.Transition(StateIdle, "reset", nil)
			sm.Transition(StateReceivingInput, "input", nil)
			sm.Transition(StateClassifying, "classify", nil)
			if s == StateClassifying {
				// already there
			} else {
				sm.Transition(StateThinking, "think", nil)
				if s == StateThinking {
					// already there
				} else {
					sm.Transition(StateToolExecuting, "exec", nil)
					if s == StateToolExecuting {
						// already there
					} else {
						sm.Transition(StateToolWaiting, "wait", nil)
						if s == StateToolWaiting {
							// already there
						} else {
							sm.Transition(StateProcessingResult, "result", nil)
							if s == StateProcessingResult {
								// already there
							} else {
								sm.Transition(StateBlocked, "blocked", nil)
								if s == StateBlocked {
									// already there
								}
							}
						}
					}
				}
			}

			err := sm.Transition(StateError, "error_from_"+s.String(), nil)
			if err != nil {
				t.Errorf("should allow transition to Error from %s: %v", s, err)
			}
		})
	}
}

func TestAgentStateMachine_TransitionReasonInHistory(t *testing.T) {
	sm := NewAgentStateMachine(slog.Default())
	sm.Transition(StateReceivingInput, "my_reason", nil)
	sm.Transition(StateClassifying, "my_reason_2", nil)

	hist := sm.History()
	if hist[0].Reason != "my_reason" {
		t.Errorf("reason = %q, want %q", hist[0].Reason, "my_reason")
	}
	if hist[1].Reason != "my_reason_2" {
		t.Errorf("reason = %q, want %q", hist[1].Reason, "my_reason_2")
	}
}

func TestAgentStateMachine_HistoryTimestamp(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "test", nil)

	hist := sm.History()
	if hist[0].Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
	if hist[0].From != StateIdle {
		t.Error("from should be idle")
	}
	if hist[0].To != StateReceivingInput {
		t.Error("to should be receiving_input")
	}
}

func TestAgentStateMachine_HistoryTurnID(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	if sm.CurrentState() != StateIdle {
		t.Error("should start in idle")
	}
	// TurnID defaults to 0 (unset) for the initial state machine tests
	// We don't set turn_id from the state machine itself, it's set by the caller
}

// --- AgentLoop-integrated tests ---

func TestAgentLoop_InitialState(t *testing.T) {
	loop := NewAgentLoop("test-session-1", "/tmp/test")
	if loop.GetState() != StateIdle {
		t.Errorf("expected new loop state Idle, got %v", loop.GetState())
	}
	if loop.IsAgentActive() {
		t.Error("new loop should not be active")
	}
}

func TestAgentLoop_GetStateHistory(t *testing.T) {
	loop := NewAgentLoop("test-session-1", "/tmp/test")
	hist := loop.GetStateHistory()
	if hist == nil {
		t.Error("history should not be nil (empty valid slice)")
	}
	if len(hist) != 0 {
		t.Errorf("new loop should have empty history, got %d entries", len(hist))
	}
}

func TestAgentLoop_StateSnapshot(t *testing.T) {
	loop := NewAgentLoop("conv-123", "/tmp/test")
	loop.agentID = "agent-001"

	snap := loop.GetStateSnapshot()
	if snap.CurrentState != "idle" {
		t.Errorf("current_state = %q, want %q", snap.CurrentState, "idle")
	}
	if snap.ConversationID != "conv-123" {
		t.Errorf("conversation_id = %q, want %q", snap.ConversationID, "conv-123")
	}
	if snap.AgentID != "agent-001" {
		t.Errorf("agent_id = %q, want %q", snap.AgentID, "agent-001")
	}
	if snap.IsTerminal {
		t.Error("should not be terminal")
	}
	if snap.IsActive {
		t.Error("should not be active")
	}
	_ = snap.Timestamp
	if snap.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestAgentLoop_StateSnapshotJSON(t *testing.T) {
	loop := NewAgentLoop("conv-456", "/tmp/test")
	loop.agentID = "agent-002"

	snap := loop.GetStateSnapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var parsed AgentStateSnapshot
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if parsed.CurrentState != "idle" {
		t.Errorf("reconstructed current_state = %q", parsed.CurrentState)
	}
}

func TestAgentStateMachine_IsTerminalMethod(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	if sm.IsTerminal() {
		t.Error("idle should not be terminal via IsTerminal()")
	}
	sm.Transition(StateReceivingInput, "input", nil)
	if sm.IsTerminal() {
		t.Error("receiving_input should not be terminal via IsTerminal()")
	}
	sm.Transition(StateClassifying, "classify", nil)
	sm.Transition(StateThinking, "think", nil)
	sm.Transition(StateToolExecuting, "exec", nil)
	sm.Transition(StateProcessingResult, "result", nil)
	sm.Transition(StateCompleted, "done", nil)
	if !sm.IsTerminal() {
		t.Error("completed should be terminal via IsTerminal()")
	}
}

func TestAgentStateMachine_IsActiveMethod(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	if sm.IsActive() {
		t.Error("idle should not be active via IsActive()")
	}
	sm.Transition(StateReceivingInput, "input", nil)
	sm.Transition(StateClassifying, "classify", nil)
	if sm.IsActive() {
		t.Error("classifying should not be active via IsActive()")
	}
	sm.Transition(StateThinking, "start", nil)
	if !sm.IsActive() {
		t.Error("thinking should be active via IsActive()")
	}
}

func TestAgentStateMachine_HistoryCopyIsIndependent(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	sm.Transition(StateReceivingInput, "1", nil)

	h1 := sm.History()
	if len(h1) != 1 {
		t.Fatalf("expected 1 entry")
	}

	// Modify the returned slice
	h1[0].Reason = "modified"

	// Original should be unchanged
	h2 := sm.History()
	if h2[0].Reason != "1" {
		t.Errorf("history should be independent copy, got reason=%q", h2[0].Reason)
	}
}

func TestAgentStateMachine_TransitionPreservesTurnID(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	err := sm.Transition(StateReceivingInput, "reason", map[string]any{"turn_id": 42})
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}
	hist := sm.History()
	if hist[0].Metadata["turn_id"] != 42 {
		t.Errorf("turn_id metadata = %v, want 42", hist[0].Metadata["turn_id"])
	}
}

// --- Phase 4 integration tests (GAP 4) ---
//
// These tests verify the state-machine integration points the Phase 4 plan
// named:
//  1. TestReasoningCycle_StateTransitions — the transition table permits the
//     full expected reasoning cycle: Idle -> Thinking -> ToolExecuting ->
//     ProcessingResult -> Thinking -> GeneratingResponse -> Completed -> Idle.
//     Full reasoningCycle execution requires LLM + tool infrastructure that
//     is too heavy for this unit test; instead we drive the state machine
//     through the exact transition sequence that reasoningCycle emits (per
//     safeTransition call sites in loop.go) and assert each step succeeds.
//  2. TestStateAwareErrorHandling — verifies handleErrorBasedOnState
//     dispatches based on the current state machine state.
//  3. TestHTTPStateEndpoint — full HTTP server setup is too heavy; instead we
//     verify GetStateSnapshot returns correct fields after transitions, which
//     is what the HTTP endpoint serializes.

// TestReasoningCycle_StateTransitions verifies the permissive transition
// table admits the full expected reasoning-cycle state sequence. Each step
// mirrors a real safeTransition call site in loop.go's reasoningCycle:
//
//	Idle -> Thinking (line ~2512, reasoning_cycle_start)
//	Thinking -> ToolExecuting (line ~2915, tool_calls_received)
//	ToolExecuting -> ProcessingResult (line ~3037, tools_completed)
//	ProcessingResult -> Thinking (next-iteration continuation)
//	Thinking -> GeneratingResponse (line ~3193, generating_response)
//	GeneratingResponse -> Completed (line ~3056 / terminal)
//	Completed -> Idle (reset for next turn)
func TestReasoningCycle_StateTransitions(t *testing.T) {
	sm := NewAgentStateMachine(nil)

	// Each step must succeed; a failure here means the transition table
	// rejects a sequence the production agent loop actually emits, which is
	// the silent-failure bug from BUG 1.
	type step struct {
		to     AgentState
		reason string
	}
	steps := []step{
		{StateThinking, "reasoning_cycle_start"},
		{StateToolExecuting, "tool_calls_received"},
		{StateProcessingResult, "tools_completed"},
		{StateThinking, "next_iteration"},
		{StateGeneratingResponse, "generating_response"},
		{StateCompleted, "done"},
		{StateIdle, "reset_for_next_turn"},
	}

	for i, s := range steps {
		if err := sm.Transition(s.to, s.reason, nil); err != nil {
			t.Fatalf("step %d (%s) transition to %s failed: %v",
				i+1, s.reason, s.to, err)
		}
	}

	if sm.CurrentState() != StateIdle {
		t.Errorf("expected final Idle after reset, got %v", sm.CurrentState())
	}

	hist := sm.History()
	// 6 transitions recorded (the 7th step is Completed->Idle, so all 7 minus
	// any same-state no-ops; none of these are same-state so we expect 7).
	if len(hist) != len(steps) {
		t.Errorf("expected %d history entries, got %d", len(steps), len(hist))
	}
	// Spot-check the first and last meaningful transitions.
	if hist[0].From != StateIdle || hist[0].To != StateThinking {
		t.Errorf("first transition = %+v, want Idle->Thinking", hist[0])
	}
	last := hist[len(hist)-1]
	if last.From != StateCompleted || last.To != StateIdle {
		t.Errorf("last transition = %+v, want Completed->Idle", last)
	}
}

// TestStateAwareErrorHandling verifies handleErrorBasedOnState dispatches
// based on the current agent-loop state. It exercises the StateThinking
// (LLM-error) and StateBlocked (no-retry) branches explicitly.
func TestStateAwareErrorHandling(t *testing.T) {
	t.Run("thinking_state_llm_error", func(t *testing.T) {
		loop := NewAgentLoop("err-test-thinking", "/tmp/test")
		// Drive the state machine to StateThinking (the permissive table
		// allows Idle -> Thinking directly).
		if err := loop.stateMachine.Transition(StateThinking, "test_setup", nil); err != nil {
			t.Fatalf("setup transition to Thinking failed: %v", err)
		}
		original := errors.New("llm boom")
		got := loop.handleErrorBasedOnState(original)
		if got == nil {
			t.Fatal("handleErrorBasedOnState(Thinking) should return non-nil error")
		}
		if !errors.Is(got, original) {
			t.Errorf("expected returned error to wrap original, got %v", got)
		}
	})

	t.Run("blocked_state_no_retry", func(t *testing.T) {
		loop := NewAgentLoop("err-test-blocked", "/tmp/test")
		// Drive to StateBlocked via the production path: Idle -> Thinking ->
		// ToolExecuting -> Blocked (matches loop.go permission-denied path).
		sm := loop.stateMachine
		for _, s := range []struct {
			to     AgentState
			reason string
		}{
			{StateThinking, "start"},
			{StateToolExecuting, "tools"},
			{StateBlocked, "permission_denied"},
		} {
			if err := sm.Transition(s.to, s.reason, nil); err != nil {
				t.Fatalf("setup transition to %s failed: %v", s.to, err)
			}
		}
		original := errors.New("blocked")
		got := loop.handleErrorBasedOnState(original)
		if got == nil {
			t.Fatal("handleErrorBasedOnState(Blocked) should return non-nil (no auto-retry)")
		}
		if !errors.Is(got, original) {
			t.Errorf("Blocked path should surface original error unwrapped, got %v", got)
		}
	})

	t.Run("terminal_state_wraps", func(t *testing.T) {
		loop := NewAgentLoop("err-test-terminal", "/tmp/test")
		if err := loop.stateMachine.Transition(StateBudgetExhausted, "budget", nil); err != nil {
			t.Fatalf("setup transition failed: %v", err)
		}
		original := errors.New("underlying")
		got := loop.handleErrorBasedOnState(original)
		if got == nil {
			t.Fatal("terminal path should return non-nil")
		}
		// Terminal states wrap with their state name prefix.
		if !errors.Is(got, original) {
			t.Errorf("terminal wrap should preserve original via %%w, got %v", got)
		}
		expectedPrefix := StateBudgetExhausted.String()
		if !strings.Contains(got.Error(), expectedPrefix) {
			t.Errorf("expected error message to mention %q, got %q", expectedPrefix, got.Error())
		}
	})
}

// TestHTTPStateEndpoint verifies the snapshot returned by GetStateSnapshot
// reflects the current state machine state. This is the data the HTTP
// /api/v1/agent/state endpoint serializes; full HTTP server setup is too
// heavy for this unit test.
func TestHTTPStateEndpoint(t *testing.T) {
	loop := NewAgentLoop("snapshot-conv", "/tmp/test")
	loop.agentID = "agent-snap"

	// Initial snapshot: idle, not terminal, not active.
	snap := loop.GetStateSnapshot()
	if snap.CurrentState != "idle" {
		t.Errorf("initial current_state = %q, want idle", snap.CurrentState)
	}
	if snap.IsTerminal {
		t.Error("initial state should not be terminal")
	}
	if snap.IsActive {
		t.Error("initial state should not be active")
	}
	if snap.ConversationID != "snapshot-conv" {
		t.Errorf("conversation_id = %q, want snapshot-conv", snap.ConversationID)
	}
	if snap.AgentID != "agent-snap" {
		t.Errorf("agent_id = %q, want agent-snap", snap.AgentID)
	}
	if snap.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}

	// Transition to Thinking and re-snapshot.
	if err := loop.stateMachine.Transition(StateThinking, "snap_test", nil); err != nil {
		t.Fatalf("transition to Thinking failed: %v", err)
	}
	snap = loop.GetStateSnapshot()
	if snap.CurrentState != "thinking" {
		t.Errorf("after transition current_state = %q, want thinking", snap.CurrentState)
	}
	if !snap.IsActive {
		t.Error("thinking should be active")
	}
	if snap.IsTerminal {
		t.Error("thinking should not be terminal")
	}
	if len(snap.History) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(snap.History))
	}

	// Transition to terminal and verify flags flip.
	if err := loop.stateMachine.Transition(StateCompleted, "done", nil); err != nil {
		t.Fatalf("transition to Completed failed: %v", err)
	}
	snap = loop.GetStateSnapshot()
	if snap.CurrentState != "completed" {
		t.Errorf("after transition current_state = %q, want completed", snap.CurrentState)
	}
	if !snap.IsTerminal {
		t.Error("completed should be terminal")
	}
	if snap.IsActive {
		t.Error("completed should not be active")
	}
	if len(snap.History) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(snap.History))
	}

	// Snapshot must serialize to JSON without error (the HTTP endpoint path).
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot failed: %v", err)
	}
	if len(data) == 0 {
		t.Error("marshaled snapshot should be non-empty")
	}
}

func TestAgentStateMachine_QuotaWaitTransitions(t *testing.T) {
	activeSources := []AgentState{
		StateIdle,
		StateThinking,
		StateToolExecuting,
		StateToolWaiting,
		StateProcessingResult,
		StateGeneratingResponse,
	}
	for _, from := range activeSources {
		m2 := NewAgentStateMachine(slog.Default())
		if from != StateIdle {
			if err := m2.Transition(StateThinking, "setup", nil); err != nil {
				t.Fatalf("setup to thinking failed: %v", err)
			}
			if from != StateThinking {
				if err := m2.Transition(from, "setup", nil); err != nil {
					t.Fatalf("setup transition to %v failed: %v", from, err)
				}
			}
		}
		if err := m2.Transition(StateQuotaWait, "quota_blocked", nil); err != nil {
			t.Errorf("expected %v -> quota_wait allowed, got %v", from, err)
		}
	}

	// StateQuotaWait exits to the documented set.
	exits := []AgentState{StateThinking, StateIdle, StateBlocked, StateError, StateCancelled}
	for _, to := range exits {
		m2 := NewAgentStateMachine(slog.Default())
		if err := m2.Transition(StateQuotaWait, "quota_blocked", nil); err != nil {
			t.Fatalf("entering quota_wait failed: %v", err)
		}
		if err := m2.Transition(to, "quota_exit", nil); err != nil {
			t.Errorf("expected quota_wait -> %v allowed, got %v", to, err)
		}
	}

	// quota_wait -> Completed is NOT in the table (must route via Idle).
	m2 := NewAgentStateMachine(slog.Default())
	_ = m2.Transition(StateQuotaWait, "quota_blocked", nil)
	if err := m2.Transition(StateCompleted, "should_reject", nil); err == nil {
		t.Error("expected quota_wait -> completed to be rejected")
	}
}

func TestAgentStateMachine_QuotaWaitFromBlocked(t *testing.T) {
	m := NewAgentStateMachine(slog.Default())
	// Setup: reach blocked from an active state (idle -> blocked is illegal).
	if err := m.Transition(StateThinking, "start", nil); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	// Blocked -> quota_wait is legal (re-block during recovery window).
	if err := m.Transition(StateBlocked, "approval_wait", nil); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := m.Transition(StateQuotaWait, "quota_blocked", nil); err != nil {
		t.Errorf("expected blocked -> quota_wait allowed, got %v", err)
	}
}
