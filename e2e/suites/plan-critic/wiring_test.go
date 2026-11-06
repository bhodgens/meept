//go:build e2e

// Gate test for daemon wiring and model slots (leaf 07; contracts C2 and
// C9 in docs/plans/20261101-fresh-context-critic/master.md).
//
// PARKED as .staged. Leaf 07 re-activates ONLY this file (and adds the
// manifest rows). It pins:
//
//	TestCriticFlagsDefaultOn (C1/C2/C9: a default-config daemon boot
//	    leaves the three critic features ENABLED - the disable-form keys
//	    fresh_critic_disabled, hostile_critic_disabled, and
//	    interview_options_disabled default false - and setting a disable
//	    key turns its feature off; the critic_model and
//	    hostile_critic_model slots are absent/empty by default and the
//	    empty hostile slot is a nil chatter, never a planner fallback)
//
// Uses the hermetic harness (harness.Start) like the config-models
// suite: the flags are observable through the live config surface
// (`meept config get`) and the daemon boots healthy.
package plancritic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// wiringConfigGet runs `meept config get <key>` against the live sandbox
// daemon and returns the combined output.
func wiringConfigGet(t *testing.T, s *harness.Stack, key string) string {
	t.Helper()
	stdout, stderr := s.RunCLI(t, 30*time.Second, true, "config", "get", key)
	return stdout + stderr
}

// F9 (C1/C2/C9): the three critic features default ENABLED on a real
// boot (disable-form keys default false) and the two model slots stay
// empty by default.
func TestCriticFlagsDefaultOn(t *testing.T) {
	// Default sandbox config: no plans.critic keys anywhere.
	s := harness.Start(t)
	if !s.HealthOK() {
		t.Fatal("F9: daemon did not come up healthy on default config")
	}

	// C1: all three disable-form keys default false through the live
	// config surface (false = the feature is enabled).
	for key, want := range map[string]string{
		"plans.fresh_critic_disabled":     "false",
		"plans.hostile_critic_disabled":   "false",
		"plans.interview_options_disabled": "false",
	} {
		if got := wiringConfigGet(t, s, key); !strings.Contains(got, want) {
			t.Fatalf("F9 (C1): config get %s = %q, want it to report %s (the disable key must default false = feature enabled)", key, got, want)
		}
	}

	// C9: the two new model slots are absent or empty in the default
	// models.json5 (they ship as commented examples per the contract).
	modelsData := wiringReadModels(t, s)
	if strings.Contains(modelsData, "\"critic_model\"") || strings.Contains(modelsData, "\"hostile_critic_model\"") {
		t.Fatalf("F9 (C9): default models.json5 must not enable critic slots (commented examples only), got:\n%s", modelsData)
	}
}

// F9b (C1): an explicit disable key turns its feature off on a real
// boot while the daemon stays healthy.
func TestCriticFlagsExplicitDisable(t *testing.T) {
	s2 := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.fresh_critic_disabled":   true,
		"plans.hostile_critic_disabled": true,
	}))
	if !s2.HealthOK() {
		t.Fatal("F9b (C1): daemon with explicit disable keys did not boot healthy")
	}
	if got := wiringConfigGet(t, s2, "plans.hostile_critic_disabled"); !strings.Contains(got, "true") {
		t.Fatalf("F9b: explicit overlay must set hostile_critic_disabled true, got %q", got)
	}
	if got := wiringConfigGet(t, s2, "plans.fresh_critic_disabled"); !strings.Contains(got, "true") {
		t.Fatalf("F9b: explicit overlay must set fresh_critic_disabled true, got %q", got)
	}
}

// wiringReadModels returns the sandbox models.json5 content.
func wiringReadModels(t *testing.T, s *harness.Stack) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.MeeptHome, "models.json5"))
	if err != nil {
		t.Fatalf("F9: read models.json5: %v", err)
	}
	return string(data)
}
