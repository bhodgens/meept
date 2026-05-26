package configui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/tailscale/hujson"
)

func TestIntegrationMenuToSection(t *testing.T) {
	app := NewApp()

	// Verify menu items
	items := app.MenuItems()
	if len(items) < 10 {
		t.Errorf("expected at least 10 primary menu items, got %d", len(items))
	}

	// Select daemon section
	app.SelectSection(0)
	if app.Phase() != PhaseSection {
		t.Fatalf("expected PhaseSection, got %v", app.Phase())
	}

	section := app.Section()
	if section == nil {
		t.Fatal("expected non-nil section")
	}
	if section.Title() != "daemon" {
		t.Errorf("expected daemon, got %s", section.Title())
	}
	if section.FieldCount() < 3 {
		t.Errorf("expected at least 3 daemon fields, got %d", section.FieldCount())
	}

	// Verify field has expected structure
	f := section.CurrentField()
	if f == nil {
		t.Fatal("expected non-nil current field")
	}

	// Navigate to the second field (data_dir, a TextField) so we can set any string
	section.MoveDown()
	f = section.CurrentField()

	// Edit a field
	f.Set("modified_value")
	if !f.IsDirty() {
		t.Error("field should be dirty after edit")
	}
	if !section.IsDirty() {
		t.Error("section should be dirty after field edit")
	}

	// Reset
	f.Reset()
	if f.IsDirty() {
		t.Error("field should not be dirty after reset")
	}

	// Go back to menu
	app.BackToMenu()
	if app.Phase() != PhaseMenu {
		t.Fatalf("expected PhaseMenu, got %v", app.Phase())
	}
}

func TestIntegrationAdvancedToggle(t *testing.T) {
	app := NewApp()
	primaryCount := len(app.MenuItems())

	app.ToggleAdvanced()
	advancedCount := len(app.MenuItems())
	if advancedCount <= primaryCount {
		t.Errorf("expected more items after toggle, got %d vs %d", advancedCount, primaryCount)
	}

	// Toggle back
	app.ToggleAdvanced()
	if len(app.MenuItems()) != primaryCount {
		t.Error("expected primary count after toggling back")
	}
}

// TestIntegrationSaveAndReload verifies the full flow:
// load config → build section → modify field → save → reload → verify persistence.
func TestIntegrationSaveAndReload(t *testing.T) {
	origLoader := loadMainConfig
	origPath := ConfigFilePath
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	t.Cleanup(func() {
		loadMainConfig = origLoader
		ConfigFilePath = origPath
	})

	// Step 1: Create initial config and write it
	cfg := config.DefaultConfig()
	cfg.Daemon.LogLevel = "INFO"
	if err := WriteConfigFile(path, cfg); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	// Step 2: Loader returns the initial config
	loadMainConfig = func() (*config.Config, error) {
		return config.LoadJSON5Config(path)
	}
	ConfigFilePath = func(name string) string { return path }

	// Step 3: Build section model for daemon section
	fields := BuildSectionFields("daemon")
	sm := NewSectionModel("daemon", "daemon", "meept.json5", fields)

	// Step 4: Find the log_level field and modify it
	var logLevelField Field
	for _, f := range sm.Fields() {
		if f.Key() == "log_level" {
			logLevelField = f
			break
		}
	}
	if logLevelField == nil {
		t.Fatal("expected to find log_level field in daemon section")
	}
	logLevelField.Set("DEBUG")
	if !logLevelField.IsDirty() {
		t.Error("log_level should be dirty after set")
	}

	// Step 5: Save
	if err := SaveSection(sm); err != nil {
		t.Fatalf("SaveSection: %v", err)
	}

	// Step 6: Reload from disk and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	standardJSON, err := hujson.Standardize(data)
	if err != nil {
		t.Fatalf("standardize: %v", err)
	}
	var cfg2 config.Config
	if err := json.Unmarshal(standardJSON, &cfg2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg2.Daemon.LogLevel != "DEBUG" {
		t.Errorf("expected log_level='DEBUG' after reload, got %q", cfg2.Daemon.LogLevel)
	}
}
