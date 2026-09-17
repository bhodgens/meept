package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTurnWatchdogConfig_Defaults pins the leaf-06 defaults: the reaper is
// ENABLED by default with 30s pass interval and 120s staleness threshold.
// A silent default-off would resurrect the exact silent-death failure mode
// the watchdog exists to close.
func TestTurnWatchdogConfig_Defaults(t *testing.T) {
	c := DefaultConfig()
	tw := c.Orchestrator.TurnWatchdog
	if !tw.Enabled {
		t.Error("TurnWatchdog.Enabled default = false, want true")
	}
	if tw.IntervalSeconds != 30 {
		t.Errorf("TurnWatchdog.IntervalSeconds default = %d, want 30", tw.IntervalSeconds)
	}
	if tw.StaleAfterSeconds != 120 {
		t.Errorf("TurnWatchdog.StaleAfterSeconds default = %d, want 120", tw.StaleAfterSeconds)
	}
}

// TestTurnWatchdogConfig_TemplateParses keeps the shipped config/meept.json5
// template honest: the orchestrator turn_watchdog block must parse into
// TurnWatchdogConfig with the documented enabled-by-default semantics.
// Mirrors TestMeeptTemplate_ClassifierPrefilterBlock.
func TestTurnWatchdogConfig_TemplateParses(t *testing.T) {
	const templatePath = "../../config/meept.json5"
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("template not found from test cwd: %v", err)
	}
	abs, err := filepath.Abs(templatePath)
	if err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	if err := LoadJSON5(abs, c); err != nil {
		t.Fatalf("template meept.json5 failed to parse: %v", err)
	}

	tw := c.Orchestrator.TurnWatchdog
	if !tw.Enabled {
		t.Error("template ships turn_watchdog enabled=false; must ship enabled=true")
	}
	if tw.IntervalSeconds != 30 {
		t.Errorf("template turn_watchdog interval_seconds = %d, want 30", tw.IntervalSeconds)
	}
	if tw.StaleAfterSeconds != 120 {
		t.Errorf("template turn_watchdog stale_after_seconds = %d, want 120", tw.StaleAfterSeconds)
	}
}

// TestTurnWatchdogConfig_ExplicitDisableSurvivesLoad proves the zero-value
// ambiguity is on the normalize side only: an explicit user "enabled":
// false (loaded from a template-shaped config) must survive and not be
// clobbered back to true by the defaults machinery.
func TestTurnWatchdogConfig_ExplicitDisableSurvivesLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	body := `{
  "orchestrator": {
    "turn_watchdog": {
      "enabled": false,
      "interval_seconds": 5,
      "stale_after_seconds": 10,
    },
  },
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	if err := LoadJSON5(path, c); err != nil {
		t.Fatalf("load: %v", err)
	}
	tw := c.Orchestrator.TurnWatchdog
	if tw.Enabled {
		t.Error("explicit enabled=false was clobbered to true by load")
	}
	if tw.IntervalSeconds != 5 || tw.StaleAfterSeconds != 10 {
		t.Errorf("explicit values lost: interval=%d stale=%d, want 5/10",
			tw.IntervalSeconds, tw.StaleAfterSeconds)
	}
}
