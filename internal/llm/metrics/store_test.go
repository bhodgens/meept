package metrics

import (
	"context"
	"os"
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
		err        error
		status     int
		expected   ErrorType
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
