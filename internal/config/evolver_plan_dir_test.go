package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvolverPlanDir_EmptyResolvesToUserSink verifies the normalization
// contract: an empty skills.evolver.plan_dir resolves to the user-scoped
// sink ~/.meept/plans/evolver (fixture HOME, never the developer's home).
func TestEvolverPlanDir_EmptyResolvesToUserSink(t *testing.T) {
	fixture := t.TempDir()
	t.Setenv("HOME", fixture)
	cfg := DefaultConfig()
	NormalizeEvolverDefaults(&cfg.Skills.Evolver)

	want := filepath.Join(fixture, ".meept", "plans", "evolver")
	if got := cfg.Skills.Evolver.PlanDir; got != want {
		t.Fatalf("empty plan_dir normalized to %q, want %q", got, want)
	}
}

// TestEvolverPlanDir_TildeExpanded verifies a user-configured ~-prefixed path
// is expanded to the fixture HOME.
func TestEvolverPlanDir_TildeExpanded(t *testing.T) {
	fixture := t.TempDir()
	t.Setenv("HOME", fixture)
	cfg := DefaultConfig()
	cfg.Skills.Evolver.PlanDir = "~/my/evolver-sink"
	NormalizeEvolverDefaults(&cfg.Skills.Evolver)

	want := filepath.Join(fixture, "my", "evolver-sink")
	if got := cfg.Skills.Evolver.PlanDir; got != want {
		t.Fatalf("plan_dir = %q, want %q", got, want)
	}
}

// TestEvolverPlanDir_AbsolutePassthrough verifies an absolute configured path
// passes through normalization unchanged.
func TestEvolverPlanDir_AbsolutePassthrough(t *testing.T) {
	fixture := t.TempDir()
	t.Setenv("HOME", fixture)
	abs := filepath.Join(fixture, "data", "sink")
	cfg := DefaultConfig()
	cfg.Skills.Evolver.PlanDir = abs
	NormalizeEvolverDefaults(&cfg.Skills.Evolver)

	if got := cfg.Skills.Evolver.PlanDir; got != abs {
		t.Fatalf("plan_dir = %q, want unchanged %q", got, abs)
	}
}

// TestEvolverPlanDir_RelativeRejected documents the chosen contract for
// relative paths: normalization REJECTS them (returns an error) rather than
// resolving them against the daemon's arbitrary CWD — the whole point of the
// sink is that plan landing must not depend on CWD.
func TestEvolverPlanDir_RelativeRejected(t *testing.T) {
	fixture := t.TempDir()
	t.Setenv("HOME", fixture)
	cfg := DefaultConfig()
	cfg.Skills.Evolver.PlanDir = "relative/sink"
	err := NormalizeEvolverDefaults(&cfg.Skills.Evolver)
	if err == nil {
		t.Fatal("relative plan_dir must be rejected by normalization, got nil error")
	}
	if !strings.Contains(err.Error(), "relative") {
		t.Fatalf("error %q should mention 'relative'", err)
	}
}

// TestEvolverPlanDir_ExistingFieldsUnchanged verifies normalization does not
// disturb the other evolver fields (guard against over-eager normalization).
func TestEvolverPlanDir_ExistingFieldsUnchanged(t *testing.T) {
	fixture := t.TempDir()
	t.Setenv("HOME", fixture)
	cfg := DefaultConfig()
	cfg.Skills.Evolver.Enabled = true
	cfg.Skills.Evolver.Interval = 42 * 3600 * 1e9 // 42h as time.Duration
	cfg.Skills.Evolver.AutoApply = true
	cfg.Skills.Evolver.RunOnStart = true
	planDir := filepath.Join(fixture, "sink")
	cfg.Skills.Evolver.PlanDir = planDir
	if err := NormalizeEvolverDefaults(&cfg.Skills.Evolver); err != nil {
		t.Fatalf("NormalizeEvolverDefaults: %v", err)
	}
	if !cfg.Skills.Evolver.Enabled {
		t.Error("enabled lost")
	}
	if got := cfg.Skills.Evolver.Interval; got != 42*3600*1e9 {
		t.Errorf("interval = %v, want 42h", got)
	}
	if !cfg.Skills.Evolver.AutoApply {
		t.Error("auto_apply lost")
	}
	if !cfg.Skills.Evolver.RunOnStart {
		t.Error("run_on_start lost")
	}
	if got := cfg.Skills.Evolver.PlanDir; got != planDir {
		t.Errorf("plan_dir = %q, want %q", got, planDir)
	}
}

// TestEvolverPlanDir_LoadJSON5Wired verifies the daemon -c load path (JSON5)
// applies normalization: an empty plan_dir in a loaded config resolves to the
// fixture sink.
func TestEvolverPlanDir_LoadJSON5Wired(t *testing.T) {
	fixture := t.TempDir()
	t.Setenv("HOME", fixture)
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
	cfg, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("LoadJSON5Config: %v", err)
	}
	want := filepath.Join(fixture, ".meept", "plans", "evolver")
	if got := cfg.Skills.Evolver.PlanDir; got != want {
		t.Fatalf("loaded plan_dir = %q, want %q", got, want)
	}
}

// TestEvolverPlanDir_LoadTOMLWired verifies the legacy TOML load path applies
// normalization the same way.
func TestEvolverPlanDir_LoadTOMLWired(t *testing.T) {
	fixture := t.TempDir()
	t.Setenv("HOME", fixture)
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := `[skills.evolver]
enabled = true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := filepath.Join(fixture, ".meept", "plans", "evolver")
	if got := cfg.Skills.Evolver.PlanDir; got != want {
		t.Fatalf("loaded plan_dir = %q, want %q", got, want)
	}
}
