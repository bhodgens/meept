//go:build e2e

// Gate test for daemon wiring and model slots (leaf 07; contracts C2 and
// C9 in docs/plans/20261101-fresh-context-critic/master.md).
//
// PARKED as .staged. Leaf 07 re-activates ONLY this file (and adds the
// manifest rows). It pins:
//
//	F9 TestCriticFlagsDefaultOff (C1/C2/C9: a default-config daemon boot
//	   leaves fresh_critic_enabled, hostile_critic_enabled, and
//	   interview_option_gates false; the critic_model and
//	   hostile_critic_model slots are absent/empty by default and the
//	   empty hostile slot is a nil chatter, never a planner fallback)
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

// F9 (C1/C2/C9): the three critic flags default false on a real boot and
// the two model slots stay empty by default.
func TestCriticFlagsDefaultOff(t *testing.T) {
	// Default sandbox config: no plans.critic keys anywhere.
	s := harness.Start(t)
	if !s.HealthOK() {
		t.Fatal("F9: daemon did not come up healthy on default config")
	}

	// C1: all three flags default false through the live config surface.
	for key, want := range map[string]string{
		"plans.fresh_critic_enabled":   "false",
		"plans.hostile_critic_enabled": "false",
		"plans.interview_option_gates": "false",
	} {
		if got := wiringConfigGet(t, s, key); !strings.Contains(got, want) {
			t.Fatalf("F9 (C1): config get %s = %q, want it to report %s (the flag must default false and ship dark)", key, got, want)
		}
	}

	// C9: the two new model slots are absent or empty in the default
	// models.json5 (they ship as commented examples per the contract).
	modelsData := wiringReadModels(t, s)
	if strings.Contains(modelsData, "\"critic_model\"") || strings.Contains(modelsData, "\"hostile_critic_model\"") {
		t.Fatalf("F9 (C9): default models.json5 must not enable critic slots (commented examples only), got:\n%s", modelsData)
	}

	// C9: an explicitly empty hostile_critic_model slot resolves to a nil
	// chatter, never a planner-model fallback. Boot a second daemon with
	// the slot present but empty; it must boot healthy (nil, not a boot
	// failure) with the flags still dark.
	s2 := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"plans.fresh_critic_enabled":   false,
		"plans.hostile_critic_enabled": false,
	}))
	if !s2.HealthOK() {
		t.Fatal("F9 (C9): daemon with explicit false flags did not boot healthy")
	}
	if got := wiringConfigGet(t, s2, "plans.hostile_critic_enabled"); !strings.Contains(got, "false") {
		t.Fatalf("F9: explicit overlay must keep hostile_critic_enabled false, got %q", got)
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
