package http

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/pkg/models"
)

// Pins for the WS relay double-delivery and ack/filter-arm race fixes in
// server.go:
//
//  1. agent.progress.synthesized matched BOTH the generic "agent.*.*"
//     wildcard subscription (handleWSEvent → transformBusEventToWS, which
//     classified it as generic type "event") AND the dedicated
//     handleWSProgress subscription (typed agent_progress frame) — one
//     publish produced two frames on the same connection. transformBusEventToWS
//     now drops the topic so only the typed frame is emitted.
//
//  2. handleWSSubscribe sent the "subscribed" ack BEFORE
//     wsHub.SubscribeSession armed the per-session filter; a client firing a
//     turn on ack receipt could lose early events. The filter now arms
//     before the ack, so an event published immediately after the ack is
//     delivered.

// TestWSProgressSynthesized_SingleFrameNoGenericDuplicate publishes on
// agent.progress.synthesized and asserts the subscribed client receives
// exactly ONE frame — the typed agent_progress from handleWSProgress — with
// no generic "event" duplicate from the wildcard relay.
func TestWSProgressSynthesized_SingleFrameNoGenericDuplicate(t *testing.T) {
	baseURL, msgBus, cancel := startWSTestServer(t)
	defer cancel()

	conn := dialWSTestClient(t, baseURL)
	defer conn.Close()
	wsDrain(conn, 2*time.Second) // welcome

	wsSendSubscribe(t, conn, "session-dedupe")
	if msg, ok := wsReadOne(conn, 2*time.Second); !ok {
		t.Fatal("no subscribed confirmation")
	} else if msg["type"] != "subscribed" {
		t.Fatalf("expected subscribed ack, got %v", msg)
	}

	payload, _ := json.Marshal(agent.SynthesizedProgressEvent{
		SessionID:   "session-dedupe",
		AgentID:     "agent-1",
		Message:     "synthesized progress",
		SourceEvent: "test.source",
		Timestamp:   time.Now().UTC(),
	})
	msgBus.Publish("agent.progress.synthesized", &models.BusMessage{
		ID: "evt-synth-1", Type: models.MessageTypeEvent, Source: "test",
		Topic:     "agent.progress.synthesized",
		Timestamp: time.Now().UTC(), Payload: payload,
	})

	// First frame must be the typed agent_progress frame.
	first, ok := wsReadOne(conn, 2*time.Second)
	if !ok {
		t.Fatal("no frame delivered for agent.progress.synthesized")
	}
	if first["type"] != "agent_progress" {
		t.Fatalf("first frame type = %v, want agent_progress", first["type"])
	}
	if first["session_id"] != "session-dedupe" {
		t.Errorf("session_id = %v, want session-dedupe", first["session_id"])
	}
	if first["message"] != "synthesized progress" {
		t.Errorf("message = %v, want 'synthesized progress'", first["message"])
	}

	// Exactly ONE frame: the generic wildcard relay must not emit a second
	// (type "event") duplicate on the same connection.
	if dup, ok := wsReadOne(conn, 700*time.Millisecond); ok {
		t.Fatalf("duplicate frame after typed agent_progress: %v", dup)
	}
}

// TestWSSubscribe_FilterArmedBeforeAck proves the session filter is armed by
// the time the client sees the "subscribed" ack: an event published for the
// session immediately on ack receipt IS delivered.
func TestWSSubscribe_FilterArmedBeforeAck(t *testing.T) {
	baseURL, msgBus, cancel := startWSTestServer(t)
	defer cancel()

	conn := dialWSTestClient(t, baseURL)
	defer conn.Close()
	wsDrain(conn, 2*time.Second) // welcome

	wsSendSubscribe(t, conn, "session-ackrace")
	if msg, ok := wsReadOne(conn, 2*time.Second); !ok {
		t.Fatal("no subscribed confirmation")
	} else if msg["type"] != "subscribed" {
		t.Fatalf("expected subscribed ack, got %v", msg)
	}

	// Publish IMMEDIATELY after the ack — no settle sleep. Before the fix
	// the filter armed after the ack was written and this event could be
	// dropped; with the fix SubscribeSession runs before the ack send.
	payload, _ := json.Marshal(map[string]any{
		"session_id": "session-ackrace",
		"content":    "early event",
	})
	msgBus.Publish("chat_message", &models.BusMessage{
		ID: "evt-ackrace-1", Type: models.MessageTypeEvent, Source: "test",
		Topic:     "chat_message",
		Timestamp: time.Now().UTC(), Payload: payload,
	})

	got, ok := wsReadOne(conn, 2*time.Second)
	if !ok {
		t.Fatal("event published right after the subscribed ack was NOT delivered: session filter was not armed before the ack")
	}
	if got["type"] != "chat_message" {
		t.Fatalf("type = %v, want chat_message", got["type"])
	}
}
