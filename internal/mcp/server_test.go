package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestServerHandlesInitialize(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test"}}}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	// Process one message
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	// Read response
	var resp JSONRPCResponse
	line, err := output.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	// Should contain server info
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["serverInfo"] == nil {
		t.Error("expected serverInfo in initialize response")
	}

	// Verify serverInfo content
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo is not a map, got %T", result["serverInfo"])
	}
	if serverInfo["name"] != "meept" {
		t.Errorf("serverInfo.name = %v, want meept", serverInfo["name"])
	}
	if serverInfo["version"] != Version {
		t.Errorf("serverInfo.version = %v, want %s", serverInfo["version"], Version)
	}

	// Verify capabilities
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities is not a map, got %T", result["capabilities"])
	}
	if caps["tools"] == nil {
		t.Error("expected tools capability")
	}

	// Verify protocol version
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", result["protocolVersion"])
	}
}

func TestServerHandlesToolsList(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	var resp JSONRPCResponse
	line, _ := output.ReadBytes('\n')
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Error("expected non-empty tools array")
	}

	// Verify we have all expected tools
	expectedTools := []string{"meept_sessions", "meept_send", "meept_events", "meept_status", "meept_session_history"}
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			t.Errorf("tool is not a map, got %T", tool)
			continue
		}
		name, _ := toolMap["name"].(string)
		toolNames[name] = true
	}
	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("missing tool: %s", expected)
		}
	}
}

func TestServerHandlesUnknownMethod(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":2,"method":"unknown/method","params":{}}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	var resp JSONRPCResponse
	line, _ := output.ReadBytes('\n')
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestServerHandlesNotification(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}
	// No response should be written for notifications
	if output.Len() > 0 {
		t.Errorf("expected no output for notification, got %q", output.String())
	}
}

func TestServerHandlesToolsCallNoClient(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"meept_status","arguments":{}}}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	var resp JSONRPCResponse
	line, _ := output.ReadBytes('\n')
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error when client is nil")
	}
	if resp.Error.Code != -32000 {
		t.Errorf("error code = %d, want -32000", resp.Error.Code)
	}
}

func TestServerHandlesToolsCallUnknownTool(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nonexistent_tool","arguments":{}}}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	var resp JSONRPCResponse
	line, _ := output.ReadBytes('\n')
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestServerRunEOF(t *testing.T) {
	// Empty input should result in clean EOF return
	input := bytes.NewBufferString("")
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	if err := srv.Run(); err != nil {
		t.Fatalf("Run with empty input: %v", err)
	}
}

func TestServerHandlesToolsCallInvalidParams(t *testing.T) {
	input := bytes.NewBufferString(
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{}}` + "\n",
	)
	output := &bytes.Buffer{}

	srv := NewServer(input, output, nil)
	if err := srv.processOne(); err != nil {
		t.Fatalf("processOne: %v", err)
	}

	var resp JSONRPCResponse
	line, _ := output.ReadBytes('\n')
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for missing tool name")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("error code = %d, want -32602", resp.Error.Code)
	}
}
