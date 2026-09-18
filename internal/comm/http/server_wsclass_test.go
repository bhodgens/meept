package http

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/pkg/models"
)

func busMsg(topic string, payload any) *models.BusMessage {
	var raw json.RawMessage
	switch p := payload.(type) {
	case string: // pre-serialized raw bytes (e.g. corrupt JSON)
		raw = json.RawMessage(p)
	default:
		enc, err := json.Marshal(payload)
		if err != nil {
			panic(err)
		}
		raw = enc
	}
	// Topic set explicitly: the bus's publish() stamps it from the topic
	// argument, so every real relay message carries it.
	return &models.BusMessage{Topic: topic, Payload: raw}
}

// TestWSClassParity is the regression fence: every prefix case of the
// legacy classification table plus the default must produce byte-identical
// event types. A failure here means classification behavior changed — fix
// the implementation, never this table. (The Flutter client creates a
// visible bubble for every "chat_message" event; misclassification is a
// real bug.)
func TestWSClassParity(t *testing.T) {
	cases := []struct {
		topic string
		want  string
	}{
		{"chat_message", "chat_message"},
		{"chat.message.received", "chat_message"},
		{"chat.progress", "agent_progress"},
		// chat.response is DELIBERATELY NOT RELAYED (scopes-2 F-A): it is
		// an RPC reply topic consumed by ChatService for the HTTP body;
		// relaying it double-delivers the reply to HTTP+WS clients
		// (AGENTS.md invariant). transformBusEventToWS returns nil —
		// asserted separately below.
		{"agent.quota_wait", "agent_progress"},
		{"agent.model_escalated", "agent_progress"},
		{"turn.terminal", "agent_progress"},
		{"metrics.cpu", "metrics_update"},
		{"task.completed", "job_update"},
		{"step.started", "job_update"},
		{"job.done", "job_update"},
		{"queue.updated", "job_update"},
		{"plan.approved", "plan_update"},
		{"employee.notify", "event"}, // matches NO prefix case -> default
		{"unknown.topic", "event"},
	}
	for _, tc := range cases {
		out := transformBusEventToWS(busMsg(tc.topic, map[string]any{"k": "v"}))
		if out == nil {
			t.Errorf("%s: transformBusEventToWS returned nil", tc.topic)
			continue
		}
		if got := out["type"]; got != tc.want {
			t.Errorf("topic %q: type = %v, want %q", tc.topic, got, tc.want)
		}
	}
	// chat.response must return nil (never relayed), not a payload.
	if out := transformBusEventToWS(busMsg("chat.response", map[string]any{"k": "v"})); out != nil {
		t.Errorf("chat.response: got type=%v, want nil (not relayed to WS clients)", out["type"])
	}
}

// TestWSClassMarker is the RED-first proof: a topic with NO prefix-table
// match classifies agent_progress VIA the typed-decode -> marker path.
// The decode table is topic-keyed (payloads are raw JSON bytes — there is
// no typed value to assert without a topic entry), so the test registers
// a temporary entry for the unmapped topic, exactly as a future
// migration of that topic would. Pre-marker this hit the default "event";
// with the marker machinery the marker method (WSProgress), not any
// topic prefix, decides the classification.
func TestWSClassMarker(t *testing.T) {
	const unmapped = "future.unmapped"
	typedPayloadDecoders[unmapped] = decodeTurnTerminalWSClass
	defer delete(typedPayloadDecoders, unmapped)

	out := transformBusEventToWS(busMsg(unmapped, agent.TurnTerminalEvent{
		ConversationID: "c1",
		TurnID:         "t1",
		HandlerCase:    "direct",
		Status:         "completed",
		Reply:          "hi",
	}))
	if out == nil {
		t.Fatal("transformBusEventToWS returned nil")
	}
	if got := out["type"]; got != "agent_progress" {
		t.Errorf("marker-classified type = %v, want agent_progress", got)
	}
}

// TestWSClassCorruptPayload: a turn.terminal message whose payload bytes
// are invalid JSON for the typed decoder must still classify
// agent_progress via the prefix fallback, with no panic.
func TestWSClassCorruptPayload(t *testing.T) {
	out := transformBusEventToWS(busMsg("turn.terminal", "{not valid json"))
	if out == nil {
		t.Fatal("transformBusEventToWS returned nil")
	}
	if got := out["type"]; got != "agent_progress" {
		t.Errorf("corrupt-payload type = %v, want agent_progress (fallback)", got)
	}
}
