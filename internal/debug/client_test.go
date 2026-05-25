package debug

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// writeDAP writes a DAP message with Content-Length framing to an io.Writer.
func writeDAP(w io.Writer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func TestDAPReadMessageBasic(t *testing.T) {
	var buf bytes.Buffer

	body := map[string]any{"type": "response", "request_seq": 1, "success": true, "command": "threads"}
	writeDAP(&buf, body)

	client := &Client{
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}
	client.stdout = bufio.NewReaderSize(&buf, 65536)

	msg, err := client.readMessage()
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}

	if len(msg) == 0 {
		t.Fatal("expected non-empty message")
	}

	var parsed map[string]any
	if err := json.Unmarshal(msg, &parsed); err != nil {
		t.Fatalf("failed to parse message body: %v", err)
	}
	if parsed["command"] != "threads" {
		t.Fatalf("expected command 'threads', got %v", parsed["command"])
	}
}

func TestDAPReadMessageMultiple(t *testing.T) {
	var buf bytes.Buffer

	for i := 0; i < 3; i++ {
		body := map[string]any{"seq": i, "type": "event", "event": "output"}
		if err := writeDAP(&buf, body); err != nil {
			t.Fatalf("writeDAP %d failed: %v", i, err)
		}
	}

	client := &Client{
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}
	client.stdout = bufio.NewReaderSize(&buf, 65536)

	for i := 0; i < 3; i++ {
		msg, err := client.readMessage()
		if err != nil {
			t.Fatalf("readMessage %d failed: %v", i, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(msg, &parsed); err != nil {
			t.Fatalf("failed to parse message %d: %v", i, err)
		}
	}
}

func TestDAPWriteMessage(t *testing.T) {
	var buf bytes.Buffer

	client := &Client{
		stdin:   &nopWriteCloser{Writer: &buf},
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}

	data := []byte(`{"seq":1,"type":"request","command":"threads"}`)
	if err := client.writeMessage(data); err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	written := buf.String()
	if !strings.HasPrefix(written, "Content-Length: ") {
		t.Fatalf("expected Content-Length header, got: %q", written[:minInt(50, len(written))])
	}
	if !strings.Contains(written, "\r\n\r\n") {
		t.Fatal("expected header/body separator")
	}
	if !strings.Contains(written, `"command":"threads"`) {
		t.Fatalf("expected threads command in body, got: %q", written)
	}
}

func TestDAPDispatchResponse(t *testing.T) {
	client := &Client{
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}

	ch := make(chan *DAPResponse, 1)
	client.pending[42] = ch

	resp := DAPResponse{
		Seq:        100,
		Type:       "response",
		RequestSeq: 42,
		Success:    true,
		Command:    "threads",
		Body:       json.RawMessage(`{"threads":[{"id":1,"name":"main"}]}`),
	}
	data, _ := json.Marshal(resp)
	client.dispatchMessage(data)

	select {
	case got := <-ch:
		if got.Command != "threads" {
			t.Fatalf("expected command 'threads', got %q", got.Command)
		}
		if !got.Success {
			t.Fatal("expected success")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestDAPDispatchEvent(t *testing.T) {
	client := &Client{
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}

	evt := DAPEvent{
		Seq:   50,
		Type:  "event",
		Event: "stopped",
		Body:  json.RawMessage(`{"reason":"breakpoint","threadId":1}`),
	}
	data, _ := json.Marshal(evt)
	client.dispatchMessage(data)

	select {
	case got := <-client.Events():
		if got.Event != "stopped" {
			t.Fatalf("expected event 'stopped', got %q", got.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestDAPSendRequestRoundTrip(t *testing.T) {
	// Use bytes.Buffer for synchronous in-memory test.
	// Write the request, simulate adapter inline, then read the response.
	adapterIn := &bytes.Buffer{}
	adapterOut := &bytes.Buffer{}

	client := &Client{
		stdin:   &nopWriteCloser{Writer: adapterIn},
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}

	// Step 1: Send the request (this writes to adapterIn and registers a pending channel).
	// We can't use SendRequest directly because it blocks waiting for a response.
	// Instead, manually write the request and dispatch the response.
	seq := client.seq.Add(1)
	req := DAPRequest{
		Seq:     int(seq),
		Type:    "request",
		Command: "threads",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	respCh := make(chan *DAPResponse, 1)
	client.mu.Lock()
	client.pending[seq] = respCh
	client.mu.Unlock()

	if err := client.writeMessage(data); err != nil {
		t.Fatalf("writeMessage: %v", err)
	}

	// Step 2: Simulate adapter reading the request and writing a response.
	reader := bufio.NewReaderSize(adapterIn, 65536)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
	}

	var gotReq DAPRequest
	if err := json.NewDecoder(reader).Decode(&gotReq); err != nil {
		t.Fatalf("decode request: %v", err)
	}

	// Verify the request.
	if gotReq.Command != "threads" {
		t.Fatalf("expected command 'threads', got %q", gotReq.Command)
	}

	// Step 3: Write response.
	resp := DAPResponse{
		Seq:        seq + 100,
		Type:       "response",
		RequestSeq: seq,
		Success:    true,
		Command:    "threads",
		Body:       json.RawMessage(`{"threads":[{"id":1,"name":"main"}]}`),
	}
	writeDAP(adapterOut, resp)

	// Step 4: Read the response through the client.
	client.stdout = bufio.NewReaderSize(adapterOut, 65536)
	msg, err := client.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	client.dispatchMessage(msg)

	// Step 5: Verify the pending channel received the response.
	select {
	case got := <-respCh:
		if got.Command != "threads" {
			t.Fatalf("expected command 'threads', got %q", got.Command)
		}
		if !got.Success {
			t.Fatal("expected success")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestDAPSendRequestCancellation(t *testing.T) {
	// Use a pipe where nobody writes, so SendRequest blocks until cancelled.
	cr, _ := io.Pipe()
	var aw bytes.Buffer

	client := &Client{
		stdin:   &nopWriteCloser{Writer: &aw},
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}
	client.stdout = bufio.NewReaderSize(cr, 65536)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := client.SendRequest(ctx, "threads", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	cr.Close()
}

func TestDAPTrimCR(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello\r", "hello"},
		{"hello", "hello"},
		{"", ""},
		{"\r", ""},
	}
	for _, tt := range tests {
		got := trimCR(tt.input)
		if got != tt.expected {
			t.Errorf("trimCR(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDAPBoolPtr(t *testing.T) {
	p := boolPtr(true)
	if p == nil || !*p {
		t.Fatal("expected *true")
	}
	p = boolPtr(false)
	if p == nil || *p {
		t.Fatal("expected *false")
	}
}

// nopWriteCloser wraps an io.Writer to implement io.WriteCloser.
type nopWriteCloser struct {
	io.Writer
}

func (w *nopWriteCloser) Close() error { return nil }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
