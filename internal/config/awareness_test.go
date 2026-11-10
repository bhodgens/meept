package config

// 00T-moment gate §5 — plan 20261110-situational leaf 01-moment/00T:
// the [awareness] config table (D1: enabled DEFAULT FALSE; refresh_minutes
// default 30), TOML + JSON5 roundtrip, and load-boundary normalization at
// BOTH boundaries (finalizeLoadedConfig + LoadJSON5Config; ledger-integrity
// precedent). TESTS ONLY; RED posture expected until leaf 01/01 lands
// (undefined: AwarenessConfig, DefaultAwarenessRefreshMinutes,
// NormalizeAwarenessDefaults).
//
// Pinned seams — implementation leaf 01/01 (01-moment-pkg.md Task 3) MUST
// provide EXACTLY these (same block the 00T-weather daemon gate pins; the
// two gates intentionally pin one shared contract — first impl leaf to
// land satisfies both):
//
//	type AwarenessConfig struct {
//	    Enabled        bool `json:"enabled"         toml:"enabled"`
//	    RefreshMinutes int  `json:"refresh_minutes" toml:"refresh_minutes"`
//	}
//	// Config gains a top-level table (sibling schema.go pattern):
//	//   Awareness AwarenessConfig `json:"awareness" toml:"awareness"`
//	const DefaultAwarenessRefreshMinutes = 30
//	func NormalizeAwarenessDefaults(c *AwarenessConfig)
//	//   RefreshMinutes <= 0 -> DefaultAwarenessRefreshMinutes (0 means
//	//   unset; negative is invalid). Enabled is NEVER forced — explicit
//	//   true/false and the omitted-key default (false) are both honored.
//	// DefaultConfig(): Enabled=false (D1 opt-in), RefreshMinutes=30.
//
// aw- prefixed helpers avoid collisions with sibling gate files in this
// package.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAwarenessDefaultsDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Awareness.Enabled {
		t.Fatal("[awareness].enabled must default false (D1 opt-in: a daemon that does not opt in renders byte-identical prompts)")
	}
	if cfg.Awareness.RefreshMinutes != DefaultAwarenessRefreshMinutes {
		t.Fatalf("refresh_minutes default = %d, want %d", cfg.Awareness.RefreshMinutes, DefaultAwarenessRefreshMinutes)
	}
}

func TestAwarenessNormalizeNonPositiveRefreshMinutes(t *testing.T) {
	for _, bad := range []int{0, -5} {
		c := AwarenessConfig{Enabled: true, RefreshMinutes: bad}
		NormalizeAwarenessDefaults(&c)
		if c.RefreshMinutes != DefaultAwarenessRefreshMinutes {
			t.Fatalf("NormalizeAwarenessDefaults(%d) -> %d, want %d", bad, c.RefreshMinutes, DefaultAwarenessRefreshMinutes)
		}
		if !c.Enabled {
			t.Fatal("normalization must never flip an explicit enabled=true")
		}
	}
	// Idempotent on valid values.
	c := AwarenessConfig{Enabled: false, RefreshMinutes: 15}
	NormalizeAwarenessDefaults(&c)
	if c.RefreshMinutes != 15 || c.Enabled {
		t.Fatalf("normalize mutated valid config: %+v", c)
	}
}

// TOML boundary: [awareness] parses through Load (the JSON5-first loader
// falls back to TOML for .toml content — the pre-existing meept.toml path).
func TestAwarenessTOMLRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := `
[awareness]
enabled = true
refresh_minutes = 45
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Awareness.Enabled {
		t.Fatal("enabled=true did not parse")
	}
	if cfg.Awareness.RefreshMinutes != 45 {
		t.Fatalf("refresh_minutes = %d, want 45", cfg.Awareness.RefreshMinutes)
	}

	// Omitted table keeps the disabled default (no behavior change for
	// existing daemons).
	bare := filepath.Join(dir, "bare", "meept.toml")
	if err := os.MkdirAll(filepath.Join(dir, "bare"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bare, []byte("[daemon]\nlog_level = \"info\"\n"), 0o600); err != nil {
		t.Fatalf("write bare config: %v", err)
	}
	cfg2, err := Load(bare)
	if err != nil {
		t.Fatalf("Load bare: %v", err)
	}
	if cfg2.Awareness.Enabled {
		t.Fatal("omitted [awareness] must stay disabled (D1)")
	}
	if cfg2.Awareness.RefreshMinutes != DefaultAwarenessRefreshMinutes {
		t.Fatalf("omitted table refresh_minutes = %d, want default %d", cfg2.Awareness.RefreshMinutes, DefaultAwarenessRefreshMinutes)
	}
}

// Load-boundary normalization (boundary 1: Load -> finalizeLoadedConfig):
// explicit refresh_minutes = 0 on disk normalizes to 30, enabled=false
// stays false.
func TestAwarenessLoadNormalizesZeroRefresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := `
[awareness]
enabled = false
refresh_minutes = 0
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Awareness.RefreshMinutes != DefaultAwarenessRefreshMinutes {
		t.Fatalf("Load boundary did not normalize refresh_minutes 0: got %d", cfg.Awareness.RefreshMinutes)
	}
	if cfg.Awareness.Enabled {
		t.Fatal("explicit enabled=false must survive normalization")
	}
}

// JSON5 boundary: the json5 key parses, and normalization is applied by
// the loader so both load boundaries hand consumers identical shapes.
func TestAwarenessJSON5OverrideParses(t *testing.T) {
	src := `
{
  awareness: {
    enabled: true,
    refresh_minutes: 10,
  },
}
`
	var cfg Config
	if err := LoadJSON5Bytes([]byte(src), &cfg); err != nil {
		t.Fatalf("LoadJSON5Bytes: %v", err)
	}
	if !cfg.Awareness.Enabled {
		t.Fatal("json5 enabled=true did not parse")
	}
	if cfg.Awareness.RefreshMinutes != 10 {
		t.Fatalf("json5 refresh_minutes = %d, want 10", cfg.Awareness.RefreshMinutes)
	}

	// Boundary 2: LoadJSON5Config normalizes a zero/absent value at the
	// same load seam as the TOML path (ledger-integrity precedent).
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	if err := os.WriteFile(path, []byte("{ awareness: { enabled: true } }"), 0o600); err != nil {
		t.Fatalf("write json5: %v", err)
	}
	cfg2, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("LoadJSON5Config: %v", err)
	}
	if cfg2.Awareness.RefreshMinutes != DefaultAwarenessRefreshMinutes {
		t.Fatalf("JSON5 load boundary did not normalize refresh_minutes: got %d", cfg2.Awareness.RefreshMinutes)
	}
	if !cfg2.Awareness.Enabled {
		t.Fatal("json5 enabled=true lost through LoadJSON5Config")
	}
}
