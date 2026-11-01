package config

// decision_log_config_test.go — T3 gate for agent-groups branch-01 leaf 02
// (docs/plans/20261030-agent-groups/01-permissions/02-decision-audit.md):
// security.groups.decision_log_level normalizes at the load boundary.
// Valid values pass through; empty and invalid fall back to "deny-warn"
// so the executor seam never sees an undefined level.

import "testing"

// TestNormalizeDecisionLogDefaults covers the three-way contract: valid
// values survive untouched, empty falls to the default, invalid falls to
// the default.
func TestNormalizeDecisionLogDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty falls to default", "", "deny-warn"},
		{"deny-warn passes through", "deny-warn", "deny-warn"},
		{"all passes through", "all", "all"},
		{"off passes through", "off", "off"},
		{"invalid falls to default", "sometimes", "deny-warn"},
		{"case-sensitive", "ALL", "deny-warn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := SecurityGroupsConfig{DecisionLogLevel: tt.in}
			NormalizeDecisionLogDefaults(&g)
			if g.DecisionLogLevel != tt.want {
				t.Fatalf("DecisionLogLevel = %q, want %q", g.DecisionLogLevel, tt.want)
			}
		})
	}
}

// TestDefaultConfig_DecisionLogLevel pins the schema default: a freshly
// built config already carries "deny-warn" so consumers never see the
// zero value even before Load runs.
func TestDefaultConfig_DecisionLogLevel(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Security.Groups.DecisionLogLevel != "deny-warn" {
		t.Fatalf("default decision_log_level = %q, want \"deny-warn\"",
			cfg.Security.Groups.DecisionLogLevel)
	}
}
