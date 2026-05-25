package configui

import (
	"testing"
)

func makeTestFields() []Field {
	return []Field{
		NewTextField("host", "Host", "localhost"),
		NewToggleField("debug", "Debug", true),
		NewSelectField("log_level", "Log Level", "info", []string{"debug", "info", "warn", "error"}),
		NewNumberField("port", "Port", 8080),
	}
}

func TestNewSectionModel(t *testing.T) {
	fields := makeTestFields()
	s := NewSectionModel("General", "daemon", "meept.json5", fields)

	if s.Title() != "General" {
		t.Errorf("Title() = %q, want %q", s.Title(), "General")
	}
	if s.KeyPath() != "meept.json5" {
		t.Errorf("KeyPath() = %q, want %q", s.KeyPath(), "meept.json5")
	}
	if s.ConfigFile() != "meept.json5" {
		t.Errorf("ConfigFile() = %q, want %q", s.ConfigFile(), "meept.json5")
	}
	if s.SectionKey() != "daemon" {
		t.Errorf("SectionKey() = %q, want %q", s.SectionKey(), "daemon")
	}
	if s.FieldCount() != 4 {
		t.Errorf("FieldCount() = %d, want 4", s.FieldCount())
	}
	if s.Cursor() != 0 {
		t.Errorf("Cursor() = %d, want 0 (initial cursor)", s.Cursor())
	}
}

func TestNewSectionModelEmptyFields(t *testing.T) {
	s := NewSectionModel("Empty", "empty", "empty.json5", nil)
	if s.FieldCount() != 0 {
		t.Errorf("FieldCount() = %d, want 0", s.FieldCount())
	}
	if s.Cursor() != 0 {
		t.Errorf("Cursor() = %d, want 0", s.Cursor())
	}
}

func TestMoveDown(t *testing.T) {
	s := NewSectionModel("General", "daemon", "meept.json5", makeTestFields())

	// cursor starts at 0
	if s.Cursor() != 0 {
		t.Fatalf("initial cursor = %d, want 0", s.Cursor())
	}

	s.MoveDown()
	if s.Cursor() != 1 {
		t.Errorf("after MoveDown: cursor = %d, want 1", s.Cursor())
	}

	s.MoveDown()
	if s.Cursor() != 2 {
		t.Errorf("after 2nd MoveDown: cursor = %d, want 2", s.Cursor())
	}

	s.MoveDown()
	if s.Cursor() != 3 {
		t.Errorf("after 3rd MoveDown: cursor = %d, want 3", s.Cursor())
	}

	// clamping: should stay at last index
	s.MoveDown()
	if s.Cursor() != 3 {
		t.Errorf("after MoveDown at end: cursor = %d, want 3 (clamped)", s.Cursor())
	}
}

func TestMoveUp(t *testing.T) {
	s := NewSectionModel("General", "daemon", "meept.json5", makeTestFields())

	// move to the bottom first
	for i := 0; i < 3; i++ {
		s.MoveDown()
	}
	if s.Cursor() != 3 {
		t.Fatalf("cursor before MoveUp tests = %d, want 3", s.Cursor())
	}

	s.MoveUp()
	if s.Cursor() != 2 {
		t.Errorf("after MoveUp: cursor = %d, want 2", s.Cursor())
	}

	s.MoveUp()
	if s.Cursor() != 1 {
		t.Errorf("after 2nd MoveUp: cursor = %d, want 1", s.Cursor())
	}

	s.MoveUp()
	if s.Cursor() != 0 {
		t.Errorf("after 3rd MoveUp: cursor = %d, want 0", s.Cursor())
	}

	// clamping: should stay at 0
	s.MoveUp()
	if s.Cursor() != 0 {
		t.Errorf("after MoveUp at top: cursor = %d, want 0 (clamped)", s.Cursor())
	}
}

func TestMoveDownEmptyFields(t *testing.T) {
	s := NewSectionModel("Empty", "empty", "empty.json5", nil)
	s.MoveDown()
	if s.Cursor() != 0 {
		t.Errorf("MoveDown on empty: cursor = %d, want 0", s.Cursor())
	}
}

func TestMoveUpEmptyFields(t *testing.T) {
	s := NewSectionModel("Empty", "empty", "empty.json5", nil)
	s.MoveUp()
	if s.Cursor() != 0 {
		t.Errorf("MoveUp on empty: cursor = %d, want 0", s.Cursor())
	}
}

func TestCurrentField(t *testing.T) {
	fields := makeTestFields()
	s := NewSectionModel("General", "daemon", "meept.json5", fields)

	// cursor at 0
	f := s.CurrentField()
	if f.Key() != "host" {
		t.Errorf("CurrentField() at 0: key = %q, want %q", f.Key(), "host")
	}

	s.MoveDown() // cursor at 1
	f = s.CurrentField()
	if f.Key() != "debug" {
		t.Errorf("CurrentField() at 1: key = %q, want %q", f.Key(), "debug")
	}

	s.MoveDown() // cursor at 2
	f = s.CurrentField()
	if f.Key() != "log_level" {
		t.Errorf("CurrentField() at 2: key = %q, want %q", f.Key(), "log_level")
	}

	s.MoveDown() // cursor at 3
	f = s.CurrentField()
	if f.Key() != "port" {
		t.Errorf("CurrentField() at 3: key = %q, want %q", f.Key(), "port")
	}
}

func TestCurrentFieldEmptyFields(t *testing.T) {
	s := NewSectionModel("Empty", "empty", "empty.json5", nil)
	f := s.CurrentField()
	if f != nil {
		t.Errorf("CurrentField() on empty section = %v, want nil", f)
	}
}

func TestFields(t *testing.T) {
	fields := makeTestFields()
	s := NewSectionModel("General", "daemon", "meept.json5", fields)

	got := s.Fields()
	if len(got) != len(fields) {
		t.Fatalf("Fields() length = %d, want %d", len(got), len(fields))
	}
	for i, f := range got {
		if f.Key() != fields[i].Key() {
			t.Errorf("Fields()[%d].Key() = %q, want %q", i, f.Key(), fields[i].Key())
		}
	}
}

func TestIsDirtyNoChanges(t *testing.T) {
	s := NewSectionModel("General", "daemon", "meept.json5", makeTestFields())
	if s.IsDirty() {
		t.Error("IsDirty() = true on freshly created section, want false")
	}
}

func TestIsDirtyWithOneDirtyField(t *testing.T) {
	s := NewSectionModel("General", "daemon", "meept.json5", makeTestFields())

	// modify the first field
	s.CurrentField().Set("example.com")

	if !s.IsDirty() {
		t.Error("IsDirty() = false after modifying first field, want true")
	}
}

func TestIsDirtyAfterReset(t *testing.T) {
	s := NewSectionModel("General", "daemon", "meept.json5", makeTestFields())

	// modify then reset
	s.CurrentField().Set("example.com")
	s.CurrentField().Reset()

	if s.IsDirty() {
		t.Error("IsDirty() = true after reset, want false")
	}
}

func TestIsDirtyMultipleFields(t *testing.T) {
	s := NewSectionModel("General", "daemon", "meept.json5", makeTestFields())

	// modify second field (move down first)
	s.MoveDown() // cursor at 1 (debug toggle)
	s.CurrentField().Set("false")

	// modify third field
	s.MoveDown() // cursor at 2 (log_level select)
	s.CurrentField().Set("warn")

	if !s.IsDirty() {
		t.Error("IsDirty() = false after modifying two fields, want true")
	}
}

func TestIsDirtyEmptyFields(t *testing.T) {
	s := NewSectionModel("Empty", "empty", "empty.json5", nil)
	if s.IsDirty() {
		t.Error("IsDirty() = true on empty section, want false")
	}
}

func TestMoveDownSingleField(t *testing.T) {
	s := NewSectionModel("Single", "single", "single.json5", []Field{
		NewTextField("only", "Only", "value"),
	})
	s.MoveDown()
	if s.Cursor() != 0 {
		t.Errorf("MoveDown on single field: cursor = %d, want 0", s.Cursor())
	}
	s.MoveUp()
	if s.Cursor() != 0 {
		t.Errorf("MoveUp on single field: cursor = %d, want 0", s.Cursor())
	}
}

// --- Drilldown section model tests ---

func TestNewDrilldownSectionModel(t *testing.T) {
	fields := makeTestFields()
	s := NewDrilldownSectionModel("test > items > myitem", "test", "test.json5", "items.myitem", fields)

	if s.Title() != "test > items > myitem" {
		t.Errorf("Title() = %q, want %q", s.Title(), "test > items > myitem")
	}
	if s.ConfigFile() != "test.json5" {
		t.Errorf("ConfigFile() = %q, want %q", s.ConfigFile(), "test.json5")
	}
	if s.SectionKey() != "test" {
		t.Errorf("SectionKey() = %q, want %q", s.SectionKey(), "test")
	}
	if !s.IsDrilldown() {
		t.Error("IsDrilldown() = false, want true")
	}
	if s.DrilldownPrefix() != "items.myitem" {
		t.Errorf("DrilldownPrefix() = %q, want %q", s.DrilldownPrefix(), "items.myitem")
	}
}

func TestNewSectionModelNotDrilldown(t *testing.T) {
	s := NewSectionModel("General", "daemon", "meept.json5", makeTestFields())
	if s.IsDrilldown() {
		t.Error("IsDrilldown() = true for normal section, want false")
	}
	if s.DrilldownPrefix() != "" {
		t.Errorf("DrilldownPrefix() = %q, want empty string", s.DrilldownPrefix())
	}
}
