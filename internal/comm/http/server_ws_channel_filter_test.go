package http

// Pins for the bughunt 2026-10-05 wave fixes to the WS relay:
//
//   - H6 (channel-blind session filter): the Flutter client subscribes to the
//     SAME session on three streams — subscribeToChat (channel `chat`),
//     subscribeToAgentProgress and subscribeToTurnTerminal (both channel
//     `progress`, websocket_service.dart:660/731/793) — and
//     unsubscribeFromChat (:677) sends ONE unsubscribe frame naming
//     `chat`. Before the fix that frame deleted the session from the
//     channel-blind filter map shared by all three, so navigating away from
//     a session in the GUI silently suppressed its progress and turn-terminal
//     too, and the turn false-stalled to the 120s liveness timer.
//
//   - H7 (session-less synthesized progress): event.SessionID derives from
//     the AgentEvent's ConversationID; an AgentEvent with an empty
//     ConversationID produced a "" filter key that matched no filter set, so
//     the typed agent.progress.synthesized relay dropped the line for every
//     armed connection while the generic wildcard relay broadcast it.

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
	"golang.org/x/net/websocket"
)

// wsSendFlat sends a raw client frame (the Flutter shape: no `data`
// envelope) and gives the read loop a beat to apply it. Unsubscribe frames
// get NO ack, so the sleep is the only sync point.
func wsSendFlat(t *testing.T, conn *websocket.Conn, frame map[string]any) {
	t.Helper()
	if err := websocket.JSON.Send(conn, frame); err != nil {
		t.Fatalf("ws send %v: %v", frame, err)
	}
	time.Sleep(150 * time.Millisecond)
}

// wsWaitForFrame polls for up to d and returns the first frame whose raw
// JSON contains marker, or nil when none arrives.
func wsWaitForFrame(t *testing.T, conn *websocket.Conn, marker string, d time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		msg, ok := wsReadOne(conn, min(200*time.Millisecond, max(time.Until(deadline), time.Millisecond)))
		if !ok {
			continue
		}
		if raw, err := json.Marshal(msg); err == nil && containsMarker(raw, marker) {
			return msg
		}
	}
	return nil
}

// wsAssertNoFrame polls for up to d and fails the test if any frame's raw
// JSON contains marker.
func wsAssertNoFrame(t *testing.T, conn *websocket.Conn, marker, what string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		msg, ok := wsReadOne(conn, min(200*time.Millisecond, max(time.Until(deadline), time.Millisecond)))
		if !ok {
			continue
		}
		if raw, err := json.Marshal(msg); err == nil && containsMarker(raw, marker) {
			t.Fatalf("%s leaked: %s", what, raw)
		}
	}
}

func containsMarker(raw []byte, marker string) bool {
	return bytes.Contains(raw, []byte(marker))
}

// publishChat publishes a chat_message for session (relayed through the
// generic wildcard relay, classified chat_message).
func publishChat(msgBus *bus.MessageBus, id, session, marker string) {
	payload, _ := json.Marshal(map[string]any{
		"session_id": session,
		"content":    marker,
	})
	msgBus.Publish("chat_message", &models.BusMessage{
		ID: id, Type: models.MessageTypeEvent, Source: "test",
		Topic:     "chat_message",
		Timestamp: time.Now().UTC(), Payload: payload,
	})
}

// publishSynth publishes an agent.progress.synthesized payload for session
// (an empty session yields a session-less AgentEvent).
func publishSynth(msgBus *bus.MessageBus, id, session, marker string) {
	payload, _ := json.Marshal(agent.SynthesizedProgressEvent{
		SessionID:   session,
		AgentID:     "pin-agent",
		Message:     marker,
		SourceEvent: "tool_execution_end",
		Timestamp:   time.Now().UTC(),
	})
	msgBus.Publish("agent.progress.synthesized", &models.BusMessage{
		ID: id, Type: models.MessageTypeEvent, Source: "test",
		Topic:     "agent.progress.synthesized",
		Timestamp: time.Now().UTC(), Payload: payload,
	})
}

// ---------------------------------------------------------------------------
// H6 pin — a chat unsubscribe must NOT silence progress/turn-terminal for
// the same session, and a progress unsubscribe must NOT silence chat.
// ---------------------------------------------------------------------------

func TestWSChannelFilter_ChatUnsubscribeKeepsProgressAndTerminal(t *testing.T) {
	baseURL, msgBus, cancel := startWSTestServer(t)
	defer cancel()

	conn := dialWSTestClient(t, baseURL)
	defer conn.Close()
	wsDrain(conn, 2*time.Second) // welcome

	const session = "session-h6-cross"
	// Arm all three streams, exactly as the GUI does.
	wsSendFlat(t, conn, map[string]any{"type": "subscribe", "channel": "chat", "session_id": session})
	wsSendFlat(t, conn, map[string]any{"type": "subscribe", "channel": "progress", "session_id": session})
	wsDrain(conn, 2*time.Second) // the two subscribed acks

	// Precondition: chat for the session delivers.
	publishChat(msgBus, "h6-pre", session, "H6-PRE-CHAT")
	if msg := wsWaitForFrame(t, conn, "H6-PRE-CHAT", 3*time.Second); msg == nil {
		t.Fatal("precondition failed: chat_message for the subscribed session was not delivered")
	}

	// The GUI navigates away from the session: ONE chat-channel unsubscribe.
	wsSendFlat(t, conn, map[string]any{"type": "unsubscribe", "channel": "chat", "session_id": session})

	// Chat is now muted for that session...
	publishChat(msgBus, "h6-chat", session, "H6-CHAT-MUTED")
	wsAssertNoFrame(t, conn, "H6-CHAT-MUTED", "chat_message after chat unsubscribe", 1500*time.Millisecond)

	// ...but progress for the SAME session must still arrive (spaced past the
	// 100ms rate-limit window).
	publishSynth(msgBus, "h6-synth", session, "H6-PROGRESS-STILL-ALIVE")
	if msg := wsWaitForFrame(t, conn, "H6-PROGRESS-STILL-ALIVE", 3*time.Second); msg == nil {
		t.Error("chat unsubscribe silenced the progress stream for the same session (H6)")
	} else if msg["type"] != "agent_progress" {
		t.Errorf("progress frame type = %v, want agent_progress", msg["type"])
	}

	// The turn-terminal stream rides the same `progress` channel and must
	// survive the chat unsubscribe too.
	time.Sleep(150 * time.Millisecond) // clear the per-connection rate limiter
	terminal, _ := json.Marshal(map[string]any{
		"session_id": session,
		"turn_id":    "turn-h6",
		"status":     "completed",
		"reply":      "H6-TERMINAL-STILL-ALIVE",
	})
	msgBus.Publish("turn.terminal", &models.BusMessage{
		ID: "h6-terminal", Type: models.MessageTypeEvent, Source: "test",
		Topic:     "turn.terminal",
		Timestamp: time.Now().UTC(), Payload: terminal,
	})
	if msg := wsWaitForFrame(t, conn, "H6-TERMINAL-STILL-ALIVE", 3*time.Second); msg == nil {
		t.Error("chat unsubscribe silenced the turn-terminal stream for the same session (H6)")
	} else if msg["type"] != "agent_progress" {
		t.Errorf("terminal frame type = %v, want agent_progress", msg["type"])
	}

	// The other direction: a PROGRESS unsubscribe must not mute chat. Run it
	// on a second session so the `chat` suppression recorded above (which
	// correctly still mutes THAT session's chat) cannot mask the assertion.
	const session2 = "session-h6-cross-2"
	wsSendFlat(t, conn, map[string]any{"type": "subscribe", "channel": "chat", "session_id": session2})
	wsSendFlat(t, conn, map[string]any{"type": "subscribe", "channel": "progress", "session_id": session2})
	wsDrain(conn, 2*time.Second) // the two subscribed acks

	wsSendFlat(t, conn, map[string]any{"type": "unsubscribe", "channel": "progress", "session_id": session2})
	publishChat(msgBus, "h6-chat2", session2, "H6-CHAT-STILL-ALIVE")
	if msg := wsWaitForFrame(t, conn, "H6-CHAT-STILL-ALIVE", 3*time.Second); msg == nil {
		t.Error("progress unsubscribe silenced the chat stream for the same session (H6)")
	} else if msg["type"] != "chat_message" {
		t.Errorf("chat frame type = %v, want chat_message", msg["type"])
	}

	// And progress IS muted now.
	publishSynth(msgBus, "h6-synth2", session2, "H6-PROGRESS-MUTED")
	wsAssertNoFrame(t, conn, "H6-PROGRESS-MUTED", "agent_progress after progress unsubscribe", 1500*time.Millisecond)

	// Re-subscribing one channel does NOT silently re-arm the other: the
	// `chat` opt-out on the FIRST session survives a `progress`-channel
	// re-subscribe, so chat stays muted and progress comes back.
	wsSendFlat(t, conn, map[string]any{"type": "subscribe", "channel": "progress", "session_id": session})
	wsDrain(conn, 2*time.Second) // subscribed ack
	time.Sleep(150 * time.Millisecond)
	publishSynth(msgBus, "h6-synth3", session, "H6-PROGRESS-REMUTED-OK")
	if msg := wsWaitForFrame(t, conn, "H6-PROGRESS-REMUTED-OK", 3*time.Second); msg == nil {
		t.Error("progress re-subscribe did not restore the progress stream")
	}
	publishChat(msgBus, "h6-chat3", session, "H6-CHAT-STILL-MUTED")
	wsAssertNoFrame(t, conn, "H6-CHAT-STILL-MUTED", "chat_message re-armed by a progress re-subscribe", 1500*time.Millisecond)
}

// TestWSChannelFilter_HubLevelChannelScoping pins the same contract at the
// hub level, without the wire: the session grant is channel-agnostic, the
// opt-out is not. Also pins the two rules the wire test cannot observe —
// a non-subscribed session stays suppressed (ws-filter-05), and a
// channel-less unsubscribe stays connection-wide.
func TestWSChannelFilter_HubLevelChannelScoping(t *testing.T) {
	hub := NewWebSocketHub(nil)
	// Subscribe on BOTH channels: the grant is channel-agnostic, so one
	// entry per channel reproduces what the GUI sends (subscribeToChat +
	// subscribeToAgentProgress + subscribeToTurnTerminal).
	hub.SubscribeSession(nilWSConn, "s1", wsChannelChat)
	hub.SubscribeSession(nilWSConn, "s1", wsChannelProgress)
	hub.SubscribeSession(nilWSConn, "s2", wsChannelChat)
	hub.SubscribeSession(nilWSConn, "s2", wsChannelProgress)

	// A `chat` unsubscribe is scoped to chat.
	hub.UnsubscribeSession(nilWSConn, "s1", wsChannelChat)
	if hub.ShouldSend(nilWSConn, "chat_message", "s1") {
		t.Error("chat unsubscribe did not mute chat_message for s1")
	}
	if !hub.ShouldSend(nilWSConn, "agent_progress", "s1") {
		t.Error("chat unsubscribe muted the progress stream for s1 (H6)")
	}
	if !hub.ShouldSend(nilWSConn, "agent_progress", "s2") {
		t.Error("chat unsubscribe for s1 muted the unrelated session s2")
	}

	// Re-subscribing is an explicit opt back in on THAT channel (the GUI's
	// dispose -> re-subscribe cycle must restore delivery, not stay muted
	// forever), and it must not re-arm a different channel.
	hub.SubscribeSession(nilWSConn, "s1", wsChannelChat)
	if !hub.ShouldSend(nilWSConn, "chat_message", "s1") {
		t.Error("re-subscribe did not restore chat delivery for s1")
	}
	if !hub.ShouldSend(nilWSConn, "agent_progress", "s1") {
		t.Error("a chat re-subscribe muted the progress stream for s1")
	}

	// A `progress`-channel re-subscribe must NOT re-arm chat.
	hub.UnsubscribeSession(nilWSConn, "s1", wsChannelChat)
	hub.SubscribeSession(nilWSConn, "s1", wsChannelProgress)
	if hub.ShouldSend(nilWSConn, "chat_message", "s1") {
		t.Error("a progress re-subscribe silently re-armed the chat stream for s1")
	}
	if !hub.ShouldSend(nilWSConn, "agent_progress", "s1") {
		t.Error("a progress re-subscribe did not restore the progress stream for s1")
	}

	// ws-filter-05 contract: an explicit unsubscribe leaves the connection
	// suppressed for NON-subscribed sessions instead of restoring broadcast.
	hub.UnsubscribeSession(nilWSConn, "s1", wsChannelProgress)
	if hub.ShouldSend(nilWSConn, "agent_progress", "never-subscribed") {
		t.Error("an explicit unsubscribe returned the connection to broadcast mode")
	}
	if hub.ShouldSend(nilWSConn, "chat_message", "never-subscribed") {
		t.Error("an explicit unsubscribe returned the connection to broadcast mode (chat)")
	}

	// A channel-less unsubscribe stays connection-wide.
	hub.UnsubscribeSession(nilWSConn, "s2", "")
	if hub.ShouldSend(nilWSConn, "chat_message", "s2") || hub.ShouldSend(nilWSConn, "agent_progress", "s2") {
		t.Error("a channel-less unsubscribe must suppress the session on every channel")
	}

	// A connection that never subscribed stays in broadcast mode. Fresh hub:
	// the one above holds grants for s1/s2.
	bare := NewWebSocketHub(nil)
	if !bare.ShouldSend(nilWSConn, "chat_message", "any") || !bare.ShouldSend(nilWSConn, "agent_progress", "any") {
		t.Error("an unarmed connection must stay in broadcast mode")
	}

	// A connection whose only frame was an unsubscribe for a session it had
	// never subscribed to: the entry exists but grants nothing, so
	// non-subscribed sessions stay suppressed (least-surprise opt-out).
	hub2 := NewWebSocketHub(nil)
	hub2.UnsubscribeSession(nilWSConn, "never-subscribed", wsChannelChat)
	if hub2.ShouldSend(nilWSConn, "chat_message", "other") {
		t.Error("an unsubscribe from a connection that never subscribed returned it to broadcast mode")
	}
	if hub2.ShouldSend(nilWSConn, "agent_progress", "other") {
		t.Error("a chat-channel unsubscribe from an unarmed connection muted the progress channel")
	}
}

// ---------------------------------------------------------------------------
// H7 pin — a synthesized progress event with an EMPTY session id reaches a
// filter-armed client (mirrors the generic relay's `eventSessionID == ""`
// bypass), while a session-bearing event stays session-filtered.
// ---------------------------------------------------------------------------

func TestWSProgress_SessionlessEventReachesArmedClient(t *testing.T) {
	baseURL, msgBus, cancel := startWSTestServer(t)
	defer cancel()

	conn := dialWSTestClient(t, baseURL)
	defer conn.Close()
	wsDrain(conn, 2*time.Second) // welcome

	const session = "session-h7-armed"
	wsSendFlat(t, conn, map[string]any{"type": "subscribe", "channel": "progress", "session_id": session})
	wsDrain(conn, 2*time.Second) // subscribed ack

	// No conversation_id -> an AgentEvent with an empty ConversationID ->
	// SynthesizedProgressEvent.SessionID == "". Before the fix the typed
	// relay filtered on "" and dropped the frame for this armed client.
	publishSynth(msgBus, "h7-sessless", "", "H7-SESSIONLESS-DELIVERED")
	msg := wsWaitForFrame(t, conn, "H7-SESSIONLESS-DELIVERED", 3*time.Second)
	if msg == nil {
		t.Fatal("a session-less synthesized progress event was dropped for a filter-armed client (H7)")
	}
	if msg["type"] != "agent_progress" {
		t.Errorf("frame type = %v, want agent_progress", msg["type"])
	}
	if sid, _ := msg["session_id"].(string); sid != "" {
		t.Errorf("session_id = %v, want empty (session-less event)", msg["session_id"])
	}

	// The typed path must STILL filter when a session IS present.
	time.Sleep(150 * time.Millisecond) // clear the per-connection rate limiter
	publishSynth(msgBus, "h7-other", "session-h7-other", "H7-OTHER-SESSION")
	wsAssertNoFrame(t, conn, "H7-OTHER-SESSION", "another session's synthesized progress", 1500*time.Millisecond)

	// And the armed session's own event still arrives (no over-broadcast).
	time.Sleep(150 * time.Millisecond)
	publishSynth(msgBus, "h7-own", session, "H7-OWN-SESSION")
	if msg := wsWaitForFrame(t, conn, "H7-OWN-SESSION", 3*time.Second); msg == nil {
		t.Error("the armed session's own synthesized progress was not delivered (H7 over-broadcast)")
	}
}
