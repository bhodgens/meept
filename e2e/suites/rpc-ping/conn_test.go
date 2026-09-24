//go:build e2e

package rpcping

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// rpcConn is a PERSISTENT JSON-RPC connection over the sandbox Unix socket.
// The daemon cancels bus subscriptions when the client disconnects (Bug C8),
// so the RPC->bus bridge scenario must hold one connection open across
// subscribe → publish → poll.
type rpcConn struct {
	conn net.Conn
	r    *bufio.Reader
	t    *testing.T
}

func dialRPC(t *testing.T, socketPath string) *rpcConn {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	t.Cleanup(func() { conn.Close() })
	return &rpcConn{conn: conn, r: bufio.NewReader(conn), t: t}
}

// call sends one request and reads one response on this connection.
func (c *rpcConn) call(method string, params any) map[string]any {
	c.t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		c.t.Fatalf("marshal request: %v", err)
	}
	if _, err := fmt.Fprintf(c.conn, "%d\n", len(payload)); err != nil {
		c.t.Fatalf("write length: %v", err)
	}
	if _, err := c.conn.Write(payload); err != nil {
		c.t.Fatalf("write payload: %v", err)
	}
	lengthLine, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read length line for %s: %v", method, err)
	}
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(lengthLine), "%d", &length); err != nil || length <= 0 {
		c.t.Fatalf("%s: bad length line %q", method, lengthLine)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		c.t.Fatalf("%s: read payload: %v", method, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf, &resp); err != nil {
		c.t.Fatalf("%s: decode response %q: %v", method, buf, err)
	}
	return resp
}

func (c *rpcConn) callResult(method string, params any) map[string]any {
	c.t.Helper()
	resp := c.call(method, params)
	if e, ok := resp["error"]; ok && e != nil {
		c.t.Fatalf("%s: RPC error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("%s: result is not an object: %v", method, resp["result"])
	}
	return result
}

var (
	_ io.Reader = (*bufio.Reader)(nil)
	_           = time.Second
)
