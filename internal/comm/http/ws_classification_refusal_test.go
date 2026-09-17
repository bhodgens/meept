package http

// Refusal-fallback tree leaf 04, Task 1: WS classification regression pin.
//
// Leaf 03's handleRefusal publishes the fallback event on the EXISTING
// agent.model_escalated topic with reason "refusal_fallback"
// (internal/agent/loop_refusal.go). transformBusEventToWS must classify that
// topic as "agent_progress" — never "chat_message" (a chat bubble would
// appear blank; AGENTS.md WS classification invariant) — so the fallback is
// visible to clients on the progress channel.
//
// This test is a PIN: it is expected to pass with no production change. If
// it fails, the prefix rule does not exist as documented and THAT is the
// finding.

import (
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

func TestWS_ClassifiesRefusalFallbackEvent(t *testing.T) {
	msgBus := bus.New(nil, slog.Default())
	defer msgBus.Close()

	cfg := DefaultServerConfig()
	cfg.Addr = ":0"
	srv := NewServer(cfg, nil, nil, nil, nil, nil, WithWebSocket(msgBus, "/ws"))
	if srv == nil {
		t.Fatal("failed to create server with WebSocket option")
	}

	topic := "agent.model_escalated"
	payload := map[string]any{
		"agent_id":   "agent-1",
		"from_model": "local/primary",
		"to_model":   "local/fb-model",
		"reason":     "refusal_fallback",
		"fix_loops":  0,
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "test", payload)
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	if got := msgBus.Publish(topic, msg); got < 1 {
		t.Fatalf("Publish delivered to %d subscribers, want >= 1 (WS bridge not subscribed)", got)
	}

	frontendData := transformBusEventToWS(msg)
	if frontendData == nil {
		t.Fatal("transformBusEventToWS returned nil, want payload")
	}
	if frontendData["type"] != "agent_progress" {
		t.Errorf("frontend type = %v, want \"agent_progress\"", frontendData["type"])
	}
	if frontendData["type"] == "chat_message" {
		t.Error("classified as chat_message; model_escalated topics must never render as chat bubbles")
	}
	if frontendData["source_topic"] != topic {
		t.Errorf("source_topic = %v, want %q", frontendData["source_topic"], topic)
	}
	if reason, ok := frontendData["reason"].(string); !ok || reason != "refusal_fallback" {
		t.Errorf("payload reason = %v, want \"refusal_fallback\" (payload must survive the transform)", frontendData["reason"])
	}
}
