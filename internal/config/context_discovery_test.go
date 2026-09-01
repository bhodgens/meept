package config

import "testing"

// TestDefaultConfig_ContextDiscovery verifies the frozen defaults for the
// [llm.context_discovery] section (llm-resilience-forest tree 05 leaf 01):
// discovery defaults OFF (opt-in; zero behavior change + zero network
// when off), and the interval default is populated so an enabled section
// inherits a sane cadence.
func TestDefaultConfig_ContextDiscovery(t *testing.T) {
	c := DefaultConfig()
	if c.LLM.ContextDiscovery.Enabled {
		t.Fatal("context_discovery default must be disabled (zero behavior change when off)")
	}
	if c.LLM.ContextDiscovery.Interval != DefaultContextDiscoveryInterval {
		t.Fatalf("interval default: got %v, want %v", c.LLM.ContextDiscovery.Interval, DefaultContextDiscoveryInterval)
	}
	if c.LLM.ContextDiscovery.AllowContextOverride {
		t.Fatal("allow_context_override default must be false (catalog wins unless allowed)")
	}
}

// TestNormalizeContextDiscoveryDefaults checks the load-boundary clamps:
// zero and negative intervals take the default; valid values pass through
// (idempotent).
func TestNormalizeContextDiscoveryDefaults(t *testing.T) {
	t.Run("zero takes default", func(t *testing.T) {
		var c ContextDiscoveryConfig
		NormalizeContextDiscoveryDefaults(&c)
		if c.Interval != DefaultContextDiscoveryInterval {
			t.Fatalf("zero interval: got %v, want %v", c.Interval, DefaultContextDiscoveryInterval)
		}
	})
	t.Run("negative takes default", func(t *testing.T) {
		c := ContextDiscoveryConfig{Interval: -1}
		NormalizeContextDiscoveryDefaults(&c)
		if c.Interval != DefaultContextDiscoveryInterval {
			t.Fatalf("negative interval: got %v, want %v", c.Interval, DefaultContextDiscoveryInterval)
		}
	})
	t.Run("valid value preserved", func(t *testing.T) {
		c := ContextDiscoveryConfig{Interval: 123 * 1e9} // 123s as time.Duration
		NormalizeContextDiscoveryDefaults(&c)
		if c.Interval != 123*1e9 {
			t.Fatalf("valid interval clobbered: %v", c.Interval)
		}
	})
}
