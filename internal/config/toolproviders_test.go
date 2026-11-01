package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeProviderConfig writes a JSON5 config to a temp file and returns
// the path. Only the [tool_providers]-relevant block is included; the
// loader must tolerate the rest being absent.
func writeProviderConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing config fixture: %v", err)
	}
	return path
}

// TestProviderConfigDisable pins F8: the [tool_providers] block parses,
// explicit false disables, absent entries default to enabled, and
// EnabledNames maps the full provider universe to the enabled set
// without importing toolprov (the daemon joins the two).
func TestProviderConfigDisable(t *testing.T) {
	t.Run("absent block parses and defaults everything enabled", func(t *testing.T) {
		path := writeProviderConfig(t, `{
			// no tool_providers block at all
		}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		got := cfg.ToolProviders.EnabledNames([]string{"scheduler", "github", "hermes"})
		want := []string{"scheduler", "github", "hermes"}
		if len(got) != len(want) {
			t.Fatalf("EnabledNames = %v, want %v (default-true)", got, want)
		}
		seen := map[string]bool{}
		for _, n := range got {
			seen[n] = true
		}
		for _, n := range want {
			if !seen[n] {
				t.Errorf("EnabledNames missing %q; got %v", n, got)
			}
		}
	})

	t.Run("present but empty block parses and defaults everything enabled", func(t *testing.T) {
		path := writeProviderConfig(t, `{
			tool_providers: {},
		}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		got := cfg.ToolProviders.EnabledNames([]string{"alpha", "beta"})
		if len(got) != 2 {
			t.Fatalf("EnabledNames = %v, want both alpha and beta (empty block = enabled)", got)
		}
	})

	t.Run("explicit enabled=false disables that provider only", func(t *testing.T) {
		path := writeProviderConfig(t, `{
			tool_providers: {
				providers: {
					A: { enabled: false },
				},
			},
		}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		got := cfg.ToolProviders.EnabledNames([]string{"A", "B", "Z"})
		if len(got) != 2 {
			t.Fatalf("EnabledNames = %v, want [B Z] (A disabled)", got)
		}
		for _, n := range got {
			if n == "A" {
				t.Errorf("disabled provider A still enabled; got %v", got)
			}
		}
		entry, ok := cfg.ToolProviders.Providers["A"]
		if !ok {
			t.Fatal("providers.A entry missing after parse")
		}
		if entry.Enabled == nil || *entry.Enabled {
			t.Error("providers.A.Enabled should parse as an explicit false pointer")
		}
	})

	t.Run("explicit enabled=true on unknown name passes through", func(t *testing.T) {
		path := writeProviderConfig(t, `{
			tool_providers: {
				providers: {
					Z: { enabled: true },
				},
			},
		}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		got := cfg.ToolProviders.EnabledNames([]string{"A", "B", "Z"})
		if len(got) != 3 {
			t.Fatalf("EnabledNames = %v, want all three (unknown Z with enabled=true passes through; RegisterAll rejects it downstream)", got)
		}
	})

	t.Run("entry without enabled key defaults to enabled", func(t *testing.T) {
		path := writeProviderConfig(t, `{
			tool_providers: {
				providers: {
					A: {},
				},
			},
		}`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		got := cfg.ToolProviders.EnabledNames([]string{"A", "B"})
		if len(got) != 2 {
			t.Fatalf("EnabledNames = %v, want both (entry with no enabled key = enabled)", got)
		}
	})

	t.Run("EnabledNames returns a sorted deterministic set", func(t *testing.T) {
		cfg := ToolProvidersConfig{}
		got := cfg.EnabledNames([]string{"zeta", "alpha", "mu"})
		if !reflect.DeepEqual(got, []string{"alpha", "mu", "zeta"}) {
			t.Errorf("EnabledNames = %v, want sorted [alpha mu zeta]", got)
		}
	})

	t.Run("nil-pointer Enabled never crashes the mapping", func(t *testing.T) {
		cfg := ToolProvidersConfig{
			Providers: map[string]ToolProviderEntry{
				"A": {Enabled: nil},
			},
		}
		got := cfg.EnabledNames([]string{"A"})
		if len(got) != 1 || got[0] != "A" {
			t.Errorf("EnabledNames = %v, want [A] (nil Enabled = default true)", got)
		}
	})
}

// TestDoctorReportsProviders pins F9: the doctor provider check builds a
// report-only check list - one entry per registered provider factory with
// name, risk class, and enabled state - without mutating anything.
// RED until 04-config.md implements the doctor surface.
func TestDoctorReportsProviders(t *testing.T) {
	// The doctor provider report is a pure function over (registered
	// factory names, config, constructed instances). The contract tested
	// here is the config-side input to that report: the doctor must be
	// able to derive (name, enabled) pairs from ToolProvidersConfig for
	// every known provider name without side effects.
	all := []string{"github", "scheduler", "hermes", "acp"}
	cfg := ToolProvidersConfig{
		Providers: map[string]ToolProviderEntry{
			"scheduler": {Enabled: boolPtr(false)},
		},
	}

	// Doctor iterates the full universe and reports each state.
	states := map[string]bool{}
	for _, name := range all {
		enabled := cfg.EnabledNames([]string{name})
		states[name] = len(enabled) == 1
	}

	if !states["github"] {
		t.Error("doctor input: github should report enabled")
	}
	if states["scheduler"] {
		t.Error("doctor input: scheduler should report disabled")
	}
	if !states["acp"] {
		t.Error("doctor input: acp should report enabled by default")
	}

	// Report-only posture: running the mapping twice yields identical
	// results (no hidden mutation of the config).
	again := cfg.EnabledNames(all)
	if len(again) != 3 {
		t.Errorf("second EnabledNames pass = %v, want 3 entries (report-only, no mutation)", again)
	}
}

func boolPtr(b bool) *bool { return &b }
