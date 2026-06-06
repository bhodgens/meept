package metrics

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreInitialize(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	cfg := StoreConfig{
		DBPath:           tmpFile,
		RetentionDays:    7,
		StatsWindowHours: 24,
		RefreshInterval:  1 * time.Minute,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify DB file was created
	if _, err := os.Stat(tmpFile); err != nil {
		t.Fatalf("DB file not created: %v", err)
	}
}

func TestStoreRecord(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	cfg := StoreConfig{
		DBPath:           tmpFile,
		RetentionDays:    7,
		StatsWindowHours: 24,
		RefreshInterval:  1 * time.Minute,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	store.StartBackground(context.Background())

	// Record a request
	record := RequestRecord{
		Timestamp:        time.Now(),
		ProviderID:       "openai",
		ModelID:          "gpt-4",
		PromptTokens:     100,
		CompletionTokens: 50,
		LatencyMs:        1500,
		HTTPStatus:       200,
		ErrorType:        ErrorTypeNone,
		Success:          true,
	}

	if err := store.Record(context.Background(), record); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Give the async worker a moment to process
	time.Sleep(100 * time.Millisecond)
}

func TestStoreRecordCachedTokens(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	cfg := StoreConfig{
		DBPath:           tmpFile,
		RetentionDays:    7,
		StatsWindowHours: 24,
		RefreshInterval:  1 * time.Minute,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if err := store.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	store.StartBackground(context.Background())

	// Record a request with cached tokens
	record := RequestRecord{
		Timestamp:        time.Now(),
		ProviderID:       "anthropic",
		ModelID:          "claude-3-opus",
		PromptTokens:     1000,
		CompletionTokens: 200,
		CachedTokens:     800,
		LatencyMs:        1500,
		HTTPStatus:       200,
		ErrorType:        ErrorTypeNone,
		Success:          true,
	}

	if err := store.Record(context.Background(), record); err != nil {
		t.Fatalf("Record failed: %v", err)
	}

	// Give the async worker a moment to process
	time.Sleep(100 * time.Millisecond)

	// Refresh stats so the aggregated data is computed
	if err := store.RefreshStats(context.Background()); err != nil {
		t.Fatalf("RefreshStats failed: %v", err)
	}

	stats, err := store.GetStats(context.Background(), "anthropic", "claude-3-opus", 24)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.RequestCount != 1 {
		t.Errorf("RequestCount = %d, want 1", stats.RequestCount)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err         error
		status      int
		expected    ErrorType
		description string
	}{
		{nil, 200, ErrorTypeNone, "no error"},
		{context.DeadlineExceeded, 0, ErrorTypeTimeout, "deadline exceeded"},
		{nil, 401, ErrorTypeAuth, "auth error"},
		{nil, 429, ErrorTypeRateLimit, "rate limit"},
		{nil, 500, ErrorTypeServer, "server error"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := ClassifyError(tt.err, tt.status)
			if result != tt.expected {
				t.Errorf("ClassifyError(%v, %d) = %v, want %v", tt.err, tt.status, result, tt.expected)
			}
		})
	}
}

func TestStore_RecordWithCost(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(StoreConfig{
		DBPath:        dbPath,
		RetentionDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	// Start background worker so Record() processes
	store.StartBackground(ctx)

	record := RequestRecord{
		Timestamp:        time.Now(),
		ProviderID:       "anthropic",
		ModelID:          "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 500,
		CachedTokens:     200,
		CostUSD:          0.0105,
		LatencyMs:        1200,
		Success:          true,
	}

	if err := store.Record(ctx, record); err != nil {
		t.Fatal(err)
	}

	// Wait for async worker
	time.Sleep(200 * time.Millisecond)

	costs, err := store.GetDailyCosts(ctx, time.Now().Add(-24*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if len(costs) != 1 {
		t.Fatalf("expected 1 cost entry, got %d", len(costs))
	}
	if costs[0].ProviderID != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", costs[0].ProviderID)
	}
	if costs[0].ModelID != "claude-sonnet-4-6" {
		t.Errorf("expected model claude-sonnet-4-6, got %s", costs[0].ModelID)
	}

	const tolerance = 0.0001
	if math.Abs(costs[0].TotalCost-record.CostUSD) > tolerance {
		t.Errorf("expected cost ~%.4f, got %.4f", record.CostUSD, costs[0].TotalCost)
	}

	// Test GetTotalCost
	total, err := store.GetTotalCost(ctx, time.Now().Add(-24*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(total-record.CostUSD) > tolerance {
		t.Errorf("expected total cost ~%.4f, got %.4f", record.CostUSD, total)
	}
}

func TestStore_GetRateLimitSummary(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(StoreConfig{
		DBPath:        dbPath,
		RetentionDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	store.StartBackground(ctx)

	now := time.Now()

	// Insert a mix of rate limit and non-rate-limit records.
	records := []RequestRecord{
		// Rate limit errors (should be counted)
		{Timestamp: now, ProviderID: "openai", ModelID: "gpt-4", HTTPStatus: 429, ErrorType: ErrorTypeRateLimit, ErrorMessage: "rate limited", LatencyMs: 120, Success: false},
		{Timestamp: now, ProviderID: "openai", ModelID: "gpt-4", HTTPStatus: 429, ErrorType: ErrorTypeRateLimit, ErrorMessage: "rate limited", LatencyMs: 130, Success: false},
		{Timestamp: now, ProviderID: "anthropic", ModelID: "claude-3-opus", HTTPStatus: 429, ErrorType: ErrorTypeRateLimit, ErrorMessage: "overloaded", LatencyMs: 200, Success: false},
		{Timestamp: now, ProviderID: "anthropic", ModelID: "claude-3-opus", HTTPStatus: 429, ErrorType: ErrorTypeRateLimit, ErrorMessage: "overloaded", LatencyMs: 210, Success: false},
		{Timestamp: now, ProviderID: "anthropic", ModelID: "claude-3-opus", HTTPStatus: 429, ErrorType: ErrorTypeRateLimit, ErrorMessage: "overloaded", LatencyMs: 220, Success: false},
		// Non-rate-limit errors (should NOT be counted)
		{Timestamp: now, ProviderID: "openai", ModelID: "gpt-4", HTTPStatus: 500, ErrorType: ErrorTypeServer, ErrorMessage: "internal error", LatencyMs: 500, Success: false},
		{Timestamp: now, ProviderID: "anthropic", ModelID: "claude-3-opus", HTTPStatus: 200, ErrorType: ErrorTypeNone, ErrorMessage: "", LatencyMs: 1500, Success: true},
		// Another rate limit for a different model
		{Timestamp: now, ProviderID: "anthropic", ModelID: "claude-sonnet", HTTPStatus: 429, ErrorType: ErrorTypeRateLimit, ErrorMessage: "rate limited", LatencyMs: 100, Success: false},
	}

	for _, r := range records {
		if err := store.Record(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	// Wait for async worker
	time.Sleep(200 * time.Millisecond)

	// Test with default limit
	summary, err := store.GetRateLimitSummary(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Verify total count: 5 openai/anthropic + 1 claude-sonnet = 6 rate limit errors
	if summary.Total24h != 6 {
		t.Errorf("Total24h = %d, want 6", summary.Total24h)
	}

	// Verify ByProvider aggregation: openai=2, anthropic=4
	if summary.ByProvider["openai"] != 2 {
		t.Errorf("ByProvider[openai] = %d, want 2", summary.ByProvider["openai"])
	}
	if summary.ByProvider["anthropic"] != 4 {
		t.Errorf("ByProvider[anthropic] = %d, want 4", summary.ByProvider["anthropic"])
	}

	// Verify ByModel aggregation: gpt-4=2, claude-3-opus=3, claude-sonnet=1
	if summary.ByModel["gpt-4"] != 2 {
		t.Errorf("ByModel[gpt-4] = %d, want 2", summary.ByModel["gpt-4"])
	}
	if summary.ByModel["claude-3-opus"] != 3 {
		t.Errorf("ByModel[claude-3-opus] = %d, want 3", summary.ByModel["claude-3-opus"])
	}
	if summary.ByModel["claude-sonnet"] != 1 {
		t.Errorf("ByModel[claude-sonnet] = %d, want 1", summary.ByModel["claude-sonnet"])
	}

	// Verify recent entries limited to default 20 (we only have 6, so all should be present)
	if len(summary.RecentEntries) != 6 {
		t.Errorf("len(RecentEntries) = %d, want 6", len(summary.RecentEntries))
	}

	// Verify recent entries limit works
	limitedSummary, err := store.GetRateLimitSummary(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(limitedSummary.RecentEntries) != 3 {
		t.Errorf("len(RecentEntries) with limit=3 = %d, want 3", len(limitedSummary.RecentEntries))
	}
	// Total and aggregates should still be correct regardless of limit
	if limitedSummary.Total24h != 6 {
		t.Errorf("Total24h with limit=3 = %d, want 6", limitedSummary.Total24h)
	}
}

func TestStore_GetRateLimitSummary_Empty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(StoreConfig{
		DBPath:        dbPath,
		RetentionDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	// No records at all
	summary, err := store.GetRateLimitSummary(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total24h != 0 {
		t.Errorf("Total24h = %d, want 0", summary.Total24h)
	}
	if len(summary.ByProvider) != 0 {
		t.Errorf("ByProvider = %v, want empty", summary.ByProvider)
	}
	if len(summary.ByModel) != 0 {
		t.Errorf("ByModel = %v, want empty", summary.ByModel)
	}
	if len(summary.RecentEntries) != 0 {
		t.Errorf("RecentEntries = %v, want empty", summary.RecentEntries)
	}
}

func TestStore_GetRateLimitSummary_OnlyNonRateLimit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := NewStore(StoreConfig{
		DBPath:        dbPath,
		RetentionDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	store.StartBackground(ctx)

	// Insert only non-rate-limit records
	records := []RequestRecord{
		{Timestamp: time.Now(), ProviderID: "openai", ModelID: "gpt-4", HTTPStatus: 200, ErrorType: ErrorTypeNone, Success: true},
		{Timestamp: time.Now(), ProviderID: "anthropic", ModelID: "claude-3-opus", HTTPStatus: 500, ErrorType: ErrorTypeServer, Success: false},
	}
	for _, r := range records {
		if err := store.Record(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	summary, err := store.GetRateLimitSummary(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total24h != 0 {
		t.Errorf("Total24h = %d, want 0", summary.Total24h)
	}
	if len(summary.RecentEntries) != 0 {
		t.Errorf("len(RecentEntries) = %d, want 0", len(summary.RecentEntries))
	}
}
