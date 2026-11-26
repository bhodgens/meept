package agent

// handler_thread_lane_pair_test.go — bughunt 11-24 H9 pin (handler half).
// The submit side (internal/rpc, chat_submit_thread_pair_test.go) stamps
// the ORIGINAL thread-scoped id into thread_session_id because its own
// canonicalization collapses both conversation_id and session_id to the
// base shapes. This file pins the consuming half: handleRequest must
// route a thread-scoped submit to the THREAD's continuity lane (observed
// through the chat.processing event's conversation_id, same discriminator
// as TestHandleRequestThreadScopedSessionIDKeepsThreadIdentity), while a
// payload WITHOUT the key (legacy publisher / non-thread submit) keeps
// the base-collapse behavior byte-identically.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/models"
)

func h9PairFixture(t *testing.T, tag string) (*ChatHandler, *bus.MessageBus, *bus.Subscriber) {
	t.Helper()
	msgBus := bus.New(nil, slogDiscardLogger())
	loop := NewAgentLoop("h9-pair-"+tag, "/tmp")
	h := NewChatHandler(loop, nil, msgBus, slogDiscardLogger())
	const baseConv = "conv-h9-base"
	store := &stubSessionStore{
		sessions: map[string]*session.Session{
			baseConv: {ID: "session-h9-primary", ConversationID: baseConv},
		},
	}
	h.SetSessionStore(store)
	sub := msgBus.Subscribe("h9-pair-"+tag, "chat.processing")
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	return h, msgBus, sub
}

// driveRequestWithThreadStamp publishes a RAW submit-shaped chat.request
// payload (the wire shape rpc/chat_submit.go builds — collapsed base ids
// + thread_session_id) straight onto the bus topic handleRequest
// consumes, so the pin drives the REAL pairing seam (json.Unmarshal into
// ChatRequest + the canonicalization), not a hand-built struct.
func driveRequestWithThreadStamp(t *testing.T, h *ChatHandler, msgBus *bus.MessageBus, sub *bus.Subscriber, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	msg := &models.BusMessage{
		ID:        "h9-req",
		Type:      models.MessageTypeRequest,
		Topic:     "chat.request",
		Source:    "test",
		Timestamp: time.Now().UTC(),
		Payload:   raw,
	}
	done := make(chan struct{})
	go func() {
		h.handleRequest(context.Background(), msg)
		close(done)
	}()
	select {
	case m := <-sub.Channel:
		var event map[string]any
		if err := json.Unmarshal(m.Payload, &event); err != nil {
			t.Fatalf("unmarshal chat.processing: %v", err)
		}
		conv, _ := event["conversation_id"].(string)
		return conv
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for chat.processing")
		return ""
	}
}

// TestHandleRequest_SubmitShapeThreadStampRestoresThreadLane pins the
// pair: a chat.submit-shaped payload (conversation_id + session_id both
// COLLAPSED to base, exactly what buildAck publishes for a thread
// submit) with thread_session_id carrying the original thread id must
// run the turn under the THREAD identity, not the base conversation.
// Pre-fix the key did not exist and the exemption never fired: the
// thread lane collapsed onto the parent.
func TestHandleRequest_SubmitShapeThreadStampRestoresThreadLane(t *testing.T) {
	h, msgBus, sub := h9PairFixture(t, "a")
	const baseConv = "conv-h9-base"
	threadID := baseConv + session.ThreadIDSeparator + "general-9"

	conv := driveRequestWithThreadStamp(t, h, msgBus, sub, map[string]any{
		"message":           "hello thread via submit",
		"conversation_id":   baseConv, // submit collapsed it (H9 defect)
		"session_id":        "session-h9-primary",
		"turn_id":           "turn-h9-a",
		"thread_session_id": threadID, // the fix's stamp
	})
	if conv != threadID {
		t.Fatalf("conversation_id = %q, want the thread-scoped id %q — the thread lane collapsed to the parent (H9)", conv, threadID)
	}
}

// TestHandleRequest_SubmitShapeWithoutStampKeepsBaseCollapse is the
// byte-identical half: a legacy publisher (no thread_session_id key) or
// a plain non-thread submit must behave EXACTLY as before — the base
// conversation identity is used and no thread lane is conjured.
func TestHandleRequest_SubmitShapeWithoutStampKeepsBaseCollapse(t *testing.T) {
	h, msgBus, sub := h9PairFixture(t, "b")
	const baseConv = "conv-h9-base"

	conv := driveRequestWithThreadStamp(t, h, msgBus, sub, map[string]any{
		"message":         "plain submit shape",
		"conversation_id": baseConv,
		"session_id":      "session-h9-primary",
		"turn_id":         "turn-h9-b",
	})
	if conv != baseConv {
		t.Fatalf("conversation_id = %q, want the base %q (no stamp: unchanged posture)", conv, baseConv)
	}

	// And an EMPTY thread_session_id value (a publisher that set the key
	// without a thread id) also keeps the base collapse — the handler
	// gate requires the separator, never trusts presence alone.
	h2, msgBus2, sub2 := h9PairFixture(t, "c")
	conv2 := driveRequestWithThreadStamp(t, h2, msgBus2, sub2, map[string]any{
		"message":           "bogus stamp shape",
		"conversation_id":   baseConv,
		"session_id":        "session-h9-primary",
		"turn_id":           "turn-h9-c",
		"thread_session_id": "not-a-thread",
	})
	if conv2 != baseConv {
		t.Fatalf("conversation_id = %q, want the base %q (stamp lacks the separator: not a thread id)", conv2, baseConv)
	}
}
