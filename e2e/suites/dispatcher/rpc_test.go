//go:build e2e

// Persistent framed-protocol JSON-RPC client for the dispatcher suite.
//
// IMPORTANT (bus subscription lifetime): bus subscriptions are tied to the
// RPC connection that created them — when the connection closes, the
// subscription is cleaned up and bus.poll reports "subscription not found".
// All bus.subscribe/bus.poll/bus.unsubscribe calls in one test must
// therefore share a single persistent connection.
package dispatcher

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// rpcSession is one persistent framed-protocol JSON-RPC connection.
type rpcSession struct {
	conn   net.Conn
	reader *bufio.Reader
}

// openRPCSession dials the daemon socket and returns a session that MUST be
// closed via Close (register t.Cleanup).
func openRPCSession(t *testing.T, s *harness.Stack) *rpcSession {
	t.Helper()
	conn, err := net.DialTimeout("unix", s.SocketPath, 5*time.Second)
	if err != nil {
		t.Fatalf("dial unix %s: %v", s.SocketPath, err)
	}
	return &rpcSession{conn: conn, reader: bufio.NewReader(conn)}
}

// Close terminates the session.
func (c *rpcSession) Close() { c.conn.Close() }

// call performs one JSON-RPC request on the persistent connection.
// Wire format (internal/rpc/protocol.go): "<length>\n<payload>" both ways.
func (c *rpcSession) call(method string, params any) (json.RawMessage, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      time.Now().UnixNano(),
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := c.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c.conn, "%d\n", len(payload)); err != nil {
		return nil, fmt.Errorf("write frame length: %w", err)
	}
	if _, err := c.conn.Write(payload); err != nil {
		return nil, fmt.Errorf("write frame payload: %w", err)
	}
	lengthLine, err := c.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}
	length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
	if err != nil {
		return nil, fmt.Errorf("invalid frame length %q: %w", lengthLine, err)
	}
	respData := make([]byte, length)
	if _, err := io.ReadFull(c.reader, respData); err != nil {
		return nil, fmt.Errorf("read frame payload: %w", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", method, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: [%d] %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// submitChatWithConversation posts a chat submit carrying an explicit
// conversation_id (the clarify-resume path keys the follow-up on it).
func submitChatWithConversation(t *testing.T, s *harness.Stack, sessionID, conversationID, message string) map[string]any {
	t.Helper()
	sess := openRPCSession(t, s)
	defer sess.Close()
	raw, err := sess.call("chat.submit", map[string]any{
		"message":         message,
		"session_id":      sessionID,
		"conversation_id": conversationID,
		"source_client":   "e2e-harness",
	})
	if err != nil {
		t.Fatalf("chat.submit: %v", err)
	}
	var ack map[string]any
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("chat.submit ack decode: %v", err)
	}
	return ack
}
