//go:build e2e

// Shared transport helpers for WAVE-B suites: a minimal framed-protocol
// JSON-RPC client over the scratch daemon's Unix socket. Each suite package
// duplicates this file because Go test packages cannot share unexported
// helpers across directories.
//
// Wire format (internal/rpc/protocol.go): "<length>\n<payload>" both ways.
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

// rpcCall dials a fresh connection per call and performs one JSON-RPC
// request over the scratch daemon's Unix socket, using the daemon's framed
// wire protocol.
func rpcCall(t *testing.T, s *harness.Stack, method string, params any) (json.RawMessage, error) {
	t.Helper()
	conn, err := net.DialTimeout("unix", s.SocketPath, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

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

	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(conn, "%d\n", len(payload)); err != nil {
		return nil, fmt.Errorf("write frame length: %w", err)
	}
	if _, err := conn.Write(payload); err != nil {
		return nil, fmt.Errorf("write frame payload: %w", err)
	}

	reader := bufio.NewReader(conn)
	lengthLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}
	length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
	if err != nil {
		return nil, fmt.Errorf("invalid frame length: %w", err)
	}
	respData := make([]byte, length)
	if _, err := io.ReadFull(reader, respData); err != nil {
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

// readAllLinesTaskState is a tiny test-only helper (kept for potential diagnostics).
func readAllLinesTaskState(s string) []string { return strings.Split(s, "\n") }
