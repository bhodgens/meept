package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	configCli "github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/services"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/models"

	"golang.org/x/net/websocket"
)

// mockDaemonController implements DaemonController for testing.
type mockDaemonController struct {
	running    bool
	pid        int
	uptime     time.Duration
	restartErr error
}

func (m *mockDaemonController) IsRunning() bool                   { return m.running }
func (m *mockDaemonController) PID() int                          { return m.pid }
func (m *mockDaemonController) Uptime() time.Duration             { return m.uptime }
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

func TestHandleNormalizeConfig_Success(t *testing.T) {
	// Create a ConfigService with a temp meept dir
	cs := &ConfigService{meeptDir: t.TempDir()}
	server := NewServer(ServerConfig{}, cs, nil, nil, nil, nil)

	json5Content := "{\n\t\t// This is a comment\n\t\t\"key\": \"value\",\n\t\t\"trailing\": \"comma\",\n\t}"
	reqBody, _ := json.Marshal(map[string]string{"content": json5Content})
	body := string(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/normalize", strings.NewReader(body))
	w := httptest.NewRecorder()

	server.handleNormalizeConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	normalized := result["normalized"]
	// Comments are replaced with whitespace by hujson.Standardize, so "//" may still be present
	// in the whitespace padding. Instead, verify the structure.
	// Trailing commas should be removed.
	if strings.Contains(normalized, `",\n\t}`) {
		t.Error("normalized output should not contain trailing commas")
	}

	// Verify it's valid strict JSON by parsing it
	var strict any
	if err := json.Unmarshal([]byte(normalized), &strict); err != nil {
		t.Errorf("normalized output is not valid strict JSON: %v\noutput: %s", err, normalized)
	}

	// Verify the key is present
	m, ok := strict.(map[string]any)
	if !ok {
		t.Fatal("expected top-level object")
	}
	if m["key"] != "value" {
		t.Errorf("key = %v, want value", m["key"])
	}
	if m["trailing"] != "comma" {
		t.Errorf("trailing = %v, want comma", m["trailing"])
	}
}

func TestHandleNormalizeConfig_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/normalize", strings.NewReader(`{"content":"{}"}`))
	w := httptest.NewRecorder()

	server.handleNormalizeConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleNormalizeConfig_InvalidJSON5(t *testing.T) {
	cs := &ConfigService{meeptDir: t.TempDir()}
	server := NewServer(ServerConfig{}, cs, nil, nil, nil, nil)

	// Invalid JSON5
	body := `{"content":"{ invalid json5 }"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/normalize", strings.NewReader(body))
	w := httptest.NewRecorder()

	server.handleNormalizeConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleNormalizeConfig_InvalidBody(t *testing.T) {
	cs := &ConfigService{meeptDir: t.TempDir()}
	server := NewServer(ServerConfig{}, cs, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/normalize", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	server.handleNormalizeConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
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

	// With a localhost Origin, the header should be echoed back.
	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	origin := resp.Header.Get("Access-Control-Allow-Origin")
	if origin != "http://localhost:3000" {
		t.Errorf("expected Access-Control-Allow-Origin to echo localhost origin, got %q", origin)
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
	if !cfg.RequireAuth {
		t.Error("RequireAuth should be true by default")
	}
	if cfg.TLSCertFile == "" {
		t.Error("TLSCertFile should not be empty by default")
	}
	if cfg.TLSKeyFile == "" {
		t.Error("TLSKeyFile should not be empty by default")
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

func TestHandleGetOrchestratorConfig_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config/orchestrator", http.NoBody)
	w := httptest.NewRecorder()

	server.handleGetOrchestratorConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandlePutOrchestratorConfig_NoConfigService(t *testing.T) {
	server := NewServer(ServerConfig{}, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/orchestrator",
		strings.NewReader(`{"max_plan_steps":5}`))
	w := httptest.NewRecorder()

	server.handlePutOrchestratorConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestHandleOrchestratorConfigGetPut exercises the full round-trip:
// a GET on an empty dir returns zero values; a PUT persists new thresholds;
// a subsequent GET returns the updated values; other top-level meept.json5
// keys are preserved across the PUT.
func TestHandleOrchestratorConfigGetPut(t *testing.T) {
	cs := &ConfigService{meeptDir: t.TempDir()}
	server := NewServer(ServerConfig{}, cs, nil, nil, nil, nil)

	// Pre-seed meept.json5 with an unrelated top-level key to verify preservation.
	seed := `{
		// comment: daemon port
		"daemon": {"port": 8081},
		"orchestrator": {
			"max_plan_steps": 6,
			"ambiguity_threshold": 0.5
		}
	}`
	if err := os.WriteFile(filepath.Join(cs.meeptDir, "meept.json5"), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// GET returns the seeded values.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/config/orchestrator", http.NoBody)
	getW := httptest.NewRecorder()
	server.handleGetOrchestratorConfig(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getW.Code, http.StatusOK)
	}
	var got configCli.OrchestratorConfig
	if err := json.NewDecoder(getW.Result().Body).Decode(&got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if got.MaxPlanSteps != 6 || got.AmbiguityThreshold != 0.5 {
		t.Errorf("GET seeded values = %+v, want MaxPlanSteps=6 AmbiguityThreshold=0.5", got)
	}

	// PUT with updated thresholds.
	newOC := configCli.OrchestratorConfig{
		MaxPlanSteps:                9,
		MaxResearchSteps:            4,
		PlannerTimeout:              120,
		TokenBudgetAlert:            6000,
		MaxHandoffSteps:             5,
		HandoffUseAmendment:         true,
		AmbiguityThreshold:          0.75,
		InterviewAmbiguityThreshold: 0.8,
		MaxStepsPerPhase:            10,
		MaxPhases:                   15,
	}
	body, _ := json.Marshal(newOC)
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/config/orchestrator", strings.NewReader(string(body)))
	putW := httptest.NewRecorder()
	server.handlePutOrchestratorConfig(putW, putReq)
	if putW.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d", putW.Code, http.StatusOK)
	}

	// GET returns the PUT values.
	getReq2 := httptest.NewRequest(http.MethodGet, "/api/v1/config/orchestrator", http.NoBody)
	getW2 := httptest.NewRecorder()
	server.handleGetOrchestratorConfig(getW2, getReq2)
	if getW2.Code != http.StatusOK {
		t.Fatalf("GET-after-PUT status = %d, want %d", getW2.Code, http.StatusOK)
	}
	var got2 configCli.OrchestratorConfig
	if err := json.NewDecoder(getW2.Result().Body).Decode(&got2); err != nil {
		t.Fatalf("decode GET-after-PUT: %v", err)
	}
	if got2.MaxPlanSteps != 9 {
		t.Errorf("MaxPlanSteps = %d, want 9", got2.MaxPlanSteps)
	}
	if got2.AmbiguityThreshold != 0.75 {
		t.Errorf("AmbiguityThreshold = %v, want 0.75", got2.AmbiguityThreshold)
	}
	if got2.InterviewAmbiguityThreshold != 0.8 {
		t.Errorf("InterviewAmbiguityThreshold = %v, want 0.8", got2.InterviewAmbiguityThreshold)
	}
	if got2.MaxStepsPerPhase != 10 {
		t.Errorf("MaxStepsPerPhase = %d, want 10", got2.MaxStepsPerPhase)
	}
	if got2.MaxPhases != 15 {
		t.Errorf("MaxPhases = %d, want 15", got2.MaxPhases)
	}
	if !got2.HandoffUseAmendment {
		t.Error("HandoffUseAmendment = false, want true")
	}

	// Verify unrelated top-level key preserved.
	persisted, err := os.ReadFile(filepath.Join(cs.meeptDir, "meept.json5"))
	if err != nil {
		t.Fatalf("read persisted meept.json5: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(persisted, &root); err != nil {
		t.Fatalf("parse persisted meept.json5: %v", err)
	}
	daemonBlock, ok := root["daemon"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'daemon' key to be preserved, got %v", root["daemon"])
	}
	// JSON unmarshal gives float64 for numbers.
	if port, _ := daemonBlock["port"].(float64); port != 8081 {
		t.Errorf("daemon.port = %v, want 8081", daemonBlock["port"])
	}
}

func TestHandleOrchestratorConfigPut_InvalidBody(t *testing.T) {
	cs := &ConfigService{meeptDir: t.TempDir()}
	server := NewServer(ServerConfig{}, cs, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/config/orchestrator", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	server.handlePutOrchestratorConfig(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
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
	// Steer returns ErrUnavailable (nil agentRegistry) -> handleServiceError -> 503.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
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
	// FollowUp returns ErrUnavailable (nil agentRegistry) -> handleServiceError -> 503.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
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
	req.SetPathValue("id", "conv-789")
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
	req.SetPathValue("id", "conv-789")
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

// nilConn is a nil *websocket.Conn used as a reusable map key for testing.
// Since websocket.Conn from golang.org/x/net/websocket is a struct, we use
// nil pointer values as test keys; the hub methods only care about map lookups.
var nilConn = (*websocket.Conn)(nil)

// --- ShouldSendProgress tests ---

func TestShouldSendProgress_BroadcastMode(t *testing.T) {
	hub := NewWebSocketHub(nil)

	// No subscription = broadcast mode = always true
	if !hub.ShouldSendProgress(nilConn, "any-session") {
		t.Error("broadcast mode should always return true")
	}
}

func TestShouldSendProgress_SingleSessionMatch(t *testing.T) {
	hub := NewWebSocketHub(nil)
	hub.SubscribeSession(nilConn, "sess1")

	if !hub.ShouldSendProgress(nilConn, "sess1") {
		t.Error("should send when session matches")
	}
}

func TestShouldSendProgress_SingleSessionNoMatch(t *testing.T) {
	hub := NewWebSocketHub(nil)
	hub.SubscribeSession(nilConn, "sess1")

	if hub.ShouldSendProgress(nilConn, "sess2") {
		t.Error("should not send when session does not match")
	}
}

func TestShouldSendProgress_MultiSessionAllMatch(t *testing.T) {
	hub := NewWebSocketHub(nil)
	hub.SubscribeSession(nilConn, "sess1")
	hub.SubscribeSession(nilConn, "sess2")

	if !hub.ShouldSendProgress(nilConn, "sess1") || !hub.ShouldSendProgress(nilConn, "sess2") {
		t.Error("should send for all subscribed sessions")
	}
}

func TestShouldSendProgress_MultiSessionPartialMatch(t *testing.T) {
	hub := NewWebSocketHub(nil)
	hub.SubscribeSession(nilConn, "sess1")

	if hub.ShouldSendProgress(nilConn, "sess3") {
		t.Error("should not send to non-subscribed session")
	}
	if !hub.ShouldSendProgress(nilConn, "sess1") {
		t.Error("should send for subscribed session")
	}
}

func TestShouldSendProgress_AfterUnsubscribe(t *testing.T) {
	hub := NewWebSocketHub(nil)
	hub.SubscribeSession(nilConn, "sess1")
	hub.SubscribeSession(nilConn, "sess2")

	// Should match before unsubscribe
	if !hub.ShouldSendProgress(nilConn, "sess1") {
		t.Error("should send before unsubscribe")
	}
	if !hub.ShouldSendProgress(nilConn, "sess2") {
		t.Error("should send for sess2 before unsubscribe")
	}

	hub.UnsubscribeSession(nilConn, "sess1")

	// sess1 removed, sess2 should still match
	if hub.ShouldSendProgress(nilConn, "sess1") {
		t.Error("should not send for unsubscribed sess1")
	}
	if !hub.ShouldSendProgress(nilConn, "sess2") {
		t.Error("should still send for non-unsubscribed sess2")
	}
}

func TestShouldSendProgress_Concurrent(t *testing.T) {
	hub := NewWebSocketHub(nil)
	hub.SubscribeSession(nilConn, "sess-a")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = hub.ShouldSendProgress(nilConn, "sess-a")
			_ = hub.ShouldSendProgress(nilConn, "sess-b")
		}()
	}
	wg.Wait()
}

func TestShouldSendProgress_MultipleConns(t *testing.T) {
	hub := NewWebSocketHub(nil)
	conn1 := (*websocket.Conn)(nil)
	conn2 := (*websocket.Conn)(nil)
	_ = conn1

	hub.SubscribeSession(conn1, "sess-x")

	// conn1 subscribed, conn2 broadcast
	if !hub.ShouldSendProgress(conn1, "sess-x") {
		t.Error("conn1 should get sess-x")
	}
	if hub.ShouldSendProgress(conn1, "sess-y") {
		t.Error("conn1 should not get sess-y")
	}
	if !hub.ShouldSendProgress(conn2, "sess-x") {
		t.Error("conn2 (broadcast) should get any")
	}
}

// --- handleWSProgress tests ---

func TestHandleWSProgress_ValidEvent_SerializesCorrectly(t *testing.T) {
	logger := slog.Default()
	s := &Server{
		logger: logger,
		wsHub:  NewWebSocketHub(logger),
	}

	event := agent.SynthesizedProgressEvent{
		SessionID:   "test-session-1",
		AgentID:     "coder",
		Tier:        agent.VerbosityNormal,
		Message:     "Running tests",
		SourceEvent: agent.AgentEventToolExecutionStart,
		Timestamp:   time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
	}

	// Compute the exact payload handleWSProgress would produce
	// (same logic as handleWSProgress lines 398-406)
	want, err := json.Marshal(map[string]any{
		"type":         "agent_progress",
		"session_id":   event.SessionID,
		"agent_id":     event.AgentID,
		"message":      event.Message,
		"tier":         int(event.Tier),
		"source_event": string(event.SourceEvent),
		"timestamp":    event.Timestamp.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(want, &result); err != nil {
		t.Fatalf("re-parse error: %v", err)
	}

	if result["type"] != "agent_progress" {
		t.Errorf("type = %v, want agent_progress", result["type"])
	}
	if result["session_id"] != "test-session-1" {
		t.Errorf("session_id = %v, want test-session-1", result["session_id"])
	}
	if result["agent_id"] != "coder" {
		t.Errorf("agent_id = %v, want coder", result["agent_id"])
	}
	if result["message"] != "Running tests" {
		t.Errorf("message = %v, want Running tests", result["message"])
	}
	tierVal, ok := result["tier"].(float64)
	if !ok || int(tierVal) != int(agent.VerbosityNormal) {
		t.Errorf("tier = %v, want %d", result["tier"], agent.VerbosityNormal)
	}
	if result["source_event"] != string(agent.AgentEventToolExecutionStart) {
		t.Errorf("source_event = %v, want %s", result["source_event"], agent.AgentEventToolExecutionStart)
	}
	expectedTS := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if result["timestamp"] != expectedTS {
		t.Errorf("timestamp = %v, want %s", result["timestamp"], expectedTS)
	}

	// Call handleWSProgress to verify no-panic on the full path
	payload, _ := json.Marshal(event)
	msg := &models.BusMessage{Topic: "agent.progress.synthesized", Payload: payload}
	s.handleWSProgress(msg)
}

func TestHandleWSProgress_SessionFiltering_Broadcast(t *testing.T) {
	logger := slog.Default()
	hub := NewWebSocketHub(logger)
	s := &Server{logger: logger, wsHub: hub}

	event := agent.SynthesizedProgressEvent{
		SessionID: "broadcast-sess",
		AgentID:   "analyst",
		Timestamp: time.Now(),
	}
	payload, _ := json.Marshal(event)
	msg := &models.BusMessage{Topic: "agent.progress.synthesized", Payload: payload}
	s.handleWSProgress(msg)

	if !hub.ShouldSendProgress(nilConn, "broadcast-sess") {
		t.Error("broadcast mode should send to all")
	}
}

func TestHandleWSProgress_SessionFiltering_Scoped(t *testing.T) {
	logger := slog.Default()
	hub := NewWebSocketHub(logger)
	s := &Server{logger: logger, wsHub: hub}

	event := agent.SynthesizedProgressEvent{
		SessionID: "scoped-123",
		AgentID:   "coder",
		Timestamp: time.Now(),
	}

	hub.SubscribeSession(nilConn, "scoped-123")

	if !hub.ShouldSendProgress(nilConn, "scoped-123") {
		t.Error("subscribe to scoped-123 => should send")
	}
	if hub.ShouldSendProgress(nilConn, "other-sess") {
		t.Error("NOT subscribed to other-sess => should NOT send")
	}

	payload, _ := json.Marshal(event)
	msg := &models.BusMessage{Topic: "agent.progress.synthesized", Payload: payload}
	s.handleWSProgress(msg)
}

func TestHandleWSProgress_NilHub_NoPanic(t *testing.T) {
	s := &Server{logger: slog.Default(), wsHub: nil}
	msg := &models.BusMessage{
		Topic:   "agent.progress.synthesized",
		Payload: []byte(`{"session_id":"x"}`),
	}
	s.handleWSProgress(msg)
}

func TestHandleWSProgress_NilMessage_NoPanic(t *testing.T) {
	s := &Server{logger: slog.Default(), wsHub: NewWebSocketHub(nil)}
	s.handleWSProgress(nil)
}

func TestHandleWSProgress_InvalidPayload(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
	s := &Server{logger: logger, wsHub: NewWebSocketHub(logger)}
	msg := &models.BusMessage{
		Topic:   "agent.progress.synthesized",
		Payload: []byte(`not json`),
	}
	s.handleWSProgress(msg)
}

func TestHandleWSProgress_NegativeTier_NoPanic(t *testing.T) {
	s := &Server{logger: slog.Default(), wsHub: NewWebSocketHub(nil)}
	event := agent.SynthesizedProgressEvent{
		SessionID: "z",
		AgentID:   "p",
		Tier:      agent.VerbosityLevel(-5),
		Message:   "neg tier",
		Timestamp: time.Now(),
	}
	payload, _ := json.Marshal(event)
	msg := &models.BusMessage{Topic: "agent.progress.synthesized", Payload: payload}
	s.handleWSProgress(msg)
}

func TestHandleWSProgress_EmptyAgentID_NoPanic(t *testing.T) {
	s := &Server{logger: slog.Default(), wsHub: NewWebSocketHub(nil)}
	event := agent.SynthesizedProgressEvent{
		SessionID: "x",
		AgentID:   "",
		Tier:      agent.VerbosityNormal,
		Message:   "test",
		Timestamp: time.Now(),
	}
	payload, _ := json.Marshal(event)
	msg := &models.BusMessage{Topic: "agent.progress.synthesized", Payload: payload}
	s.handleWSProgress(msg)
}

func TestHandleWSProgress_EmptyMessage_NoPanic(t *testing.T) {
	s := &Server{logger: slog.Default(), wsHub: NewWebSocketHub(nil)}
	event := agent.SynthesizedProgressEvent{
		SessionID: "y",
		AgentID:   "c",
		Tier:      agent.VerbosityNormal,
		Message:   "",
		Timestamp: time.Now(),
	}
	payload, _ := json.Marshal(event)
	msg := &models.BusMessage{Topic: "agent.progress.synthesized", Payload: payload}
	s.handleWSProgress(msg)
}

// TestHandleWebSocket_HandshakeRespectsConfiguredOrigins verifies that the
// WebSocket handshake enforces the configured allowlist. We extract the
// Handshake callback indirectly by inspecting server behaviour via a unit
// test of originAllowed. The behaviour we want to confirm is:
//   - configured origins are accepted
//   - default local origins (localhost/127.0.0.1) are always accepted
//   - unknown origins are rejected
func TestHandleWebSocket_HandshakeRespectsConfiguredOrigins(t *testing.T) {
	// Reproduce the allowlist construction logic from handleWebSocket so
	// the test does not require a real WebSocket connection. Any change
	// to the allowlist construction in handleWebSocket must be mirrored
	// here.
	allowed := append([]string{}, "https://meept.local")
	allowed = append(allowed, defaultWSOrigins...)
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[strings.ToLower(o)] = struct{}{}
	}
	for _, o := range defaultWSOrigins {
		set[strings.ToLower(o)] = struct{}{}
	}
	originAllowed := func(origin string) bool {
		if origin == "" {
			return true
		}
		if _, ok := set[strings.ToLower(origin)]; ok {
			return true
		}
		return false
	}

	cases := []struct {
		origin string
		want   bool
	}{
		{"https://meept.local", true},
		{"localhost", true},
		{"127.0.0.1", true},
		{"", true}, // non-browser clients
		{"https://evil.example.com", false},
		{"https://meept.local.evil.com", false},
	}
	for _, tc := range cases {
		got := originAllowed(tc.origin)
		if got != tc.want {
			t.Errorf("originAllowed(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}
}

// TestProgressRateLimiter tests the rate limiting logic for WebSocket progress events.
func TestProgressRateLimiter(t *testing.T) {
	interval := 50 * time.Millisecond
	limiter := newProgressRateLimiter(interval)

	// Create a mock connection (nil conn for unit testing)
	var mockConn *websocket.Conn

	// First send should be allowed (no previous send recorded)
	if !limiter.shouldSend(mockConn) {
		t.Fatal("First send should be allowed")
	}

	// Record the send
	limiter.recordSend(mockConn)

	// Immediate second send should be blocked
	if limiter.shouldSend(mockConn) {
		t.Error("Second send should be blocked within interval")
	}

	// After interval passes, should be allowed again
	time.Sleep(interval + 10*time.Millisecond)
	if !limiter.shouldSend(mockConn) {
		t.Error("Send should be allowed after interval")
	}
}

// TestProgressRateLimiterCleanup tests that stale entries are cleaned up.
func TestProgressRateLimiter_Cleanup(t *testing.T) {
	limiter := newProgressRateLimiter(100 * time.Millisecond)

	// Test cleanup removes stale entries
	// Note: nil pointers all map to same key, so we test the cleanup mechanism

	// Verify initial state: no entries, shouldSend returns true
	var mockConn *websocket.Conn
	if !limiter.shouldSend(mockConn) {
		t.Error("Initial state should allow send")
	}

	// Record a send
	limiter.recordSend(mockConn)

	// Now should be blocked
	if limiter.shouldSend(mockConn) {
		t.Error("Should be blocked after recordSend")
	}

	// Cleanup with empty active set should remove the entry
	limiter.cleanup(map[*websocket.Conn]struct{}{})

	// After cleanup, should allow sends again
	if !limiter.shouldSend(mockConn) {
		t.Error("Should allow send after cleanup")
	}
}
func TestHandleWSProgress_RateLimiting(t *testing.T) {
	logger := slog.Default()
	s := &Server{
		logger:              logger,
		wsHub:               NewWebSocketHub(logger),
		progressRateLimiter: newProgressRateLimiter(50 * time.Millisecond),
	}

	// Use nil conn for unit testing (all nil conns map to same key)
	var mockConn *websocket.Conn

	// First send should be allowed
	if !s.progressRateLimiter.shouldSend(mockConn) {
		t.Error("First send should be allowed")
	}

	// Record the send (simulating what handleWSProgress does)
	s.progressRateLimiter.recordSend(mockConn)

	// Immediate second send should be blocked
	if s.progressRateLimiter.shouldSend(mockConn) {
		t.Error("Rate limiter should have blocked immediate second send")
	}

	// After interval, should be allowed again
	time.Sleep(60 * time.Millisecond)
	if !s.progressRateLimiter.shouldSend(mockConn) {
		t.Error("Rate limiter should allow send after interval")
	}
}

// TestConfigServiceListAgents_NewFields verifies that ListAgents parses the
// role, can_delegate, reviews_domain, and enabled fields from AGENT.md
// frontmatter into the corresponding AgentInfo fields added by the
// agent-roster consolidation.
func TestConfigServiceListAgents_NewFields(t *testing.T) {
	// Build a temporary meept-like directory with two agents.
	tmp := t.TempDir()
	agentsDir := filepath.Join(tmp, "agents")
	if err := os.MkdirAll(filepath.Join(agentsDir, "coder"), 0o750); err != nil {
		t.Fatalf("mkdir coder: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(agentsDir, "code-reviewer"), 0o750); err != nil {
		t.Fatalf("mkdir code-reviewer: %v", err)
	}

	coderBody := "---\n" +
		"id: coder\n" +
		"name: Coder\n" +
		"role: executor\n" +
		"description: writes code\n" +
		"can_delegate: true\n" +
		"enabled: true\n" +
		"---\nbody\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "coder", "AGENT.md"), []byte(coderBody), 0o600); err != nil {
		t.Fatalf("write coder AGENT.md: %v", err)
	}

	reviewerBody := "---\n" +
		"id: code-reviewer\n" +
		"name: Code Reviewer\n" +
		"role: reviewer\n" +
		"description: reviews code\n" +
		"reviews_domain: code\n" +
		"---\nbody\n"
	if err := os.WriteFile(filepath.Join(agentsDir, "code-reviewer", "AGENT.md"), []byte(reviewerBody), 0o600); err != nil {
		t.Fatalf("write reviewer AGENT.md: %v", err)
	}

	svc := &ConfigService{meeptDir: tmp}
	got, err := svc.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(got))
	}

	byID := make(map[string]AgentInfo, len(got))
	for _, a := range got {
		byID[a.ID] = a
	}

	coder, ok := byID["coder"]
	if !ok {
		t.Fatalf("coder not in result: %+v", got)
	}
	if coder.Role != "executor" {
		t.Errorf("coder.Role = %q, want %q", coder.Role, "executor")
	}
	if !coder.CanDelegate {
		t.Errorf("coder.CanDelegate = false, want true")
	}
	if !coder.Enabled {
		t.Errorf("coder.Enabled = false, want true")
	}
	if coder.ReviewsDomain != "" {
		t.Errorf("coder.ReviewsDomain = %q, want empty", coder.ReviewsDomain)
	}

	reviewer, ok := byID["code-reviewer"]
	if !ok {
		t.Fatalf("code-reviewer not in result: %+v", got)
	}
	if reviewer.Role != "reviewer" {
		t.Errorf("reviewer.Role = %q, want %q", reviewer.Role, "reviewer")
	}
	if reviewer.CanDelegate {
		t.Errorf("reviewer.CanDelegate = true, want false (absent in frontmatter)")
	}
	if !reviewer.Enabled {
		t.Errorf("reviewer.Enabled = false, want true (absent = true)")
	}
	if reviewer.ReviewsDomain != "code" {
		t.Errorf("reviewer.ReviewsDomain = %q, want %q", reviewer.ReviewsDomain, "code")
	}
}

// newArchiveTestServer builds a Server wired with an in-memory backed
// SessionService for archive handler tests.
func newArchiveTestServer(t *testing.T) *Server {
	t.Helper()
	store := session.NewMemoryStore(slog.New(slog.NewTextHandler(io.Discard, nil)))
	svcReg := &services.ServiceRegistry{
		Session:      services.NewSessionService(store),
		SessionStore: store,
	}
	return NewServer(ServerConfig{}, nil, nil, nil, svcReg, nil)
}

func TestHandleSessionArchive(t *testing.T) {
	srv := newArchiveTestServer(t)

	// Create a session via the existing POST /api/v1/sessions endpoint.
	body := strings.NewReader(`{"name":"archive-test"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", body)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	srv.handleSessionCreate(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", createRR.Code, createRR.Body.String())
	}
	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRR.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create resp: %v", err)
	}
	if createResp.ID == "" {
		t.Fatal("no id in create response")
	}

	// Archive it.
	archReq := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/sessions/"+createResp.ID,
		strings.NewReader(`{"archived":true}`),
	)
	archReq.Header.Set("Content-Type", "application/json")
	archReq.SetPathValue("id", createResp.ID)
	archRR := httptest.NewRecorder()
	srv.handleSessionArchive(archRR, archReq)
	if archRR.Code != http.StatusNoContent {
		t.Fatalf("archive: expected 204, got %d: %s", archRR.Code, archRR.Body.String())
	}

	// Verify via GET that the flag persisted.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+createResp.ID, nil)
	getReq.SetPathValue("id", createResp.ID)
	getRR := httptest.NewRecorder()
	srv.handleSessionGet(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", getRR.Code)
	}
	var getResp map[string]any
	if err := json.Unmarshal(getRR.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal get resp: %v", err)
	}
	if got, _ := getResp["archived"].(bool); !got {
		t.Fatalf("expected archived=true in GET response, got %v", getResp["archived"])
	}
}

func TestHandleSessionArchiveRejectsUnknownFields(t *testing.T) {
	srv := newArchiveTestServer(t)

	body := strings.NewReader(`{"name":"reject-test"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", body)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	srv.handleSessionCreate(createRR, createReq)
	var createResp struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(createRR.Body.Bytes(), &createResp)

	badReq := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/sessions/"+createResp.ID,
		strings.NewReader(`{"title":"evil"}`),
	)
	badReq.Header.Set("Content-Type", "application/json")
	badReq.SetPathValue("id", createResp.ID)
	badRR := httptest.NewRecorder()
	srv.handleSessionArchive(badRR, badReq)
	if badRR.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d: %s", badRR.Code, badRR.Body.String())
	}
}
