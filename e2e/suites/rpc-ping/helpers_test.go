//go:build e2e

// Shared JSON-RPC helpers for the rpc-ping suite: raw RPC over the sandbox
// Unix socket using the daemon's length-prefixed framing
// (<length>\n<payload>), plus a compact JSON renderer for substring
// assertions. Deliberately local to this suite (the harness package is not
// to be extended by wave branches).
package rpcping

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

// rpcCall sends one JSON-RPC request over the given Unix socket and decodes
// the full response envelope ({jsonrpc, id, result?, error?}). It fails the
// test on transport errors only — application-level errors are returned for
// the caller to assert on.
func rpcCall(t *testing.T, socketPath, method string, params any) map[string]any {
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

// rpcResult asserts the call succeeded (no error object) and returns the
// result object.
func rpcResult(t *testing.T, socketPath, method string, params any) map[string]any {
	t.Helper()
	resp := rpcCall(t, socketPath, method, params)
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("%s: RPC error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: result is not an object: %v", method, resp["result"])
	}
	return result
}

// rawJSON renders v compactly for substring assertions.
func rawJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
