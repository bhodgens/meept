package config

// NOTE: gate tests for the github-intake-factory tree (leaf 00T) — F6.
// Intentionally RED: the [intake] block (Contract 4: IntakeConfig + the
// root Config.Intake field) lands in leaf 04, and the loader's
// stringDurationKeys exemption gains "poll_interval" in the same leaf.
// Compile failures naming IntakeConfig / Config.Intake are the expected
// RED; the duration-round-trip test additionally stays RED until BOTH the
// struct AND the loader exemption land (that two-sided RED is correct per
// the master Contract 4 note).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestIntakeConfig_Parse: a full [intake] block parses into Config.Intake
// with every field of Contract 4 (F6, leaf 04 Task 1).
func TestIntakeConfig_Parse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "intake": {
    "enabled": true,
    "default_employee": "factory-bot",
    "poll_interval": "5m",
    "watch_repo": "octo/forge",
    "required_labels": ["factory", "intake"],
    "done_label": "factory:done",
    "gate_command": "go test ./...",
    "project_number": 42,
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("LoadJSON5Config: %v", err)
	}
	in := cfg.Intake
	if !in.Enabled {
		t.Errorf("intake.enabled = false, want true")
	}
	if in.DefaultEmployee != "factory-bot" {
		t.Errorf("intake.default_employee = %q, want %q", in.DefaultEmployee, "factory-bot")
	}
	if in.PollInterval != "5m" {
		t.Errorf("intake.poll_interval = %q, want the STRING %q", in.PollInterval, "5m")
	}
	if in.WatchRepo != "octo/forge" {
		t.Errorf("intake.watch_repo = %q, want %q", in.WatchRepo, "octo/forge")
	}
	if len(in.RequiredLabels) != 2 || in.RequiredLabels[0] != "factory" || in.RequiredLabels[1] != "intake" {
		t.Errorf("intake.required_labels = %v, want [factory intake]", in.RequiredLabels)
	}
	if in.DoneLabel != "factory:done" {
		t.Errorf("intake.done_label = %q, want %q", in.DoneLabel, "factory:done")
	}
	if in.GateCommand != "go test ./..." {
		t.Errorf("intake.gate_command = %q, want %q", in.GateCommand, "go test ./...")
	}
	if in.ProjectNumber != 42 {
		t.Errorf("intake.project_number = %d, want 42", in.ProjectNumber)
	}
}

// TestIntakeConfig_DurationRoundTrip: CRITICAL two-sided regression
// (master Contract 4 note). Saving a config with poll_interval = "5m" and
// reloading it must yield the STRING "5m" — the json5 duration-rewrite
// pass must exempt intake.poll_interval exactly as it exempts
// queue.interactive_window — AND a real duration-typed field elsewhere
// must still convert to nanoseconds (F6, leaf 04 Task 2).
//
// This test stays RED until leaf 04 lands BOTH the IntakeConfig struct
// AND the stringDurationKeys exemption; that is the pinned gate.
func TestIntakeConfig_DurationRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "intake": {
    "enabled": true,
    "default_employee": "factory-bot",
    "poll_interval": "5m",
    "watch_repo": "octo/forge",
  },
  "llm": {
    "failure_policy": {
      "horizon": "24h",
    },
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if got := cfg.Intake.PollInterval; got != "5m" {
		t.Fatalf("intake.poll_interval after load = %q, want the string %q (loader rewrote a declared-string duration)", got, "5m")
	}
	if got := cfg.LLM.FailurePolicy.Horizon; got != 24*time.Hour {
		t.Fatalf("llm.failure_policy.horizon = %v, want 24h (real duration fields must still convert)", got)
	}

	// Side 2: serialize the parsed config back to JSON5/JSON and reload —
	// the round trip through the writer must not corrupt the string form.
	saved := marshalConfigForRoundTrip(t, cfg)
	path2 := filepath.Join(dir, "meept.reloaded.json5")
	if err := os.WriteFile(path2, []byte(saved), 0o600); err != nil {
		t.Fatalf("write reloaded config: %v", err)
	}
	cfg2, err := LoadJSON5Config(path2)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := cfg2.Intake.PollInterval; got != "5m" {
		t.Errorf("intake.poll_interval after save/load round trip = %q, want %q", got, "5m")
	}
	if got := cfg2.LLM.FailurePolicy.Horizon; got != 24*time.Hour {
		t.Errorf("failure_policy.horizon after round trip = %v, want 24h", got)
	}

	// Side 3 (loader-level pin, the stringDurationKeys mechanism): the
	// preprocessDurations pass itself must leave poll_interval quoted.
	out := preprocessDurations(content)
	if !strings.Contains(out, `"poll_interval": "5m"`) && !strings.Contains(out, `poll_interval": "5m"`) {
		t.Errorf("preprocessDurations rewrote intake.poll_interval:\n%s", out)
	}
	if !strings.Contains(out, `"horizon": 86400000000000`) {
		t.Errorf("preprocessDurations did not convert llm.failure_policy.horizon:\n%s", out)
	}
}

// marshalConfigForRoundTrip serializes cfg the way the config writer does
// (configui save path): indented JSON via the standard encoder. The
// intake schema is JSON-only, so encoding/json is the wire truth.
func marshalConfigForRoundTrip(t *testing.T, cfg *Config) string {
	t.Helper()
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config for round trip: %v", err)
	}
	return string(b)
}
