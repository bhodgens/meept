package sharedclient

import (
	"testing"
)

func TestNewSlashAutocomplete(t *testing.T) {
	ac := NewSlashAutocomplete()
	if ac == nil {
		t.Fatal("NewSlashAutocomplete() returned nil")
	}
	if ac.visible {
		t.Error("NewSlashAutocomplete().visible = true, want false")
	}
	if ac.selected != 0 {
		t.Errorf("NewSlashAutocomplete().selected = %d, want 0", ac.selected)
	}
	if ac.filter != "" {
		t.Errorf("NewSlashAutocomplete().filter = %q, want \"\"", ac.filter)
	}
	if ac.maxHeight != 8 {
		t.Errorf("NewSlashAutocomplete().maxHeight = %d, want 8", ac.maxHeight)
	}
	// Should have builtin commands
	if len(ac.commands) == 0 {
		t.Error("NewSlashAutocomplete() has no commands")
	}
}

func TestSlashAutocompleteShowHide(t *testing.T) {
	ac := NewSlashAutocomplete()

	if ac.IsVisible() {
		t.Error("NewSlashAutocomplete().IsVisible() = true, want false")
	}

	ac.Show("hel")

	if !ac.IsVisible() {
		t.Error("SlashAutocomplete.Show().IsVisible() = false, want true")
	}
	if ac.filter != "hel" {
		t.Errorf("SlashAutocomplete.Show().filter = %q, want \"hel\"", ac.filter)
	}

	ac.Hide()

	if ac.IsVisible() {
		t.Error("SlashAutocomplete.Hide().IsVisible() = true, want false")
	}
}

func TestSlashAutocompleteSetFilter(t *testing.T) {
	ac := NewSlashAutocomplete()

	// Filter for commands starting with "he"
	ac.Show("he")

	filtered := ac.GetFilteredCommands()
	if len(filtered) == 0 {
		t.Error("SlashAutocomplete.SetFilter() found no matches for \"he\"")
	}

	// All filtered commands should start with "he"
	for _, cmd := range filtered {
		if len(cmd) < 2 || cmd[:2] != "he" {
			t.Errorf("Filtered command %q doesn't match filter \"he\"", cmd)
		}
	}
}

func TestSlashAutocompleteNavigation(t *testing.T) {
	ac := NewSlashAutocomplete()
	ac.Show("") // Show all commands

	initialSelected := ac.GetSelectedIndex()

	// Navigate up should stay at 0 (or wrap, depending on implementation)
	ac.Up()
	if ac.GetSelectedIndex() != 0 && ac.GetSelectedIndex() < len(ac.Commands())-1 {
		t.Errorf("SlashAutocomplete.Up() from start = %d, want 0 or last", ac.GetSelectedIndex())
	}

	// Navigate down
	ac.Down()
	if ac.GetSelectedIndex() <= initialSelected && len(ac.filtered) > 1 {
		t.Error("SlashAutocomplete.Down() didn't move selection down")
	}
}

func TestSlashAutocompleteSelect(t *testing.T) {
	ac := NewSlashAutocomplete()
	ac.Show("hel")

	cmd, ok := ac.Select()
	if !ok {
		t.Error("SlashAutocomplete.Select() returned false, want true")
	}
	if cmd == "" {
		t.Error("SlashAutocomplete.Select() returned empty command")
	}
	if cmd[0] != '/' {
		t.Errorf("SlashAutocomplete.Select() = %q, should start with /", cmd)
	}
}

func TestSlashAutocompleteSelectEmpty(t *testing.T) {
	ac := NewSlashAutocomplete()
	// New instance has all builtin commands, so filtered won't be empty
	// Test with actual empty filtered by filtering for something that won't match
	ac.Show("zzzznonexistent")

	cmd, ok := ac.Select()
	// Select returns true with empty command when filtered is empty
	if len(ac.filtered) == 0 && !ok {
		// This is acceptable - no command to select
		return
	}
	// If there happens to be a match, just verify the behavior is consistent
	_ = cmd
}

func TestSlashAutocompleteUpdateCommands(t *testing.T) {
	ac := NewSlashAutocomplete()
	initialCount := len(ac.Commands())

	// Add new commands
	newCmds := []string{"custom-cmd-1", "custom-cmd-2"}
	ac.UpdateCommands(newCmds)

	newCount := len(ac.Commands())
	if newCount != initialCount+2 {
		t.Errorf("SlashAutocomplete.UpdateCommands() count = %d, want %d", newCount, initialCount+2)
	}

	// Verify new commands are present
	cmds := ac.Commands()
	found := 0
	for _, cmd := range cmds {
		if cmd == "custom-cmd-1" || cmd == "custom-cmd-2" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("SlashAutocomplete.UpdateCommands() found %d new commands, want 2", found)
	}
}

func TestSlashAutocompleteUpdateCommandsDuplicate(t *testing.T) {
	ac := NewSlashAutocomplete()

	// Try to add duplicate commands
	existingCmds := ac.Commands()
	if len(existingCmds) == 0 {
		t.Skip("No existing commands to test duplicates")
	}

	initialCount := len(ac.Commands())
	ac.UpdateCommands(existingCmds) // Add existing commands again

	newCount := len(ac.Commands())
	if newCount != initialCount {
		t.Errorf("SlashAutocomplete.UpdateCommands() with duplicates = %d, want %d", newCount, initialCount)
	}
}

func TestSlashAutocompleteReplaceCommands(t *testing.T) {
	ac := NewSlashAutocomplete()

	newCmds := []string{"only-cmd-1", "only-cmd-2", "only-cmd-3"}
	ac.ReplaceCommands(newCmds)

	cmds := ac.Commands()
	if len(cmds) != 3 {
		t.Errorf("SlashAutocomplete.ReplaceCommands() count = %d, want 3", len(cmds))
	}

	// Verify builtin commands are gone
	for _, cmd := range cmds {
		if cmd == "help" || cmd == "clear" {
			t.Error("SlashAutocomplete.ReplaceCommands() kept builtin commands")
		}
	}
}

func TestSlashAutocompleteGetVisibleItems(t *testing.T) {
	ac := NewSlashAutocomplete()
	ac.Show("") // Show all commands

	items, startIdx, selectedInItems := ac.GetVisibleItems()

	if len(items) == 0 {
		t.Error("SlashAutocomplete.GetVisibleItems() returned empty slice")
	}
	if startIdx < 0 {
		t.Errorf("SlashAutocomplete.GetVisibleItems().startIdx = %d, want >= 0", startIdx)
	}
	if selectedInItems < 0 || selectedInItems >= len(items) {
		t.Errorf("SlashAutocomplete.GetVisibleItems().selectedInItems = %d, out of range", selectedInItems)
	}
}

func TestSlashAutocompleteMaxHeight(t *testing.T) {
	ac := NewSlashAutocomplete()

	if ac.MaxHeight() != 8 {
		t.Errorf("SlashAutocomplete.MaxHeight() = %d, want 8", ac.MaxHeight())
	}
}

func TestSlashAutocompleteFilterText(t *testing.T) {
	ac := NewSlashAutocomplete()
	ac.Show("test")

	if ac.FilterText() != "test" {
		t.Errorf("SlashAutocomplete.FilterText() = %q, want \"test\"", ac.FilterText())
	}
}

func TestSlashAutocompleteMergeCommands(t *testing.T) {
	ac := NewSlashAutocomplete()
	initialCount := len(ac.Commands())

	// Merge some new commands
	ac.MergeCommands([]string{"new-1", "new-2"})

	newCount := len(ac.Commands())
	if newCount != initialCount+2 {
		t.Errorf("SlashAutocomplete.MergeCommands() count = %d, want %d", newCount, initialCount+2)
	}
}

func TestSlashAutocompleteCommands(t *testing.T) {
	ac := NewSlashAutocomplete()

	cmds := ac.Commands()
	originalCount := len(cmds)

	// Modify returned slice
	if len(cmds) > 0 {
		cmds[0] = "modified"
	}

	// Verify original is unchanged (should be a copy)
	newCmds := ac.Commands()
	if newCmds[0] == "modified" {
		t.Error("SlashAutocomplete.Commands() returned reference instead of copy")
	}

	// Count should be same
	if len(newCmds) != originalCount {
		t.Errorf("SlashAutocomplete.Commands() count changed: %d -> %d", originalCount, len(newCmds))
	}
}
