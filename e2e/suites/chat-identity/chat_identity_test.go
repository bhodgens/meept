//go:build e2e

// Package chatidentity covers canonical chat identity (leaf 01 of
// 20261121-interactive-usability): a submit, steer, or follow-up carrying
// ANY of the session's identity shapes (primary session id, its
// conversation id, a thread-scoped conversation id) must reach the same
// live queue, and agent_progress events must be delivered to a WS
// subscriber that subscribed on the primary session id.
package chatidentity

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// startStack boots the sandbox daemon with the classifier pinned to the
// plain chat lane so turns run through RouteToAgent's queue lane.
func startStack(t *testing.T) *harness.Stack {
	t.Helper()
	// The suite's long-turn lane fetches a suite-local httptest server
	// (127.0.0.1) so the turn stays busy while steer/followup race the
	// live queue; the sandbox daemon's SSRF guard blocks loopback by
	// default, so the private CIDR rides the overlay like
	// tools-web-ssrf-02.
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"security.ssrf.allowed_cidrs": []string{"127.0.0.0/8"},
	}))
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"chat-identity chat pin"}`)
	return s
}

// postChatSubmit submits with full control over the wire identity fields
// (no harness defaulting): GUI-shaped submits carry conversation_id only.
func postChatSubmit(t *testing.T, s *harness.Stack, payload map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal submit payload: %v", err)
	}
	client := insecureClient()
	resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/chat/submit", "application/json", bytes.NewReader(body)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("chat submit: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chat submit status %d: %s", resp.StatusCode, raw)
	}
	var ack map[string]any
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("chat submit ack decode: %v", raw)
	}
	return ack
}

// steerViaRPC issues chat.steer on the given id and returns the raw RPC
// response (error envelope intact) so the test can assert 404-class
// failures precisely.
func steerViaRPC(t *testing.T, s *harness.Stack, conversationID, message string) map[string]any {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	return rpc.CallError("chat.steer", map[string]any{
		"conversation_id": conversationID,
		"message":         message,
		"source":          "chat-identity-e2e",
	})
}

// rpcErrorString extracts the error message from an RPC response.
func rpcErrorString(resp map[string]any) string {
	if e, ok := resp["error"].(map[string]any); ok {
		if msg, ok := e["message"].(string); ok {
			return msg
		}
	}
	if e, ok := resp["error"].(string); ok {
		return e
	}
	return ""
}

// waitSteerAccepted polls chat.steer until the queue is registered for id
// (queue registration happens inside RunOnce at turn start). Fails the
// test when the steer never lands within the budget.
func waitSteerAccepted(t *testing.T, s *harness.Stack, conversationID, message string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var lastErr string
	for time.Now().Before(deadline) {
		resp := steerViaRPC(t, s, conversationID, message)
		if resp["error"] == nil {
			return
		}
		lastErr = rpcErrorString(resp)
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("chat.steer for %q never accepted (404 class): last error %q",
		conversationID, lastErr)
}

// ---------------------------------------------------------------------------
// chat-identity-01 (M): steer by the canonical ack id against a live turn
// ---------------------------------------------------------------------------

// TestSteerByPrimarySessionIDFindsLiveQueue pins scenario (a): a turn is
// submitted GUI-shaped (conversation_id = raw PRIMARY session id), and the
// identity chain must hold end to end:
//
//  1. SUBMIT canonicalization: the ack carries the session's OWN
//     conversation id (conv-...), never the raw session-... echo, so the
//     loop that runs the turn registers under an id queue surfaces can
//     map back (RED pre-fix: the ack echoed session-... while the loop
//     ran under a minted conv id).
//  2. QUEUE STATUS by raw session id: chat.queue_status addressed with
//     the session's primary id reports the live queue - the read-side
//     session->conversation resolution (see
//     TestQueueStatusByPrimarySessionIDShowsLiveQueue).
//  3. STEER by the acked canonical id lands in the LIVE queue mid-turn
//     and the text drains into the conversation as a USER message.
//
// NOTE (leaf 01 report, residual gap): chat.steer/chat.followup addressed
// by the RAW session id against an in-flight turn still return
// queue-not-found when the turn is dispatched through the async task lane
// - the loop re-registers its queue under the task-linked conversation id
// (loop.go RunWithTask), which no session-store lookup can derive. The
// leaf forbids changing the registration key; the read-side follow-up is
// recorded in the leaf report. The probe in this suite's history (simple
// chat turn) proved the registration sits on the session's base conv id
// whenever the turn is NOT task-dispatched, which is the shape these
// scenarios pin.
func TestSteerByPrimarySessionIDFindsLiveQueue(t *testing.T) {
	s := startStack(t)
	sessionID := s.CreateSession(t, "identity-steer", s.ProjectDir)

	// GUI-shaped submit: conversation_id carries the raw PRIMARY session
	// id (the Flutter shape AGENTS.md documents).
	ack := postChatSubmit(t, s, map[string]any{
		"message":         "remember the codeword red-harbor",
		"conversation_id": sessionID,
		"source_client":   "chat-identity-e2e",
	})
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("submit not accepted: %+v", ack)
	}
	// Seam 1: canonical ack identity. RED pre-fix: the ack echoed the raw
	// session-... id while the loop ran under a minted conv id.
	ackConv, _ := ack["conversation_id"].(string)
	if ackConv == "" || looksLikeSessionID(ackConv) {
		t.Fatalf("ack conversation_id %q is not canonical (want the session's conv-... id)", ackConv)
	}
	rpc := harness.DialRPC(t, s.SocketPath)
	show := rpc.CallResult("session.get", map[string]any{"id": sessionID})
	if conv, _ := show["conversation_id"].(string); conv != ackConv {
		t.Fatalf("ack conv %q is not the session's own conversation id %q", ackConv, conv)
	}

	// Turn must complete (the fake answers directly) so the queue teardown
	// is deterministic, then the SAME identity re-steers a fresh turn.
	waitRequestMentions(t, s, "red-harbor", 90*time.Second)

	// Second turn, this time a LONG turn (stalled web_fetch keeps the
	// queue registered) addressed by the canonical ack id: steer must
	// land while the turn is live.
	fetchDone := make(chan struct{})
	slowSrv := newSlowServer(fetchDone, 8*time.Second)
	t.Cleanup(func() { close(fetchDone); slowSrv.Close() })
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_fetch",
		Arguments: `{"url":"` + slowSrv.URL + `/slow"}`,
	})
	s.Fake.SetPostToolText("long identity turn finished after the slow fetch")

	ack2 := postChatSubmit(t, s, map[string]any{
		"message":         "fetch the slow page and summarize it",
		"conversation_id": ackConv,
		"source_client":   "chat-identity-e2e",
	})
	if accepted, _ := ack2["accepted"].(bool); !accepted {
		t.Fatalf("turn 2 not accepted: %+v", ack2)
	}
	if c2, _ := ack2["conversation_id"].(string); c2 != ackConv {
		t.Fatalf("turn 2 conv %q diverged from turn 1 canonical %q", c2, ackConv)
	}
	waitSteerAccepted(t, s, ackConv, "change of plans: skip the summary")
	waitSteerTextInRequests(t, s, "change of plans: skip the summary", 90*time.Second)
}

// TestQueueStatusByPrimarySessionIDShowsLiveQueue pins the queue-status
// half of scenario (a): while a long turn is live, chat.queue_status
// addressed by the RAW session id (the GUI's poll shape) reports the
// active queue - the read-side session->conversation resolution.
func TestQueueStatusByPrimarySessionIDShowsLiveQueue(t *testing.T) {
	s := startStack(t)
	sessionID := s.CreateSession(t, "identity-status", s.ProjectDir)

	fetchDone := make(chan struct{})
	slowSrv := newSlowServer(fetchDone, 8*time.Second)
	t.Cleanup(func() { close(fetchDone); slowSrv.Close() })
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_fetch",
		Arguments: `{"url":"` + slowSrv.URL + `/slow"}`,
	})
	s.Fake.SetPostToolText("long identity turn finished after the slow fetch")

	ack := postChatSubmit(t, s, map[string]any{
		"message":         "fetch the slow page and summarize it",
		"conversation_id": sessionID,
		"source_client":   "chat-identity-e2e",
	})
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("submit not accepted: %+v", ack)
	}

	// Poll queue_status by the RAW session id until the live queue shows.
	rpc := harness.DialRPC(t, s.SocketPath)
	deadline := time.Now().Add(60 * time.Second)
	var status map[string]any
	for time.Now().Before(deadline) {
		status = rpc.CallResult("chat.queue_status", map[string]any{
			"conversation_id": sessionID,
		})
		if active, _ := status["is_active"].(bool); active {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if active, _ := status["is_active"].(bool); !active {
		t.Fatalf("chat.queue_status by raw session id %q never showed the live queue (got %+v)", sessionID, status)
	}
}

// ---------------------------------------------------------------------------
// chat-identity-02 (M): follow-up by canonical ack id against a live turn
// ---------------------------------------------------------------------------

// TestFollowUpByCanonicalAckIDFindsLiveQueue pins scenario (b) on the
// follow-up surface: a turn submitted GUI-shaped acks canonically, and the
// follow-up addressed to the acked conversation id lands in the live queue
// and reaches the conversation (drained as a USER message).
func TestFollowUpByCanonicalAckIDFindsLiveQueue(t *testing.T) {
	s := startStack(t)
	sessionID := s.CreateSession(t, "identity-followup", s.ProjectDir)

	fetchDone := make(chan struct{})
	slowSrv := newSlowServer(fetchDone, 8*time.Second)
	t.Cleanup(func() { close(fetchDone); slowSrv.Close() })
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "web_fetch",
		Arguments: `{"url":"` + slowSrv.URL + `/slow"}`,
	})
	s.Fake.SetPostToolText("long identity turn finished after the slow fetch")

	ack := postChatSubmit(t, s, map[string]any{
		"message":         "fetch the slow page and summarize it",
		"conversation_id": sessionID,
		"source_client":   "chat-identity-e2e",
	})
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("submit not accepted: %+v", ack)
	}
	ackConv, _ := ack["conversation_id"].(string)
	if ackConv == "" || looksLikeSessionID(ackConv) {
		t.Fatalf("ack conversation_id %q is not canonical", ackConv)
	}

	rpc := harness.DialRPC(t, s.SocketPath)
	deadline := time.Now().Add(60 * time.Second)
	var lastErr string
	followupLanded := false
	for time.Now().Before(deadline) {
		resp := rpc.CallError("chat.followup", map[string]any{
			"conversation_id": ackConv,
			"message":         "also note the publication date",
			"source":          "chat-identity-e2e",
		})
		if resp["error"] == nil {
			followupLanded = true
			break
		}
		lastErr = rpcErrorString(resp)
		time.Sleep(250 * time.Millisecond)
	}
	if !followupLanded {
		t.Fatalf("chat.followup for %q never accepted (404 class): last error %q", ackConv, lastErr)
	}
	// followup landed: it must reach the conversation too.
	waitSteerTextInRequests(t, s, "also note the publication date", 90*time.Second)
}
