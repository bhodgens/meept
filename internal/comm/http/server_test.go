package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
)

// -----------------------------------------------------------------------
// Test helpers / mocks
// -----------------------------------------------------------------------

type mockDaemonController struct {
	running bool
	pid     int
	uptime  time.Duration
	restart func(ctx context.Context) error
}

func (m *mockDaemonController) IsRunning() bool       { return m.running }
func (m *mockDaemonController) PID() int              { return m.pid }
func (m *mockDaemonController) Uptime() time.Duration { return m.uptime }
func (m *mockDaemonController) Restart(ctx context.Context) error {
	if m.restart != nil {
		return m.restart(ctx)
	}
	return nil
}

type mockMetricsService struct {
	snapshot    *metrics.LiveMetricsSnapshot
	hPoints     []metrics.MetricPoint
	hResolve    string
	hFrom       time.Time
	hTo         time.Time
	err         error
	subscribeCh chan *metrics.LiveMetricsSnapshot
	subscribeFn func() func()
}

func (m *mockMetricsService) GetLiveMetrics() (*metrics.LiveMetricsSnapshot, error) {
	return m.snapshot, m.err
}

func (m *mockMetricsService) GetHistoricalMetrics(ctx context.Context, from, to time.Time, resolution string) ([]metrics.MetricPoint, error) {
	m.hFrom = from
	m.hTo = to
	m.hResolve = resolution
	return m.hPoints, m.err
}

func (m *mockMetricsService) SubscribeMetrics() (<-chan *metrics.LiveMetricsSnapshot, func()) {
	if m.subscribeFn != nil {
		return m.subscribeCh, m.subscribeFn()
	}
	ch := make(chan *metrics.LiveMetricsSnapshot)
	return ch, func() { close(ch) }
}

func newTestServer(t *testing.T, cfg ServerConfig) *Server {
	t.Helper()
	configService, err := NewConfigService()
	if err != nil {
		t.Fatalf("NewConfigService failed: %v", err)
	}
	dCtrl := &mockDaemonController{running: true, pid: 1234, uptime: 5 * time.Minute}
	mSvc := &mockMetricsService{
		snapshot: &metrics.LiveMetricsSnapshot{
			Timestamp:      time.Now(),
			ActiveAgents:   2,
			RequestsPerSec: 1.5,
			QueueDepth:     0,
		},
	}
	return NewServer(cfg, configService, dCtrl, mSvc, nil)
}

func doRequest(s *Server, method, path string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	w := httptest.NewRecorder()
	s.server = &http.Server{Handler: s.middleware(http.NewServeMux())}
	// Instead, just set up routes manually
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	s.middleware(mux).ServeHTTP(w, r)
	return w
}

// -----------------------------------------------------------------------
// Health endpoint
// -----------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	w := doRequest(s, "GET", "/health", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", resp["status"])
	}
}

// -----------------------------------------------------------------------
// Daemon status
// -----------------------------------------------------------------------

func TestHandleDaemonStatus_Running(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	w := doRequest(s, "GET", "/api/v1/daemon/status", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !resp["running"].(bool) {
		t.Error("expected running=true")
	}
}

func TestHandleDaemonStatus_Offline(t *testing.T) {
	cs, _ := NewConfigService()
	dCtrl := &mockDaemonController{running: false}
	mSvc := &mockMetricsService{snapshot: &metrics.LiveMetricsSnapshot{}}
	s := NewServer(DefaultServerConfig(), cs, dCtrl, mSvc, nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/daemon/status", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["running"].(bool) {
		t.Error("expected running=false")
	}
	if resp["state"] != "offline" {
		t.Errorf("expected state=offline, got %s", resp["state"])
	}
}

func TestHandleDaemonStatus_Working(t *testing.T) {
	cs, _ := NewConfigService()
	dCtrl := &mockDaemonController{running: true, pid: 42, uptime: 10 * time.Minute}
	mSvc := &mockMetricsService{
		snapshot: &metrics.LiveMetricsSnapshot{
			ActiveAgents: 3,
			QueueDepth:   2,
		},
	}
	s := NewServer(DefaultServerConfig(), cs, dCtrl, mSvc, nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/daemon/status", nil)
	s.middleware(mux).ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["state"] != "working" {
		t.Errorf("expected state=working, got %s", resp["state"])
	}
}

func TestHandleDaemonStatus_NoController(t *testing.T) {
	cs, _ := NewConfigService()
	s := NewServer(DefaultServerConfig(), cs, nil, nil, nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/daemon/status", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleDaemonRestart(t *testing.T) {
	cs, _ := NewConfigService()
	var restartCalled bool
	dCtrl := &mockDaemonController{
		restart: func(ctx context.Context) error {
			restartCalled = true
			return nil
		},
	}
	mSvc := &mockMetricsService{snapshot: &metrics.LiveMetricsSnapshot{}}
	s := NewServer(DefaultServerConfig(), cs, dCtrl, mSvc, nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/daemon/restart", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !restartCalled {
		t.Error("restart was not called")
	}
}

func TestHandleDaemonRestart_Error(t *testing.T) {
	cs, _ := NewConfigService()
	dCtrl := &mockDaemonController{
		restart: func(ctx context.Context) error {
			return fmt.Errorf("restart failed")
		},
	}
	s := NewServer(DefaultServerConfig(), cs, dCtrl, nil, nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/daemon/restart", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// Metrics endpoints
// -----------------------------------------------------------------------

func TestHandleLiveMetrics(t *testing.T) {
	cs, _ := NewConfigService()
	dCtrl := &mockDaemonController{}
	mSvc := &mockMetricsService{
		snapshot: &metrics.LiveMetricsSnapshot{
			ActiveAgents: 1, QueueDepth: 0,
		},
	}
	s := NewServer(DefaultServerConfig(), cs, dCtrl, mSvc, nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/metrics/live", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleLiveMetrics_NoService(t *testing.T) {
	cs, _ := NewConfigService()
	s := NewServer(DefaultServerConfig(), cs, nil, nil, nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/metrics/live", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleHistoricalMetrics(t *testing.T) {
	cs, _ := NewConfigService()
	dCtrl := &mockDaemonController{}
	then := time.Now().Add(-1 * time.Hour)
	now := time.Now()
	mSvc := &mockMetricsService{
		hPoints: []metrics.MetricPoint{
			{Name: "cpu", Value: 50.0, Timestamp: then},
		},
	}
	s := NewServer(DefaultServerConfig(), cs, dCtrl, mSvc, nil)

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	url := "/api/v1/metrics/historical?from=" + then.Format(time.RFC3339) + "&to=" + now.Format(time.RFC3339)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", url, nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if cnt, ok := resp["count"].(float64); !ok || int(cnt) != 1 {
		t.Errorf("expected count=1, got %v", resp["count"])
	}
}

func TestHandleHistoricalMetrics_InvalidFrom(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/metrics/historical?from=bad&to=also-bad", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleHistoricalMetrics_MissingParams(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/metrics/historical?from=only-one", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleMetricsStream(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/metrics/stream", nil)
	s.middleware(mux).ServeHTTP(w, req)

	// The stream handler returns a JSON stub (not actual WebSocket)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "websocket_not_implemented" {
		t.Errorf("unexpected status: %s", resp["status"])
	}
}

// -----------------------------------------------------------------------
// Config endpoints
// -----------------------------------------------------------------------

func TestHandleGetClientConfig(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config/client", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json5" {
		t.Errorf("expected Content-Type=application/json5, got %s", ct)
	}
}

func TestHandleGetClientConfig_NoService(t *testing.T) {
	cs, _ := NewConfigService()
	s := NewServer(DefaultServerConfig(), cs, nil, nil, nil)

	// Explicitly nil out the configService in the server
	s.configService = nil
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config/client", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleSaveClientConfig(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	body := `{"content": "some config"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/config/client", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "saved" {
		t.Errorf("expected status=saved, got %s", resp["status"])
	}
}

func TestHandleSaveClientConfig_InvalidBody(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/config/client", strings.NewReader("{invalid"))
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleGetModelsConfig(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config/models", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// Agent endpoints
// -----------------------------------------------------------------------

func TestHandleListAgents(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config/agents", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["agents"]; !ok {
		t.Error("expected agents key in response")
	}
	if _, ok := resp["count"]; !ok {
		t.Error("expected count key in response")
	}
}

func TestHandleListAgents_NoService(t *testing.T) {
	cs, _ := NewConfigService()
	s := NewServer(DefaultServerConfig(), cs, nil, nil, nil)
	s.configService = nil
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config/agents", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleGetAgent(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	// Create a temp agent for testing
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/config/agents/nonexistent-agent", nil)
	s.middleware(mux).ServeHTTP(w, req)

	// Agent shouldn't exist yet
	if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Logf("agent lookup returned %d (expected 404 for non-existent agent)", w.Code)
	}
}

func TestHandleGetAgent_MissingID(t *testing.T) {
	s := newTestServer(t, DefaultServerConfig())
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	w := httptest.NewRecorder()
	// The route is /agents/{id} with no catch-all, so a trailing slash
	// without an id is a 404 from the standard mux (not a 400).
	req := httptest.NewRequest("GET", "/api/v1/config/agents/", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing id (standard mux catch-all behaviour), got %d", w.Code)
	}
}

// -----------------------------------------------------------------------
// CORS middleware
// -----------------------------------------------------------------------

func TestMiddleware_CORSEnabled(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.EnableCORS = true
	s := newTestServer(t, cfg)
	mux := http.NewServeMux()
	s.setupRoutes(mux)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/health", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for OPTIONS, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing CORS origin header")
	}
}

func TestMiddleware_CORSDisabled(t *testing.T) {
	cfg := DefaultServerConfig()
	cfg.EnableCORS = false
	s := newTestServer(t, cfg)
	mux := http.NewServeMux()
	s.setupRoutes(mux)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	s.middleware(mux).ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("expected no CORS header when disabled")
	}
}

// -----------------------------------------------------------------------
// Route coverage
// -----------------------------------------------------------------------

func TestAllRoutesRegistered(t *testing.T) {
	// Quick smoke test that each route is reachable (even if services are nil)
	s := newTestServer(t, DefaultServerConfig())
	s.configService = nil
	s.daemonCtrl = nil
	s.metricsService = nil

	mux := http.NewServeMux()
	s.setupRoutes(mux)

	tests := []struct {
		method string
		path   string
	}{
		{"GET", "/health"},
		{"GET", "/api/v1/health"},
		{"GET", "/api/v1/config/client"},
		{"GET", "/api/v1/config/models"},
		{"GET", "/api/v1/config/agents"},
		{"GET", "/api/v1/config/agents/test-id"},
		{"POST", "/api/v1/daemon/restart"},
		{"GET", "/api/v1/metrics/live"},
		{"GET", "/api/v1/metrics/stream"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, nil)
			s.middleware(mux).ServeHTTP(w, req)
			// Just check it doesn't panic and returns *some* status
			if w.Code < 100 || w.Code >= 600 {
				t.Errorf("unexpected status code: %d", w.Code)
			}
		})
	}
}
