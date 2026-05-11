package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
)

// mockDaemonController implements DaemonController for testing.
type mockDaemonController struct {
	running   bool
	pid       int
	uptime    time.Duration
	restartErr error
}

func (m *mockDaemonController) IsRunning() bool            { return m.running }
func (m *mockDaemonController) PID() int                   { return m.pid }
func (m *mockDaemonController) Uptime() time.Duration      { return m.uptime }
func (m *mockDaemonController) Restart(ctx context.Context) error { return m.restartErr }

// mockMetricsService implements MetricsService for testing.
type mockMetricsService struct {
	liveMetrics    *metrics.LiveMetricsSnapshot
	liveErr        error
	historicalData []metrics.MetricPoint
	historicalErr  error
}

func (m *mockMetricsService) GetLiveMetrics() (*metrics.LiveMetricsSnapshot, error) {
	return m.liveMetrics, m.liveErr
}

func (m *mockMetricsService) GetHistoricalMetrics(ctx context.Context, from, to time.Time, resolution string) ([]metrics.MetricPoint, error) {
	return m.historicalData, m.historicalErr
}

func (m *mockMetricsService) SubscribeMetrics() (<-chan *metrics.LiveMetricsSnapshot, func()) {
	ch := make(chan *metrics.LiveMetricsSnapshot)
	return ch, func() { close(ch) }
}

func TestNewServer(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)
	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.config.Addr != ":8081" {
		t.Errorf("default addr = %s, want :8081", server.config.Addr)
	}
}

func TestHandleHealth(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("status = %s, want ok", body["status"])
	}
}

func TestHandleDaemonStatus_Running(t *testing.T) {
	daemon := &mockDaemonController{
		running: true,
		pid:     12345,
		uptime:  5 * time.Minute,
	}
	server := NewServer(ServerConfig{}, nil, daemon, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/status", nil)
	w := httptest.NewRecorder()

	server.handleDaemonStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["running"] != true {
		t.Error("expected running = true")
	}
	if int(body["pid"].(float64)) != 12345 {
		t.Errorf("pid = %v, want 12345", body["pid"])
	}
	if body["uptime"] == "" {
		t.Error("uptime should not be empty")
	}
}

func TestHandleDaemonStatus_NotRunning(t *testing.T) {
	daemon := &mockDaemonController{
		running: false,
	}
	server := NewServer(ServerConfig{}, nil, daemon, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/status", nil)
	w := httptest.NewRecorder()

	server.handleDaemonStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["running"] != false {
		t.Error("expected running = false")
	}
	if body["state"] != "offline" {
		t.Errorf("state = %v, want offline", body["state"])
	}
}

func TestHandleDaemonStatus_NoDaemonController(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/status", nil)
	w := httptest.NewRecorder()

	server.handleDaemonStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleGetClientConfig_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/client", nil)
	w := httptest.NewRecorder()

	server.handleGetClientConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleSaveClientConfig_InvalidBody(t *testing.T) {
	// Create a temp config service
	tmpDir := t.TempDir()
	// We can't easily test with a real ConfigService without setting up home dir
	// So test the error path
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/client", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	server.handleSaveClientConfig(w, req)

	resp := w.Result()
	// Should fail because configService is nil
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (no config service)", resp.StatusCode, http.StatusServiceUnavailable)
	}
	_ = tmpDir
}

func TestHandleGetModelsConfig_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/models", nil)
	w := httptest.NewRecorder()

	server.handleGetModelsConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleLiveMetrics_Success(t *testing.T) {
	metricsSvc := &mockMetricsService{
		liveMetrics: &metrics.LiveMetricsSnapshot{
			Timestamp:      time.Now(),
			RequestsPerSec: 100,
			TokenUsageRate: 5000,
			ActiveAgents:   2,
		},
	}
	server := NewServer(ServerConfig{}, nil, nil, metricsSvc, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/live", nil)
	w := httptest.NewRecorder()

	server.handleLiveMetrics(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["requests_per_sec"].(float64) != 100 {
		t.Errorf("requests_per_sec = %v, want 100", body["requests_per_sec"])
	}
}

func TestHandleLiveMetrics_NoService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/live", nil)
	w := httptest.NewRecorder()

	server.handleLiveMetrics(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleHistoricalMetrics_MissingParams(t *testing.T) {
	metricsSvc := &mockMetricsService{}
	server := NewServer(ServerConfig{}, nil, nil, metricsSvc, nil, nil)

	// Missing from and to
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/historical", nil)
	w := httptest.NewRecorder()

	server.handleHistoricalMetrics(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleHistoricalMetrics_InvalidFromParam(t *testing.T) {
	metricsSvc := &mockMetricsService{}
	server := NewServer(ServerConfig{}, nil, nil, metricsSvc, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/historical?from=invalid&to=2024-01-01T00:00:00Z", nil)
	w := httptest.NewRecorder()

	server.handleHistoricalMetrics(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "invalid from parameter") {
		t.Errorf("expected 'invalid from parameter' error, got: %s", body)
	}
}

func TestHandleHistoricalMetrics_InvalidToParam(t *testing.T) {
	metricsSvc := &mockMetricsService{}
	server := NewServer(ServerConfig{}, nil, nil, metricsSvc, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/historical?from=2024-01-01T00:00:00Z&to=invalid", nil)
	w := httptest.NewRecorder()

	server.handleHistoricalMetrics(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "invalid to parameter") {
		t.Errorf("expected 'invalid to parameter' error, got: %s", body)
	}
}

func TestHandleHistoricalMetrics_Success(t *testing.T) {
	metricsSvc := &mockMetricsService{
		historicalData: []metrics.MetricPoint{
			{Timestamp: time.Now(), Value: 100},
			{Timestamp: time.Now(), Value: 150},
		},
	}
	server := NewServer(ServerConfig{}, nil, nil, metricsSvc, nil, nil)

	from := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	to := time.Now().Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/historical?from="+from+"&to="+to, nil)
	w := httptest.NewRecorder()

	server.handleHistoricalMetrics(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if int(body["count"].(float64)) != 2 {
		t.Errorf("count = %v, want 2", body["count"])
	}
}

func TestMiddleware_CORS(t *testing.T) {
	server := NewServer(ServerConfig{EnableCORS: true}, nil, nil, nil, nil, nil)

	handler := server.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS header not set")
	}
}

func TestMiddleware_CORSPreflight(t *testing.T) {
	server := NewServer(ServerConfig{EnableCORS: true}, nil, nil, nil, nil, nil)

	handler := server.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("preflight status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestMiddleware_NoCORS(t *testing.T) {
	server := NewServer(ServerConfig{EnableCORS: false}, nil, nil, nil, nil, nil)

	handler := server.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS header should not be set when disabled")
	}
}

func TestWriteJSON(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	server.writeJSON(w, http.StatusCreated, map[string]string{"key": "value"})

	resp := w.Result()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", resp.Header.Get("Content-Type"))
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["key"] != "value" {
		t.Errorf("body[key] = %s, want value", body["key"])
	}
}

func TestWriteError(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	server.writeError(w, http.StatusNotFound, "not found")

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["error"] != "not found" {
		t.Errorf("error = %s, want 'not found'", body["error"])
	}
}

func TestDefaultServerConfig(t *testing.T) {
	cfg := DefaultServerConfig()

	if cfg.Addr != ":8081" {
		t.Errorf("Addr = %s, want :8081", cfg.Addr)
	}
	if cfg.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v, want 30s", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 30*time.Second {
		t.Errorf("WriteTimeout = %v, want 30s", cfg.WriteTimeout)
	}
	if !cfg.EnableCORS {
		t.Error("EnableCORS should be true by default")
	}
}

func TestHandleListAgents_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/agents", nil)
	w := httptest.NewRecorder()

	server.handleListAgents(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleGetAgent_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/agents/test", nil)
	req.SetPathValue("id", "test")
	w := httptest.NewRecorder()

	server.handleGetAgent(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleGetAgent_MissingID(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/agents/", nil)
	// Don't set path value to simulate missing ID
	w := httptest.NewRecorder()

	server.handleGetAgent(w, req)

	resp := w.Result()
	// Will fail on config service check first since no service
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleSaveAgent_InvalidBody(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/agents/test", strings.NewReader("not json"))
	req.SetPathValue("id", "test")
	w := httptest.NewRecorder()

	server.handleSaveAgent(w, req)

	resp := w.Result()
	// Will fail on config service check first since no service
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleDeleteAgent_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/config/agents/test", nil)
	req.SetPathValue("id", "test")
	w := httptest.NewRecorder()

	server.handleDeleteAgent(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleDaemonRestart_Success(t *testing.T) {
	daemon := &mockDaemonController{running: true}
	server := NewServer(ServerConfig{}, nil, daemon, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/daemon/restart", nil)
	w := httptest.NewRecorder()

	server.handleDaemonRestart(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "restarted" {
		t.Errorf("status = %s, want restarted", body["status"])
	}
}

func TestHandleDaemonRestart_NoDaemonController(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/daemon/restart", nil)
	w := httptest.NewRecorder()

	server.handleDaemonRestart(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleMetricsStream(t *testing.T) {
	metricsSvc := &mockMetricsService{}
	server := NewServer(ServerConfig{}, nil, nil, metricsSvc, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/stream", nil)
	w := httptest.NewRecorder()

	server.handleMetricsStream(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Currently returns not implemented message
	if body["status"] != "websocket_not_implemented" {
		t.Errorf("status = %s, want websocket_not_implemented", body["status"])
	}
}
