package palette

import (
	"image/color"
	"sync"

	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/theme"
)

// The components and prompts sub-packages cannot import the parent tui
// package (tui imports them), so they resolve theme roles directly from the
// embedded tokens via this bridge — mirroring models/palette_bridge.go.
// Like the parent palette, themes are restart-applied: the tokens are parsed
// once and the active variant is fixed at process start, so the once-guarded
// lookup needs no synchronization afterwards.
var (
	roleOnce  sync.Once
	roleMap   map[string]string
	roleParse error
)

func loadRoles() (map[string]string, error) {
	roleOnce.Do(func() {
		tokens, err := theme.Parse(theme.TokensJSON5)
		if err != nil {
			roleParse = err
			return
		}
		roles, ok := tokens[theme.FrozenVariants[0]]
		if !ok {
			roleParse = errThemeVariant(theme.FrozenVariants[0])
			return
		}
		roleMap = roles
	})
	return roleMap, roleParse
}

type themeError struct{ variant string }

func errThemeVariant(variant string) error { return &themeError{variant} }

func (e *themeError) Error() string {
	return "palette: default theme variant " + e.variant + " missing from tokens"
}

// roleHex returns the hex value for a theme role in the active variant, or
// fallback (a historical literal) when the tokens are unavailable.
func roleHex(role, fallback string) string {
	roles, _ := loadRoles()
	if hex, ok := roles[role]; ok && hex != "" {
		return hex
	}
	return fallback
}

// Current resolves a named theme role to a lipgloss color using the shared
// theme tokens, with hardcoded cyberpunk fallbacks so rendering never fails
// even if token parsing is broken. Valid roles are those in
// theme.FrozenRoles (primary, success, warning, error, info, textMuted,
// border, textPrimary, surfaceAlt, ...).
func Current(role string) color.Color {
	return lipgloss.Color(roleHex(role, fallbackRoles[role]))
}

// fallbackRoles mirrors the historical literals that preceded the unified
// theming work; used only when the embedded tokens cannot be parsed.
var fallbackRoles = map[string]string{
	"primary":       "#F97316",
	"primaryBright": "#FB923C",
	"primaryDark":   "#C2410C",
	"primaryGlow":   "#FDBA74",
	"accent":        "#F59E0B",
	"secondary":     "#10B981",
	"success":       "#10B981",
	"warning":       "#F59E0B",
	"error":         "#EF4444",
	"info":          "#3B82F6",
	"background":    "#000000",
	"surface":       "#111827",
	"surfaceAlt":    "#1F2937",
	"border":        "#374151",
	"textPrimary":   "#E5E7EB",
	"textMuted":     "#6B7280",
	"terminalGreen": "#10B981",
	"terminalAmber": "#F59E0B",
}
