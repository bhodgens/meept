package harness

// Client helpers for stream surfaces the daemon exposes over its HTTP
// listener and Unix RPC socket:
//
//   - WSClient: dial the daemon's /ws endpoint (Sec-WebSocket-Protocol
//     "bearer.<key>" auth), send subscribe/unsubscribe, receive typed
//     events with a deadline.
//   - SSEResponse / ReadSSEEvent: parse the text/event-stream body of
//     POST /api/v1/chat/stream ("data:"/"event:" lines, terminated by
//     "event: done" or "event: error").
//   - RPCClient: the canonical length-prefixed JSON-RPC unix-socket
//     client (three suites previously grew local copies of this wire
//     helper; new suites use this one).

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// WSClient is one daemon WebSocket connection.
type WSClient struct {
	conn *websocket.Conn
	t    testing.TB
	// skipped holds frames seen while waiting for other types, so a
	// WaitEvent(scan A, then scan B) sequence cannot lose a B frame that
	// arrived during the A scan.
	skipped []WSEvent
}

// DialWS dials the daemon's WS endpoint over TLS (self-signed scratch
// cert). apiKey may be empty when the sandbox runs with auth disabled
// (require_auth=false); a non-empty key is carried via the
// Sec-WebSocket-Protocol "bearer.<key>" subprotocol. The first message
// (the daemon's {"type":"status"} welcome) is consumed and returned.
func DialWS(t testing.TB, stack *Stack, apiKey string) *WSClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wsURL := "wss://" + stack.Daemon.httpAddr + "/ws"
	opts := &websocket.DialOptions{
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			},
		},
	}
	if apiKey != "" {
		opts.Subprotocols = []string{"bearer." + apiKey}
	}
	conn, resp, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("harness: dial ws %s: %v", wsURL, err)
	}
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	c := &WSClient{conn: conn, t: t}
	welcome := c.RecvEvent(10 * time.Second) // welcome {"type":"status","connected":true}
	if welcome == nil {
		t.Fatalf("harness: ws connect: no welcome frame within 10s")
	}
	t.Cleanup(func() { _ = c.conn.Close(websocket.StatusNormalClosure, "e2e done") })
	return c
}

// Subscribe sends {"type":"subscribe","data":{"channel":channel,
// "session_id":sessionID}} (either may be empty) and waits for the
// {"type":"subscribed"} ack.
func (c *WSClient) Subscribe(channel, sessionID string) {
	c.t.Helper()
	data := map[string]string{}
	if channel != "" {
		data["channel"] = channel
	}
	if sessionID != "" {
		data["session_id"] = sessionID
	}
	raw, _ := json.Marshal(data)
	payload, _ := json.Marshal(map[string]any{"type": "subscribe", "data": json.RawMessage(raw)})
	c.write(payload)
	for {
		msg := c.RecvEvent(10 * time.Second)
		if msg == nil {
			c.t.Fatalf("harness: ws subscribe: no ack within 10s")
		}
		if msg.Type == "subscribed" {
			return
		}
		if msg.Type == "error" {
			c.t.Fatalf("harness: ws subscribe: server error: %s", string(msg.Data))
		}
	}
}

// Unsubscribe sends {"type":"unsubscribe",...}. There is no ack.
func (c *WSClient) Unsubscribe(channel, sessionID string) {
	c.t.Helper()
	data := map[string]string{}
	if channel != "" {
		data["channel"] = channel
	}
	if sessionID != "" {
		data["session_id"] = sessionID
	}
	raw, _ := json.Marshal(data)
	payload, _ := json.Marshal(map[string]any{"type": "unsubscribe", "data": json.RawMessage(raw)})
	c.write(payload)
}

func (c *WSClient) write(payload []byte) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, payload); err != nil {
		c.t.Fatalf("harness: ws write: %v", err)
	}
}

// WSEvent is one received WebSocket frame.
type WSEvent struct {
	Type string
	Data json.RawMessage
}

// RecvEvent reads one frame or returns nil after timeout (and fails the
// test on any read error other than the normal-close path).
func (c *WSClient) RecvEvent(timeout time.Duration) *WSEvent {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	kind, data, err := c.conn.Read(ctx)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil
		}
		c.t.Fatalf("harness: ws read: %v", err)
	}
	if kind != websocket.MessageText {
		return c.RecvEvent(timeout) // ignore binary frames
	}
	var msg WSEvent
	if err := json.Unmarshal(data, &msg); err != nil {
		c.t.Fatalf("harness: ws frame decode: %v (%s)", err, data)
	}
	return &msg
}

// WaitEvent scans frames (dropping non-matching ones) until one of the
// given types arrives or the timeout expires (nil).
func (c *WSClient) WaitEvent(timeout time.Duration, types ...string) *WSEvent {
	c.t.Helper()
	want := map[string]bool{}
	for _, ty := range types {
		want[ty] = true
	}
	// Replay frames a previous WaitEvent skipped (in arrival order) before
	// reading anything new from the wire.
	for i, ev := range c.skipped {
		if want[ev.Type] {
			c.skipped = append(c.skipped[:i], c.skipped[i+1:]...)
			return &ev
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		budget := time.Until(deadline)
		if budget <= 0 {
			return nil
		}
		msg := c.RecvEvent(budget)
		if msg == nil {
			return nil
		}
		if want[msg.Type] {
			return msg
		}
		c.skipped = append(c.skipped, *msg)
	}
}

// --- SSE ----------------------------------------------------------------

// SSEResponse is an in-progress SSE body from POST /api/v1/chat/stream.
type SSEResponse struct {
	resp   *http.Response
	reader *bufio.Reader
	t      testing.TB
}

// PostChatStreamSSE posts to /api/v1/chat/stream with an unbounded body
// read window (the caller drives reading via NextEvent; the HTTP client
// itself does not time out mid-stream).
func (s *Stack) PostChatStreamSSE(t testing.TB, message string) *SSEResponse {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"message": message})
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed scratch cert
			ForceAttemptHTTP2:     false,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
	resp, err := client.Post(s.HTTPBaseURL()+"/api/v1/chat/stream", "application/json", strings.NewReader(string(payload))) //nolint:noctx // bounded by ResponseHeaderTimeout + caller cancellation
	if err != nil {
		t.Fatalf("harness: chat stream post: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("harness: chat stream status %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("harness: chat stream content-type %q, want text/event-stream", ct)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return &SSEResponse{resp: resp, reader: bufio.NewReader(resp.Body), t: t}
}

// SSEEvent is one parsed SSE frame.
type SSEEvent struct {
	Event string // "data" | "done" | "error" ("" means a bare data frame)
	Data  string
}

// NextEvent reads one SSE frame (data: lines + optional event: line).
// Returns (event, nil) on a frame, (nil, io.EOF) when the server closes,
// and fails the test on transport errors.
func (r *SSEResponse) NextEvent() (*SSEEvent, error) {
	r.t.Helper()
	ev := &SSEEvent{}
	sawFrame := false
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && sawFrame {
				return ev, nil
			}
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil, io.EOF
			}
			r.t.Fatalf("harness: sse read: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if sawFrame {
				return ev, nil
			}
			// leading blank line: ignore
		case strings.HasPrefix(line, "event:"):
			ev.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			sawFrame = true
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ")
			if ev.Data != "" {
				ev.Data += "\n"
			}
			ev.Data += data
			sawFrame = true
		}
		if ev.Event == "done" || ev.Event == "error" {
			// Terminal frames may omit the terminating blank line.
			return ev, nil
		}
	}
}

// --- JSON-RPC over the unix socket --------------------------------------

// RPCClient is a persistent length-prefixed JSON-RPC connection over the
// daemon's Unix socket. Wire framing (per internal/rpc): a decimal length
// line, then exactly that many bytes of JSON. The daemon cancels bus
// subscriptions when the client disconnects, so scenario suites needing
// subscribe→publish→poll hold ONE client open for the whole test.
type RPCClient struct {
	conn net.Conn
	r    *bufio.Reader
	t    testing.TB
	next int64
}

// DialRPC dials the sandbox daemon's Unix RPC socket.
func DialRPC(t testing.TB, socketPath string) *RPCClient {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("harness: dial rpc socket %s: %v", socketPath, err)
	}
	t.Cleanup(func() { conn.Close() })
	return &RPCClient{conn: conn, r: bufio.NewReader(conn), t: t}
}

// Call sends one request and reads one response on this connection.
// Params may be nil (omitted).
func (c *RPCClient) Call(method string, params any) map[string]any {
	c.t.Helper()
	c.next++
	req := map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		c.t.Fatalf("harness: marshal rpc request: %v", err)
	}
	if _, err := fmt.Fprintf(c.conn, "%d\n", len(payload)); err != nil {
		c.t.Fatalf("harness: rpc write length: %v", err)
	}
	if _, err := c.conn.Write(payload); err != nil {
		c.t.Fatalf("harness: rpc write payload: %v", err)
	}
	return c.readResponse(method)
}

func (c *RPCClient) readResponse(method string) map[string]any {
	c.t.Helper()
	lengthLine, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("harness: rpc read length line for %s: %v", method, err)
	}
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(lengthLine), "%d", &length); err != nil || length <= 0 {
		c.t.Fatalf("harness: rpc %s: bad length line %q", method, lengthLine)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		c.t.Fatalf("harness: rpc %s: read payload: %v", method, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf, &resp); err != nil {
		c.t.Fatalf("harness: rpc %s: decode response %q: %v", method, buf, err)
	}
	return resp
}

// CallResult calls and unwraps "result" as an object, failing the test on
// an RPC error envelope or a non-object result.
func (c *RPCClient) CallResult(method string, params any) map[string]any {
	c.t.Helper()
	resp := c.Call(method, params)
	if e, ok := resp["error"]; ok && e != nil {
		c.t.Fatalf("harness: rpc %s error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		c.t.Fatalf("harness: rpc %s result is not an object: %v", method, resp["result"])
	}
	return result
}

// CallError calls and returns the raw response without failing on an
// error envelope (for negative-path assertions).
func (c *RPCClient) CallError(method string, params any) map[string]any {
	return c.Call(method, params)
}
