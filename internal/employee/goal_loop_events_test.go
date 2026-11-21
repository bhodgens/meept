package employee

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

// TestGoalLoop_BotTierResultCarriesTriggerIdentity pins issue #160 M1 on the
// bot tier: a bus-triggered round that has an active goal and a speak
// context must publish trigger_ref (from the goal) and session_id (from
// SetSpeakContext) on bot.result, so the durable execution row carries the
// round's real identity (the GUI derives worktype from it).
func TestGoalLoop_BotTierResultCarriesTriggerIdentity(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	botResults := subscribeGateRaw(t, b, "gate-id-bot-result", gateBotResultTopic)
	defer bus.Unsubscribe(b, botResults)

	goal := &Goal{ID: "goal_identity_pin", EmployeeID: "gate-employee-id",
		State: GoalActive, TriggerRef: "intake:meept/meept#160"}
	loop := NewGoalLoop("gate-employee-id", testTier1Constitution(), nil, nil).
		WithExecutor(&emitGoalExecutor{output: "bot done", tokens: 5}).
		WithGoalLookup(goalLookupFunc(func(_ context.Context, _ string) (*Goal, error) {
			return goal, nil
		}))
	loop.SetBus(b)
	// Per-round tier + identity, exactly as the Manager's round-entry sets
	// them for a bus_event trigger.
	loop.SetResultTopic(agent.TopicBotResult.Name)
	loop.SetSpeakContext("sess-pin-1", "conv-pin-1")

	if _, err := loop.Execute(context.Background(), PlanRef{ID: "p-gate-id", State: "executing"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	resultMsg := recvGateEventRaw(t, botResults, "identity bot.result")
	var result gateResultShape
	if err := json.Unmarshal(resultMsg.Payload, &result); err != nil {
		t.Fatalf("decode identity bot.result payload: %v", err)
	}
	if result.TriggerRef != "intake:meept/meept#160" {
		t.Errorf("trigger_ref = %q, want %q (active goal's TriggerRef must ride the result)",
			result.TriggerRef, "intake:meept/meept#160")
	}
	if result.SessionID != "sess-pin-1" {
		t.Errorf("session_id = %q, want %q (SetSpeakContext session must ride the result)",
			result.SessionID, "sess-pin-1")
	}
}

// TestGoalLoop_ResultOmitsEmptyIdentity pins the other half of M1: a round
// with no active goal and no speak context publishes the identity fields
// ABSENT (omitempty), never invented or empty-string values on the wire.
func TestGoalLoop_ResultOmitsEmptyIdentity(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	employeeResults := subscribeGateRaw(t, b, "gate-id-none-result", gateEmployeeResultTopic)
	defer bus.Unsubscribe(b, employeeResults)

	// No goalLookup, no goal store -> lookupActiveGoal yields (nil, nil);
	// SetSpeakContext never called -> empty session.
	loop := NewGoalLoop("gate-employee-noid", testTier1Constitution(), nil, nil).
		WithExecutor(&emitGoalExecutor{output: "ok", tokens: 1})
	loop.SetBus(b)

	if _, err := loop.Execute(context.Background(), PlanRef{ID: "p-gate-noid", State: "executing"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	resultMsg := recvGateEventRaw(t, employeeResults, "no-identity employee.result")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(resultMsg.Payload, &fields); err != nil {
		t.Fatalf("decode payload into fields: %v", err)
	}
	if _, ok := fields["trigger_ref"]; ok {
		t.Errorf("trigger_ref must be omitted when no goal is active, got %s", fields["trigger_ref"])
	}
	if _, ok := fields["session_id"]; ok {
		t.Errorf("session_id must be omitted without a speak context, got %s", fields["session_id"])
	}
}

// TestGoalLoop_GoalLookupFailureWarnsAndPublishes pins the degradation
// contract of the M1 identity lookup: when the active-goal lookup ERRORS,
// the round logs a Warn, still publishes its result (without trigger_ref),
// and never drops the event or fails the execution.
func TestGoalLoop_GoalLookupFailureWarnsAndPublishes(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	employeeResults := subscribeGateRaw(t, b, "gate-id-fail-result", gateEmployeeResultTopic)
	defer bus.Unsubscribe(b, employeeResults)

	rec := &recordingHandler{}
	loop := NewGoalLoop("gate-employee-idfail", testTier1Constitution(), nil, slog.New(rec)).
		WithExecutor(&emitGoalExecutor{output: "ok", tokens: 1}).
		WithGoalLookup(goalLookupFunc(func(_ context.Context, _ string) (*Goal, error) {
			return nil, errors.New("goal store unreachable")
		}))
	loop.SetBus(b)

	if _, err := loop.Execute(context.Background(), PlanRef{ID: "p-gate-idfail", State: "executing"}); err != nil {
		t.Fatalf("Execute must succeed despite goal lookup failure: %v", err)
	}

	resultMsg := recvGateEventRaw(t, employeeResults, "employee.result after lookup failure")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(resultMsg.Payload, &fields); err != nil {
		t.Fatalf("decode payload into fields: %v", err)
	}
	if _, ok := fields["trigger_ref"]; ok {
		t.Errorf("failed goal lookup must publish WITHOUT trigger_ref, got %s", fields["trigger_ref"])
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	var warned bool
	for i := range rec.records {
		if rec.records[i].Level >= slog.LevelWarn &&
			rec.records[i].Message == "execute: active goal lookup for trigger_ref failed; publishing without trigger_ref" {
			warned = true
			break
		}
	}
	if !warned {
		t.Error("goal lookup failure must log the documented Warn (result kept, ref dropped)")
	}
}

// TestTriggerRef_BusRoundWithoutGoal pins a bus trigger with no active
// goal: the published trigger_ref must be the round's actual trigger,
// not empty. A one-line deletion of the roundTriggerRef fallback in
// Execute makes this fail (the field is omitted).
func TestTriggerRef_BusRoundWithoutGoal(t *testing.T) {
	b := bus.New(nil, nil)
	defer b.Close()

	botResults := subscribeGateRaw(t, b, "gate-trigref-bot", gateBotResultTopic)
	defer bus.Unsubscribe(b, botResults)

	reflector := newStubReflector()
	reflector.queueResponse(`{"candidates":[{"title":"x","description":"d","prompt":"handle the event"}]}`)

	loop := NewGoalLoop("trigref-bus", testTier1Constitution(), nil, slog.New(slog.DiscardHandler)).
		WithReflector(reflector).
		WithExecutor(&emitGoalExecutor{output: "handled", tokens: 4})
	loop.SetBus(b)
	// No goal lookup and no goal store: lookupActiveGoal yields (nil, nil).

	m := NewManager(nil)
	_, err := m.triggerViaGoalLoop(context.Background(), "trigref-bus", map[string]any{
		"source":      "bus_event",
		"trigger_ref": "bus_event:alerts.fired",
		"prompt":      "Event received on topic alerts.fired from bus",
	}, "tier_1_reactive", loop)
	if err != nil {
		t.Fatalf("triggerViaGoalLoop: %v", err)
	}

	resultMsg := recvGateEventRaw(t, botResults, "bus.result without a goal")
	var result gateResultShape
	if err := json.Unmarshal(resultMsg.Payload, &result); err != nil {
		t.Fatalf("decode bot.result: %v", err)
	}
	if result.TriggerRef == "" {
		t.Fatal("trigger_ref must be non-empty when a bus round has no active goal")
	}
	if result.TriggerRef != "bus_event:alerts.fired" {
		t.Errorf("trigger_ref = %q, want bus_event:alerts.fired", result.TriggerRef)
	}
}
