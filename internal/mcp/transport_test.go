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
