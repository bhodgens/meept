package skills

import (
	"os"
	"testing"
)

func TestWebBrowsingSkillParses(t *testing.T) {
	if _, err := os.Stat("../../config/skills/web-browsing/SKILL.md"); err != nil {
		t.Skip("skill file not present")
	}
	entry, err := ParseSkillMetadataOnly("../../config/skills/web-browsing/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillMetadataOnly: %v", err)
	}
	if entry.Name != "web-browsing" {
		t.Errorf("name = %q", entry.Name)
	}
	if entry.Description == "" {
		t.Error("description empty")
	}
	if entry.RiskLevel != "low" {
		t.Errorf("risk_level = %q, want low", entry.RiskLevel)
	}
	if len(entry.Examples) == 0 {
		t.Error("examples should be non-empty for trigger matching")
	}
	skill, err := ParseSkillFile("../../config/skills/web-browsing/SKILL.md")
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	if len(skill.Body) == 0 {
		t.Error("body empty")
	}
}
