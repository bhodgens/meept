package llm

import (
	"testing"

	llmmetrics "github.com/caimlas/meept/internal/llm/metrics"
	"github.com/caimlas/meept/internal/metrics"
)

// TestAllSetters_NilSafe verifies that every Set* method on llm-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// Pre-build structs whose constructors initialize required fields.
	resolver := NewResolver(&ProvidersConfig{}, nil)
	client := NewClient(&ModelConfig{})
	runtimeMgr := NewRuntimeManager(nil)
	l1Cache := NewL1Cache(L1CacheConfig{})
	tokenCache, err := NewTokenCacheCoordinator(CacheConfig{})
	if err != nil {
		t.Fatalf("NewTokenCacheCoordinator: %v", err)
	}
	compressor := NewContextCompressor(CompressionConfig{}, nil, nil, nil)
	firewall := NewContextFirewall(nil, nil, ContextFirewallConfig{}, nil, nil, nil)

	tests := []struct {
		name    string
		setFunc func()
	}{
		// Client setters (internal/llm/client.go) — uses inner llm/metrics.
		{"Client.SetMetricsStore", func() { client.SetMetricsStore((*llmmetrics.Store)(nil)) }},

		// Resolver setters (internal/llm/resolver.go)
		{"Resolver.SetPricingSyncer", func() { resolver.SetPricingSyncer((*PricingSyncer)(nil)) }},

		// RuntimeManager setters (internal/llm/runtime_manager.go).
		// SetMetricsRecorder accepts an interface; pass nil to exercise the
		// typed-nil guard.
		{"RuntimeManager.SetMetricsRecorder", func() { runtimeMgr.SetMetricsRecorder(nil) }},

		// L1Cache setters (internal/llm/token_cache_l1.go) — uses top-level metrics.
		{"L1Cache.SetMetricsStore", func() { l1Cache.SetMetricsStore((*metrics.Store)(nil)) }},

		// TokenCacheCoordinator setters (internal/llm/token_cache.go) — uses top-level metrics.
		{"TokenCacheCoordinator.SetMetricsStore", func() { tokenCache.SetMetricsStore((*metrics.Store)(nil)) }},

		// ContextCompressor setters (internal/llm/context_compressor.go)
		{"ContextCompressor.SetCompactor", func() { compressor.SetCompactor((*ContextCompactor)(nil)) }},

		// ContextFirewall setters (internal/llm/context_firewall.go)
		{"ContextFirewall.SetCompactor", func() { firewall.SetCompactor((*ContextCompactor)(nil)) }},
		{"ContextFirewall.SetCompactor_withRatio", func() { firewall.SetCompactor((*ContextCompactor)(nil), 0.5) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Set method panicked on nil: %v", r)
				}
			}()
			tt.setFunc()
		})
	}
}
