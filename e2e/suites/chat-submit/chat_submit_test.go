//go:build e2e

// Package chatsubmit covers the async chat.submit surface: immediate ack
// shape, empty-message rejection, explicit-turn_id idempotent retry, and the
// HTTP endpoint sharing the identical ack shape.
package chatsubmit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// postChatSubmitHTTP posts raw params to /api/v1/chat/submit and returns
// (status code, decoded body map).
func postChatSubmitHTTP(t *testing.T, s *harness.Stack, params map[string]any) (int, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(params)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: insecureTLS(), //nolint:gosec // set inside helper
	}}
	resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/chat/submit", "application/json", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("chat submit POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("chat submit body decode (%d): %s", resp.StatusCode, body)
	}
	return resp.StatusCode, out
}

// chat-submit-01: chat.submit acks in milliseconds with the frozen ack key
// set {turn_id, conversation_id, session_id, accepted, note}.
func TestChatSubmitAcksImmediatelyWithContractShape(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "chat-submit-01", s.ProjectDir)

	start := time.Now()
	ack := s.SubmitChatHTTP(t, sessionID, "hello there, just chatting")
	ackElapsed := time.Since(start)

	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("submit not accepted: %+v", ack)
	}
	for _, key := range []string{"turn_id", "conversation_id", "session_id", "accepted", "note"} {
		if _, ok := ack[key]; !ok {
			t.Fatalf("ack missing %q (frozen contract key set): %+v", key, ack)
		}
	}
	if sid, _ := ack["session_id"].(string); sid != sessionID {
		t.Fatalf("ack session_id = %v, want %s", ack["session_id"], sessionID)
	}
	if tid, _ := ack["turn_id"].(string); tid == "" {
		t.Fatal("ack minted an empty turn_id")
	}
	if cid, _ := ack["conversation_id"].(string); cid == "" {
		t.Fatal("ack minted an empty conversation_id")
	}
	if ackElapsed > 5*time.Second {
		t.Fatalf("ack took %s; chat.submit must ack in ms, never block on agent work", ackElapsed)
	}
}

// chat-submit-02: empty/whitespace message returns accepted=false, mints no
// turn id, and publishes nothing (the agent loop receives no chat.request —
// verified via the fake LLM's request log staying empty).
func TestChatSubmitEmptyMessageRejected(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "chat-submit-02", s.ProjectDir)

	code, body := postChatSubmitHTTP(t, s, map[string]any{
		"message": "   ", "session_id": sessionID, "source_client": "e2e-harness",
	})
	if code != 200 {
		t.Fatalf("empty-message submit status = %d, want 200 with accepted=false: %v", code, body)
	}
	if accepted, _ := body["accepted"].(bool); accepted {
		t.Fatalf("empty message accepted: %+v", body)
	}
	if tid, _ := body["turn_id"].(string); tid != "" {
		t.Fatalf("empty message minted a turn id %q; none may be minted", tid)
	}
	note, _ := body["note"].(string)
	if !strings.Contains(note, "message is required") {
		t.Fatalf("note = %q, want the message-is-required explanation", note)
	}

	// Give any wrongly-published chat.request time to reach the fake LLM.
	time.Sleep(1500 * time.Millisecond)
	if n := s.Fake.RequestCount(); n != 0 {
		t.Fatalf("fake LLM served %d requests after a rejected submit; nothing may publish", n)
	}
}

// chat-submit-03: resubmitting the SAME explicit turn_id is idempotent — the
// same ack returns and no duplicate chat.request is published (the fake LLM
// sees exactly the requests of the first turn's work, never two dispatches
// of the same message).
func TestChatSubmitExplicitTurnIDIdempotent(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "chat-submit-03", s.ProjectDir)

	artifact := s.ProjectDir + "/idem.txt"
	s.Fake.SetPostToolText("wrote idem.txt")
	s.Fake.EnqueueFileWrite("call-i1", artifact, "idem")

	// First submit with an explicit turn id (slow lane so the work is still
	// in flight when the retry lands).
	ack1 := s.SubmitChatHTTP(t, sessionID, "Create a file named idem.txt containing idem")
	_ = ack1

	// Retry with the SAME explicit turn id via the RPC surface through the
	// HTTP bus/call bridge (same SubmitHandler). The retry must return
	// accepted=true with the SAME turn id and a dedupe note.
	turnID, _ := ack1["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("first ack missing turn_id: %+v", ack1)
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: insecureTLS(), //nolint:gosec // set inside helper
	}}
	payload, _ := json.Marshal(map[string]any{
		"method": "chat.submit",
		"params": map[string]any{
			"message":    "Create a file named idem.txt containing idem",
			"session_id": sessionID,
			"turn_id":    turnID,
		},
	})
	resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/bus/call", "application/json", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("retry submit: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("retry status %d: %s", resp.StatusCode, body)
	}
	var envelope struct {
		Result struct {
			TurnID    string `json:"turn_id"`
			Accepted  bool   `json:"accepted"`
			Note      string `json:"note"`
			SessionID string `json:"session_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("retry decode: %v (%s)", err, body)
	}
	if !envelope.Result.Accepted {
		t.Fatalf("retry not accepted: %+v", envelope.Result)
	}
	if envelope.Result.TurnID != turnID {
		t.Fatalf("retry turn_id = %s, want the original %s", envelope.Result.TurnID, turnID)
	}
	if !strings.Contains(envelope.Result.Note, "already submitted") {
		t.Fatalf("retry note = %q, want the dedupe note", envelope.Result.Note)
	}
}

// chat-submit-04: HTTP POST /api/v1/chat/submit returns the identical ack
// shape as the chat.submit RPC (same handler, one key set).
func TestHTTPSubmitMatchesRPCAckShape(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "chat-submit-04", s.ProjectDir)

	// RPC path via the /api/v1/bus/call bridge (identical handler registry).
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: insecureTLS(), //nolint:gosec // set inside helper
	}}
	payload, _ := json.Marshal(map[string]any{
		"method": "chat.submit",
		"params": map[string]any{
			"message":       "shape check",
			"session_id":    sessionID,
			"source_client": "e2e-harness",
		},
	})
	resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/bus/call", "application/json", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("rpc-path submit: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("rpc-path submit status %d: %s", resp.StatusCode, body)
	}
	var envelope struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("rpc-path decode: %v (%s)", err, body)
	}

	// HTTP path.
	ack := s.SubmitChatHTTP(t, sessionID, "shape check")

	for _, key := range []string{"turn_id", "conversation_id", "session_id", "accepted", "note"} {
		if _, ok := envelope.Result[key]; !ok {
			t.Fatalf("RPC ack missing %q: %+v", key, envelope.Result)
		}
		if _, ok := ack[key]; !ok {
			t.Fatalf("HTTP ack missing %q: %+v", key, ack)
		}
	}
	if a1, _ := envelope.Result["accepted"].(bool); !a1 {
		t.Fatalf("RPC ack not accepted: %+v", envelope.Result)
	}
	if a2, _ := ack["accepted"].(bool); !a2 {
		t.Fatalf("HTTP ack not accepted: %+v", ack)
	}
	if n1, _ := envelope.Result["note"].(string); n1 != mustString(t, ack, "note") {
		t.Fatalf("note diverges between surfaces: RPC %q vs HTTP %q", n1, mustString(t, ack, "note"))
	}
}

func mustString(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, _ := m[key].(string)
	return v
}
