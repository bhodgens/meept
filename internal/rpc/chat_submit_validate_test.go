package rpc

// 03-context TEST-GATE T1 (docs/plans/20261119-usability-remediation/
// 03-context/00T-gate.md, master contract C4; findings F-ORCH-22, W11).
//
// chat.submit must validate session identity BEFORE publishing:
//   - an unknown session_id / conversation_id is rejected with
//     accepted:false + an actionable note; no phantom conversation is
//     fabricated and nothing is published (F-ORCH-22: today unknown ids
//     are accepted and a fresh conv-* id is minted).
//   - when BOTH ids are present they must pass a membership check:
//     the pair is corrected to the session's owning conversation or
//     rejected; a mismatched pair is never silently accepted.
//   - legitimate thread-scoped conversation ids are unwrapped before
//     validation and never rejected (01-session-history T1 contract).
//
// RED today: buildAck resolves a conversation only when the field is
// empty (chat_submit.go:149-159); unknown ids fall through to
// id.Generate("conv-") and accepted:true. GREEN after 03-context/01.
// Control arms (known ids, consistent pair, thread id) pass today and
// must keep passing; they prove the fixture is not the failure reason.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
)

// gateValidateStore is a chatSubmitSessionStore double holding a fixed
// session set (the daemon wires the real session store through
// SetSessionStore; validation must consult exactly this surface).
type gateValidateStore struct {
	sessions map[string]*session.Session
}

func (s *gateValidateStore) Get(id string) *session.Session {
	return s.sessions[id]
}

func (s *gateValidateStore) GetByConversationID(conversationID string) *session.Session {
	for _, sess := range s.sessions {
		if sess.ConversationID == conversationID {
			return sess
		}
	}
	return nil
}

// newGateValidateHandler builds a SubmitHandler with the session store
// wired (the production shape) and a fake registry for dedupe tracking.
func newGateValidateHandler(t *testing.T) (*SubmitHandler, *bus.MessageBus, *bus.Subscriber) {
	t.Helper()
	msgBus := bus.New(nil, nil)
	t.Cleanup(func() { msgBus.Close() })
	h := NewSubmitHandler(msgBus, newFakeSubmitRegistry(), nil)
	h.SetSessionStore(&gateValidateStore{sessions: map[string]*session.Session{
		"sess-gate-a": {ID: "sess-gate-a", ConversationID: "conv-gate-a"},
		"sess-gate-b": {ID: "sess-gate-b", ConversationID: "conv-gate-b"},
	}})
	sub := msgBus.Subscribe("gate-validate", "chat.request")
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	return h, msgBus, sub
}

// gateSubmitAck submits and returns the decoded ack map.
func gateSubmitAck(t *testing.T, h *SubmitHandler, params string) map[string]any {
	t.Helper()
	ackAny, err := h.BuildChatSubmitAck(context.Background(), json.RawMessage(params))
	if err != nil {
		t.Fatalf("chat.submit returned error: %v", err)
	}
	ack, ok := ackAny.(map[string]any)
	if !ok {
		t.Fatalf("ack type = %T, want map[string]any", ackAny)
	}
	return ack
}

// gateAssertNoPublish fails when a chat.request was published.
func gateAssertNoPublish(t *testing.T, sub *bus.Subscriber, what string) {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		t.Fatalf("%s published chat.request: %s", what, msg.Payload)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestUnknownIDRejected pins C4 validation: unknown session_id and
// unknown conversation_id are rejected at the ack with an actionable
// note, no turn id minted, no conversation fabricated, nothing published.
func TestUnknownIDRejected(t *testing.T) {
	h, _, sub := newGateValidateHandler(t)

	cases := []struct {
		name   string
		params string
	}{
		{
			name:   "unknown session_id (F-ORCH-22 F3)",
			params: `{"message":"hello","session_id":"session-doesnotexist999"}`,
		},
		{
			name:   "unknown conversation_id (F-ORCH-22 F4)",
			params: `{"message":"hello","conversation_id":"conv-doesnotexist999"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ack := gateSubmitAck(t, h, tc.params)
			if ack["accepted"] != false {
				t.Fatalf("accepted = %v, want false for an unknown id (C4: never fabricate a phantom session); ack = %v",
					ack["accepted"], ack)
			}
			note, _ := ack["note"].(string)
			lower := strings.ToLower(note)
			if !strings.Contains(lower, "unknown") || !strings.Contains(lower, "create") {
				t.Errorf("note = %q, want an actionable rejection naming the unknown id and the create-first remedy (01-session-history T1)", note)
			}
			if turnID, _ := ack["turn_id"].(string); turnID != "" {
				t.Errorf("rejected submit minted turn_id %q; none may be minted", turnID)
			}
			if len(ack) != 5 {
				t.Errorf("ack has %d keys, want the frozen 5-key set", len(ack))
			}
			gateAssertNoPublish(t, sub, "unknown-id submit")
		})
	}

	// Control (passes today): a KNOWN session id resolves to the session's
	// own conversation and is accepted. Proves the store fixture works and
	// the rejection arms above fail for the missing validation, not a
	// broken fixture.
	t.Run("known session id still accepted", func(t *testing.T) {
		ack := gateSubmitAck(t, h, `{"message":"hello","session_id":"sess-gate-a"}`)
		if ack["accepted"] != true {
			t.Fatalf("known session rejected: %v", ack)
		}
		if ack["conversation_id"] != "conv-gate-a" {
			t.Errorf("conversation_id = %v, want the session's own conv-gate-a", ack["conversation_id"])
		}
	})
}

// TestBothIDMismatch pins the C4 membership check: when both ids are
// present the pair must belong together. A mismatched pair is corrected
// to the owning conversation or rejected - never accepted as submitted.
func TestBothIDMismatch(t *testing.T) {
	h, _, sub := newGateValidateHandler(t)

	cases := []struct {
		name   string
		params string
	}{
		{
			name:   "session A paired with session B's conversation",
			params: `{"message":"hello","session_id":"sess-gate-a","conversation_id":"conv-gate-b"}`,
		},
		{
			name:   "session A paired with an unknown conversation",
			params: `{"message":"hello","session_id":"sess-gate-a","conversation_id":"conv-doesnotexist999"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ack := gateSubmitAck(t, h, tc.params)
			accepted, _ := ack["accepted"].(bool)
			convID, _ := ack["conversation_id"].(string)
			switch {
			case !accepted:
				// Rejection arm: actionable note, no turn minted, no publish.
				note, _ := ack["note"].(string)
				if strings.TrimSpace(note) == "" {
					t.Errorf("rejected mismatched pair carries no note: %v", ack)
				}
				if turnID, _ := ack["turn_id"].(string); turnID != "" {
					t.Errorf("rejected mismatch minted turn_id %q", turnID)
				}
				gateAssertNoPublish(t, sub, "mismatched both-ids submit")
			case accepted && convID == "conv-gate-a":
				// Correction arm: the ack (and the published payload) must
				// carry the OWNING conversation, never the submitted one.
				payload := waitForChatRequest(t, sub)
				if payload["conversation_id"] != "conv-gate-a" {
					t.Errorf("published conversation_id = %v, want the owning conv-gate-a", payload["conversation_id"])
				}
			default:
				t.Fatalf("mismatched both-ids pair accepted as submitted (conversation_id = %v); C4 requires correction to the owning conversation or rejection; ack = %v",
					convID, ack)
			}
		})
	}

	// Controls (pass today): a consistent pair is preserved, and a
	// thread-scoped conversation id belonging to a known session is never
	// rejected (thread unwrap before validation, 01-session-history T1).
	t.Run("consistent pair preserved", func(t *testing.T) {
		ack := gateSubmitAck(t, h, `{"message":"hello","session_id":"sess-gate-a","conversation_id":"conv-gate-a"}`)
		if ack["accepted"] != true || ack["conversation_id"] != "conv-gate-a" {
			t.Fatalf("consistent pair not preserved: %v", ack)
		}
	})
	t.Run("legitimate thread id accepted", func(t *testing.T) {
		ack := gateSubmitAck(t, h, `{"message":"hello","conversation_id":"conv-gate-a-thread-topic-1"}`)
		if ack["accepted"] != true {
			t.Fatalf("legitimate thread-scoped id rejected: %v (validation must unwrap thread ids, never reject them)", ack)
		}
	})
}

// TestChatSubmitPayloadCarriesOwnerPrimarySessionID pins the bughunt-wave2
// finding-3 fix: the published chat.request payload's session_id must carry
// the resolved owning session's PRIMARY id. A GUI-shaped submit sends an
// EMPTY session_id and conversation_id = the session primary id; before the
// fix the payload kept the empty value, so ChatHandler persisted under
// persistID=conversationID (session_messages keyed conv-... while history
// readers query the session primary id — the GUI transcript lost its own
// turns on reload) and WS frames carried the empty/conv id against grant
// sets keyed by the session id. The ack's own fields stay unchanged.
func TestChatSubmitPayloadCarriesOwnerPrimarySessionID(t *testing.T) {
	h, _, sub := newGateValidateHandler(t)

	// GUI-shaped: no session_id, conversation_id = the session PRIMARY id.
	ack := gateSubmitAck(t, h, `{"message":"hello gui","conversation_id":"sess-gate-a","turn_id":"turn-gui-1"}`)
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("gui-shaped submit rejected: %v", ack["note"])
	}
	// The ack echoes the client's submitted session_id (empty) verbatim —
	// the fix must not change the ack contract.
	if sid, _ := ack["session_id"].(string); sid != "" {
		t.Errorf("ack session_id = %q, want empty (ack fields unchanged)", sid)
	}

	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("chat.request payload unmarshal: %v", err)
		}
		if got, _ := payload["session_id"].(string); got != "sess-gate-a" {
			t.Errorf("chat.request payload session_id = %q, want the owner primary id %q", got, "sess-gate-a")
		}
		// The conversation must still be canonicalized to the session's
		// OWN conv id (leaf 01 contract, unchanged by this fix).
		if got, _ := payload["conversation_id"].(string); got != "conv-gate-a" {
			t.Errorf("chat.request payload conversation_id = %q, want conv-gate-a", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no chat.request published for the gui-shaped submit")
	}
}

// TestChatSubmitPayloadSessionIDFromExplicitSession: the session_id-shaped
// submit keeps carrying the resolved primary id in the payload (unchanged
// behavior — regression guard for the finding-3 stamp).
func TestChatSubmitPayloadSessionIDFromExplicitSession(t *testing.T) {
	h, _, sub := newGateValidateHandler(t)

	ack := gateSubmitAck(t, h, `{"message":"hello cli","session_id":"sess-gate-b","turn_id":"turn-cli-1"}`)
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("session-shaped submit rejected: %v", ack["note"])
	}

	select {
	case msg := <-sub.Channel:
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("chat.request payload unmarshal: %v", err)
		}
		if got, _ := payload["session_id"].(string); got != "sess-gate-b" {
			t.Errorf("chat.request payload session_id = %q, want sess-gate-b", got)
		}
		if got, _ := payload["conversation_id"].(string); got != "conv-gate-b" {
			t.Errorf("chat.request payload conversation_id = %q, want conv-gate-b", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no chat.request published for the session-shaped submit")
	}
}
