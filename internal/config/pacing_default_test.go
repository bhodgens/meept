package config

// Pacing default-flip test (endpoint-wait overhaul, part B): adaptive
// pacing is now ON by default (D15 default updated after the ticket-
// reservation gap fix). The knob remains: an explicit
// `pacing.enabled = false` must survive load + normalization byte-identically
// (Enabled is deliberately NOT normalized — the default-on value comes from
// DefaultConfig(), which user config unmarshals onto).

import (
	"encoding/json"
	"testing"
)

// TestDefaultConfig_PacingEnabledByDefault pins the flipped default: the
// config default constructs the pacer (components.go wiring), while every
// interval knob keeps its documented value.
func TestDefaultConfig_PacingEnabledByDefault(t *testing.T) {
	c := DefaultConfig()
	fp := c.LLM.FailurePolicy
	if !fp.Pacing.Enabled {
		t.Fatal("pacing.enabled = false, want true (pacing is default-ON)")
	}
	if fp.Pacing.Target429PerHour != DefaultPacingTarget429Hour {
		t.Errorf("target_429_per_hour = %d, want %d", fp.Pacing.Target429PerHour, DefaultPacingTarget429Hour)
	}
	if fp.Pacing.MinInterval != DefaultPacingMinInterval {
		t.Errorf("min_interval = %v, want %v", fp.Pacing.MinInterval, DefaultPacingMinInterval)
	}
	if fp.Pacing.MaxInterval != DefaultPacingMaxInterval {
		t.Errorf("max_interval = %v, want %v", fp.Pacing.MaxInterval, DefaultPacingMaxInterval)
	}
}

// TestPacingExplicitDisableSurvivesLoad pins the opt-out: a user config
// carrying `pacing.enabled = false` keeps it false after default-on
// inheritance + normalization (the knob must keep working).
func TestPacingExplicitDisableSurvivesLoad(t *testing.T) {
	// Inherit the (now true) default, then apply an explicit disable the
	// way the JSON5/TOML load does, then normalize.
	c := DefaultConfig()
	overlay := struct {
		Pacing PacingConfig `json:"pacing"`
	}{}
	if err := json.Unmarshal([]byte(`{"pacing":{"enabled":false}}`), &overlay); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c.LLM.FailurePolicy.Pacing.Enabled = overlay.Pacing.Enabled

	NormalizeFailurePolicyDefaults(&c.LLM.FailurePolicy)
	if c.LLM.FailurePolicy.Pacing.Enabled {
		t.Fatal("pacing.enabled = true after explicit disable, want false (opt-out must survive load)")
	}
}
