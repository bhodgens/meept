package main

// doctor_groups.go — the report-only groups permissions check for
// `meept doctor` (agent-groups plan tree leaf 06, task T3).
//
// User norm: doctor NEVER mutates/installs. This check:
//  1. validates the [security.groups] config structure (the SAME
//     groups.Check engine the daemon's groups.check RPC runs — one
//     engine, no drift);
//  2. counts agents whose dispatch-visible tools diverge from
//     Resolved.AllowedTools — structurally zero when the dispatch
//     intersection (leaf 04) is wired; a non-zero count is wiring drift.
//
// Limitation (noted per the leaf contract): the doctor process cannot
// reach the daemon's LIVE registry, so the divergence leg runs against an
// injected spec map (empty in the real CLI run -> the posture is reported
// with the live pointer to `meept groups check`); the engine call itself
// is identical.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

// groupsDoctorConfigHook is the config seam the doctor's groups check
// reads through (injected by tests; production loads the default config).
var groupsDoctorConfigHook = func() *config.Config {
	cfg, err := config.LoadDefault()
	if err != nil {
		return nil
	}
	return cfg
}

// checkConfiguredGroupsDoctor loads the config for the groups check via
// the seam (nil when no config file exists — the check degrades to the
// skip line).
func checkConfiguredGroupsDoctor() *config.Config {
	return groupsDoctorConfigHook()
}

// checkGroupsDoctorGroups is the runDoctor call-site adapter (keeps the
// long seam signature out of the wiring block).
func checkGroupsDoctorGroups(cfg *config.Config) []doctorCheck {
	return checkGroupsDoctor(cfg, nil, nil, nil)
}

func checkGroupsDoctor(cfg *config.Config, wiredSpecs map[string][]string, overOverrides groups.OverridesReader, discover func() []groups.DiagnosisAgent) []doctorCheck {
	if cfg == nil {
		return []doctorCheck{{
			name:   "groups-config",
			ok:     true,
			warn:   true,
			detail: "no config loaded; groups checks skipped",
		}}
	}
	g := cfg.Security.Groups

	// Empty tables: legacy posture, nothing to validate.
	if len(g.Policies) == 0 && len(g.Bindings) == 0 && !g.DefaultManaged {
		return []doctorCheck{{
			name:   "groups-config",
			ok:     true,
			detail: "no groups policies or bindings configured (legacy spec-allowlist posture)",
		}}
	}

	engAgents := doctorAgents(discover)
	res := groups.Check(doctorPolicies(g), doctorBindings(g), engAgents, nil, g.DefaultManaged, overOverrides, nil)

	var checks []doctorCheck

	// Structural findings -> one doctor line each (report-only).
	defectCount := 0
	for _, d := range res.Defects {
		switch d.Code {
		case groups.DefectActiveOverride, groups.DefectExpiredOverride, groups.DefectOverridesReader:
			continue // rendered by the overrides line below
		}
		defectCount++
		checks = append(checks, doctorCheck{
			name:   "groups-config",
			ok:     false,
			detail: fmt.Sprintf("%s: %s: %s", d.Code, d.Name, d.Detail),
		})
	}
	if defectCount == 0 {
		checks = append(checks, doctorCheck{
			name:   "groups-config",
			ok:     true,
			detail: fmt.Sprintf("groups config valid (%d policies, %d bindings, %d agents evaluated)", len(g.Policies), len(g.Bindings), res.AgentsChecked),
		})
	}

	// Dispatch-visible divergence (wiring drift): compare each probed
	// agent's dispatcher-visible set with the config-resolved
	// AllowedTools. The structural expectation is ZERO divergence when
	// leaf 04's filterTools intersection is wired. wiredSpecs==nil (the
	// CLI process cannot see the live registry) reports the posture
	// without a false alarm.
	diverged := 0
	var divergedNames []string
	probed := 0
	if wiredSpecs != nil {
		for _, a := range engAgents {
			if !a.Enabled {
				continue
			}
			wired, ok := wiredSpecs[a.ID]
			if !ok {
				continue
			}
			probed++
			expected := doctorAllowedTools(g, a)
			if !stringSlicesSetEqual(expected, wired) {
				diverged++
				divergedNames = append(divergedNames, a.ID)
			}
		}
	}
	switch {
	case wiredSpecs == nil:
		checks = append(checks, doctorCheck{
			name: "groups-permissions",
			ok:   true,
			detail: fmt.Sprintf(
				"dispatch divergence not probed from the CLI process (config posture: %d agents evaluated; live answer: meept groups check)",
				res.AgentsChecked),
		})
	case diverged == 0:
		checks = append(checks, doctorCheck{
			name:   "groups-permissions",
			ok:     true,
			detail: fmt.Sprintf("dispatch-visible tools match Resolved.AllowedTools for all %d probed agents", probed),
		})
	default:
		sort.Strings(divergedNames)
		checks = append(checks, doctorCheck{
			name:   "groups-permissions",
			ok:     false,
			detail: fmt.Sprintf("%d agent(s) diverge from Resolved.AllowedTools (wiring drift): %s", diverged, strings.Join(divergedNames, ", ")),
		})
	}

	// Overrides (leaf 07 seam) — report-only summary.
	checks = append(checks, doctorCheck{
		name: "groups-overrides",
		ok:   true,
		warn: res.Overrides.Expired > 0,
		detail: func() string {
			if res.Overrides.Note != "" && res.Overrides.Total == 0 {
				return "overrides: none counted (" + res.Overrides.Note + ")"
			}
			return fmt.Sprintf("overrides: %d active, %d expired", res.Overrides.Active, res.Overrides.Expired)
		}(),
	})

	return checks
}

// doctorAllowedTools resolves ONE agent's effective allow set through the
// shared groups.Explain engine (the same resolution the gates enforce —
// never a parallel implementation).
func doctorAllowedTools(g config.SecurityGroupsConfig, a groups.DiagnosisAgent) []string {
	res := groups.Explain(
		a.Tools, a.ID, a.Groups,
		doctorPolicies(g), doctorBindings(g),
		&groups.ResolveOptions{},
		g.DefaultManaged,
	)
	return res.AllowedTools
}

// doctorAgents runs the roster seam (nil = real discovery tiers).
func doctorAgents(discover func() []groups.DiagnosisAgent) []groups.DiagnosisAgent {
	if discover != nil {
		return discover()
	}
	disc := agents.NewDiscovery(agents.WithBundledPath("config/agents"))
	defs, err := disc.Discover()
	if err != nil || defs == nil {
		return nil
	}
	out := make([]groups.DiagnosisAgent, 0, len(defs))
	for _, def := range defs {
		if def == nil {
			continue
		}
		out = append(out, groups.DiagnosisAgent{
			ID:      def.ID,
			Groups:  def.Groups,
			Tools:   def.AdditionalTools,
			Enabled: def.IsEnabled(),
		})
	}
	return out
}

// stringSlicesSetEqual compares two string slices as sets.
func stringSlicesSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	bset := make(map[string]struct{}, len(b))
	for _, s := range b {
		bset[s] = struct{}{}
	}
	for _, s := range a {
		if _, ok := bset[s]; !ok {
			return false
		}
	}
	return true
}

// doctorPolicies maps the config policy table into engine rows.
func doctorPolicies(g config.SecurityGroupsConfig) []groups.Policy {
	out := make([]groups.Policy, 0, len(g.Policies))
	for _, p := range g.Policies {
		out = append(out, groups.Policy{
			Name:           p.Name,
			ToolsAllowed:   p.ToolsAllowed,
			ToolsDenied:    p.ToolsDenied,
			MemoryMaxScope: p.MemoryMaxScope,
			FSRoots:        p.FSRoots,
			NetworkClass:   p.NetworkClass,
			AllowedModels:  p.AllowedModels,
		})
	}
	return out
}

// doctorBindings maps the config binding table into engine rows.
func doctorBindings(g config.SecurityGroupsConfig) []groups.Binding {
	out := make([]groups.Binding, 0, len(g.Bindings))
	for _, b := range g.Bindings {
		out = append(out, groups.Binding{Subject: b.Subject, Policy: b.Policy, Scope: b.Scope})
	}
	return out
}
