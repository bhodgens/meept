package lite

import (
	"github.com/charmbracelet/lipgloss"
)

// Color definitions for meept-lite.
// Uses terminal defaults where possible, with explicit colors only where necessary.
var (
	// Status colors based on percentage thresholds
	colorGreen  = lipgloss.Color("#10B981") // <50%
	colorYellow = lipgloss.Color("#F59E0B") // 50-80%
	colorOrange = lipgloss.Color("#F97316") // 80-95%
	colorRed    = lipgloss.Color("#EF4444") // >=95%

	// Muted color for inactive elements
	colorMuted = lipgloss.Color("#6B7280")

	// Border color
	colorBorder = lipgloss.Color("#374151")

	// User message accent
	colorUserAccent = lipgloss.Color("#60A5FA") // Blue for user messages
)

// Styles provides lipgloss styles for meept-lite TUI.
type Styles struct {
	// Status bar
	StatusBar    lipgloss.Style
	StatusGreen  lipgloss.Style
	StatusYellow lipgloss.Style
	StatusOrange lipgloss.Style
	StatusRed    lipgloss.Style

	// Messages
	UserMessage   lipgloss.Style
	AssistantMsg  lipgloss.Style
	SystemMessage lipgloss.Style

	// Input
	Prompt      lipgloss.Style
	InputBorder lipgloss.Style

	// Menu
	MenuItem     lipgloss.Style
	MenuSelected lipgloss.Style
	MenuBorder   lipgloss.Style

	// Agent status
	AgentStatus lipgloss.Style

	// General
	Muted lipgloss.Style
	Bold  lipgloss.Style
	Error lipgloss.Style
}

// DefaultStyles returns the default style set for meept-lite.
func DefaultStyles() *Styles {
	s := &Styles{}

	// Status bar - uses terminal defaults for background
	s.StatusBar = lipgloss.NewStyle().
		Padding(0, 1)

	// Status colors based on percentage thresholds
	s.StatusGreen = lipgloss.NewStyle().
		Foreground(colorGreen).
		Bold(true)

	s.StatusYellow = lipgloss.NewStyle().
		Foreground(colorYellow).
		Bold(true)

	s.StatusOrange = lipgloss.NewStyle().
		Foreground(colorOrange).
		Bold(true)

	s.StatusRed = lipgloss.NewStyle().
		Foreground(colorRed).
		Bold(true)

	// Messages - distinct styling for user vs assistant
	s.UserMessage = lipgloss.NewStyle().
		Foreground(colorUserAccent).
		Bold(true).
		PaddingLeft(2)

	s.AssistantMsg = lipgloss.NewStyle().
		PaddingLeft(2)

	s.SystemMessage = lipgloss.NewStyle().
		Foreground(colorMuted).
		Italic(true).
		PaddingLeft(2)

	// Input - shell-like prompt styling
	s.Prompt = lipgloss.NewStyle().
		Bold(true)

	s.InputBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorBorder)

	// Menu styling
	s.MenuItem = lipgloss.NewStyle().
		PaddingLeft(2)

	s.MenuSelected = lipgloss.NewStyle().
		PaddingLeft(2).
		Bold(true).
		Reverse(true)

	s.MenuBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1)

	// Agent status display
	s.AgentStatus = lipgloss.NewStyle().
		Foreground(colorMuted).
		Italic(true)

	// General styles
	s.Muted = lipgloss.NewStyle().
		Foreground(colorMuted)

	s.Bold = lipgloss.NewStyle().
		Bold(true)

	s.Error = lipgloss.NewStyle().
		Foreground(colorRed).
		Bold(true)

	return s
}

// StatusColor returns the appropriate status style based on percentage.
// Thresholds:
//   - Green:  <50%
//   - Yellow: 50-80%
//   - Orange: 80-95%
//   - Red:    >=95%
func (s *Styles) StatusColor(percent float64) lipgloss.Style {
	switch {
	case percent < 50:
		return s.StatusGreen
	case percent < 80:
		return s.StatusYellow
	case percent < 95:
		return s.StatusOrange
	default:
		return s.StatusRed
	}
}
