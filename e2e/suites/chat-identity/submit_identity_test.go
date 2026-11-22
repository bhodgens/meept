//go:build e2e

// Scenario (c) of leaf 01 (20261121-interactive-usability): a turn
// submitted GUI-shaped (conversation_id = raw PRIMARY session id) and a
// later turn submitted TUI-shaped (session_id only) must run in the SAME
// canonical conversation - both acks carry the session's own conversation
// id, and the second turn's context carries the first turn's content.
package chatidentity

import (
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

func TestGUIShapedSubmitLinksContextForTUICrossSubmit(t *testing.T) {
	s := startStack(t)
	sessionID := s.CreateSession(t, "identity-cross", s.ProjectDir)

	// Turn 1: GUI-shaped (conversation_id = raw session id, no session_id
	// - the exact shape the Flutter sdk_client sends).
	ack1 := postChatSubmit(t, s, map[string]any{
		"message":         "remember the codeword amber-lantern",
		"conversation_id": sessionID,
		"source_client":   "chat-identity-e2e",
	})
	if accepted, _ := ack1["accepted"].(bool); !accepted {
		t.Fatalf("turn 1 not accepted: %+v", ack1)
	}
	conv1, _ := ack1["conversation_id"].(string)

	// Turn 2: TUI-shaped (session_id only) - the CLI shape.
	ack2 := postChatSubmit(t, s, map[string]any{
		"message":       "what codeword did I give you?",
		"session_id":    sessionID,
		"source_client": "chat-identity-e2e",
	})
	if accepted, _ := ack2["accepted"].(bool); !accepted {
		t.Fatalf("turn 2 not accepted: %+v", ack2)
	}
	conv2, _ := ack2["conversation_id"].(string)

	// Both acks under the SAME canonical conversation id.
	if conv1 == "" || conv1 != conv2 {
		t.Fatalf("identity split: GUI-shaped ack conv %q != TUI-shaped ack conv %q", conv1, conv2)
	}
	if looksLikeSessionID(conv1) {
		t.Fatalf("ack conversation_id %q is still the raw session id; submit must canonicalize to the owning conversation", conv1)
	}

	// The canonical id is the session's OWN conversation id (not a fresh
	// mint): derivable from the session record via session.get.
	rpc := harness.DialRPC(t, s.SocketPath)
	show := rpc.CallResult("session.get", map[string]any{"id": sessionID})
	if conv, _ := show["conversation_id"].(string); conv != conv2 {
		t.Fatalf("canonical conv %q is not the session's own conversation id %q", conv2, conv)
	}

	// Context continuity: the second turn's executor request must carry
	// the codeword (conversation restore from the shared conversation).
	waitRequestMentions(t, s, "amber-lantern", 90*time.Second)
}
