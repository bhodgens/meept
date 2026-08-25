package config

import (
	"testing"
)

// TestRuntimeConfig_EnvPolicyDefaults verifies that a freshly constructed
// RuntimeConfig carries NO env-policy defaults pre-normalization: Mode must
// be empty so user configuration is not silently overwritten before
// NormalizeRuntimeDefaults runs.
func TestRuntimeConfig_EnvPolicyDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Runtime.EnvPolicy.Mode != "" {
		t.Fatalf("expected empty mode pre-normalization, got %q", cfg.Runtime.EnvPolicy.Mode)
	}
	if len(cfg.Runtime.EnvPolicy.Allowlist) != 0 {
		t.Fatalf("expected empty allowlist pre-normalization, got %v", cfg.Runtime.EnvPolicy.Allowlist)
	}
	if len(cfg.Runtime.EnvPolicy.DenyGlobs) != 0 {
		t.Fatalf("expected empty deny globs pre-normalization, got %v", cfg.Runtime.EnvPolicy.DenyGlobs)
	}
}

// TestRuntimeConfig_EnvPolicyNormalize verifies the normalization helper:
// empty Mode becomes "allowlist" (secure default) and DenyGlobs are defaulted
// to the standard secret-ish name globs when unset.
func TestRuntimeConfig_EnvPolicyNormalize(t *testing.T) {
	cfg := DefaultConfig()
	NormalizeRuntimeDefaults(&cfg.Runtime)

	if cfg.Runtime.EnvPolicy.Mode != "allowlist" {
		t.Fatalf("default mode after normalize must be allowlist, got %q", cfg.Runtime.EnvPolicy.Mode)
	}
	foundDeny := false
	for _, g := range cfg.Runtime.EnvPolicy.DenyGlobs {
		if g == "*KEY*" {
			foundDeny = true
		}
	}
	if !foundDeny {
		t.Fatalf("default deny globs missing *KEY*; got %v", cfg.Runtime.EnvPolicy.DenyGlobs)
	}
	want := []string{"*KEY*", "*TOKEN*", "*SECRET*", "*PASSWORD*", "*CREDENTIAL*"}
	if len(cfg.Runtime.EnvPolicy.DenyGlobs) != len(want) {
		t.Fatalf("expected %d default deny globs, got %d: %v", len(want), len(cfg.Runtime.EnvPolicy.DenyGlobs), cfg.Runtime.EnvPolicy.DenyGlobs)
	}
	for i, g := range want {
		if cfg.Runtime.EnvPolicy.DenyGlobs[i] != g {
			t.Fatalf("deny glob[%d] = %q, want %q", i, cfg.Runtime.EnvPolicy.DenyGlobs[i], g)
		}
	}
}

// TestRuntimeConfig_EnvPolicyNormalizePreservesUserSettings verifies that
// explicit user choices survive normalization untouched.
func TestRuntimeConfig_EnvPolicyNormalizePreservesUserSettings(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Runtime.EnvPolicy.Mode = "inherit"
	cfg.Runtime.EnvPolicy.Allowlist = []string{"MY_VAR"}
	cfg.Runtime.EnvPolicy.DenyGlobs = []string{"*CUSTOM*"}

	NormalizeRuntimeDefaults(&cfg.Runtime)

	if cfg.Runtime.EnvPolicy.Mode != "inherit" {
		t.Fatalf("explicit inherit mode must be preserved, got %q", cfg.Runtime.EnvPolicy.Mode)
	}
	if len(cfg.Runtime.EnvPolicy.Allowlist) != 1 || cfg.Runtime.EnvPolicy.Allowlist[0] != "MY_VAR" {
		t.Fatalf("user allowlist must be preserved, got %v", cfg.Runtime.EnvPolicy.Allowlist)
	}
	if len(cfg.Runtime.EnvPolicy.DenyGlobs) != 1 || cfg.Runtime.EnvPolicy.DenyGlobs[0] != "*CUSTOM*" {
		t.Fatalf("user deny globs must be preserved, got %v", cfg.Runtime.EnvPolicy.DenyGlobs)
	}
}
