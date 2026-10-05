package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
)

// waitForRoutingEvent drains a subscription channel until a message on the
// wanted topic arrives (or times out), returning its unmarshaled payload.
func waitForRoutingEvent(t *testing.T, sub *bus.Subscriber, topic string) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-sub.Channel:
			if msg.Topic != topic {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("failed to unmarshal %s payload: %v", topic, err)
			}
			return payload
		case <-deadline:
			t.Fatalf("timeout waiting for %s event", topic)
		}
	}
}

func TestRoutingTelemetry_RecordHintRoute(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sub := msgBus.Subscribe("test-routing-decision", "routing.decision")
	defer msgBus.Unsubscribe(sub)

	rt := NewRoutingTelemetry(msgBus, slogDiscardLogger())
	rt.RecordHintRoute("step-1", "coder", "hint_table")

	payload := waitForRoutingEvent(t, sub, "routing.decision")
	if payload["step_id"] != "step-1" {
		t.Errorf("expected step_id %q, got %v", "step-1", payload["step_id"])
	}
	if payload["agent_id"] != "coder" {
		t.Errorf("expected agent_id %q, got %v", "coder", payload["agent_id"])
	}
	if payload["source"] != "hint_table" {
		t.Errorf("expected source %q, got %v", "hint_table", payload["source"])
	}
}

func TestRoutingTelemetry_RecordHintMiss(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sub := msgBus.Subscribe("test-hint-miss", "routing.hint_miss")
	defer msgBus.Unsubscribe(sub)

	rt := NewRoutingTelemetry(msgBus, slogDiscardLogger())
	rt.RecordHintMiss("step-2", "nonexistent_hint_xyz")

	payload := waitForRoutingEvent(t, sub, "routing.hint_miss")
	if payload["step_id"] != "step-2" {
		t.Errorf("expected step_id %q, got %v", "step-2", payload["step_id"])
	}
	if payload["tool_hint"] != "nonexistent_hint_xyz" {
		t.Errorf("expected tool_hint %q, got %v", "nonexistent_hint_xyz", payload["tool_hint"])
	}
	if payload["resolved_to"] != "chat" {
		t.Errorf("expected resolved_to %q, got %v", "chat", payload["resolved_to"])
	}
}

func TestRoutingTelemetry_RecordHandoff(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sub := msgBus.Subscribe("test-handoff-outcome", "routing.handoff_outcome")
	defer msgBus.Unsubscribe(sub)

	rt := NewRoutingTelemetry(msgBus, slogDiscardLogger())
	rt.RecordHandoff("task-1", "step-from", "debugger", true)

	payload := waitForRoutingEvent(t, sub, "routing.handoff_outcome")
	if payload["task_id"] != "task-1" {
		t.Errorf("expected task_id %q, got %v", "task-1", payload["task_id"])
	}
	if payload["from_step_id"] != "step-from" {
		t.Errorf("expected from_step_id %q, got %v", "step-from", payload["from_step_id"])
	}
	if payload["to_agent_id"] != "debugger" {
		t.Errorf("expected to_agent_id %q, got %v", "debugger", payload["to_agent_id"])
	}
	if payload["accepted"] != true {
		t.Errorf("expected accepted true, got %v", payload["accepted"])
	}
}

func TestRoutingTelemetry_NilBusNoPanic(t *testing.T) {
	rt := NewRoutingTelemetry(nil, slogDiscardLogger())
	rt.RecordHintRoute("step-1", "coder", "hint_table")
	rt.RecordHintMiss("step-2", "bogus")
	rt.RecordHandoff("task-1", "step-from", "debugger", false)
}

func TestRoutingTelemetry_NilReceiverNoPanic(t *testing.T) {
	var rt *RoutingTelemetry
	rt.RecordHintRoute("step-1", "coder", "hint_table")
	rt.RecordHintMiss("step-2", "bogus")
	rt.RecordHandoff("task-1", "step-from", "debugger", false)
}

func TestRoutingTelemetry_NilLoggerNoPanic(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	sub := msgBus.Subscribe("test-nil-logger", "routing.decision")
	defer msgBus.Unsubscribe(sub)

	rt := NewRoutingTelemetry(msgBus, nil)
	rt.RecordHintRoute("step-1", "coder", "hint_table")

	waitForRoutingEvent(t, sub, "routing.decision")
}

// TestTacticalScheduler_RoutingTelemetry_WirePoints drives the scheduler's
// selectAgent/assignStepAgent paths and asserts the expected routing events.
func TestTacticalScheduler_RoutingTelemetry_WirePoints(t *testing.T) {
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	decisionSub := msgBus.Subscribe("test-wire-decision", "routing.decision")
	defer msgBus.Unsubscribe(decisionSub)
	missSub := msgBus.Subscribe("test-wire-miss", "routing.hint_miss")
	defer msgBus.Unsubscribe(missSub)

	ts := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore: nil,
		TaskStore: nil,
		Queue:     nil,
		Bus:       msgBus,
		Logger:    slogDiscardLogger(),
	})

	// (a) Routed hint: "code" hits the hint table → hint_table decision.
	hintStep := &task.TaskStep{ID: "step-hint", ToolHint: "code"}
	if got := ts.selectAgent(hintStep); got != config.AgentIDCoder {
		t.Fatalf("selectAgent(code) = %q, want %q", got, config.AgentIDCoder)
	}
	payload := waitForRoutingEvent(t, decisionSub, "routing.decision")
	if payload["source"] != "hint_table" || payload["agent_id"] != config.AgentIDCoder || payload["step_id"] != "step-hint" {
		t.Errorf("unexpected hint_table decision payload: %v", payload)
	}

	// (b) Unrouted hint falls through to chat → hint_miss + chat_fallback.
	missStep := &task.TaskStep{ID: "step-miss", ToolHint: "nonexistent_hint_xyz"}
	if got := ts.selectAgent(missStep); got != config.AgentIDChat {
		t.Fatalf("selectAgent(unrouted) = %q, want %q", got, config.AgentIDChat)
	}
	missPayload := waitForRoutingEvent(t, missSub, "routing.hint_miss")
	if missPayload["tool_hint"] != "nonexistent_hint_xyz" || missPayload["resolved_to"] != "chat" {
		t.Errorf("unexpected hint_miss payload: %v", missPayload)
	}
	fallbackPayload := waitForRoutingEvent(t, decisionSub, "routing.decision")
	if fallbackPayload["source"] != "chat_fallback" || fallbackPayload["agent_id"] != config.AgentIDChat {
		t.Errorf("unexpected chat_fallback decision payload: %v", fallbackPayload)
	}

	// (c) Explicit assignment wins → "explicit" decision, no hint events.
	explicitStep := &task.TaskStep{ID: "step-explicit", ToolHint: "code"}
	explicitStep.AgentID = config.AgentIDDebugger
	ts.assignStepAgent(explicitStep)
	if explicitStep.AgentID != config.AgentIDDebugger {
		t.Fatalf("assignStepAgent clobbered explicit assignment: %q", explicitStep.AgentID)
	}
	explicitPayload := waitForRoutingEvent(t, decisionSub, "routing.decision")
	if explicitPayload["source"] != "explicit" || explicitPayload["agent_id"] != config.AgentIDDebugger {
		t.Errorf("unexpected explicit decision payload: %v", explicitPayload)
	}
}

// TestTacticalScheduler_RoutingTelemetry_HandoffOutcomes asserts
// routing.handoff_outcome events for one accepted handoff (direct path) and
// one rate-limited rejection (cap=1, second handoff rejected).
func TestTacticalScheduler_RoutingTelemetry_HandoffOutcomes(t *testing.T) {
	taskStore, stepStore := newTestTaskAndStepStore(t)
	msgBus := bus.New(nil, slogDiscardLogger())
	defer msgBus.Close()

	outcomeSub := msgBus.Subscribe("test-handoff-outcomes", "routing.handoff_outcome")
	defer msgBus.Unsubscribe(outcomeSub)

	tk := task.NewTask("task-handoff-tel", "handoff telemetry task")
	tk.TotalJobs = 1
	if err := taskStore.Create(tk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
		StepStore:       stepStore,
		TaskStore:       taskStore,
		Queue:           &mockQueue{},
		Bus:             msgBus,
		Logger:          slogDiscardLogger(),
		MaxHandoffSteps: 1,
	})

	// Accepted handoff: from-step in completed state so the new step promotes.
	fromStep := task.NewTaskStep(tk.ID, "initial coding step", 1)
	fromStep.State = task.StepCompleted
	fromStep.ToolHint = "code"
	if err := stepStore.Create(fromStep); err != nil {
		t.Fatalf("failed to create from step: %v", err)
	}

	acceptReq := HandoffRequest{
		TaskID:      tk.ID,
		FromStepID:  fromStep.ID,
		FromAgentID: config.AgentIDCoder,
		ToAgentID:   config.AgentIDDebugger,
		Description: "Debug the failing test",
	}
	if err := scheduler.HandleHandoff(context.Background(), handoffBusMsg(acceptReq)); err != nil {
		t.Fatalf("accepted handoff failed: %v", err)
	}

	acceptPayload := waitForRoutingEvent(t, outcomeSub, "routing.handoff_outcome")
	if acceptPayload["accepted"] != true {
		t.Errorf("expected accepted true, got %v", acceptPayload["accepted"])
	}
	if acceptPayload["task_id"] != tk.ID {
		t.Errorf("expected task_id %q, got %v", tk.ID, acceptPayload["task_id"])
	}
	if acceptPayload["from_step_id"] != fromStep.ID {
		t.Errorf("expected from_step_id %q, got %v", fromStep.ID, acceptPayload["from_step_id"])
	}
	if acceptPayload["to_agent_id"] != config.AgentIDDebugger {
		t.Errorf("expected to_agent_id %q, got %v", config.AgentIDDebugger, acceptPayload["to_agent_id"])
	}

	// Rate-limited rejection: maxHandoffSteps=1 and one handoff step already
	// exists, so the second handoff must be rejected with accepted=false.
	rejectReq := HandoffRequest{
		TaskID:      tk.ID,
		FromStepID:  fromStep.ID,
		FromAgentID: config.AgentIDCoder,
		ToAgentID:   config.AgentIDPlanner,
		Description: "Second handoff — should be rate limited",
	}
	if err := scheduler.HandleHandoff(context.Background(), handoffBusMsg(rejectReq)); err == nil {
		t.Fatal("expected rate-limit rejection, got nil error")
	}

	rejectPayload := waitForRoutingEvent(t, outcomeSub, "routing.handoff_outcome")
	if rejectPayload["accepted"] != false {
		t.Errorf("expected accepted false, got %v", rejectPayload["accepted"])
	}
	if rejectPayload["to_agent_id"] != config.AgentIDPlanner {
		t.Errorf("expected to_agent_id %q, got %v", config.AgentIDPlanner, rejectPayload["to_agent_id"])
	}
}
