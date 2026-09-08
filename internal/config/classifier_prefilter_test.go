package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultConfig_ClassifierPrefilterDisabled verifies the frozen default:
// the Stage-0 prefilter is off until explicitly enabled — LLM classification
// behavior is unchanged until the user opts in.
func TestDefaultConfig_ClassifierPrefilterDisabled(t *testing.T) {
	c := DefaultConfig()
	if c.Orchestrator.Prefilter.Enabled {
		t.Fatal("classifier_prefilter.enabled must default to false")
	}
	if c.Orchestrator.Prefilter.AssertOnly {
		t.Error("assert_only must default to false")
	}
	// kNN neighbor floor (not a routing threshold): votes require k=5
	// unanimous neighbors above this floor.
	if c.Orchestrator.Prefilter.Threshold != 0.70 {
		t.Errorf("prefilter threshold default = %v, want 0.70", c.Orchestrator.Prefilter.Threshold)
	}
	if c.Orchestrator.Prefilter.TimeoutSeconds != 2 {
		t.Errorf("prefilter timeout default = %d, want 2", c.Orchestrator.Prefilter.TimeoutSeconds)
	}
}

// TestLoadJSON5_ClassifierPrefilterRoundTrip verifies the json5 keys parse
// into ClassifierPrefilterConfig on the real Config type (daemon -c path).
func TestLoadJSON5_ClassifierPrefilterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  "orchestrator": {
    "classifier_prefilter": {
      "enabled": true,
      "base_url": "http://127.0.0.1:8090/v1",
      "model": "qwen3-embedding",
      "dimension": 1024,
      "threshold": 0.88,
      "centroids_path": "/tmp/centroids.json",
      "timeout_seconds": 3,
    },
  },
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	if err := LoadJSON5(path, c); err != nil {
		t.Fatalf("LoadJSON5: %v", err)
	}
	pf := c.Orchestrator.Prefilter
	if !pf.Enabled {
		t.Error("enabled=true lost in json5 round-trip")
	}
	if pf.BaseURL != "http://127.0.0.1:8090/v1" {
		t.Errorf("base_url = %q", pf.BaseURL)
	}
	if pf.Model != "qwen3-embedding" {
		t.Errorf("model = %q", pf.Model)
	}
	if pf.Dimension != 1024 {
		t.Errorf("dimension = %d, want 1024", pf.Dimension)
	}
	if pf.Threshold != 0.88 {
		t.Errorf("threshold = %v, want 0.88", pf.Threshold)
	}
	if pf.CentroidsPath != "/tmp/centroids.json" {
		t.Errorf("centroids_path = %q", pf.CentroidsPath)
	}
	if pf.TimeoutSeconds != 3 {
		t.Errorf("timeout_seconds = %d, want 3", pf.TimeoutSeconds)
	}
}
