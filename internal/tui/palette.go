package tui

import (
	"fmt"
	"image/color"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/theme"
)

// Palette is the resolved role set for one theme variant. Every field is a
// finished color ready to hand to lipgloss styles. Roles mirror the frozen
// role list in theme.FrozenRoles; see theme/tokens.json5 for the values.
type Palette struct {
	Primary       color.Color // brand orange (titles, highlights, focus)
	PrimaryBright color.Color // hover/bright variant of primary
	PrimaryDark   color.Color // pressed/dark variant of primary
	PrimaryGlow   color.Color // glow/halo around primary elements
	Accent        color.Color // secondary highlight (amber family)
	Secondary     color.Color // supporting green
	Success       color.Color // success states
	Warning       color.Color // warning states
	ErrorC        color.Color // error states (Styles.Error takes the short name)
	Info          color.Color // informational accents (blue family)
	Background    color.Color // app background
	Surface       color.Color // panel/card surface
	SurfaceAlt    color.Color // alternate surface (nested panels, rows)
	Border        color.Color // borders and dividers
	TextPrimary   color.Color // primary text
	TextMuted     color.Color // muted text
	TerminalGreen color.Color // terminal-style green output
	TerminalAmber color.Color // terminal-style amber output
}

// The palette state is written by SetPalette before the bubbletea program
// starts (NewApp) and is intentionally not synchronized afterwards: themes
// are restart-applied, there is no hot reload, and styles are read from the
// program goroutine only.
var (
	paletteOnce     sync.Once
	paletteTokens   theme.Tokens
	paletteParseErr error

	// Initialized as a var initializer (not in init()): styles.go's Color*
	// vars call Current(), and Go initializes same-package vars in
	// dependency order, guaranteeing currentPalette exists first. An init()
	// function would run after all var initializers and nil-panic.
	currentPalette = newDefaultPalette()
)

// newDefaultPalette returns the built-in fallback palette: the historical
// styles.go literals for the ten legacy roles, plus conservative companions
// for the eight roles the legacy block never had.
func newDefaultPalette() *Palette {
	return &Palette{
		Primary:       lipgloss.Color("#F97316"), // orange
		PrimaryBright: lipgloss.Color("#FB923C"), // orange-400
		PrimaryDark:   lipgloss.Color("#C2410C"), // orange-700
		PrimaryGlow:   lipgloss.Color("#FDBA74"), // orange-300
		Accent:        lipgloss.Color("#F59E0B"), // amber
		Secondary:     lipgloss.Color("#10B981"), // green
		Success:       lipgloss.Color("#10B981"), // green
		Warning:       lipgloss.Color("#F59E0B"), // amber
		ErrorC:        lipgloss.Color("#EF4444"), // red
		Info:          lipgloss.Color("#3B82F6"), // blue
		Background:    lipgloss.Color("#1F2937"), // dark gray
		Surface:       lipgloss.Color("#111827"), // gray-900
		SurfaceAlt:    lipgloss.Color("#1F2937"), // gray-800
		Border:        lipgloss.Color("#374151"), // medium gray
		TextPrimary:   lipgloss.Color("#E5E7EB"), // light gray
		TextMuted:     lipgloss.Color("#6B7280"), // gray
		TerminalGreen: lipgloss.Color("#10B981"), // green
		TerminalAmber: lipgloss.Color("#F59E0B"), // amber
	}
}

// loadTokens parses the embedded theme tokens exactly once. Both the parse
// result and any parse error are cached; SetPalette surfaces the error.
func loadTokens() (theme.Tokens, error) {
	paletteOnce.Do(func() {
		tokens, err := theme.Parse(theme.TokensJSON5)
		paletteTokens, paletteParseErr = tokens, err
	})
	return paletteTokens, paletteParseErr
}

// PaletteNames lists the selectable theme variants.
func PaletteNames() []string {
	names := make([]string, len(theme.FrozenVariants))
	copy(names, theme.FrozenVariants)
	return names
}

// buildPalette resolves one validated variant into a Palette. Only variants
// in theme.FrozenVariants are selectable: theme.Parse validates exactly that
// set, so anything else in the token file is unvetted.
func buildPalette(tokens theme.Tokens, name string) (*Palette, error) {
	frozen := false
	for _, v := range theme.FrozenVariants {
		if v == name {
			frozen = true
			break
		}
	}
	if !frozen {
		return nil, fmt.Errorf("tui: unknown ui theme %q (valid: %s)",
			name, strings.Join(PaletteNames(), ", "))
	}
	roles, ok := tokens[name]
	if !ok {
		// Unreachable while theme.Parse validates the frozen variants, kept
		// as a guard against future parser changes.
		return nil, fmt.Errorf("tui: ui theme %q missing from tokens", name)
	}
	get := func(role string) color.Color {
		// Presence is guaranteed by theme.Parse validation.
		return lipgloss.Color(roles[role])
	}
	return &Palette{
		Primary:       get("primary"),
		PrimaryBright: get("primaryBright"),
		PrimaryDark:   get("primaryDark"),
		PrimaryGlow:   get("primaryGlow"),
		Accent:        get("accent"),
		Secondary:     get("secondary"),
		Success:       get("success"),
		Warning:       get("warning"),
		ErrorC:        get("error"),
		Info:          get("info"),
		Background:    get("background"),
		Surface:       get("surface"),
		SurfaceAlt:    get("surfaceAlt"),
		Border:        get("border"),
		TextPrimary:   get("textPrimary"),
		TextMuted:     get("textMuted"),
		TerminalGreen: get("terminalGreen"),
		TerminalAmber: get("terminalAmber"),
	}, nil
}

// SetPalette switches the active theme variant by name and repoints the
// package-level Color* style vars to match. Call it once at startup (see
// NewApp) before building styles; unknown names return an error listing the
// valid ones and leave the current palette untouched.
func SetPalette(name string) error {
	tokens, err := loadTokens()
	if err != nil {
		return fmt.Errorf("tui: load theme tokens: %w", err)
	}
	pal, err := buildPalette(tokens, name)
	if err != nil {
		return err
	}
	currentPalette = pal
	applyPalette(pal)
	return nil
}

// Current returns the active palette. Without a prior SetPalette call this
// is the built-in default palette.
func Current() *Palette {
	if currentPalette == nil {
		// Defensive: only reachable if initialization order ever changes.
		currentPalette = newDefaultPalette()
	}
	return currentPalette
}

// applyPalette repoints the exported legacy Color* vars so the hundreds of
// existing references across the TUI and cmd packages pick up the active
// palette without any call-site changes.
func applyPalette(p *Palette) {
	ColorPrimary = p.Primary
	ColorSecondary = p.Secondary
	ColorAccent = p.Accent
	ColorError = p.ErrorC
	ColorWarning = p.Warning
	ColorSuccess = p.Success
	ColorMuted = p.TextMuted
	ColorForeground = p.TextPrimary
	ColorBackground = p.Background
	ColorBorder = p.Border
}
