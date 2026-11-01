package config

// Tier policy config tests (llm-serving-tiers tree leaf 01, Task T1):
// the llm.tiers schema lands with a default-off posture - Enabled=false,
// MaxDepth=0 (inactive while disabled), SignalDegraded=true (meaningful
// only when enabled), and json5 keys llm.tiers.*.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTierPolicyDefaults pins the default-off posture (Contract C1).
func TestTierPolicyDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.LLM.Tiers.Enabled {
		t.Error("Default llm.tiers.enabled should be false (opt-in per docs/agents/opt-in-defaults.md)")
	}
	if cfg.LLM.Tiers.MaxDepth != 0 {
		t.Errorf("Default llm.tiers.max_depth = %d, want 0 (inactive while disabled)", cfg.LLM.Tiers.MaxDepth)
	}
	if !cfg.LLM.Tiers.SignalDegraded {
		t.Error("Default llm.tiers.signal_degraded should be true (applies when enabled)")
	}
	if len(cfg.LLM.Tiers.TurnCeilings) != 0 {
		t.Errorf("Default llm.tiers.turn_ceilings should be empty, got %v", cfg.LLM.Tiers.TurnCeilings)
	}
}

// TestTierPolicyJSON5Override pins the json5 key shape (llm.tiers.*): an
// override flips enabled on, sets max_depth and a turn_ceilings map, and
// turns signaling off; unspecified fields keep the DefaultConfig baseline
// (Load seeds defaults before unmarshal).
func TestTierPolicyJSON5Override(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
		"llm": {
			"tiers": {
				"enabled": true,
				"max_depth": 5,
				"turn_ceilings": {"small": 1, "code": 3},
				"signal_degraded": false
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

	if !cfg.LLM.Tiers.Enabled {
		t.Error("llm.tiers.enabled = false, want true after override")
	}
	if cfg.LLM.Tiers.MaxDepth != 5 {
		t.Errorf("llm.tiers.max_depth = %d, want 5", cfg.LLM.Tiers.MaxDepth)
	}
	if cfg.LLM.Tiers.TurnCeilings["small"] != 1 || cfg.LLM.Tiers.TurnCeilings["code"] != 3 {
		t.Errorf("llm.tiers.turn_ceilings = %v, want map[small:1 code:3]", cfg.LLM.Tiers.TurnCeilings)
	}
	if cfg.LLM.Tiers.SignalDegraded {
		t.Error("llm.tiers.signal_degraded = true, want false after override")
	}
}

// TestNormalizeTierPolicyDefaults pins the normalize helper: MaxDepth
// clamps to the default ONLY when enabled (a disabled policy stays
// inert with MaxDepth 0); the helper is idempotent.
func TestNormalizeTierPolicyDefaults(t *testing.T) {
	enabled := TierPolicyConfig{Enabled: true}
	NormalizeTierPolicyDefaults(&enabled)
	if enabled.MaxDepth != DefaultTierPolicyMaxDepth {
		t.Errorf("enabled policy MaxDepth = %d, want %d", enabled.MaxDepth, DefaultTierPolicyMaxDepth)
	}
	// Idempotent: an explicit value survives.
	enabled.MaxDepth = 7
	NormalizeTierPolicyDefaults(&enabled)
	if enabled.MaxDepth != 7 {
		t.Errorf("explicit MaxDepth clobbered: got %d, want 7", enabled.MaxDepth)
	}

	disabled := TierPolicyConfig{}
	NormalizeTierPolicyDefaults(&disabled)
	if disabled.MaxDepth != 0 {
		t.Errorf("disabled policy MaxDepth = %d, want 0 (inactive while disabled)", disabled.MaxDepth)
	}
}
