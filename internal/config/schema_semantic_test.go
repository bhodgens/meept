package config

// Semantic cache config tests (llm-serving-tiers leaf 02, Task T1;
// default posture flipped by audit-P4 owner ruling 2026-11-27):
// llm.cache.semantic lands with the DEFAULT-ON posture -- Enabled=true
// (opt-out per docs/agents/opt-in-defaults.md; inert without a resolvable
// embedder), Threshold=0.95 (R0 research addendum, superseding master
// C4's 0.92), MaxEntries=1024, TTLMin=0 (mirrors llm.cache.default_ttl_min
// at the daemon wiring site); json5 keys under llm.cache.semantic.*.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSemanticCacheDefaults pins the default-on posture (audit-P4 owner
// ruling + R0 addendum threshold). Safety argument for ON-by-default:
// buildSemanticCacheTier degrades to exact-only when no embedder
// resolves, so a default install sees zero behavior change.
func TestSemanticCacheDefaults(t *testing.T) {
	cfg := DefaultConfig()

	sc := cfg.LLM.Cache.Semantic
	if !sc.Enabled {
		t.Error("Default llm.cache.semantic.enabled should be true (default-on per audit-P4; opt-out per docs/agents/opt-in-defaults.md)")
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
// (llm.cache.semantic.*): an override sets threshold, max_entries,
// ttl_min, and embedder_alias; unspecified fields keep the
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
		t.Errorf("llm.cache.semantic.ttl_min = %d, want 30 after ttl_min override", sc.TTLMin)
	}
	if sc.EmbedderAlias != "small" {
		t.Errorf("llm.cache.semantic.embedder_alias = %q, want \"small\" after override", sc.EmbedderAlias)
	}
}

// TestSemanticCacheAbsentKeyDefaultsOn pins the audit-P4 default-on
// posture AT THE LOAD BOUNDARY: a config file that never mentions
// llm.cache.semantic (the pre-flip installed-home shape) must still
// resolve Enabled=true. The mechanism is "unmarshal ONTO DefaultConfig"
// (Load/LoadJSON5Config seed cfg := DefaultConfig() before decoding),
// so an absent bool keeps the seeded true -- no presence sentinel needed.
func TestSemanticCacheAbsentKeyDefaultsOn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
		"llm": {
			"cache": {
				"enabled": true
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
	if !cfg.LLM.Cache.Semantic.Enabled {
		t.Error("absent llm.cache.semantic.enabled must default ON (audit-P4 ruling); got false -- the unmarshal-onto-defaults mechanism regressed")
	}
	if cfg.LLM.Cache.Semantic.Threshold != 0.95 || cfg.LLM.Cache.Semantic.MaxEntries != 1024 {
		t.Errorf("absent-key clamps lost: threshold=%v max_entries=%d", cfg.LLM.Cache.Semantic.Threshold, cfg.LLM.Cache.Semantic.MaxEntries)
	}

	// Empty object arm: "semantic": {} with no enabled key is also absent.
	path2 := filepath.Join(dir, "empty.json5")
	if err := os.WriteFile(path2, []byte(`{"llm": {"cache": {"semantic": {}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg2, err := LoadJSON5Config(path2)
	if err != nil {
		t.Fatalf("LoadJSON5Config (empty semantic object) failed: %v", err)
	}
	if !cfg2.LLM.Cache.Semantic.Enabled {
		t.Error(`"semantic": {} (absent enabled) must stay ON`)
	}
}

// TestSemanticCacheExplicitFalseSurvivesLoad is the ANTI-PIN for the
// default-on flip: an explicit enabled=false must win over the seeded
// default at both load paths (JSON5 and the legacy TOML fallback).
func TestSemanticCacheExplicitFalseSurvivesLoad(t *testing.T) {
	dir := t.TempDir()

	json5Path := filepath.Join(dir, "meept.json5")
	body := `{
		"llm": {
			"cache": {
				"semantic": {
					"enabled": false,
					"threshold": 0.9
				}
			}
		}
	}`
	if err := os.WriteFile(json5Path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadJSON5Config(json5Path)
	if err != nil {
		t.Fatalf("LoadJSON5Config failed: %v", err)
	}
	if cfg.LLM.Cache.Semantic.Enabled {
		t.Error("explicit llm.cache.semantic.enabled=false was clobbered to true by load -- opt-out BROKEN (audit-P4)")
	}
	if cfg.LLM.Cache.Semantic.Threshold != 0.9 {
		t.Errorf("explicit threshold lost: %v, want 0.9", cfg.LLM.Cache.Semantic.Threshold)
	}

	// Legacy TOML fallback path (Load's non-JSON5 branch): an explicit
	// false there must honor opt-out too.
	tomlPath := filepath.Join(dir, "meept.toml")
	tomlBody := "[llm.cache.semantic]\nenabled = false\n"
	if err := os.WriteFile(tomlPath, []byte(tomlBody), 0o600); err != nil {
		t.Fatal(err)
	}
	tcfg, err := Load(tomlPath)
	if err != nil {
		t.Fatalf("Load (TOML path) failed: %v", err)
	}
	if tcfg.LLM.Cache.Semantic.Enabled {
		t.Error("explicit TOML llm.cache.semantic.enabled=false was clobbered to true -- opt-out BROKEN")
	}
}

// TestSemanticCacheShippedTemplate pins config/meept.json5 honesty: the
// shipped template carries the semantic block with enabled=true (the
// kanban/turn_watchdog template-ships-default-on precedent).
func TestSemanticCacheShippedTemplate(t *testing.T) {
	const templatePath = "../../config/meept.json5"
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("template not found from test cwd: %v", err)
	}
	abs, err := filepath.Abs(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadJSON5Config(abs)
	if err != nil {
		t.Fatalf("template meept.json5 failed to parse: %v", err)
	}
	if !cfg.LLM.Cache.Semantic.Enabled {
		t.Error("template ships llm.cache.semantic.enabled=false; must ship true (audit-P4)")
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
	// Leaf 03 (wiring-followups-final): verify_floor clamps.
	t.Run("verify_floor valid band passes through", func(t *testing.T) {
		sc := SemanticCacheConfig{Threshold: 0.95, VerifyFloor: 0.85}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.VerifyFloor != 0.85 {
			t.Errorf("verify_floor = %v, want 0.85 (valid band kept)", sc.VerifyFloor)
		}
	})
	t.Run("verify_floor at/above threshold clamps to 0", func(t *testing.T) {
		sc := SemanticCacheConfig{Threshold: 0.95, VerifyFloor: 0.95}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.VerifyFloor != 0 {
			t.Errorf("verify_floor = %v, want 0 (band at/above threshold is empty)", sc.VerifyFloor)
		}
		sc = SemanticCacheConfig{Threshold: 0.95, VerifyFloor: 0.99}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.VerifyFloor != 0 {
			t.Errorf("verify_floor = %v, want 0 (band above threshold is empty)", sc.VerifyFloor)
		}
	})
	t.Run("negative verify_floor clamps to 0", func(t *testing.T) {
		sc := SemanticCacheConfig{Threshold: 0.95, VerifyFloor: -0.1}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.VerifyFloor != 0 {
			t.Errorf("verify_floor = %v, want 0", sc.VerifyFloor)
		}
	})
	t.Run("verify_floor zero stays zero (band disabled)", func(t *testing.T) {
		sc := SemanticCacheConfig{Threshold: 0.95}
		NormalizeSemanticCacheDefaults(&sc)
		if sc.VerifyFloor != 0 {
			t.Errorf("verify_floor = %v, want 0 (default disabled band)", sc.VerifyFloor)
		}
	})
}

// TestSemanticCacheVerifyFloorJSON5Override pins the json5 key
// (llm.cache.semantic.verify_floor): an override engages the verify band;
// NormalizeSemanticCacheDefaults runs at load (band configured + clamped).
func TestSemanticCacheVerifyFloorJSON5Override(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
		"llm": {
			"cache": {
				"semantic": {
					"enabled": true,
					"threshold": 0.95,
					"verify_floor": 0.85
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
	if sc.VerifyFloor != 0.85 {
		t.Errorf("llm.cache.semantic.verify_floor = %v, want 0.85 after override", sc.VerifyFloor)
	}
	if sc.Threshold != 0.95 {
		t.Errorf("llm.cache.semantic.threshold = %v, want 0.95", sc.Threshold)
	}
}
