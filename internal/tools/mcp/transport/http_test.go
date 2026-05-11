package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPTransport_StartStop(t *testing.T) {
	transport := NewHTTPTransport("http://localhost:8080", nil, DefaultConfig())

	ctx := context.Background()

	if err := transport.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !transport.IsRunning() {
		t.Error("transport should be running after Start")
	}

	if err := transport.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if transport.IsRunning() {
		t.Error("transport should not be running after Close")
	}
}

func TestHTTPTransport_Send(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("expected Content-Type: application/json")
		}

		// Parse request
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		// Send response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  map[string]any{"status": "ok"},
		})
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, DefaultConfig())

	ctx := context.Background()
	transport.Start(ctx)
	defer transport.Close()

	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "test",
	}
	reqData, _ := json.Marshal(request)

	response, err := transport.Send(ctx, reqData)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(response, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp["id"] != float64(1) {
		t.Errorf("expected id 1, got %v", resp["id"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp["result"])
	}
	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", result["status"])
	}
}

func TestHTTPTransport_SendWithHeaders(t *testing.T) {
	var receivedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, map[string]string{
		"Authorization": "Bearer test-token",
		"X-Custom":      "custom-value",
	}, DefaultConfig())

	ctx := context.Background()
	transport.Start(ctx)
	defer transport.Close()

	_, err := transport.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`))
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if receivedHeaders.Get("Authorization") != "Bearer test-token" {
		t.Error("Authorization header not sent")
	}
	if receivedHeaders.Get("X-Custom") != "custom-value" {
		t.Error("X-Custom header not sent")
	}
}

func TestHTTPTransport_SessionID(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// First call: send session ID
		if callCount == 1 {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		} else if r.Header.Get("Mcp-Session-Id") != "test-session-123" {
			// Second call: verify session ID was sent
			t.Error("Session ID not sent in subsequent request")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, DefaultConfig())

	ctx := context.Background()
	transport.Start(ctx)
	defer transport.Close()

	// First request
	transport.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`))

	if transport.GetSessionID() != "test-session-123" {
		t.Error("Session ID not stored")
	}

	// Second request
	transport.Send(ctx, []byte(`{"jsonrpc":"2.0","id":2,"method":"test"}`))

	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestHTTPTransport_SSEResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Write SSE format response
		w.Write([]byte("event: message\n"))
		w.Write([]byte(`data: {"jsonrpc":"2.0","id":1,"result":{"status":"from_sse"}}` + "\n"))
		w.Write([]byte("\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, DefaultConfig())

	ctx := context.Background()
	transport.Start(ctx)
	defer transport.Close()

	response, err := transport.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`))
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(response, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp["result"])
	}
	if result["status"] != "from_sse" {
		t.Errorf("expected status 'from_sse', got %v", result["status"])
	}
}

func TestHTTPTransport_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, nil, DefaultConfig())

	ctx := context.Background()
	transport.Start(ctx)
	defer transport.Close()

	_, err := transport.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"test"}`))
	if err == nil {
		t.Error("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestHTTPTransport_SendNotRunning(t *testing.T) {
	transport := NewHTTPTransport("http://localhost:8080", nil, DefaultConfig())

	ctx := context.Background()

	// Send without Start should fail
	_, err := transport.Send(ctx, []byte(`{}`))
	if err == nil {
		t.Error("expected error when not running")
	}
}

func TestHTTPTransport_SetSessionID(t *testing.T) {
	transport := NewHTTPTransport("http://localhost:8080", nil, DefaultConfig())

	transport.SetSessionID("manual-session")

	if transport.GetSessionID() != "manual-session" {
		t.Error("SetSessionID did not work")
	}
}
