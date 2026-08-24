package viz

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/theme"
)

// Visualization colors, derived from the shared theme tokens. The mapping to
// the unified roles is: Idle→textPrimary, Working→primary,
// Success→success, Muted→textMuted, Carrying→info, Error→error,
// Dispatcher→warning, DotLine→border.
//
// Like the parent tui package's palette (which viz cannot import — tui
// imports viz), themes are restart-applied: SetPalette reads the embedded
// tokens and repoints these vars; without it they hold the historical
// literals.
var (
	ColorIdle       = lipgloss.Color("#E5E7EB") // light gray - idle/foreground
	ColorWorking    = lipgloss.Color("#F97316") // orange - primary/working
	ColorSuccess    = lipgloss.Color("#10B981") // green - task complete
	ColorMuted      = lipgloss.Color("#6B7280") // gray - dispatching subtask
	ColorCarrying   = lipgloss.Color("#3B82F6") // blue - carrying/working halo
	ColorError      = lipgloss.Color("#EF4444") // red - failed/problems
	ColorDispatcher = lipgloss.Color("#F59E0B") // amber - dispatcher block
	ColorDotLine    = lipgloss.Color("#374151") // dark gray - dotted lines
)

// SetPalette switches visualization colors to a named theme variant by
// parsing the shared embedded tokens via the theme package. Unknown names
// return an error listing valid ones and leave the current colors untouched.
func SetPalette(name string) error {
	tokens, err := theme.Parse(theme.TokensJSON5)
	if err != nil {
		return fmt.Errorf("viz: load theme tokens: %w", err)
	}
	frozen := false
	for _, v := range theme.FrozenVariants {
		if v == name {
			frozen = true
			break
		}
	}
	if !frozen {
		return fmt.Errorf("viz: unknown ui theme %q (valid: %s)",
			name, strings.Join(theme.FrozenVariants, ", "))
	}
	roles := tokens[name] // present for every frozen variant; Parse validated

	ColorIdle = lipgloss.Color(roles["textPrimary"])
	ColorWorking = lipgloss.Color(roles["primary"])
	ColorSuccess = lipgloss.Color(roles["success"])
	ColorMuted = lipgloss.Color(roles["textMuted"])
	ColorCarrying = lipgloss.Color(roles["info"])
	ColorError = lipgloss.Color(roles["error"])
	ColorDispatcher = lipgloss.Color(roles["warning"])
	ColorDotLine = lipgloss.Color(roles["border"])
	return nil
}

// PaletteSnapshot captures the active visualization colors as concrete
// values so callers can take them once and pass them around without racing
// later SetPalette calls.
type PaletteSnapshot struct {
	Idle       color.Color
	Working    color.Color
	Success    color.Color
	Muted      color.Color
	Carrying   color.Color
	Error      color.Color
	Dispatcher color.Color
	DotLine    color.Color
}

// Current returns the active visualization colors.
func Current() PaletteSnapshot {
	return PaletteSnapshot{
		Idle:       ColorIdle,
		Working:    ColorWorking,
		Success:    ColorSuccess,
		Muted:      ColorMuted,
		Carrying:   ColorCarrying,
		Error:      ColorError,
		Dispatcher: ColorDispatcher,
		DotLine:    ColorDotLine,
	}
}
