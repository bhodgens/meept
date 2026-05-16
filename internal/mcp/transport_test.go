package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestReadMessage(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n"
	r := bytes.NewReader([]byte(input))

	msg, err := ReadMessage(r)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.Method != "tools/list" {
		t.Errorf("Method = %q, want %q", msg.Method, "tools/list")
	}
	if msg.ID != float64(1) {
		t.Errorf("ID = %v, want 1", msg.ID)
	}
}

func TestReadMessageBufferedMultiple(t *testing.T) {
	// Test that a BufferedReader correctly reads multiple messages
	// from a pre-buffered reader without losing data.
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"test","arguments":{}}}` + "\n"

	br := NewBufferedReader(bytes.NewReader([]byte(input)))

	// Read first message
	msg1, err := ReadMessageBuffered(br)
	if err != nil {
		t.Fatalf("ReadMessageBuffered(1): %v", err)
	}
	if msg1.Method != "tools/list" {
		t.Errorf("message 1 method = %q, want %q", msg1.Method, "tools/list")
	}
	if msg1.ID != float64(1) {
		t.Errorf("message 1 ID = %v, want 1", msg1.ID)
	}

	// Read second message
	msg2, err := ReadMessageBuffered(br)
	if err != nil {
		t.Fatalf("ReadMessageBuffered(2): %v", err)
	}
	if msg2.Method != "initialize" {
		t.Errorf("message 2 method = %q, want %q", msg2.Method, "initialize")
	}
	if msg2.ID != float64(2) {
		t.Errorf("message 2 ID = %v, want 2", msg2.ID)
	}

	// Read third message
	msg3, err := ReadMessageBuffered(br)
	if err != nil {
		t.Fatalf("ReadMessageBuffered(3): %v", err)
	}
	if msg3.Method != "tools/call" {
		t.Errorf("message 3 method = %q, want %q", msg3.Method, "tools/call")
	}
	if msg3.ID != float64(3) {
		t.Errorf("message 3 ID = %v, want 3", msg3.ID)
	}
}

func TestWriteMessage(t *testing.T) {
	var buf bytes.Buffer
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      float64(1),
		Result:  json.RawMessage(`{"tools":[]}`),
	}
	if err := WriteMessage(&buf, resp); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}
	// Should end with newline
	if buf.Bytes()[buf.Len()-1] != '\n' {
		t.Error("expected trailing newline")
	}
}
