package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadModelsConfig_PlannerModel pins the daemon's models-config load
// path carrying the planner_model slot end to end (struct-tag driven, same
// pattern as the memory_model / extract_model slots): the field parses
// from models.json5 and defaults to empty when absent.
func TestLoadModelsConfig_PlannerModel(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "models.json5")
		content := `{
  // dedicated model for the strategic planner
  "model": "main/primary",
  "planner_model": "zai/glm-5.2",
}`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write models.json5: %v", err)
		}
		cfg, err := LoadModelsConfig(path)
		if err != nil {
			t.Fatalf("LoadModelsConfig: %v", err)
		}
		if cfg.PlannerModel != "zai/glm-5.2" {
			t.Fatalf("PlannerModel = %q, want %q", cfg.PlannerModel, "zai/glm-5.2")
		}
	})

	t.Run("empty_default", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "models.json5")
		content := `{"model": "main/primary"}`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write models.json5: %v", err)
		}
		cfg, err := LoadModelsConfig(path)
		if err != nil {
			t.Fatalf("LoadModelsConfig: %v", err)
		}
		if cfg.PlannerModel != "" {
			t.Fatalf("PlannerModel = %q, want empty (slot absent must not change selection)", cfg.PlannerModel)
		}
	})
}
