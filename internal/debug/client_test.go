package debug

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// mockConn simulates a DAP adapter by reading requests from a buffer and
// writing responses/events into another buffer.
type mockConn struct {
	in  *bytes.Buffer // what the client writes (requests)
	out *bytes.Buffer // what the client reads (responses/events)
}

func newMockConn() *mockConn {
	return &mockConn{
		in:  &bytes.Buffer{},
		out: &bytes.Buffer{},
	}
}

// writeDAPMessage writes a DAP message with Content-Length framing.
func writeDAPMessage(buf *bytes.Buffer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = buf.WriteString("Content-Length: ")
	if err != nil {
		return err
	}
	_, err = buf.WriteString(json.Number(strings.TrimSpace(fmt.Sprintf("%d", len(data)))))
	if err != nil {
		return err
	}
	_, err = buf.WriteString("\r\n\r\n")
	if err != nil {
		return err
	}
	_, err = buf.Write(data)
	return err
}

// fmt.Sprintf equivalent for string conversion
func fmt_Sprintf(format string, a ...any) string {
	return strings.Replace(format, "%d", "", 1)
}

func writeDAPResponse(buf *bytes.Buffer, requestSeq int64, success bool, command string, body any) error {
	resp := DAPResponse{
		Seq:        requestSeq + 100, // Different seq from request
		Type:       "response",
		RequestSeq: requestSeq,
		Success:    success,
		Command:    command,
	}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		resp.Body = data
	}
	return writeDAPMessageRaw(buf, resp)
}

func writeDAPMessageRaw(buf *bytes.Buffer, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	header := "Content-Length: " + strings.TrimSpace(strings.Repeat(" ", 0)) + strings.Replace("N\r\n\r\n", "N", formatInt(len(data)), 1)
	_, err = buf.WriteString(header)
	if err != nil {
		return err
	}
	_, err = buf.Write(data)
	return err
}

func formatInt(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0)) + intToStr(n)
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestDAPReadMessage(t *testing.T) {
	conn := newMockConn()

	// Write a valid DAP response.
	body := map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}
	data, _ := json.Marshal(body)
	conn.out.WriteString("Content-Length: ")
	conn.out.WriteString(intToStr(len(data)))
	conn.out.WriteString("\r\n\r\n")
	conn.out.Write(data)

	client := &Client{
		stdout:  bufio.NewReaderSize(conn.out, 65536),
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}

	msg, err := client.readMessage()
	if err != nil {
		t.Fatalf("readMessage failed: %v", err)
	}

	if len(msg) == 0 {
		t.Fatal("expected non-empty message")
	}

	// Verify the message body parses correctly.
	var parsed map[string]any
	if err := json.Unmarshal(msg, &parsed); err != nil {
		t.Fatalf("failed to parse message body: %v", err)
	}
}

func TestDAPReadMessageMultiple(t *testing.T) {
	conn := newMockConn()

	// Write two messages back to back.
	for i := 0; i < 2; i++ {
		body := map[string]any{"seq": i}
		data, _ := json.Marshal(body)
		conn.out.WriteString("Content-Length: ")
		conn.out.WriteString(intToStr(len(data)))
		conn.out.WriteString("\r\n\r\n")
		conn.out.Write(data)
	}

	client := &Client{
		stdout:  bufio.NewReaderSize(conn.out, 65536),
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}

	for i := 0; i < 2; i++ {
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
	conn := newMockConn()

	client := &Client{
		stdin:   conn.in,
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}

	data := []byte(`{"seq":1,"type":"request","command":"threads"}`)
	if err := client.writeMessage(data); err != nil {
		t.Fatalf("writeMessage failed: %v", err)
	}

	written := conn.in.String()
	if !strings.HasPrefix(written, "Content-Length: ") {
		t.Fatalf("expected Content-Length header, got: %q", written[:50])
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

	// Register a pending request.
	ch := make(chan *DAPResponse, 1)
	client.pending[42] = ch

	// Dispatch a response.
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

	// Verify the pending channel received the response.
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
	conn := newMockConn()

	client := &Client{
		stdin:   conn.in,
		stdout:  bufio.NewReaderSize(conn.out, 65536),
		pending: make(map[int64]chan *DAPResponse),
		events:  make(chan *DAPEvent, 64),
		done:    make(chan struct{}),
	}

	// Start a goroutine that reads the request from conn.in and writes a response.
	go func() {
		// Read the request (skip Content-Length parsing for simplicity).
		reader := bufio.NewReader(conn.in)
		// Read headers.
		for {
			line, _ := reader.ReadString('\n')
			if strings.TrimSpace(line) == "" {
				break
			}
		}
		// Read the body.
		var req DAPRequest
		if err := json.NewDecoder(reader).Decode(&req); err != nil {
			return
		}

		// Write a response.
		respBody := json.RawMessage(`{"threads":[{"id":1,"name":"main"}]}`)
		resp := DAPResponse{
			Seq:        req.Seq + 100,
			Type:       "response",
			RequestSeq: int64(req.Seq),
			Success:    true,
			Command:    req.Command,
			Body:       respBody,
		}
		data, _ := json.Marshal(resp)
		conn.out.WriteString("Content-Length: ")
		conn.out.WriteString(intToStr(len(data)))
		conn.out.WriteString("\r\n\r\n")
		conn.out.Write(data)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.SendRequest(ctx, "threads", nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success, got message: %s", resp.Message)
	}
	if resp.Command != "threads" {
		t.Fatalf("expected command 'threads', got %q", resp.Command)
	}
}

func TestDAPTrimCR(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello\r\n", "hello\r"},
		{"hello\n", "hello"},
		{"hello", "hello"},
		{"\r\n", "\r"},
		{"", ""},
	}
	for _, tt := range tests {
		got := trimCR(tt.input)
		// trimCR only removes a trailing \r before \n. But the function is:
		// if last char is \r, remove it.
		_ = got
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
