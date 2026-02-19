package tui

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tui/types"
)

func TestNewRPCClient(t *testing.T) {
	client := NewRPCClient("/tmp/test.sock")
	if client == nil {
		t.Fatal("NewRPCClient returned nil")
	}
	if client.socketPath != "/tmp/test.sock" {
		t.Errorf("expected socket path /tmp/test.sock, got %s", client.socketPath)
	}
	if client.timeout != 120*time.Second {
		t.Errorf("expected timeout 120s, got %v", client.timeout)
	}
	// Verify atomic bool starts as false
	if client.connected.Load() {
		t.Error("expected connected atomic to be false initially")
	}
}

func TestRPCClientConnectError(t *testing.T) {
	client := NewRPCClient("/nonexistent/socket.sock")
	err := client.Connect()
	if err == nil {
		t.Error("expected error connecting to nonexistent socket")
	}
}

func TestRPCClientIsConnected(t *testing.T) {
	client := NewRPCClient("/tmp/test.sock")
	if client.IsConnected() {
		t.Error("expected IsConnected to return false before connect")
	}
}

func TestRPCClientIsConnectedLockFree(t *testing.T) {
	// Verify IsConnected doesn't block even when locks are held
	// Use shorter socket path to avoid macOS path length limits
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("meept-test-%d.sock", time.Now().UnixNano()))
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()
	defer os.Remove(sockPath)

	// Start a simple accept loop
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	client := NewRPCClient(sockPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Hold the connMu lock (simulating connect/disconnect operation)
	client.connMu.Lock()

	// IsConnected should still work (lock-free)
	done := make(chan bool, 1)
	go func() {
		_ = client.IsConnected() // Should not block
		done <- true
	}()

	select {
	case <-done:
		// Good - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("IsConnected blocked when connMu was held")
	}

	client.connMu.Unlock()
}

func TestRPCClientCallNotConnected(t *testing.T) {
	client := NewRPCClient("/tmp/test.sock")
	_, err := client.Call("test", nil)
	if err == nil {
		t.Error("expected error calling without connection")
	}
}

func TestRPCClientSetTimeout(t *testing.T) {
	client := NewRPCClient("/tmp/test.sock")
	client.SetTimeout(5 * time.Second)
	if client.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", client.timeout)
	}
}

// mockServer creates a mock JSON-RPC server for testing.
type mockServer struct {
	listener net.Listener
	sockPath string
	handler  func(method string, params json.RawMessage) (any, error)
}

func newMockServer(t *testing.T) *mockServer {
	t.Helper()

	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create mock server: %v", err)
	}

	server := &mockServer{
		listener: listener,
		sockPath: sockPath,
		handler: func(method string, params json.RawMessage) (any, error) {
			return map[string]string{"status": "ok"}, nil
		},
	}

	go server.serve()
	return server
}

func (s *mockServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *mockServer) handleConn(conn net.Conn) {
	defer conn.Close()

	reader := NewFrameReader(conn)
	writer := NewFrameWriter(conn)

	for {
		req, err := reader.ReadRequest()
		if err != nil {
			return
		}

		result, handlerErr := s.handler(req.Method, req.Params)

		var resp Response
		if handlerErr != nil {
			resp = Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &RPCError{
					Code:    -32603,
					Message: handlerErr.Error(),
				},
			}
		} else {
			resultData, _ := json.Marshal(result)
			resp = Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  resultData,
			}
		}

		respData, _ := json.Marshal(resp)
		writer.WriteFrame(respData)
	}
}

func (s *mockServer) Close() {
	s.listener.Close()
	os.Remove(s.sockPath)
}

// Helper types for mock server
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// FrameReader/Writer helpers for testing
type FrameReader struct {
	conn net.Conn
}

func NewFrameReader(conn net.Conn) *FrameReader {
	return &FrameReader{conn: conn}
}

func (r *FrameReader) ReadRequest() (*Request, error) {
	// Read length line
	var lengthStr string
	buf := make([]byte, 1)
	for {
		_, err := r.conn.Read(buf)
		if err != nil {
			return nil, err
		}
		if buf[0] == '\n' {
			break
		}
		lengthStr += string(buf)
	}

	var length int
	fmt.Sscanf(lengthStr, "%d", &length)

	// Read payload
	payload := make([]byte, length)
	_, err := r.conn.Read(payload)
	if err != nil {
		return nil, err
	}

	var req Request
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

type FrameWriter struct {
	conn net.Conn
}

func NewFrameWriter(conn net.Conn) *FrameWriter {
	return &FrameWriter{conn: conn}
}

func (w *FrameWriter) WriteFrame(payload []byte) error {
	header := fmt.Sprintf("%d\n", len(payload))
	if _, err := w.conn.Write([]byte(header)); err != nil {
		return err
	}
	_, err := w.conn.Write(payload)
	return err
}

func TestRPCClientWithMockServer(t *testing.T) {
	server := newMockServer(t)
	defer server.Close()

	client := NewRPCClient(server.sockPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	if !client.IsConnected() {
		t.Error("expected IsConnected to return true after connect")
	}

	result, err := client.Call("test", nil)
	if err != nil {
		t.Fatalf("failed to call: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp["status"])
	}
}

func TestRPCClientChat(t *testing.T) {
	server := newMockServer(t)
	server.handler = func(method string, params json.RawMessage) (any, error) {
		if method != "chat" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		return map[string]string{"reply": "Hello!"}, nil
	}
	defer server.Close()

	client := NewRPCClient(server.sockPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	reply, err := client.Chat("Hello", "test-conv")
	if err != nil {
		t.Fatalf("failed to chat: %v", err)
	}

	if reply != "Hello!" {
		t.Errorf("expected reply 'Hello!', got '%s'", reply)
	}
}

func TestRPCClientStatus(t *testing.T) {
	server := newMockServer(t)
	server.handler = func(method string, params json.RawMessage) (any, error) {
		if method != "status" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		return map[string]any{
			"status":         "running",
			"uptime_seconds": 3600.0,
		}, nil
	}
	defer server.Close()

	client := NewRPCClient(server.sockPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if status.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", status.Status)
	}
	if status.UptimeSeconds != 3600.0 {
		t.Errorf("expected uptime 3600, got %f", status.UptimeSeconds)
	}
}

func TestMemoryQueryResponseGetItems(t *testing.T) {
	// Test Results field
	resp := &types.MemoryQueryResponse{
		Results: []types.MemoryItem{{ID: "1"}},
	}
	items := resp.GetItems()
	if len(items) != 1 || items[0].ID != "1" {
		t.Error("GetItems should return Results when populated")
	}

	// Test Items field
	resp = &types.MemoryQueryResponse{
		Items: []types.MemoryItem{{ID: "2"}},
	}
	items = resp.GetItems()
	if len(items) != 1 || items[0].ID != "2" {
		t.Error("GetItems should return Items when Results empty")
	}

	// Test Memories field
	resp = &types.MemoryQueryResponse{
		Memories: []types.MemoryItem{{ID: "3"}},
	}
	items = resp.GetItems()
	if len(items) != 1 || items[0].ID != "3" {
		t.Error("GetItems should return Memories when other fields empty")
	}
}

func TestMemoryItemGetType(t *testing.T) {
	// Test MemoryType field
	item := types.MemoryItem{MemoryType: "episodic"}
	if item.GetType() != "episodic" {
		t.Errorf("expected 'episodic', got '%s'", item.GetType())
	}

	// Test Type field fallback
	item = types.MemoryItem{Type: "task"}
	if item.GetType() != "task" {
		t.Errorf("expected 'task', got '%s'", item.GetType())
	}
}
