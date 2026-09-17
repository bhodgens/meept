package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadModelsConfig_RefusalModel pins the daemon's models-config load
// path carrying the refusal_model slot end to end (refusal-fallback tree
// 02): struct-tag driven, so this should pass once the field is declared;
// it stays as a regression pin against an explicit field filter.
func TestLoadModelsConfig_RefusalModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")
	content := `{
  // global default refusal fallback (per-agent spec refusal_model overrides)
  "model": "main/primary",
  "refusal_model": "mlx-local/uncensored-8b",
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	cfg, err := LoadModelsConfig(path)
	if err != nil {
		t.Fatalf("LoadModelsConfig: %v", err)
	}
	if cfg.RefusalModel != "mlx-local/uncensored-8b" {
		t.Fatalf("RefusalModel = %q, want %q", cfg.RefusalModel, "mlx-local/uncensored-8b")
	}
}
