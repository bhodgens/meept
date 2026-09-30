package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Phase 3 keybinding override tests (leaf 03): an override in
// client.json5 remaps a default palette key and the model responds to the
// NEW binding (config.go KeybindingsConfig → CommandPaletteModal items →
// app.go handleModalKey dispatch). The App resolves client.json5 at
// construction; tests construct the App the way NewApp does (loadClientConfig
// equivalent) by building a ClientConfig with the override applied.

// TestKeybindingOverrideRemapsPaletteAction: with
// keybindings.command_palette.view_chat = "k" (default "c"), ctrl+x then
// 'k' switches to the chat view and 'c' no longer does.
func TestKeybindingOverrideRemapsPaletteAction(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()

	// Apply the override the way LoadClientConfig would have: remap the
	// resolved config and rebuild the palette from it (NewApp wires
	// clientConfig into the modal at construction; the override test
	// replays that wiring with the remapped value).
	hp.app.clientConfig.Keybindings.CommandPalette.ViewChat = "k"
	hp.app.commandPalette = CommandPaletteModal(DefaultStyles(), hp.app.clientConfig)

	// Baseline: switch to a NON-chat view so the remap has observable
	// effect.
	hp.send(keyPress('x', tea.ModCtrl))
	hp.send(keyPress('s', 0)) // sessions view
	settleAsync()
	if hp.app.ActiveView() != ViewSessions {
		t.Fatalf("baseline: expected sessions view, got %v", hp.app.ActiveView())
	}

	// New binding 'k' opens the palette and returns to chat.
	hp.send(keyPress('x', tea.ModCtrl))
	hp.send(keyPress('k', 0))
	settleAsync()
	app := hp.finish(goldenWidth, goldenHeight)
	if app.ActiveView() != ViewChat {
		t.Errorf("remapped palette key 'k' did not switch to chat: view=%v", app.ActiveView())
	}

	// The OLD default key no longer triggers the action: switch away,
	// then ctrl+x 'c' must NOT return to chat.
	hp2 := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()
	hp2.app.clientConfig.Keybindings.CommandPalette.ViewChat = "k"
	hp2.app.commandPalette = CommandPaletteModal(DefaultStyles(), hp2.app.clientConfig)
	hp2.send(keyPress('x', tea.ModCtrl))
	hp2.send(keyPress('s', 0))
	settleAsync()
	hp2.send(keyPress('x', tea.ModCtrl))
	hp2.send(keyPress('c', 0))
	settleAsync()
	app2 := hp2.finish(goldenWidth, goldenHeight)
	if app2.ActiveView() != ViewSessions {
		t.Errorf("old default key 'c' still acts after the override: view=%v", app2.ActiveView())
	}
}

// TestKeybindingOverrideNewSession: remapping new_session exercises the
// action-dispatch table (createSession cmd).
func TestKeybindingOverrideNewSession(t *testing.T) {
	hp := newHeadlessApp(t, goldenWidth, goldenHeight)
	settleAsync()
	hp.app.clientConfig.Keybindings.CommandPalette.NewSession = "z"
	hp.app.commandPalette = CommandPaletteModal(DefaultStyles(), hp.app.clientConfig)

	hp.send(keyPress('x', tea.ModCtrl))
	hp.send(keyPress('z', 0))
	settleAsync()
	// createSession issues the RPC; the status message updates via the
	// async SessionLoadedMsg. Finish and assert no panic + palette closed
	// (the action consumed the key).
	app := hp.finish(goldenWidth, goldenHeight)
	if app.ActiveModal() != ModalNone {
		t.Errorf("palette still open after executing the overridden new-session action")
	}
}
