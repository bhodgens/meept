package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserConfig_DisabledByDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Browser.Enabled {
		t.Error("browser should be disabled by default")
	}
	if !cfg.Browser.HeadlessEnabled() {
		t.Error("headless should default to true")
	}
}

func TestBrowserConfig_TOMLLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.toml")
	content := `
[browser]
enabled = true
chrome_path = "/usr/local/bin/chromium"
headless = false
max_pages = 5
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	b := cfg.Browser
	if !b.Enabled {
		t.Error("enabled not parsed")
	}
	if b.ChromePath != "/usr/local/bin/chromium" {
		t.Errorf("chrome_path = %q", b.ChromePath)
	}
	if b.HeadlessEnabled() {
		t.Error("headless=false should parse as false")
	}
	if b.MaxPages != 5 {
		t.Errorf("max_pages = %d, want 5", b.MaxPages)
	}
}
