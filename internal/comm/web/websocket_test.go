package web

import (
	"testing"
)

func TestWebSocketHub_New(t *testing.T) {
	hub := NewWebSocketHub(nil)
	if hub == nil {
		t.Fatalf("expected non-nil hub")
	}
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestWebSocketHub_Broadcast_NoClients_NoPanic(t *testing.T) {
	hub := NewWebSocketHub(nil)
	// Should not panic
	hub.Broadcast("status", map[string]string{"state": "running"})
	hub.Broadcast("chat", map[string]string{"msg": "hello"})
}

func TestWebSocketHub_RegisterUnregister_Count(t *testing.T) {
	hub := NewWebSocketHub(nil)
	// Without actual WebSocket connections, we can only test the hub's
	// basic state management.
	if count := hub.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients initially, got %d", count)
	}
}

func TestWSMessage_Type(t *testing.T) {
	msg := WSMessage{Type: "ping"}
	if msg.Type != "ping" {
		t.Fatalf("expected ping type")
	}
}
