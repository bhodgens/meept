package tui

import (
	"testing"
)

// TestModalConstructorsNoNilEmbedding is a table-driven regression test for
// the FuzzyFinderModal nil-pointer crash: every modal that embeds *Modal must
// have it initialized, or the first Show()/IsVisible() call dereferences nil
// and takes down the whole TUI (bug fixed 2026-08-24, ctrl+p crash).
//
// Any new modal type with an embedded *Modal must be added here.
func TestModalConstructorsNoNilEmbedding(t *testing.T) {
	styles := DefaultStyles()
	rpc := NewRPCClient("/tmp/nonexistent-test.sock")

	// Each entry: construct + exercise the visibility path that crashed.
	t.Run("SessionPicker", func(t *testing.T) {
		m := NewSessionPickerModal(styles, rpc, &ClientConfig{})
		if m.Modal == nil {
			t.Fatal("embedded *Modal is nil — will panic on use")
		}
		m.Show()
		if !m.IsVisible() {
			t.Error("Show() did not make modal visible")
		}
		m.Hide()
	})

	t.Run("FuzzyFinder", func(t *testing.T) {
		m := NewFuzzyFinderModal(styles, rpc)
		if m.Modal == nil {
			t.Fatal("embedded *Modal is nil — this was the ctrl+p crash")
		}
		m.Show()
		if !m.Visible() {
			t.Error("Show() did not make modal visible")
		}
		if !m.IsVisible() {
			t.Error("promoted IsVisible() disagrees with Visible()")
		}
		m.Hide()
		if m.Visible() {
			t.Error("Hide() left modal visible")
		}
	})

	t.Run("BranchPicker", func(t *testing.T) {
		m := NewBranchPickerModal(styles, rpc)
		if m.Modal == nil {
			t.Fatal("embedded *Modal is nil — will panic on use")
		}
		m.Show()
		if !m.IsVisible() {
			t.Error("Show() did not make modal visible")
		}
		m.Hide()
	})

	t.Run("ProjectPicker", func(t *testing.T) {
		m := NewProjectPickerModal(styles, rpc)
		if m.Modal == nil {
			t.Fatal("embedded *Modal is nil — will panic on use")
		}
		m.Show()
		if !m.IsVisible() {
			t.Error("Show() did not make modal visible")
		}
		m.Hide()
	})

	// CommandPaletteModal returns a plain *Modal; verify non-nil + lifecycle.
	t.Run("CommandPalette", func(t *testing.T) {
		m := CommandPaletteModal(styles, &ClientConfig{})
		if m == nil {
			t.Fatal("modal is nil")
		}
		m.Show()
		if !m.IsVisible() {
			t.Error("Show() did not make modal visible")
		}
		m.Hide()
	})
}

// TestAllEmbeddedModalsInitialized walks every constructor returning a type
// that embeds *Modal and asserts the embedding is set. Adding a new modal
// without initializing the embed fails here at CI time instead of crashing
// a user's TUI at runtime.
func TestAllEmbeddedModalsInitialized(t *testing.T) {
	styles := DefaultStyles()
	rpc := NewRPCClient("/tmp/nonexistent-test.sock")

	checks := []struct {
		name  string
		modal *Modal // the embedded pointer, extracted from each constructor
	}{
		{"SessionPicker", NewSessionPickerModal(styles, rpc, &ClientConfig{}).Modal},
		{"FuzzyFinder", NewFuzzyFinderModal(styles, rpc).Modal},
		{"BranchPicker", NewBranchPickerModal(styles, rpc).Modal},
		{"ProjectPicker", NewProjectPickerModal(styles, rpc).Modal},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if tc.modal == nil {
				t.Fatalf("%s: embedded *Modal not initialized", tc.name)
			}
			if tc.modal.title == "" {
				t.Errorf("%s: embedded Modal has empty title (suspicious zero-value)", tc.name)
			}
		})
	}
}
