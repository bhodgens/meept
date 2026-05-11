package mcp

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNewRequest(t *testing.T) {
	req := NewRequest(1, "test/method", map[string]any{"key": "value"})

	if req.JSONRPC != "2.0" {
		t.Errorf("expected JSONRPC '2.0', got %q", req.JSONRPC)
	}
	if req.ID != 1 {
		t.Errorf("expected ID 1, got %v", req.ID)
	}
	if req.Method != "test/method" {
		t.Errorf("expected method 'test/method', got %q", req.Method)
	}

	// Test serialization
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["jsonrpc"] != "2.0" {
		t.Error("jsonrpc field missing or incorrect")
	}
	if parsed["id"].(float64) != 1 {
		t.Error("id field missing or incorrect")
	}
}

func TestNewNotification(t *testing.T) {
	notif := NewNotification("notifications/test", nil)

	if notif.JSONRPC != "2.0" {
		t.Errorf("expected JSONRPC '2.0', got %q", notif.JSONRPC)
	}
	if notif.ID != nil {
		t.Errorf("expected nil ID for notification, got %v", notif.ID)
	}

	// When marshaled, ID should be omitted
	data, err := json.Marshal(notif)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)

	if _, exists := parsed["id"]; exists {
		t.Error("notification should not have id field")
	}
}

func TestRPCError(t *testing.T) {
	err := &RPCError{
		Code:    ErrCodeMethodNotFound,
		Message: "Method not found",
	}

	errStr := err.Error()
	if errStr != "RPC error -32601: Method not found" {
		t.Errorf("unexpected error string: %s", errStr)
	}

	// With data
	err.Data = map[string]any{"detail": "extra info"}
	errStr = err.Error()
	if errStr == "RPC error -32601: Method not found" {
		t.Error("error string should include data")
	}
}

func TestParseResponse(t *testing.T) {
	// Success response
	t.Run("success", func(t *testing.T) {
		data := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
		resp, err := ParseResponse(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.ID != float64(1) {
			t.Errorf("expected ID 1, got %v", resp.ID)
		}
		if resp.Error != nil {
			t.Error("expected no error")
		}
		if resp.Result == nil {
			t.Error("expected result")
		}
	})

	// Error response
	t.Run("error", func(t *testing.T) {
		data := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`)
		resp, err := ParseResponse(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Error == nil {
			t.Fatal("expected error")
		}
		if resp.Error.Code != ErrCodeMethodNotFound {
			t.Errorf("expected code %d, got %d", ErrCodeMethodNotFound, resp.Error.Code)
		}
	})

	// Invalid JSON
	t.Run("invalid json", func(t *testing.T) {
		data := []byte(`{invalid}`)
		_, err := ParseResponse(data)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestExtractResult(t *testing.T) {
	// Success case
	t.Run("success", func(t *testing.T) {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`{"tools":[{"name":"test"}]}`),
		}

		result, err := ExtractResult[ListToolsResult](resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Tools) != 1 {
			t.Errorf("expected 1 tool, got %d", len(result.Tools))
		}
		if result.Tools[0].Name != "test" {
			t.Errorf("expected tool name 'test', got %q", result.Tools[0].Name)
		}
	})

	// Error response
	t.Run("error response", func(t *testing.T) {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      1,
			Error: &RPCError{
				Code:    ErrCodeMethodNotFound,
				Message: "Method not found",
			},
		}

		_, err := ExtractResult[ListToolsResult](resp)
		if err == nil {
			t.Error("expected error")
		}
		rpcErr := &RPCError{}
		ok := errors.As(err, &rpcErr)
		if !ok {
			t.Errorf("expected *RPCError, got %T", err)
		}
		if rpcErr.Code != ErrCodeMethodNotFound {
			t.Errorf("expected code %d, got %d", ErrCodeMethodNotFound, rpcErr.Code)
		}
	})

	// Invalid result JSON
	t.Run("invalid result", func(t *testing.T) {
		resp := &Response{
			JSONRPC: "2.0",
			ID:      1,
			Result:  json.RawMessage(`{invalid}`),
		}

		_, err := ExtractResult[ListToolsResult](resp)
		if err == nil {
			t.Error("expected error for invalid result")
		}
	})
}

func TestInitializeParams(t *testing.T) {
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ClientCapabilities{},
		ClientInfo: ImplementationInfo{
			Name:    "meept",
			Version: "0.2.0",
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed InitializeParams
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.ProtocolVersion != ProtocolVersion {
		t.Errorf("expected protocol version %q, got %q", ProtocolVersion, parsed.ProtocolVersion)
	}
	if parsed.ClientInfo.Name != "meept" {
		t.Errorf("expected client name 'meept', got %q", parsed.ClientInfo.Name)
	}
}

func TestToolInfo(t *testing.T) {
	tool := ToolInfo{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "Input value",
				},
			},
			"required": []string{"input"},
		},
	}

	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed ToolInfo
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.Name != "test_tool" {
		t.Errorf("expected name 'test_tool', got %q", parsed.Name)
	}
	if parsed.InputSchema == nil {
		t.Error("expected input schema")
	}
}

func TestCallToolResult(t *testing.T) {
	result := CallToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: "Hello, World!"},
		},
		IsError: false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed CallToolResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(parsed.Content) != 1 {
		t.Errorf("expected 1 content block, got %d", len(parsed.Content))
	}
	if parsed.Content[0].Text != "Hello, World!" {
		t.Errorf("expected text 'Hello, World!', got %q", parsed.Content[0].Text)
	}
	if parsed.IsError {
		t.Error("expected IsError to be false")
	}
}
