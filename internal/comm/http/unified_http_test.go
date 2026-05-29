package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	gohttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/models"
	"golang.org/x/net/websocket"
)

// startTestServer creates and starts a server on a random port, returning the
// base URL and a cancel function. The caller must defer cancel().
func startTestServer(t *testing.T, opts ...http.ServerOption) (baseURL string, cancel context.CancelFunc) {
	t.Helper()
	cfg := http.DefaultServerConfig()
	cfg.Addr = ":0"
	cfg.UseTLS = false      // Disable TLS for test servers (plain HTTP)
	cfg.RequireAuth = false // Disable auth for test servers (no API keys)

	srv := http.NewServer(cfg, nil, nil, nil, nil, nil, opts...)
	if srv == nil {
		t.Fatal("failed to create server")
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for listener to be ready
	for i := 0; i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", "127.0.0.1"+srv.Addr(), time.Second)
		if err == nil {
			conn.Close()
			break
		}
	}

	// Get actual address — format for URL (handle IPv6 [::]:port)
	addr := srv.Addr()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to parse server address %q: %v", addr, err)
	}
	if host == "" || host == "::" {
		host = "127.0.0.1"
	}
	baseURL = "http://" + host + ":" + port
	return baseURL, cancel
}

// TestUnifiedHTTPServer_WebSocketOption tests that WithWebSocket option registers handler.
func TestUnifiedHTTPServer_WebSocketOption(t *testing.T) {
	msgBus := bus.New(nil, nil)
	cfg := http.DefaultServerConfig()

	srv := http.NewServer(cfg, nil, nil, nil, nil, nil, http.WithWebSocket(msgBus, "/ws"))

	if srv == nil {
		t.Fatal("failed to create server with WebSocket option")
	}
}

// TestUnifiedHTTPServer_MCPOption tests that WithMCP option registers handler.
func TestUnifiedHTTPServer_MCPOption(t *testing.T) {
	svcRegistry := &services.ServiceRegistry{}
	cfg := http.DefaultServerConfig()

	srv := http.NewServer(cfg, nil, nil, nil, svcRegistry, nil, http.WithMCP(svcRegistry, "/mcp"))

	if srv == nil {
		t.Fatal("failed to create server with MCP option")
	}
}

// TestUnifiedHTTPServer_BothOptions tests enabling both WebSocket and MCP.
func TestUnifiedHTTPServer_BothOptions(t *testing.T) {
	msgBus := bus.New(nil, nil)
	sessionStore := session.NewMemoryStore(nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: sessionStore,
	}
	cfg := http.DefaultServerConfig()

	srv := http.NewServer(cfg, nil, nil, nil, svcRegistry, nil,
		http.WithWebSocket(msgBus, "/ws"),
		http.WithMCP(svcRegistry, "/mcp"),
	)

	if srv == nil {
		t.Fatal("failed to create server with both options")
	}
}

// TestUnifiedHTTPServer_ContextCancellation tests graceful shutdown.
func TestUnifiedHTTPServer_ContextCancellation(t *testing.T) {
	cfg := http.DefaultServerConfig()
	cfg.Addr = ":0"         // Let OS choose available port
	cfg.UseTLS = false      // Disable TLS for test server
	cfg.RequireAuth = false // Disable auth for test server

	srv := http.NewServer(cfg, nil, nil, nil, nil, nil)
	if srv == nil {
		t.Fatal("failed to create server")
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Logf("server shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not shutdown within timeout")
	}
}

// TestUnifiedHTTPServer_MCPRouteRegistration verifies MCP POST and SSE routes
// are registered when WithMCP is used (not returning 404).
func TestUnifiedHTTPServer_MCPRouteRegistration(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	// POST /mcp should respond (not 404)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	resp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("MCP POST request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == gohttp.StatusNotFound {
		t.Error("POST /mcp returned 404 — route not registered")
	}

	// GET /mcp/sse should respond (not 404) — use short context to avoid hanging
	sseCtx, sseCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer sseCancel()
	req, err := gohttp.NewRequestWithContext(sseCtx, "GET", baseURL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("failed to create SSE request: %v", err)
	}
	resp2, err := client.Do(req)
	if err != nil && !strings.Contains(err.Error(), "context deadline") {
		t.Logf("SSE request error (may be expected on timeout): %v", err)
	}
	if resp2 != nil {
		defer resp2.Body.Close()
		if resp2.StatusCode == gohttp.StatusNotFound {
			t.Error("GET /mcp/sse returned 404 — route not registered")
		}
		io.Copy(io.Discard, resp2.Body)
	}
}

// TestUnifiedHTTPServer_CustomWSPath verifies WebSocket uses the configured path.
func TestUnifiedHTTPServer_CustomWSPath(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	// Use custom WS path /custom-ws
	baseURL, cancel := startTestServer(t,
		http.WithWebSocket(msgBus, "/custom-ws"),
		http.WithMCP(svcRegistry, "/custom-mcp"),
	)
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	// GET /custom-ws should respond (not 404) — regular HTTP GET won't upgrade but shouldn't 404
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer wsCancel()
	req, err := gohttp.NewRequestWithContext(wsCtx, "GET", baseURL+"/custom-ws", nil)
	if err != nil {
		t.Fatalf("failed to create WS request: %v", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("WebSocket upgrade request error: %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode == gohttp.StatusNotFound {
			t.Error("GET /custom-ws returned 404 — custom WS path not registered")
		}
	}

	// GET /ws should return 404 since we configured /custom-ws
	req2, err2 := gohttp.NewRequestWithContext(wsCtx, "GET", baseURL+"/ws", nil)
	if err2 != nil {
		t.Logf("Default WS path request creation error: %v", err2)
	} else {
		resp2, err := client.Do(req2)
		if err != nil {
			t.Logf("Default WS path request error: %v", err)
		} else {
			defer resp2.Body.Close()
			if resp2.StatusCode != gohttp.StatusNotFound {
				t.Errorf("GET /ws expected 404, got %d — default path should not be registered", resp2.StatusCode)
			}
		}
	}
}

// mcpResponse is a helper to parse MCP JSON-RPC responses.
type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// mcpPost sends a MCP JSON-RPC request and returns the parsed response.
func mcpPost(t *testing.T, client *gohttp.Client, baseURL, method string, params map[string]any) mcpResponse {
	t.Helper()
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		reqBody["params"] = params
	}
	body, _ := json.Marshal(reqBody)
	resp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("MCP POST %s failed: %v", method, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var mcpResp mcpResponse
	if err := json.Unmarshal(data, &mcpResp); err != nil {
		t.Fatalf("failed to parse MCP response: %v\nbody: %s", err, string(data))
	}
	return mcpResp
}

// TestUnifiedHTTPServer_MCPToolsInitialize verifies the MCP initialize handshake.
func TestUnifiedHTTPServer_MCPToolsInitialize(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	resp := mcpPost(t, client, baseURL, "initialize", nil)
	if resp.Error != nil {
		t.Fatalf("initialize returned error: %s", resp.Error.Message)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse initialize result: %v", err)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocol version 2024-11-05, got %v", result["protocolVersion"])
	}
}

// TestUnifiedHTTPServer_MCPToolsSend verifies meept_send publishes to bus.
func TestUnifiedHTTPServer_MCPToolsSend(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	// Subscribe to chat.request to verify the message is published
	sub := msgBus.Subscribe("test-send", "chat.request")

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name": "meept_send",
		"arguments": map[string]any{
			"session_id": "test-session",
			"message":    "hello from test",
		},
	})
	if resp.Error != nil {
		t.Fatalf("meept_send returned error: %s", resp.Error.Message)
	}

	// Verify the message was published on the bus
	select {
	case msg := <-sub.Channel:
		if msg.Topic != "chat.request" {
			t.Errorf("expected topic chat.request, got %s", msg.Topic)
		}
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to parse payload: %v", err)
		}
		if payload["message"] != "hello from test" {
			t.Errorf("expected message 'hello from test', got %v", payload["message"])
		}
		if payload["conversation_id"] != "test-session" {
			t.Errorf("expected conversation_id 'test-session', got %v", payload["conversation_id"])
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for bus message from meept_send")
	}
}

// TestUnifiedHTTPServer_MCPToolStatus verifies meept_status returns daemon info.
func TestUnifiedHTTPServer_MCPToolStatus(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name":      "meept_status",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("meept_status returned error: %s", resp.Error.Message)
	}
}

// TestUnifiedHTTPServer_MCPToolsList verifies tools/list returns all 5 tools.
func TestUnifiedHTTPServer_MCPToolsList(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	resp := mcpPost(t, client, baseURL, "tools/list", nil)
	if resp.Error != nil {
		t.Fatalf("tools/list returned error: %s", resp.Error.Message)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse tools/list result: %v", err)
	}

	expectedTools := map[string]bool{
		"meept_sessions":        false,
		"meept_send":            false,
		"meept_events":          false,
		"meept_status":          false,
		"meept_session_history": false,
	}
	for _, tool := range result.Tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		}
	}
	for name, found := range expectedTools {
		if !found {
			t.Errorf("tool %s not found in tools/list response", name)
		}
	}
}

// TestUnifiedHTTPServer_MCPInvalidJSON verifies MCP returns 400 for invalid JSON.
func TestUnifiedHTTPServer_MCPInvalidJSON(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("MCP POST with invalid JSON failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

// TestUnifiedHTTPServer_MCPWrongContentType verifies MCP returns 400 for wrong Content-Type.
func TestUnifiedHTTPServer_MCPWrongContentType(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(baseURL+"/mcp", "text/plain", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if err != nil {
		t.Fatalf("MCP POST with wrong content type failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusBadRequest {
		t.Errorf("expected 400 for wrong content type, got %d", resp.StatusCode)
	}
}

// TestUnifiedHTTPServer_MCPUnknownMethod verifies MCP returns -32601 for unknown method.
func TestUnifiedHTTPServer_MCPUnknownMethod(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp := mcpPost(t, client, baseURL, "nonexistent/method", nil)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

// TestUnifiedHTTPServer_MCPNotificationInitialized verifies notifications/initialized returns 204.
func TestUnifiedHTTPServer_MCPNotificationInitialized(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	body := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	resp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("MCP POST notifications/initialized failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusNoContent {
		t.Errorf("expected 204 No Content for notification, got %d", resp.StatusCode)
	}
}

// TestUnifiedHTTPServer_MCPMissingToolName verifies tools/call returns error for missing tool name.
func TestUnifiedHTTPServer_MCPMissingToolName(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"params": map[string]any{},
	})
	if resp.Error == nil {
		t.Fatal("expected error for missing tool name, got nil")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("expected error code -32602, got %d", resp.Error.Code)
	}
}

// TestUnifiedHTTPServer_MCPUnknownTool verifies tools/call returns error for unknown tool.
func TestUnifiedHTTPServer_MCPUnknownTool(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name": "nonexistent_tool",
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected error code -32601, got %d", resp.Error.Code)
	}
}

// TestUnifiedHTTPServer_MCPSessionsTool verifies meept_sessions tool call.
func TestUnifiedHTTPServer_MCPSessionsTool(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name": "meept_sessions",
		"arguments": map[string]any{
			"action": "list",
		},
	})
	if resp.Error != nil {
		t.Fatalf("meept_sessions returned error: %s", resp.Error.Message)
	}
}

// TestUnifiedHTTPServer_MCPSessionHistoryTool verifies meept_session_history tool call.
func TestUnifiedHTTPServer_MCPSessionHistoryTool(t *testing.T) {
	msgBus := bus.New(nil, nil)
	store := session.NewMemoryStore(nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: store,
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name": "meept_session_history",
		"arguments": map[string]any{
			"session_id": "nonexistent",
		},
	})
	// Tool should not return a JSON-RPC error (may return empty results)
	if resp.Error != nil {
		t.Fatalf("meept_session_history returned error: %s", resp.Error.Message)
	}
}

// TestUnifiedHTTPServer_MCPEventsTool verifies meept_events tool call.
func TestUnifiedHTTPServer_MCPEventsTool(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	// Call without subscription_id — should return error in content
	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name":      "meept_events",
		"arguments": map[string]any{},
	})
	// The tool should not crash; it returns error info in the content
	if resp.Error != nil {
		// JSON-RPC level error is acceptable for missing required param
		t.Logf("meept_events returned JSON-RPC error: %s", resp.Error.Message)
	}
}

// TestUnifiedHTTPServer_SSEHeaders verifies SSE endpoint sets correct headers.
func TestUnifiedHTTPServer_SSEHeaders(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	sseCtx, sseCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer sseCancel()

	req, err := gohttp.NewRequestWithContext(sseCtx, "GET", baseURL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("failed to create SSE request: %v", err)
	}

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil && !strings.Contains(err.Error(), "context deadline") {
		t.Logf("SSE request error: %v", err)
	}
	if resp != nil {
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)

		ct := resp.Header.Get("Content-Type")
		if ct != "text/event-stream" {
			t.Errorf("expected Content-Type text/event-stream, got %s", ct)
		}
		cc := resp.Header.Get("Cache-Control")
		if cc != "no-cache" {
			t.Errorf("expected Cache-Control no-cache, got %s", cc)
		}
	}
}

// TestUnifiedHTTPServer_MCPNotEnabled verifies MCP endpoints return 503 when not enabled.
func TestUnifiedHTTPServer_MCPNotEnabled(t *testing.T) {
	// Start server without MCP option
	baseURL, cancel := startTestServer(t)
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	resp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("MCP POST request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusNotFound {
		t.Errorf("expected 404 when MCP not enabled, got %d", resp.StatusCode)
	}
}

// TestUnifiedHTTPServer_WebSocketConnectionAndBroadcast tests actual WS connection and broadcast.
func TestUnifiedHTTPServer_WebSocketConnectionAndBroadcast(t *testing.T) {
	msgBus := bus.New(nil, nil)

	baseURL, cancel := startTestServer(t, http.WithWebSocket(msgBus, "/ws"))
	defer cancel()

	// Replace http:// with ws:// for WebSocket URL
	wsURL := "ws://" + strings.TrimPrefix(baseURL, "http://") + "/ws"

	// Connect a WebSocket client
	config, err := websocket.NewConfig(wsURL, baseURL)
	if err != nil {
		t.Fatalf("failed to create WS config: %v", err)
	}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Publish a message on the bus — the WS forwarding goroutine should broadcast it.
	// Note: use a single-segment topic because the bus subscribes with "*" which
	// only matches single-segment topics in segment-based wildcard matching.
	msgBus.Publish("chat.message.received", &models.BusMessage{
		ID:      "ws-test-1",
		Type:    models.MessageTypeEvent,
		Source:  "test",
		Payload: json.RawMessage(`{"message":"hello ws","conversation_id":"test-ws"}`),
	})

	// Read the broadcast message from WebSocket.
	// The WS event transformer converts bus topics to frontend types:
	// "chat.message.received" -> type "chat_message"
	var received map[string]any
	deadline := time.Now().Add(3 * time.Second)
	conn.SetReadDeadline(deadline)
	err = websocket.JSON.Receive(conn, &received)
	if err != nil {
		t.Fatalf("failed to read WS broadcast: %v", err)
	}

	if received["type"] != "chat_message" {
		t.Errorf("expected type 'chat_message', got %v", received["type"])
	}
}

// TestUnifiedHTTPServer_WebSocketClientCount verifies hub tracks connected clients.
func TestUnifiedHTTPServer_WebSocketClientCount(t *testing.T) {
	msgBus := bus.New(nil, nil)

	baseURL, cancel := startTestServer(t, http.WithWebSocket(msgBus, "/ws"))
	defer cancel()

	wsURL := "ws://" + strings.TrimPrefix(baseURL, "http://") + "/ws"
	config, err := websocket.NewConfig(wsURL, baseURL)
	if err != nil {
		t.Fatalf("failed to create WS config: %v", err)
	}

	// Connect first client
	conn1, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("failed to connect first WS client: %v", err)
	}
	defer conn1.Close()

	// Connect second client
	conn2, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("failed to connect second WS client: %v", err)
	}
	defer conn2.Close()

	// Give a moment for registration
	time.Sleep(50 * time.Millisecond)

	// Both clients connected — publish a message to verify they're alive.
	// Use a single-segment topic to match the "*" wildcard subscription.
	msgBus.Publish("taskstatus", &models.BusMessage{
		ID:      "ws-count-1",
		Type:    models.MessageTypeEvent,
		Source:  "test",
		Payload: json.RawMessage(`{"ping":"pong"}`),
	})

	// Read from both connections to verify they're alive.
	// The WS event transformer converts "task.status" -> type "job_update"
	var msg1, msg2 map[string]any
	conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	websocket.JSON.Receive(conn1, &msg1)
	websocket.JSON.Receive(conn2, &msg2)

	if msg1["type"] != "job_update" {
		t.Errorf("conn1 expected type 'job_update', got %v", msg1["type"])
	}
	if msg2["type"] != "job_update" {
		t.Errorf("conn2 expected type 'job_update', got %v", msg2["type"])
	}
}

// TestUnifiedHTTPServer_SSESessionEvent verifies SSE sends initial session event.
func TestUnifiedHTTPServer_SSESessionEvent(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	sseCtx, sseCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer sseCancel()

	req, err := gohttp.NewRequestWithContext(sseCtx, "GET", baseURL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("failed to create SSE request: %v", err)
	}

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil && !strings.Contains(err.Error(), "context deadline") {
		t.Fatalf("SSE request failed: %v", err)
	}
	if resp == nil {
		t.Fatal("SSE response is nil")
	}
	defer resp.Body.Close()

	// Read initial data — should contain the session event
	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Logf("SSE read error: %v", err)
	}

	body := string(buf[:n])
	if !strings.Contains(body, "event: session") {
		t.Errorf("expected 'event: session' in SSE stream, got: %s", body)
	}
	if !strings.Contains(body, "session_id") {
		t.Errorf("expected 'session_id' in SSE stream, got: %s", body)
	}
	io.Copy(io.Discard, resp.Body)
}

// TestUnifiedHTTPServer_SSEBusEventForwarding verifies bus events are forwarded as SSE events.
func TestUnifiedHTTPServer_SSEBusEventForwarding(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	sseCtx, sseCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer sseCancel()

	req, err := gohttp.NewRequestWithContext(sseCtx, "GET", baseURL+"/mcp/sse", nil)
	if err != nil {
		t.Fatalf("failed to create SSE request: %v", err)
	}

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil && !strings.Contains(err.Error(), "context deadline") {
		t.Fatalf("SSE request failed: %v", err)
	}
	if resp == nil {
		t.Fatal("SSE response is nil")
	}
	defer resp.Body.Close()

	// Read initial session event
	buf := make([]byte, 8192)
	n, _ := resp.Body.Read(buf)
	initialBody := string(buf[:n])
	if !strings.Contains(initialBody, "event: session") {
		t.Fatalf("missing initial session event, got: %s", initialBody)
	}

	// Publish a bus event — SSE should forward it.
	// Use a single-segment topic to match the "*" wildcard subscription.
	msgBus.Publish("chatresp", &models.BusMessage{
		ID:      "sse-fwd-1",
		Type:    models.MessageTypeEvent,
		Source:  "test",
		Payload: json.RawMessage(`{"content":"hello from SSE test"}`),
	})

	// Give the bus goroutine time to forward the event
	time.Sleep(100 * time.Millisecond)

	// Read the forwarded event — may need multiple reads
	var forwardedBody string
	for i := 0; i < 10; i++ {
		n, _ = resp.Body.Read(buf)
		if n > 0 {
			forwardedBody = string(buf[:n])
			if strings.Contains(forwardedBody, "event: chatresp") {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !strings.Contains(forwardedBody, "event: chatresp") {
		t.Errorf("expected 'event: chatresp' in SSE forwarded data, got: %s", forwardedBody)
	}
	if !strings.Contains(forwardedBody, "hello from SSE test") {
		t.Errorf("expected forwarded payload in SSE data, got: %s", forwardedBody)
	}

	io.Copy(io.Discard, resp.Body)
}

// TestUnifiedHTTPServer_OptionNilGuards verifies options handle nil dependencies gracefully.
func TestUnifiedHTTPServer_OptionNilGuards(t *testing.T) {
	cfg := http.DefaultServerConfig()

	// WithWebSocket with nil bus should not panic
	srv1 := http.NewServer(cfg, nil, nil, nil, nil, nil, http.WithWebSocket(nil, "/ws"))
	if srv1 == nil {
		t.Fatal("server should still be created with nil bus WS option")
	}

	// WithMCP with nil services should not panic
	srv2 := http.NewServer(cfg, nil, nil, nil, nil, nil, http.WithMCP(nil, "/mcp"))
	if srv2 == nil {
		t.Fatal("server should still be created with nil services MCP option")
	}
}

// TestUnifiedHTTPServer_MCPToolSendMissingParams verifies meept_send validates required params.
func TestUnifiedHTTPServer_MCPToolSendMissingParams(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	// Missing session_id
	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name":      "meept_send",
		"arguments": map[string]any{"message": "hello"},
	})
	// Tool returns error content (not JSON-RPC error), so check result contains error text
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content in result")
	}
	textEntry, _ := content[0].(map[string]any)
	text, _ := textEntry["text"].(string)
	if !strings.Contains(text, "error") {
		t.Errorf("expected error text about missing params, got: %s", text)
	}

	// Missing message
	resp2 := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name":      "meept_send",
		"arguments": map[string]any{"session_id": "test"},
	})
	if resp2.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp2.Error.Message)
	}
	var result2 map[string]any
	if err := json.Unmarshal(resp2.Result, &result2); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	content2, _ := result2["content"].([]any)
	if len(content2) == 0 {
		t.Fatal("expected content in result")
	}
	textEntry2, _ := content2[0].(map[string]any)
	text2, _ := textEntry2["text"].(string)
	if !strings.Contains(text2, "error") {
		t.Errorf("expected error text about missing params, got: %s", text2)
	}
}

// TestUnifiedHTTPServer_MCPToolSessionsUnknownAction verifies meept_sessions rejects unknown actions.
func TestUnifiedHTTPServer_MCPToolSessionsUnknownAction(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name":      "meept_sessions",
		"arguments": map[string]any{"action": "nonexistent_action"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content in result")
	}
	textEntry, _ := content[0].(map[string]any)
	text, _ := textEntry["text"].(string)
	if !strings.Contains(text, "unknown action") {
		t.Errorf("expected error about unknown action, got: %s", text)
	}
}

// TestUnifiedHTTPServer_MCPToolEventsMissingSubscription verifies meept_events requires subscription_id.
func TestUnifiedHTTPServer_MCPToolEventsMissingSubscription(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name":      "meept_events",
		"arguments": map[string]any{},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("expected content in result")
	}
	textEntry, _ := content[0].(map[string]any)
	text, _ := textEntry["text"].(string)
	if !strings.Contains(text, "subscription_id is required") {
		t.Errorf("expected error about missing subscription_id, got: %s", text)
	}
}

// TestUnifiedHTTPServer_MCPToolSessionHistoryMissingSession verifies meept_session_history handles missing session.
func TestUnifiedHTTPServer_MCPToolSessionHistoryMissingSession(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	// Call with a session_id that doesn't exist — should return empty/nil, not crash
	resp := mcpPost(t, client, baseURL, "tools/call", map[string]any{
		"name":      "meept_session_history",
		"arguments": map[string]any{"session_id": "nonexistent-session"},
	})
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %s", resp.Error.Message)
	}
	// Should not crash — empty results are valid
}

// TestUnifiedHTTPServer_MCPInvalidParamsJSON verifies tools/call returns -32602 for malformed params.
func TestUnifiedHTTPServer_MCPInvalidParamsJSON(t *testing.T) {
	msgBus := bus.New(nil, nil)
	svcRegistry := &services.ServiceRegistry{
		Bus:          services.NewBusService(msgBus),
		SessionStore: session.NewMemoryStore(nil),
	}

	baseURL, cancel := startTestServer(t, http.WithMCP(svcRegistry, "/mcp"))
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}

	// Send tools/call with params as a string instead of object
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not-an-object"}`
	resp, err := client.Post(baseURL+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("MCP POST failed: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var mcpResp mcpResponse
	if err := json.Unmarshal(data, &mcpResp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if mcpResp.Error == nil {
		t.Fatal("expected error for invalid params, got nil")
	}
	if mcpResp.Error.Code != -32602 {
		t.Errorf("expected error code -32602, got %d", mcpResp.Error.Code)
	}
}

// TestUnifiedHTTPServer_RuntimeStatus_NoRuntime verifies runtime status returns 503 without runtime service.
func TestUnifiedHTTPServer_RuntimeStatus_NoRuntime(t *testing.T) {
	// Server without runtime service — should return 503
	baseURL, cancel := startTestServer(t)
	defer cancel()

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/runtime/status")
	if err != nil {
		t.Fatalf("runtime status request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusServiceUnavailable {
		t.Errorf("expected 503 without runtime service, got %d", resp.StatusCode)
	}
}

// TestUnifiedHTTPServer_RuntimeStatus_WithManager verifies runtime status returns 200 with runtime service.
func TestUnifiedHTTPServer_RuntimeStatus_WithManager(t *testing.T) {
	msgBus := bus.New(nil, nil)
	mgr := llm.NewRuntimeManager(nil)

	svcRegistry := &services.ServiceRegistry{
		Bus:     services.NewBusService(msgBus),
		Runtime: services.NewRuntimeService(mgr),
	}

	cfg := http.DefaultServerConfig()
	cfg.Addr = ":0"
	srv := http.NewServer(cfg, nil, nil, nil, svcRegistry, nil)
	if srv == nil {
		t.Fatal("failed to create server")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Start(ctx)

	// Wait for server to be ready
	for i := 0; i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		conn, err := net.DialTimeout("tcp", "127.0.0.1"+srv.Addr(), time.Second)
		if err == nil {
			conn.Close()
			break
		}
	}

	addr := srv.Addr()
	host, port, _ := net.SplitHostPort(addr)
	if host == "" || host == "::" {
		host = "127.0.0.1"
	}
	baseURL := "http://" + host + ":" + port

	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/v1/runtime/status")
	if err != nil {
		t.Fatalf("runtime status request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}
}
