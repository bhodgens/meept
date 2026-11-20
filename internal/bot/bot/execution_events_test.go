package bot

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

// Frozen topic names from master C1/C2. By subscribing by NAME (not via the
// agent.TopicBotResult / agent.TopicEmployeeResult vars, which are owned by
// branch 01 and not committed at gate time) this gate fails RED only on the
// pinned SetBus seam, not on missing topic declarations.
const (
	emitGateBotResultTopic      = "bot.result"
	emitGateEmployeeResultTopic = "employee.result"
	emitGateTurnStateTopic      = "agent.turn.state"
)

// emitTestExecutor is a minimal BotExecutor that returns a canned output or
// error. Self-contained so this gate does not depend on runner_test.go fixtures.
type emitTestExecutor struct {
	output string
	tokens int
	err    error
}

func (e *emitTestExecutor) ExecuteBot(_ context.Context, _, _ string) (string, int, error) {
	if e.err != nil {
		return "", 0, e.err
	}
	return e.output, e.tokens, nil
}

// subscribeTopicRaw subscribes to a topic by name and returns the raw subscriber.
func subscribeTopicRaw(t *testing.T, b *bus.MessageBus, id, topic string) *bus.Subscriber {
	t.Helper()
	sub := b.Subscribe(id, topic)
	if sub == nil {
		t.Fatalf("subscribe to %q returned nil", topic)
	}
	return sub
}

// recvBusEvent waits for one message on sub with a bounded timeout, decodes
// its payload into dst, and returns the message source.
func recvBusEvent(t *testing.T, sub *bus.Subscriber, dst any, what string) string {
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

// recvBusEventRaw is recvBusEvent without decoding; used to inspect the raw
// payload for the forbidden "kind" field.
func recvBusEventRaw(t *testing.T, sub *bus.Subscriber, what string) *models.BusMessage {
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

// assertNoBusEvent fails if a message has been published to sub. Pins that a
// bot result never leaks onto the employee topic and vice versa.
func assertNoBusEvent(t *testing.T, sub *bus.Subscriber, what string) {
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

// executionResultClientShape mirrors the frozen C1 wire payload. Decoding into
// an untyped mirror keeps this gate independent of the internal/agent type
// declaration (owned by branch 01); the JSON tags below are the frozen contract.
type executionResultClientShape struct {
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

// turnStateClientShape mirrors the frozen #139 TurnStateEvent wire payload.
type turnStateClientShape struct {
	AgentID    string `json:"agent_id"`
	SessionID  string `json:"session_id,omitempty"`
	ChildID    string `json:"child_id,omitempty"`
	State      string `json:"state"`
	StopReason string `json:"stop_reason,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Timestamp  string `json:"timestamp"`
}

// assertNoKindField fails if a result payload carries a "kind" key. The topic
// name is the sole tier discriminator; there is deliberately no kind field.
func assertNoKindField(t *testing.T, payload json.RawMessage) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatalf("decode result payload into fields: %v", err)
	}
	if _, ok := fields["kind"]; ok {
		t.Fatalf("result payload must not carry a \"kind\" field: %s", payload)
	}
}

// drainTurnStates reads exactly two turn-state edges and asserts the frozen
// running -> idle sequence with the given stop reason.
func drainTurnStates(t *testing.T, sub *bus.Subscriber, wantAgentID, wantStopReason string) {
	t.Helper()
	var first, second turnStateClientShape
	recvBusEvent(t, sub, &first, "turn-state (running)")
	recvBusEvent(t, sub, &second, "turn-state (idle)")

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

// TestBotRunner_EmitsResultAndTurnState (F3/T1) pins: on a successful bot
// execution the runner publishes exactly one result event on bot.result with
// source "bot" and status=completed / stop_reason=end_turn, plus running and
// idle edges on agent.turn.state. No result leaks onto employee.result.
func TestBotRunner_EmitsResultAndTurnState(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	botResults := subscribeTopicRaw(t, b, "gate-bot-result", emitGateBotResultTopic)
	defer bus.Unsubscribe(b, botResults)
	employeeResults := subscribeTopicRaw(t, b, "gate-bot-employee-result", emitGateEmployeeResultTopic)
	defer bus.Unsubscribe(b, employeeResults)
	turnStates := subscribeTopicRaw(t, b, "gate-bot-turn", emitGateTurnStateTopic)
	defer bus.Unsubscribe(b, turnStates)

	exec := &emitTestExecutor{output: "ci is green", tokens: 42}
	def := BotDefinition{ID: "gate-bot", Prompt: "test"}
	runner := NewBotRunner(def).WithExecutor(exec)

	// PINNED: the bus seam must exist, be nil-safe, and accept *bus.MessageBus.
	runner.SetBus(b)

	if _, err := runner.Execute(context.Background(), &BotState{}, "trigger"); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	resultMsg := recvBusEventRaw(t, botResults, "bot.result")
	if resultMsg.Source != "bot" {
		t.Errorf("bot.result source = %q, want %q", resultMsg.Source, "bot")
	}
	assertNoKindField(t, resultMsg.Payload)

	var result executionResultClientShape
	if err := json.Unmarshal(resultMsg.Payload, &result); err != nil {
		t.Fatalf("decode bot.result payload: %v", err)
	}
	if result.AgentID != "gate-bot" {
		t.Errorf("bot.result agent_id = %q, want %q", result.AgentID, "gate-bot")
	}
	if result.Status != "completed" {
		t.Errorf("bot.result status = %q, want %q", result.Status, "completed")
	}
	if result.StopReason != string(agent.StopReasonEndTurn) {
		t.Errorf("bot.result stop_reason = %q, want %q", result.StopReason, agent.StopReasonEndTurn)
	}
	if result.Output != "ci is green" {
		t.Errorf("bot.result output = %q, want %q", result.Output, "ci is green")
	}
	if result.TokensUsed != 42 {
		t.Errorf("bot.result tokens_used = %d, want 42", result.TokensUsed)
	}

	drainTurnStates(t, turnStates, "gate-bot", string(agent.StopReasonEndTurn))
	assertNoBusEvent(t, employeeResults, "employee.result (bot must not leak)")
}

// TestBotRunner_EmitsResultOnFailure (F3/T3) pins: an executor error still
// produces a result event on bot.result with status=failed / stop_reason=error,
// and the idle turn-state carries stop_reason=error.
func TestBotRunner_EmitsResultOnFailure(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	botResults := subscribeTopicRaw(t, b, "gate-bot-fail-result", emitGateBotResultTopic)
	defer bus.Unsubscribe(b, botResults)
	turnStates := subscribeTopicRaw(t, b, "gate-bot-fail-turn", emitGateTurnStateTopic)
	defer bus.Unsubscribe(b, turnStates)

	exec := &emitTestExecutor{err: errors.New("executor exploded")}
	def := BotDefinition{ID: "gate-bot-fail", Prompt: "test"}
	runner := NewBotRunner(def).WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 0, RetryBackoff: time.Millisecond})
	runner.SetBus(b)

	if _, err := runner.Execute(context.Background(), &BotState{}, "trigger"); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	resultMsg := recvBusEventRaw(t, botResults, "failure bot.result")
	if resultMsg.Source != "bot" {
		t.Errorf("bot.result source = %q, want %q", resultMsg.Source, "bot")
	}
	assertNoKindField(t, resultMsg.Payload)

	var result executionResultClientShape
	if err := json.Unmarshal(resultMsg.Payload, &result); err != nil {
		t.Fatalf("decode failure bot.result payload: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("bot.result status = %q, want %q", result.Status, "failed")
	}
	if result.StopReason != string(agent.StopReasonError) {
		t.Errorf("bot.result stop_reason = %q, want %q", result.StopReason, agent.StopReasonError)
	}
	if result.Error == "" {
		t.Error("bot.result error must carry the executor error text")
	}

	drainTurnStates(t, turnStates, "gate-bot-fail", string(agent.StopReasonError))
}

// TestBotRunner_SetBusNilIsNoOp pins the nil=no-op contract: a runner with a
// nil bus still executes without panicking.
func TestBotRunner_SetBusNilIsNoOp(t *testing.T) {
	runner := NewBotRunner(BotDefinition{ID: "gate-bot-nil", Prompt: "test"}).
		WithExecutor(&emitTestExecutor{output: "ok", tokens: 1})
	runner.SetBus(nil)

	result, err := runner.Execute(context.Background(), &BotState{}, "trigger")
	if err != nil {
		t.Fatalf("Execute with nil bus returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful execution with nil bus, got %+v", result)
	}
}
