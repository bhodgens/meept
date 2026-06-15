package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/comm/web"
)

// ---------------------------------------------------------------------------
// Integration test helpers
// ---------------------------------------------------------------------------

// stubHandler implements web.Handler for integration tests.
type stubHandler struct {
	chatFn   func(ctx context.Context, message string) (string, error)
	statusFn func(ctx context.Context) (map[string]any, error)
}

func (h *stubHandler) Chat(ctx context.Context, message string) (string, error) {
	if h.chatFn != nil {
		return h.chatFn(ctx, message)
	}
	return "echo: " + message, nil
}

func (h *stubHandler) Status(ctx context.Context) (map[string]any, error) {
	if h.statusFn != nil {
		return h.statusFn(ctx)
	}
	return map[string]any{"status": "running", "uptime": "1h"}, nil
}

type testEnv struct {
	server *web.Server
	mux    *http.ServeMux
}

func newTestEnv() *testEnv {
	cfg := web.DefaultServerConfig()
	handler := &stubHandler{}
	s := web.NewServer(cfg, handler, web.NoAuth{}, nil)
	mux := http.NewServeMux()
	s.SetupRoutesForTest(mux)
	return &testEnv{server: s, mux: mux}
}

func (e *testEnv) doRequest(method, path string, body any) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, req)
	return w
}

func (e *testEnv) doGet(path string) *httptest.ResponseRecorder {
	return e.doRequest(http.MethodGet, path, nil)
}

func parseJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v (body: %s)", err, w.Body.String())
	}
	return result
}

// ---------------------------------------------------------------------------
// Integration tests: Full API surface
// ---------------------------------------------------------------------------

func TestIntegration_HealthEndpoints(t *testing.T) {
	env := newTestEnv()

	for _, path := range []string{"/health", "/api/v1/health"} {
		t.Run(path, func(t *testing.T) {
			w := env.doGet(path)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			body := parseJSON(t, w)
			if body["status"] != "ok" {
				t.Fatalf("expected status ok, got %v", body["status"])
			}
		})
	}
}

func TestIntegration_StatusEndpoint(t *testing.T) {
	env := newTestEnv()
	w := env.doGet("/api/v1/status")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseJSON(t, w)
	if body["status"] != "running" {
		t.Fatalf("expected running, got %v", body["status"])
	}
}

func TestIntegration_ChatEndpoint(t *testing.T) {
	env := newTestEnv()
	w := env.doRequest(http.MethodPost, "/api/v1/chat", map[string]string{"message": "hello"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseJSON(t, w)
	if body["response"] != "echo: hello" {
		t.Fatalf("expected 'echo: hello', got %v", body["response"])
	}
}

func TestIntegration_ChatStreamEndpoint(t *testing.T) {
	env := newTestEnv()
	w := env.doRequest(http.MethodPost, "/api/v1/chat/stream", map[string]string{"message": "hello"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "data:") {
		t.Fatalf("expected SSE data events, got: %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("expected done event, got: %s", body)
	}
}

func TestIntegration_SessionsCRUD(t *testing.T) {
	// Without SessionManager configured, all should gracefully degrade
	env := newTestEnv()

	// List
	w := env.doGet("/api/v1/sessions")
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}

	// Create - should fail gracefully
	w = env.doRequest(http.MethodPost, "/api/v1/sessions", map[string]string{"name": "test"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("create: expected 503, got %d", w.Code)
	}

	// Get - should fail gracefully
	w = env.doGet("/api/v1/sessions/abc")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("get: expected 503, got %d", w.Code)
	}

	// Delete - should fail gracefully
	w = env.doRequest(http.MethodDelete, "/api/v1/sessions/abc", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("delete: expected 503, got %d", w.Code)
	}
}

func TestIntegration_AgentsList(t *testing.T) {
	env := newTestEnv()
	w := env.doGet("/api/v1/agents")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseJSON(t, w)
	// Should return default 8 agents
	if count, ok := body["count"].(float64); !ok || count != 8 {
		t.Fatalf("expected 8 agents, got %v", body["count"])
	}
}

func TestIntegration_AgentsDelegate_NotConfigured(t *testing.T) {
	env := newTestEnv()
	w := env.doRequest(http.MethodPost, "/api/v1/agents/coder/delegate", map[string]string{"message": "fix bug"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestIntegration_ToolsList_NotConfigured(t *testing.T) {
	env := newTestEnv()
	w := env.doGet("/api/v1/tools")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := parseJSON(t, w)
	if _, ok := body["tools"]; !ok {
		t.Fatalf("expected tools key in response")
	}
}

func TestIntegration_MemoryEndpoints(t *testing.T) {
	env := newTestEnv()

	// Search - no query param
	w := env.doGet("/api/v1/memory/search")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("search no query: expected 400, got %d", w.Code)
	}

	// Search - with query
	w = env.doGet("/api/v1/memory/search?q=test")
	if w.Code != http.StatusOK {
		t.Fatalf("search: expected 200, got %d", w.Code)
	}

	// Store - not configured
	w = env.doRequest(http.MethodPost, "/api/v1/memory", map[string]string{"content": "test"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("store: expected 503, got %d", w.Code)
	}
}

func TestIntegration_SkillsEndpoints(t *testing.T) {
	env := newTestEnv()

	// List
	w := env.doGet("/api/v1/skills")
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}

	// Execute - not configured
	w = env.doRequest(http.MethodPost, "/api/v1/skills/review/execute", map[string]string{"input": "test"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("execute: expected 503, got %d", w.Code)
	}
}

func TestIntegration_JobsEndpoints(t *testing.T) {
	env := newTestEnv()

	// List - not configured but graceful
	w := env.doGet("/api/v1/jobs")
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}

	// Create - not configured
	w = env.doRequest(http.MethodPost, "/api/v1/jobs", map[string]string{"name": "test"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("create: expected 503, got %d", w.Code)
	}

	// Get - not configured
	w = env.doGet("/api/v1/jobs/abc")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("get: expected 503, got %d", w.Code)
	}

	// Cancel - not configured
	w = env.doRequest(http.MethodDelete, "/api/v1/jobs/abc", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("cancel: expected 503, got %d", w.Code)
	}
}

func TestIntegration_AllEndpoints_Exist(t *testing.T) {
	// Verify all routes are registered by hitting them and getting non-404
	env := newTestEnv()

	endpoints := []struct {
		method string
		path   string
		body   any
	}{
		{"GET", "/health", nil},
		{"GET", "/api/v1/health", nil},
		{"GET", "/api/v1/status", nil},
		{"POST", "/api/v1/chat", map[string]string{"message": "hi"}},
		{"POST", "/api/v1/query", map[string]string{"message": "hi"}},
		{"POST", "/api/v1/chat/stream", map[string]string{"message": "hi"}},
		{"GET", "/api/v1/sessions", nil},
		{"POST", "/api/v1/sessions", map[string]string{"name": "test"}},
		{"GET", "/api/v1/sessions/s1", nil},
		{"DELETE", "/api/v1/sessions/s1", nil},
		{"GET", "/api/v1/memory/search?q=test", nil},
		{"POST", "/api/v1/memory", map[string]string{"content": "test"}},
		{"GET", "/api/v1/skills", nil},
		{"POST", "/api/v1/skills/review/execute", map[string]string{"input": "test"}},
		{"GET", "/api/v1/jobs", nil},
		{"POST", "/api/v1/jobs", map[string]string{"name": "test"}},
		{"GET", "/api/v1/jobs/j1", nil},
		{"DELETE", "/api/v1/jobs/j1", nil},
		{"GET", "/api/v1/agents", nil},
		{"POST", "/api/v1/agents/coder/delegate", map[string]string{"message": "test"}},
		{"GET", "/api/v1/tools", nil},
		// WebSocket endpoint requires HTTP Hijack support (not available in httptest),
		// so it's tested separately in TestIntegration_WebSocketRoute_Registered.
	}

	for _, ep := range endpoints {
		name := fmt.Sprintf("%s %s", ep.method, ep.path)
		t.Run(name, func(t *testing.T) {
			w := env.doRequest(ep.method, ep.path, ep.body)
			// 404 means the route is not registered
			if w.Code == http.StatusNotFound {
				t.Fatalf("route not registered: %s %s (got 404)", ep.method, ep.path)
			}
		})
	}
}

func TestIntegration_ServerLifecycle(t *testing.T) {
	cfg := web.DefaultServerConfig()
	cfg.Addr = ":0" // Use random port
	handler := &stubHandler{}
	s := web.NewServer(cfg, handler, web.NoAuth{}, nil)

	// Verify server can be created and has WSHub
	hub := s.WSHub()
	if hub == nil {
		t.Fatalf("expected non-nil WSHub")
	}

	// Verify config defaults
	if cfg.ReadTimeout != 30*time.Second {
		t.Fatalf("expected 30s read timeout, got %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 30*time.Second {
		t.Fatalf("expected 30s write timeout, got %v", cfg.WriteTimeout)
	}
}

func TestIntegration_CORSHeaders(t *testing.T) {
	cfg := web.DefaultServerConfig()
	cfg.EnableCORS = true
	handler := &stubHandler{}
	s := web.NewServer(cfg, handler, web.NoAuth{}, nil)
	mux := http.NewServeMux()
	s.SetupRoutesForTest(mux)

	// Wrap with middleware like the server does
	handler2 := s.TestMiddleware(mux)

	// Test OPTIONS preflight with an allowed origin
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/status", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	handler2.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPTIONS, got %d", w.Code)
	}
	if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3000" {
		t.Fatalf("expected CORS origin http://localhost:3000, got %s", origin)
	}

	// Test actual request has CORS headers
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/status", http.NoBody)
	req2.Header.Set("Origin", "http://localhost:3000")
	w2 := httptest.NewRecorder()
	handler2.ServeHTTP(w2, req2)
	if origin := w2.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3000" {
		t.Fatalf("expected CORS origin http://localhost:3000, got %s", origin)
	}
}

func TestIntegration_WebSocketRoute_Registered(t *testing.T) {
	// Verify the WebSocket route is registered by checking it responds
	// with something other than 404. We use a real HTTP server since
	// httptest.ResponseRecorder doesn't support Hijack.
	env := newTestEnv()
	srv := httptest.NewServer(env.mux)
	defer srv.Close()

	// Try a regular GET; the WebSocket handler will respond with 400
	// because the upgrade headers are missing, which proves the route exists.
	resp, err := http.Get(srv.URL + "/api/v1/ws")
	if err != nil {
		t.Fatalf("GET /api/v1/ws request failed: %v", err)
	}
	defer resp.Body.Close()
	// 400 is expected (bad WebSocket upgrade), NOT 404 (route not found)
	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("WebSocket route not registered (got 404)")
	}
}
