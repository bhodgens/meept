package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"

	"github.com/charmbracelet/x/ansi"
)

// TUI/GUI parity inventory test (tui-e2e-plan.md §7, leaf 01): the TUI's
// palette command list, DefaultKeyMap shortcuts, and status-bar element
// inventory are compared against a checked-in manifest
// (internal/tui/testdata/gui_inventory.json) that mirrors the Flutter GUI
// (ui/flutter_ui). The GUI has no Go test runner, so the manifest is the
// shared contract: when a surface changes legitimately, update BOTH the
// code and the manifest in the same commit — the test failure IS the
// reminder to check the other surface (AGENTS.md parity rule).
//
// Manifest shape:
// {
//   "palette_commands": [ {"key","label","description","gui":true} ... ],
//   "shortcuts":       [ {"key","action","gui":true} ... ],
//   "status_elements": [ "..." ]
// }
// gui:false marks a documented TUI-only deviation (justification in the
// "deviations" map: item -> why the other surface does not carry it).

type guiInventory struct {
	PaletteCommands []struct {
		Key         string `json:"key"`
		Label       string `json:"label"`
		Description string `json:"description"`
		GUI         bool   `json:"gui"`
	} `json:"palette_commands"`
	Shortcuts []struct {
		Key    string `json:"key"`
		Action string `json:"action"`
		GUI    bool   `json:"gui"`
	} `json:"shortcuts"`
	StatusElements []string          `json:"status_elements"`
	Deviations     map[string]string `json:"deviations"`
}

func loadGUIInventory(t *testing.T) *guiInventory {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "gui_inventory.json"))
	if err != nil {
		t.Fatalf("read gui inventory manifest: %v", err)
	}
	var inv guiInventory
	if err := json.Unmarshal(raw, &inv); err != nil {
		t.Fatalf("parse gui inventory manifest: %v", err)
	}
	return &inv
}

// tuiPaletteItems extracts the live palette inventory from the same
// constructor the App uses (CommandPaletteModal) — the manifest comparison
// runs against real code, not a copy.
func tuiPaletteItems(t *testing.T) map[string]string {
	t.Helper()
	m := CommandPaletteModal(DefaultStyles(), DefaultClientConfig())
	items := make(map[string]string, len(m.items))
	for _, it := range m.items {
		items[it.Label] = it.Key
	}
	return items
}

func TestParityPaletteCommands(t *testing.T) {
	inv := loadGUIInventory(t)
	tui := tuiPaletteItems(t)

	var missingInTUI, missingInGUI []string
	for _, want := range inv.PaletteCommands {
		key, ok := tui[want.Label]
		if !ok {
			missingInTUI = append(missingInTUI, want.Label)
			continue
		}
		if want.GUI && key != want.Key {
			t.Errorf("palette %q: TUI key %q, manifest (GUI) key %q — surfaces diverged",
				want.Label, key, want.Key)
		}
	}
	for label := range tui {
		found := false
		for _, want := range inv.PaletteCommands {
			if want.Label == label {
				found = true
				break
			}
		}
		if !found {
			missingInGUI = append(missingInGUI, label)
		}
	}
	if len(missingInTUI) > 0 {
		t.Errorf("palette commands in manifest but NOT in the TUI (stale manifest or TUI regression): %v",
			missingInTUI)
	}
	if len(missingInGUI) > 0 {
		sort.Strings(missingInGUI)
		t.Errorf("palette commands in the TUI but NOT in the manifest — update the GUI "+
			"(ui/flutter_ui/lib/widgets/command_palette.dart) or document the deviation in "+
			"testdata/gui_inventory.json deviations: %v", missingInGUI)
	}
}

func TestParityShortcuts(t *testing.T) {
	inv := loadGUIInventory(t)

	// Live TUI shortcut inventory from DefaultKeyMap + the fixed global
	// handlers in Update (ctrl+s/t/v/p/d/b), keyed "key" -> "action".
	tui := map[string]string{
		"ctrl+x": "command palette",
		"ctrl+c": "quit",
		"ctrl+s": "steer / sessions",
		"ctrl+t": "toggle tts",
		"ctrl+m": "toggle markdown",
		"ctrl+v": "cycle verbosity",
		"ctrl+p": "fuzzy finder",
		"ctrl+d": "pending changes",
		"esc":    "cancel / back",
	}
	// KeyMap entries are the source of truth for the bound ones.
	km := DefaultKeyMap()
	tui[joinKeys(km.Quit)] = "quit"
	tui[joinKeys(km.Command)] = "command palette"
	tui[joinKeys(km.ToggleTTS)] = "toggle tts"
	tui[joinKeys(km.ToggleMarkdown)] = "toggle markdown"

	for _, want := range inv.Shortcuts {
		got, ok := tui[want.Key]
		if !ok {
			t.Errorf("shortcut %q (%s) in manifest but not bound in the TUI", want.Key, want.Action)
			continue
		}
		if want.GUI && got != want.Action {
			t.Errorf("shortcut %q: TUI action %q, manifest action %q", want.Key, got, want.Action)
		}
	}
	for key := range tui {
		found := false
		for _, want := range inv.Shortcuts {
			if want.Key == key {
				found = true
				break
			}
		}
		if !found {
			if just, ok := inv.Deviations["shortcut:"+key]; ok {
				_ = just // documented deviation
				continue
			}
			t.Errorf("TUI shortcut %q missing from the parity manifest — bind it in the GUI "+
				"or document the deviation (testdata/gui_inventory.json)", key)
		}
	}
}

func joinKeys(b key.Binding) string {
	keys := b.Keys()
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// TestParityStatusBarElements checks the manifest's status-element list
// against the elements renderStatusBar can emit today. The bar is
// contextual; the manifest lists the INVARIANT elements (always present)
// plus conditionals with their trigger documented in deviations.
func TestParityStatusBarElements(t *testing.T) {
	inv := loadGUIInventory(t)
	app := createTestApp()
	bar := stripANSIForInventory(app.renderStatusBar())

	for _, el := range inv.StatusElements {
		if !strings.Contains(bar, el) {
			t.Errorf("status bar missing parity element %q; bar: %q", el, bar)
		}
	}
}

// stripANSIForInventory removes ANSI escapes so substring checks run on
// plain text (app_test.go uses ansi.Strip for the same purpose).
func stripANSIForInventory(s string) string {
	return ansi.Strip(s)
}
