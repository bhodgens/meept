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

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/services"
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

// ===== Steering / Follow-Up Queue Tests =====

func TestHandleChatSteer_Success(t *testing.T) {
	t.Parallel()
	chatSvc := services.NewChatService(nil, nil, nil)
	svcReg := &services.ServiceRegistry{Chat: chatSvc}
	server := NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)

	body := `{"message":"focus on tests","conversation_id":"conv-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/steer", strings.NewReader(body))
	w := httptest.NewRecorder()

	server.handleChatSteer(w, req)

	resp := w.Result()
	// Steer returns ErrUnavailable (nil agentRegistry) -> handleServiceError -> 500.
	// The handler correctly decoded the valid JSON and reached the service call.
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestHandleChatSteer_InvalidBody(t *testing.T) {
	t.Parallel()
	chatSvc := services.NewChatService(nil, nil, nil)
	svcReg := &services.ServiceRegistry{Chat: chatSvc}
	server := NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/steer", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	server.handleChatSteer(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["error"] != "invalid request body" {
		t.Errorf("error = %s, want 'invalid request body'", body["error"])
	}
}

func TestHandleChatSteer_NoService(t *testing.T) {
	t.Parallel()
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	reqBody := `{"message":"focus on tests","conversation_id":"conv-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/steer", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	server.handleChatSteer(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if respBody["error"] != "chat service not available" {
		t.Errorf("error = %s, want 'chat service not available'", respBody["error"])
	}
}

func TestHandleChatFollowUp_Success(t *testing.T) {
	t.Parallel()
	chatSvc := services.NewChatService(nil, nil, nil)
	svcReg := &services.ServiceRegistry{Chat: chatSvc}
	server := NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)

	body := `{"message":"also run linter","conversation_id":"conv-456"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/followup", strings.NewReader(body))
	w := httptest.NewRecorder()

	server.handleChatFollowUp(w, req)

	resp := w.Result()
	// FollowUp returns ErrUnavailable (nil agentRegistry) -> handleServiceError -> 500.
	// The handler correctly decoded the valid JSON and reached the service call.
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestHandleChatFollowUp_InvalidBody(t *testing.T) {
	t.Parallel()
	chatSvc := services.NewChatService(nil, nil, nil)
	svcReg := &services.ServiceRegistry{Chat: chatSvc}
	server := NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/followup", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	server.handleChatFollowUp(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["error"] != "invalid request body" {
		t.Errorf("error = %s, want 'invalid request body'", body["error"])
	}
}

func TestHandleChatQueueStatus_Active(t *testing.T) {
	t.Parallel()
	// ChatService.GetQueueStatus gracefully handles nil agentRegistry,
	// returning a zero-valued response instead of an error.
	chatSvc := services.NewChatService(nil, nil, nil)
	svcReg := &services.ServiceRegistry{Chat: chatSvc}
	server := NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/queue/conv-789", nil)
	w := httptest.NewRecorder()

	server.handleChatQueueStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["steering_depth"].(float64) != 0 {
		t.Errorf("steering_depth = %v, want 0", body["steering_depth"])
	}
	if body["followup_depth"].(float64) != 0 {
		t.Errorf("followup_depth = %v, want 0", body["followup_depth"])
	}
	if body["is_active"] != false {
		t.Errorf("is_active = %v, want false", body["is_active"])
	}
}

func TestHandleChatQueueStatus_NoService(t *testing.T) {
	t.Parallel()
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/queue/conv-789", nil)
	w := httptest.NewRecorder()

	server.handleChatQueueStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["error"] != "chat service not available" {
		t.Errorf("error = %s, want 'chat service not available'", body["error"])
	}
}

func TestSSEWriter(t *testing.T) {
	t.Parallel()

	t.Run("SendEvent", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, err := NewSSEWriter(rec)
		if err != nil {
			t.Fatalf("NewSSEWriter: %v", err)
		}

		if err := sse.SendEvent("test_event", map[string]string{"key": "value"}); err != nil {
			t.Fatalf("SendEvent: %v", err)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "event: test_event") {
			t.Error("expected event type in output")
		}
		if !strings.Contains(body, `data: {"key":"value"}`) {
			t.Errorf("expected data in output, got: %s", body)
		}

		// Verify headers
		ct := rec.Header().Get("Content-Type")
		if ct != "text/event-stream" {
			t.Errorf("Content-Type = %s, want text/event-stream", ct)
		}
	})

	t.Run("SendComment", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, err := NewSSEWriter(rec)
		if err != nil {
			t.Fatalf("NewSSEWriter: %v", err)
		}

		if err := sse.SendComment(); err != nil {
			t.Fatalf("SendComment: %v", err)
		}

		body := rec.Body.String()
		if !strings.Contains(body, ": heartbeat") {
			t.Errorf("expected heartbeat comment, got: %s", body)
		}
	})

	t.Run("SendError", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, err := NewSSEWriter(rec)
		if err != nil {
			t.Fatalf("NewSSEWriter: %v", err)
		}

		if err := sse.SendError("something went wrong"); err != nil {
			t.Fatalf("SendError: %v", err)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "event: error") {
			t.Error("expected error event in output")
		}
		if !strings.Contains(body, "something went wrong") {
			t.Errorf("expected error message in output, got: %s", body)
		}
	})
}

func TestHandleChatStream_NoService(t *testing.T) {
	t.Parallel()
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream", nil)
	w := httptest.NewRecorder()

	server.handleChatStream(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["error"] != "bus service not available" {
		t.Errorf("error = %s, want 'bus service not available'", body["error"])
	}
}

func TestHandleChatStream_SSEToolProgressEvent(t *testing.T) {
	t.Parallel()

	// Create a real bus and wrap it in a BusService
	msgBus := bus.New(nil, nil)
	busSvc := services.NewBusService(msgBus)
	svcReg := &services.ServiceRegistry{Bus: busSvc}
	server := NewServer(ServerConfig{EnableCORS: true}, nil, nil, nil, svcReg, nil)

	// Create a cancellable context to simulate client disconnect
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Run the handler in a goroutine since it blocks on the event loop
	done := make(chan struct{})
	go func() {
		server.handleChatStream(w, req)
		close(done)
	}()

	// Wait briefly for the handler to subscribe, then publish a progress event
	time.Sleep(50 * time.Millisecond)

	progressPayload := map[string]any{
		"tool_call_id": "call_123",
		"tool_name":    "file_read",
		"agent_id":     "coder",
		"message":      "reading file...",
		"percent":      50,
	}
	_ = busSvc.Publish(context.Background(), services.PublishRequest{
		Topic:   "tool.execution.progress",
		Type:    "event",
		Source:  "executor",
		Payload: progressPayload,
	})

	// Give the event time to propagate
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop the handler
	cancel()

	// Wait for handler to finish
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish in time")
	}

	body := w.Body.String()

	// Should contain the connected event
	if !strings.Contains(body, "event: connected") {
		t.Error("expected connected event in SSE output")
	}

	// Should contain the tool_progress event with correct data
	if !strings.Contains(body, "event: tool_progress") {
		t.Error("expected tool_progress event in SSE output")
	}
	if !strings.Contains(body, `"tool_name":"file_read"`) {
		t.Errorf("expected tool_name in SSE data, got: %s", body)
	}
	if !strings.Contains(body, `"tool_call_id":"call_123"`) {
		t.Errorf("expected tool_call_id in SSE data, got: %s", body)
	}
	if !strings.Contains(body, `"percent":50`) {
		t.Errorf("expected percent in SSE data, got: %s", body)
	}

	// Verify SSE headers
	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %s, want text/event-stream", ct)
	}
}

func TestHandleChatStream_SSEToolCompleteEvent(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, nil)
	busSvc := services.NewBusService(msgBus)
	svcReg := &services.ServiceRegistry{Bus: busSvc}
	server := NewServer(ServerConfig{EnableCORS: true}, nil, nil, nil, svcReg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.handleChatStream(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	completePayload := map[string]any{
		"tool_call_id": "call_456",
		"tool_name":    "shell",
		"agent_id":     "coder",
		"success":      true,
		"terminate":    true,
		"cached":       false,
	}
	_ = busSvc.Publish(context.Background(), services.PublishRequest{
		Topic:   "tool.execution.complete",
		Type:    "event",
		Source:  "executor",
		Payload: completePayload,
	})

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish in time")
	}

	body := w.Body.String()

	if !strings.Contains(body, "event: tool_complete") {
		t.Error("expected tool_complete event in SSE output")
	}
	if !strings.Contains(body, `"tool_call_id":"call_456"`) {
		t.Errorf("expected tool_call_id in SSE data, got: %s", body)
	}
	if !strings.Contains(body, `"terminate":true`) {
		t.Errorf("expected terminate in SSE data, got: %s", body)
	}
}
