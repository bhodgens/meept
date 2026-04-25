// Package lite provides a lightweight TUI for meept.
package lite

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
)

// Color thresholds for context fill
const (
	thresholdGreen  = 50  // <50%
	thresholdYellow = 80  // 50-80%
	thresholdOrange = 95  // 80-95%
	// >=95% is red
)

// Width breakpoints for responsive layout
const (
	widthFull    = 76 // Full layout
	widthCompact = 52 // Compact layout
	// Below 52 is minimal layout
)

// Colors are defined in styles.go
// Additional status-specific colors
var (
	colorFg = lipgloss.Color("#E5E7EB") // light gray foreground
	colorBg = lipgloss.Color("#1F2937") // dark gray background
)

// StatusBar is a responsive status bar component showing model info, token usage, cost, and duration.
type StatusBar struct {
	modelName  string
	tokensUsed int
	tokensMax  int
	costCents  int
	startTime  time.Time
	width      int

	// Styles
	baseStyle     lipgloss.Style
	modelStyle    lipgloss.Style
	mutedStyle    lipgloss.Style
	separatorChar string
}

// NewStatusBar creates a new StatusBar component.
func NewStatusBar() *StatusBar {
	return &StatusBar{
		modelName:     "",
		tokensUsed:    0,
		tokensMax:     128000, // reasonable default
		costCents:     0,
		startTime:     time.Now(),
		width:         80,
		baseStyle:     lipgloss.NewStyle().Background(colorBg).Foreground(colorFg),
		modelStyle:    lipgloss.NewStyle().Foreground(colorFg).Bold(true),
		mutedStyle:    lipgloss.NewStyle().Foreground(colorMuted),
		separatorChar: " | ",
	}
}

// SetSize updates the width of the status bar for responsive layout.
func (s *StatusBar) SetSize(width int) {
	s.width = width
}

// SetModel sets the model name to display.
func (s *StatusBar) SetModel(name string) {
	s.modelName = name
}

// SetTokens sets the current and max token counts.
func (s *StatusBar) SetTokens(used, max int) {
	s.tokensUsed = used
	s.tokensMax = max
}

// SetCost sets the cost in cents.
func (s *StatusBar) SetCost(cents int) {
	s.costCents = cents
}

// SetStartTime sets the start time for duration calculation.
func (s *StatusBar) SetStartTime(t time.Time) {
	s.startTime = t
}

// Update handles bubbletea messages. Currently no-op but kept for interface consistency.
func (s *StatusBar) Update(msg tea.Msg) tea.Cmd {
	return nil
}

// View renders the status bar according to current width.
func (s *StatusBar) View() string {
	if s.width < widthCompact {
		return s.renderMinimal()
	}
	if s.width < widthFull {
		return s.renderCompact()
	}
	return s.renderFull()
}

// renderFull renders the full layout (>=76 columns):
// | claude-sonnet | 2.3k/128k [--------] 18% | $0.02 | 3m42s           |
func (s *StatusBar) renderFull() string {
	var parts []string

	// Model name
	modelDisplay := s.getModelDisplay(20)
	parts = append(parts, s.modelStyle.Render(modelDisplay))

	// Token usage with visual bar
	tokenPart := s.renderTokensFull()
	parts = append(parts, tokenPart)

	// Cost
	costPart := s.renderCost()
	parts = append(parts, costPart)

	// Duration
	durationPart := s.renderDuration()
	parts = append(parts, durationPart)

	content := strings.Join(parts, s.separatorChar)
	return s.padToWidth(content)
}

// renderCompact renders the compact layout (52-75 columns):
// | sonnet | 2.3k/128k 18% | $0.02 | 3m42s |
func (s *StatusBar) renderCompact() string {
	var parts []string

	// Abbreviated model name
	modelDisplay := s.getModelDisplay(10)
	parts = append(parts, s.modelStyle.Render(modelDisplay))

	// Token usage without visual bar
	tokenPart := s.renderTokensCompact()
	parts = append(parts, tokenPart)

	// Cost
	costPart := s.renderCost()
	parts = append(parts, costPart)

	// Duration
	durationPart := s.renderDuration()
	parts = append(parts, durationPart)

	content := strings.Join(parts, s.separatorChar)
	return s.padToWidth(content)
}

// renderMinimal renders the minimal layout (<52 columns):
// | sonnet 18% |
func (s *StatusBar) renderMinimal() string {
	// Just model + percent
	modelDisplay := s.getModelDisplay(8)
	percent := s.calculatePercent()
	percentStyle := s.getPercentStyle(percent)

	content := fmt.Sprintf("%s %s",
		s.modelStyle.Render(modelDisplay),
		percentStyle.Render(fmt.Sprintf("%d%%", percent)),
	)
	return s.padToWidth(content)
}

// renderTokensFull renders tokens with visual bar for full layout.
func (s *StatusBar) renderTokensFull() string {
	usedStr := formatTokenCount(s.tokensUsed)
	maxStr := formatTokenCount(s.tokensMax)
	percent := s.calculatePercent()
	percentStyle := s.getPercentStyle(percent)

	// Visual bar (8 chars)
	bar := s.renderBar(8, percent)

	return fmt.Sprintf("%s/%s %s %s",
		usedStr,
		maxStr,
		bar,
		percentStyle.Render(fmt.Sprintf("%d%%", percent)),
	)
}

// renderTokensCompact renders tokens without visual bar for compact layout.
func (s *StatusBar) renderTokensCompact() string {
	usedStr := formatTokenCount(s.tokensUsed)
	maxStr := formatTokenCount(s.tokensMax)
	percent := s.calculatePercent()
	percentStyle := s.getPercentStyle(percent)

	return fmt.Sprintf("%s/%s %s",
		usedStr,
		maxStr,
		percentStyle.Render(fmt.Sprintf("%d%%", percent)),
	)
}

// renderBar renders a visual progress bar with color coding.
func (s *StatusBar) renderBar(width, percent int) string {
	if width <= 2 {
		return ""
	}

	innerWidth := width - 2 // Account for brackets
	if innerWidth <= 0 {
		return "[]"
	}

	filled := (percent * innerWidth) / 100
	if filled > innerWidth {
		filled = innerWidth
	}
	empty := innerWidth - filled

	// Get fill character color based on percentage
	fillColor := s.getPercentColor(percent)
	fillStyle := lipgloss.NewStyle().Foreground(fillColor)
	emptyStyle := lipgloss.NewStyle().Foreground(colorMuted)

	filledStr := fillStyle.Render(strings.Repeat("=", filled))
	emptyStr := emptyStyle.Render(strings.Repeat("-", empty))

	return "[" + filledStr + emptyStr + "]"
}

// renderCost renders the cost in dollars.
func (s *StatusBar) renderCost() string {
	dollars := float64(s.costCents) / 100.0
	return s.mutedStyle.Render(fmt.Sprintf("$%.2f", dollars))
}

// renderDuration renders elapsed time in a compact format.
func (s *StatusBar) renderDuration() string {
	elapsed := time.Since(s.startTime)
	return s.mutedStyle.Render(formatDuration(elapsed))
}

// calculatePercent returns the context fill percentage.
func (s *StatusBar) calculatePercent() int {
	if s.tokensMax <= 0 {
		return 0
	}
	percent := (s.tokensUsed * 100) / s.tokensMax
	if percent > 100 {
		percent = 100
	}
	return percent
}

// getPercentColor returns the appropriate color for a given percentage.
func (s *StatusBar) getPercentColor(percent int) lipgloss.Color {
	switch {
	case percent < thresholdGreen:
		return colorGreen
	case percent < thresholdYellow:
		return colorYellow
	case percent < thresholdOrange:
		return colorOrange
	default:
		return colorRed
	}
}

// getPercentStyle returns a lipgloss style for the given percentage.
func (s *StatusBar) getPercentStyle(percent int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.getPercentColor(percent))
}

// getModelDisplay returns the model name, truncated or abbreviated as needed.
func (s *StatusBar) getModelDisplay(maxLen int) string {
	if s.modelName == "" {
		return "unknown"
	}

	name := s.modelName

	// Common abbreviations for compact layouts
	if maxLen <= 10 {
		name = abbreviateModelName(name)
	}

	// Truncate if still too long
	if len(name) > maxLen {
		if maxLen > 3 {
			name = name[:maxLen-3] + "..."
		} else {
			name = name[:maxLen]
		}
	}

	return name
}

// padToWidth pads the content to fill the full width with a styled background.
func (s *StatusBar) padToWidth(content string) string {
	// Calculate visible width (without ANSI codes)
	visibleWidth := lipgloss.Width(content)

	if visibleWidth < s.width {
		padding := s.width - visibleWidth
		content = content + strings.Repeat(" ", padding)
	}

	return s.baseStyle.Width(s.width).MaxWidth(s.width).Render(content)
}

// abbreviateModelName returns a shortened version of common model names.
func abbreviateModelName(name string) string {
	// Common abbreviations
	abbreviations := map[string]string{
		"claude-3-opus":         "opus",
		"claude-3-sonnet":       "sonnet",
		"claude-3-haiku":        "haiku",
		"claude-3.5-sonnet":     "sonnet-3.5",
		"claude-3.5-haiku":      "haiku-3.5",
		"claude-opus-4":         "opus-4",
		"claude-sonnet-4":       "sonnet-4",
		"gpt-4":                 "gpt4",
		"gpt-4-turbo":           "gpt4t",
		"gpt-4o":                "gpt4o",
		"gpt-3.5-turbo":         "gpt35",
		"gemini-pro":            "gemini",
		"gemini-1.5-pro":        "gem1.5p",
		"gemini-1.5-flash":      "gem1.5f",
		"mistral-large":         "mistral",
		"mixtral-8x7b":          "mixtral",
		"llama-3-70b":           "llama70",
		"llama-3-8b":            "llama8",
	}

	// Check for exact matches first
	if abbr, ok := abbreviations[strings.ToLower(name)]; ok {
		return abbr
	}

	// Check for partial matches
	lowerName := strings.ToLower(name)
	for full, abbr := range abbreviations {
		if strings.Contains(lowerName, strings.ToLower(full)) {
			return abbr
		}
	}

	// Extract key parts from name
	// e.g., "claude-3-opus-20240229" -> "opus"
	parts := strings.Split(name, "-")
	for _, part := range parts {
		switch strings.ToLower(part) {
		case "opus", "sonnet", "haiku":
			return part
		}
	}

	// Fallback: return first 8 chars
	if len(name) > 8 {
		return name[:8]
	}
	return name
}

// formatTokenCount formats a token count in a human-readable way.
func formatTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 10000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	if n < 1000000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}

// formatDuration formats a duration in a compact form like "3m42s" or "1h23m".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}

	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%02dm", hours, minutes)
}
