// Package lite provides a lightweight TUI for meept chat.
package lite

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/render"
)

// ANSI escape sequences for scrolling region control.
const (
	// CSI sequences
	csi = "\033["

	// DECSTBM - Set Top and Bottom Margins (scrolling region)
	// Usage: \033[<top>;<bottom>r
	setScrollRegion = csi + "%d;%dr"

	// Reset scrolling region to full screen
	resetScrollRegion = csi + "r"

	// CUP - Cursor Position
	// Usage: \033[<row>;<col>H
	cursorPosition = csi + "%d;%dH"

	// ED - Erase in Display (clear from cursor to end of screen)
	clearToEnd = csi + "J"

	// EL - Erase in Line (clear from cursor to end of line)
	clearLine = csi + "K"

	// Save and restore cursor position
	saveCursor    = csi + "s"
	restoreCursor = csi + "u"

	// Hide/show cursor
	hideCursor = csi + "?25l"
	showCursor = csi + "?25h"
)

// ScrollRegionPrinter manages printing to a scrolling region while keeping
// a fixed area at the bottom for prompt/dashboard.
type ScrollRegionPrinter struct {
	width           int
	height          int
	scrollRegionEnd int // Last line of scrolling region (1-indexed)
	fixedLines      int // Number of fixed lines at bottom (prompt + dashboard)
	renderer        *render.MarkdownRenderer
	initialized     bool
}

// Message styles (same as printer.go)
var (
	srUserLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F59E0B"))

	srAssistantLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#10B981"))

	srSystemLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true)

	srNotificationLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#22C55E"))

	srUserContent = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5E7EB")).
			PaddingLeft(2)

	srAssistantContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E5E7EB")).
				PaddingLeft(2)

	srSystemContent = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Italic(true).
			PaddingLeft(2)

	srNotificationContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#86EFAC")).
				PaddingLeft(2)

	srTimestamp = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")).
			MarginLeft(1)

	srSeparator = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#374151"))
)

// NewScrollRegionPrinter creates a new scroll region printer.
func NewScrollRegionPrinter(fixedLines int) *ScrollRegionPrinter {
	md, _ := render.NewMarkdownRenderer(76, true)
	return &ScrollRegionPrinter{
		width:      80,
		height:     24,
		fixedLines: fixedLines, // Usually 3 (prompt 2 lines + dashboard 1 line)
		renderer:   md,
	}
}

// SetSize updates dimensions and recalculates the scrolling region.
func (p *ScrollRegionPrinter) SetSize(width, height int) tea.Cmd {
	p.width = width
	p.height = height
	p.scrollRegionEnd = height - p.fixedLines

	if p.renderer != nil {
		_ = p.renderer.SetWidth(width - 4)
	}

	// Return command to set up scrolling region
	return p.setupScrollRegion()
}

// setupScrollRegion returns a command that prints ANSI codes to set up the scrolling region.
func (p *ScrollRegionPrinter) setupScrollRegion() tea.Cmd {
	if p.scrollRegionEnd <= 0 {
		return nil
	}

	// Set scrolling region from line 1 to scrollRegionEnd
	// This keeps lines below scrollRegionEnd fixed (our prompt area)
	setup := fmt.Sprintf(setScrollRegion, 1, p.scrollRegionEnd)

	// Position cursor at bottom of scroll region for next print
	setup += fmt.Sprintf(cursorPosition, p.scrollRegionEnd, 1)

	return tea.Printf("%s", setup)
}

// PrintMessage returns a command that prints a formatted message within the scroll region.
func (p *ScrollRegionPrinter) PrintMessage(role, content string) tea.Cmd {
	formatted := p.formatMessage(role, content)

	// Print within the scrolling region - content will scroll up naturally
	return tea.Println(formatted)
}

// PrintSeparator returns a command that prints a separator line.
func (p *ScrollRegionPrinter) PrintSeparator() tea.Cmd {
	sep := srSeparator.Render(strings.Repeat("─", p.width-4))
	return tea.Println(sep)
}

// formatMessage formats a single message with role label, timestamp, and content.
func (p *ScrollRegionPrinter) formatMessage(role, content string) string {
	var sb strings.Builder

	// Role label with timestamp
	label := p.formatRoleLabel(role)
	timestamp := srTimestamp.Render(time.Now().Format("15:04"))
	sb.WriteString(label + timestamp + "\n")

	// Content
	formatted := p.formatContent(role, content)
	sb.WriteString(formatted)

	return sb.String()
}

// formatRoleLabel returns the styled role label.
func (p *ScrollRegionPrinter) formatRoleLabel(role string) string {
	switch role {
	case "user":
		return srUserLabel.Render("you:")
	case "assistant":
		return srAssistantLabel.Render("meept:")
	case "system":
		return srSystemLabel.Render("system:")
	case "notification":
		return srNotificationLabel.Render("task:")
	default:
		return srSystemLabel.Render(role + ":")
	}
}

// formatContent formats the message content with appropriate styling.
func (p *ScrollRegionPrinter) formatContent(role, content string) string {
	// Try markdown rendering for assistant messages
	if role == "assistant" && p.renderer != nil && render.DetectMarkdown(content) {
		rendered, err := p.renderer.Render(content)
		if err == nil {
			lines := strings.Split(rendered, "\n")
			for i, line := range lines {
				lines[i] = "  " + line
			}
			return strings.Join(lines, "\n")
		}
	}

	// Plain text with word wrapping
	var style lipgloss.Style
	switch role {
	case "user":
		style = srUserContent
	case "assistant":
		style = srAssistantContent
	case "system":
		style = srSystemContent
	case "notification":
		style = srNotificationContent
	default:
		style = srAssistantContent
	}

	wrapped := p.wrapText(content, p.width-4)
	return style.Render(wrapped)
}

// wrapText wraps text to the given width.
func (p *ScrollRegionPrinter) wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}

		for len(line) > width {
			breakPoint := width
			for j := width - 1; j >= 0; j-- {
				if line[j] == ' ' {
					breakPoint = j
					break
				}
			}
			if breakPoint == 0 {
				breakPoint = width
			}

			result.WriteString(line[:breakPoint])
			result.WriteString("\n")
			line = strings.TrimLeft(line[breakPoint:], " ")
		}
		result.WriteString(line)
	}

	return result.String()
}

// RenderFixedArea returns the content for the fixed area (prompt + dashboard)
// with proper cursor positioning.
func (p *ScrollRegionPrinter) RenderFixedArea(promptView, dashboardView string) string {
	if p.scrollRegionEnd <= 0 {
		return promptView + "\n" + dashboardView
	}

	var sb strings.Builder

	// Save cursor position (we're in the scrolling region)
	sb.WriteString(saveCursor)

	// Move cursor to the fixed area (first line below scrolling region)
	sb.WriteString(fmt.Sprintf(cursorPosition, p.scrollRegionEnd+1, 1))

	// Clear the fixed area
	sb.WriteString(clearToEnd)

	// Render prompt
	sb.WriteString(promptView)
	sb.WriteString("\n")

	// Render dashboard
	sb.WriteString(dashboardView)

	// Restore cursor to scrolling region
	sb.WriteString(restoreCursor)

	return sb.String()
}

// Cleanup returns ANSI codes to reset terminal state.
func (p *ScrollRegionPrinter) Cleanup() string {
	return resetScrollRegion + showCursor
}
