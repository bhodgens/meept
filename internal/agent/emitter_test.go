package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

func TestEventEmitter_Emit_NoListeners(t *testing.T) {
	emitter := NewEventEmitter("test-agent", nil, nil)

	// Should not panic with no listeners
	emitter.Emit(context.Background(), AgentEventTurnStart, TurnStartData{
		TurnNumber: 1,
	})
}

func TestEventEmitter_Emit_SyncListener(t *testing.T) {
	emitter := NewEventEmitter("test-agent", nil, nil)

	var called atomic.Int32
	emitter.On(AgentEventTurnStart, "test-listener", func(ctx context.Context, event AgentEvent) {
		called.Add(1)

		if event.Type != AgentEventTurnStart {
			t.Errorf("expected type %s, got %s", AgentEventTurnStart, event.Type)
		}
		if event.AgentID != "test-agent" {
			t.Errorf("expected agent_id test-agent, got %s", event.AgentID)
		}
		data, ok := event.Data.(TurnStartData)
		if !ok {
			t.Fatalf("expected TurnStartData, got %T", event.Data)
		}
		if data.TurnNumber != 1 {
			t.Errorf("expected turn 1, got %d", data.TurnNumber)
		}
	})

	emitter.Emit(context.Background(), AgentEventTurnStart, TurnStartData{
		TurnNumber: 1,
	})

	if called.Load() != 1 {
		t.Errorf("expected listener called once, got %d", called.Load())
	}
}

func TestEventEmitter_Emit_AsyncListener_WaitForIdle(t *testing.T) {
	emitter := NewEventEmitter("test-agent", nil, nil)

	var called atomic.Int32
	emitter.OnAsync(AgentEventTurnEnd, "async-listener", func(ctx context.Context, event AgentEvent) {
		time.Sleep(50 * time.Millisecond) // simulate work
		called.Add(1)
	})

	emitter.Emit(context.Background(), AgentEventTurnEnd, TurnEndData{
		TurnNumber: 1,
	})

	// WaitForIdle should block until the async listener completes
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := emitter.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle returned error: %v", err)
	}

	if called.Load() != 1 {
		t.Errorf("expected async listener called once after WaitForIdle, got %d", called.Load())
	}
}

func TestEventEmitter_Emit_BusBridge(t *testing.T) {
	msgBus := bus.New(bus.DefaultConfig(), nil)
	emitter := NewEventEmitter("test-agent", msgBus, nil)

	// Subscribe to the bus topic
	sub := msgBus.Subscribe("test-sub", BusTopic(AgentEventAgentStart))
	defer msgBus.Unsubscribe(sub)

	emitter.Emit(context.Background(), AgentEventAgentStart, AgentStartData{
		AgentID:   "test-agent",
		AgentType: "chat",
		ModelRef:  "gpt-4",
	})

	// Wait for the bus message
	select {
	case msg := <-sub.Channel:
		if msg.Topic != BusTopic(AgentEventAgentStart) {
			t.Errorf("expected topic %s, got %s", BusTopic(AgentEventAgentStart), msg.Topic)
		}
		// Verify payload contains the correct event type
		var raw struct {
			Type    AgentEventType `json:"type"`
			AgentID string         `json:"agent_id"`
		}
		if err := json.Unmarshal(msg.Payload, &raw); err != nil {
			t.Fatalf("failed to unmarshal payload: %v", err)
		}
		if raw.Type != AgentEventAgentStart {
			t.Errorf("expected type %s, got %s", AgentEventAgentStart, raw.Type)
		}
		if raw.AgentID != "test-agent" {
			t.Errorf("expected agent_id test-agent, got %s", raw.AgentID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for bus message")
	}
}

func TestEventEmitter_Emit_NilBus(t *testing.T) {
	emitter := NewEventEmitter("test-agent", nil, nil)

	var called atomic.Int32
	emitter.On(AgentEventAgentEnd, "listener", func(ctx context.Context, event AgentEvent) {
		called.Add(1)
	})

	// Should not panic with nil bus
	emitter.Emit(context.Background(), AgentEventAgentEnd, AgentEndData{
		AgentID: "test-agent",
		Reason:  "completed",
	})

	if called.Load() != 1 {
		t.Errorf("expected listener called, got %d", called.Load())
	}
}

func TestEventEmitter_WaitForIdle_NoPending(t *testing.T) {
	emitter := NewEventEmitter("test-agent", nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should return immediately with no pending async listeners
	if err := emitter.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle with no pending returned error: %v", err)
	}
}

func TestEventEmitter_Off_RemovesListener(t *testing.T) {
	emitter := NewEventEmitter("test-agent", nil, nil)

	var called atomic.Int32
	emitter.On(AgentEventTurnStart, "removable", func(ctx context.Context, event AgentEvent) {
		called.Add(1)
	})

	// Emit should trigger the listener
	emitter.Emit(context.Background(), AgentEventTurnStart, TurnStartData{})
	if called.Load() != 1 {
		t.Fatalf("expected listener called once, got %d", called.Load())
	}

	// Remove the listener
	emitter.Off("removable")

	// Emit should NOT trigger the listener
	emitter.Emit(context.Background(), AgentEventTurnStart, TurnStartData{})
	if called.Load() != 1 {
		t.Errorf("expected listener NOT called after Off, got %d", called.Load())
	}
}

func TestEventEmitter_OnAll(t *testing.T) {
	emitter := NewEventEmitter("test-agent", nil, nil)

	var turnStartCalled, agentEndCalled atomic.Int32
	emitter.OnAll("all-listener", func(ctx context.Context, event AgentEvent) {
		switch event.Type {
		case AgentEventTurnStart:
			turnStartCalled.Add(1)
		case AgentEventAgentEnd:
			agentEndCalled.Add(1)
		}
	})

	emitter.Emit(context.Background(), AgentEventTurnStart, TurnStartData{})
	emitter.Emit(context.Background(), AgentEventAgentEnd, AgentEndData{})

	if turnStartCalled.Load() != 1 {
		t.Errorf("expected all-listener called for turn_start, got %d", turnStartCalled.Load())
	}
	if agentEndCalled.Load() != 1 {
		t.Errorf("expected all-listener called for agent_end, got %d", agentEndCalled.Load())
	}
}

func TestEventEmitter_EmitWithFields(t *testing.T) {
	emitter := NewEventEmitter("test-agent", nil, nil)

	var receivedConvID string
	emitter.On(AgentEventTurnStart, "test", func(ctx context.Context, event AgentEvent) {
		receivedConvID = event.ConversationID
	})

	event := AgentEvent{
		Type:           AgentEventTurnStart,
		ConversationID: "conv-123",
		Iteration:      5,
		Data:           TurnStartData{TurnNumber: 5},
	}
	emitter.EmitWithFields(context.Background(), event)

	if receivedConvID != "conv-123" {
		t.Errorf("expected conversation_id conv-123, got %s", receivedConvID)
	}
}

func TestBusTopic(t *testing.T) {
	tests := []struct {
		eventType AgentEventType
		expected  string
	}{
		{AgentEventTurnStart, "agent.event.turn_start"},
		{AgentEventAgentStart, "agent.event.agent_start"},
		{AgentEventToolExecutionEnd, "agent.event.tool_execution_end"},
		{AgentEventSessionEnd, "agent.event.session_end"},
	}
	for _, tt := range tests {
		got := BusTopic(tt.eventType)
		if got != tt.expected {
			t.Errorf("BusTopic(%s) = %q, want %q", tt.eventType, got, tt.expected)
		}
	}
}

// Verify BusMessage is used correctly in test.
var _ *models.BusMessage = nil
