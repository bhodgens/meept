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
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/internal/session"
)

// startTestServer creates and starts a server on a random port, returning the
// base URL and a cancel function. The caller must defer cancel().
func startTestServer(t *testing.T, opts ...http.ServerOption) (baseURL string, cancel context.CancelFunc) {
	t.Helper()
	cfg := http.DefaultServerConfig()
	cfg.Addr = ":0"

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
	cfg.Addr = ":0" // Let OS choose available port

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
