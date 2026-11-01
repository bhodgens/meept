package main

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestDoctorReportsProvidersState pins the doctor tool-provider report
// surface (04-config.md T3 / F9): report-only lines naming each
// registered provider with its enabled state, unknown config names
// surfaced as warn, and no check that could flip the doctor exit status.
func TestDoctorReportsProvidersState(t *testing.T) {
	f := false
	tr := true
	cfg := &config.Config{}
	cfg.ToolProviders.Providers = map[string]config.ToolProviderEntry{
		"github":    {Enabled: &f},
		"scheduler": {Enabled: &tr},
		"nosuch":    {},
	}

	checks := providerChecksDoctor(cfg)
	joined := ""
	for _, c := range checks {
		if c.name != "tool-provider" {
			t.Errorf("unexpected check name %q", c.name)
		}
		if !c.ok {
			t.Errorf("provider check for detail %q must stay ok=true (report-only, exit 0 unchanged)", c.detail)
		}
		joined += c.detail + "\n"
	}

	for _, want := range []string{
		"github: disabled",
		"scheduler: enabled (config)",
		"nosuch: unknown",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("doctor provider report missing %q; got:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "enabled") || strings.Contains(joined, "hermes: disabled") {
		t.Errorf("default-true providers must report enabled; got:\n%s", joined)
	}

	// Nil config (load failed elsewhere) must not panic: all-registered
	// providers default to enabled.
	for _, c := range providerChecksDoctor(nil) {
		if !c.ok {
			t.Errorf("nil-config provider check not ok: %+v", c)
		}
	}
}

// TestProviderCheckNamesSortedDeterministic keeps the report order
// stable run-to-run (doctor output is user-facing).
func TestProviderCheckNamesSortedDeterministic(t *testing.T) {
	got := sortProviderCheckNames([]string{"zeta", "alpha", "mu"})
	want := "alpha,mu,zeta"
	if strings.Join(got, ",") != want {
		t.Errorf("sortProviderCheckNames = %v, want %v", got, want)
	}
}
