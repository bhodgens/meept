package security

import (
	"testing"
)

// TestCheckPermission_ComputerUse covers the cua-driver MCP server risk
// rules. Tools register as "cua-driver.<action>" (server name prefix added by
// internal/tools/mcp client registration). Observation actions are LOW risk;
// every input-injection action is HIGH risk (confirmation-gated under the
// default require_confirmation_high config); unknown cua-driver actions fail
// closed at HIGH.
func TestCheckPermission_ComputerUse(t *testing.T) {
	pc := NewPermissionChecker(Config{
		RequireConfirmationHigh: true,
	})

	tests := []struct {
		name            string
		action          string
		wantAllowed     bool
		wantRisk        RiskLevel
		wantNeedConfirm bool
	}{
		// Observation class: LOW risk, runs without confirmation.
		{"capture", "cua-driver.capture", true, RiskLow, false},
		{"screenshot", "cua-driver.screenshot", true, RiskLow, false},
		{"list apps", "cua-driver.list_apps", true, RiskLow, false},
		{"list windows", "cua-driver.list_windows", true, RiskLow, false},
		{"get window state", "cua-driver.get_window_state", true, RiskLow, false},

		// Input-injection class: HIGH risk, confirmation-gated.
		{"click", "cua-driver.click", false, RiskHigh, true},
		{"type_text", "cua-driver.type_text", false, RiskHigh, true},
		{"hotkey", "cua-driver.hotkey", false, RiskHigh, true},
		{"key", "cua-driver.key", false, RiskHigh, true},
		{"scroll", "cua-driver.scroll", false, RiskHigh, true},
		{"drag", "cua-driver.drag", false, RiskHigh, true},
		{"move mouse", "cua-driver.move_mouse", false, RiskHigh, true},
		{"wait", "cua-driver.wait", false, RiskHigh, true},
		{"set_value", "cua-driver.set_value", false, RiskHigh, true},

		// Fail-closed: unknown cua-driver actions classify HIGH.
		{"unknown action", "cua-driver.hover_unicorn", false, RiskHigh, true},
		{"bare prefix", "cua-driver.", false, RiskHigh, true},

		// Non-matching tools are unaffected by the prefix rules.
		{"plain click is unknown action", "click", false, RiskSafe, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pc.CheckPermission(tt.action, map[string]string{})
			if result.Allowed != tt.wantAllowed {
				t.Errorf("CheckPermission(%q).Allowed = %v, want %v (reason: %s)",
					tt.action, result.Allowed, tt.wantAllowed, result.Reason)
			}
			if result.EffectiveRisk != tt.wantRisk {
				t.Errorf("CheckPermission(%q).EffectiveRisk = %v, want %v",
					tt.action, result.EffectiveRisk, tt.wantRisk)
			}
			if result.NeedsConfirm != tt.wantNeedConfirm {
				t.Errorf("CheckPermission(%q).NeedsConfirm = %v, want %v",
					tt.action, result.NeedsConfirm, tt.wantNeedConfirm)
			}
		})
	}
}

// TestCheckPermission_ComputerUseNoConfirmConfig verifies the confirmation
// gate honors require_confirmation_high: with the flag off, HIGH-risk
// computer-use actions are permitted (still reported at HIGH risk).
func TestCheckPermission_ComputerUseNoConfirmConfig(t *testing.T) {
	pc := NewPermissionChecker(Config{})

	result := pc.CheckPermission("cua-driver.click", map[string]string{})
	if !result.Allowed {
		t.Errorf("expected cua-driver.click allowed with RequireConfirmationHigh off, got denied: %s", result.Reason)
	}
	if result.EffectiveRisk != RiskHigh {
		t.Errorf("expected risk HIGH even when permitted, got %v", result.EffectiveRisk)
	}

	obs := pc.CheckPermission("cua-driver.capture", map[string]string{})
	if !obs.Allowed || obs.EffectiveRisk != RiskLow {
		t.Errorf("expected cua-driver.capture allowed at LOW, got allowed=%v risk=%v", obs.Allowed, obs.EffectiveRisk)
	}
}

// TestComputerUseRule exercises the classifier directly, including the
// not-a-cua-driver-tool case.
func TestComputerUseRule(t *testing.T) {
	tests := []struct {
		action string
		wantOK bool
	}{
		{"cua-driver.capture", true},
		{"cua-driver.click", true},
		{"github.create_issue", false},
		{"file_read", false},
		{"cua-driver", false},
		{"", false},
	}

	for _, tt := range tests {
		if _, ok := ComputerUseRule(tt.action); ok != tt.wantOK {
			t.Errorf("ComputerUseRule(%q) ok = %v, want %v", tt.action, ok, tt.wantOK)
		}
	}
}
