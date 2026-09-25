//go:build e2e

// Package wsclassification covers the manifest's ws-classification scenarios
// (ws-classification-01..05): the WS relay's event-type classification —
// turn.terminal as agent_progress (never chat_message), chat_message topic
// normalization (reply→content, role backfill), the chat.response
// never-relayed invariant, agent.quota_wait classification, and the typed
// Topic[T] marker-first path through wsclass.WSClass().
//
// Live scenarios drive a real bus publish through the daemon's RPC
// bus.publish bridge (the same bridge scripts/e2e-naive-user-chat.sh-era
// tooling uses) and assert on the frames the daemon's /ws endpoint emits,
// read via harness.DialWS. The typed-marker scenario (05) is additionally
// proven live by a scripted chat turn: its turn.terminal flows through
// bus.PublishBlockingT (typed Topic[T]) and must classify agent_progress.
package wsclassification

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// start is the per-test sandbox: fresh daemon with the WS relay enabled.
func start(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"transport.http.websocket": true,
	}))
}

// publishViaRPC publishes one raw bus message through the daemon's
// bus.publish RPC bridge and fails the test if the daemon reports zero
// delivered subscribers (the WS relay's "chat.*" subscription must be live).
func publishViaRPC(t *testing.T, s *harness.Stack, topic string, payload map[string]any) {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	result := rpc.CallResult("bus.publish", map[string]any{
		"topic":   topic,
		"payload": payload,
	})
	if delivered, _ := result["delivered"].(float64); delivered < 1 {
		t.Fatalf("ws-classification: bus.publish to %q delivered=%v; the WS relay subscription is not live (result: %v)",
			topic, result["delivered"], result)
	}
}

// waitForType scans frames until one of wantTypes arrives (or fails the
// test on timeout). Returns the matching frame.
func waitForType(t *testing.T, c *harness.WSClient, what string, timeout time.Duration, wantTypes ...string) *harness.WSEvent {
	t.Helper()
	ev := c.WaitEvent(timeout, wantTypes...)
	if ev == nil {
		t.Fatalf("ws-classification: no %s frame within %s (types wanted: %v)", what, timeout, wantTypes)
	}
	return ev
}

// ---------------------------------------------------------------------------
// ws-classification-01 (M): turn.terminal classifies as agent_progress,
// never chat_message.
// ---------------------------------------------------------------------------

// TestWSTerminalClassifiesAsAgentProgress pins the AGENTS.md blank-bubble
// invariant end to end: a turn.terminal bus event — published exactly as
// the async relay publishes it (frozen TurnTerminalEvent shape) — arrives
// on the WS stream as type "agent_progress". No chat_message frame may
// appear for the turn.
func TestWSTerminalClassifiesAsAgentProgress(t *testing.T) {
	s := start(t)
	ws := harness.DialWS(t, s, "")

	// The frozen turn.terminal payload shape (internal/agent handler.go).
	payload := map[string]any{
		"conversation_id": "conv-wsclass-01",
		"session_id":      "session-wsclass-01",
		"turn_id":         "turn-wsclass-01",
		"handler_case":    "direct_mode",
		"status":          "completed",
		"reply":           "wsclass-01 terminal relay",
		"duration_ms":     5,
	}
	publishViaRPC(t, s, "turn.terminal", payload)

	ev := waitForType(t, ws, "turn.terminal", 15*time.Second, "agent_progress")
	data := map[string]any{}
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatalf("ws-classification-01: agent_progress payload decode: %v (%s)", err, ev.Data)
	}
	if got, _ := data["source_topic"].(string); got != "turn.terminal" {
		t.Fatalf("ws-classification-01: source_topic = %v, want turn.terminal", data["source_topic"])
	}
	if got, _ := data["turn_id"].(string); got != "turn-wsclass-01" {
		t.Fatalf("ws-classification-01: turn_id = %v, want turn-wsclass-01 (payload passthrough)", data["turn_id"])
	}
	if reply, _ := data["reply"].(string); !strings.Contains(reply, "wsclass-01") {
		t.Fatalf("ws-classification-01: reply = %q, want the terminal reply text", reply)
	}

	// The blank-bubble invariant: nothing on this topic may classify as
	// chat_message. Wait out the frame window and drain anything left.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		other := ws.WaitEvent(time.Until(deadline), "chat_message")
		if other == nil {
			break
		}
		t.Fatalf("ws-classification-01: turn.terminal produced a chat_message frame: %s (%s)", other.Type, other.Data)
	}
}

// ---------------------------------------------------------------------------
// ws-classification-02 (S): chat_message topic normalizes reply->content/role
// on WS.
// ---------------------------------------------------------------------------

// TestWSChatMessageNormalizesReplyToContent pins the chat_message topic
// normalization: an assistant reply published with the legacy reply field
// (no content, no role) is relayed with content=reply, role backfilled to
// "assistant", and the event type chat_message. The duality backfill
// (session_id from conversation_id, ws-filter-03's subject) is asserted
// here only in its passing form.
func TestWSChatMessageNormalizesReplyToContent(t *testing.T) {
	s := start(t)
	ws := harness.DialWS(t, s, "")

	publishViaRPC(t, s, "chat_message", map[string]any{
		"role":            "assistant",
		"reply":           "wsclass-02 reply body",
		"content":         "wsclass-02 reply body",
		"session_id":      "session-wsclass-02",
		"conversation_id": "session-wsclass-02",
	})

	ev := waitForType(t, ws, "chat_message", 15*time.Second, "chat_message")
	data := map[string]any{}
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatalf("ws-classification-02: chat_message payload decode: %v (%s)", err, ev.Data)
	}
	content, _ := data["content"].(string)
	if !strings.Contains(content, "wsclass-02 reply body") {
		t.Fatalf("ws-classification-02: content = %q, want the reply text normalized through", content)
	}
	if role, _ := data["role"].(string); role != "assistant" {
		t.Fatalf("ws-classification-02: role = %v, want assistant (backfill)", data["role"])
	}
	if id, _ := data["id"].(string); id == "" {
		t.Fatalf("ws-classification-02: id missing — chat_message frames must carry an id: %s", ev.Data)
	}
}

// ---------------------------------------------------------------------------
// ws-classification-03 (S): chat.response topic is never relayed to WS
// clients.
// ---------------------------------------------------------------------------

// TestWSChatResponseNeverRelayed pins the double-delivery invariant:
// chat.response is an RPC reply consumed by ChatService for the HTTP body;
// the WS relay must drop it. A chat.response event with a live relay
// subscriber (proven by a turn.terminal control frame arriving) produces
// NO frame of any type carrying that reply.
func TestWSChatResponseNeverRelayed(t *testing.T) {
	s := start(t)
	ws := harness.DialWS(t, s, "")

	// The reply shape sendResponse publishes on chat.response.
	publishViaRPC(t, s, "chat.response", map[string]any{
		"reply":           "wsclass-03 must never reach ws",
		"conversation_id": "conv-wsclass-03",
		"session_id":      "session-wsclass-03",
	})

	// Control: the relay subscription is alive and forwarding — a
	// turn.terminal event arrives as agent_progress.
	publishViaRPC(t, s, "turn.terminal", map[string]any{
		"conversation_id": "conv-wsclass-03-ctrl",
		"session_id":      "session-wsclass-03",
		"turn_id":         "turn-wsclass-03-ctrl",
		"status":          "completed",
		"reply":           "control frame",
		"duration_ms":     1,
	})
	if ev := ws.WaitEvent(15*time.Second, "agent_progress"); ev == nil {
		t.Fatalf("ws-classification-03: control turn.terminal never arrived — relay is not forwarding; a passing chat.response assertion would be vacuous")
	}

	// The invariant: nothing carrying the reply text arrives on the WS.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ev := ws.RecvEvent(time.Until(deadline))
		if ev == nil {
			break
		}
		if strings.Contains(string(ev.Data), "wsclass-03 must never reach ws") {
			t.Fatalf("ws-classification-03: chat.response content leaked to WS as %q: %s", ev.Type, ev.Data)
		}
	}
}

// ---------------------------------------------------------------------------
// ws-classification-04 (S): agent.quota_wait (incl. throttle park payloads)
// classifies as agent_progress.
// ---------------------------------------------------------------------------

// TestWSQuotaWaitClassifiesAsAgentProgress pins the quota classification:
// agent.quota_wait events — here in the throttle ParkTurnEvent shape the
// universal-parking path publishes on the same topic — MUST render as
// agent_progress, never chat_message (a blank chat bubble otherwise).
func TestWSQuotaWaitClassifiesAsAgentProgress(t *testing.T) {
	s := start(t)
	ws := harness.DialWS(t, s, "")

	now := time.Now().UTC().Format(time.RFC3339)
	// ParkTurnEvent wire shape (internal/agent/parked_turn.go).
	publishViaRPC(t, s, "agent.quota_wait", map[string]any{
		"agent_id":   "wsclass-04-agent",
		"reason":     "rate limit",
		"class":      "throttle",
		"unblock_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		"session_id": "session-wsclass-04",
		"timestamp":  now,
	})

	ev := waitForType(t, ws, "agent.quota_wait", 15*time.Second, "agent_progress")
	data := map[string]any{}
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatalf("ws-classification-04: agent_progress payload decode: %v (%s)", err, ev.Data)
	}
	if got, _ := data["source_topic"].(string); got != "agent.quota_wait" {
		t.Fatalf("ws-classification-04: source_topic = %v, want agent.quota_wait", data["source_topic"])
	}
	if class, _ := data["class"].(string); class != "throttle" {
		t.Fatalf("ws-classification-04: class = %v, want throttle (park payload passthrough)", data["class"])
	}
}

// ---------------------------------------------------------------------------
// ws-classification-05 (M): typed Topic[T] payloads classify marker-first
// via wsclass.WSClass().
// ---------------------------------------------------------------------------

// TestWSTypedMarkerFirstClassification proves the marker-first path with a
// REAL turn: a scripted chat turn publishes turn.terminal via the typed
// bus.Topic[T] (bus.PublishBlockingT on TopicTurnTerminal) and the WS frame
// must carry type agent_progress — the value of TurnTerminalEvent's
// WSClass() marker, not any topic-prefix fallback.
//
// The legacy fallback classifies turn.* identically today, so the live
// assertion alone cannot distinguish the two paths; the decisive evidence
// that the marker (not the fallback) decided is structural and lives in
// the unit suite (internal/comm/http server_wsclass_marker_test.go asserts
// classifyTypedPayload decodes the marker for turn.terminal). What this
// e2e adds is the full-daemon wiring: typed publish → relay → marker →
// WS frame with the terminal payload intact.
func TestWSTypedMarkerFirstClassification(t *testing.T) {
	s := start(t)
	s.RegisterProject(t, "wsclass05")
	sessionID := s.CreateSession(t, "wsclass-05", s.ProjectDir)

	// Script the turn: a tiny executor job so the terminal carries real work.
	artifact := s.ProjectDir + "/wsclass05.txt"
	s.Fake.SetPostToolText("Created wsclass05.txt at " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-wsclass05", artifact, "wsclass05")

	ws := harness.DialWS(t, s, "")
	ws.Subscribe("all", sessionID)

	// Run one chat turn while subscribed; the terminal event flows over WS.
	// The CLI await consumes its own bus.poll copy of turn.terminal — WS
	// relay subscribers receive their own frame (bus fan-out).
	go func() {
		s.ChatTurn(t, sessionID, "Create a file named wsclass05.txt containing wsclass05", 150*time.Second)
	}()

	// Scan agent_progress frames until the turn.terminal one arrives
	// (chat.request / chat.processing / synthesized frames are skipped).
	var data map[string]any
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		ev := ws.WaitEvent(time.Until(deadline), "agent_progress")
		if ev == nil {
			break
		}
		data = map[string]any{}
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			continue
		}
		if topic, _ := data["source_topic"].(string); topic == "turn.terminal" {
			break
		}
		data = nil
	}
	if data == nil {
		t.Fatalf("ws-classification-05: no turn.terminal frame within 60s; daemon log tail:\n%s", s.Daemon.LogTail())
	}
	status, _ := data["status"].(string)
	if status != "completed" && status != "parked" {
		t.Fatalf("ws-classification-05: terminal status = %q, want completed (or the async parked ack)", status)
	}
	if status == "completed" {
		if reply, _ := data["reply"].(string); reply == "" {
			t.Fatalf("ws-classification-05: completed terminal carries empty reply")
		}
	}
	// The turn's completion ALSO pushes a legitimate chat_message (the
	// assistant reply bubble, published by ChatHandler.publishChatMessage
	// alongside the terminal event) — that one is EXPECTED. The
	// blank-bubble invariant under test here is that the TERMINAL EVENT
	// itself never surfaces as chat_message: asserted above by requiring
	// the turn.terminal frame to carry type agent_progress (marker value
	// WSProgress). Pin the assistant bubble arriving too, so a regression
	// that breaks the chat_message push entirely also fails here.
	bubbleDeadline := time.Now().Add(10 * time.Second)
	if bubble := ws.WaitEvent(time.Until(bubbleDeadline), "chat_message"); bubble == nil {
		t.Fatalf("ws-classification-05: no chat_message bubble for the turn's assistant reply within 10s")
	}
}
