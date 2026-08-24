package tui

import (
	"fmt"
	"strings"
	"testing"
)

// hexOf renders a color.Color back to its #RRGGBB form so tests can compare
// palette values against token literals.
func hexOf(t *testing.T, c interface{ RGBA() (r, g, b, a uint32) }) string {
	t.Helper()
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8&0xff, g>>8&0xff, b>>8&0xff)
}

func TestPaletteNamesListsFrozenVariants(t *testing.T) {
	names := PaletteNames()
	want := []string{"cyberpunk", "midnight", "solarized"}
	if len(names) != len(want) {
		t.Fatalf("PaletteNames() = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("PaletteNames()[%d] = %q, want %q", i, names[i], n)
		}
	}
}

func TestSetPaletteValidVariants(t *testing.T) {
	// Restore the built-in default so this test does not leak a theme into
	// other tests in the package.
	defer func() {
		if err := SetPalette("cyberpunk"); err != nil {
			t.Errorf("restore cyberpunk: %v", err)
		}
	}()

	for _, name := range PaletteNames() {
		before := Current()
		if err := SetPalette(name); err != nil {
			t.Fatalf("SetPalette(%q): %v", name, err)
		}
		if Current() == before && name != "cyberpunk" {
			// cyberpunk may legitimately equal the previous state; every
			// other switch must produce a distinct palette pointer.
			t.Errorf("SetPalette(%q) did not change the active palette", name)
		}
		if got := Current(); got == nil {
			t.Fatalf("Current() is nil after SetPalette(%q)", name)
		}
	}
}

func TestSetPaletteUnknownReturnsErrorListingNames(t *testing.T) {
	err := SetPalette("no-such-theme")
	if err == nil {
		t.Fatal("expected error for unknown theme name")
	}
	for _, valid := range PaletteNames() {
		if !strings.Contains(err.Error(), valid) {
			t.Errorf("error %q does not mention valid name %q", err.Error(), valid)
		}
	}
}

func TestSetPaletteMidnightChangesPrimaryAndVars(t *testing.T) {
	// Restore so later tests see the built-in default.
	defer func() {
		if err := SetPalette("cyberpunk"); err != nil {
			t.Errorf("restore cyberpunk: %v", err)
		}
	}()

	if err := SetPalette("midnight"); err != nil {
		t.Fatalf("SetPalette(midnight): %v", err)
	}

	pal := Current()
	if pal == nil {
		t.Fatal("Current() is nil after SetPalette(midnight)")
	}
	const wantPrimary = "#7DC4FF"
	if got := hexOf(t, pal.Primary); got != wantPrimary {
		t.Errorf("midnight Primary = %q, want %q", got, wantPrimary)
	}

	// The exported legacy vars must follow the active palette.
	if got := hexOf(t, ColorPrimary); got != wantPrimary {
		t.Errorf("ColorPrimary = %q, want %q", got, wantPrimary)
	}

	// DefaultStyles must pick up the new palette through the repointed var.
	title := DefaultStyles().Title.Render("probe")
	// #7DC4FF = r 125, g 196, b 255. Matched without the leading "\x1b["
	// because lipgloss merges bold+foreground into a single SGR sequence
	// ("\x1b[1;38;2;...m").
	const wantANSI = "38;2;125;196;255m"
	if !strings.Contains(title, wantANSI) {
		t.Errorf("Title render missing midnight primary sequence %q; got %q",
			wantANSI, title)
	}
}
