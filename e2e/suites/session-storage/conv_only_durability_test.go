//go:build e2e

// 03-context TEST-GATE T6 (docs/plans/20261119-usability-remediation/
// 03-context/00T-gate.md, branch matrix F9; master contract C4; findings
// F-ORCH-22/F-ORCH-29, W11).
//
// Live-daemon extensions of the session-storage suite:
//
//   - session-storage-03: conversation-only dialog durability. A
//     chat.submit carrying ONLY a conversation_id must end with its
//     user+assistant exchange readable through session.messages.get
//     (today the persist hits FOREIGN KEY 787 and history is lost).
//   - session-storage-04: submit identity validation over the wire.
//     Unknown session/conversation ids are rejected at the ack
//     (accepted:false + actionable note), a mismatched both-ids pair is
//     corrected or rejected, and legitimate ids keep working.
//
// RED today: the durability arm finds no history rows (FK 787); the
// validation arms see accepted:true for unknown ids. GREEN after
// 03-context/01. Control arms pass today.
package session_storage_test

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// gatePostSubmit posts an arbitrary chat.submit payload to the HTTP
// endpoint and returns (status, decoded ack).
func gatePostSubmit(t *testing.T, s *harness.Stack, params map[string]any) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal submit params: %v", err)
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
		},
	}
	resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/chat/submit", "application/json", bytes.NewReader(payload)) //nolint:noctx // bounded test call
	if err != nil {
		t.Fatalf("chat submit POST: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read submit body: %v", err)
	}
	var ack map[string]any
	if err := json.Unmarshal(body, &ack); err != nil {
		t.Fatalf("decode submit ack (%d): %s", resp.StatusCode, body)
	}
	return resp.StatusCode, ack
}

// gateTerminalCollector subscribes to turn.terminal on ONE persistent RPC
// connection (bus subscriptions die with their connection) and waits for
// the named turn's non-parked terminal.
type gateTerminalCollector struct {
	rpc   *harness.RPCClient
	subID string
}

func gateNewTerminalCollector(t *testing.T, s *harness.Stack) *gateTerminalCollector {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	res := rpc.CallResult("bus.subscribe", map[string]any{"topics": []string{"turn.terminal"}})
	subID, _ := res["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", res)
	}
	return &gateTerminalCollector{rpc: rpc, subID: subID}
}

// waitFinal polls until the turn's non-parked terminal arrives.
func (c *gateTerminalCollector) waitFinal(t *testing.T, turnID string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res := c.rpc.CallResult("bus.poll", map[string]string{"subscription_id": c.subID})
		if events, ok := res["events"].([]any); ok {
			for _, evAny := range events {
				ev, ok := evAny.(map[string]any)
				if !ok || ev["topic"] != "turn.terminal" {
					continue
				}
				payload, ok := ev["payload"].(map[string]any)
				if !ok || payload["turn_id"] != turnID {
					continue
				}
				if payload["status"] == "parked" {
					continue
				}
				return payload
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no non-parked turn.terminal for %s within %s", turnID, timeout)
	return nil
}

// gateMessagesGet reads session.messages.get over RPC.
func gateMessagesGet(t *testing.T, s *harness.Stack, sessionID string) map[string]any {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	return rpc.CallResult("session.messages.get", map[string]any{
		"session_id": sessionID, "offset": 0, "limit": 100,
	})
}

// TestConvOnlyDialogDurability pins C4 history durability over the live
// daemon: a conversation-only submit's exchange must be readable after
// the turn completes (F-ORCH-29: FK 787 silently dropped it).
//
// Contract alignment (03-context/01 T1, landed): chat.submit VALIDATES ids
// - an unknown conversation id is rejected accepted:false ("create a
// session first"), never a fabricated phantom session (F-ORCH-22, pinned
// by internal/rpc/chat_submit_validate_test.go TestUnknownIDRejected). The
// durability scenario is therefore the GUI/bench shape that actually
// reaches the daemon: conversation_id ONLY (no session_id) for an EXISTING
// session. The finding-3 owner resolution persists the exchange under the
// session's primary id, so session.messages.get on the primary id must see
// user + assistant rows (the upsert-before-persist lane, never FK 787).
func TestConvOnlyDialogDurability(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")

	// The real session behind the conversation id, then read its own conv
	// id back (the shape the Flutter GUI sends as conversation_id).
	sessionID := s.CreateSession(t, "gate-conv-only", s.ProjectDir)
	rpc := harness.DialRPC(t, s.SocketPath)
	sess := rpc.CallResult("session.get", map[string]any{"id": sessionID})
	convID, _ := sess["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("session.get returned no conversation_id: %v", sess)
	}

	const userMsg = "hello from a conversation-only submit, please reply"
	// The direct chat loop's completion request carries tool schemas, so
	// the fake serves it through the executor arm: pin BOTH shapes to the
	// same reply (chat text for tool-less calls, post-tool text once the
	// scripted tool queue is empty).
	s.Fake.SetChatText("hello back from the assistant")
	s.Fake.SetPostToolText("hello back from the assistant")
	// Pin the chat lane (direct loop): the bare message otherwise drifts
	// into the dispatched task lane and the persisted assistant row is the
	// async ack, not the chat reply this durability pin reads back.
	s.Fake.Script(harness.IsClassifierRequest(),
		harness.ClassifierJSONResponse(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`))

	coll := gateNewTerminalCollector(t, s)
	code, ack := gatePostSubmit(t, s, map[string]any{
		"message":         userMsg,
		"conversation_id": convID,
		"source_client":   "e2e-gate",
	})
	if code != http.StatusOK {
		t.Fatalf("conv-only submit status %d: %v", code, ack)
	}
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("conv-only submit rejected for a KNOWN session's conversation id: %v", ack)
	}
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %v", ack)
	}

	terminal := coll.waitFinal(t, turnID, 120*time.Second)
	if terminal["status"] != "completed" {
		t.Fatalf("conv-only turn did not complete: %v", terminal)
	}

	// The exchange must be durable: user + assistant rows readable via
	// session.messages.get keyed on the session's primary id (finding-3
	// owner stamping). Read both id shapes defensively - the durable rows
	// must land under exactly one of them, never vanish to FK 787.
	readable := func() (string, int) {
		for _, key := range []string{sessionID, convID} {
			res := gateMessagesGet(t, s, key)
			msgs, _ := res["messages"].([]any)
			if len(msgs) >= 2 {
				raw, err := json.Marshal(res)
				if err != nil {
					t.Fatalf("marshal messages: %v", err)
				}
				return string(raw), len(msgs)
			}
		}
		return "", 0
	}
	harness.WaitFor(t, 15*time.Second, "conv-only history rows", func() bool {
		_, n := readable()
		return n >= 2
	})
	raw, _ := readable()
	if !strings.Contains(raw, userMsg) {
		t.Fatalf("RED (F-ORCH-29): conv-only user message lost from history (FK 787): %s", raw)
	}
	if !strings.Contains(raw, "hello back from the assistant") {
		t.Fatalf("RED (F-ORCH-29): conv-only assistant reply lost from history (FK 787): %s", raw)
	}
}

// TestSubmitValidationOverWire pins C4 submit validation on the live
// daemon (F-ORCH-22): unknown ids rejected with an actionable note, no
// phantom conversation; mismatched both-ids pairs corrected or rejected.
func TestSubmitValidationOverWire(t *testing.T) {
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "gate-validation", s.ProjectDir)

	// Resolve the session's own conversation id for the controls.
	rpc := harness.DialRPC(t, s.SocketPath)
	sess := rpc.CallResult("session.get", map[string]any{"id": sessionID})
	ownConv, _ := sess["conversation_id"].(string)
	if ownConv == "" {
		t.Fatalf("session.get returned no conversation_id: %v", sess)
	}

	t.Run("unknown session_id rejected", func(t *testing.T) {
		code, ack := gatePostSubmit(t, s, map[string]any{
			"message":    "hello",
			"session_id": "session-doesnotexist999",
		})
		if code != http.StatusOK {
			t.Fatalf("status %d: %v", code, ack)
		}
		if accepted, _ := ack["accepted"].(bool); accepted {
			t.Fatalf("RED (F-ORCH-22): unknown session_id accepted, phantom conv fabricated: %v", ack)
		}
		note, _ := ack["note"].(string)
		if !strings.Contains(strings.ToLower(note), "unknown") {
			t.Errorf("note = %q, want an actionable unknown-id explanation", note)
		}
		if tid, _ := ack["turn_id"].(string); tid != "" {
			t.Errorf("rejected submit minted turn_id %q", tid)
		}
	})

	t.Run("unknown conversation_id rejected", func(t *testing.T) {
		code, ack := gatePostSubmit(t, s, map[string]any{
			"message":         "hello",
			"conversation_id": "conv-doesnotexist999",
		})
		if code != http.StatusOK {
			t.Fatalf("status %d: %v", code, ack)
		}
		if accepted, _ := ack["accepted"].(bool); accepted {
			t.Fatalf("RED (F-ORCH-22): unknown conversation_id accepted: %v", ack)
		}
	})

	t.Run("mismatched both-ids corrected or rejected", func(t *testing.T) {
		// A second real session supplies a FOREIGN conversation id.
		otherID := s.CreateSession(t, "gate-validation-other", s.ProjectDir)
		other := rpc.CallResult("session.get", map[string]any{"id": otherID})
		otherConv, _ := other["conversation_id"].(string)
		if otherConv == "" || otherConv == ownConv {
			t.Fatalf("fixture: second session conversation unresolved: %v", other)
		}
		code, ack := gatePostSubmit(t, s, map[string]any{
			"message":         "hello",
			"session_id":      sessionID,
			"conversation_id": otherConv,
		})
		if code != http.StatusOK {
			t.Fatalf("status %d: %v", code, ack)
		}
		accepted, _ := ack["accepted"].(bool)
		convID, _ := ack["conversation_id"].(string)
		if accepted && convID != ownConv {
			t.Fatalf("RED (C4): mismatched both-ids pair accepted as submitted (conversation_id=%v, want correction to %v or rejection): %v",
				convID, ownConv, ack)
		}
	})

	// Controls (pass today): legitimate submits keep working.
	t.Run("known session accepted", func(t *testing.T) {
		code, ack := gatePostSubmit(t, s, map[string]any{
			"message":    "hello there, just chatting",
			"session_id": sessionID,
		})
		if code != http.StatusOK {
			t.Fatalf("status %d: %v", code, ack)
		}
		if accepted, _ := ack["accepted"].(bool); !accepted {
			t.Fatalf("known session rejected: %v", ack)
		}
		if ack["conversation_id"] != ownConv {
			t.Errorf("conversation_id = %v, want the session's own %v", ack["conversation_id"], ownConv)
		}
	})
	t.Run("consistent pair accepted", func(t *testing.T) {
		code, ack := gatePostSubmit(t, s, map[string]any{
			"message":         "hello again",
			"session_id":      sessionID,
			"conversation_id": ownConv,
		})
		if code != http.StatusOK {
			t.Fatalf("status %d: %v", code, ack)
		}
		if accepted, _ := ack["accepted"].(bool); !accepted {
			t.Fatalf("consistent pair rejected: %v", ack)
		}
	})
}
