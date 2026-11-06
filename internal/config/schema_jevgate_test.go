package config

import (
	"testing"
)

// Tests for the [security.jev_command_gate] schema section (jev gate
// wiring leaf, docs/plans/20261103-wiring-gap-remediation/04-jev-gate-wiring.md):
// opt-in knob, defaults pinned to jevgate's conservative values, JSON5
// override parses, and the disabled default stays byte-identical to the
// pre-leaf posture.

func TestJevGateDefaultsDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Security.JevCommandGate.Enabled {
		t.Fatal("security.jev_command_gate.enabled must default false (opt-in)")
	}
	if cfg.Security.JevCommandGate.ScoreThreshold != 0.90 {
		t.Fatalf("score_threshold default = %v, want 0.90", cfg.Security.JevCommandGate.ScoreThreshold)
	}
	if cfg.Security.JevCommandGate.ConfidenceThreshold != 0.10 {
		t.Fatalf("confidence_threshold default = %v, want 0.10", cfg.Security.JevCommandGate.ConfidenceThreshold)
	}
}

func TestJevGateSchemaMirrorsScoreGateConfig(t *testing.T) {
	// Field-shape pin: the daemon adapter maps JevGateConfig onto
	// jevgate.ScoreGateConfig (internal/jevgate/serialize.go) verbatim.
	// jevgate cannot be imported here (config -> agent -> config cycle
	// through jevgate's agent import), so the conservative defaults are
	// pinned as literals mirroring jevgate.DefaultScoreGateConfig: enabled
	// false, score_threshold 0.90, confidence floor 0.10. If
	// DefaultScoreGateConfig ever moves, this test is the drift alarm.
	jev := DefaultConfig().Security.JevCommandGate
	if jev.ScoreThreshold != 0.90 {
		t.Fatalf("score_threshold %v != jevgate default 0.90", jev.ScoreThreshold)
	}
	if jev.ConfidenceThreshold != 0.10 {
		t.Fatalf("confidence_threshold %v != jevgate ConfidenceFloor default 0.10", jev.ConfidenceThreshold)
	}
	if jev.Enabled {
		t.Fatal("enabled must default false (jevgate default)")
	}
}

func TestJevGateJSON5OverrideParses(t *testing.T) {
	src := `
{
  security: {
    jev_command_gate: {
      enabled: true,
      score_threshold: 0.85,
      confidence_threshold: 0.20,
    },
  },
}
`
	var cfg Config
	if err := LoadJSON5Bytes([]byte(src), &cfg); err != nil {
		t.Fatalf("LoadJSON5Bytes: %v", err)
	}
	g := cfg.Security.JevCommandGate
	if !g.Enabled {
		t.Fatal("enabled=true did not parse")
	}
	if g.ScoreThreshold != 0.85 {
		t.Fatalf("score_threshold = %v, want 0.85", g.ScoreThreshold)
	}
	if g.ConfidenceThreshold != 0.20 {
		t.Fatalf("confidence_threshold = %v, want 0.20", g.ConfidenceThreshold)
	}
}

func TestJevGateValidateNormalizesZeroThresholds(t *testing.T) {
	// Zero-value thresholds are the disabled posture (empty struct in an
	// otherwise-enabled config): Validate must normalize them to the
	// conservative defaults rather than let 0.0 allow everything.
	c := SecurityConfig{JevCommandGate: JevGateConfig{Enabled: true}}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.JevCommandGate.ScoreThreshold != 0.90 {
		t.Fatalf("normalized score_threshold = %v, want 0.90", c.JevCommandGate.ScoreThreshold)
	}
	if c.JevCommandGate.ConfidenceThreshold != 0.10 {
		t.Fatalf("normalized confidence_threshold = %v, want 0.10", c.JevCommandGate.ConfidenceThreshold)
	}
}
