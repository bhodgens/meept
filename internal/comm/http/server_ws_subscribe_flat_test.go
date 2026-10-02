package http

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/models"
	"golang.org/x/net/websocket"
)

// The Flutter GUI client sends subscribe/unsubscribe frames in a FLAT shape
// without the {type, data} envelope, e.g.
//
//	{"type":"subscribe","channel":"chat","session_id":"sess-1"}
//
// (ui/flutter_ui/lib/services/websocket_service.dart, subscribeToChat).
// Before this pin, handleWSSubscribe only parsed msg.Data, so for flat
// frames channel fell back to "all" and sessionID stayed "" — the
// server-side session filter NEVER armed for the GUI and correctness
// depended entirely on client-side stream filters.
//
// These tests dial a real TLS WS server (startWSTestServer) and prove the
// session filter arms for BOTH frame shapes. They publish on the
// "chat_message" topic, which is relayed through exactly one path
// (handleWSEvent → ShouldSendProgress per-connection filter), avoiding the
// wildcard-relay duplicate frames the agent.progress.* topics produce.
func TestWSSubscribe_FlatFrameArmsSessionFilter(t *testing.T) {
	baseURL, msgBus, cancel := startWSTestServer(t)
	defer cancel()

	conn := dialWSTestClient(t, baseURL)
	defer conn.Close()
	wsDrain(conn, 2*time.Second) // welcome

	// FLAT shape — exactly the bytes the Flutter client sends.
	if err := websocket.JSON.Send(conn, map[string]any{
		"type":       "subscribe",
		"channel":    "chat",
		"session_id": "session-flat",
	}); err != nil {
		t.Fatalf("send flat subscribe: %v", err)
	}

	// Wait for the subscribed confirmation, then give the handler a beat:
	// the ack is written BEFORE SubscribeSession registers the filter.
	if msg, ok := wsReadOne(conn, 2*time.Second); !ok {
		t.Fatal("no subscribed confirmation for flat frame")
	} else if msg["type"] != "subscribed" {
		t.Fatalf("expected subscribed ack, got %v", msg)
	}
	time.Sleep(100 * time.Millisecond)

	pubChat := func(id, session string) {
		payload, _ := json.Marshal(map[string]any{
			"session_id": session,
			"content":    "hello from " + session,
		})
		msgBus.Publish("chat_message", &models.BusMessage{
			ID: id, Type: models.MessageTypeEvent, Source: "test",
			Topic: "chat_message",
			Timestamp: time.Now().UTC(), Payload: payload,
		})
	}

	// chat_message for the subscribed session must be delivered.
	pubChat("evt-flat", "session-flat")
	if msg, ok := wsReadOne(conn, 2*time.Second); !ok {
		t.Error("flat-frame subscribe did NOT arm the session filter (no chat_message delivered)")
	} else if msg["type"] != "chat_message" {
		t.Errorf("type = %v, want chat_message", msg["type"])
	}

	// chat_message for a DIFFERENT session must be suppressed by the filter.
	pubChat("evt-other", "session-other")
	if msg, ok := wsReadOne(conn, 500*time.Millisecond); ok {
		t.Errorf("session filter not armed: got frame for other session: %v", msg)
	}
}

func TestWSSubscribe_EnvelopedFrameStillWorks(t *testing.T) {
	baseURL, msgBus, cancel := startWSTestServer(t)
	defer cancel()

	conn := dialWSTestClient(t, baseURL)
	defer conn.Close()
	wsDrain(conn, 2*time.Second) // welcome

	// ENVELOPED shape (backward compatibility) — existing CLI/TUI clients.
	data, _ := json.Marshal(map[string]string{
		"channel":    "chat",
		"session_id": "session-env",
	})
	if err := websocket.JSON.Send(conn, map[string]any{
		"type": "subscribe", "data": json.RawMessage(data),
	}); err != nil {
		t.Fatalf("send enveloped subscribe: %v", err)
	}
	if msg, ok := wsReadOne(conn, 2*time.Second); !ok || msg["type"] != "subscribed" {
		t.Fatalf("enveloped subscribe: no subscribed ack (ok=%v msg=%v)", ok, msg)
	}
	time.Sleep(100 * time.Millisecond)

	payload, _ := json.Marshal(map[string]any{
		"session_id": "session-env",
		"content":    "hello enveloped",
	})
	msgBus.Publish("chat_message", &models.BusMessage{
		ID: "evt-env", Type: models.MessageTypeEvent, Source: "test",
		Topic: "chat_message",
		Timestamp: time.Now().UTC(), Payload: payload,
	})
	if msg, ok := wsReadOne(conn, 2*time.Second); !ok {
		t.Error("enveloped subscribe regressed: no chat_message delivered")
	} else if msg["type"] != "chat_message" {
		t.Errorf("type = %v, want chat_message", msg["type"])
	}
}

func TestWSUnsubscribe_FlatFrameRemovesSessionFilter(t *testing.T) {
	baseURL, msgBus, cancel := startWSTestServer(t)
	defer cancel()

	conn := dialWSTestClient(t, baseURL)
	defer conn.Close()
	wsDrain(conn, 2*time.Second) // welcome

	pubChat := func(id, session string) {
		payload, _ := json.Marshal(map[string]any{
			"session_id": session,
			"content":    "hello from " + session,
		})
		msgBus.Publish("chat_message", &models.BusMessage{
			ID: id, Type: models.MessageTypeEvent, Source: "test",
			Topic: "chat_message",
			Timestamp: time.Now().UTC(), Payload: payload,
		})
	}

	// Arm via flat subscribe.
	if err := websocket.JSON.Send(conn, map[string]any{
		"type": "subscribe", "channel": "progress", "session_id": "session-u",
	}); err != nil {
		t.Fatalf("send flat subscribe: %v", err)
	}
	if _, ok := wsReadOne(conn, 2*time.Second); !ok {
		t.Fatal("no subscribed ack")
	}
	time.Sleep(100 * time.Millisecond)

	// While armed, session-other frames are suppressed.
	pubChat("evt-pre", "session-other")
	if msg, ok := wsReadOne(conn, 500*time.Millisecond); ok {
		t.Fatalf("precondition failed: filter not armed, got %v", msg)
	}

	// Remove via flat unsubscribe (Flutter client shape).
	if err := websocket.JSON.Send(conn, map[string]any{
		"type": "unsubscribe", "channel": "progress", "session_id": "session-u",
	}); err != nil {
		t.Fatalf("send flat unsubscribe: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// After the flat unsubscribe the specific filter is gone, but the
	// connection's empty filter map is deliberately left in place (least-
	// surprise opt-out, pinned by the ws-filter-05 e2e scenario): the
	// previously-suppressed session-other frame must STAY suppressed. The
	// removal itself is proven by re-subscribing: once session-other is
	// explicitly armed, its frames deliver again.
	pubChat("evt-post", "session-other")
	if msg, ok := wsReadOne(conn, 500*time.Millisecond); ok {
		t.Fatalf("flat-frame unsubscribe did not suppress session-other after opt-out (broadcast leaked): %v", msg)
	}

	if err := websocket.JSON.Send(conn, map[string]any{
		"type": "subscribe", "channel": "progress", "session_id": "session-other",
	}); err != nil {
		t.Fatalf("re-subscribe: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	wsDrain(conn, 2*time.Second) // subscribed ack for the re-arm

	pubChat("evt-re", "session-other")
	if msg, ok := wsReadOne(conn, 2*time.Second); !ok {
		t.Error("re-subscribed session-other frame not delivered")
	} else if msg["type"] != "chat_message" {
		t.Errorf("type = %v, want chat_message", msg["type"])
	}
}
