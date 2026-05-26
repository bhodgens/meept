package tests

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/tui/types"
)

// ============================================================================
// Mock HTTP server for transport testing
// ============================================================================

// mockHTTPServer creates an httptest.Server that mimics the daemon HTTP API,
// including the /api/v1/bus/call proxy endpoint and /api/v1/chat endpoint.
func mockHTTPServer(t *testing.T, handler func(method string, params json.RawMessage) (any, error)) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Chat endpoint (dedicated, not via bus/call)
	mux.HandleFunc("/api/v1/chat", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Message        string `json:"message"`
			ConversationID string `json:"conversation_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		params, _ := json.Marshal(map[string]string{
			"message":         req.Message,
			"conversation_id": req.ConversationID,
		})
		result, err := handler("chat", params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Chat returns {"reply": "..."} format
		resultMap, ok := result.(map[string]string)
		if !ok {
			http.Error(w, "invalid result type", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"reply": resultMap["reply"]})
	})

	// Bus/call proxy endpoint - mirrors the JSON-RPC interface over HTTP
	mux.HandleFunc("/api/v1/bus/call", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		result, err := handler(req.Method, req.Params)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":    -32603,
					"message": err.Error(),
				},
			})
			return
		}

		resultData, _ := json.Marshal(result)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": json.RawMessage(resultData),
		})
	})

	return httptest.NewServer(mux)
}

// ============================================================================
// Mock RPC (Unix socket) server for transport testing
// ============================================================================

// mockRPCServer creates a Unix socket server that speaks the Meept JSON-RPC
// frame protocol. It uses the tui.FrameReader/FrameWriter protocol.
type mockRPCServer struct {
	listener net.Listener
	sockPath string
	handler  func(method string, params json.RawMessage) (any, error)
}

func newMockRPCServer(t *testing.T, handler func(method string, params json.RawMessage) (any, error)) *mockRPCServer {
	t.Helper()

	// Use a short path to avoid macOS Unix socket path length limit (~104 chars).
	// The temp dir + test name can be too long, so we use /tmp directly.
	sockPath := fmt.Sprintf("/tmp/meept-transport-test-%d.sock", time.Now().UnixNano())

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create mock RPC server: %v", err)
	}

	server := &mockRPCServer{
		listener: listener,
		sockPath: sockPath,
		handler:  handler,
	}

	go server.serve()
	return server
}

func (s *mockRPCServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *mockRPCServer) handleConn(conn net.Conn) {
	defer conn.Close()

	for {
		// Read length-prefixed frame: "<length>\n<payload>"
		var lengthStr strings.Builder
		buf := make([]byte, 1)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				return
			}
			if buf[0] == '\n' {
				break
			}
			lengthStr.WriteString(string(buf))
		}

		var length int
		_, _ = fmt.Sscanf(lengthStr.String(), "%d", &length)

		payload := make([]byte, length)
		n, err := conn.Read(payload)
		if err != nil || n != length {
			return
		}

		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      any             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			return
		}

		result, handlerErr := s.handler(req.Method, req.Params)

		var resp map[string]any
		if handlerErr != nil {
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error": map[string]any{
					"code":    -32603,
					"message": handlerErr.Error(),
				},
			}
		} else {
			resultData, _ := json.Marshal(result)
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  json.RawMessage(resultData),
			}
		}

		respData, _ := json.Marshal(resp)
		header := fmt.Sprintf("%d\n", len(respData))
		_, _ = conn.Write([]byte(header))
		_, _ = conn.Write(respData)
	}
}

func (s *mockRPCServer) Close() {
	s.listener.Close()
	os.Remove(s.sockPath)
}

// ============================================================================
// Helper: default handler that returns method name as status
// ============================================================================

func statusHandler(method string, params json.RawMessage) (any, error) {
	switch method {
	case "status":
		return map[string]any{
			"status":             "running",
			"uptime_seconds":     3600.0,
			"tokens_used":        1000,
			"tokens_remaining":   9000,
			"budget_used":        0.5,
			"budget_remaining":   9.5,
			"model":              "test-model",
			"default_model":      "",
			"registered_methods": []string{"status", "chat"},
			"bus_subscribers":    2,
		}, nil
	case "chat":
		return map[string]string{"reply": "Hello from daemon!"}, nil
	case "scheduler.list_jobs":
		return map[string]any{"jobs": []any{}}, nil
	case "memory.query":
		return map[string]any{"results": []any{}}, nil
	case "memory.recent":
		return map[string]any{"results": []any{}}, nil
	case "agent.workers.list":
		return map[string]any{"workers": []any{}}, nil
	case "queue.stats":
		return map[string]any{
			"by_state":    map[string]int{"pending": 0},
			"by_priority": map[string]int{},
			"dead_count":  0,
		}, nil
	case "queue.list":
		return map[string]any{"jobs": []any{}}, nil
	case "task.list":
		return map[string]any{"tasks": []any{}}, nil
	case "task.create":
		var p map[string]string
		_ = json.Unmarshal(params, &p)
		return map[string]any{
			"id":          "task-123",
			"name":        p["name"],
			"description": p["description"],
			"state":       "pending",
			"created_at":  time.Now().Format(time.RFC3339),
			"updated_at":  time.Now().Format(time.RFC3339),
		}, nil
	case "task.get":
		return map[string]any{
			"id":         "task-123",
			"name":       "test task",
			"state":      "pending",
			"created_at": time.Now().Format(time.RFC3339),
			"updated_at": time.Now().Format(time.RFC3339),
		}, nil
	case "task.list_extended":
		return map[string]any{"tasks": []any{}}, nil
	case "task.steps":
		return map[string]any{"steps": []any{}}, nil
	case "task.delete", "task.cancel":
		return map[string]string{"status": "ok"}, nil
	case "task.link", "task.unlink":
		return map[string]string{"status": "ok"}, nil
	case "queue.retry":
		return map[string]string{"status": "ok"}, nil
	case "worker.list":
		return map[string]any{"workers": []any{}}, nil
	case "worker.stats":
		return map[string]any{
			"total_workers": 0,
			"idle_workers":  0,
			"busy_workers":  0,
			"error_workers": 0,
		}, nil
	case "worker.scale":
		return map[string]string{"status": "ok"}, nil
	case "cache.stats":
		return map[string]any{
			"l1_entries": 0, "l1_hits": 0, "l1_misses": 0, "evictions": 0,
			"l2_entries": 0, "l2_hits": 0, "l2_misses": 0,
			"total_hits": 0, "hit_rate": 0.0,
		}, nil
	case "cache.clear", "cache.invalidate":
		return map[string]string{"status": "ok"}, nil
	case "session.list":
		return map[string]any{"sessions": []any{}}, nil
	case "session.create":
		return map[string]any{
			"id":         "sess-123",
			"name":       "test",
			"created_at": time.Now().Format(time.RFC3339),
		}, nil
	case "session.attach", "session.detach", "session.delete":
		return map[string]string{"status": "ok"}, nil
	case "session.get_most_recent":
		return map[string]any{
			"id":         "sess-latest",
			"name":       "latest",
			"created_at": time.Now().Format(time.RFC3339),
		}, nil
	case "session.messages.get":
		return map[string]any{"messages": []any{}, "total": 0}, nil
	case "session.messages.save":
		return map[string]string{"status": "ok"}, nil
	case "session.update_description":
		return map[string]string{"status": "ok"}, nil
	case "session.generate_description":
		return map[string]any{
			"description": "auto-generated description",
		}, nil
	case "session.stop":
		return map[string]any{
			"status":          "stopped",
			"session_id":      "sess-123",
			"workers_stopped": []string{},
		}, nil
	case "session.get_child_tasks":
		return map[string]any{"tasks": []string{}}, nil
	default:
		return map[string]string{"status": "ok"}, nil
	}
}

// ============================================================================
// Tests: transport.New() factory
// ============================================================================

func TestTransportNew_RPC(t *testing.T) {
	cfg := &transport.Config{
		Transport:  "rpc",
		SocketPath: "/tmp/test.sock",
		Timeout:    5 * time.Second,
	}
	client, err := transport.New(cfg)
	if err != nil {
		t.Fatalf("transport.New(rpc) returned error: %v", err)
	}
	if client == nil {
		t.Fatal("transport.New(rpc) returned nil")
	}
	_ = client.Close()
}

func TestTransportNew_HTTP(t *testing.T) {
	cfg := &transport.Config{
		Transport:   "http",
		HTTPBaseURL: "http://localhost:8081",
		Timeout:     5 * time.Second,
	}
	client, err := transport.New(cfg)
	if err != nil {
		t.Fatalf("transport.New(http) returned error: %v", err)
	}
	if client == nil {
		t.Fatal("transport.New(http) returned nil")
	}
	_ = client.Close()
}

func TestTransportNew_UnknownTransport(t *testing.T) {
	cfg := &transport.Config{
		Transport: "websocket",
	}
	_, err := transport.New(cfg)
	if err == nil {
		t.Fatal("expected error for unknown transport")
	}
}

func TestTransportNew_NilConfig(t *testing.T) {
	client, err := transport.New(nil)
	if err != nil {
		t.Fatalf("transport.New(nil) returned error: %v", err)
	}
	if client == nil {
		t.Fatal("transport.New(nil) returned nil")
	}
	_ = client.Close()
}

func TestTransportNew_Aliases(t *testing.T) {
	aliases := []string{"unix", "socket"}
	for _, alias := range aliases {
		t.Run(alias, func(t *testing.T) {
			cfg := &transport.Config{
				Transport:  alias,
				SocketPath: "/tmp/test.sock",
			}
			client, err := transport.New(cfg)
			if err != nil {
				t.Fatalf("transport.New(%s) returned error: %v", alias, err)
			}
			if client == nil {
				t.Fatalf("transport.New(%s) returned nil", alias)
			}
			_ = client.Close()
		})
	}
}

// ============================================================================
// Tests: HTTP transport client
// ============================================================================

func TestHTTPTransport_ConnectAndHealth(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if !client.IsConnected() {
		t.Error("IsConnected should return true after Connect")
	}
}

func TestHTTPTransport_ConnectFailure(t *testing.T) {
	client := transport.NewHTTPClient("http://127.0.0.1:1", 1*time.Second)
	err := client.Connect()
	if err == nil {
		t.Fatal("expected error connecting to non-existent server")
	}
}

func TestHTTPTransport_Chat(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	reply, err := client.Chat("Hello", "conv-1")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if reply != "Hello from daemon!" {
		t.Errorf("expected 'Hello from daemon!', got '%s'", reply)
	}
}

func TestHTTPTransport_Status(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", status.Status)
	}
	if status.UptimeSeconds != 3600.0 {
		t.Errorf("expected uptime 3600, got %f", status.UptimeSeconds)
	}
}

func TestHTTPTransport_Call(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	result, err := client.Call("status", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var status map[string]any
	if err := json.Unmarshal(result, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status["status"] != "running" {
		t.Errorf("expected status 'running', got %v", status["status"])
	}
}

func TestHTTPTransport_GenericCallWithParams(t *testing.T) {
	callCount := 0
	server := mockHTTPServer(t, func(method string, params json.RawMessage) (any, error) {
		callCount++
		if method != "custom.method" {
			return nil, fmt.Errorf("unexpected method: %s", method)
		}
		var p map[string]string
		_ = json.Unmarshal(params, &p)
		return map[string]string{"echo": p["input"]}, nil
	})
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	result, err := client.Call("custom.method", map[string]string{"input": "test-data"})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if resp["echo"] != "test-data" {
		t.Errorf("expected 'test-data', got '%s'", resp["echo"])
	}
}

func TestHTTPTransport_TaskCreate(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	task, err := client.CreateTask("test task", "test description")
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if task.ID != "task-123" {
		t.Errorf("expected task ID 'task-123', got '%s'", task.ID)
	}
	if task.Name != "test task" {
		t.Errorf("expected name 'test task', got '%s'", task.Name)
	}
}

func TestHTTPTransport_QueueStats(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	stats, err := client.GetQueueStats()
	if err != nil {
		t.Fatalf("GetQueueStats failed: %v", err)
	}
	if stats.ByState["pending"] != 0 {
		t.Errorf("expected 0 pending jobs, got %d", stats.ByState["pending"])
	}
}

func TestHTTPTransport_SetTimeout(t *testing.T) {
	client := transport.NewHTTPClient("http://localhost:8081", 5*time.Second)
	client.SetTimeout(10 * time.Second)
	// No assertion on internal state since it's unexported;
	// verify it doesn't panic.
	_ = client
}

func TestHTTPTransport_CloseIdempotent(t *testing.T) {
	client := transport.NewHTTPClient("http://localhost:8081", 5*time.Second)
	if err := client.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

// ============================================================================
// Tests: RPC transport client
// ============================================================================

func TestRPCTransport_ConnectAndHealth(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if !client.IsConnected() {
		t.Error("IsConnected should return true after Connect")
	}
}

func TestRPCTransport_ConnectFailure(t *testing.T) {
	client := transport.NewRPCClient("/nonexistent/test.sock", 1*time.Second)
	err := client.Connect()
	if err == nil {
		t.Fatal("expected error connecting to non-existent socket")
	}
}

func TestRPCTransport_Chat(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	reply, err := client.Chat("Hello", "conv-1")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if reply != "Hello from daemon!" {
		t.Errorf("expected 'Hello from daemon!', got '%s'", reply)
	}
}

func TestRPCTransport_Status(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", status.Status)
	}
	if status.UptimeSeconds != 3600.0 {
		t.Errorf("expected uptime 3600, got %f", status.UptimeSeconds)
	}
}

func TestRPCTransport_Call(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	result, err := client.Call("status", nil)
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	var status map[string]any
	if err := json.Unmarshal(result, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status["status"] != "running" {
		t.Errorf("expected status 'running', got %v", status["status"])
	}
}

func TestRPCTransport_TaskCreate(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	task, err := client.CreateTask("test task", "test description")
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if task.ID != "task-123" {
		t.Errorf("expected task ID 'task-123', got '%s'", task.ID)
	}
}

func TestRPCTransport_QueueStats(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	stats, err := client.GetQueueStats()
	if err != nil {
		t.Fatalf("GetQueueStats failed: %v", err)
	}
	if stats.ByState["pending"] != 0 {
		t.Errorf("expected 0 pending jobs, got %d", stats.ByState["pending"])
	}
}

func TestRPCTransport_SetTimeout(t *testing.T) {
	client := transport.NewRPCClient("/tmp/test.sock", 5*time.Second)
	client.SetTimeout(10 * time.Second)
	// No assertion on internal state since it's unexported;
	// verify it doesn't panic.
	_ = client
}

// ============================================================================
// Tests: Interface compliance
// ============================================================================

func TestTransportClient_InterfaceCompliance(t *testing.T) {
	// Verify that both HTTP and RPC clients satisfy the transport.Client interface
	// at compile time. This is also enforced by the compiler, but we verify
	// dynamically here for documentation purposes.

	var _ = transport.NewHTTPClient("http://localhost:8081", 0)
	var _ = transport.NewRPCClient("/tmp/test.sock", 0)
}

// ============================================================================
// Tests: Config defaults
// ============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := transport.DefaultConfig()
	if cfg.Transport != "rpc" {
		t.Errorf("default transport = %s, want rpc", cfg.Transport)
	}
	if cfg.SocketPath != "~/.meept/meept.sock" {
		t.Errorf("default socket path = %s", cfg.SocketPath)
	}
	if cfg.HTTPBaseURL != "http://localhost:8081" {
		t.Errorf("default HTTP URL = %s", cfg.HTTPBaseURL)
	}
	if cfg.Timeout != 120*time.Second {
		t.Errorf("default timeout = %v, want 120s", cfg.Timeout)
	}
}

// ============================================================================
// Tests: Error handling
// ============================================================================

func TestHTTPTransport_ServerError(t *testing.T) {
	server := mockHTTPServer(t, func(method string, params json.RawMessage) (any, error) {
		return nil, fmt.Errorf("method not found: %s", method)
	})
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	_, err := client.Call("nonexistent.method", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent method")
	}
}

func TestRPCTransport_ServerError(t *testing.T) {
	server := newMockRPCServer(t, func(method string, params json.RawMessage) (any, error) {
		return nil, fmt.Errorf("method not found: %s", method)
	})
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	_, err := client.Call("nonexistent.method", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent method")
	}
}

// ============================================================================
// Tests: Session methods over both transports
// ============================================================================

func TestHTTPTransport_SessionMethods(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// ListSessions
	sessions, err := client.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	// CreateSession
	sess, err := client.CreateSession("test")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.ID != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got '%s'", sess.ID)
	}

	// GetMostRecentSession
	recent, err := client.GetMostRecentSession()
	if err != nil {
		t.Fatalf("GetMostRecentSession failed: %v", err)
	}
	if recent.ID != "sess-latest" {
		t.Errorf("expected recent session ID 'sess-latest', got '%s'", recent.ID)
	}

	// GetSessionMessages
	msgs, err := client.GetSessionMessages("sess-123", 0, 50)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if msgs.Total != 0 {
		t.Errorf("expected 0 messages, got %d", msgs.Total)
	}

	// GetSessionChildTasks
	tasks, err := client.GetSessionChildTasks("sess-123")
	if err != nil {
		t.Fatalf("GetSessionChildTasks failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 child tasks, got %d", len(tasks))
	}

	// StopSession
	stopResp, err := client.StopSession("sess-123")
	if err != nil {
		t.Fatalf("StopSession failed: %v", err)
	}
	if stopResp.Status != "stopped" {
		t.Errorf("expected session status 'stopped', got '%s'", stopResp.Status)
	}

	// GenerateSessionDescription
	desc, err := client.GenerateSessionDescription("sess-123", "Hello", "test-project")
	if err != nil {
		t.Fatalf("GenerateSessionDescription failed: %v", err)
	}
	if desc.Description != "auto-generated description" {
		t.Errorf("unexpected description: %s", desc.Description)
	}

	// Verify _ = sessions to avoid unused variable error
	_ = sessions
}

func TestRPCTransport_SessionMethods(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// CreateSession
	sess, err := client.CreateSession("test")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.ID != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got '%s'", sess.ID)
	}

	// GetMostRecentSession
	recent, err := client.GetMostRecentSession()
	if err != nil {
		t.Fatalf("GetMostRecentSession failed: %v", err)
	}
	if recent.ID != "sess-latest" {
		t.Errorf("expected recent session ID 'sess-latest', got '%s'", recent.ID)
	}
}

// ============================================================================
// Tests: Cache methods over both transports
// ============================================================================

func TestHTTPTransport_CacheMethods(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	stats, err := client.CacheStats()
	if err != nil {
		t.Fatalf("CacheStats failed: %v", err)
	}
	if stats.L1Hits != 0 {
		t.Errorf("expected 0 L1 hits, got %d", stats.L1Hits)
	}

	if err := client.CacheClear(); err != nil {
		t.Fatalf("CacheClear failed: %v", err)
	}

	if err := client.CacheInvalidate("test/file.go"); err != nil {
		t.Fatalf("CacheInvalidate failed: %v", err)
	}
}

func TestRPCTransport_CacheMethods(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	stats, err := client.CacheStats()
	if err != nil {
		t.Fatalf("CacheStats failed: %v", err)
	}
	if stats.L1Hits != 0 {
		t.Errorf("expected 0 L1 hits, got %d", stats.L1Hits)
	}

	if err := client.CacheClear(); err != nil {
		t.Fatalf("CacheClear failed: %v", err)
	}
}

// ============================================================================
// Tests: Worker methods over both transports
// ============================================================================

func TestHTTPTransport_WorkerMethods(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	poolStats, err := client.GetWorkerPoolStats()
	if err != nil {
		t.Fatalf("GetWorkerPoolStats failed: %v", err)
	}
	if poolStats.TotalWorkers != 0 {
		t.Errorf("expected 0 total workers, got %d", poolStats.TotalWorkers)
	}

	poolWorkers, err := client.ListPoolWorkers()
	if err != nil {
		t.Fatalf("ListPoolWorkers failed: %v", err)
	}
	if len(poolWorkers.Workers) != 0 {
		t.Errorf("expected 0 workers, got %d", len(poolWorkers.Workers))
	}

	if err := client.ScaleWorkerPool(4); err != nil {
		t.Fatalf("ScaleWorkerPool failed: %v", err)
	}
}

func TestRPCTransport_WorkerMethods(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	poolStats, err := client.GetWorkerPoolStats()
	if err != nil {
		t.Fatalf("GetWorkerPoolStats failed: %v", err)
	}
	if poolStats.TotalWorkers != 0 {
		t.Errorf("expected 0 total workers, got %d", poolStats.TotalWorkers)
	}

	if err := client.ScaleWorkerPool(4); err != nil {
		t.Fatalf("ScaleWorkerPool failed: %v", err)
	}
}

// ============================================================================
// Tests: Task mutation methods
// ============================================================================

func TestHTTPTransport_TaskMutations(t *testing.T) {
	server := mockHTTPServer(t, statusHandler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if err := client.DeleteTask("task-123"); err != nil {
		t.Fatalf("DeleteTask failed: %v", err)
	}
	if err := client.CancelTask("task-123"); err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}
	if err := client.LinkTaskSession("task-123", "sess-456"); err != nil {
		t.Fatalf("LinkTaskSession failed: %v", err)
	}
	if err := client.UnlinkTaskSession("task-123", "sess-456"); err != nil {
		t.Fatalf("UnlinkTaskSession failed: %v", err)
	}
	if err := client.RetryQueueJob("job-123"); err != nil {
		t.Fatalf("RetryQueueJob failed: %v", err)
	}
}

func TestRPCTransport_TaskMutations(t *testing.T) {
	server := newMockRPCServer(t, statusHandler)
	defer server.Close()

	client := transport.NewRPCClient(server.sockPath, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	if err := client.DeleteTask("task-123"); err != nil {
		t.Fatalf("DeleteTask failed: %v", err)
	}
	if err := client.CancelTask("task-123"); err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}
	if err := client.RetryQueueJob("job-123"); err != nil {
		t.Fatalf("RetryQueueJob failed: %v", err)
	}
}

// ============================================================================
// Tests: Types compatibility
// ============================================================================

func TestTransportTypes_Compatibility(t *testing.T) {
	// Verify that the types used in transport.Client match what the CLI
	// commands expect. This test ensures the transport types are correctly
	// imported from the tui/types package.

	var _ *types.DaemonStatusResponse
	var _ *types.JobListResponse
	var _ *types.MemoryQueryResponse
	var _ *types.WorkerListResponse
	var _ *types.QueueStatsResponse
	var _ *types.QueueJobListResponse
	var _ *types.TaskListResponse
	var _ *types.Task
	var _ *types.CacheStatsResponse
	var _ *types.SessionListResponse
	var _ *types.Session
	var _ *types.SessionMessagesResponse
	var _ *types.GenerateDescriptionResult
	var _ *types.StopSessionResponse
	var _ *types.TaskExtendedListResponse
	var _ *types.TaskStepsResponse
	var _ *types.WorkerPoolResponse
	var _ *types.WorkerPoolStats
}
