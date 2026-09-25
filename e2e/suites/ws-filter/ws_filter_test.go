//go:build e2e

// Package wsfilter covers the manifest's ws-filter scenarios
// (ws-filter-01..06): the WS relay's per-connection session filtering —
// session-scoped delivery, the session_id→conversation_id fallback
// (duality invariant), the chat_message payload's session_id backfill,
// broadcast mode for unfiltered connections, unsubscribe removing
// per-session and channel-level filters, and synthesized progress events
// (session-filtered + rate-limited).
//
// It deliberately carries its OWN minimal WS client (wssSession) instead
// of harness.WSClient: the filter tests depend on the subscribe frame's
// data being a JSON OBJECT ({"type":"subscribe","data":{"channel":...,
// "session_id":...}}) so the daemon registers the session filter. Raw
// control of the wire shape here keeps the suite immune to client-side
// encoding details.
//
// Live events are injected through the daemon's bus.publish RPC bridge so
// each scenario controls exactly which payloads flow while the WS client
// asserts on what the relay delivers.
package wsfilter

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/e2e/harness"
)

// start is the per-test sandbox with the WS relay enabled.
func start(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"transport.http.websocket": true,
	}))
}

// wsFrame is one decoded WS frame from the daemon. Raw keeps the whole
// frame text: handleWSProgress sends synthesized progress as a FLAT map
// (no "data" envelope), so marker matching must look at the raw frame.
type wsFrame struct {
	Type string
	Data json.RawMessage
	Raw  string
}

// wssSession is one daemon WS connection with raw-frame control. A single
// background reader pumps frames into an inbox: coder/websocket read errors
// are terminal for the connection, so per-call deadlines must NEVER be
// applied to conn.Read directly.
type wssSession struct {
	conn   *websocket.Conn
	t      *testing.T
	inbox  chan *wsFrame
	closed chan struct{}
}

// dialWSS opens /ws on the sandbox daemon (no auth: the sandbox runs with
// require_auth=false) and consumes the welcome frame.
func dialWSS(t *testing.T, s *harness.Stack) *wssSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wsURL := "wss" + strings.TrimPrefix(s.HTTPBaseURL(), "https") + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			},
		},
	})
	if err != nil {
		t.Fatalf("ws-filter: dial ws: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "e2e done") })
	w := &wssSession{
		conn:   conn,
		t:      t,
		inbox:  make(chan *wsFrame, 256),
		closed: make(chan struct{}),
	}
	go w.pump()
	// Welcome frame: {"type":"status","data":{"connected":true}}.
	w.recv(10*time.Second, "welcome")
	return w
}

// pump reads frames until the connection dies, buffering them in inbox.
func (w *wssSession) pump() {
	defer close(w.closed)
	for {
		_, data, err := w.conn.Read(context.Background())
		if err != nil {
			return
		}
		w.inbox <- decodeFrame(data, "frame")
	}
}

// send marshals v and writes one text frame.
func (w *wssSession) send(v map[string]any) {
	w.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		w.t.Fatalf("ws-filter: marshal frame: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.conn.Write(ctx, websocket.MessageText, raw); err != nil {
		w.t.Fatalf("ws-filter: ws write: %v", err)
	}
}

// recv reads one frame from the inbox or fails the test on timeout.
func (w *wssSession) recv(timeout time.Duration, what string) *wsFrame {
	w.t.Helper()
	select {
	case f := <-w.inbox:
		return f
	case <-time.After(timeout):
		w.t.Fatalf("ws-filter: no %s frame within %s", what, timeout)
		return nil
	}
}

// decodeFrame decodes one raw WS frame, tolerating flat (data-less) shapes.
func decodeFrame(data []byte, what string) *wsFrame {
	var top struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return &wsFrame{Type: "", Raw: string(data)}
	}
	return &wsFrame{Type: top.Type, Data: top.Data, Raw: string(data)}
}

// tryRecv reads one frame if one arrives within timeout; nil otherwise.
func (w *wssSession) tryRecv(timeout time.Duration) *wsFrame {
	w.t.Helper()
	select {
	case f := <-w.inbox:
		return f
	case <-time.After(timeout):
		return nil
	}
}

// subscribeSession sends the subscribe frame and waits for the ack. The
// data field MUST be a nested JSON object for the daemon to register the
// session filter.
func (w *wssSession) subscribeSession(sessionID string) {
	w.t.Helper()
	data := map[string]any{"channel": "all"}
	if sessionID != "" {
		data["session_id"] = sessionID
	}
	w.send(map[string]any{"type": "subscribe", "data": data})
	for {
		f := w.recv(10*time.Second, "subscribe ack")
		if f.Type == "subscribed" {
			return
		}
		if f.Type == "error" {
			w.t.Fatalf("ws-filter: subscribe error: %s", f.Data)
		}
	}
}

// unsubscribeSession removes one session filter (no ack).
func (w *wssSession) unsubscribeSession(sessionID string) {
	w.t.Helper()
	w.send(map[string]any{"type": "unsubscribe", "data": map[string]any{
		"session_id": sessionID,
	}})
}

// unsubscribeChannel removes the channel and ALL its session filters
// (no ack).
func (w *wssSession) unsubscribeChannel(channel string) {
	w.t.Helper()
	w.send(map[string]any{"type": "unsubscribe", "data": map[string]any{
		"channel": channel,
	}})
}

// publishViaRPC publishes one raw bus message via the daemon's bus.publish
// bridge; fails the test when nothing is delivered (relay not subscribed).
func publishViaRPC(t *testing.T, s *harness.Stack, topic string, payload map[string]any) {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	result := rpc.CallResult("bus.publish", map[string]any{
		"topic":   topic,
		"payload": payload,
	})
	if delivered, _ := result["delivered"].(float64); delivered < 1 {
		t.Fatalf("ws-filter: bus.publish to %q delivered=%v; WS relay subscription not live", topic, result["delivered"])
	}
}

// terminalPayload builds a turn.terminal-shaped payload for a session.
func terminalPayload(sess, marker string) map[string]any {
	return map[string]any{
		"conversation_id": "conv-" + sess,
		"session_id":      sess,
		"turn_id":         "turn-" + marker,
		"status":          "completed",
		"reply":           marker,
		"duration_ms":     1,
	}
}

// waitMarker scans frames until one whose payload contains marker arrives
// (or fails the test on timeout). Other frames are dropped.
func waitMarker(t *testing.T, w *wssSession, marker, what string, d time.Duration) *wsFrame {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		f := w.tryRecv(time.Until(deadline))
		if f == nil {
			break
		}
		if strings.Contains(f.Raw, marker) {
			return f
		}
	}
	t.Fatalf("ws-filter: no %s frame within %s", what, d)
	return nil
}

// waitAbsent asserts no frame containing marker arrives within d. Any
// other frames in the window are dropped.
func waitAbsent(t *testing.T, w *wssSession, marker, what string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		f := w.tryRecv(time.Until(deadline))
		if f == nil {
			return
		}
		if strings.Contains(f.Raw, marker) {
			t.Fatalf("ws-filter: %s leaked to the wrong client: %s (%s)", what, f.Type, f.Raw)
		}
	}
}

// ---------------------------------------------------------------------------
// ws-filter-01 (M): a WS client subscribed with session_id receives only
// that session's events.
// ---------------------------------------------------------------------------

// TestWSSessionFilterDeliversOnlySubscribedSession runs two turn.terminal
// events for different sessions past one filtered client: only session A's
// event arrives, session B's never does.
func TestWSSessionFilterDeliversOnlySubscribedSession(t *testing.T) {
	s := start(t)
	ws := dialWSS(t, s)
	ws.subscribeSession("session-wsfilter-01-a")

	// Other session first (would leak before the filter if broken), then
	// the subscribed session's event.
	publishViaRPC(t, s, "turn.terminal", terminalPayload("session-wsfilter-01-b", "WSFILTER01-B-REPLY"))
	publishViaRPC(t, s, "turn.terminal", terminalPayload("session-wsfilter-01-a", "WSFILTER01-A-REPLY"))

	ev := waitMarker(t, ws, "WSFILTER01-A-REPLY", "session A terminal", 15*time.Second)
	var data map[string]any
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatalf("ws-filter-01: payload decode: %v", err)
	}
	if got, _ := data["session_id"].(string); got != "session-wsfilter-01-a" {
		t.Fatalf("ws-filter-01: session_id = %v, want the subscribed session", data["session_id"])
	}

	// Session B's reply must never arrive (drain the 3s window).
	waitAbsent(t, ws, "WSFILTER01-B-REPLY", "session B terminal", 3*time.Second)
}

// ---------------------------------------------------------------------------
// ws-filter-02 (M): the WS filter falls back session_id → conversation_id
// (duality invariant).
// ---------------------------------------------------------------------------

// TestWSFilterFallsBackToConversationID publishes an event that carries
// ONLY conversation_id (no session_id) — the chat.* lifecycle family —
// filtered on the session id. The duality invariant requires the filter
// to match and deliver it.
func TestWSFilterFallsBackToConversationID(t *testing.T) {
	s := start(t)
	ws := dialWSS(t, s)
	ws.subscribeSession("session-wsfilter-02")

	publishViaRPC(t, s, "chat.processing", map[string]any{
		"request_id":      "req-wsfilter-02",
		"conversation_id": "session-wsfilter-02", // the session id AS the conversation id (duality)
		"status":          "processing",
		"worker_id":       "worker-wsfilter-02",
	})

	waitMarker(t, ws, "req-wsfilter-02", "conversation-only event", 15*time.Second)
}

// ---------------------------------------------------------------------------
// ws-filter-03 (S): chat_message payload session_id backfilled from
// conversation_id.
// ---------------------------------------------------------------------------

// TestWSChatMessageBackfillsSessionIDFromConversation pins the
// normalization backfill: a chat_message payload with ONLY conversation_id
// is relayed with session_id set to that conversation id — so a client
// filtered on the session receives it (and can route it).
func TestWSChatMessageBackfillsSessionIDFromConversation(t *testing.T) {
	s := start(t)
	ws := dialWSS(t, s)
	ws.subscribeSession("session-wsfilter-03")

	publishViaRPC(t, s, "chat_message", map[string]any{
		"role":            "assistant",
		"content":         "WSFILTER03-BACKFILL reply",
		"conversation_id": "session-wsfilter-03", // no session_id on the payload
	})

	ev := waitMarker(t, ws, "WSFILTER03-BACKFILL", "backfilled chat_message", 15*time.Second)
	var data map[string]any
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatalf("ws-filter-03: payload decode: %v", err)
	}
	sid, _ := data["session_id"].(string)
	if sid != "session-wsfilter-03" {
		t.Fatalf("ws-filter-03: session_id = %v, want the conversation id backfilled (%s)", data["session_id"], ev.Data)
	}
}

// ---------------------------------------------------------------------------
// ws-filter-04 (S): connections with no session filter stay in broadcast
// mode.
// ---------------------------------------------------------------------------

// TestWSNoFilterStaysInBroadcastMode proves a connection that never
// subscribes receives events for EVERY session.
func TestWSNoFilterStaysInBroadcastMode(t *testing.T) {
	s := start(t)
	ws := dialWSS(t, s) // no subscribe: broadcast mode

	for _, sess := range []string{"broadcast-x", "broadcast-y"} {
		publishViaRPC(t, s, "turn.terminal", terminalPayload(sess, "BCAST-"+sess))
	}

	waitMarker(t, ws, "BCAST-broadcast-x", "first session in broadcast mode", 15*time.Second)
	waitMarker(t, ws, "BCAST-broadcast-y", "second session in broadcast mode", 15*time.Second)
}

// ---------------------------------------------------------------------------
// ws-filter-05 (S): unsubscribe removes per-session and channel-level
// filters.
// ---------------------------------------------------------------------------

// TestWSUnsubscribeRestoresDelivery covers both unsubscribe forms:
//
//  1. session-level unsubscribe ({session_id}) — the per-session filter
//     for that session is removed; the connection stops matching it
//     (ShouldSendProgress consults the remaining filter set);
//  2. channel-level unsubscribe ({channel}) — ALL session filters on the
//     connection are dropped (the map is deleted), returning the
//     connection to broadcast mode, so the formerly filtered session's
//     events are delivered again.
func TestWSUnsubscribeRestoresDelivery(t *testing.T) {
	s := start(t)
	ws := dialWSS(t, s)
	ws.subscribeSession("session-wsfilter-05-a")

	publishA := func(marker string) {
		t.Helper()
		publishViaRPC(t, s, "turn.terminal", terminalPayload("session-wsfilter-05-a", marker))
	}

	// Filtered: session A's own event is delivered.
	publishA("WSFILTER05-DELIVERED")
	waitMarker(t, ws, "WSFILTER05-DELIVERED", "filtered delivery", 15*time.Second)

	// Session-level unsubscribe removes the A filter: A's events are no
	// longer matched (the remaining filter set is empty-but-present, so
	// nothing matches).
	ws.unsubscribeSession("session-wsfilter-05-a")
	publishA("WSFILTER05-STOPPED")
	waitAbsent(t, ws, "WSFILTER05-STOPPED", "event after session unsubscribe", 3*time.Second)

	// Channel-level unsubscribe drops ALL session filters on the
	// connection → broadcast mode → A's events delivered again.
	ws.unsubscribeChannel("all")
	publishA("WSFILTER05-RESTORED-CHANNEL")
	waitMarker(t, ws, "WSFILTER05-RESTORED-CHANNEL", "event after channel unsubscribe", 15*time.Second)
}

// ---------------------------------------------------------------------------
// ws-filter-06 (M): synthesized progress events are session-filtered and
// rate-limited.
// ---------------------------------------------------------------------------

// synthPayload builds an agent.progress.synthesized payload
// (agent.SynthesizedProgressEvent wire shape) for a session.
func synthPayload(sess, marker string) map[string]any {
	return map[string]any{
		"session_id":   sess,
		"agent_id":     "wsfilter-06-agent",
		"tier":         1,
		"message":      marker,
		"source_event": "tool_execution_end",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
}

// TestWSSynthesizedProgressFilteredAndRateLimited pins the
// agent.progress.synthesized relay path. The topic reaches WS clients over
// TWO independent surfaces, both asserted here:
//
//   - handleWSProgress: the SynthesizedProgressEvent decoded and re-emitted
//     as a flat agent_progress frame — session-filtered AND rate-limited
//     (100ms default interval): 10 back-to-back events deliver far fewer
//     than 10 on that surface;
//   - handleWSEvent: the generic "event" surface — session-filtered but
//     NOT rate-limited, so all 10 arrive there.
//
// The rate-limit assertion targets the agent_progress frames specifically;
// the filter assertion applies to both surfaces.
func TestWSSynthesizedProgressFilteredAndRateLimited(t *testing.T) {
	s := start(t)
	sessS := "session-wsfilter-06-s"
	sessT := "session-wsfilter-06-t"

	wsS := dialWSS(t, s)
	wsS.subscribeSession(sessS)
	wsT := dialWSS(t, s)
	wsT.subscribeSession(sessT)

	// 10 synthesized events for session S, published back-to-back over
	// ONE rpc connection (each round trip is sub-millisecond, so the
	// burst lands well inside the 100ms rate-limit window).
	rpc := harness.DialRPC(t, s.SocketPath)
	const total = 10
	for i := 0; i < total; i++ {
		result := rpc.CallResult("bus.publish", map[string]any{
			"topic":   "agent.progress.synthesized",
			"payload": synthPayload(sessS, "WSFILTER06-SYNTH"),
		})
		if delivered, _ := result["delivered"].(float64); delivered < 1 {
			t.Fatalf("ws-filter-06: bus.publish delivered=%v; relay subscription not live", result["delivered"])
		}
	}

	// Session S's client: count the agent_progress (rate-limited) frames.
	deliveredS := 0
	gotEvent := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f := wsS.tryRecv(time.Until(deadline))
		if f == nil {
			break
		}
		if !strings.Contains(f.Raw, "WSFILTER06-SYNTH") {
			continue
		}
		switch f.Type {
		case "agent_progress":
			deliveredS++
		case "event":
			gotEvent = true
		}
	}
	if !gotEvent {
		t.Fatalf("ws-filter-06: generic event surface delivered none of %d synthesized events", total)
	}
	if deliveredS == 0 {
		t.Fatalf("ws-filter-06: agent_progress surface delivered none of %d synthesized events", total)
	}
	if deliveredS >= total {
		t.Fatalf("ws-filter-06: rate limiter did not drop any of %d back-to-back agent_progress frames (delivered %d)", total, deliveredS)
	}
	t.Logf("ws-filter-06: rate limiter delivered %d of %d back-to-back agent_progress frames", deliveredS, total)

	// Session T's client (different filter) receives NONE of session S's
	// events on either surface. Give any leaked frame time to arrive.
	waitAbsent(t, wsT, "WSFILTER06-SYNTH", "other session's synthesized progress", 3*time.Second)
}
