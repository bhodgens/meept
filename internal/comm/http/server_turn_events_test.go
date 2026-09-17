package http

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

func mustJSONMap(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return b
}

// TestTransformBusEventToWS_TurnTerminalIsAgentProgress pins the WS
// classification contract for turn lifecycle topics (turn.terminal,
// async-turn-migration step 1 / leaf 01-turn-terminal-event Task 4):
// turn.* must classify as agent_progress — NEVER chat_message — per the
// AGENTS.md WS classification invariant (lifecycle topics that render as
// chat_message produce blank bubbles).
func TestTransformBusEventToWS_TurnTerminalIsAgentProgress(t *testing.T) {
	msg := &models.BusMessage{
		Topic:   "turn.terminal",
		Payload: mustJSONMap(t, map[string]any{"conversation_id": "c1", "turn_id": "t1", "status": "completed", "reply": "x"}),
	}
	out := transformBusEventToWS(msg)
	if out == nil {
		t.Fatal("transformBusEventToWS returned nil, want payload")
	}
	if out["type"] != "agent_progress" {
		t.Errorf("type = %v, want agent_progress", out["type"])
	}
	if out["type"] == "chat_message" {
		t.Error("turn.terminal classified as chat_message — must never render as a chat bubble")
	}
	if out["source_topic"] != "turn.terminal" {
		t.Errorf("source_topic = %v, want turn.terminal", out["source_topic"])
	}
}

// TestTransformBusEventToWS_TurnPrefixIsAgentProgress extends the same
// guarantee to the whole turn.* topic family (future lifecycle events ride
// the same prefix classification).
func TestTransformBusEventToWS_TurnPrefixIsAgentProgress(t *testing.T) {
	for _, topic := range []string{"turn.terminal", "turn.started", "turn.parked"} {
		msg := &models.BusMessage{
			Topic:   topic,
			Payload: mustJSONMap(t, map[string]any{"conversation_id": "c1"}),
		}
		out := transformBusEventToWS(msg)
		if out == nil {
			t.Errorf("topic %q: transformBusEventToWS returned nil", topic)
			continue
		}
		if out["type"] != "agent_progress" {
			t.Errorf("topic %q: type = %v, want agent_progress", topic, out["type"])
		}
	}
}
