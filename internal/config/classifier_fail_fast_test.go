package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig_ClassifierFailFast verifies the frozen default:
// classifier_fail_fast defaults to false — production keeps rotation; the
// flag exists to make classifier failures honest during testing/iteration.
func TestDefaultConfig_ClassifierFailFast(t *testing.T) {
	c := DefaultConfig()
	if c.Orchestrator.ClassifierFailFast {
		t.Fatal("classifier_fail_fast must default to false")
	}
}

// TestLoadJSON5_ClassifierFailFastRoundTrip verifies the json5 key
// "classifier_fail_fast" parses into OrchestratorConfig.ClassifierFailFast
// on the real Config type (daemon -c path).
func TestLoadJSON5_ClassifierFailFastRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "orchestrator": {
    "classifier_fail_fast": true,
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		t.Fatalf("LoadJSON5: %v", err)
	}
	if !cfg.Orchestrator.ClassifierFailFast {
		t.Fatal("classifier_fail_fast=true lost in json5 round-trip")
	}
}
