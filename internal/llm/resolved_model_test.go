package llm

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// resolvedModelCapture records the "model" field of every chat request a test
// server receives.
type resolvedModelCapture struct {
	mu     sync.Mutex
	models []string
	hits   int32
}

func (c *resolvedModelCapture) record(model string) {
	atomic.AddInt32(&c.hits, 1)
	c.mu.Lock()
	c.models = append(c.models, model)
	c.mu.Unlock()
}

func (c *resolvedModelCapture) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.models) == 0 {
		return ""
	}
	return c.models[len(c.models)-1]
}

func (c *resolvedModelCapture) count() int { return int(atomic.LoadInt32(&c.hits)) }

// completionServer serves a minimal OpenAI-compatible completion while
// capturing the requested model id.
func completionServer(cap *resolvedModelCapture) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cap.record(body.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
}

// TestClient_ResolvedModelReachesRequestAndLedger pins bug 1 + bug 2 at the
// Client layer: a request-scoped resolver selection (WithResolvedModel) puts
// the RESOLVED model id on the wire and records the provider/model that
// actually served the call into metrics.db llm_calls — instead of the
// client's configured default. The client's stored config must stay
// untouched (the selection is per-request, never shared state).
func TestClient_ResolvedModelReachesRequestAndLedger(t *testing.T) {
	cap := &resolvedModelCapture{}
	server := completionServer(cap)
	defer server.Close()

	store, dbPath := newUsageTestStoreAtPath(t, filepath.Join(t.TempDir(), "metrics.db"))
	db := openLLMCallsSidecar(t, dbPath)

	// The client's configured default is NOT the model the resolver chose.
	client := NewClient(&ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "local",
		ModelID:    "lfm-8b-mlx-4bit",
	})
	client.SetUsageStore(store)

	resolved := &ModelConfig{
		ProviderID: "local",
		ModelID:    "/Volumes/LLMs/LiquidAI/LFM2.5-8B-A1B-GGUF/LFM2.5-8B-A1B-Q4_K_M.gguf",
	}
	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithResolvedModel(resolved), WithAgentScope("coder")); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got := cap.last(); got != resolved.ModelID {
		t.Errorf("wire model = %q, want the resolved alias model %q", got, resolved.ModelID)
	}
	if got := client.Config().ModelID; got != "lfm-8b-mlx-4bit" {
		t.Errorf("client config mutated by a request-scoped selection: ModelID = %q, want the configured default", got)
	}

	var rows []struct {
		Provider string `db:"provider"`
		ModelID  string `db:"model_id"`
		AgentID  string `db:"agent_id"`
	}
	pollUntil(t, func() bool {
		rows = nil
		return db.Select(&rows, `SELECT provider, model_id, agent_id FROM llm_calls`) == nil && len(rows) == 1
	})
	if len(rows) != 1 {
		t.Fatalf("expected 1 llm_calls row, got %d", len(rows))
	}
	if rows[0].Provider != "local" || rows[0].ModelID != resolved.ModelID {
		t.Errorf("llm_calls row = %s|%s, want local|%s (the model that served the call)",
			rows[0].Provider, rows[0].ModelID, resolved.ModelID)
	}
	if rows[0].AgentID != "coder" {
		t.Errorf("agent attribution lost: agent_id = %q, want coder", rows[0].AgentID)
	}
}

// TestProviderManager_ResolvedModelRoutesToNamedProviderAndLedger pins the
// end-to-end bug-1/bug-2 fix through the seam the daemon actually wires: a
// request-scoped selection must route THIS call to the named provider (first,
// keeping the rest as failover tail) and the ledger must name that provider.
// The control call without the option proves health/priority ordering is
// unchanged for turns that do not resolve an alias.
func TestProviderManager_ResolvedModelRoutesToNamedProviderAndLedger(t *testing.T) {
	agnesCap := &resolvedModelCapture{}
	agnes := completionServer(agnesCap)
	defer agnes.Close()
	localCap := &resolvedModelCapture{}
	local := completionServer(localCap)
	defer local.Close()

	store, dbPath := newUsageTestStoreAtPath(t, filepath.Join(t.TempDir(), "metrics.db"))
	db := openLLMCallsSidecar(t, dbPath)

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "agnes", ModelID: "agnes-2.5-flash", BaseURL: agnes.URL},
			{ProviderID: "local", ModelID: "lfm-8b-mlx-4bit", BaseURL: local.URL},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	pm.SetUsageStore(store)

	resolved := &ModelConfig{ProviderID: "local", ModelID: "LFM2.5-8B-A1B-Q4_K_M.gguf"}
	if _, err := pm.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithResolvedModel(resolved), WithAgentScope("coder")); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if agnesCap.count() != 0 {
		t.Errorf("agnes received %d calls, want 0 (the request selected the local provider)", agnesCap.count())
	}
	if localCap.count() != 1 {
		t.Fatalf("local received %d calls, want 1", localCap.count())
	}
	if got := localCap.last(); got != resolved.ModelID {
		t.Errorf("local wire model = %q, want %q", got, resolved.ModelID)
	}

	var rows []struct {
		Provider string `db:"provider"`
		ModelID  string `db:"model_id"`
	}
	pollUntil(t, func() bool {
		rows = nil
		return db.Select(&rows, `SELECT provider, model_id FROM llm_calls`) == nil && len(rows) == 1
	})
	if len(rows) != 1 {
		t.Fatalf("expected 1 llm_calls row, got %d (%+v)", len(rows), rows)
	}
	if rows[0].Provider != "local" || rows[0].ModelID != resolved.ModelID {
		t.Errorf("llm_calls row = %s|%s, want local|%s", rows[0].Provider, rows[0].ModelID, resolved.ModelID)
	}

	// Control: WITHOUT a request-scoped selection the manager still routes by
	// health/priority (agnes first) — failover/cost ordering is untouched.
	if _, err := pm.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("control Chat: %v", err)
	}
	if agnesCap.count() != 1 {
		t.Errorf("control call: agnes received %d calls, want 1 (default ordering preserved)", agnesCap.count())
	}
}

// TestProviderManager_ResolvedModelPreferredProviderFailsOver keeps the
// failover contract: if the provider named by the request-scoped selection is
// unhealthy, the call still reaches the next provider instead of failing
// outright.
func TestProviderManager_ResolvedModelPreferredProviderFailsOver(t *testing.T) {
	backupCap := &resolvedModelCapture{}
	backup := completionServer(backupCap)
	defer backup.Close()

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "agnes", ModelID: "agnes-2.5-flash", BaseURL: "http://127.0.0.1:0"},
			{ProviderID: "backup", ModelID: "backup-model", BaseURL: backup.URL},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	// Mark the preferred provider unhealthy; the manager must skip it when a
	// healthy alternative exists and still serve from the backup.
	pm.mu.Lock()
	pm.providers[0].Health.Status = ProviderStatusUnhealthy
	pm.mu.Unlock()

	resp, err := pm.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithResolvedModel(&ModelConfig{ProviderID: "agnes", ModelID: "agnes-2.5-flash"}))
	if err != nil {
		t.Fatalf("Chat should fail over: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if backupCap.count() != 1 {
		t.Errorf("backup received %d calls, want 1 (failover preserved)", backupCap.count())
	}
	if got := backupCap.last(); got != "backup-model" {
		t.Errorf("backup wire model = %q, want its own configured model (the selection belongs to agnes)", got)
	}
}
