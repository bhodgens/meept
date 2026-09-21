package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig_ClassifierSessionStateUpgrade verifies the frozen default:
// the one-way quickplan session-state upgrade is off until explicitly enabled
// — dispatch behavior is byte-identical to legacy until the user opts in
// (docs/plans/quickplan-session-upgrade/, contract C3).
func TestDefaultConfig_ClassifierSessionStateUpgrade(t *testing.T) {
	c := DefaultConfig()
	if c.Orchestrator.Classifier.SessionStateUpgrade {
		t.Fatal("classifier.session_state_upgrade must default to false")
	}
}

// TestLoadJSON5_ClassifierSessionStateUpgradeRoundTrip verifies the json5
// key parses into ClassifierConfig at the correct nesting (a key at the
// wrong level parses silently and invisible defaults apply).
func TestLoadJSON5_ClassifierSessionStateUpgradeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "orchestrator": {
    "classifier": {
      "session_state_upgrade": true
    }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	if err := LoadJSON5(path, c); err != nil {
		t.Fatalf("config failed to parse: %v", err)
	}
	if !c.Orchestrator.Classifier.SessionStateUpgrade {
		t.Fatal("session_state_upgrade=true did not parse at orchestrator.classifier nesting")
	}
}

// TestMeeptTemplate_ClassifierSessionStateUpgradeBlock keeps the shipped
// config/meept.json5 template honest: the documented classifier block must
// parse and ship disabled by default.
func TestMeeptTemplate_ClassifierSessionStateUpgradeBlock(t *testing.T) {
	const templatePath = "../../config/meept.json5"
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("template not found from test cwd: %v", err)
	}
	abs, err := filepath.Abs(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	if err := LoadJSON5(abs, c); err != nil {
		t.Fatalf("template meept.json5 failed to parse: %v", err)
	}
	if c.Orchestrator.Classifier.SessionStateUpgrade {
		t.Error("template ships session_state_upgrade enabled; must ship disabled")
	}
}
