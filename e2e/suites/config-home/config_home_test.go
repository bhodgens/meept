//go:build e2e

// Package confighome covers config-home-01: MEEPT_HOME redirects every
// meept path, including config values carrying the ~/.meept prefix.
package confighome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// config-home-01: with MEEPT_HOME set, MeeptHome() resolves to the override
// and ExpandMeeptPath redirects the shipped "~/.meept" default (and any
// "~/.meept/<suffix>" config value) under the override. Other "~" paths keep
// expanding to the user's real home; non-tilde paths pass through.
func TestMEEPTHomeRedirectsAllPaths(t *testing.T) {
	override := filepath.Join(t.TempDir(), "meept-home")
	if err := os.MkdirAll(override, 0o755); err != nil {
		t.Fatalf("mkdir override: %v", err)
	}

	t.Setenv("MEEPT_HOME", override)

	// MeeptHome: the single resolution point.
	if got := config.MeeptHome(); got != override {
		t.Fatalf("MeeptHome() = %q, want %q", got, override)
	}

	// The exact shipped default redirects to the override itself.
	if got := config.ExpandMeeptPath("~/.meept"); got != override {
		t.Fatalf("ExpandMeeptPath(~/.meept) = %q, want %q", got, override)
	}

	// A ~/.meept-prefixed config value lands under the override.
	got := config.ExpandMeeptPath("~/.meept/repomap_cache")
	want := filepath.Join(override, "repomap_cache")
	if got != want {
		t.Fatalf("ExpandMeeptPath(~/.meept/repomap_cache) = %q, want %q", got, want)
	}

	// A deeper prefix follows the same rule.
	got = config.ExpandMeeptPath("~/.meept/models/registry.json5")
	want = filepath.Join(override, "models", "registry.json5")
	if got != want {
		t.Fatalf("ExpandMeeptPath(~/.meept/models/registry.json5) = %q, want %q", got, want)
	}

	// MeeptPath joins onto the override.
	if got := config.MeeptPath("skills"); got != filepath.Join(override, "skills") {
		t.Fatalf("MeeptPath(skills) = %q, want under %q", got, override)
	}

	// Without MEEPT_HOME, ~/.meept stays HOME-relative (the legacy shape).
	t.Setenv("MEEPT_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no resolvable home dir: %v", err)
	}
	if got := config.ExpandMeeptPath("~/.meept/cache"); got != filepath.Join(home, ".meept", "cache") {
		t.Fatalf("ExpandMeeptPath without override = %q, want under $HOME/.meept", got)
	}

	// MEEPT_HOME itself may carry a tilde (expandTilde contract).
	t.Setenv("MEEPT_HOME", "~/.meept-e2e-tilde")
	if got := config.MeeptHome(); got != filepath.Join(home, ".meept-e2e-tilde") {
		t.Fatalf("MeeptHome with tilde override = %q, want %q", got, filepath.Join(home, ".meept-e2e-tilde"))
	}
}
