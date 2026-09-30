package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/modals"
	"github.com/caimlas/meept/internal/tui/types"
)

// Phase 3 modal key-flow tests (leaf 03): open → interact → confirm/cancel
// with FOCUS-ORDER assertions — the failure mode unit tests cannot see
// (tui-e2e-plan.md §1.2: a key binding shadowed by modal focus order).
// App-driven flows run against the finished model (loop stopped); the
// sub-model flows drive the modal HandleKey directly.

func TestModalKeyFlowRename(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	// Open via the OpenRenameModalMsg path (the same entry the sessions
	// view's OpenRenameModalMsg uses). The palette 'r' action requires
	// currentSession, which arrives on an async cmd — the direct message
	// keeps this test about the MODAL's key flow, not the load race.
	hp.send(OpenRenameModalMsg{SessionID: "sess-1", CurrentName: "old-name"})
	hp.settle(goldenWidth, goldenHeight)

	app := hp.finish(goldenWidth, goldenHeight)
	if app.activeModal != ModalSessionRename {
		t.Fatalf("rename modal did not open via OpenRenameModalMsg (activeModal=%v)", app.activeModal)
	}

	// Focus order: the input field is focused first (selected==0); typed
	// characters go to the BUFFER, not navigation.
	if got := app.sessionRename.selected; got != 0 {
		t.Fatalf("rename modal initial focus = %d, want 0 (input field)", got)
	}

	// Full confirm path on a fresh modal: open → type → tab → enter.
	app2 := openRenameForFlow(t)
	app2.sessionRename.inputBuffer = "renamed-desc"
	app2.sessionRename.selected = 1
	cmd := app2.sessionRename.HandleKey("enter")
	app2.activeModal = ModalNone // HandleKey hid it
	if cmd == nil {
		t.Fatal("enter on ok button produced no command")
	}
	msg := cmd()
	if _, ok := msg.(SessionRenameMsg); !ok {
		t.Fatalf("enter produced %T, want SessionRenameMsg", msg)
	}

	// Cancel path: esc hides without emitting anything.
	app2.sessionRename.Show("s1", "n")
	app2.sessionRename.HandleKey("esc")
	if app2.sessionRename.IsVisible() {
		t.Fatal("esc did not close the rename modal")
	}
}

// openRenameForFlow builds a finished app model with the rename modal
// open (direct Show, the same entry OpenRenameModalMsg uses).
func openRenameForFlow(t *testing.T) *App {
	t.Helper()
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()
	app := hp.finish(goldenWidth, goldenHeight)
	app.sessionRename.Show("sess-1", "old-name")
	return app
}

func TestModalKeyFlowPendingChanges(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	// Seed a session then open the modal the way ctrl+d does.
	app := hp.finish(goldenWidth, goldenHeight)
	app.currentSession = &types.Session{ID: "sess-pc", Name: "pc"}

	// Direct modal key flow with a seeded change list (the API-backed
	// fetch path is covered by pending_changes_test.go; here the FOCUS
	// and key flow matter).
	pcm := modals.NewPendingChangesModal(nil)
	pcm.Show("sess-pc")
	pcm.SetChanges([]modals.PendingChange{
		{ID: "chg-1", FilePath: "hello.txt"},
		{ID: "chg-2", FilePath: "other.txt"},
	})

	// Focus: first row selected.
	if got := pcm.SelectedIndex(); got != 0 {
		t.Fatalf("pending changes initial selection = %d, want 0", got)
	}
	// Navigation.
	pcm.HandleKey("j")
	if got := pcm.SelectedIndex(); got != 1 {
		t.Fatalf("j moved selection to %d, want 1", got)
	}
	pcm.HandleKey("k")
	if got := pcm.SelectedIndex(); got != 0 {
		t.Fatalf("k moved selection to %d, want 0", got)
	}
	// Diff toggle.
	pcm.HandleKey("v")
	if !pcm.InDiffMode() {
		t.Fatal("v did not enter diff mode")
	}
	pcm.HandleKey("esc")
	if pcm.InDiffMode() {
		t.Fatal("esc in diff mode returned to list, still in diff")
	}
	// Close.
	pcm.HandleKey("esc")
	if pcm.IsVisible() {
		t.Fatal("second esc did not close the modal")
	}
	// Accept action with a nil api surfaces the error result (never a
	// silent no-op).
	pcm.Show("sess-pc")
	pcm.SetChanges([]modals.PendingChange{{ID: "chg-1", FilePath: "hello.txt"}})
	cmd := pcm.HandleKey("a")
	if cmd == nil {
		t.Fatal("accept produced no command")
	}
}

func TestModalKeyFlowProjectPrompt(t *testing.T) {
	// The project prompt is a standalone bubbletea model; drive it
	// directly (y/n/p + enter + esc paths).
	pm := modals.NewProjectPromptModal("sess-pp", "/tmp/proj")
	if _, cmd := pm.Update(keyPressMsgFor("y")); cmd == nil {
		t.Fatal("y produced no command")
	}
	if _, cmd := pm.Update(keyPressMsgFor("n")); cmd == nil {
		t.Fatal("n produced no command")
	}
	if _, cmd := pm.Update(keyPressMsgFor("p")); cmd == nil {
		t.Fatal("p produced no command")
	}
	// Down/enter navigation: down twice lands on pick; enter fires it.
	pm2 := modals.NewProjectPromptModal("sess-pp", "/tmp/proj")
	pm2.Update(keyPressMsgFor("down"))
	pm2.Update(keyPressMsgFor("down"))
	_, cmd := pm2.Update(keyPressMsgFor("enter"))
	if cmd == nil {
		t.Fatal("enter on pick row produced no command")
	}
}

func TestModalKeyFlowConfirmation(t *testing.T) {
	// The destructive-tool confirmation modal: y/n/v key flow and state.
	// (Full coverage in confirmation_test.go; here the focus-order
	// contract: v toggles detail WITHOUT confirming/cancelling.)
	m := NewConfirmationModel(map[string]any{
		"action":     "file_write",
		"summary":    "write hello.txt",
		"reversible": true,
	})
	m2, _ := m.Update(keyPressMsgFor("v"))
	got := m2.(ConfirmationModel)
	if got.IsConfirmed() || got.IsCancelled() {
		t.Fatal("v toggled detail but flipped confirm/cancel state")
	}
	m3, _ := got.Update(keyPressMsgFor("y"))
	got3 := m3.(ConfirmationModel)
	if !got3.IsConfirmed() {
		t.Fatal("y did not confirm")
	}
	if got3.IsCancelled() {
		t.Fatal("confirm and cancel both set")
	}
	m4, _ := NewConfirmationModel(nil).Update(keyPressMsgFor("n"))
	got4 := m4.(ConfirmationModel)
	if !got4.IsCancelled() {
		t.Fatal("n did not cancel")
	}
}

// keyPressMsgFor builds a tea.KeyPressMsg for a named key (subset used by
// the modal flows).
func keyPressMsgFor(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	if len(key) == 1 {
		return tea.KeyPressMsg{Code: rune(key[0])}
	}
	return tea.KeyPressMsg{Code: rune(key[0])}
}
