package daemon

// Semantic cache tier daemon-wiring tests (wiring-followups-final leaf
// 05, llm-serving-tiers leaf 02 T5): the construction site in
// components.go must arm the tier ONLY when llm.cache.semantic.enabled
// is true (opt-in default false = byte-identical), map every schema
// field onto llm.SemanticCacheConfig, and keep the degraded posture --
// no embedder resolves -> Warn + exact-only, never a boot failure.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"log/slog"
)

// fakeVectorProvider is a minimal memory-stack embedding provider for
// the reuse path (satisfies vector.Provider structurally).
type fakeVectorProvider struct{ calls int }

func (f *fakeVectorProvider) GenerateEmbedding(_ context.Context, _ string) ([]float32, error) {
	f.calls++
	return []float32{1, 0, 0, 0}, nil
}

func (f *fakeVectorProvider) GenerateEmbeddings(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0, 0}
	}
	return out, nil
}

func (f *fakeVectorProvider) Dimension() int { return 4 }

func wiringLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func semanticCfg(enabled bool) *config.Config {
	cfg := config.DefaultConfig()
	cfg.LLM.Cache.Semantic.Enabled = enabled
	cfg.LLM.Cache.Enabled = false
	return cfg
}

// TestSemanticCacheWiringDisabled (T3): the opt-in default is a nil
// cache and ZERO embedder consultation - byte-identical to pre-leaf.
func TestSemanticCacheWiringDisabled(t *testing.T) {
	prov := &fakeVectorProvider{}
	cache := buildSemanticCacheTier(semanticCfg(false), prov, wiringLogger())
	if cache != nil {
		t.Fatalf("disabled config must build no cache, got %+v", cache)
	}
	if prov.calls != 0 {
		t.Fatalf("disabled wiring consulted the embedder %d times", prov.calls)
	}
}

// TestSemanticCacheWiringEnabledMemoryStack (T1): enabled config with
// the shared memory-stack provider attached -> live cache whose Enabled
// () is true, sourced from the memory stack (reuse, no second
// provider).
func TestSemanticCacheWiringEnabledMemoryStack(t *testing.T) {
	prov := &fakeVectorProvider{}
	cache := buildSemanticCacheTier(semanticCfg(true), prov, wiringLogger())
	if cache == nil || !cache.Enabled() {
		t.Fatalf("enabled cfg + embedder must install a live cache, got nil/disabled")
	}
}

// TestSemanticCacheWiringDegradedNoEmbedder (T3 failure shape): enabled
// config but NOTHING resolves an embedder (no memory-stack provider,
// empty alias) -> nil cache (exact-only), never a panic and never a
// boot failure.
func TestSemanticCacheWiringDegradedNoEmbedder(t *testing.T) {
	cache := buildSemanticCacheTier(semanticCfg(true), nil, wiringLogger())
	if cache != nil {
		t.Fatal("no embedder must degrade to exact-only (nil cache)")
	}
}

// TestSemanticCacheWiringAliasResolution: embedder_alias resolves
// through the alias/capability machinery against a MEEPT_HOME
// models.json5; an unresolvable alias degrades to exact-only EVEN when
// the memory-stack embedder exists (alias failure does not fall
// through - per the leaf doc).
func TestSemanticCacheWiringAliasResolution(t *testing.T) {
	home := t.TempDir()
	models := `{
  "providers": {
    "embprov": {
      "api": "openai",
      "options": { "baseURL": "http://127.0.0.1:1/v1", "apiKey": "k" },
      "models": { "embed-model": { "name": "embed-model" } }
    }
  },
  "model_aliases": {
    "embedder": { "models": ["embprov/embed-model"], "timeout": 5, "max_fails": 1 }
  }
}`
	if err := os.WriteFile(filepath.Join(home, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEEPT_HOME", home)

	// Sanity: with cwd outside the repo-bundled config dir, the user
	// file is the base this resolves against. The adapter is a real
	// embeddings client bound to the stub BaseURL - resolution is what
	// this test pins, not a live embed round-trip.
	embedder, source := resolveSemanticEmbedder(
		config.SemanticCacheConfig{Enabled: true, EmbedderAlias: "embedder"}, nil, wiringLogger())
	if embedder == nil || source != "alias:embedder" {
		t.Fatalf("alias resolution failed: embedder=%v source=%q", embedder, source)
	}

	// Unresolvable alias: degrade to exact-only even with a memory-stack
	// provider present (no fall-through).
	t.Setenv("MEEPT_HOME", t.TempDir()) // no models.json5 -> load fails
	prov := &fakeVectorProvider{}
	embedder2, source2 := resolveSemanticEmbedder(
		config.SemanticCacheConfig{Enabled: true, EmbedderAlias: "ghost"}, prov, wiringLogger())
	if embedder2 != nil || source2 != "" {
		t.Fatalf("unresolvable alias must degrade to nil embedder, got %v/%q", embedder2, source2)
	}
	cache := buildSemanticCacheTier(withAlias("ghost", true), prov, wiringLogger())
	if cache != nil {
		t.Fatal("alias failure with memory embedder present must still be exact-only")
	}
}

func withAlias(alias string, enabled bool) *config.Config {
	cfg := semanticCfg(enabled)
	cfg.LLM.Cache.Semantic.EmbedderAlias = alias
	return cfg
}

// TestSemanticCacheConfigMapping (T2): every schema field maps onto the
// llm-side config; TTLMin zero mirrors llm.cache.default_ttl_min.
func TestSemanticCacheConfigMapping(t *testing.T) {
	sc := config.SemanticCacheConfig{
		Enabled:     true,
		Threshold:   0.9,
		MaxEntries:  42,
		TTLMin:      7,
		VerifyFloor: 0.8,
	}
	got := semanticCacheConfigFromSchema(sc, 60)
	if !got.Enabled || got.Threshold != 0.9 || got.MaxEntries != 42 ||
		got.TTL.String() != "7m0s" || got.VerifyFloor != 0.8 {
		t.Fatalf("field mapping wrong: %+v", got)
	}

	// TTLMin zero -> mirror llm.cache.default_ttl_min.
	got = semanticCacheConfigFromSchema(config.SemanticCacheConfig{Enabled: true, TTLMin: 0}, 30)
	if got.TTL.String() != "30m0s" {
		t.Fatalf("zero ttl_min must mirror default_ttl_min, got %v", got.TTL)
	}

	// Both zero -> unset TTL (llm-side DefaultSemanticTTL fallback).
	got = semanticCacheConfigFromSchema(config.SemanticCacheConfig{}, 0)
	if got.TTL != 0 {
		t.Fatalf("zero/zero must leave TTL unset, got %v", got.TTL)
	}

	// The attached cache carries the mapped config (Enabled + live).
	cfg := semanticCfg(true)
	cfg.LLM.Cache.Semantic.Threshold = 0.9
	cfg.LLM.Cache.Semantic.MaxEntries = 10
	cache := buildSemanticCacheTier(cfg, &fakeVectorProvider{}, wiringLogger())
	if cache == nil || !cache.Enabled() || cache.Len() != 0 {
		t.Fatalf("constructed cache wrong: %+v", cache)
	}
}
