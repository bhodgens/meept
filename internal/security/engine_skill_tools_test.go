package security

import (
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestSkillToolRiskClassification covers the DB-backed engine's
// classification for the skill-authoring and media-ingest tools
// (skill-authoring-and-media-ingest leaf 05): transcript_fetch is
// observation-only LOW (runs without confirmation), while skills_create and
// skills_patch are HIGH self-modification (confirmation-gated under the
// default require_confirmation_high config). This exercises the full
// pipeline — seed insert, tool_rule lookup, needsConfirmation — not just
// the seed data.
func TestSkillToolRiskClassification(t *testing.T) {
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
		name            string
		action          string
		wantAllowed     bool
		wantRisk        RiskLevel
		wantNeedConfirm bool
	}{
		// Observation class: LOW risk, runs without confirmation.
		{"transcript_fetch", "transcript_fetch", true, RiskLow, false},

		// Self-modification class: HIGH risk, confirmation-gated.
		{"skills_create", "skills_create", false, RiskHigh, true},
		{"skills_patch", "skills_patch", false, RiskHigh, true},
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
