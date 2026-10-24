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
		{"agent.progress", "agent_progress"},
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

// TestWSClassAgentProgressRawFrame pins the 2026-10-24 legacy-table fix:
// a raw agent.progress bus frame — the exact acp_executor.go
// publishACPUpdate shape (stage=acp + detail + agent_id + acp_agent +
// persona + conversation_id) — classifies as agent_progress, NOT the
// generic "event" bucket it fell into before. The fix routes the frame
// onto the `progress` WS channel (wsEventChannel -> ShouldSend), so a
// connection that only granted `progress` for the session receives it.
// The payload keys must survive the transform verbatim (the GUI's
// AgentProgress.fromJson reads detail/acp_agent/persona directly).
func TestWSClassAgentProgressRawFrame(t *testing.T) {
	acpPayload := map[string]any{
		"conversation_id": "conv-acp-1",
		"stage":           "acp",
		"detail":          "streaming thought",
		"agent_id":        "bob",
		"acp_agent":       "hermes",
		"persona":         "worker",
		"timestamp":       "2026-10-24T00:00:00Z",
	}
	out := transformBusEventToWS(busMsg("agent.progress", acpPayload))
	if out == nil {
		t.Fatal("transformBusEventToWS(agent.progress) returned nil, want payload")
	}
	if got := out["type"]; got != "agent_progress" {
		t.Errorf("type = %v, want agent_progress (legacy table maps agent.progress)", got)
	}
	if got := out["source_topic"]; got != "agent.progress" {
		t.Errorf("source_topic = %v, want agent.progress", got)
	}
	// Passthrough keys the GUI/TUI surfaces read.
	for _, k := range []string{"stage", "detail", "agent_id", "acp_agent", "persona", "conversation_id"} {
		if out[k] != acpPayload[k] {
			t.Errorf("payload key %q = %v, want %v (must survive the transform)", k, out[k], acpPayload[k])
		}
	}

	// Channel routing: agent_progress resolves to the progress channel, so
	// a progress-only grant (what subscribeToAgentProgress arms) admits it.
	if ch := wsEventChannel("agent_progress"); ch != wsChannelProgress {
		t.Errorf("wsEventChannel(agent_progress) = %q, want %q", ch, wsChannelProgress)
	}
	// Channel scoping note: the GRANT is channel-agnostic (SubscribeSession
	// doc), so a session granted via chat alone still admits progress
	// frames — only the OPT-OUT ledger is per-channel. Assert the
	// progress-channel grant path, which is what subscribeToAgentProgress arms.
	hub := NewWebSocketHub(nil)
	wc := &wsConn{}
	hub.SubscribeSession(wc, "conv-acp-1", wsChannelProgress)
	if !hub.ShouldSend(wc, out["type"].(string), "conv-acp-1") {
		t.Error("ShouldSend: progress-channel grant must deliver the reclassified agent.progress frame")
	}
	// A progress-channel opt-out silences it (per-channel suppression).
	hub.UnsubscribeSession(wc, "conv-acp-1", wsChannelProgress)
	if hub.ShouldSend(wc, "agent_progress", "conv-acp-1") {
		t.Error("ShouldSend: progress-channel unsubscribe must suppress the frame")
	}
	// A chat-channel unsubscribe must NOT silence progress (H6 invariant).
	wc2 := &wsConn{}
	hub.SubscribeSession(wc2, "conv-acp-1", wsChannelProgress)
	hub.UnsubscribeSession(wc2, "conv-acp-1", wsChannelChat)
	if !hub.ShouldSend(wc2, "agent_progress", "conv-acp-1") {
		t.Error("ShouldSend: chat-channel unsubscribe must not mute the progress stream")
	}

	// Sibling topic stays dropped: agent.progress.synthesized must NOT
	// classify via this new case (dedicated handleWSProgress is its only
	// surface; ws-filter-06 pins no double-delivery).
	if out := transformBusEventToWS(busMsg("agent.progress.synthesized", acpPayload)); out != nil {
		t.Errorf("agent.progress.synthesized: got type=%v, want nil (single-surface pin)", out["type"])
	}

	// Non-acp loop progress frames (publishProgress shape: iteration/
	// token_count, no acp keys) reclassify too — same topic, same rule.
	loopPayload := map[string]any{
		"conversation_id": "conv-acp-1",
		"iteration":       3,
		"stage":           "executing",
		"detail":          "running tools...",
		"token_count":     120,
	}
	if out := transformBusEventToWS(busMsg("agent.progress", loopPayload)); out == nil || out["type"] != "agent_progress" {
		t.Errorf("loop-stage agent.progress: got %v, want type agent_progress", out)
	}
}
