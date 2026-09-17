package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestProvidersConfig_RefusalModelOverlay pins the refusal_model slot's
// overlay semantics (refusal-fallback tree 02): a non-empty overlay
// (user models.json5) RefusalModel wins; an empty overlay RefusalModel
// leaves the bundled base value intact. Mirrors the ExtractModel merge
// block inside MergeProvidersConfig.
func TestProvidersConfig_RefusalModelOverlay(t *testing.T) {
	baseJSON := `{"model": "a/b", "refusal_model": "bundled/refusal"}`
	overlayJSON := `{"refusal_model": "ollama/dolphin-mixtral"}`
	var base, overlay ProvidersConfig
	if err := json.Unmarshal([]byte(baseJSON), &base); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(overlayJSON), &overlay); err != nil {
		t.Fatal(err)
	}

	merged := MergeProvidersConfig(&base, &overlay)
	if merged.RefusalModel != "ollama/dolphin-mixtral" {
		t.Fatalf("overlay did not apply: %q", merged.RefusalModel)
	}

	// Empty overlay value keeps the base (feature stays off unless set).
	empty := &ProvidersConfig{}
	kept := MergeProvidersConfig(&base, empty)
	if kept.RefusalModel != "bundled/refusal" {
		t.Fatalf("empty overlay clobbered base: %q", kept.RefusalModel)
	}

	// Both empty = off.
	if got := MergeProvidersConfig(&ProvidersConfig{}, &ProvidersConfig{}).RefusalModel; got != "" {
		t.Fatalf("both-empty merge produced %q, want empty (off)", got)
	}
}

// TestLoadProvidersConfig_RefusalModel pins the JSON5 load path carrying
// the refusal_model slot end to end into ProvidersConfig (struct-tag
// driven; regression guard against an explicit field filter).
func TestLoadProvidersConfig_RefusalModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json5")
	content := `{
  // refusal fallback target (global default; per-agent spec overrides)
  "model": "main/primary",
  "refusal_model": "mlx-local/uncensored-8b",
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write models.json5: %v", err)
	}
	cfg, err := LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	if cfg.RefusalModel != "mlx-local/uncensored-8b" {
		t.Fatalf("RefusalModel = %q, want %q", cfg.RefusalModel, "mlx-local/uncensored-8b")
	}
}
