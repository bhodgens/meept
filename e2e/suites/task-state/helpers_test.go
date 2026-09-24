//go:build e2e

// Shared transport helper for the task-state suite: persistent framed
// JSON-RPC session (subscriptions are tied to their connection).
package taskstate

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

// openRPCSession dials the daemon socket; caller must Close it.
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

// call performs one JSON-RPC request. Wire format: "<length>\n<payload>".
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
		return nil, fmt.Errorf("invalid frame length: %w", err)
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

// jsonUnmarshal is a thin alias so test files avoid importing encoding/json
// twice under different names.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
