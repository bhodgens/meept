package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestDefaultConfig_FailurePolicy verifies the frozen defaults for the
// [llm.failure_policy] section (llm-resilience-forest tree 02 leaf 02,
// DECISIONS.md D5/D8): exponential base 30s, 402 quota extra 5m, 1h polling
// floor, 24h give-up horizon, 3 short retries, pacing off.
func TestDefaultConfig_FailurePolicy(t *testing.T) {
	c := DefaultConfig()
	fp := c.LLM.FailurePolicy

	if fp.Horizon != DefaultFailurePolicyHorizon {
		t.Errorf("horizon = %v, want %v", fp.Horizon, DefaultFailurePolicyHorizon)
	}
	if fp.BaseThrottle != DefaultFailurePolicyBaseThrottle {
		t.Errorf("base_throttle = %v, want %v", fp.BaseThrottle, DefaultFailurePolicyBaseThrottle)
	}
	if fp.BaseQuota402Extra != DefaultFailurePolicyBaseQuota402Extra {
		t.Errorf("base_quota_402_extra = %v, want %v", fp.BaseQuota402Extra, DefaultFailurePolicyBaseQuota402Extra)
	}
	if fp.PollFloor != DefaultFailurePolicyPollFloor {
		t.Errorf("poll_floor = %v, want %v", fp.PollFloor, DefaultFailurePolicyPollFloor)
	}
	if fp.ShortRetries != DefaultFailurePolicyShortRetries {
		t.Errorf("short_retries = %d, want %d", fp.ShortRetries, DefaultFailurePolicyShortRetries)
	}
	if fp.Pacing.Enabled != DefaultPacingEnabled {
		t.Errorf("pacing.enabled = %v, want %v (D15: pacing is opt-in)", fp.Pacing.Enabled, DefaultPacingEnabled)
	}
	if fp.Pacing.MinInterval != DefaultPacingMinInterval {
		t.Errorf("pacing.min_interval = %v, want %v", fp.Pacing.MinInterval, DefaultPacingMinInterval)
	}
	if fp.Pacing.MaxInterval != DefaultPacingMaxInterval {
		t.Errorf("pacing.max_interval = %v, want %v", fp.Pacing.MaxInterval, DefaultPacingMaxInterval)
	}
}

// TestNormalizeFailurePolicyDefaults checks the load-boundary clamps:
// zero/negative durations take defaults, a non-positive ShortRetries takes
// the default, valid values pass through idempotently, and max_interval <
// min_interval lifts max back to its default (so a bad pair cannot pin
// pacing above its ceiling).
func TestNormalizeFailurePolicyDefaults(t *testing.T) {
	t.Run("zero value gets defaults", func(t *testing.T) {
		var f FailurePolicyConfig
		NormalizeFailurePolicyDefaults(&f)
		if f.Horizon != DefaultFailurePolicyHorizon ||
			f.BaseThrottle != DefaultFailurePolicyBaseThrottle ||
			f.BaseQuota402Extra != DefaultFailurePolicyBaseQuota402Extra ||
			f.PollFloor != DefaultFailurePolicyPollFloor ||
			f.ShortRetries != DefaultFailurePolicyShortRetries ||
			f.Pacing.MinInterval != DefaultPacingMinInterval ||
			f.Pacing.MaxInterval != DefaultPacingMaxInterval {
			t.Fatalf("zero value not normalized: %+v", f)
		}
	})

	t.Run("negative durations take defaults", func(t *testing.T) {
		f := FailurePolicyConfig{Horizon: -1, BaseThrottle: -time.Minute, PollFloor: -1}
		NormalizeFailurePolicyDefaults(&f)
		if f.Horizon != DefaultFailurePolicyHorizon ||
			f.BaseThrottle != DefaultFailurePolicyBaseThrottle ||
			f.PollFloor != DefaultFailurePolicyPollFloor {
			t.Fatalf("negatives not normalized: %+v", f)
		}
	})

	t.Run("valid values preserved", func(t *testing.T) {
		f := FailurePolicyConfig{
			Horizon:      48 * time.Hour,
			BaseThrottle: 45 * time.Second,
			PollFloor:    2 * time.Hour,
			ShortRetries: 5,
		}
		NormalizeFailurePolicyDefaults(&f)
		if f.Horizon != 48*time.Hour || f.BaseThrottle != 45*time.Second ||
			f.PollFloor != 2*time.Hour || f.ShortRetries != 5 {
			t.Fatalf("valid values clobbered: %+v", f)
		}
	})

	t.Run("pacing max below min lifts to default max", func(t *testing.T) {
		f := FailurePolicyConfig{Pacing: PacingConfig{MinInterval: 10 * time.Second, MaxInterval: 2 * time.Second}}
		NormalizeFailurePolicyDefaults(&f)
		if f.Pacing.MaxInterval != DefaultPacingMaxInterval {
			t.Fatalf("max_interval = %v, want default %v when below min", f.Pacing.MaxInterval, DefaultPacingMaxInterval)
		}
	})
}

// TestFailurePolicyConfigTags pins the json/toml tag surface: the wire keys
// (llm.failure_policy.* and llm.failure_policy.pacing.*) must exist on the
// schema so config get/set and the docs stay in 1:1 correspondence.
func TestFailurePolicyConfigTags(t *testing.T) {
	tests := []struct {
		name    string
		jsonTag string
		tomlTag string
	}{
		{name: "horizon", jsonTag: "horizon", tomlTag: "horizon"},
		{name: "base_throttle", jsonTag: "base_throttle", tomlTag: "base_throttle"},
		{name: "base_quota_402_extra", jsonTag: "base_quota_402_extra", tomlTag: "base_quota_402_extra"},
		{name: "poll_floor", jsonTag: "poll_floor", tomlTag: "poll_floor"},
		{name: "short_retries", jsonTag: "short_retries", tomlTag: "short_retries"},
		{name: "pacing", jsonTag: "pacing", tomlTag: "pacing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := reflect.TypeOf(FailurePolicyConfig{})
			var field reflect.StructField
			var found bool
			for i := 0; i < st.NumField(); i++ {
				if strings.Split(st.Field(i).Tag.Get("json"), ",")[0] == tt.jsonTag {
					field, found = st.Field(i), true
					break
				}
			}
			if !found {
				t.Fatalf("field with json tag %q not found on FailurePolicyConfig", tt.jsonTag)
			}
			if got := field.Tag.Get("toml"); got != tt.tomlTag {
				t.Errorf("toml tag = %q, want %q", got, tt.tomlTag)
			}
		})
	}
}

// TestFailurePolicyConfigJSON5RoundTrip proves the section survives a real
// LoadJSON5Config pass: overrides for every knob land on the struct
// (quoted duration strings per the JSON5 loader's duration handling) while
// untouched knobs keep their defaults (unmarshal ONTO DefaultConfig).
func TestFailurePolicyConfigJSON5RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "llm": {
    "failure_policy": {
      "horizon": "48h",
      "base_throttle": "45s",
      "base_quota_402_extra": "10m",
      "poll_floor": "2h",
      "short_retries": 5,
      "pacing": {
        "enabled": true,
        "min_interval": "2s",
        "max_interval": "45s",
      },
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
	fp := cfg.LLM.FailurePolicy
	if fp.Horizon != 48*time.Hour {
		t.Errorf("horizon = %v, want 48h", fp.Horizon)
	}
	if fp.BaseThrottle != 45*time.Second {
		t.Errorf("base_throttle = %v, want 45s", fp.BaseThrottle)
	}
	if fp.BaseQuota402Extra != 10*time.Minute {
		t.Errorf("base_quota_402_extra = %v, want 10m", fp.BaseQuota402Extra)
	}
	if fp.PollFloor != 2*time.Hour {
		t.Errorf("poll_floor = %v, want 2h", fp.PollFloor)
	}
	if fp.ShortRetries != 5 {
		t.Errorf("short_retries = %d, want 5", fp.ShortRetries)
	}
	if !fp.Pacing.Enabled {
		t.Error("pacing.enabled = false, want true")
	}
	if fp.Pacing.MinInterval != 2*time.Second {
		t.Errorf("pacing.min_interval = %v, want 2s", fp.Pacing.MinInterval)
	}
	if fp.Pacing.MaxInterval != 45*time.Second {
		t.Errorf("pacing.max_interval = %v, want 45s", fp.Pacing.MaxInterval)
	}
}
