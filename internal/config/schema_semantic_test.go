package config

// Semantic cache config tests (llm-serving-tiers leaf 02, Task T1):
// llm.cache.semantic lands with the default-off posture -- Enabled=false,
// Threshold=0.95 (R0 research addendum, superseding master C4's 0.92),
// MaxEntries=1024, TTLMin=0 (mirrors llm.cache.default_ttl_min at the
// daemon wiring site); json5 keys under llm.cache.semantic.*.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSemanticCacheDefaults pins the default-off posture (Contract C4 +
// R0 addendum threshold).
func TestSemanticCacheDefaults(t *testing.T) {
	cfg := DefaultConfig()

	sc := cfg.LLM.Cache.Semantic
	if sc.Enabled {
		t.Error("Default llm.cache.semantic.enabled should be false (opt-in per docs/agents/opt-in-defaults.md)")
	}
	if sc.Threshold != 0.95 {
		t.Errorf("Default llm.cache.semantic.threshold = %v, want 0.95 (R0 addendum supersedes C4's 0.92)", sc.Threshold)
	}
	if sc.MaxEntries != 1024 {
		t.Errorf("Default llm.cache.semantic.max_entries = %d, want 1024", sc.MaxEntries)
	}
	if sc.TTLMin != 0 {
		t.Errorf("Default llm.cache.semantic.ttl_min = %d, want 0 (mirrors llm.cache.default_ttl_min at wiring)", sc.TTLMin)
	}
}

// TestSemanticCacheJSON5Override pins the json5 key shape
// (llm.cache.semantic.*): an override flips enabled on, sets threshold,
// max_entries, ttl_min, and embedder_alias; unspecified fields keep the
// DefaultConfig baseline (Load seeds defaults before unmarshal).
func TestSemanticCacheJSON5Override(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
		"llm": {
			"cache": {
				"semantic": {
					"enabled": true,
					"threshold": 0.97,
					"max_entries": 256,
					"ttl_min": 30,
					"embedder_alias": "small"
				}
			}
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("LoadJSON5Config failed: %v", err)
	}

	sc := cfg.LLM.Cache.Semantic
	if !sc.Enabled {
		t.Error("llm.cache.semantic.enabled = false, want true after override")
	}
	if sc.Threshold != 0.97 {
		t.Errorf("llm.cache.semantic.threshold = %v, want 0.97 after override", sc.Threshold)
	}
	if sc.MaxEntries != 256 {
		t.Errorf("llm.cache.semantic.max_entries = %d, want 256 after override", sc.MaxEntries)
	}
	if sc.TTLMin != 30 {
		t.Errorf("llm.cache.semantic.ttl_min = %d, want 30 after override", sc.TTLMin)
	}
	if sc.EmbedderAlias != "small" {
		t.Errorf("llm.cache.semantic.embedder_alias = %q, want \"small\" after override", sc.EmbedderAlias)
	}
}

// TestNormalizeSemanticCacheDefaults pins the load-boundary clamps:
// non-positive or >= 1 threshold -> 0.95, non-positive max_entries ->
// 1024, negative ttl_min -> 0; valid values pass through idempotently.
func TestNormalizeSemanticCacheDefaults(t *testing.T) {
	t.Run("zero threshold clamps to 0.95", func(t *testing.T) {
		sc := SemanticCacheConfig{}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.Threshold != 0.95 {
			t.Errorf("threshold = %v, want 0.95", sc.Threshold)
		}
		if sc.MaxEntries != 1024 {
			t.Errorf("max_entries = %d, want 1024", sc.MaxEntries)
		}
	})
	t.Run("out-of-range threshold clamps to 0.95", func(t *testing.T) {
		sc := SemanticCacheConfig{Threshold: 1.5}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.Threshold != 0.95 {
			t.Errorf("threshold = %v, want 0.95", sc.Threshold)
		}
	})
	t.Run("valid values pass through", func(t *testing.T) {
		sc := SemanticCacheConfig{Threshold: 0.9, MaxEntries: 64, TTLMin: 15}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.Threshold != 0.9 || sc.MaxEntries != 64 || sc.TTLMin != 15 {
			t.Errorf("valid values mutated: %+v", sc)
		}
	})
	t.Run("negative ttl_min clamps to 0", func(t *testing.T) {
		sc := SemanticCacheConfig{TTLMin: -5}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.TTLMin != 0 {
			t.Errorf("ttl_min = %d, want 0", sc.TTLMin)
		}
	})
	t.Run("idempotent", func(t *testing.T) {
		sc := SemanticCacheConfig{}
		NormalizeSemanticCacheDefaults(&sc)
		again := sc
		NormalizeSemanticCacheDefaults(&again)
		if again != sc {
			t.Errorf("not idempotent: %+v vs %+v", again, sc)
		}
	})
}
