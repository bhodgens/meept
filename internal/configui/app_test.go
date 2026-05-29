// internal/configui/app_test.go
package configui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestJumpToSectionExactTitle(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("daemon") {
		t.Fatal("expected to find 'daemon' section")
	}
	if app.Phase() != PhaseSection {
		t.Fatalf("expected PhaseSection, got %v", app.Phase())
	}
	sec := app.Section()
	if sec == nil {
		t.Fatal("expected non-nil section")
	}
	if sec.Title() != "daemon" {
		t.Errorf("expected title 'daemon', got %q", sec.Title())
	}
}

func TestJumpToSectionExactTitleCaseInsensitive(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("Daemon") {
		t.Fatal("expected to find 'Daemon' section")
	}
	if app.Phase() != PhaseSection {
		t.Fatalf("expected PhaseSection, got %v", app.Phase())
	}
}

func TestJumpToSectionExactKeyPath(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("mcp_servers") {
		t.Fatal("expected to find 'mcp_servers' section")
	}
	if app.Phase() != PhaseSection {
		t.Fatalf("expected PhaseSection, got %v", app.Phase())
	}
	sec := app.Section()
	if sec == nil {
		t.Fatal("expected non-nil section")
	}
	if sec.Title() != "mcp servers" {
		t.Errorf("expected title 'mcp servers', got %q", sec.Title())
	}
}

func TestJumpToSectionAliasMCP(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("mcp") {
		t.Fatal("expected alias 'mcp' to resolve")
	}
	sec := app.Section()
	if sec.Title() != "mcp servers" {
		t.Errorf("expected 'mcp servers', got %q", sec.Title())
	}
}

func TestJumpToSectionAliasTUI(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("tui") {
		t.Fatal("expected alias 'tui' to resolve")
	}
	sec := app.Section()
	if sec.Title() != "client / tui" {
		t.Errorf("expected 'client / tui', got %q", sec.Title())
	}
}

func TestJumpToSectionAliasClient(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("client") {
		t.Fatal("expected alias 'client' to resolve")
	}
	sec := app.Section()
	if sec.Title() != "client / tui" {
		t.Errorf("expected 'client / tui', got %q", sec.Title())
	}
}

func TestJumpToSectionAliasAgent(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("agent") {
		t.Fatal("expected alias 'agent' to resolve")
	}
	sec := app.Section()
	if sec.Title() != "agent loop" {
		t.Errorf("expected 'agent loop', got %q", sec.Title())
	}
}

func TestJumpToSectionAliasQ(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("q") {
		t.Fatal("expected alias 'q' to resolve")
	}
	sec := app.Section()
	if sec.Title() != "q agent" {
		t.Errorf("expected 'q agent', got %q", sec.Title())
	}
}

func TestJumpToSectionPrefixMatch(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("sec") {
		t.Fatal("expected prefix 'sec' to match 'security'")
	}
	sec := app.Section()
	if sec.Title() != "security" {
		t.Errorf("expected 'security', got %q", sec.Title())
	}
}

func TestJumpToSectionPrefixKeyPath(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("dist") {
		t.Fatal("expected prefix 'dist' to match 'distributed memory' via keypath")
	}
	sec := app.Section()
	if sec.Title() != "distributed memory" {
		t.Errorf("expected 'distributed memory', got %q", sec.Title())
	}
}

func TestJumpToSectionAdvancedAutoEnabled(t *testing.T) {
	app := NewApp()
	// "queue" is an advanced-only section
	if !app.JumpToSection("queue") {
		t.Fatal("expected to find 'queue' section (advanced)")
	}
	// Advanced mode should have been enabled automatically
	if !app.showAdvanced {
		t.Error("expected advanced mode to be auto-enabled for advanced-only section")
	}
	sec := app.Section()
	if sec.Title() != "queue" {
		t.Errorf("expected 'queue', got %q", sec.Title())
	}
}

func TestJumpToSectionPrimaryNoAdvanced(t *testing.T) {
	app := NewApp()
	app.ToggleAdvanced() // turn on advanced
	if !app.JumpToSection("daemon") {
		t.Fatal("expected to find 'daemon' section")
	}
	// Since daemon is a primary item, advanced should be turned off
	if app.showAdvanced {
		t.Error("expected advanced mode to be off for primary section")
	}
}

func TestJumpToSectionNotFound(t *testing.T) {
	app := NewApp()
	if app.JumpToSection("zzz-nonexistent") {
		t.Error("expected JumpToSection to return false for unknown section")
	}
	if app.Phase() != PhaseMenu {
		t.Errorf("expected PhaseMenu when not found, got %v", app.Phase())
	}
}

func TestJumpToSectionWhitespace(t *testing.T) {
	app := NewApp()
	if !app.JumpToSection("  daemon  ") {
		t.Fatal("expected whitespace-trimmed 'daemon' to match")
	}
	sec := app.Section()
	if sec.Title() != "daemon" {
		t.Errorf("expected 'daemon', got %q", sec.Title())
	}
}

// --- Drilldown tests ---

func TestDrilldownEnterAndBack(t *testing.T) {
	app := NewApp()
	// Build a section with a drilldown field
	items := []DrilldownItem{
		{Name: "first", Fields: []Field{NewTextField("k", "K", "v1")}},
		{Name: "second", Fields: []Field{NewTextField("k", "K", "v2")}},
	}
	df := NewDrilldownField("things", "Things", items)
	sectionFields := []Field{
		NewTextField("name", "Name", "test"),
		df,
	}
	app.section = NewSectionModel("test section", "test", "test.json5", sectionFields)
	app.phase = PhaseSection

	// Move cursor to the drilldown field (index 1)
	app.section.MoveDown()
	if app.section.Cursor() != 1 {
		t.Fatalf("cursor should be at 1, got %d", app.section.Cursor())
	}

	// Press enter on the drilldown field
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(*App)
	if a.Phase() != PhaseDrilldown {
		t.Fatalf("expected PhaseDrilldown, got %v", a.Phase())
	}
	if len(a.drilldownItems) != 2 {
		t.Errorf("expected 2 drilldown items, got %d", len(a.drilldownItems))
	}
	if a.drilldownCursor != 0 {
		t.Errorf("expected cursor at 0, got %d", a.drilldownCursor)
	}
	if a.drilldownField != df {
		t.Error("drilldownField should point to the DrilldownField")
	}

	// Navigate down
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	a = model.(*App)
	if a.drilldownCursor != 1 {
		t.Errorf("expected cursor at 1, got %d", a.drilldownCursor)
	}

	// Go back with esc
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a = model.(*App)
	if a.Phase() != PhaseSection {
		t.Fatalf("expected PhaseSection after esc, got %v", a.Phase())
	}
	if a.drilldownField != nil {
		t.Error("drilldownField should be nil after back")
	}
}

func TestDrilldownNavigateUpDown(t *testing.T) {
	app := NewApp()
	items := []DrilldownItem{
		{Name: "a", Fields: []Field{NewTextField("k", "K", "1")}},
		{Name: "b", Fields: []Field{NewTextField("k", "K", "2")}},
		{Name: "c", Fields: []Field{NewTextField("k", "K", "3")}},
	}
	df := NewDrilldownField("items", "Items", items)
	app.section = NewSectionModel("test", "test", "test.json5", []Field{df})
	app.phase = PhaseSection

	// Enter drilldown
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(*App)

	// Down twice using 'j' key
	model, _ = a.Update(tea.KeyPressMsg{Text: "j"})
	a = model.(*App)
	model, _ = a.Update(tea.KeyPressMsg{Text: "j"})
	a = model.(*App)
	if a.drilldownCursor != 2 {
		t.Errorf("expected cursor at 2, got %d", a.drilldownCursor)
	}

	// Down again should clamp
	model, _ = a.Update(tea.KeyPressMsg{Text: "j"})
	a = model.(*App)
	if a.drilldownCursor != 2 {
		t.Errorf("expected cursor clamped at 2, got %d", a.drilldownCursor)
	}

	// Up to top using 'k' key
	model, _ = a.Update(tea.KeyPressMsg{Text: "k"})
	a = model.(*App)
	model, _ = a.Update(tea.KeyPressMsg{Text: "k"})
	a = model.(*App)
	if a.drilldownCursor != 0 {
		t.Errorf("expected cursor at 0, got %d", a.drilldownCursor)
	}

	// Up again should clamp
	model, _ = a.Update(tea.KeyPressMsg{Text: "k"})
	a = model.(*App)
	if a.drilldownCursor != 0 {
		t.Errorf("expected cursor clamped at 0, got %d", a.drilldownCursor)
	}
}

func TestDrilldownEnterItem(t *testing.T) {
	app := NewApp()
	items := []DrilldownItem{
		{Name: "first", Fields: []Field{NewTextField("k", "K", "v1")}},
		{Name: "second", Fields: []Field{NewTextField("k", "K", "v2")}},
	}
	df := NewDrilldownField("things", "Things", items)
	app.section = NewSectionModel("test section", "test", "test.json5", []Field{df})
	app.phase = PhaseSection

	// Enter drilldown
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(*App)

	// Enter on first item - should create a new section for the item's fields
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a = model.(*App)
	if a.Phase() != PhaseSection {
		t.Fatalf("expected PhaseSection after entering item, got %v", a.Phase())
	}
	sec := a.Section()
	if sec == nil {
		t.Fatal("expected non-nil section")
	}
	// Title should include breadcrumb
	if sec.Title() != "test section > Things > first" {
		t.Errorf("expected breadcrumb title, got %q", sec.Title())
	}
	if sec.FieldCount() != 1 {
		t.Errorf("expected 1 field in item section, got %d", sec.FieldCount())
	}
	// Verify drilldown prefix is set correctly
	if !sec.IsDrilldown() {
		t.Error("expected IsDrilldown() to be true for drilldown sub-section")
	}
	if sec.DrilldownPrefix() != "things.first" {
		t.Errorf("expected drilldown prefix 'things.first', got %q", sec.DrilldownPrefix())
	}
	// SectionKey and ConfigFile should be inherited from parent
	if sec.SectionKey() != "test" {
		t.Errorf("expected section key 'test', got %q", sec.SectionKey())
	}
	if sec.ConfigFile() != "test.json5" {
		t.Errorf("expected config file 'test.json5', got %q", sec.ConfigFile())
	}
}

func TestDrilldownNewItem(t *testing.T) {
	app := NewApp()
	items := []DrilldownItem{
		{Name: "existing", Fields: []Field{NewTextField("k", "K", "v")}},
	}
	df := NewDrilldownField("things", "Things", items)
	app.section = NewSectionModel("test", "test", "test.json5", []Field{df})
	app.phase = PhaseSection

	// Enter drilldown
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(*App)

	// Press 'n' to add new item
	model, _ = a.Update(tea.KeyPressMsg{Text: "n"})
	a = model.(*App)
	if len(a.drilldownItems) != 2 {
		t.Errorf("expected 2 items after new, got %d", len(a.drilldownItems))
	}
	if a.drilldownItems[1].Name != "new item" {
		t.Errorf("expected 'new item', got %q", a.drilldownItems[1].Name)
	}
	if a.drilldownCursor != 1 {
		t.Errorf("expected cursor at new item (1), got %d", a.drilldownCursor)
	}
	// DrilldownField should also be updated
	if len(df.Items) != 2 {
		t.Errorf("expected DrilldownField.Items to have 2 items, got %d", len(df.Items))
	}
}

func TestDrilldownDeleteItem(t *testing.T) {
	app := NewApp()
	items := []DrilldownItem{
		{Name: "first", Fields: []Field{NewTextField("k", "K", "v1")}},
		{Name: "second", Fields: []Field{NewTextField("k", "K", "v2")}},
		{Name: "third", Fields: []Field{NewTextField("k", "K", "v3")}},
	}
	df := NewDrilldownField("things", "Things", items)
	app.section = NewSectionModel("test", "test", "test.json5", []Field{df})
	app.phase = PhaseSection

	// Enter drilldown
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(*App)

	// Move to index 1
	model, _ = a.Update(tea.KeyPressMsg{Text: "j"})
	a = model.(*App)

	// Delete item at index 1 ("second")
	model, _ = a.Update(tea.KeyPressMsg{Text: "d"})
	a = model.(*App)
	if len(a.drilldownItems) != 2 {
		t.Fatalf("expected 2 items after delete, got %d", len(a.drilldownItems))
	}
	if a.drilldownItems[0].Name != "first" {
		t.Errorf("expected first item 'first', got %q", a.drilldownItems[0].Name)
	}
	if a.drilldownItems[1].Name != "third" {
		t.Errorf("expected second item 'third', got %q", a.drilldownItems[1].Name)
	}
	// Cursor should now be at 1 (was at 1, now points to "third")
	if a.drilldownCursor != 1 {
		t.Errorf("expected cursor at 1 after delete, got %d", a.drilldownCursor)
	}
}

func TestDrilldownDeleteLastItem(t *testing.T) {
	app := NewApp()
	items := []DrilldownItem{
		{Name: "only", Fields: []Field{NewTextField("k", "K", "v")}},
	}
	df := NewDrilldownField("things", "Things", items)
	app.section = NewSectionModel("test", "test", "test.json5", []Field{df})
	app.phase = PhaseSection

	// Enter drilldown
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(*App)

	// Delete the only item
	model, _ = a.Update(tea.KeyPressMsg{Text: "d"})
	a = model.(*App)
	if len(a.drilldownItems) != 0 {
		t.Errorf("expected 0 items after delete, got %d", len(a.drilldownItems))
	}
	if a.drilldownCursor != 0 {
		t.Errorf("expected cursor at 0 after deleting only item, got %d", a.drilldownCursor)
	}
}

func TestDrilldownEmptyItemsNoOp(t *testing.T) {
	app := NewApp()
	df := NewDrilldownField("things", "Things", []DrilldownItem{})
	app.section = NewSectionModel("test", "test", "test.json5", []Field{df})
	app.phase = PhaseSection

	// Enter drilldown - should stay at PhaseSection since items is empty
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(*App)
	if a.Phase() != PhaseSection {
		t.Errorf("expected PhaseSection for empty drilldown, got %v", a.Phase())
	}
}

func TestDrilldownEnterEmptyFieldsNoOp(t *testing.T) {
	app := NewApp()
	items := []DrilldownItem{
		{Name: "empty", Fields: []Field{}},
	}
	df := NewDrilldownField("things", "Things", items)
	app.section = NewSectionModel("test", "test", "test.json5", []Field{df})
	app.phase = PhaseSection

	// Enter drilldown
	model, _ := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a := model.(*App)
	if a.Phase() != PhaseDrilldown {
		t.Fatalf("expected PhaseDrilldown, got %v", a.Phase())
	}

	// Enter on item with empty fields should be no-op
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a = model.(*App)
	if a.Phase() != PhaseDrilldown {
		t.Errorf("expected PhaseDrilldown (no-op for empty fields), got %v", a.Phase())
	}
}

func TestDrilldownViewBreadcrumb(t *testing.T) {
	app := NewApp()
	items := []DrilldownItem{
		{Name: "item1", Fields: []Field{NewTextField("k", "K", "v")}},
	}
	df := NewDrilldownField("things", "Things", items)
	app.section = NewSectionModel("my section", "mysec", "test.json5", []Field{df})
	app.phase = PhaseDrilldown
	app.drilldownField = df
	app.drilldownItems = items
	app.drilldownCursor = 0

	view := app.View()
	rendered := view.Content
	if !contains(rendered, "my section") {
		t.Error("view should contain section name in breadcrumb")
	}
	if !contains(rendered, "Things") {
		t.Error("view should contain drilldown field label in breadcrumb")
	}
	if !contains(rendered, "item1") {
		t.Error("view should contain item name")
	}
	if !contains(rendered, "enter view details") {
		t.Error("view should contain help text for enter")
	}
}

// contains is a helper for checking if a string contains a substring.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- Section cache tests ---

func TestSectionCachePreservesDirtySection(t *testing.T) {
	app := NewApp()
	// Select the first section (daemon, index 0)
	app.SelectSection(0)
	sec := app.Section()
	if sec == nil {
		t.Fatal("expected non-nil section after select")
	}

	// Dirty a text field (data_dir at index 1) by editing it
	f := sec.Fields()[1]
	if err := f.Set("modified_value_for_cache_test"); err != nil {
		t.Fatalf("failed to set field: %v", err)
	}
	if !sec.IsDirty() {
		t.Fatal("expected section to be dirty after field change")
	}

	// Go back to menu — should stash dirty section in cache
	app.BackToMenu()
	if app.section != nil {
		t.Error("expected section to be nil after BackToMenu")
	}

	// Cache should contain the dirty section
	cached, ok := app.sectionCache["daemon"]
	if !ok {
		t.Fatal("expected dirty section to be cached by section key")
	}
	if !cached.IsDirty() {
		t.Error("cached section should still be dirty")
	}

	// Navigate back to the same section — should get the cached version
	app.SelectSection(0)
	restored := app.Section()
	if restored == nil {
		t.Fatal("expected non-nil section after re-select")
	}
	if restored != cached {
		t.Error("expected the restored section to be the same cached instance")
	}
	if restored.Fields()[1].Get() != "modified_value_for_cache_test" {
		t.Errorf("expected dirty value to be preserved, got %q", restored.Fields()[1].Get())
	}
}

func TestSectionCacheCleanNotCached(t *testing.T) {
	app := NewApp()
	// Select a section without dirtying anything
	app.SelectSection(0)
	app.BackToMenu()

	// Cache should NOT contain the clean section
	_, ok := app.sectionCache["daemon"]
	if ok {
		t.Error("expected clean section to NOT be cached")
	}
}

func TestSectionCacheClearedOnSave(t *testing.T) {
	app := NewApp()
	app.SelectSection(0)
	// Dirty a text field (data_dir at index 1)
	f := app.Section().Fields()[1]
	if err := f.Set("will_be_saved"); err != nil {
		t.Fatalf("failed to set field: %v", err)
	}

	// Stash the dirty section (simulates what happens when entering confirm save
	// from a clean menu navigation or when the user presses 's' and then navigates)
	app.stashCurrentSection()

	// Verify the section IS in cache
	_, ok := app.sectionCache["daemon"]
	if !ok {
		t.Fatal("expected dirty section to be cached before save confirm")
	}

	// Simulate the save path: delete from cache (as handleConfirmKey does on "y")
	if app.section != nil {
		delete(app.sectionCache, app.section.SectionKey())
	}
	_, ok = app.sectionCache["daemon"]
	if ok {
		t.Error("expected section to be removed from cache after save")
	}
}

func TestSectionCacheClearedOnDiscard(t *testing.T) {
	app := NewApp()
	app.SelectSection(0)
	// Dirty a text field (data_dir at index 1)
	f := app.Section().Fields()[1]
	if err := f.Set("will_be_discarded"); err != nil {
		t.Fatalf("failed to set field: %v", err)
	}

	// Simulate stash (normally done by BackToMenu or SelectSection)
	app.stashCurrentSection()
	_, ok := app.sectionCache["daemon"]
	if !ok {
		t.Fatal("expected dirty section to be cached")
	}

	// Now simulate the discard path: reset fields and delete from cache
	if app.section != nil {
		for _, field := range app.section.Fields() {
			field.Reset()
		}
		delete(app.sectionCache, app.section.SectionKey())
	}
	_, ok = app.sectionCache["daemon"]
	if ok {
		t.Error("expected section to be removed from cache after discard")
	}
}

func TestSectionCachePreservesAcrossSwitch(t *testing.T) {
	app := NewApp()

	// Select daemon (index 0), dirty a text field (data_dir at index 1)
	app.SelectSection(0)
	if err := app.Section().Fields()[1].Set("daemon_dirty"); err != nil {
		t.Fatalf("failed to set field: %v", err)
	}

	// Switch to a different section (transport, index 1) — daemon should be stashed
	app.SelectSection(1)
	_, ok := app.sectionCache["daemon"]
	if !ok {
		t.Fatal("expected dirty daemon section to be cached when switching away")
	}

	// Transport section should be fresh from disk (not cached)
	if app.Section().IsDirty() {
		t.Error("transport section should be clean (loaded fresh)")
	}

	// Switch back to daemon — should restore cached dirty state
	app.SelectSection(0)
	if !app.Section().IsDirty() {
		t.Error("restored daemon section should be dirty")
	}
	if app.Section().Fields()[1].Get() != "daemon_dirty" {
		t.Errorf("expected preserved value 'daemon_dirty', got %q", app.Section().Fields()[1].Get())
	}
}

func TestSectionCacheDrilldownNotCached(t *testing.T) {
	app := NewApp()
	// Drilldown sub-sections should NOT be cached (they have drilldownPrefix set)
	// Create a drilldown sub-section manually
	sec := NewDrilldownSectionModel("test > sub", "test", "test.json5", "test.sub", []Field{
		NewTextField("k", "K", "modified"),
	})
	app.section = sec
	app.phase = PhaseSection

	// Stash should NOT cache drilldown sections
	app.stashCurrentSection()
	_, ok := app.sectionCache["test"]
	if ok {
		t.Error("expected drilldown sub-section to NOT be cached")
	}
}
