package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig_ClassifierBootFailFast verifies the F-D8 default: a local
// classifier runtime that never becomes healthy at boot is a platform
// failure — the daemon must log and exit by default (classifier_boot_fail_fast
// defaults to true).
func TestDefaultConfig_ClassifierBootFailFast(t *testing.T) {
	c := DefaultConfig()
	if !c.Orchestrator.ClassifierBootFailFast {
		t.Fatal("classifier_boot_fail_fast must default to true")
	}
}

// TestLoadJSON5_ClassifierBootFailFastRoundTrip verifies the json5 key
// "classifier_boot_fail_fast" parses into
// OrchestratorConfig.ClassifierBootFailFast on the real Config type
// (daemon -c path), including the false escape hatch.
func TestLoadJSON5_ClassifierBootFailFastRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "orchestrator": {
    "classifier_boot_fail_fast": false,
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		t.Fatalf("LoadJSON5: %v", err)
	}
	if cfg.Orchestrator.ClassifierBootFailFast {
		t.Fatal("classifier_boot_fail_fast=false lost in json5 round-trip")
	}
}

// TestLoadTOML_ClassifierBootFailFastRoundTrip verifies the toml key.
func TestLoadTOML_ClassifierBootFailFastRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := `
[orchestrator]
classifier_boot_fail_fast = false
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := DefaultConfig()
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	*cfg = *loaded
	if cfg.Orchestrator.ClassifierBootFailFast {
		t.Fatal("classifier_boot_fail_fast=false lost in toml round-trip")
	}
}
