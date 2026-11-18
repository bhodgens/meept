package employee

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Frozen topic names from master C1/C2. Subscribe by NAME so this gate fails
// RED only on the pinned SetBus seam, not on the branch-01 topic declarations.
const (
	gateEmployeeResultTopic = "employee.result"
	gateBotResultTopic      = "bot.result"
	gateTurnStateTopic      = "agent.turn.state"
)

// emitGoalExecutor is a minimal BotExecutor for the employee gate. Self-
// contained so this gate does not depend on goal_loop_test.go fixtures.
type emitGoalExecutor struct {
	output string
	tokens int
	err    error
}

func (e *emitGoalExecutor) ExecuteBot(_ context.Context, _, _ string) (string, int, error) {
	if e.err != nil {
		return "", 0, e.err
	}
	return e.output, e.tokens, nil
}

// subscribeGateRaw subscribes to a topic by name and returns the raw subscriber.
func subscribeGateRaw(t *testing.T, b *bus.MessageBus, id, topic string) *bus.Subscriber {
	t.Helper()
	sub := b.Subscribe(id, topic)
	if sub == nil {
		t.Fatalf("subscribe to %q returned nil", topic)
	}
	return sub
}

// recvGateEvent waits for one message with a bounded timeout, decodes its
// payload into dst, and returns the source.
func recvGateEvent(t *testing.T, sub *bus.Subscriber, dst any, what string) string {
	t.Helper()
	select {
	case m, ok := <-sub.Channel:
		if !ok {
			t.Fatalf("channel closed before %s event arrived", what)
		}
		if err := json.Unmarshal(m.Payload, dst); err != nil {
			t.Fatalf("decode %s payload: %v (raw=%s)", what, err, m.Payload)
		}
		return m.Source
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s event", what)
	}
	return ""
}

// recvGateEventRaw is recvGateEvent without decoding, for source + kind checks.
func recvGateEventRaw(t *testing.T, sub *bus.Subscriber, what string) *models.BusMessage {
	t.Helper()
	select {
	case m, ok := <-sub.Channel:
		if !ok {
			t.Fatalf("channel closed before %s event arrived", what)
		}
		return m
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s event", what)
	}
	return nil
}

// assertNoGateEvent fails if a message was published to sub.
func assertNoGateEvent(t *testing.T, sub *bus.Subscriber, what string) {
	t.Helper()
	select {
	case m, ok := <-sub.Channel:
		if !ok {
			return
		}
		t.Fatalf("unexpected %s event: source=%q payload=%s", what, m.Source, m.Payload)
	case <-time.After(150 * time.Millisecond):
	}
}

// gateResultShape mirrors the frozen C1 wire payload (no kind field).
type gateResultShape struct {
	AgentID    string `json:"agent_id"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	TokensUsed int    `json:"tokens_used"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	StopReason string `json:"stop_reason,omitempty"`
	TriggerRef string `json:"trigger_ref,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Timestamp  string `json:"timestamp"`
}

// gateTurnStateShape mirrors the frozen #139 TurnStateEvent wire payload.
type gateTurnStateShape struct {
	AgentID    string `json:"agent_id"`
	SessionID  string `json:"session_id,omitempty"`
	ChildID    string `json:"child_id,omitempty"`
	State      string `json:"state"`
	StopReason string `json:"stop_reason,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Timestamp  string `json:"timestamp"`
}

// assertGateNoKindField fails if a result payload carries the forbidden
// "kind" key; the topic is the sole tier discriminator.
func assertGateNoKindField(t *testing.T, payload json.RawMessage) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode result payload into fields: %v", err)
	}
	if _, ok := fields["kind"]; ok {
		t.Fatalf("result payload must not carry a \"kind\" field: %s", payload)
	}
}

// drainGateTurnStates reads exactly two edges and asserts running -> idle
// with the given stop reason and no phase.
func drainGateTurnStates(t *testing.T, sub *bus.Subscriber, wantAgentID, wantStopReason string) {
	t.Helper()
	var first, second gateTurnStateShape
	recvGateEvent(t, sub, &first, "turn-state (running)")
	recvGateEvent(t, sub, &second, "turn-state (idle)")

	if first.State != string(agent.TurnStateRunning) {
		t.Errorf("first turn-state = %q, want %q", first.State, agent.TurnStateRunning)
	}
	if first.AgentID != wantAgentID {
		t.Errorf("running turn-state agent_id = %q, want %q", first.AgentID, wantAgentID)
	}
	if second.State != string(agent.TurnStateIdle) {
		t.Errorf("second turn-state = %q, want %q", second.State, agent.TurnStateIdle)
	}
	if second.AgentID != wantAgentID {
		t.Errorf("idle turn-state agent_id = %q, want %q", second.AgentID, wantAgentID)
	}
	if second.StopReason != wantStopReason {
		t.Errorf("idle turn-state stop_reason = %q, want %q", second.StopReason, wantStopReason)
	}
	if second.Phase != "" {
		t.Errorf("idle turn-state must carry no phase, got %q", second.Phase)
	}
}

// TestGoalLoop_EmitsResultAndTurnState (F4/T2) pins: on a successful employee
// execution GoalLoop publishes exactly one result event on employee.result
// with source "employee" and status=completed / stop_reason=end_turn, plus
// running and idle edges on agent.turn.state. No result leaks onto bot.result.
func TestGoalLoop_EmitsResultAndTurnState(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	employeeResults := subscribeGateRaw(t, b, "gate-emp-result", gateEmployeeResultTopic)
	defer bus.Unsubscribe(b, employeeResults)
	botResults := subscribeGateRaw(t, b, "gate-emp-bot-result", gateBotResultTopic)
	defer bus.Unsubscribe(b, botResults)
	turnStates := subscribeGateRaw(t, b, "gate-emp-turn", gateTurnStateTopic)
	defer bus.Unsubscribe(b, turnStates)

	exec := &emitGoalExecutor{output: "employee done", tokens: 88}
	loop := NewGoalLoop("gate-employee", testTier1Constitution(), nil, nil).
		WithExecutor(exec)

	// PINNED: the bus seam must exist, be nil-safe, and accept *bus.MessageBus.
	loop.SetBus(b)

	if _, err := loop.Execute(context.Background(), PlanRef{ID: "p-gate", State: "executing"}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	resultMsg := recvGateEventRaw(t, employeeResults, "employee.result")
	if resultMsg.Source != "employee" {
		t.Errorf("employee.result source = %q, want %q", resultMsg.Source, "employee")
	}
	assertGateNoKindField(t, resultMsg.Payload)

	var result gateResultShape
	if err := json.Unmarshal(resultMsg.Payload, &result); err != nil {
		t.Fatalf("decode employee.result payload: %v", err)
	}
	if result.AgentID != "gate-employee" {
		t.Errorf("employee.result agent_id = %q, want %q", result.AgentID, "gate-employee")
	}
	if result.Status != "completed" {
		t.Errorf("employee.result status = %q, want %q", result.Status, "completed")
	}
	if result.StopReason != string(agent.StopReasonEndTurn) {
		t.Errorf("employee.result stop_reason = %q, want %q", result.StopReason, agent.StopReasonEndTurn)
	}
	if result.Output != "employee done" {
		t.Errorf("employee.result output = %q, want %q", result.Output, "employee done")
	}
	if result.TokensUsed != 88 {
		t.Errorf("employee.result tokens_used = %d, want 88", result.TokensUsed)
	}

	drainGateTurnStates(t, turnStates, "gate-employee", string(agent.StopReasonEndTurn))
	assertNoGateEvent(t, botResults, "bot.result (employee must not leak)")
}

// TestGoalLoop_EmitsResultOnFailure pins: an executor error still produces a
// result event on employee.result with status=failed / stop_reason=error, and
// the idle turn-state carries stop_reason=error.
func TestGoalLoop_EmitsResultOnFailure(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	employeeResults := subscribeGateRaw(t, b, "gate-emp-fail-result", gateEmployeeResultTopic)
	defer bus.Unsubscribe(b, employeeResults)
	turnStates := subscribeGateRaw(t, b, "gate-emp-fail-turn", gateTurnStateTopic)
	defer bus.Unsubscribe(b, turnStates)

	exec := &emitGoalExecutor{err: errors.New("employee exec failed")}
	loop := NewGoalLoop("gate-employee-fail", testTier1Constitution(), nil, nil).
		WithExecutor(exec)
	loop.SetBus(b)

	if _, err := loop.Execute(context.Background(), PlanRef{ID: "p-gate-fail"}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	resultMsg := recvGateEventRaw(t, employeeResults, "failure employee.result")
	if resultMsg.Source != "employee" {
		t.Errorf("employee.result source = %q, want %q", resultMsg.Source, "employee")
	}
	assertGateNoKindField(t, resultMsg.Payload)

	var result gateResultShape
	if err := json.Unmarshal(resultMsg.Payload, &result); err != nil {
		t.Fatalf("decode failure employee.result payload: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("employee.result status = %q, want %q", result.Status, "failed")
	}
	if result.StopReason != string(agent.StopReasonError) {
		t.Errorf("employee.result stop_reason = %q, want %q", result.StopReason, agent.StopReasonError)
	}
	if result.Error == "" {
		t.Error("employee.result error must carry the executor error text")
	}

	drainGateTurnStates(t, turnStates, "gate-employee-fail", string(agent.StopReasonError))
}

// TestGoalLoop_SetBusNilIsNoOp pins the nil=no-op contract: a loop with a nil
// bus still executes without panicking.
func TestGoalLoop_SetBusNilIsNoOp(t *testing.T) {
	loop := NewGoalLoop("gate-employee-nil", testTier1Constitution(), nil, nil).
		WithExecutor(&emitGoalExecutor{output: "ok", tokens: 1})
	loop.SetBus(nil)

	result, err := loop.Execute(context.Background(), PlanRef{ID: "p-gate-nil"})
	if err != nil {
		t.Fatalf("Execute with nil bus returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful execution with nil bus, got %+v", result)
	}
}
