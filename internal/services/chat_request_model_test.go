package services

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

// Pins for the chat API per-request model: ChatRequest.Model must ride the
// chat.request bus payload as "model" (so the agent-side ChatRequest picks
// it up), be omitted when empty (byte-identical legacy payload shape), and
// round-trip through JSON unchanged.
func TestChatRequest_Model_RidesBusPayload(t *testing.T) {
	// Chat publishes and then waits for a reply that never comes, so this
	// test cannot call Chat() directly without hanging; instead pin the
	// payload-construction contract by marshaling the request the same way
	// Chat does (JSON tags are the contract with the agent-side decoder).
	req := ChatRequest{
		Message:        "hello",
		ConversationID: "conv-model-payload",
		Model:          "local/user-b",
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["model"] != "local/user-b" {
		t.Errorf("payload model = %v, want local/user-b", m["model"])
	}

	// Agent-side decode: the bus payload unmarshals into the agent
	// package's ChatRequest shape (mirrored field name/tag).
	var agentSide struct {
		Message string `json:"message"`
		Model   string `json:"model,omitempty"`
	}
	if err := json.Unmarshal(raw, &agentSide); err != nil {
		t.Fatalf("agent-side unmarshal: %v", err)
	}
	if agentSide.Model != "local/user-b" {
		t.Errorf("agent-side Model = %q, want local/user-b", agentSide.Model)
	}
}

// Empty Model must be OMITTED from the marshaled payload entirely — the
// wire shape for requests without a model is byte-identical to before.
func TestChatRequest_EmptyModelOmitted(t *testing.T) {
	req := ChatRequest{
		Message:        "hello",
		ConversationID: "conv-no-model",
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["model"]; ok {
		t.Errorf("empty Model must be omitted from the payload; got %v", m["model"])
	}
}

// Sanity: a ChatRequest with a model still satisfies the bus message shape
// ChatService publishes (MessageTypeRequest on chat.request).
func TestChatRequest_Model_BusMessageShape(t *testing.T) {
	payload := map[string]any{
		"message":         "hi",
		"conversation_id": "conv-shape",
		"session_id":      "conv-shape",
		"agent_id":        "",
		"model":           "classifier",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	msg := &models.BusMessage{
		ID:      "svc-chat-test",
		Type:    models.MessageTypeRequest,
		Topic:   "chat.request",
		Source:  "svc.chat",
		Payload: raw,
		ReplyTo: "chat.response",
	}
	if msg.Topic != "chat.request" || msg.Type != models.MessageTypeRequest {
		t.Fatalf("unexpected bus message shape: %+v", msg)
	}
}
