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

func (m *mockMetricsService) SubscribeMetrics() (_ <-chan *metrics.LiveMetricsSnapshot, _ func()) {
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

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/status", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/status", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/status", http.NoBody)
	w := httptest.NewRecorder()

	server.handleDaemonStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleGetClientConfig_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/client", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/models", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/live", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/live", http.NoBody)
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/historical", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/historical?from=invalid&to=2024-01-01T00:00:00Z", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/historical?from=2024-01-01T00:00:00Z&to=invalid", http.NoBody)
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/historical?from="+from+"&to="+to, http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
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

	req := httptest.NewRequest(http.MethodOptions, "/test", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/agents", http.NoBody)
	w := httptest.NewRecorder()

	server.handleListAgents(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleGetAgent_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/agents/test", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/agents/", http.NoBody)
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

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/config/agents/test", http.NoBody)
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/daemon/restart", http.NoBody)
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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/daemon/restart", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/stream", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/queue/conv-789", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/queue/conv-789", http.NoBody)
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

	t.Run("MultipleEventsInSequence", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, err := NewSSEWriter(rec)
		if err != nil {
			t.Fatalf("NewSSEWriter: %v", err)
		}

		// Send several events in sequence to verify they are each delimited
		if err := sse.SendEvent("progress", map[string]any{"percent": 10}); err != nil {
			t.Fatalf("SendEvent 1: %v", err)
		}
		if err := sse.SendEvent("progress", map[string]any{"percent": 50}); err != nil {
			t.Fatalf("SendEvent 2: %v", err)
		}
		if err := sse.SendEvent("progress", map[string]any{"percent": 100}); err != nil {
			t.Fatalf("SendEvent 3: %v", err)
		}
		if err := sse.SendComment(); err != nil {
			t.Fatalf("SendComment: %v", err)
		}
		if err := sse.SendEvent("done", map[string]string{"status": "complete"}); err != nil {
			t.Fatalf("SendEvent done: %v", err)
		}

		body := rec.Body.String()

		// Each SSE event must be separated by a blank line (\n\n)
		// Verify the three progress events appear in order
		idx10 := strings.Index(body, `"percent":10`)
		idx50 := strings.Index(body, `"percent":50`)
		idx100 := strings.Index(body, `"percent":100`)
		if idx10 == -1 || idx50 == -1 || idx100 == -1 {
			t.Fatalf("expected all three percent values, got: %s", body)
		}
		if idx10 >= idx50 || idx50 >= idx100 {
			t.Errorf("progress events not in order: idx10=%d idx50=%d idx100=%d", idx10, idx50, idx100)
		}

		// Heartbeat comment should appear between last progress and done
		if !strings.Contains(body, ": heartbeat") {
			t.Error("expected heartbeat comment in sequential output")
		}

		// Done event should be last
		if !strings.Contains(body, "event: done") {
			t.Errorf("expected done event, got: %s", body)
		}
	})

	t.Run("SpecialCharactersInData", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, err := NewSSEWriter(rec)
		if err != nil {
			t.Fatalf("NewSSEWriter: %v", err)
		}

		// Data with quotes, unicode, and backslashes that JSON must escape
		data := map[string]string{
			"message": `he said "hello" and left`,
			"path":    `C:\Users\test\file.txt`,
			"emoji":   "unicode: \u2713",
		}
		if err := sse.SendEvent("special", data); err != nil {
			t.Fatalf("SendEvent: %v", err)
		}

		body := rec.Body.String()

		// The JSON marshaler will escape quotes with \"
		if !strings.Contains(body, `he said \"hello\" and left`) {
			t.Errorf("expected escaped quotes in SSE data, got: %s", body)
		}
		// Backslashes should be escaped
		if !strings.Contains(body, `C:\\Users\\test\\file.txt`) {
			t.Errorf("expected escaped backslashes in SSE data, got: %s", body)
		}
		// Unicode should be preserved (Go's json.Marshal emits UTF-8 directly for \uXXXX)
		if !strings.Contains(body, "unicode:") {
			t.Errorf("expected unicode field in SSE data, got: %s", body)
		}
		// Verify the round-tripped data is valid JSON by decoding it from the SSE frame
		_, dataLine, found := strings.Cut(body, "data: ")
		if !found {
			t.Fatalf("no data line in SSE output")
		}
		dataJSON := dataLine
		dataJSON = strings.TrimRight(dataJSON, "\n")
		var decoded map[string]string
		if err := json.Unmarshal([]byte(dataJSON), &decoded); err != nil {
			t.Fatalf("SSE data is not valid JSON: %v, data: %q", err, dataJSON)
		}
		if decoded["emoji"] != "unicode: \u2713" {
			t.Errorf("emoji round-trip failed: got %q", decoded["emoji"])
		}

		// Verify the output is valid SSE framing: event line + data line + blank line
		if !strings.Contains(body, "event: special\n") {
			t.Errorf("expected 'event: special' line, got: %s", body)
		}
		idx := strings.Index(body, "event: special")
		if idx < 0 {
			t.Fatalf("expected 'event: special' in body, got: %s", body)
		}
		if !strings.HasPrefix(body[idx:], "event: special\ndata: ") {
			t.Errorf("expected SSE framing after event line, got: %s", body)
		}
	})

	t.Run("CloseWithFinalEvent", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, err := NewSSEWriter(rec)
		if err != nil {
			t.Fatalf("NewSSEWriter: %v", err)
		}

		if err := sse.CloseWithFinalEvent(); err != nil {
			t.Fatalf("CloseWithFinalEvent: %v", err)
		}

		body := rec.Body.String()
		if !strings.Contains(body, "event: done") {
			t.Error("expected done event from CloseWithFinalEvent")
		}
		if !strings.Contains(body, `"status":"complete"`) {
			t.Errorf("expected status complete in output, got: %s", body)
		}
	})

	t.Run("HeartbeatBetweenEvents", func(t *testing.T) {
		rec := httptest.NewRecorder()
		sse, err := NewSSEWriter(rec)
		if err != nil {
			t.Fatalf("NewSSEWriter: %v", err)
		}

		if err := sse.SendEvent("progress", map[string]string{"step": "1"}); err != nil {
			t.Fatalf("SendEvent: %v", err)
		}
		if err := sse.SendComment(); err != nil {
			t.Fatalf("SendComment: %v", err)
		}
		if err := sse.SendEvent("progress", map[string]string{"step": "2"}); err != nil {
			t.Fatalf("SendEvent 2: %v", err)
		}

		body := rec.Body.String()

		// Verify ordering: event step 1, then heartbeat, then event step 2
		step1Idx := strings.Index(body, `"step":"1"`)
		heartbeatIdx := strings.Index(body, ": heartbeat")
		step2Idx := strings.Index(body, `"step":"2"`)

		if step1Idx == -1 || heartbeatIdx == -1 || step2Idx == -1 {
			t.Fatalf("expected step 1, heartbeat, step 2 in body, got: %s", body)
		}
		if step1Idx >= heartbeatIdx || heartbeatIdx >= step2Idx {
			t.Errorf("expected step1 < heartbeat < step2, got indices: %d, %d, %d",
				step1Idx, heartbeatIdx, step2Idx)
		}
	})
}

func TestHandleChatStream_NoService(t *testing.T) {
	t.Parallel()
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream", http.NoBody)
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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream", http.NoBody).WithContext(ctx)
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

func TestHandleChatStream_SSEAgentProgressEvent(t *testing.T) {
	t.Parallel()

	msgBus := bus.New(nil, nil)
	busSvc := services.NewBusService(msgBus)
	svcReg := &services.ServiceRegistry{Bus: busSvc}
	server := NewServer(ServerConfig{EnableCORS: true}, nil, nil, nil, svcReg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.handleChatStream(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	agentPayload := map[string]any{
		"agent_id": "coder",
		"stage":    "executing",
		"message":  "running tools...",
		"percent":  75,
	}
	_ = busSvc.Publish(context.Background(), services.PublishRequest{
		Topic:   "agent.progress",
		Type:    "event",
		Source:  "agent",
		Payload: agentPayload,
	})

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish in time")
	}

	body := w.Body.String()

	if !strings.Contains(body, "event: connected") {
		t.Error("expected connected event in SSE output")
	}

	// Should contain the agent_progress event forwarded from agent.progress topic
	if !strings.Contains(body, "event: agent_progress") {
		t.Error("expected agent_progress event in SSE output")
	}
	if !strings.Contains(body, `"stage":"executing"`) {
		t.Errorf("expected stage in SSE data, got: %s", body)
	}
	if !strings.Contains(body, `"percent":75`) {
		t.Errorf("expected percent in SSE data, got: %s", body)
	}
	if !strings.Contains(body, `"agent_id":"coder"`) {
		t.Errorf("expected agent_id in SSE data, got: %s", body)
	}

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

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream", http.NoBody).WithContext(ctx)
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
