package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMeeptTemplate_ClassifierPrefilterBlock keeps the shipped
// config/meept.json5 template honest: the documented prefilter block must
// parse into ClassifierPrefilterConfig with disabled-by-default semantics.
func TestMeeptTemplate_ClassifierPrefilterBlock(t *testing.T) {
	// Locate the repo template relative to this package.
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

	pf := c.Orchestrator.Prefilter
	if pf.Enabled {
		t.Error("template ships prefilter enabled; must ship disabled")
	}
	if pf.BaseURL == "" {
		t.Error("template base_url empty; expected the documented default endpoint")
	}
	if pf.Model != "qwen3-embedding" {
		t.Errorf("template model = %q", pf.Model)
	}
	if pf.Dimension != 1024 {
		t.Errorf("template dimension = %d, want 1024", pf.Dimension)
	}
	if pf.Threshold != 0.90 {
		t.Errorf("template threshold = %v, want 0.90", pf.Threshold)
	}
}
