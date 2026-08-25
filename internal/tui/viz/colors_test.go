package viz

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/theme"
)

// hexOf renders a color.Color back to its #RRGGBB form.
func hexOf(t *testing.T, c interface{ RGBA() (r, g, b, a uint32) }) string {
	t.Helper()
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8&0xff, g>>8&0xff, b>>8&0xff)
}

func TestVizSetPaletteMidnight(t *testing.T) {
	// Restore the historical defaults so other viz tests are unaffected.
	defer func() {
		ColorIdle = lipgloss.Color("#E5E7EB")
		ColorWorking = lipgloss.Color("#F97316")
		ColorSuccess = lipgloss.Color("#10B981")
		ColorMuted = lipgloss.Color("#6B7280")
		ColorCarrying = lipgloss.Color("#3B82F6")
		ColorError = lipgloss.Color("#EF4444")
		ColorDispatcher = lipgloss.Color("#F59E0B")
		ColorDotLine = lipgloss.Color("#374151")
	}()

	if err := SetPalette("midnight"); err != nil {
		t.Fatalf("SetPalette(midnight): %v", err)
	}

	// Spot-check against tokens.json5: primary→Working, info→Carrying,
	// border→DotLine.
	tokens := theme.MustParse(theme.TokensJSON5)
	checks := map[string]struct {
		got  interface{ RGBA() (r, g, b, a uint32) }
		role string
	}{
		"ColorWorking":    {ColorWorking, "primary"},
		"ColorCarrying":   {ColorCarrying, "info"},
		"ColorDispatcher": {ColorDispatcher, "warning"},
		"ColorDotLine":    {ColorDotLine, "border"},
	}
	for label, tc := range checks {
		if want := tokens.Hex("midnight", tc.role); hexOf(t, tc.got) != want {
			t.Errorf("%s = %s, want %s (midnight.%s)",
				label, hexOf(t, tc.got), want, tc.role)
		}
	}

	snap := Current()
	if got := hexOf(t, snap.Working); got != "#7DC4FF" {
		t.Errorf("snapshot Working = %q, want #7DC4FF", got)
	}
}

func TestVizSetPaletteUnknownReturnsError(t *testing.T) {
	err := SetPalette("no-such-theme")
	if err == nil {
		t.Fatal("expected error for unknown theme name")
	}
	for _, valid := range theme.FrozenVariants {
		if !strings.Contains(err.Error(), valid) {
			t.Errorf("error %q does not mention valid name %q", err.Error(), valid)
		}
	}
}
