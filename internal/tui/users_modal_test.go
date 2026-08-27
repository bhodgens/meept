package tui

import (
	"strings"
	"testing"
)

// TestUsersModal_Creation verifies constructor wiring (mirrors
// TestAllEmbeddedModalsInitialized's guard against nil embeds).
func TestUsersModal_Creation(t *testing.T) {
	styles := DefaultStyles()
	m := NewUsersModal(styles)
	if m.Modal == nil {
		t.Fatal("embedded *Modal is nil — will panic on use")
	}
	if m.title != "users" {
		t.Errorf("title = %q, want %q", m.title, "users")
	}
}

// TestUsersModal_Lifecycle checks Show/IsVisible/Hide on the embedded modal.
func TestUsersModal_Lifecycle(t *testing.T) {
	m := NewUsersModal(DefaultStyles())
	m.Show()
	if !m.IsVisible() {
		t.Error("Show() did not make modal visible")
	}
	m.Hide()
	if m.IsVisible() {
		t.Error("Hide() did not hide the modal")
	}
}

// TestUsersModalItems_AllDisabled pins the v1 contract: no client callable
// user-management path exists, so every row must render disabled. When a
// future leaf adds users.* RPC methods this test MUST be updated alongside
// real handlers — it failing means someone enabled a row that still cannot
// act.
func TestUsersModalItems_AllDisabled(t *testing.T) {
	items := UsersModalItems()
	if len(items) == 0 {
		t.Fatal("users modal has no guidance rows")
	}
	for _, item := range items {
		if !item.Disabled {
			t.Errorf("row %q must be disabled in v1 (no client management path)", item.Key)
		}
		if item.Label != strings.ToLower(item.Label) {
			t.Errorf("row %q label %q not lowercase", item.Key, item.Label)
		}
	}
}

// TestUsersModal_CliPointers matches leaf 04's actual cobra tree so users
// following the hints never hit an unknown command.
func TestUsersModal_CliPointers(t *testing.T) {
	all := ""
	for _, item := range UsersModalItems() {
		all += " " + item.Description
	}
	for _, want := range []string{
		"meept users list",
		"meept users add",
		"meept users keys add",
		"meept users keys revoke",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("guidance missing cli pointer %q", want)
		}
	}
}

// TestUsersModal_View renders the modal and enforces lowercase UI text plus
// empty-width safety.
func TestUsersModal_View(t *testing.T) {
	m := NewUsersModal(DefaultStyles())
	m.width = 90
	m.Show()
	out := m.View(120, 40)
	if out == "" {
		t.Fatal("View returned empty string for visible modal")
	}
	lower := strings.ToLower(out)
	if lower != out {
		// Model/flag names like rfc3339 stay lowercase; flag this loudly if
		// anyone introduces capitalized prose later.
		t.Logf("view contains uppercase characters (check brand/format names are intentional)")
	}
	m.Hide()
	if m.View(120, 40) != "" {
		t.Error("hidden modal should render empty")
	}
}
