package rpc

// chat_submit_thread_pair_test.go — bughunt 11-24 H9 pin (submit half).
// Thread-scoped ids were being resolved to their owning session by
// buildAck's canonicalization, so a thread submit published ONLY the
// collapsed base ids: the ChatHandler 11-23 '-thread-' exemption never
// fired and the thread's LLM continuity lane collapsed onto the parent
// conversation. The fix stamps the ORIGINAL thread id into
// thread_session_id (additive, omitted for non-thread submits); the
// handler half is pinned in internal/agent
// (handler_thread_lane_pair_test.go) against the same wire shape.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
)

func TestChatSubmit_ThreadScopedSessionIDStampedInPayload(t *testing.T) {
	h, _, sub := newGateValidateHandler(t)

	// session_id-shaped submit INTO A THREAD: the thread id unwraps to
	// sess-gate-a through ResolveThreadConversationID, so validation
	// passes and the payload collapses — the stamp is what preserves the
	// thread identity.
	threadID := "conv-gate-a" + session.ThreadIDSeparator + "general-1"
	ack := gateSubmitAck(t, h, `{"message":"in the thread","session_id":"`+threadID+`","turn_id":"turn-thread-1"}`)
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("thread submit rejected: %v", ack["note"])
	}

	payload := waitForChatRequest(t, sub)
	if got, _ := payload["conversation_id"].(string); got != "conv-gate-a" {
		t.Errorf("conversation_id = %q, want the resolved base conv-gate-a (canonicalization unchanged)", got)
	}
	stamped, ok := payload["thread_session_id"]
	if !ok {
		t.Fatal("thread_session_id missing — handleRequest cannot restore the thread lane (H9)")
	}
	if stamped != threadID {
		t.Errorf("thread_session_id = %v, want %q", stamped, threadID)
	}
	// The ack contract stays frozen (5 keys).
	if len(ack) != 5 {
		t.Errorf("ack has %d keys, want the frozen 5-key set", len(ack))
	}
}

func TestChatSubmit_ThreadScopedConversationIDStampedInPayload(t *testing.T) {
	h, _, sub := newGateValidateHandler(t)

	// GUI shape: the THREAD id arrives as conversation_id (Flutter sends
	// the thread-scoped conversation id), no session_id.
	threadID := "conv-gate-b" + session.ThreadIDSeparator + "work-2"
	ack := gateSubmitAck(t, h, `{"message":"gui thread","conversation_id":"`+threadID+`","turn_id":"turn-thread-2"}`)
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("thread-shaped conversation submit rejected: %v", ack["note"])
	}
	payload := waitForChatRequest(t, sub)
	if stamped, ok := payload["thread_session_id"]; !ok || stamped != threadID {
		t.Errorf("thread_session_id = %v (present=%v), want %q", stamped, ok, threadID)
	}
}

func TestChatSubmit_NonThreadPayloadByteIdentical(t *testing.T) {
	h, _, sub := newGateValidateHandler(t)

	// Plain (non-thread) submit: the payload key set must stay the
	// frozen 7 keys — thread_session_id is OMITTED, never an empty
	// string (omitempty semantics).
	gateSubmitAck(t, h, `{"message":"plain","session_id":"sess-gate-a","turn_id":"turn-plain-1"}`)
	raw := waitRawChatRequest(t, sub)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["thread_session_id"]; ok {
		t.Errorf("non-thread submit carries thread_session_id: %s", raw)
	}
	if len(payload) != 7 {
		t.Errorf("payload has %d keys, want the frozen 7 (message, conversation_id, session_id, agent_id, source_client, model, turn_id): %s", len(payload), raw)
	}
}

// waitRawChatRequest returns the raw payload bytes of the next
// chat.request (byte-level key assertions).
func waitRawChatRequest(t *testing.T, sub *bus.Subscriber) json.RawMessage {
	t.Helper()
	select {
	case msg := <-sub.Channel:
		if msg.Topic != "chat.request" {
			t.Fatalf("message topic = %q, want chat.request", msg.Topic)
		}
		return msg.Payload
	case <-time.After(5 * time.Second):
		t.Fatal("no chat.request published")
		return nil
	}
}
