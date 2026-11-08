package config

import (
	"encoding/json"
	"testing"
)

// TestDelegationConfigDefaults pins the [delegation] defaults (DSH-5):
// disabled with no allowlist — model selection off, every route permitted.
func TestDelegationConfigDefaults(t *testing.T) {
	c := DefaultConfig()
	if c.Delegation.Enabled {
		t.Error("delegation must default to disabled (model selection off)")
	}
	if len(c.Delegation.AllowedRoutes) != 0 {
		t.Errorf("allowed_routes must default empty (allow all), got %d entries", len(c.Delegation.AllowedRoutes))
	}
}

// TestDelegationConfigParse pins that the [delegation] block round-trips
// through JSON5 (the meept.json5 loader path): enabled + allowed_routes as
// provider/model objects.
func TestDelegationConfigParse(t *testing.T) {
	raw := `{
		"delegation": {
			"enabled": true,
			"allowed_routes": [
				{"provider": "openai", "model": "gpt-5"},
				{"provider": "anthropic", "model": "claude-sonnet"}
			]
		}
	}`
	c := DefaultConfig()
	if err := json.Unmarshal([]byte(raw), c); err != nil {
		t.Fatalf("unmarshal delegation block: %v", err)
	}
	if !c.Delegation.Enabled {
		t.Error("delegation.enabled did not parse")
	}
	if len(c.Delegation.AllowedRoutes) != 2 {
		t.Fatalf("allowed_routes = %d entries, want 2", len(c.Delegation.AllowedRoutes))
	}
	if c.Delegation.AllowedRoutes[0].Provider != "openai" || c.Delegation.AllowedRoutes[0].Model != "gpt-5" {
		t.Errorf("route 0 = %+v", c.Delegation.AllowedRoutes[0])
	}
	if c.Delegation.AllowedRoutes[1].Provider != "anthropic" || c.Delegation.AllowedRoutes[1].Model != "claude-sonnet" {
		t.Errorf("route 1 = %+v", c.Delegation.AllowedRoutes[1])
	}
}

// TestEmployeesGoalAutoAdvanceDefault pins the DSH-3 knob default: absent
// means auto-advance ON (the leaf's zero-human-turns contract); only an
// explicit false restores the manual-only behavior.
func TestEmployeesGoalAutoAdvanceDefault(t *testing.T) {
	c := DefaultConfig()
	if !c.Employees.GoalAutoAdvanceEnabled() {
		t.Error("goal_auto_advance must default to enabled (leaf contract: zero human turns)")
	}

	// Explicit false.
	var off Config
	_ = json.Unmarshal([]byte(`{"employees":{"goal_auto_advance":false}}`), &off)
	if off.Employees.GoalAutoAdvanceEnabled() {
		t.Error("explicit goal_auto_advance=false must resolve disabled")
	}

	// Explicit true.
	var on Config
	_ = json.Unmarshal([]byte(`{"employees":{"goal_auto_advance":true}}`), &on)
	if !on.Employees.GoalAutoAdvanceEnabled() {
		t.Error("explicit goal_auto_advance=true must resolve enabled")
	}

	// Nil receiver is safe.
	var nilCfg *EmployeesConfig
	if !nilCfg.GoalAutoAdvanceEnabled() {
		t.Error("nil EmployeesConfig must default enabled")
	}
}
