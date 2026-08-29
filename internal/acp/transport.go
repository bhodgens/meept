package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ErrClosed is returned when Call or Notify is used after Close.
var ErrClosed = errors.New("transport closed")

type callResult struct {
	resp *Response
	err  error
}

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Transport is a newline-delimited JSON-RPC 2.0 client over an io pair.
type Transport struct {
	in  io.Reader
	out io.Writer

	writeMu sync.Mutex

	mu       sync.Mutex
	closed   bool
	nextID   int64
	pending  map[int64]chan callResult
	handlers []func(Notification)
}

// NewTransport returns a Transport that reads from in and writes to out.
func NewTransport(in io.Reader, out io.Writer) *Transport {
	return &Transport{
		in:      in,
		out:     out,
		pending: make(map[int64]chan callResult),
	}
}

// Call sends a JSON-RPC request and waits for the matching response.
func (t *Transport) Call(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("acp: call %s: %w", method, err)
	}

	ch := make(chan callResult, 1)
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return fmt.Errorf("acp: call %s: %w", method, ErrClosed)
	}
	t.nextID++
	id := t.nextID
	t.pending[id] = ch
	t.mu.Unlock()

	req := Request{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := t.writeJSON(req); err != nil {
		t.dropPending(id)
		return fmt.Errorf("acp: call %s: %w", method, err)
	}

	select {
	case cr := <-ch:
		if cr.err != nil {
			return fmt.Errorf("acp: call %s: %w", method, cr.err)
		}
		if cr.resp == nil {
			return fmt.Errorf("acp: call %s: %w", method, ErrClosed)
		}
		if cr.resp.Error != nil {
			return cr.resp.Error
		}
		if result != nil && len(cr.resp.Result) > 0 && string(cr.resp.Result) != "null" {
			if err := json.Unmarshal(cr.resp.Result, result); err != nil {
				return fmt.Errorf("acp: decode %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		t.dropPending(id)
		return fmt.Errorf("acp: call %s: %w", method, ctx.Err())
	}
}

// Notify sends a JSON-RPC notification (no response).
func (t *Transport) Notify(method string, params any) error {
	var payload json.RawMessage
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("acp: notify %s: %w", method, err)
		}
		payload = raw
	}
	n := Notification{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  payload,
	}
	if err := t.writeJSON(n); err != nil {
		return fmt.Errorf("acp: notify %s: %w", method, err)
	}
	return nil
}

// Reply writes a JSON-RPC response for an inbound request (e.g. permission).
func (t *Transport) Reply(id int64, result any, rpcErr *RPCError) error {
	resp := Response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   rpcErr,
	}
	if rpcErr == nil && result != nil {
		raw, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("acp: reply %d: %w", id, err)
		}
		resp.Result = raw
	}
	if err := t.writeJSON(resp); err != nil {
		return fmt.Errorf("acp: reply %d: %w", id, err)
	}
	return nil
}

// OnNotification registers a handler for inbound notifications and
// inbound requests that carry a method (e.g. session/requestPermission).
func (t *Transport) OnNotification(fn func(Notification)) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	t.handlers = append(t.handlers, fn)
	t.mu.Unlock()
}

// Serve is a blocking read loop. It returns when the input is closed or
// Close is called.
func (t *Transport) Serve() {
	dec := json.NewDecoder(t.in)
	for {
		var msg wireMessage
		if err := dec.Decode(&msg); err != nil {
			if closeErr := t.Close(); closeErr != nil {
				return
			}
			return
		}
		t.dispatch(msg)
	}
}

// Close is idempotent. It unblocks in-flight Calls with ErrClosed
// wrapping context.Canceled.
func (t *Transport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	pending := t.pending
	t.pending = make(map[int64]chan callResult)
	in, out := t.in, t.out
	t.mu.Unlock()

	var inErr, outErr error
	if c, ok := in.(io.Closer); ok {
		inErr = c.Close()
	}
	if c, ok := out.(io.Closer); ok {
		outErr = c.Close()
	}

	closedErr := fmt.Errorf("acp: %w: %w", ErrClosed, context.Canceled)
	for _, ch := range pending {
		ch <- callResult{err: closedErr}
	}

	switch {
	case inErr != nil && outErr != nil:
		return fmt.Errorf("acp: close: %w", errors.Join(inErr, outErr))
	case inErr != nil:
		return fmt.Errorf("acp: close input: %w", inErr)
	case outErr != nil:
		return fmt.Errorf("acp: close output: %w", outErr)
	default:
		return nil
	}
}

func (t *Transport) dropPending(id int64) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

func (t *Transport) dispatch(msg wireMessage) {
	if msg.Method != "" {
		n := Notification{
			JSONRPC: msg.JSONRPC,
			ID:      msg.ID,
			Method:  msg.Method,
			Params:  msg.Params,
		}
		t.mu.Lock()
		handlers := make([]func(Notification), len(t.handlers))
		copy(handlers, t.handlers)
		t.mu.Unlock()
		for _, h := range handlers {
			h(n)
		}
		return
	}
	if msg.ID == nil {
		return
	}
	id := *msg.ID
	t.mu.Lock()
	ch, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
	}
	t.mu.Unlock()
	if !ok {
		return
	}
	ch <- callResult{
		resp: &Response{
			JSONRPC: msg.JSONRPC,
			ID:      id,
			Result:  msg.Result,
			Error:   msg.Error,
		},
	}
}

func (t *Transport) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	buf := make([]byte, len(data)+1)
	copy(buf, data)
	buf[len(data)] = '\n'

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if _, err := t.out.Write(buf); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}
