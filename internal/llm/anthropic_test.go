package llm

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestAnthropicClient_AdaptiveTimeout verifies that when a
// *metrics.Calculator is configured, the client consults it before issuing
// a request. The timeout is applied via context.WithTimeout (LLM-3 FIX:
// not by mutating the shared httpClient.Timeout) so concurrent calls
// are safe. The concrete Calculator returns the static default while the
// store is in warmup; we assert the context received the correct deadline.
func TestAnthropicClient_AdaptiveTimeout(t *testing.T) {

	// Capture the deadline from the HTTP request's context.
	var capturedDeadline time.Time
	var hasDeadline bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		hasDeadline = ok
		if ok {
			capturedDeadline = deadline
		}
		resp := map[string]any{
			"id":          "m_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-test",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer server.Close()

	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    server.URL,
		APIKey:     "test",
		MaxTokens:  128,
	}

	storeCfg := metrics.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "metrics.db"),
		RetentionDays:    1,
		StatsWindowHours: 1,
		RefreshInterval:  time.Minute,
	}
	store, err := metrics.NewStore(storeCfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	calc := metrics.NewCalculator(store, metrics.AdaptiveTimeoutConfig{
		Enabled:        true,
		MinTimeout:     time.Second,
		MaxTimeout:     10 * time.Second,
		WarmupRequests: 10,
		WindowHours:    1,
	})

	c := NewAnthropicClient(cfg,
		WithAnthropicMetricsStore(store),
		WithAnthropicTimeoutCalculator(calc),
	)

	// Store original timeout so we can assert it is NOT mutated by the adaptive path.
	originalTimeout := c.httpClient.Timeout

	_, err = c.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// The adaptive timeout path should have set a deadline on the request context.
	if !hasDeadline {
		t.Error("request context has no deadline; adaptive timeout was not applied")
	}

	// The deadline should be approximately originalTimeout from now.
	expectedWall := time.Now().Add(-originalTimeout).Add(-5 * time.Second)
	if capturedDeadline.Before(expectedWall) {
		t.Errorf("captured deadline %v is too early; expected >= %v",
			capturedDeadline, expectedWall)
	}

	// The HTTP client's timeout must not have been mutated (LLM-3 FIX verification).
	if c.httpClient.Timeout != originalTimeout {
		t.Errorf("httpClient.Timeout was mutated from %v to %v (should be unchanged)",
			originalTimeout, c.httpClient.Timeout)
	}
}

// TestAnthropicClient_RecordsMetrics verifies that after a Chat call, a
// corresponding record is written to the metrics store.
func TestAnthropicClient_RecordsMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":          "m_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-test",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))
	defer server.Close()

	storeCfg := metrics.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "metrics.db"),
		RetentionDays:    1,
		StatsWindowHours: 1,
		RefreshInterval:  time.Minute,
	}
	store, err := metrics.NewStore(storeCfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	store.StartBackground(context.Background())

	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-test",
		BaseURL:    server.URL,
		APIKey:     "test",
		MaxTokens:  128,
	}
	c := NewAnthropicClient(cfg, WithAnthropicMetricsStore(store))

	if _, err := c.Chat(context.Background(), []ChatMessage{
		{Role: RoleUser, Content: "hi"},
	}); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Record() is async; poll briefly for the row to appear.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := store.RefreshStats(context.Background()); err != nil {
			t.Fatalf("RefreshStats: %v", err)
		}
		stats, err := store.GetStats(context.Background(), "anthropic", "claude-test", 1)
		if err != nil {
			t.Fatalf("GetStats: %v", err)
		}
		if stats != nil && stats.RequestCount > 0 {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no metrics recorded after Chat")
}

// TestBroker_InjectsMetricsAndTimeout verifies that NewModelBroker passes
// MetricsStore / TimeoutCalc through to the Chatter it builds.
func TestBroker_InjectsMetricsAndTimeout(t *testing.T) {
	storeCfg := metrics.StoreConfig{
		DBPath:           filepath.Join(t.TempDir(), "metrics.db"),
		RetentionDays:    1,
		StatsWindowHours: 1,
		RefreshInterval:  time.Minute,
	}
	store, err := metrics.NewStore(storeCfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	calc := metrics.NewCalculator(store, metrics.AdaptiveTimeoutConfig{
		Enabled:        true,
		MinTimeout:     time.Second,
		MaxTimeout:     10 * time.Second,
		WarmupRequests: 10,
		WindowHours:    1,
	})

	b := &ModelBroker{
		entries: make(map[string]*brokerEntry),
		config: BrokerConfig{
			MetricsStore: store,
			TimeoutCalc:  calc,
		},
		logger: discardLogger(),
	}

	anthropicCfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-foo",
		BaseURL:    "https://api.anthropic.example",
	}
	openaiCfg := &ModelConfig{
		ProviderID: "openai",
		ModelID:    "gpt-foo",
		BaseURL:    "https://api.openai.example",
	}

	ac, ok := b.newChatterFor(anthropicCfg).(*AnthropicClient)
	if !ok {
		t.Fatal("expected AnthropicClient")
	}
	if ac.metricsStore != store {
		t.Error("Anthropic metricsStore not wired")
	}
	if ac.timeoutCalc != calc {
		t.Error("Anthropic timeoutCalc not wired")
	}

	oc, ok := b.newChatterFor(openaiCfg).(*Client)
	if !ok {
		t.Fatal("expected OpenAI Client")
	}
	if oc.metricsStore != store {
		t.Error("Client metricsStore not wired")
	}
	if oc.timeoutCalc != calc {
		t.Error("Client timeoutCalc not wired")
	}
}
