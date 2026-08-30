package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// durationProbeConfig mirrors the shape that broke in the wiki smoke test:
// a time.Duration field inside a nested object loaded via LoadJSON5 (the
// daemon -c path). Before the fix, LoadJSON5 skipped the duration
// preprocessing that UnmarshalJSON5 applied, so "1h" failed with
// "type mismatch: found string, expected time.Duration".
type durationProbeConfig struct {
	Skills struct {
		Evolver struct {
			Enabled  bool          `json:"enabled"`
			Interval time.Duration `json:"interval"`
		} `json:"evolver"`
	} `json:"skills"`
}

// TestLoadJSON5_QuotedDurationString reproduces the smoke-test failure: a
// QUOTED Go duration string ("1h") as a JSON value for a time.Duration field
// must parse through the LoadJSON5 (daemon -c) path exactly as the TOML path
// allows.
func TestLoadJSON5_QuotedDurationString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "skills": {
    "evolver": {
      "enabled": true,
      "interval": "1h",
    },
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var cfg durationProbeConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		t.Fatalf("LoadJSON5 with quoted duration: %v", err)
	}
	if !cfg.Skills.Evolver.Enabled {
		t.Fatal("enabled flag lost")
	}
	if got := cfg.Skills.Evolver.Interval; got != time.Hour {
		t.Fatalf("interval = %v, want 1h", got)
	}
}

// TestLoadJSON5_BareDurationLiteral covers the bare (unquoted) form: 6h.
func TestLoadJSON5_BareDurationLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "skills": {
    "evolver": {
      "interval": 6h,
    },
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var cfg durationProbeConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		t.Fatalf("LoadJSON5 with bare duration: %v", err)
	}
	if got := cfg.Skills.Evolver.Interval; got != 6*time.Hour {
		t.Fatalf("interval = %v, want 6h", got)
	}
}

// TestLoadJSON5_NanosecondInteger covers the numeric form used as the
// workaround (3600000000000) — must keep working.
func TestLoadJSON5_NanosecondInteger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "skills": {
    "evolver": {
      "interval": 3600000000000,
    },
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var cfg durationProbeConfig
	if err := LoadJSON5(path, &cfg); err != nil {
		t.Fatalf("LoadJSON5 with nanosecond integer: %v", err)
	}
	if got := cfg.Skills.Evolver.Interval; got != time.Hour {
		t.Fatalf("interval = %v, want 1h", got)
	}
}

// TestLoadJSON5_DurationLikeStringsPreserved guards the preprocessor's
// colon-anchored regex: duration-looking text that is NOT a JSON value
// (no preceding colon) must pass through unmodified.
func TestLoadJSON5_DurationLikeStringsPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "description": "runs about 1h and 30m total",
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var probe struct {
		Description string `json:"description"`
	}
	if err := LoadJSON5(path, &probe); err != nil {
		t.Fatalf("LoadJSON5: %v", err)
	}
	if probe.Description != "runs about 1h and 30m total" {
		t.Fatalf("description mangled by duration preprocessing: %q", probe.Description)
	}
}
