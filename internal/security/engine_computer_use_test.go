package security

import (
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestEngineCheckComputerUse covers the DB-backed engine's cua-driver rules.
// MCP tools register as "cua-driver.<action>"; observation actions are LOW,
// input-injection actions are HIGH (confirmation-gated under the default
// require_confirmation_high config), and unknown cua-driver actions fail
// closed at HIGH instead of falling through to the MEDIUM default.
func TestEngineCheckComputerUse(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	tests := []struct {
		name          string
		action        string
		wantAllowed   bool
		wantRisk      RiskLevel
		wantNeedConfirm bool
	}{
		// Observation class: LOW risk, runs without confirmation.
		{"capture", "cua-driver.capture", true, RiskLow, false},
		{"screenshot", "cua-driver.screenshot", true, RiskLow, false},
		{"list apps", "cua-driver.list_apps", true, RiskLow, false},
		{"get window state", "cua-driver.get_window_state", true, RiskLow, false},

		// Input-injection class: HIGH risk, confirmation-gated.
		{"click", "cua-driver.click", false, RiskHigh, true},
		{"type_text", "cua-driver.type_text", false, RiskHigh, true},
		{"hotkey", "cua-driver.hotkey", false, RiskHigh, true},
		{"scroll", "cua-driver.scroll", false, RiskHigh, true},
		{"drag", "cua-driver.drag", false, RiskHigh, true},
		{"set_value", "cua-driver.set_value", false, RiskHigh, true},

		// Fail-closed: unknown cua-driver actions classify HIGH.
		{"unknown action", "cua-driver.hover_unicorn", false, RiskHigh, true},

		// Non-matching tools keep the existing MEDIUM fallback.
		{"non-cua tool", "github.create_issue", true, RiskMedium, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := engine.Check(tt.action, tt.action, nil, "")
			if d.Allowed != tt.wantAllowed {
				t.Errorf("Check(%q).Allowed = %v, want %v (reason: %s)",
					tt.action, d.Allowed, tt.wantAllowed, d.Reason)
			}
			if d.RiskLevel != tt.wantRisk {
				t.Errorf("Check(%q).RiskLevel = %v, want %v",
					tt.action, d.RiskLevel, tt.wantRisk)
			}
			if d.RequiresConfirmation != tt.wantNeedConfirm {
				t.Errorf("Check(%q).RequiresConfirmation = %v, want %v",
					tt.action, d.RequiresConfirmation, tt.wantNeedConfirm)
			}
		})
	}
}

// TestLookupBaseRule_ComputerUse verifies the prefix classifier is consulted
// by lookupBaseRule before the MEDIUM fallback. The second return is the
// rule-level requires_confirmation flag, which stays false for computer-use
// rules: confirmation gating happens in Stage 5 via needsConfirmation.
func TestLookupBaseRule_ComputerUse(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	engine, err := NewEngine(dbPath, &config.SecurityConfig{}, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	risk, confirm := engine.lookupBaseRule("cua-driver.click", "cua-driver.click")
	if risk != RiskHigh {
		t.Errorf("cua-driver.click risk = %v, want HIGH", risk)
	}
	if confirm {
		t.Error("cua-driver.click rule-level confirm should be false (Stage 5 gates instead)")
	}

	risk, confirm = engine.lookupBaseRule("cua-driver.capture", "cua-driver.capture")
	if risk != RiskLow {
		t.Errorf("cua-driver.capture risk = %v, want LOW", risk)
	}
	if confirm {
		t.Error("cua-driver.capture rule-level confirm should be false")
	}

	risk, _ = engine.lookupBaseRule("cua-driver.mystery", "cua-driver.mystery")
	if risk != RiskHigh {
		t.Errorf("unknown cua-driver action: risk=%v, want HIGH fail-closed (not the MEDIUM fallback)", risk)
	}

	ruleRisk, _ := engine.lookupBaseRule("file_read", "file_read")
	if ruleRisk != RiskSafe {
		t.Errorf("file_read base rule should still resolve via seeded table, got %v", ruleRisk)
	}
}
