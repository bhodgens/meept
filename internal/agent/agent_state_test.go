package agent

import (
	"encoding/json"
	"log/slog"
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
	sm := NewAgentStateMachine(nil)
	// Must go through ReceivingInput -> Classifying before reaching Thinking, not idle -> completed
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

func TestAgentStateMachine_CannotDirectToCompleted(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	err := sm.Transition(StateCompleted, "direct_complete", nil)
	if err == nil {
		t.Error("should not allow direct transition to completed from idle")
	}
}

func TestAgentStateMachine_Invalidation(t *testing.T) {
	sm := NewAgentStateMachine(nil)
	// From idle, can go to receiving_input or cancelled
	// Can NOT go to thinking
	err := sm.Transition(StateThinking, "bypass", nil)
	if err == nil {
		t.Error("should not allow idle -> thinking")
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
