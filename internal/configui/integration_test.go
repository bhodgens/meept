package configui

import (
	"testing"
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
