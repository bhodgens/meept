package main

// doctor_groups_test.go — leaf 06 T3 gate: the doctor groups check is
// report-only, surfaces structural findings with names, and detects
// dispatch-visible divergence through the injected fake.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

func doctorFixtureConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.Groups.Policies = []config.GroupsPolicyConfig{{
		Name:         "coders",
		ToolsAllowed: []string{"file_read", "shell"},
	}}
	cfg.Security.Groups.Bindings = []config.GroupsBindingConfig{{
		Subject: "group:coders",
		Policy:  "coders",
		Scope:   "daemon",
	}}
	return cfg
}

func doctorFixtureRoster() []groups.DiagnosisAgent {
	return []groups.DiagnosisAgent{{
		ID:      "aastub",
		Groups:  []string{"coders"},
		Tools:   []string{"file_read", "shell"},
		Enabled: true,
	}}
}

// clean config + matching dispatch-visible tools: both lines green.
func TestDoctorGroupsCheckCleanAndMatching(t *testing.T) {
	checks := checkGroupsDoctor(doctorFixtureConfig(),
		map[string][]string{"aastub": {"file_read", "shell"}},
		nil,
		doctorFixtureRoster,
	)
	sawConfig, sawPerms := false, false
	for _, c := range checks {
		if c.name == "groups-config" && c.ok {
			sawConfig = true
		}
		if c.name == "groups-permissions" && c.ok && strings.Contains(c.detail, "match Resolved.AllowedTools") {
			sawPerms = true
		}
		if !c.ok && c.name == "groups-overrides" {
			t.Fatalf("overrides summary must not be a failure: %+v", c)
		}
	}
	if !sawConfig || !sawPerms {
		t.Fatalf("doctor checks: config=%v perms=%v; checks: %+v", sawConfig, sawPerms, checks)
	}
}

// dispatch-visible tools DIVERGE from Resolved.AllowedTools: named wiring
// drift (the divergence detection requirement, injected fake).
func TestDoctorGroupsCheckDivergenceDetected(t *testing.T) {
	checks := checkGroupsDoctor(doctorFixtureConfig(),
		// The dispatcher shows web_fetch which the config does NOT allow.
		map[string][]string{"aastub": {"file_read", "shell", "web_fetch"}},
		nil,
		doctorFixtureRoster,
	)
	found := false
	for _, c := range checks {
		if c.name == "groups-permissions" && !c.ok && strings.Contains(c.detail, "aastub") {
			found = true
		}
	}
	if !found {
		t.Fatalf("doctor: divergence not reported; checks: %+v", checks)
	}
}

// structural defect surfaces as a named non-ok line.
func TestDoctorGroupsCheckStructuralDefectNamed(t *testing.T) {
	cfg := doctorFixtureConfig()
	cfg.Security.Groups.Bindings = append(cfg.Security.Groups.Bindings,
		config.GroupsBindingConfig{Subject: "group:coders", Policy: "missing_policy"})
	checks := checkGroupsDoctor(cfg, nil, nil, doctorFixtureRoster)
	found := false
	for _, c := range checks {
		if c.name == "groups-config" && !c.ok && strings.Contains(c.detail, groups.DefectDanglingPolicy) &&
			strings.Contains(c.detail, "missing_policy") {
			found = true
		}
	}
	if !found {
		t.Fatalf("doctor: dangling policy not named; checks: %+v", checks)
	}
}

// nil config degrades to the skip line (never a failure, never a prompt).
func TestDoctorGroupsCheckNilConfig(t *testing.T) {
	checks := checkGroupsDoctor(nil, nil, nil, nil)
	if len(checks) != 1 || checks[0].name != "groups-config" || !checks[0].ok {
		t.Fatalf("doctor nil config: %+v", checks)
	}
}

// empty tables: the legacy posture line.
func TestDoctorGroupsCheckEmptyTables(t *testing.T) {
	checks := checkGroupsDoctor(&config.Config{}, nil, nil, nil)
	if len(checks) != 1 || !checks[0].ok || !strings.Contains(checks[0].detail, "legacy spec-allowlist") {
		t.Fatalf("doctor empty tables: %+v", checks)
	}
}

// CLI posture (wiredSpecs nil): posture line, not a false alarm.
func TestDoctorGroupsCheckPostureWithoutWiring(t *testing.T) {
	checks := checkGroupsDoctor(doctorFixtureConfig(), nil, nil, doctorFixtureRoster)
	found := false
	for _, c := range checks {
		if c.name == "groups-permissions" && c.ok && strings.Contains(c.detail, "not probed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("doctor: posture line missing; checks: %+v", checks)
	}
}

// expired overrides surface through the reader seam as a warning line.
func TestDoctorGroupsCheckOverridesSummary(t *testing.T) {
	over := doctorFakeOverrides{expired: []groups.OverrideRow{
		{Subject: "agent:aastub", Tool: "web_fetch", ExpiresAt: "2026-01-01T00:00:00Z"},
	}}
	checks := checkGroupsDoctor(doctorFixtureConfig(), nil, over, doctorFixtureRoster)
	found := false
	for _, c := range checks {
		if c.name == "groups-overrides" && c.warn && strings.Contains(c.detail, "1 expired") {
			found = true
		}
	}
	if !found {
		t.Fatalf("doctor: expired override not summarized; checks: %+v", checks)
	}
}

// doctorFakeOverrides is the leaf-07 seam double.
type doctorFakeOverrides struct {
	active  []groups.OverrideRow
	expired []groups.OverrideRow
}

func (f doctorFakeOverrides) ActiveOverrides(_ context.Context) ([]groups.OverrideRow, error) {
	return f.active, nil
}

func (f doctorFakeOverrides) ExpiredOverrides(_ context.Context) ([]groups.OverrideRow, error) {
	return f.expired, nil
}

// wiring shape: the runDoctor adapter passes nil specs + nil reader.
func TestDoctorGroupsWiringAdapter(t *testing.T) {
	orig := groupsDoctorConfigHook
	groupsDoctorConfigHook = func() *config.Config { return doctorFixtureConfig() }
	t.Cleanup(func() { groupsDoctorConfigHook = orig })
	checks := checkGroupsDoctorGroups(checkConfiguredGroupsDoctor())
	if len(checks) == 0 {
		t.Fatal("adapter produced no checks")
	}
}
