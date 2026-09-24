//go:build e2e

// Package stateidentity covers the session_id vs conversation_id duality:
// both identifiers are distinct, persisted, and both resolve the same
// session through the store's lookups.
package stateidentity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// rawRPCCall performs the length-prefixed framing exchange over the sandbox
// Unix socket and returns the full response envelope.
func rawRPCCall(t *testing.T, socketPath, method string, params any) map[string]any {
	t.Helper()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	defer conn.Close()

	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := fmt.Fprintf(conn, "%d\n", len(payload)); err != nil {
		t.Fatalf("write length: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	reader := bufio.NewReader(conn)
	lengthLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read length line for %s: %v", method, err)
	}
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(lengthLine), "%d", &length); err != nil || length <= 0 {
		t.Fatalf("%s: bad length line %q", method, lengthLine)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		t.Fatalf("%s: read payload: %v", method, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("%s: decode response %q: %v", method, buf, err)
	}
	return resp
}

func rpcResult(t *testing.T, socketPath, method string, params any) map[string]any {
	t.Helper()
	resp := rawRPCCall(t, socketPath, method, params)
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("%s: RPC error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: result is not an object: %v", method, resp["result"])
	}
	return result
}

func rawJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// state-identity-01: conversation_id vs session_id duality holds in the
// persisted store and in lookups:
//
//   - a created session carries BOTH a session- prefixed id and a conv-
//     prefixed conversation_id;
//   - the two are distinct;
//   - both remain stable across a store round-trip (session.get / list);
//   - chat submit with conversation_id=<session_id> (the Flutter client's
//     documented behavior) still resolves the session in the ack — the
//     daemon-side dual lookup accepts either identifier.
func TestSessionAndConversationIDDuality(t *testing.T) {
	s := harness.Start(t)

	create := rpcResult(t, s.SocketPath, "session.create", map[string]any{"name": "identity-01"})
	sessionID, _ := create["id"].(string)
	conversationID, _ := create["conversation_id"].(string)

	if sessionID == "" || conversationID == "" {
		t.Fatalf("create returned id=%q conversation_id=%q; both must be set", sessionID, conversationID)
	}
	if sessionID == conversationID {
		t.Fatalf("session_id and conversation_id must be distinct identifiers, got %q for both", sessionID)
	}
	if !strings.HasPrefix(sessionID, "session-") {
		t.Fatalf("session id %q lacks the session- prefix", sessionID)
	}
	if !strings.HasPrefix(conversationID, "conv-") {
		t.Fatalf("conversation id %q lacks the conv- prefix", conversationID)
	}

	// Store round-trip via session.get: both fields persist unchanged.
	got := rpcResult(t, s.SocketPath, "session.get", map[string]any{"id": sessionID})
	if got["id"] != sessionID {
		t.Fatalf("session.get id = %v, want %s", got["id"], sessionID)
	}
	if got["conversation_id"] != conversationID {
		t.Fatalf("session.get conversation_id = %v, want %s", got["conversation_id"], conversationID)
	}

	// The list view (a different store read path) carries the same duality.
	list := rpcResult(t, s.SocketPath, "session.list", map[string]any{"limit": 100})
	raw := rawJSON(t, list)
	if !strings.Contains(raw, sessionID) || !strings.Contains(raw, conversationID) {
		t.Fatalf("list view lost the duality pair (%s / %s): %s", sessionID, conversationID, raw)
	}

	// Chat-path duality: submitting with conversation_id=<session_id> (the
	// Flutter client's documented behavior) acks and echoes BOTH the
	// session id and the conversation id it was given.
	s.RegisterProject(t, "e2e-project")
	ack := s.SubmitChatHTTP(t, sessionID, "duality check")
	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("submit not accepted: %+v", ack)
	}
	if sid, _ := ack["session_id"].(string); sid != sessionID {
		t.Fatalf("ack session_id = %v, want %s", ack["session_id"], sessionID)
	}
	if cid, _ := ack["conversation_id"].(string); cid == "" {
		t.Fatal("ack carries an empty conversation_id; duality broken on the chat path")
	}
}
