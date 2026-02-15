package rpc

import (
	"bytes"
	"testing"
)

func TestFrameReader_ReadFrame(t *testing.T) {
	// Create a buffer with a valid frame
	// {"test":"data"} = 15 bytes
	data := "15\n{\"test\":\"data\"}"
	reader := NewFrameReader(bytes.NewBufferString(data))

	frame, err := reader.ReadFrame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"test":"data"}`
	if string(frame) != expected {
		t.Errorf("expected %q, got %q", expected, string(frame))
	}
}

func TestFrameReader_ReadRequest(t *testing.T) {
	data := `52
{"jsonrpc":"2.0","id":1,"method":"ping","params":{}}`
	reader := NewFrameReader(bytes.NewBufferString(data))

	req, err := reader.ReadRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Method != "ping" {
		t.Errorf("expected method ping, got %s", req.Method)
	}
	if req.ID != float64(1) {
		t.Errorf("expected id 1, got %v", req.ID)
	}
}

func TestFrameWriter_WriteFrame(t *testing.T) {
	var buf bytes.Buffer
	writer := NewFrameWriter(&buf)

	payload := []byte(`{"result":"pong"}`)
	if err := writer.WriteFrame(payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "17\n{\"result\":\"pong\"}"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestMakeResponse(t *testing.T) {
	resp := MakeResponse(1, "pong")

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
	if resp.ID != 1 {
		t.Errorf("expected id 1, got %v", resp.ID)
	}
	if resp.Error != nil {
		t.Error("expected no error")
	}
}

func TestMakeErrorResponse(t *testing.T) {
	resp := MakeErrorResponse(1, -32601, "method not found", nil)

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "method not found" {
		t.Errorf("expected 'method not found', got %s", resp.Error.Message)
	}
}
