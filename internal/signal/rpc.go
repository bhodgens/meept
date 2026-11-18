package signal

// JSON-RPC 2.0 client over the signal-cli unix socket (contract C3,
// owner leaf 10).
//
// Wire discipline:
//   - ONE request-id mutex serializes concurrent requests: exactly one
//     json-rpc frame is outstanding on the conn at a time, so a torn or
//     interleaved frame can never happen and every response id correlates
//     with the request that produced it.
//   - A demux goroutine reads the conn continuously and routes frames:
//     a frame WITH an id resolves the pending call (the id match is the
//     correlation); a frame WITHOUT an id is a notification and fans out
//     to the registered handlers (callEvent -> CallEvents streams and
//     RegisterCallEvents callbacks; envelope -> Messages streams and
//     RegisterMessages callbacks).
//   - Envelope dedupe keys on (sourceUuid, timestamp) — signal-cli can
//     replay an envelope on reconnect; the replay must not surface twice.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Named sentinel errors (repo rule).
var (
	// ErrConnClosed is returned by requests issued after Close.
	ErrConnClosed = errors.New("signal rpc: connection closed")
	// ErrRPCError wraps a json-rpc error object from signal-cli.
	ErrRPCError = errors.New("signal rpc: json-rpc error")
	// ErrNoPendingCall is returned when a response arrives with an id
	// matching no in-flight request (a protocol violation).
	ErrNoPendingCall = errors.New("signal rpc: response id matches no pending request")
)

const (
	rpcMethodStartCall               = "startCall"
	rpcMethodAcceptCall              = "acceptCall"
	rpcMethodHangupCall              = "hangupCall"
	rpcMethodListCalls               = "listCalls"
	rpcMethodSubscribeCallEvents     = "subscribeCallEvents"
	rpcMethodSend                    = "send"
	rpcMethodTyping                  = "sendTyping"
	notifyMethodCallEvent            = "callEvent"
	notifyMethodEnvelope             = "envelope"
	notificationReadTimeout          = 500 * time.Millisecond
	dedupeTTL                        = 5 * time.Minute
	maxPendingFrameBytes         int = 4 << 20
)

// request is one json-rpc 2.0 request frame.
type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// response is one json-rpc 2.0 response frame.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the json-rpc 2.0 error object.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%s (code %d): %s", ErrRPCError, e.Code, e.Message)
}

func (e *rpcError) Unwrap() error { return ErrRPCError }

// notification is one json-rpc 2.0 notification frame (no id).
type notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Client is a json-rpc 2.0 client over one signal-cli unix socket.
type Client struct {
	conn   net.Conn
	reader *bufio.Reader

	// requestMu is THE request-id mutex: concurrent requests serialize
	// on the conn (write frame, read the matching response) so frames
	// never interleave and ids always correlate. The demux goroutine
	// never takes it — it owns the reader side exclusively.
	requestMu sync.Mutex

	// pendingReq correlates responses to in-flight requests by id; the
	// demux goroutine (sole conn reader) resolves them.
	pendingReq pendingRegistry

	closeMu   sync.Mutex
	closed    bool
	closeOnce sync.Once
	done      chan struct{} // closed when the demux goroutine exits

	// callFns / msgFns are the Register* callback fan-outs.
	callFnsMu sync.Mutex
	callFns   []func(CallEvent)
	msgFns    []func(DataMessage)

	// streamsMu guards the per-callID and message channel registries
	// backing CallEvents/Messages.
	streamsMu   sync.Mutex
	callStreams map[string][]chan CallEvent
	msgStreams  []chan DataMessage

	// dedupeMu guards the (sourceUuid, timestamp) seen-set.
	dedupeMu sync.Mutex
	seen     map[dedupeKey]time.Time
}

type dedupeKey struct {
	sourceUUID string
	timestamp  int64
}

// Dial connects to the signal-cli json-rpc socket and starts the
// notification demux goroutine. Before returning it performs a bounded
// handshake probe (`version`, tolerated to answer with either a result
// or a json-rpc unknown-method error) so the peer's accept/read loop is
// confirmed LIVE when Dial returns — mirroring the supervised-child
// HandshakeProbe discipline (internal/voice/childproc.go). Without it a
// caller that pushes a notification immediately after Dial can race the
// server's per-conn goroutine and lose the frame.
func Dial(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("signal rpc: dial %s: %w", socketPath, err)
	}
	c := &Client{
		conn:        conn,
		reader:      bufio.NewReaderSize(conn, 64*1024),
		done:        make(chan struct{}),
		callStreams: make(map[string][]chan CallEvent),
		seen:        make(map[dedupeKey]time.Time),
	}
	go c.demux()
	if err := c.handshakeProbe(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

// handshakeProbeTimeout bounds the Dial handshake probe.
const handshakeProbeTimeout = 5 * time.Second

// handshakeProbe round-trips one `version` request. ANY correlated
// response proves the peer's read loop is live: a result (signal-cli
// >= 0.13 exposes `version`) or a json-rpc error object (an older/other
// server that rejects the method) both pass; only a transport failure
// or the timeout fails the dial.
func (c *Client) handshakeProbe() error {
	done := make(chan error, 1)
	go func() {
		_, err := c.call("version", map[string]any{})
		// A json-rpc ERROR response is still a live peer: swallow it.
		var rpcErr *rpcError
		if err == nil || errors.As(err, &rpcErr) {
			done <- nil
			return
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("signal rpc: handshake probe: %w", err)
		}
		return nil
	case <-time.After(handshakeProbeTimeout):
		return fmt.Errorf("signal rpc: handshake probe timed out after %s", handshakeProbeTimeout)
	}
}

// Close shuts down the conn and stops the demux goroutine. Idempotent.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		c.closed = true
		c.closeMu.Unlock()
	})
	if c.conn != nil {
		// Unblock the demux reader; its exit closes the streams.
		if err := c.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("signal rpc: close: %w", err)
		}
	}
	if c.done != nil {
		select {
		case <-c.done:
		case <-time.After(time.Second):
		}
	}
	return nil
}

func (c *Client) isClosed() bool {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	return c.closed
}

// call issues one request and waits for the correlated response. The
// request-id mutex serializes the WRITE side so json-rpc frames never
// interleave; the demux goroutine is the conn's SOLE reader and routes
// every response to its waiting request by id.
func (c *Client) call(method string, params any) (json.RawMessage, error) {
	if c.isClosed() {
		return nil, ErrConnClosed
	}
	c.requestMu.Lock()
	defer c.requestMu.Unlock()

	id := nextRequestID()
	wait := make(chan *response, 1)
	c.registerPending(id, wait)
	defer c.unregisterPending(id)

	if err := c.writeRequest(method, params, id); err != nil {
		return nil, err
	}
	resp := <-wait
	if resp == nil {
		return nil, ErrConnClosed // demux exited (conn closed)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// nextRequestID hands out ids for frames written under requestMu.
func nextRequestID() int {
	idMu.Lock()
	defer idMu.Unlock()
	idMu.last++
	return idMu.last
}

var idMu struct {
	sync.Mutex
	last int
}

func (c *Client) writeRequest(method string, params any, id int) error {
	frame, err := json.Marshal(request{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("signal rpc: marshal request: %w", err)
	}
	if err := writeFrame(c.conn, frame); err != nil {
		return fmt.Errorf("signal rpc: write request: %w", err)
	}
	return nil
}

// pendingRegistry is the id->waiter map the demux goroutine resolves.
// Writes happen under requestMu (inside call); the demux reads under
// pendingMu and never blocks on I/O while holding it.
type pendingRegistry struct {
	pendingMu sync.Mutex
	pending   map[int]chan *response
}

func (r *pendingRegistry) register(id int, wait chan *response) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if r.pending == nil {
		r.pending = make(map[int]chan *response)
	}
	r.pending[id] = wait
}

func (r *pendingRegistry) unregister(id int) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	delete(r.pending, id)
}

func (r *pendingRegistry) resolve(id int, resp *response) bool {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	wait, ok := r.pending[id]
	if !ok {
		return false
	}
	delete(r.pending, id)
	wait <- resp // buffered(1): never blocks the demux
	return true
}

// failAll releases every waiter when the conn dies (nil = conn closed).
func (r *pendingRegistry) failAll() {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	for id, wait := range r.pending {
		delete(r.pending, id)
		wait <- nil
	}
}

// registerPending / unregisterPending / resolvePending / failAllPending
// adapt the shared registry onto Client.
func (c *Client) registerPending(id int, wait chan *response) {
	c.pendingReq.register(id, wait)
}

func (c *Client) unregisterPending(id int) {
	c.pendingReq.unregister(id)
}

func (c *Client) resolvePending(id int, resp *response) bool {
	return c.pendingReq.resolve(id, resp)
}

func (c *Client) failAllPending() {
	c.pendingReq.failAll()
}

// demux is the conn's SOLE reader: it decodes every frame and routes
// responses to their waiting request by id and notifications to the
// fan-outs. On conn death it fails all pending waiters and closes the
// stream registry.
func (c *Client) demux() {
	defer close(c.done)
	defer c.failAllPending()
	for {
		line, err := readFrame(c.reader)
		if err != nil {
			// Conn gone (Close or peer death).
			return
		}
		// Try response first: a response frame carries an id (and a
		// result or an error); a notification carries a method, no id.
		var resp response
		if jerr := json.Unmarshal(line, &resp); jerr == nil && resp.ID != 0 &&
			(len(resp.Result) > 0 || resp.Error != nil) {
			c.resolvePending(resp.ID, &resp)
			continue
		}
		var n notification
		if jerr := json.Unmarshal(line, &n); jerr != nil || n.Method == "" {
			continue // not a frame shape we know: drop
		}
		c.routeNotification(n.Method, n.Params)
	}
}

// routeNotification demuxes one notification to every registered
// stream and callback.
func (c *Client) routeNotification(method string, params json.RawMessage) {
	switch method {
	case notifyMethodCallEvent:
		ev, ok := parseCallEvent(params)
		if !ok {
			return
		}
		c.dispatchCallEvent(ev)
	case notifyMethodEnvelope:
		msg, ok := parseEnvelope(params)
		if !ok {
			return
		}
		if !c.markSeen(msg.SourceUUID, msg.Timestamp) {
			return // replayed envelope: deduped
		}
		c.dispatchMessage(msg)
	}
}

// parseCallEvent extracts a CallEvent from a callEvent notification.
// The observed signal-cli shape is params.envelope.callEvent:
//
//	{"account":"+1..","envelope":{"sourceUuid":"..",
//	 "callEvent":{"callId":"..","state":"CONNECTED","reason":".."}}}
//
// A flat shape (params carrying callId/state directly) is accepted too,
// guarding against signal-cli version drift.
func parseCallEvent(params json.RawMessage) (CallEvent, bool) {
	var wire struct {
		Envelope struct {
			SourceUUID string `json:"sourceUuid"`
			CallEvent  struct {
				CallID string `json:"callId"`
				State  string `json:"state"`
				Reason string `json:"reason"`
			} `json:"callEvent"`
		} `json:"envelope"`
		CallID string `json:"callId"`
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(params, &wire); err != nil {
		return CallEvent{}, false
	}
	if wire.Envelope.CallEvent.CallID != "" || wire.Envelope.CallEvent.State != "" {
		return CallEvent{
			EnvelopeSourceUUID: wire.Envelope.SourceUUID,
			CallID:             wire.Envelope.CallEvent.CallID,
			State:              CallState(wire.Envelope.CallEvent.State),
			Reason:             wire.Envelope.CallEvent.Reason,
		}, true
	}
	if wire.CallID == "" && wire.State == "" {
		return CallEvent{}, false
	}
	return CallEvent{
		CallID: wire.CallID,
		State:  CallState(wire.State),
		Reason: wire.Reason,
	}, true
}

// parseEnvelope extracts a DataMessage from an envelope notification.
// The observed signal-cli shape is params.envelope.dataMessage:
//
//	{"account":"+1..","envelope":{"sourceUuid":"..","sourceName":"..",
//	 "timestamp":1731..,"dataMessage":{"timestamp":1731..,"message":"..",
//	 "attachments":[...],"groupInfo":{...}|null}}}
func parseEnvelope(params json.RawMessage) (DataMessage, bool) {
	var wire struct {
		Envelope struct {
			SourceUUID  string `json:"sourceUuid"`
			SourceName  string `json:"sourceName"`
			DataMessage struct {
				Timestamp   int64  `json:"timestamp"`
				Message     string `json:"message"`
				Attachments []struct {
					Path string `json:"path"`
				} `json:"attachments"`
				GroupInfo *struct {
					GroupID string `json:"groupId"`
				} `json:"groupInfo"`
			} `json:"dataMessage"`
		} `json:"envelope"`
	}
	if err := json.Unmarshal(params, &wire); err != nil {
		return DataMessage{}, false
	}
	env := wire.Envelope
	dm := env.DataMessage
	if env.SourceUUID == "" && dm.Message == "" {
		return DataMessage{}, false
	}
	msg := DataMessage{
		SourceUUID: env.SourceUUID,
		SourceName: env.SourceName,
		Body:       dm.Message,
		Timestamp:  dm.Timestamp,
	}
	for _, a := range dm.Attachments {
		msg.Attachments = append(msg.Attachments, a.Path)
	}
	if dm.GroupInfo != nil {
		msg.GroupID = dm.GroupInfo.GroupID
	}
	return msg, true
}

// markSeen records a (sourceUuid, timestamp) key; returns false when it
// was already seen (replay). Stale keys age out of the map so it does
// not grow unboundedly.
func (c *Client) markSeen(sourceUUID string, ts int64) bool {
	key := dedupeKey{sourceUUID: sourceUUID, timestamp: ts}
	c.dedupeMu.Lock()
	if _, dup := c.seen[key]; dup {
		c.dedupeMu.Unlock()
		return false
	}
	c.seen[key] = time.Now()
	// Age out entries older than dedupeTTL (under the same lock; map
	// access is memory-only, the TTL check needs no I/O).
	cutoff := time.Now().Add(-dedupeTTL)
	for k, at := range c.seen {
		if at.Before(cutoff) {
			delete(c.seen, k)
		}
	}
	c.dedupeMu.Unlock()
	return true
}

// dispatchCallEvent fans one event to callbacks and per-callID streams.
func (c *Client) dispatchCallEvent(ev CallEvent) {
	c.callFnsMu.Lock()
	fns := make([]func(CallEvent), len(c.callFns))
	copy(fns, c.callFns)
	c.callFnsMu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
	c.streamsMu.Lock()
	chans := make([]chan CallEvent, 0, len(c.callStreams[ev.CallID]))
	chans = append(chans, c.callStreams[ev.CallID]...)
	c.streamsMu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- ev:
		case <-time.After(notificationReadTimeout):
			// No reader drained the stream: drop rather than wedge
			// the demux path.
		}
	}
}

// dispatchMessage fans one message to callbacks and the message streams.
func (c *Client) dispatchMessage(msg DataMessage) {
	c.callFnsMu.Lock()
	fns := make([]func(DataMessage), len(c.msgFns))
	copy(fns, c.msgFns)
	c.callFnsMu.Unlock()
	for _, fn := range fns {
		fn(msg)
	}
	c.streamsMu.Lock()
	chans := make([]chan DataMessage, len(c.msgStreams))
	copy(chans, c.msgStreams)
	c.streamsMu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- msg:
		case <-time.After(notificationReadTimeout):
		}
	}
}

// RegisterCallEvents adds a callback invoked for every callEvent
// notification. Register from one goroutine at startup.
func (c *Client) RegisterCallEvents(fn func(CallEvent)) {
	c.callFnsMu.Lock()
	defer c.callFnsMu.Unlock()
	c.callFns = append(c.callFns, fn)
}

// RegisterMessages adds a callback invoked for every inbound
// DataMessage (post-dedupe).
func (c *Client) RegisterMessages(fn func(DataMessage)) {
	c.callFnsMu.Lock()
	defer c.callFnsMu.Unlock()
	c.msgFns = append(c.msgFns, fn)
}

// CallEvents returns a channel of call events for the given callID.
// Cancel ctx or Close the client to release it.
func (c *Client) CallEvents(ctx context.Context, callID string) <-chan CallEvent {
	ch := make(chan CallEvent, 8)
	c.streamsMu.Lock()
	c.callStreams[callID] = append(c.callStreams[callID], ch)
	c.streamsMu.Unlock()
	go func() {
		<-ctx.Done()
		c.streamsMu.Lock()
		list := c.callStreams[callID]
		out := list[:0]
		for _, e := range list {
			if e != ch {
				out = append(out, e)
			}
		}
		if len(out) == 0 {
			delete(c.callStreams, callID)
		} else {
			c.callStreams[callID] = out
		}
		c.streamsMu.Unlock()
	}()
	return ch
}

// Messages returns a channel of inbound DataMessages (post-dedupe).
// Cancel ctx or Close the client to release it.
func (c *Client) Messages(ctx context.Context) <-chan DataMessage {
	ch := make(chan DataMessage, 8)
	c.streamsMu.Lock()
	c.msgStreams = append(c.msgStreams, ch)
	c.streamsMu.Unlock()
	go func() {
		<-ctx.Done()
		c.streamsMu.Lock()
		out := c.msgStreams[:0]
		for _, e := range c.msgStreams {
			if e != ch {
				out = append(out, e)
			}
		}
		c.msgStreams = out
		c.streamsMu.Unlock()
	}()
	return ch
}

// StartCall places a call to recipient with the given audio mode and
// returns the signal-cli call id.
func (c *Client) StartCall(recipient string, audioMode CallAudioMode) (callID string, err error) {
	params := map[string]any{
		"recipient": recipient,
	}
	if audioMode.SocketAudio {
		params["audioMode"] = map[string]any{"socketAudio": true, "socketPath": audioMode.SocketPath}
	}
	result, err := c.call(rpcMethodStartCall, params)
	if err != nil {
		return "", err
	}
	var out struct {
		CallID string `json:"callId"`
	}
	if uerr := json.Unmarshal(result, &out); uerr != nil {
		return "", fmt.Errorf("signal rpc: decode startCall result: %w", uerr)
	}
	return out.CallID, nil
}

// AcceptCall accepts an incoming call.
func (c *Client) AcceptCall(callID string) error {
	_, err := c.call(rpcMethodAcceptCall, map[string]any{"callId": callID})
	return err
}

// HangupCall terminates a call.
func (c *Client) HangupCall(callID string) error {
	_, err := c.call(rpcMethodHangupCall, map[string]any{"callId": callID})
	return err
}

// ListCalls returns the live calls signal-cli knows about.
func (c *Client) ListCalls() ([]CallInfo, error) {
	result, err := c.call(rpcMethodListCalls, map[string]any{})
	if err != nil {
		return nil, err
	}
	var out []CallInfo
	// The stub shape is {"callIds": [".."]}; a full callInfo array is
	// accepted when signal-cli provides it.
	if uerr := json.Unmarshal(result, &out); uerr == nil && out != nil {
		return out, nil
	}
	var wrapper struct {
		CallIDs []string `json:"callIds"`
	}
	if uerr := json.Unmarshal(result, &wrapper); uerr != nil {
		return nil, fmt.Errorf("signal rpc: decode listCalls result: %w", uerr)
	}
	calls := make([]CallInfo, 0, len(wrapper.CallIDs))
	for _, id := range wrapper.CallIDs {
		calls = append(calls, CallInfo{CallID: id})
	}
	return calls, nil
}

// SubscribeCallEvents subscribes the signal-cli call-event stream under
// the given subscription id.
func (c *Client) SubscribeCallEvents(subscriptionID string) error {
	_, err := c.call(rpcMethodSubscribeCallEvents, map[string]any{"subscriptionId": subscriptionID})
	return err
}

// Send delivers a message (with optional attachment local paths) and
// returns the message timestamp — the delivery handle signal-cli hands
// back from `send`.
func (c *Client) Send(recipient string, message string, attachments []string) (ts int64, err error) {
	params := map[string]any{
		"recipient": recipient,
		"message":   message,
	}
	if len(attachments) > 0 {
		params["attachments"] = attachments
	}
	result, err := c.call(rpcMethodSend, params)
	if err != nil {
		return 0, err
	}
	var out struct {
		Timestamp int64 `json:"timestamp"`
	}
	if uerr := json.Unmarshal(result, &out); uerr != nil {
		return 0, fmt.Errorf("signal rpc: decode send result: %w", uerr)
	}
	return out.Timestamp, nil
}

// Typing starts (stop=false) or stops (stop=true) the typing indicator
// toward recipient via `sendTyping`.
func (c *Client) Typing(recipient string, stop bool) error {
	_, err := c.call(rpcMethodTyping, map[string]any{
		"recipient": recipient,
		"stop":      stop,
	})
	return err
}

// writeFrame writes one newline-delimited json frame. Pure conn I/O,
// no mutex (callers hold requestMu or own the conn's lifecycle).
func writeFrame(conn net.Conn, frame []byte) error {
	buf := make([]byte, 0, len(frame)+1)
	buf = append(buf, frame...)
	buf = append(buf, '\n')
	_, err := conn.Write(buf)
	return err
}

// readFrame reads one newline-delimited json frame from a persistent
// buffered reader (a fresh scanner per frame would lose bytes the
// scanner buffered past the first newline).
func readFrame(r *bufio.Reader) ([]byte, error) {
	line, err := readFrameLine(r)
	if err != nil {
		return nil, err
	}
	if len(line) > maxPendingFrameBytes {
		return nil, fmt.Errorf("signal rpc: frame exceeds %d bytes", maxPendingFrameBytes)
	}
	return line, nil
}

// readFrameLine reads one \\n-terminated line (terminator stripped),
// tolerating \\r\\n.
func readFrameLine(r *bufio.Reader) ([]byte, error) {
	chunk, err := r.ReadString('\n')
	if err != nil {
		if len(chunk) > 0 && (err == io.EOF) {
			return trimEOL([]byte(chunk)), nil
		}
		return nil, err
	}
	return trimEOL([]byte(chunk)), nil
}

// trimEOL strips trailing \\n and \\r.
func trimEOL(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}
