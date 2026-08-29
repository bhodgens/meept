package security

import (
	"testing"
)

// TestCheckPermission_ACPAgent covers the acp_agent meta-tool risk rules.
// The registered tool name is always "acp_agent"; the verb lives in
// details["verb"]. launch/send are HIGH (confirmation-gated under
// require_confirmation_high); read/stop are LOW; unknown verbs fail
// closed at HIGH. Other tool names are unaffected.
func TestCheckPermission_ACPAgent(t *testing.T) {
	pc := NewPermissionChecker(Config{
		RequireConfirmationHigh: true,
	})

	tests := []struct {
		name            string
		action          string
		details         map[string]string
		wantAllowed     bool
		wantRisk        RiskLevel
		wantNeedConfirm bool
	}{
		// HIGH class: spawn a process / inject content; confirmation-gated.
		{"launch", "acp_agent", map[string]string{"verb": "launch"}, false, RiskHigh, true},
		{"send", "acp_agent", map[string]string{"verb": "send"}, false, RiskHigh, true},

		// LOW class: observation / teardown; runs without confirmation.
		{"read", "acp_agent", map[string]string{"verb": "read"}, true, RiskLow, false},
		{"stop", "acp_agent", map[string]string{"verb": "stop"}, true, RiskLow, false},

		// Fail-closed: unknown or missing verb classifies HIGH.
		{"garbage verb", "acp_agent", map[string]string{"verb": "explode"}, false, RiskHigh, true},
		{"empty verb", "acp_agent", map[string]string{"verb": ""}, false, RiskHigh, true},
		{"missing verb", "acp_agent", map[string]string{}, false, RiskHigh, true},

		// Non-matching tools are unaffected by the acp_agent rule.
		{"browser_navigate unaffected", "browser_navigate", map[string]string{"verb": "launch"}, false, RiskSafe, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pc.CheckPermission(tt.action, tt.details)
			if result.Allowed != tt.wantAllowed {
				t.Errorf("CheckPermission(%q, %v).Allowed = %v, want %v (reason: %s)",
					tt.action, tt.details, result.Allowed, tt.wantAllowed, result.Reason)
			}
			if result.EffectiveRisk != tt.wantRisk {
				t.Errorf("CheckPermission(%q, %v).EffectiveRisk = %v, want %v",
					tt.action, tt.details, result.EffectiveRisk, tt.wantRisk)
			}
			if result.NeedsConfirm != tt.wantNeedConfirm {
				t.Errorf("CheckPermission(%q, %v).NeedsConfirm = %v, want %v",
					tt.action, tt.details, result.NeedsConfirm, tt.wantNeedConfirm)
			}
		})
	}
}

// TestCheckPermission_ACPAgentNoConfirmConfig verifies the confirmation
// gate honors require_confirmation_high: with the flag off, HIGH-risk
// acp_agent actions are permitted (still reported at HIGH risk).
func TestCheckPermission_ACPAgentNoConfirmConfig(t *testing.T) {
	pc := NewPermissionChecker(Config{})

	result := pc.CheckPermission("acp_agent", map[string]string{"verb": "launch"})
	if !result.Allowed {
		t.Errorf("expected acp_agent launch allowed with RequireConfirmationHigh off, got denied: %s", result.Reason)
	}
	if result.EffectiveRisk != RiskHigh {
		t.Errorf("expected risk HIGH even when permitted, got %v", result.EffectiveRisk)
	}

	obs := pc.CheckPermission("acp_agent", map[string]string{"verb": "read"})
	if !obs.Allowed || obs.EffectiveRisk != RiskLow {
		t.Errorf("expected acp_agent read allowed at LOW, got allowed=%v risk=%v", obs.Allowed, obs.EffectiveRisk)
	}
}

// TestACPAgentRule exercises the classifier directly, including the
// not-an-acp-agent-tool case.
func TestACPAgentRule(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		details map[string]string
		want    RiskLevel
		wantOK  bool
	}{
		{"launch", "acp_agent", map[string]string{"verb": "launch"}, RiskHigh, true},
		{"send", "acp_agent", map[string]string{"verb": "send"}, RiskHigh, true},
		{"read", "acp_agent", map[string]string{"verb": "read"}, RiskLow, true},
		{"stop", "acp_agent", map[string]string{"verb": "stop"}, RiskLow, true},
		{"garbage verb", "acp_agent", map[string]string{"verb": "explode"}, RiskHigh, true},
		{"missing verb", "acp_agent", nil, RiskHigh, true},
		{"browser_navigate", "browser_navigate", map[string]string{"verb": "launch"}, RiskSafe, false},
		{"file_read", "file_read", nil, RiskSafe, false},
		{"empty action", "", nil, RiskSafe, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ACPAgentRule(tt.action, tt.details)
			if ok != tt.wantOK {
				t.Errorf("ACPAgentRule(%q, %v) ok = %v, want %v", tt.action, tt.details, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("ACPAgentRule(%q, %v) risk = %v, want %v", tt.action, tt.details, got, tt.want)
			}
		})
	}
}
