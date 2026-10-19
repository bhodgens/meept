package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHermesConfig_DisabledByDefault pins Contract A defaults: hermes interop
// ships OFF; enabling it is an explicit user decision.
func TestHermesConfig_DisabledByDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Hermes.Enabled {
		t.Error("hermes should be disabled by default")
	}
	if cfg.Hermes.BaseURL != "http://127.0.0.1:8642" {
		t.Errorf("base_url = %q, want http://127.0.0.1:8642", cfg.Hermes.BaseURL)
	}
	if cfg.Hermes.APIKeyEnv != "HERMES_API_SERVER_KEY" {
		t.Errorf("api_key_env = %q, want HERMES_API_SERVER_KEY", cfg.Hermes.APIKeyEnv)
	}
	if cfg.Hermes.TimeoutSeconds != 300 {
		t.Errorf("timeout_seconds = %d, want 300", cfg.Hermes.TimeoutSeconds)
	}
	if cfg.Hermes.AutoApprove {
		t.Error("auto_approve should default to false (approve verb must ask the user first)")
	}
}

// TestHermesConfig_JSON5RoundTrip proves the config section loads from the
// JSON5 file format via LoadJSON5 (the loader the template test exercises),
// including defaults-fill semantics on a partial section.
func TestHermesConfig_JSON5RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	content := `{
  // hermes interop (JSON5: comments + trailing commas)
  "hermes": {
    "enabled": true,
    "base_url": "http://192.168.1.50:9000",
    "api_key_env": "MY_HERMES_KEY",
    "timeout_seconds": 30,
    "auto_approve": true,
  },
}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if err := LoadJSON5(path, cfg); err != nil {
		t.Fatalf("load json5: %v", err)
	}
	h := cfg.Hermes
	if !h.Enabled {
		t.Error("enabled not parsed")
	}
	if h.BaseURL != "http://192.168.1.50:9000" {
		t.Errorf("base_url = %q", h.BaseURL)
	}
	if h.APIKeyEnv != "MY_HERMES_KEY" {
		t.Errorf("api_key_env = %q", h.APIKeyEnv)
	}
	if h.TimeoutSeconds != 30 {
		t.Errorf("timeout_seconds = %d, want 30", h.TimeoutSeconds)
	}
	if !h.AutoApprove {
		t.Error("auto_approve = false, want true (round-trip)")
	}
}

// TestMeeptTemplate_HermesBlock keeps the shipped config/meept.json5 template
// honest: the [hermes] section must parse and must ship DISABLED (the API key
// never lives in the template — env indirection only).
func TestMeeptTemplate_HermesBlock(t *testing.T) {
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
	if c.Hermes.Enabled {
		t.Error("template ships hermes enabled; must ship disabled")
	}
	if c.Hermes.BaseURL == "" {
		t.Error("template base_url empty; expected the documented default endpoint")
	}
}
