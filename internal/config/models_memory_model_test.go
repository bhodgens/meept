package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadModelsConfig_MemoryModel pins the daemon's models-config load
// path carrying the memory_model slot end to end (struct-tag driven, same
// pattern as the refusal_model / extract_model slots): the field parses
// from models.json5 and defaults to empty when absent.
func TestLoadModelsConfig_MemoryModel(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "models.json5")
		content := `{
  // dedicated model for ambient epistemic extraction + distill summarization
  "model": "main/primary",
  "memory_model": "zai/glm-4.5-air",
}`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write models.json5: %v", err)
		}
		cfg, err := LoadModelsConfig(path)
		if err != nil {
			t.Fatalf("LoadModelsConfig: %v", err)
		}
		if cfg.MemoryModel != "zai/glm-4.5-air" {
			t.Fatalf("MemoryModel = %q, want %q", cfg.MemoryModel, "zai/glm-4.5-air")
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
		if cfg.MemoryModel != "" {
			t.Fatalf("MemoryModel = %q, want empty (slot absent must not change selection)", cfg.MemoryModel)
		}
	})
}
