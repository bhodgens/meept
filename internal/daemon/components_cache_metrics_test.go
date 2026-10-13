package daemon

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"

	_ "modernc.org/sqlite" // register the "sqlite" driver for direct DB verification
)

// TestCacheCoordinatorGetsMetricsStore verifies the context-reduction L02
// contract: when [llm.cache] enabled=true, NewComponents passes the real
// metrics store into the token-cache coordinator (never nil), so cache.hit /
// cache.miss reach metrics.db on the stock wiring.
//
// The end-to-end leg closes the store (final flush) and reads the rows back
// from the SQLite file — the same observable a dashboard would consume.
func TestCacheCoordinatorGetsMetricsStore(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	tmpDir := t.TempDir()
	cfg := &config.Config{
		Cluster: config.ClusterConfig{Enabled: false},
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
		},
		Daemon: config.DaemonConfig{
			DataDir: tmpDir,
		},
		LLM: config.LLMConfig{
			Cache: config.LLMSimpleFeatureConfig{
				Enabled:   true,
				L2Enabled: true,
			},
		},
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = comps.Stop(ctx)
	})

	if comps.TokenCache == nil {
		t.Fatal("expected non-nil TokenCache when llm.cache.enabled=true")
	}
	if comps.MetricsStore == nil {
		t.Fatal("expected non-nil MetricsStore wired into the token cache coordinator: stock wiring must not pass a nil metrics store (L02)")
	}

	// miss -> put -> hit: the Get/Put pair that must emit cache.miss and
	// cache.hit through the coordinator's metricsStore.
	ctx := context.Background()
	key := llm.CacheKey{PromptHash: "l02-metrics-store-hash", ModelID: "test-model"}
	if _, ok := comps.TokenCache.Get(ctx, key); ok {
		t.Fatal("unexpected hit on empty cache")
	}
	comps.TokenCache.Put(ctx, key, &llm.Response{Content: "cached-reply", Model: "test-model"})
	if _, ok := comps.TokenCache.Get(ctx, key); !ok {
		t.Fatal("expected cache hit after Put")
	}

	// Close flushes the batch and checkpoints the WAL, making the rows
	// readable from the main DB file.
	if err := comps.MetricsStore.Close(); err != nil {
		t.Fatalf("close metrics store: %v", err)
	}

	db, err := sql.Open("sqlite", filepath.Join(tmpDir, "metrics.db"))
	if err != nil {
		t.Fatalf("open metrics.db for verification: %v", err)
	}
	defer db.Close()

	countMetric := func(name string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM metrics_live WHERE metric_name = ?`, name,
		).Scan(&n); err != nil {
			t.Fatalf("query %s from metrics.db: %v", name, err)
		}
		return n
	}
	if n := countMetric("cache.miss"); n < 1 {
		t.Errorf("expected >=1 cache.miss row in metrics.db, got %d", n)
	}
	if n := countMetric("cache.hit"); n < 1 {
		t.Errorf("expected >=1 cache.hit row in metrics.db, got %d", n)
	}
}

// TestCacheDisabledKeepsStockBehavior pins the L02 checklist invariant: with
// [llm.cache] enabled=false (the default) the coordinator is nil, the
// components-level metrics store is NOT opened (no behavior change), and
// construction succeeds.
func TestCacheDisabledKeepsStockBehavior(t *testing.T) {
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	tmpDir := t.TempDir()
	cfg := &config.Config{
		Cluster: config.ClusterConfig{Enabled: false},
		Security: config.SecurityConfig{
			AllowedPaths: []string{tmpDir},
		},
		Daemon: config.DaemonConfig{
			DataDir: tmpDir,
		},
		// LLM.Cache zero value: Enabled=false (the default posture).
	}

	comps, err := NewComponents(context.Background(), cfg, msgBus, logger)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = comps.Stop(ctx)
	})

	if comps.TokenCache != nil {
		t.Error("expected nil TokenCache when llm.cache.enabled=false")
	}
	if comps.MetricsStore != nil {
		t.Error("expected nil MetricsStore when llm.cache.enabled=false (store ownership stays with the daemon)")
	}
}
