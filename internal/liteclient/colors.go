// Package liteclient provides a minimalistic console client for meept.
package liteclient

import "github.com/nsf/termbox-go"

// Color palette for meept-lite prompt and UI.
// termbox-go uses 256-color mode, we'll use closest matches.
// Background is always termbox.ColorDefault to support terminal transparency.
const (
	// Prompt colors (256-color palette)
	ColorOrange = 208 // #F97316 -> closest is rgb(255,135,0)
	ColorWhite  = 255 // #E5E7EB -> light grey
	ColorGrey   = 242 // #6B7280 -> medium grey

	// UI colors
	ColorMuted   = 242
	ColorError   = 196 // bright red
	ColorSuccess = 35  // green
)

// DefaultBg returns the default terminal background attribute for transparency support.
func DefaultBg() termbox.Attribute {
	return termbox.ColorDefault
}

// Style represents a text style with foreground and background colors.
type Style struct {
	Fg termbox.Attribute
	Bg termbox.Attribute
}

// NewStyle creates a new style with default background.
func NewStyle(fg termbox.Attribute) Style {
	return Style{Fg: fg, Bg: termbox.ColorDefault}
}
