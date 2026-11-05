package config

import (
	"os"
	"testing"
)

// TestMemoryTiersJSON5TemplateRoundtrip (leaf T1 config half): the shipped
// template's [memory.tiers] block parses, the disabled posture survives
// untouched, and NormalizeMemoryTiersDefaults is idempotent on it.
func TestMemoryTiersJSON5TemplateRoundtrip(t *testing.T) {
	cfg := DefaultConfig()
	if err := LoadJSON5("../../config/meept.json5", cfg); err != nil {
		t.Fatalf("template load: %v", err)
	}
	tiers := cfg.Memory.Tiers
	if tiers.Enabled {
		t.Fatal("template must ship memory.tiers disabled (opt-in-defaults rule)")
	}
	if tiers.RetentionDays != 30 {
		t.Fatalf("template retention_days = %d, want 30", tiers.RetentionDays)
	}
	if tiers.Demotion.StaleDays != 30 || tiers.Demotion.MinUsefulness != 0.1 {
		t.Fatalf("template demotion = %+v, want stale 30 / floor 0.1", tiers.Demotion)
	}

	// User config that only flips enabled: every unset knob resolves at
	// the load boundary (JSON5 path runs NormalizeMemoryTiersDefaults).
	enabled := DefaultConfig()
	tmp := t.TempDir() + "/tiers.json5"
	content := []byte(`{ "memory": { "tiers": { "enabled": true } } }`)
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadJSON5(tmp, enabled); err != nil {
		t.Fatalf("enabled load: %v", err)
	}
	got := enabled.Memory.Tiers
	if !got.Enabled {
		t.Fatal("enabled flag lost")
	}
	if got.EvictOverRatio != 0.9 || got.CompressOverRatio != 0.8 {
		t.Fatalf("ratios unresolved: %+v", got)
	}
	if got.IntervalSeconds != 300 || got.RetentionDays != 30 {
		t.Fatalf("interval/retention unresolved: %+v", got)
	}
	if got.Demotion.StaleDays != 30 || got.Demotion.MinUsefulness != 0.1 {
		t.Fatalf("demotion unresolved: %+v", got.Demotion)
	}

	// Disabled + garbage knobs stays untouched (the engine is inert;
	// nothing consumes the values while off).
	off := MemoryTiersConfig{EvictOverRatio: -5, IntervalSeconds: -1, RetentionDays: -3, Demotion: MemoryTiersDemotionConfig{StaleDays: -2, MinUsefulness: 7}}
	NormalizeMemoryTiersDefaults(&off)
	if off != (MemoryTiersConfig{EvictOverRatio: -5, IntervalSeconds: -1, RetentionDays: -3, Demotion: MemoryTiersDemotionConfig{StaleDays: -2, MinUsefulness: 7}}) {
		t.Fatalf("disabled normalize must leave the config untouched, got %+v", off)
	}

	// Idempotency: feeding resolved values through again is a no-op.
	before := got
	NormalizeMemoryTiersDefaults(&got)
	if got != before {
		t.Fatalf("normalize not idempotent: %+v -> %+v", before, got)
	}

	// Explicit values are honored (negative rejection does not clobber
	// deliberate choices inside the valid range).
	custom := DefaultConfig()
	tmp2 := t.TempDir() + "/tiers2.json5"
	content2 := []byte(`{ "memory": { "tiers": { "enabled": true, "retention_days": 14, "evict_over_ratio": 0.95, "demotion": { "stale_days": 7, "min_usefulness": 0.25 } } } }`)
	if err := os.WriteFile(tmp2, content2, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadJSON5(tmp2, custom); err != nil {
		t.Fatalf("custom load: %v", err)
	}
	got2 := custom.Memory.Tiers
	if got2.RetentionDays != 14 || got2.EvictOverRatio != 0.95 {
		t.Fatalf("explicit values clobbered: %+v", got2)
	}
	if got2.Demotion.StaleDays != 7 || got2.Demotion.MinUsefulness != 0.25 {
		t.Fatalf("explicit demotion clobbered: %+v", got2.Demotion)
	}
}
