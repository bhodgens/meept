package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
)

// newUsageStore builds a real metrics.Store seeded with two llm_calls rows and
// two agent task outcomes. It satisfies MetricsService and
// ModelAgentUsageProvider, exercising the type-assertion path.
func newUsageStore(t *testing.T) *metrics.Store {
	t.Helper()
	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now()
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		AgentID: "chat", TokensSent: 100, TokensRecv: 10, LatencyMs: 100})
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: now, Provider: "prov-a", ModelID: "m1",
		AgentID: "chat", TokensSent: 200, TokensRecv: 20, LatencyMs: 300})

	if _, err := store.DB().Exec(`CREATE TABLE IF NOT EXISTS agent_task_outcomes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
		task_id TEXT NOT NULL, agent_id TEXT NOT NULL, success BOOLEAN)`); err != nil {
		t.Fatalf("create agent_task_outcomes: %v", err)
	}
	for _, r := range []struct {
		task  string
		ok    int
	}{{"t1", 1}, {"t2", 1}} {
		if _, err := store.DB().Exec(
			`INSERT INTO agent_task_outcomes (task_id, agent_id, success) VALUES (?, 'chat', ?)`,
			r.task, r.ok); err != nil {
			t.Fatalf("insert outcome: %v", err)
		}
	}
	return store
}

func doLiveMetrics(t *testing.T, s *Server, mux *http.ServeMux) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/live", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body: %s", err, w.Body.String())
	}
	t.Logf("GET /api/v1/metrics/live -> %s", w.Body.String())
	return body
}

// TestLiveMetrics_ModelAgentUsageFromStore verifies the new keys, their types,
// and that existing snapshot keys survive.
func TestLiveMetrics_ModelAgentUsageFromStore(t *testing.T) {
	store := newUsageStore(t)
	s := NewServer(ServerConfig{}, nil, nil, store, nil, nil)
	mux := http.NewServeMux()
	s.setupRESTRoutes(mux)

	body := doLiveMetrics(t, s, mux)

	// Existing keys must remain.
	for _, k := range []string{"timestamp", "active_agents", "requests_per_sec",
		"token_usage_rate", "queue_depth", "model_failovers"} {
		if _, ok := body[k]; !ok {
			t.Errorf("missing existing key %q", k)
		}
	}

	models, ok := body["models"].([]any)
	if !ok {
		t.Fatalf("models is %T, want array", body["models"])
	}
	if len(models) != 1 {
		t.Fatalf("models = %v, want 1 row", models)
	}
	m := models[0].(map[string]any)
	if m["id"] != "prov-a/m1" {
		t.Errorf("models[0].id = %v, want prov-a/m1", m["id"])
	}
	if m["calls"] != float64(2) || m["tokens_in"] != float64(300) ||
		m["tokens_out"] != float64(30) || m["avg_latency_ms"] != float64(200) {
		t.Errorf("models[0] = %v, want calls=2 in=300 out=30 avg=200", m)
	}

	agents, ok := body["agents"].([]any)
	if !ok {
		t.Fatalf("agents is %T, want array", body["agents"])
	}
	if len(agents) != 1 {
		t.Fatalf("agents = %v, want 1 row", agents)
	}
	a := agents[0].(map[string]any)
	if a["id"] != "chat" || a["tasks_completed"] != float64(2) || a["tasks_failed"] != float64(0) {
		t.Errorf("agents[0] = %v, want id=chat completed=2 failed=0", a)
	}
	if _, ok := a["state"]; !ok {
		t.Errorf("agents[0] missing state key: %v", a)
	}

	totals, ok := body["totals"].(map[string]any)
	if !ok {
		t.Fatalf("totals is %T, want object", body["totals"])
	}
	if totals["calls"] != float64(2) || totals["tokens_in"] != float64(300) ||
		totals["tokens_out"] != float64(30) {
		t.Errorf("totals = %v, want calls=2 in=300 out=30", totals)
	}
}

// TestLiveMetrics_EmptyArraysNotNull verifies arrays are emitted as [] (never
// null) when there is no usage data and no reachable metrics.db.
func TestLiveMetrics_EmptyArraysNotNull(t *testing.T) {
	t.Setenv("MEEPT_HOME", t.TempDir()) // no metrics.db here
	svc := &mockMetricsService{
		liveMetrics: &metrics.LiveMetricsSnapshot{Timestamp: time.Now(), ActiveAgents: 1},
	}
	s := NewServer(ServerConfig{}, nil, nil, svc, nil, nil)
	mux := http.NewServeMux()
	s.setupRESTRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/live", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	raw := w.Body.String()
	for _, want := range []string{`"models":[]`, `"agents":[]`, `"totals":`} {
		if !strings.Contains(raw, want) {
			t.Errorf("response %s missing %q", raw, want)
		}
	}
	if strings.Contains(raw, `"models":null`) || strings.Contains(raw, `"agents":null`) {
		t.Errorf("response emitted null arrays: %s", raw)
	}
}

// TestLiveMetrics_FallsBackToMetricsDB exercises the read-only metrics.db
// fallback used when the metrics service does not implement the provider
// interface (the production metricsStoreWrapper).
func TestLiveMetrics_FallsBackToMetricsDB(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	store, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  filepath.Join(home, "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	store.RecordLLMCall(metrics.LLMCallRecord{Timestamp: time.Now(), Provider: "p-ro", ModelID: "m9",
		AgentID: "chat", TokensSent: 11, TokensRecv: 22, LatencyMs: 5})
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	svc := &mockMetricsService{liveMetrics: &metrics.LiveMetricsSnapshot{Timestamp: time.Now()}}
	s := NewServer(ServerConfig{}, nil, nil, svc, nil, nil)
	mux := http.NewServeMux()
	s.setupRESTRoutes(mux)

	body := doLiveMetrics(t, s, mux)
	models, _ := body["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %v, want 1 row from metrics.db fallback", models)
	}
	if got := models[0].(map[string]any)["id"]; got != "p-ro/m9" {
		t.Errorf("models[0].id = %v, want p-ro/m9", got)
	}
}
