package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// A rig launched with MEEPT_HOME must read models.json5 from that home.
// Regression: loadModelsConfigWithPath used os.UserHomeDir() directly, so an
// isolated rig silently read the operator's real ~/.meept/models.json5
// (found 2026-09-12 while standing up the researcher-path rig — the daemon
// honored MEEPT_HOME for certs, sockets and logs but not for the model
// config, so the rig resolved a cloud provider instead of the local model).
func TestLoadModelsConfigWithPath_HonorsMeeptHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)

	models := `{
		"model": "local/rig-model",
		"providers": {
			"local": {
				"api": "openai",
				"options": {"baseURL": "http://127.0.0.1:9/v1"},
				"models": {
					"rig-model": {"capabilities": ["completion"], "context_limit": 4096}
				}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(home, "models.json5"), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, path, err := loadModelsConfigWithPath(slog.Default())
	if err != nil {
		t.Fatalf("loadModelsConfigWithPath: %v", err)
	}
	if cfg.Model != "local/rig-model" {
		t.Errorf("resolved model = %q, want local/rig-model from MEEPT_HOME", cfg.Model)
	}
	if path != filepath.Join(home, "models.json5") {
		t.Errorf("config path = %q, want the MEEPT_HOME path", path)
	}
}
