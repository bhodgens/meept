package lite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig() returned nil")
	}

	// Verify defaults
	if !cfg.ShowTokens {
		t.Error("ShowTokens should default to true")
	}
	if !cfg.ShowCost {
		t.Error("ShowCost should default to true")
	}
	if !cfg.ShowDuration {
		t.Error("ShowDuration should default to true")
	}
	if !cfg.AutoScroll {
		t.Error("AutoScroll should default to true")
	}
	if cfg.HistorySize != 100 {
		t.Errorf("HistorySize should default to 100, got %d", cfg.HistorySize)
	}

	// Verify keybindings exist
	if cfg.Keybindings == nil {
		t.Fatal("Keybindings should not be nil")
	}
	if cfg.Keybindings["menu"] != "ctrl+x" {
		t.Errorf("menu keybinding should be ctrl+x, got %s", cfg.Keybindings["menu"])
	}
	if cfg.Keybindings["quit"] != "ctrl+c" {
		t.Errorf("quit keybinding should be ctrl+c, got %s", cfg.Keybindings["quit"])
	}
}

func TestConfigPath(t *testing.T) {
	path := ConfigPath()
	if path == "" {
		t.Skip("Could not determine home directory")
	}

	// Should end with lite.json5
	if filepath.Base(path) != "lite.json5" {
		t.Errorf("ConfigPath should end with lite.json5, got %s", path)
	}

	// Should be in .meept directory
	if filepath.Base(filepath.Dir(path)) != ".meept" {
		t.Errorf("ConfigPath should be in .meept directory, got %s", path)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	// LoadConfig should return defaults when file doesn't exist
	cfg, err := LoadConfig()
	if err != nil {
		// Error is acceptable if home dir issues, but cfg should still be valid
		if cfg == nil {
			t.Fatal("LoadConfig should return defaults even on error")
		}
	}

	defaults := DefaultConfig()
	if cfg.HistorySize != defaults.HistorySize {
		t.Errorf("LoadConfig should return defaults, got HistorySize=%d", cfg.HistorySize)
	}
}

func TestConfigSaveAndLoad(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Override home directory for test
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create custom config
	cfg := &Config{
		DefaultModel: "gpt-4",
		ShowTokens:   false,
		ShowCost:     true,
		ShowDuration: false,
		AutoScroll:   false,
		HistorySize:  50,
		Keybindings: map[string]string{
			"menu": "ctrl+m",
			"quit": "ctrl+q",
		},
	}

	// Save it
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file exists
	path := ConfigPath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Config file not created: %v", err)
	}

	// Load it back
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	// Verify values
	if loaded.DefaultModel != "gpt-4" {
		t.Errorf("DefaultModel mismatch: got %s", loaded.DefaultModel)
	}
	if loaded.ShowTokens != false {
		t.Error("ShowTokens should be false")
	}
	if loaded.HistorySize != 50 {
		t.Errorf("HistorySize mismatch: got %d", loaded.HistorySize)
	}
	if loaded.Keybindings["menu"] != "ctrl+m" {
		t.Errorf("menu keybinding mismatch: got %s", loaded.Keybindings["menu"])
	}
}

func TestConfigJSON5Support(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Override home directory for test
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create .meept directory
	meeptDir := filepath.Join(tmpDir, ".meept")
	if err := os.MkdirAll(meeptDir, 0o755); err != nil {
		t.Fatalf("Failed to create .meept dir: %v", err)
	}

	// Write JSON5 format (with comments and trailing commas)
	json5Content := `{
  // Model to use by default
  "default_model": "claude-3",
  "show_tokens": true,
  "history_size": 200, // trailing comma
}`

	configPath := filepath.Join(meeptDir, "lite.json5")
	if err := os.WriteFile(configPath, []byte(json5Content), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Load it
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}

	if cfg.DefaultModel != "claude-3" {
		t.Errorf("DefaultModel should be claude-3, got %s", cfg.DefaultModel)
	}
	if cfg.HistorySize != 200 {
		t.Errorf("HistorySize should be 200, got %d", cfg.HistorySize)
	}
}

func TestGetKeybinding(t *testing.T) {
	cfg := DefaultConfig()

	// Test existing binding
	if binding := cfg.GetKeybinding("menu"); binding != "ctrl+x" {
		t.Errorf("GetKeybinding(menu) = %s, want ctrl+x", binding)
	}

	// Test custom override
	cfg.Keybindings["menu"] = "ctrl+m"
	if binding := cfg.GetKeybinding("menu"); binding != "ctrl+m" {
		t.Errorf("GetKeybinding(menu) = %s, want ctrl+m", binding)
	}

	// Test missing binding
	if binding := cfg.GetKeybinding("nonexistent"); binding != "" {
		t.Errorf("GetKeybinding(nonexistent) = %s, want empty", binding)
	}

	// Test nil keybindings map (should fall back to defaults)
	cfg.Keybindings = nil
	if binding := cfg.GetKeybinding("menu"); binding != "ctrl+x" {
		t.Errorf("GetKeybinding(menu) with nil map = %s, want ctrl+x", binding)
	}
}

func TestConfigClone(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DefaultModel = "original"

	clone := cfg.Clone()

	// Modify original
	cfg.DefaultModel = "modified"
	cfg.Keybindings["menu"] = "ctrl+m"

	// Clone should be unchanged
	if clone.DefaultModel != "original" {
		t.Errorf("Clone DefaultModel was modified: %s", clone.DefaultModel)
	}
	if clone.Keybindings["menu"] != "ctrl+x" {
		t.Errorf("Clone keybinding was modified: %s", clone.Keybindings["menu"])
	}
}

func TestConfigMerge(t *testing.T) {
	base := DefaultConfig()
	base.DefaultModel = "base-model"
	base.HistorySize = 50

	other := &Config{
		DefaultModel: "other-model",
		Keybindings: map[string]string{
			"custom": "ctrl+z",
		},
	}

	base.Merge(other)

	if base.DefaultModel != "other-model" {
		t.Errorf("DefaultModel should be other-model, got %s", base.DefaultModel)
	}
	if base.HistorySize != 50 {
		t.Errorf("HistorySize should remain 50, got %d", base.HistorySize)
	}
	if base.Keybindings["custom"] != "ctrl+z" {
		t.Errorf("custom keybinding should be ctrl+z, got %s", base.Keybindings["custom"])
	}
	if base.Keybindings["menu"] != "ctrl+x" {
		t.Errorf("menu keybinding should be preserved, got %s", base.Keybindings["menu"])
	}
}

func TestConfigMergeNil(t *testing.T) {
	cfg := DefaultConfig()
	original := cfg.DefaultModel

	// Merging nil should not panic or change anything
	cfg.Merge(nil)

	if cfg.DefaultModel != original {
		t.Errorf("Merge(nil) modified config")
	}
}

func TestConfigJSONMarshal(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DefaultModel = "test-model"

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed Config
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed.DefaultModel != "test-model" {
		t.Errorf("DefaultModel mismatch: got %s", parsed.DefaultModel)
	}
}
