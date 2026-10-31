package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/models"
)

// Activity-view wiring tests (leaf 03 T1): the ViewActivity keybinding (via
// the command palette), the hide-tail hook on view switch, and the tab
// presence. The model-level behavior lives in internal/tui/models.

// TestActivityView_PaletteKeySwitchesView: ctrl+x then 'a' lands on
// ViewActivity (default binding "a").
func TestActivityView_PaletteKeySwitchesView(t *testing.T) {
	app := createTestApp()

	app.activeModal = ModalCommandPalette
	app.commandPalette.Show()

	msg := tea.KeyPressMsg{Code: 'a', Text: "a"}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.currentView != ViewActivity {
		t.Errorf("expected ViewActivity, got %d", newApp.currentView)
	}
	if newApp.activeModal != ModalNone {
		t.Error("expected modal to be closed after selection")
	}
}

// TestActivityView_EmptyBindingDisabled: with view_activity = "" the palette
// action is inert (the "" case must not panic on an unkeyed switch arm).
func TestActivityView_EmptyBindingDisabled(t *testing.T) {
	app := createTestApp()
	app.clientConfig.Keybindings.CommandPalette.ViewActivity = ""
	app.commandPalette = CommandPaletteModal(DefaultStyles(), app.clientConfig)

	app.activeModal = ModalCommandPalette
	app.commandPalette.Show()

	msg := tea.KeyPressMsg{Code: 'a', Text: "a"}
	newModel, _ := app.Update(msg)
	newApp := newModel.(*App)

	if newApp.currentView == ViewActivity {
		t.Error("empty binding must not switch to the activity view")
	}
}

// TestActivityView_HideStopsTail: leaving the activity view delivers
// hideActivityViewMsg so the polling ticker stops (no background churn).
func TestActivityView_HideStopsTail(t *testing.T) {
	app := createTestApp()
	if app.activity == nil {
		t.Fatal("app.activity not wired in createTestApp")
	}

	// Arm the tail directly (the model owns the poller).
	app.currentView = ViewActivity
	if cmd := app.activity.Update(tea.KeyPressMsg{Code: 't', Text: "t"}); cmd == nil {
		t.Fatal("tail toggle returned no cmd")
	}
	if !app.activity.TailOn() {
		t.Fatal("tail not armed")
	}

	// Switch away through the palette action path the app itself uses:
	// any view change must hide the tail.
	app.currentView = ViewSessions
	app.activity.Update(models.HideActivityView())

	if app.activity.TailOn() {
		t.Fatal("tail still on after leaving the activity view")
	}
	// The tick that was armed before the switch must not poll.
	if cmd := app.activity.Update(models.ActivityTickMsg{}); cmd != nil {
		t.Fatal("tick while tail off returned a poll cmd")
	}
}

// TestActivityView_TabPresent: the tab bar carries the activity tab.
func TestActivityView_TabPresent(t *testing.T) {
	app := createTestApp()
	tabs := stripANSIForInventory(app.renderTabs())
	if !strings.Contains(tabs, "activity") {
		t.Errorf("expected tabs to contain \"activity\"; tabs: %q", tabs)
	}
}

// TestActivityView_StatusBarQuickActions: the activity view advertises its
// own quick actions in the status bar.
func TestActivityView_StatusBarQuickActions(t *testing.T) {
	app := createTestApp()
	app.currentView = ViewActivity
	bar := stripANSIForInventory(app.renderStatusBar())
	for _, want := range []string{"sort mode", "live tail", "findings + drift"} {
		if !strings.Contains(bar, want) {
			t.Errorf("status bar missing activity quick action %q; bar: %q", want, bar)
		}
	}
}

// TestActivityView_ModelInitFetches: initCurrentView on ViewActivity issues
// the initial fetch command.
func TestActivityView_ModelInitFetches(t *testing.T) {
	app := createTestApp()
	app.currentView = ViewActivity
	if cmd := app.initCurrentView(); cmd == nil {
		t.Fatal("initCurrentView returned no cmd for ViewActivity")
	}
}

// TestActivityView_ViewSwitchStopsTail: the app (not the test) must deliver
// the hide message. Arming the tail and then switching to another view
// through initCurrentView - the choke point every palette/tab switch calls -
// stops the poller. The hand-delivered variant above cannot catch a missing
// app-level hook; this one can.
func TestActivityView_ViewSwitchStopsTail(t *testing.T) {
	app := createTestApp()
	if app.activity == nil {
		t.Fatal("app.activity not wired in createTestApp")
	}

	// Arm the tail while the activity view is current.
	app.currentView = ViewActivity
	if cmd := app.activity.Update(tea.KeyPressMsg{Code: 't', Text: "t"}); cmd == nil {
		t.Fatal("tail toggle returned no cmd")
	}
	if !app.activity.TailOn() {
		t.Fatal("tail not armed")
	}

	// Switch away the way the app does it: reassign the view, then
	// initialize it. No hand-delivered hide message.
	app.currentView = ViewSessions
	app.initCurrentView()

	if app.activity.TailOn() {
		t.Fatal("app did not stop the live tail on view switch")
	}
}

var _ = time.Now // time import anchor for future timing assertions
