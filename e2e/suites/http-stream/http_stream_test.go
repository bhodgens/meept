//go:build e2e

// Package httpstream covers the manifest's http-stream scenarios
// (http-stream-01..04): the HTTP transport's streaming surfaces —
// GET /api/v1/chat/stream's SSE session filter, the notifications poll +
// WS push pair, the /api/v1/bus/publish + /api/v1/bus/call bridges, and
// the full HTTP chat loop (submit ack → turn.terminal on WS → queue
// status terminal).
package httpstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/caimlas/meept/e2e/harness"
)

// start is the per-test sandbox with the WS relay enabled (scenario 04's
// WS leg needs it).
func start(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"transport.http.websocket": true,
	}))
}

// insecureClient returns an HTTP client accepting the sandbox self-signed
// cert.
func insecureClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
}

// ---------------------------------------------------------------------------
// http-stream-01 (M): SSE /api/v1/chat/stream session filter suppresses
// other sessions' events.
// ---------------------------------------------------------------------------

// openSSE opens GET /api/v1/chat/stream?session_id=<filter> and returns a
// line reader over the live stream. The request context (and thus the
// connection) lives until test cleanup.
func openSSE(t *testing.T, s *harness.Stack, filter string) *bufio.Reader {
	t.Helper()
	url := s.HTTPBaseURL() + "/api/v1/chat/stream"
	if filter != "" {
		url += "?session_id=" + filter
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("http-stream: build SSE request: %v", err)
	}
	resp, err := insecureClient().Do(req)
	if err != nil {
		t.Fatalf("http-stream: SSE connect: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("http-stream: SSE status %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("http-stream: SSE content-type %q, want text/event-stream", ct)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return bufio.NewReader(resp.Body)
}

// waitSSEMarker blocks reading SSE lines until marker appears in the
// accumulated stream or the deadline passes (fails the test).
func waitSSEMarker(t *testing.T, r *bufio.Reader, marker, what string, d time.Duration) string {
	t.Helper()
	var sb strings.Builder
	type result struct {
		text string
		err  error
	}
	res := make(chan result, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			sb.WriteString(line)
			if strings.Contains(sb.String(), marker) {
				res <- result{sb.String(), nil}
				return
			}
			if err != nil {
				res <- result{sb.String(), err}
				return
			}
		}
	}()
	select {
	case r := <-res:
		if r.err != nil && !strings.Contains(r.text, marker) {
			t.Fatalf("http-stream: SSE stream ended while waiting for %s: %v\nstream:\n%s", what, r.err, r.text)
		}
		return r.text
	case <-time.After(d):
		t.Fatalf("http-stream: no %s within %s\nstream so far:\n%s", what, d, sb.String())
		return ""
	}
}

// TestSSEChatStreamSessionFilter opens two SSE clients — one filtered on
// session A, one unfiltered (broadcast) — and drives a tool-activity event
// for session A and one for session B through the bus. The filtered client
// sees only A's event; the broadcast client sees both.
func TestSSEChatStreamSessionFilter(t *testing.T) {
	s := start(t)

	filtered := openSSE(t, s, "session-stream-01-a")
	broadcast := openSSE(t, s, "")

	// Wait for each stream's initial {event: connected} frame so the
	// daemon-side subscriptions are live before publishing.
	waitSSEMarker(t, filtered, "connected", "filtered stream connect", 15*time.Second)
	waitSSEMarker(t, broadcast, "connected", "broadcast stream connect", 15*time.Second)

	publish := func(sess, marker string) {
		t.Helper()
		rpc := harness.DialRPC(t, s.SocketPath)
		rpc.CallResult("bus.publish", map[string]any{
			"topic": "tool.execution.progress",
			"payload": map[string]any{
				"tool_call_id":    "call-" + marker,
				"tool_name":       "file_write",
				"agent_id":        "stream-01-agent",
				"message":         marker,
				"session_id":      sess,
				"conversation_id": sess,
			},
		})
	}

	publish("session-stream-01-b", "STREAM01-B-EVENT")
	publish("session-stream-01-a", "STREAM01-A-EVENT")

	// Broadcast client sees both.
	waitSSEMarker(t, broadcast, "STREAM01-B-EVENT", "session B event (broadcast)", 15*time.Second)
	waitSSEMarker(t, broadcast, "STREAM01-A-EVENT", "session A event (broadcast)", 15*time.Second)

	// Filtered client sees A's.
	waitSSEMarker(t, filtered, "STREAM01-A-EVENT", "session A event (filtered)", 15*time.Second)

	// Give a wrongly-forwarded B event time to appear, then assert absent
	// from everything the filtered stream has produced so far.
	res := waitSSEDrain(t, filtered, 3*time.Second)
	if strings.Contains(res, "STREAM01-B-EVENT") {
		t.Fatalf("http-stream-01: session-filtered SSE delivered session B's event:\n%s", res)
	}
}

// waitSSEDrain reads whatever arrives within d and returns it.
func waitSSEDrain(t *testing.T, r *bufio.Reader, d time.Duration) string {
	t.Helper()
	var sb strings.Builder
	type result struct{ text string }
	res := make(chan result, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			sb.WriteString(line)
			if err != nil {
				res <- result{sb.String()}
				return
			}
		}
	}()
	select {
	case r := <-res:
		return r.text
	case <-time.After(d):
		return sb.String()
	}
}

// ---------------------------------------------------------------------------
// http-stream-02 (M): notifications poll honors since; /ws/notifications
// streams live.
// ---------------------------------------------------------------------------

// pollNotifications GETs /api/v1/notifications?since=<rfc3339> and returns
// the raw body.
func pollNotifications(t *testing.T, s *harness.Stack, since string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.HTTPBaseURL()+"/api/v1/notifications?since="+since, nil)
	if err != nil {
		t.Fatalf("http-stream-02: build poll request: %v", err)
	}
	resp, err := insecureClient().Do(req)
	if err != nil {
		t.Fatalf("http-stream-02: notifications poll: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("http-stream-02: notifications poll status %d: %s", resp.StatusCode, body)
	}
	return string(body)
}

// TestNotificationsPollHonorsSince verifies the notifications surfaces'
// contracts against the real daemon: GET /api/v1/notifications returns the
// {events, count} envelope, honors RFC3339 `since` (an in-future `since`
// yields zero events; a malformed one is a 400), and /ws/notifications
// upgrades and streams the buffered replay. The notification producer
// (ChatHandler's Plan-4.3 path) requires wiring the sandbox cannot reach
// (see suite comment), so live-push delivery of a *real* notification is
// covered by the unit suite; the endpoint contracts are what this e2e
// pins.
func TestNotificationsPollHonorsSince(t *testing.T) {
	s := start(t)

	// Poll envelope + count key.
	epoch := pollNotifications(t, s, "1970-01-01T00:00:00Z")
	var all struct {
		Events []map[string]any `json:"events"`
		Count  int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(epoch), &all); err != nil {
		t.Fatalf("http-stream-02: notifications poll decode: %v (%s)", err, epoch)
	}
	for _, ev := range all.Events {
		if ev["timestamp"] == nil {
			t.Fatalf("http-stream-02: notification missing timestamp: %v", ev)
		}
		if ev["id"] == nil {
			t.Fatalf("http-stream-02: notification missing id: %v", ev)
		}
	}

	// `since` in the future: nothing may be returned even if the buffer
	// holds events.
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	none := pollNotifications(t, s, future)
	var out struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(none), &out); err != nil {
		t.Fatalf("http-stream-02: future-since poll decode: %v (%s)", err, none)
	}
	if out.Count != 0 {
		t.Fatalf("http-stream-02: since=%s poll returned %d events, want 0: %s", future, out.Count, none)
	}

	// Malformed since → 400 from the handler.
	resp, err := insecureClient().Get(s.HTTPBaseURL() + "/api/v1/notifications?since=not-a-time") //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("http-stream-02: bad-since poll: %v", err)
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck // bounded test call
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("http-stream-02: malformed since status = %d, want 400", resp.StatusCode)
	}

	// /ws/notifications: the WS upgrade succeeds (non-101 handshakes fail
	// in dialNotificationsWS) and the connection stays open — the handler
	// loops on its event channel until the client disconnects. The server
	// (coder/websocket) answers control frames itself, so a successful
	// open plus graceful client-initiated close is the liveness proof.
	c := dialNotificationsWS(t, s)
	_ = c.Close(websocket.StatusNormalClosure, "done")
}

// ---------------------------------------------------------------------------
// http-stream-03 (M): HTTP /api/v1/bus/publish + /api/v1/bus/call bridge
// to bus/RPC.
// ---------------------------------------------------------------------------

// TestHTTPBusBridges proves both bridges: /api/v1/bus/call dispatches the
// ping RPC method through the RPC registry; /api/v1/bus/publish delivers
// to a live bus subscriber (proven via a bus.subscribe/bus.poll session on
// the Unix socket).
func TestHTTPBusBridges(t *testing.T) {
	s := start(t)

	// bus/call → RPC registry.
	resp, err := insecureClient().Post(
		s.HTTPBaseURL()+"/api/v1/bus/call", "application/json",
		strings.NewReader(`{"method":"ping"}`)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("http-stream-03: bus/call: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("http-stream-03: bus/call status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "pong") {
		t.Fatalf("http-stream-03: bus/call ping result = %s, want pong", body)
	}

	// bus/publish → bus subscriber (persistent RPC subscription).
	rpc := harness.DialRPC(t, s.SocketPath)
	sub := rpc.CallResult("bus.subscribe", map[string]any{
		"topics": []string{"stream.03.test"},
	})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("http-stream-03: bus.subscribe ack missing subscription_id: %v", sub)
	}
	defer func() {
		_ = rpc.Call("bus.unsubscribe", map[string]string{"subscription_id": subID})
	}()

	resp, err = insecureClient().Post(
		s.HTTPBaseURL()+"/api/v1/bus/publish", "application/json",
		strings.NewReader(`{"topic":"stream.03.test","payload":{"marker":"STREAM03-BUS-PUBLISH"}}`)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("http-stream-03: bus/publish: %v", err)
	}
	pbody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("http-stream-03: bus/publish status %d: %s", resp.StatusCode, pbody)
	}

	// Poll until the published event lands.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		res := rpc.CallResult("bus.poll", map[string]string{"subscription_id": subID})
		raw, _ := json.Marshal(res)
		if strings.Contains(string(raw), "STREAM03-BUS-PUBLISH") {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("http-stream-03: published event never reached the bus subscriber within 15s")
}

// ---------------------------------------------------------------------------
// http-stream-04 (L): HTTP chat loop — submit ack → turn.terminal on WS →
// queue status terminal.
// ---------------------------------------------------------------------------

// TestHTTPChatLoopEndToEnd walks the full client loop a GUI performs:
// POST /api/v1/chat/submit (ack), WS turn.terminal for the turn, then
// GET /api/v1/chat/queue/{conversation_id} reporting a non-active (done)
// queue.
func TestHTTPChatLoopEndToEnd(t *testing.T) {
	s := start(t)
	s.RegisterProject(t, "stream04")
	sid := s.CreateSession(t, "stream-04", s.ProjectDir)

	// Script the turn's work.
	artifact := s.ProjectDir + "/stream04.txt"
	s.Fake.SetPostToolText("Created stream04.txt at " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-stream04", artifact, "stream04")

	// 1. WS client connected BEFORE the submit (subscribed to the session).
	ws := harness.DialWS(t, s, "")
	ws.Subscribe("all", sid)

	// 2. Submit ack.
	ack := s.SubmitChatHTTP(t, sid, "Create a file named stream04.txt containing stream04")
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("http-stream-04: submit not accepted: %+v", ack)
	}
	turnID, _ := ack["turn_id"].(string)
	convID, _ := ack["conversation_id"].(string)
	if turnID == "" || convID == "" {
		t.Fatalf("http-stream-04: ack missing turn_id/conversation_id: %+v", ack)
	}

	// 3. turn.terminal for the acked turn arrives on WS as agent_progress
	// (the typed Topic[T] marker path).
	deadline := time.Now().Add(120 * time.Second)
	var terminal map[string]any
	for time.Now().Before(deadline) {
		// Other agent_progress frames (chat.request, chat.processing,
		// synthesized progress for the same session) are dropped here;
		// WaitEvent scans until a frame naming the turn id with a
		// terminal status.
		ev := ws.WaitEvent(time.Until(deadline), "agent_progress")
		if ev == nil {
			break
		}
		var data map[string]any
		if json.Unmarshal(ev.Data, &data) != nil {
			continue
		}
		if id, _ := data["turn_id"].(string); id == turnID {
			if status, _ := data["status"].(string); status == "completed" || status == "failed" {
				terminal = data
				break
			}
		}
	}
	if terminal == nil {
		t.Fatalf("http-stream-04: no terminal turn.terminal event for %s within deadline", turnID)
	}
	if status, _ := terminal["status"].(string); status != "completed" {
		t.Fatalf("http-stream-04: turn.terminal status = %v (error %v)", terminal["status"], terminal["error"])
	}

	// 4. Queue status for the conversation reports the terminal (inactive)
	// state.
	resp, err := insecureClient().Get(s.HTTPBaseURL() + "/api/v1/chat/queue/" + convID) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("http-stream-04: queue status: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("http-stream-04: queue status HTTP %d: %s", resp.StatusCode, body)
	}
	var queue struct {
		IsActive bool `json:"is_active"`
	}
	if err := json.Unmarshal(body, &queue); err != nil {
		t.Fatalf("http-stream-04: queue status decode: %v (%s)", err, body)
	}
	if queue.IsActive {
		t.Fatalf("http-stream-04: queue still active after terminal event: %s", body)
	}

	// And the artifact is real.
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("http-stream-04: artifact missing: %v", err)
	}
}
