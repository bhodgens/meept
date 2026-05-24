// internal/configui/app_test.go
package configui

import (
	"testing"
)

func TestNewApp(t *testing.T) {
	app := NewApp()
	if app == nil {
		t.Fatal("expected non-nil app")
	}
	if app.Phase() != PhaseMenu {
		t.Errorf("expected PhaseMenu, got %v", app.Phase())
	}
}

func TestAppSections(t *testing.T) {
	app := NewApp()
	sections := app.MenuItems()
	if len(sections) == 0 {
		t.Error("expected at least one menu section")
	}
	// Verify primary sections exist
	names := make(map[string]bool)
	for _, s := range sections {
		names[s.Title] = true
	}
	for _, required := range []string{"daemon", "transport", "llm", "models", "agents", "memory", "security", "mcp servers", "client / tui", "scheduler"} {
		if !names[required] {
			t.Errorf("missing required section: %s", required)
		}
	}
}

func TestAppSelectSection(t *testing.T) {
	app := NewApp()
	app.SelectSection(0) // select first section
	if app.Phase() != PhaseSection {
		t.Errorf("expected PhaseSection after select, got %v", app.Phase())
	}
}

func TestAppBackToMenu(t *testing.T) {
	app := NewApp()
	app.SelectSection(0)
	app.BackToMenu()
	if app.Phase() != PhaseMenu {
		t.Errorf("expected PhaseMenu after back, got %v", app.Phase())
	}
}

func TestAppAdvancedToggle(t *testing.T) {
	app := NewApp()
	primaryCount := len(app.MenuItems())
	app.ToggleAdvanced()
	allCount := len(app.MenuItems())
	if allCount <= primaryCount {
		t.Errorf("expected more items with advanced, got %d vs %d", allCount, primaryCount)
	}
}

func TestAppSelectSectionBounds(t *testing.T) {
	app := NewApp()
	// Negative index should be no-op
	app.SelectSection(-1)
	if app.Phase() != PhaseMenu {
		t.Errorf("expected PhaseMenu for negative index, got %v", app.Phase())
	}
	// Out-of-range index should be no-op
	app.SelectSection(len(app.MenuItems()))
	if app.Phase() != PhaseMenu {
		t.Errorf("expected PhaseMenu for out-of-range index, got %v", app.Phase())
	}
}

func TestAppToggleAdvancedTwice(t *testing.T) {
	app := NewApp()
	originalCount := len(app.MenuItems())
	app.ToggleAdvanced()
	if len(app.MenuItems()) == originalCount {
		t.Error("expected more items after first toggle")
	}
	app.ToggleAdvanced()
	if len(app.MenuItems()) != originalCount {
		t.Errorf("expected original count %d after second toggle, got %d", originalCount, len(app.MenuItems()))
	}
}

func TestAppMenuCursorClamp(t *testing.T) {
	app := NewApp()
	// Toggle advanced, move cursor past primary count, then toggle back
	app.ToggleAdvanced()
	lastIdx := len(app.MenuItems()) - 1
	app.menuCursor = lastIdx
	app.ToggleAdvanced()
	// Cursor should be clamped to last primary item
	if app.MenuCursor() >= len(app.MenuItems()) {
		t.Errorf("cursor should be clamped, got %d with %d items", app.MenuCursor(), len(app.MenuItems()))
	}
}

func TestAppSectionFields(t *testing.T) {
	app := NewApp()
	app.SelectSection(0) // daemon section
	sec := app.Section()
	if sec == nil {
		t.Fatal("expected non-nil section after select")
	}
	if sec.FieldCount() == 0 {
		t.Error("expected daemon section to have fields")
	}
}

func TestAppSelectStubSection(t *testing.T) {
	app := NewApp()
	// Select an advanced section that has no builder (should get stub)
	app.ToggleAdvanced()
	app.SelectSection(len(app.MenuItems()) - 1) // last advanced item, likely unimplemented
	sec := app.Section()
	if sec == nil {
		t.Fatal("expected non-nil section")
	}
	if sec.FieldCount() != 1 {
		t.Errorf("expected 1 stub field, got %d", sec.FieldCount())
	}
}

func TestBuildSectionFieldsDaemon(t *testing.T) {
	fields := BuildSectionFields("daemon")
	if len(fields) == 0 {
		t.Error("expected daemon fields")
	}
	// First field should be log_level select
	if fields[0].Key() != "log_level" {
		t.Errorf("expected first field key 'log_level', got %s", fields[0].Key())
	}
}

func TestBuildSectionFieldsScheduler(t *testing.T) {
	fields := BuildSectionFields("scheduler")
	if len(fields) != 2 {
		t.Errorf("expected 2 scheduler fields, got %d", len(fields))
	}
}

func TestBuildSectionFieldsStub(t *testing.T) {
	fields := BuildSectionFields("nonexistent")
	if len(fields) != 1 {
		t.Errorf("expected 1 stub field for unknown section, got %d", len(fields))
	}
	if fields[0].Key() != "_stub" {
		t.Errorf("expected stub key '_stub', got %s", fields[0].Key())
	}
}
