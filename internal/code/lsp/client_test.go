package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// mockTransport implements transport.Transport for testing.
type mockTransport struct {
	mu          sync.Mutex
	readData    chan []byte
	writeData   [][]byte
	writeErr    error
	readErr     error
	closed      bool
	closeErr    error
	onWrite     func(data []byte) // optional callback for inspection
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		readData:  make(chan []byte, 10),
		writeData: nil,
	}
}

func (mt *mockTransport) Read(ctx context.Context) ([]byte, error) {
	if mt.readErr != nil {
		return nil, mt.readErr
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case data := <-mt.readData:
		return data, nil
	}
}

func (mt *mockTransport) Write(ctx context.Context, data []byte) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if mt.writeErr != nil {
		return mt.writeErr
	}

	mt.writeData = append(mt.writeData, data)

	if mt.onWrite != nil {
		mt.onWrite(data)
	}

	return nil
}

func (mt *mockTransport) Close() error {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.closed = true
	return mt.closeErr
}

func (mt *mockTransport) getWrites() [][]byte {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	result := make([][]byte, len(mt.writeData))
	copy(result, mt.writeData)
	return result
}

func (mt *mockTransport) injectResponse(resp *JSONRPCResponse) {
	data, _ := json.Marshal(resp)
	mt.readData <- data
}

// ---------------------------------------------------------------------------
// Client initialization
// ---------------------------------------------------------------------------

func TestNewClient(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if client.Capabilities().HoverProvider {
		t.Error("new client should not have capabilities yet")
	}

	if client.RootURI() != "" {
		t.Error("new client should have empty root URI")
	}
}

func TestClient_Close(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)

	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !transport.closed {
		t.Error("transport should be closed after client Close")
	}
}

func TestClient_Close_Idempotent(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)

	if err := client.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// JSON-RPC message formatting
// ---------------------------------------------------------------------------

func TestNotify_MessageFormat(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)
	ctx := context.Background()

	err := client.Notify(ctx, "textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        "file:///test.go",
			LanguageID: "go",
			Version:    1,
			Text:       "package main",
		},
	})
	if err != nil {
		t.Fatalf("Notify failed: %v", err)
	}

	writes := transport.getWrites()
	if len(writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(writes))
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(writes[0], &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if req.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc version %q, got %q", "2.0", req.JSONRPC)
	}

	if req.Method != "textDocument/didOpen" {
		t.Errorf("expected method %q, got %q", "textDocument/didOpen", req.Method)
	}

	if req.ID != nil {
		t.Error("notification should not have an ID")
	}

	// Verify params
	var params DidOpenTextDocumentParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("failed to unmarshal params: %v", err)
	}
	if params.TextDocument.URI != "file:///test.go" {
		t.Errorf("expected URI %q, got %q", "file:///test.go", params.TextDocument.URI)
	}
}

func TestNotify_NilParams(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)
	ctx := context.Background()

	err := client.Notify(ctx, "exit", nil)
	if err != nil {
		t.Fatalf("Notify with nil params failed: %v", err)
	}

	writes := transport.getWrites()
	if len(writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(writes))
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(writes[0], &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if req.Method != "exit" {
		t.Errorf("expected method %q, got %q", "exit", req.Method)
	}
}

func TestCall_RequestFormat(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start the read loop so it can dispatch responses
	client.Start(ctx)

	// Inject a fake response for the initialize call
	go func() {
		time.Sleep(50 * time.Millisecond)
		transport.injectResponse(&JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      float64(1),
			Result:  json.RawMessage(`{"capabilities":{"hoverProvider":true,"definitionProvider":true}}`),
		})
	}()

	result, err := client.Call(ctx, "initialize", InitializeParams{
		ProcessID: 12345,
		RootURI:   "file:///workspace",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	writes := transport.getWrites()
	if len(writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(writes))
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(writes[0], &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	if req.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc version %q, got %q", "2.0", req.JSONRPC)
	}

	if req.Method != "initialize" {
		t.Errorf("expected method %q, got %q", "initialize", req.Method)
	}

	if req.ID == nil {
		t.Fatal("request should have an ID")
	}

	// ID should be numeric
	id, ok := req.ID.(float64)
	if !ok {
		t.Fatalf("expected numeric ID, got %T", req.ID)
	}
	if id != 1 {
		t.Errorf("expected ID 1, got %v", id)
	}

	// Verify params
	var params InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("failed to unmarshal params: %v", err)
	}
	if params.ProcessID != 12345 {
		t.Errorf("expected ProcessID 12345, got %d", params.ProcessID)
	}
	if params.RootURI != "file:///workspace" {
		t.Errorf("expected RootURI %q, got %q", "file:///workspace", params.RootURI)
	}

	// Verify result
	if string(result) != `{"capabilities":{"hoverProvider":true,"definitionProvider":true}}` {
		t.Errorf("unexpected result: %s", string(result))
	}
}

func TestCall_ErrorResponse(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start the read loop so it can dispatch responses
	client.Start(ctx)

	go func() {
		time.Sleep(50 * time.Millisecond)
		transport.injectResponse(&JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      float64(1),
			Error: &JSONRPCError{
				Code:    ErrorCodeMethodNotFound,
				Message: "method not found",
			},
		})
	}()

	_, err := client.Call(ctx, "unknown/method", nil)
	if err == nil {
		t.Fatal("expected error for error response")
	}

	if err.Error() != "method not found" {
		t.Errorf("expected error message %q, got %q", "method not found", err.Error())
	}
}

func TestCall_ContextCancelled(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Call(ctx, "test/method", nil)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

func TestCall_WriteError(t *testing.T) {
	transport := newMockTransport()
	transport.writeErr = fmt.Errorf("write failed")
	client := NewClient(transport)
	ctx := context.Background()

	_, err := client.Call(ctx, "test/method", nil)
	if err == nil {
		t.Fatal("expected error when write fails")
	}
}

// ---------------------------------------------------------------------------
// Multiple sequential calls
// ---------------------------------------------------------------------------

func TestCall_SequentialIDs(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)

	// Start the read loop so it can dispatch responses
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client.Start(ctx)

	// Helper to do a call with immediate response
	doCall := func(t *testing.T, id float64) {
		t.Helper()
		callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)

		go func() {
			time.Sleep(20 * time.Millisecond)
			transport.injectResponse(&JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      id,
				Result:  json.RawMessage(`{}`),
			})
		}()

		_, err := client.Call(callCtx, "test/method", nil)
		callCancel()
		if err != nil {
			t.Fatalf("Call %d failed: %v", int(id), err)
		}
	}

	doCall(t, 1)
	doCall(t, 2)
	doCall(t, 3)

	writes := transport.getWrites()
	if len(writes) != 3 {
		t.Fatalf("expected 3 writes, got %d", len(writes))
	}

	// Verify IDs are sequential
	ids := make([]float64, len(writes))
	for i, w := range writes {
		var req JSONRPCRequest
		if err := json.Unmarshal(w, &req); err != nil {
			t.Fatalf("failed to unmarshal request %d: %v", i, err)
		}
		id, ok := req.ID.(float64)
		if !ok {
			t.Fatalf("request %d: expected numeric ID", i)
		}
		ids[i] = id
	}

	if ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Errorf("expected sequential IDs [1,2,3], got %v", ids)
	}
}

// ---------------------------------------------------------------------------
// Notification handler
// ---------------------------------------------------------------------------

func TestOnNotification(t *testing.T) {
	transport := newMockTransport()
	client := NewClient(transport)

	received := make(chan string, 1)
	client.OnNotification("textDocument/publishDiagnostics", func(method string, params json.RawMessage) {
		received <- method
	})

	// Start the client's read loop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.Start(ctx)

	// Inject a notification
	notif := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params:  json.RawMessage(`{"uri":"file:///test.go","diagnostics":[]}`),
	}
	data, _ := json.Marshal(notif)
	transport.readData <- data

	select {
	case method := <-received:
		if method != "textDocument/publishDiagnostics" {
			t.Errorf("expected method %q, got %q", "textDocument/publishDiagnostics", method)
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for notification")
	}
}

// ---------------------------------------------------------------------------
// Protocol types
// ---------------------------------------------------------------------------

func TestDiagnosticSeverity_String(t *testing.T) {
	tests := []struct {
		sev  DiagnosticSeverity
		want string
	}{
		{DiagnosticSeverityError, "error"},
		{DiagnosticSeverityWarning, "warning"},
		{DiagnosticSeverityInformation, "information"},
		{DiagnosticSeverityHint, "hint"},
		{DiagnosticSeverity(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.sev.String()
			if got != tt.want {
				t.Errorf("DiagnosticSeverity(%d).String() = %q, want %q", tt.sev, got, tt.want)
			}
		})
	}
}

func TestJSONRPCError_Error(t *testing.T) {
	e := &JSONRPCError{
		Code:    ErrorCodeInternalError,
		Message: "internal error",
	}
	if e.Error() != "internal error" {
		t.Errorf("JSONRPCError.Error() = %q, want %q", e.Error(), "internal error")
	}
}

func TestPosition_JSON(t *testing.T) {
	p := Position{Line: 10, Character: 5}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal Position: %v", err)
	}

	var p2 Position
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("failed to unmarshal Position: %v", err)
	}
	if p2.Line != 10 || p2.Character != 5 {
		t.Errorf("Position roundtrip failed: got %+v", p2)
	}
}

func TestInitializeParams_JSON(t *testing.T) {
	params := InitializeParams{
		ProcessID: 42,
		RootURI:   "file:///project",
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Synchronization: TextDocumentSyncClientCapabilities{
					DidSave: true,
				},
			},
			Workspace: WorkspaceClientCapabilities{
				WorkspaceFolders: true,
			},
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("failed to marshal InitializeParams: %v", err)
	}

	// Verify key fields are present
	s := string(data)
	if !contains(s, `"processId":42`) {
		t.Errorf("missing processId in JSON: %s", s)
	}
	if !contains(s, `"rootUri":"file:///project"`) {
		t.Errorf("missing rootUri in JSON: %s", s)
	}
	if !contains(s, `"didSave":true`) {
		t.Errorf("missing didSave in JSON: %s", s)
	}
	if !contains(s, `"workspaceFolders":true`) {
		t.Errorf("missing workspaceFolders in JSON: %s", s)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
