package metrics

import (
	"context"
	"testing"
	"time"
)

func TestCalculatorTokenRateBased(t *testing.T) {
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

	// Populate some test data
	now := time.Now()
	for i := range 30 {
		record := RequestRecord{
			Timestamp:        now.Add(-time.Duration(i) * time.Minute),
			ProviderID:       "openai",
			ModelID:          "gpt-4",
			PromptTokens:     100,
			CompletionTokens: 50,
			LatencyMs:        int64(1000 + i*50), // varies from 1000 to 2450ms
			HTTPStatus:       200,
			ErrorType:        ErrorTypeNone,
			Success:          true,
		}
		store.recordSync(context.Background(), record)
	}

	// Refresh stats
	if err := store.RefreshStats(context.Background()); err != nil {
		t.Fatalf("RefreshStats failed: %v", err)
	}

	calcCfg := AdaptiveTimeoutConfig{
		Enabled:                true,
		StddevMultiplier:       3.0,
		MinTimeout:             10 * time.Second,
		MaxTimeout:             300 * time.Second,
		WarmupRequests:         10,
		WindowHours:            24,
		StddevTokenRateTimeout: true,
	}

	calc := NewCalculator(store, calcCfg)

	// Test token-rate based calculation
	timeout := calc.Calculate(context.Background(), "openai", "gpt-4", 4096, 120*time.Second)

	if timeout < 10*time.Second || timeout > 300*time.Second {
		t.Errorf("timeout %v out of bounds [10s, 300s]", timeout)
	}
}

func TestCalculatorLatencyBased(t *testing.T) {
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

	// Populate test data
	now := time.Now()
	for i := range 30 {
		record := RequestRecord{
			Timestamp:        now.Add(-time.Duration(i) * time.Minute),
			ProviderID:       "anthropic",
			ModelID:          "claude-3",
			PromptTokens:     100,
			CompletionTokens: 50,
			LatencyMs:        int64(2000 + i*100),
			HTTPStatus:       200,
			ErrorType:        ErrorTypeNone,
			Success:          true,
		}
		store.recordSync(context.Background(), record)
	}

	if err := store.RefreshStats(context.Background()); err != nil {
		t.Fatalf("RefreshStats failed: %v", err)
	}

	calcCfg := AdaptiveTimeoutConfig{
		Enabled:                false, // latency-based mode
		StddevMultiplier:       3.0,
		MinTimeout:             10 * time.Second,
		MaxTimeout:             300 * time.Second,
		WarmupRequests:         10,
		WindowHours:            24,
		StddevTokenRateTimeout: false,
	}

	calc := NewCalculator(store, calcCfg)
	calc.SetLogger(nil) // Suppress logs in tests

	// Should return default during warmup (only 30 requests, well above 10)
	// Actually, 30 >= 10, so it should calculate
	timeout := calc.Calculate(context.Background(), "anthropic", "claude-3", 4096, 120*time.Second)

	if timeout < 10*time.Second || timeout > 300*time.Second {
		t.Errorf("timeout %v out of bounds [10s, 300s]", timeout)
	}
}

func TestCalculatorWarmup(t *testing.T) {
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

	calcCfg := AdaptiveTimeoutConfig{
		Enabled:        true,
		WarmupRequests: 100, // High threshold
		WindowHours:    24,
	}

	calc := NewCalculator(store, calcCfg)
	calc.SetLogger(nil)

	// Should return default since we have no requests
	timeout := calc.Calculate(context.Background(), "test", "model", 4096, 60*time.Second)

	if timeout != 60*time.Second {
		t.Errorf("warmup timeout = %v, want 60s", timeout)
	}
}

func TestMeanStddev(t *testing.T) {
	values := []float64{10, 20, 30, 40, 50}
	mean, stddev := meanStddev(values)

	expectedMean := 30.0
	if mean != expectedMean {
		t.Errorf("mean = %v, want %v", mean, expectedMean)
	}

	if stddev <= 0 {
		t.Errorf("stddev should be positive, got %v", stddev)
	}
}
