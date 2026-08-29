package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"
)

func newTestPipes(t *testing.T) (tr *Transport, fromClient *io.PipeReader, toClient *io.PipeWriter) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	tr = NewTransport(inR, outW)
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Logf("transport close: %v", err)
		}
		if err := inW.Close(); err != nil {
			t.Logf("toClient close: %v", err)
		}
		if err := outR.Close(); err != nil {
			t.Logf("fromClient close: %v", err)
		}
	})
	return tr, outR, inW
}

func startServe(t *testing.T, tr *Transport) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		tr.Serve()
		close(done)
	}()
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Logf("serve close: %v", err)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("Serve did not return after Close")
		}
	})
}

func TestTransportCallInitialize(t *testing.T) {
	tr, fromClient, toClient := newTestPipes(t)
	startServe(t, tr)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var result InitializeResult
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Call(ctx, MethodInitialize, InitializeParams{ProtocolVersion: ProtocolVersion}, &result)
	}()

	var req Request
	if err := json.NewDecoder(fromClient).Decode(&req); err != nil {
		t.Fatalf("read request: %v", err)
	}
	if req.Method != MethodInitialize || req.JSONRPC != "2.0" {
		t.Fatalf("request = %+v", req)
	}

	body, err := json.Marshal(InitializeResult{ProtocolVersion: ProtocolVersion})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	resp := Response{JSONRPC: "2.0", ID: req.ID, Result: body}
	if err := json.NewEncoder(toClient).Encode(resp); err != nil {
		t.Fatalf("write response: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Fatalf("result.ProtocolVersion = %d, want %d", result.ProtocolVersion, ProtocolVersion)
	}
}

func TestTransportUnknownIDResponseIgnored(t *testing.T) {
	tr, fromClient, toClient := newTestPipes(t)
	startServe(t, tr)

	unknown := Response{JSONRPC: "2.0", ID: 99999, Result: json.RawMessage(`{}`)}
	if err := json.NewEncoder(toClient).Encode(unknown); err != nil {
		t.Fatalf("write unknown response: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var result map[string]any
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Call(ctx, MethodSessionNew, SessionNewParams{Cwd: "/tmp"}, &result)
	}()

	var req Request
	if err := json.NewDecoder(fromClient).Decode(&req); err != nil {
		t.Fatalf("read request: %v", err)
	}
	body := json.RawMessage(`{"sessionId":"sess_ok"}`)
	resp := Response{JSONRPC: "2.0", ID: req.ID, Result: body}
	if err := json.NewEncoder(toClient).Encode(resp); err != nil {
		t.Fatalf("write response: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Call after unknown-id: %v", err)
	}
	if got, _ := result["sessionId"].(string); got != "sess_ok" {
		t.Fatalf("result = %#v", result)
	}
}

func TestTransportNotificationDelivered(t *testing.T) {
	tr, _, toClient := newTestPipes(t)

	got := make(chan Notification, 1)
	tr.OnNotification(func(n Notification) {
		got <- n
	})
	startServe(t, tr)

	n := Notification{
		JSONRPC: "2.0",
		Method:  MethodSessionUpdate,
		Params:  json.RawMessage(`{"sessionId":"sess_1"}`),
	}
	if err := json.NewEncoder(toClient).Encode(n); err != nil {
		t.Fatalf("write notification: %v", err)
	}

	select {
	case n := <-got:
		if n.Method != MethodSessionUpdate {
			t.Fatalf("method = %q", n.Method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification not delivered")
	}
}

func TestTransportConcurrentCalls(t *testing.T) {
	tr, fromClient, toClient := newTestPipes(t)
	startServe(t, tr)

	const n = 3
	type callRes struct {
		id  int
		err error
		out SessionNewResult
	}
	results := make(chan callRes, n)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			var out SessionNewResult
			err := tr.Call(ctx, MethodSessionNew, SessionNewParams{Cwd: "/tmp"}, &out)
			results <- callRes{id: i, err: err, out: out}
		}()
	}

	dec := json.NewDecoder(fromClient)
	enc := json.NewEncoder(toClient)
	for i := 0; i < n; i++ {
		var req Request
		if err := dec.Decode(&req); err != nil {
			t.Fatalf("read request %d: %v", i, err)
		}
		body, err := json.Marshal(SessionNewResult{SessionID: "sess_" + strconv.FormatInt(req.ID, 10)})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := enc.Encode(Response{JSONRPC: "2.0", ID: req.ID, Result: body}); err != nil {
			t.Fatalf("write response %d: %v", i, err)
		}
	}

	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for r := range results {
		if r.err != nil {
			t.Errorf("call %d: %v", r.id, r.err)
			continue
		}
		if r.out.SessionID == "" {
			t.Errorf("call %d empty session id", r.id)
			continue
		}
		if seen[r.out.SessionID] {
			t.Errorf("duplicate session id %q", r.out.SessionID)
		}
		seen[r.out.SessionID] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d unique results, want %d", len(seen), n)
	}
}

func TestTransportCallTimeout(t *testing.T) {
	tr, fromClient, _ := newTestPipes(t)
	startServe(t, tr)

	// Drain outbound so Call's write cannot block on a full pipe.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := fromClient.Read(buf); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := tr.Call(ctx, MethodInitialize, InitializeParams{ProtocolVersion: ProtocolVersion}, &InitializeResult{})
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call error = %v, want deadline exceeded", err)
	}
	if elapsed > time.Second {
		t.Fatalf("Call hung for %v after timeout", elapsed)
	}
}

func TestTransportCloseUnblocksCall(t *testing.T) {
	tr, fromClient, _ := newTestPipes(t)
	startServe(t, tr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Call(context.Background(), MethodInitialize, nil, nil)
	}()

	var req Request
	if err := json.NewDecoder(fromClient).Decode(&req); err != nil {
		t.Fatalf("read in-flight request: %v", err)
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("in-flight Call succeeded after Close")
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) {
			t.Fatalf("Call error = %v, want closed/canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock in-flight Call")
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestTransportNotifyWritesNotification(t *testing.T) {
	tr, fromClient, _ := newTestPipes(t)

	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Notify(MethodSessionCancel, SessionCancelParams{SessionID: "sess_1"})
	}()

	var n Notification
	if err := json.NewDecoder(fromClient).Decode(&n); err != nil {
		t.Fatalf("read notify: %v", err)
	}
	if n.Method != MethodSessionCancel {
		t.Fatalf("method = %q", n.Method)
	}
	if n.JSONRPC != "2.0" {
		t.Fatalf("jsonrpc = %q", n.JSONRPC)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Notify: %v", err)
	}
}

func TestTransportRPCErrorReturned(t *testing.T) {
	tr, fromClient, toClient := newTestPipes(t)
	startServe(t, tr)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Call(ctx, MethodInitialize, nil, nil)
	}()

	var req Request
	if err := json.NewDecoder(fromClient).Decode(&req); err != nil {
		t.Fatalf("read request: %v", err)
	}
	resp := Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Error:   &RPCError{Code: -32601, Message: "method not found"},
	}
	if err := json.NewEncoder(toClient).Encode(resp); err != nil {
		t.Fatalf("write response: %v", err)
	}

	err := <-errCh
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error = %v, want *RPCError", err)
	}
	if rpcErr.Code != -32601 {
		t.Fatalf("code = %d", rpcErr.Code)
	}
}

func TestTransportInboundRequestIDAndReply(t *testing.T) {
	tr, fromClient, toClient := newTestPipes(t)
	got := make(chan Notification, 1)
	tr.OnNotification(func(n Notification) { got <- n })
	startServe(t, tr)

	id := int64(42)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  MethodSessionRequestPermission,
		"params":  map[string]any{"sessionId": "s1"},
	}
	if err := json.NewEncoder(toClient).Encode(req); err != nil {
		t.Fatalf("write inbound: %v", err)
	}

	n := <-got
	if n.Method != MethodSessionRequestPermission {
		t.Fatalf("method = %q", n.Method)
	}
	if n.ID == nil || *n.ID != id {
		t.Fatalf("id = %v, want %d", n.ID, id)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.Reply(*n.ID, map[string]string{"outcome": "selected"}, nil)
	}()
	var resp Response
	if err := json.NewDecoder(fromClient).Decode(&resp); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if resp.ID != id || resp.Error != nil || len(resp.Result) == 0 {
		t.Fatalf("reply = %+v", resp)
	}
}
