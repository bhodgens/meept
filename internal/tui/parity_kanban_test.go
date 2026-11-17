package tui

import "testing"

// Kanban-specific parity pin (issue #144). The generic manifest-driven
// tests in parity_test.go cover the shared contract; this file pins the
// kanban board's specifics so a future manifest edit can't silently flip
// the board's tracked state:
//   - palette key 'b' = "kanban board", gui:true (flipped in 1a2ade63
//     after the GUI /kanban route + command_palette.dart entry landed;
//     Dart pins: ui/flutter_ui/test/kanban/kanban_nav_test.dart).
//   - deviation "kanban:interaction-model" documents why per-view keys
//     (h/l move, t takeover, a assign, c comment) are NOT in shortcuts[].
//   - shortcuts[] is global bindings only — it must not grow board
//     per-view keys.
func TestParityKanbanPalette(t *testing.T) {
	inv := loadGUIInventory(t)

	// (1) palette contains key 'b' = "kanban board" with gui:true.
	var found bool
	for _, pc := range inv.PaletteCommands {
		if pc.Key != "b" {
			continue
		}
		found = true
		if pc.Label != "kanban board" {
			t.Errorf("palette key 'b': label %q, want %q", pc.Label, "kanban board")
		}
		if !pc.GUI {
			t.Errorf("palette key 'b' (kanban board): gui=false, want gui=true (GUI /kanban route exists; see deviations[\"palette:kanban board\"])")
		}
	}
	if !found {
		t.Fatal("palette_commands has no entry with key 'b' — kanban board affordance missing from the parity manifest")
	}

	// (2) deviations map contains 'kanban:interaction-model'.
	if just, ok := inv.Deviations["kanban:interaction-model"]; !ok {
		t.Error("deviations missing \"kanban:interaction-model\" — the surface-model deviation (TUI keyboard-driven board vs GUI mouse/touch-first) must stay documented")
	} else if just == "" {
		t.Error("deviations[\"kanban:interaction-model\"] is empty — the justification is the point")
	}

	// (3) shortcuts[] carries NO per-view board keys (global-bindings-only rule).
	perView := map[string]bool{"h": true, "l": true, "t": true, "a": true, "c": true}
	for _, sc := range inv.Shortcuts {
		if perView[sc.Key] {
			t.Errorf("shortcuts[] contains per-view board key %q (%s) — shortcuts[] is global bindings only; per-view keys belong in deviations[\"kanban:interaction-model\"] (sessions j/k precedent)", sc.Key, sc.Action)
		}
	}
}
