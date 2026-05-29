// Package lite provides a lightweight TUI for meept chat.
package lite

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/render"
)

// MessagePrinter formats messages for printing to terminal scrollback.
// Unlike Viewport, this doesn't maintain state - it just formats and prints.
type MessagePrinter struct {
	width    int
	renderer *render.MarkdownRenderer
}

// Styles for different message roles (matching viewport styles).
var (
	printerUserLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F59E0B"))

	printerAssistantLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#10B981"))

	printerSystemLabel = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true)

	printerNotificationLabel = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#22C55E"))

	printerUserContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E5E7EB")).
				PaddingLeft(2)

	printerAssistantContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E5E7EB")).
				PaddingLeft(2)

	printerSystemContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9CA3AF")).
				Italic(true).
				PaddingLeft(2)

	printerNotificationContent = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#86EFAC")).
					PaddingLeft(2)

	printerTimestamp = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4B5563")).
				MarginLeft(1)

	printerSeparator = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#374151"))
)

// NewMessagePrinter creates a new message printer.
func NewMessagePrinter() *MessagePrinter {
	md, _ := render.NewMarkdownRenderer(76, true)
	return &MessagePrinter{
		width:    80,
		renderer: md,
	}
}

// SetWidth updates the printer width for word wrapping.
func (p *MessagePrinter) SetWidth(width int) {
	p.width = width
	if p.renderer != nil {
		_ = p.renderer.SetWidth(width - 4)
	}
}

// PrintMessage returns a tea.Cmd that prints a formatted message to scrollback.
func (p *MessagePrinter) PrintMessage(role, content string) tea.Cmd {
	formatted := p.formatMessage(role, content)
	return tea.Println(formatted)
}

// PrintSeparator returns a tea.Cmd that prints a separator line.
func (p *MessagePrinter) PrintSeparator() tea.Cmd {
	sep := printerSeparator.Render(strings.Repeat("─", p.width-4))
	return tea.Println(sep)
}

// formatMessage formats a single message with role label, timestamp, and content.
func (p *MessagePrinter) formatMessage(role, content string) string {
	var sb strings.Builder

	// Role label with timestamp
	label := p.formatRoleLabel(role)
	timestamp := printerTimestamp.Render(time.Now().Format("15:04"))
	sb.WriteString(label + timestamp + "\n")

	// Content
	formatted := p.formatContent(role, content)
	sb.WriteString(formatted)

	return sb.String()
}

// formatRoleLabel returns the styled role label.
func (p *MessagePrinter) formatRoleLabel(role string) string {
	switch role {
	case "user":
		return printerUserLabel.Render("you:")
	case "assistant":
		return printerAssistantLabel.Render("meept:")
	case "system":
		return printerSystemLabel.Render("system:")
	case "notification":
		return printerNotificationLabel.Render("task:")
	default:
		return printerSystemLabel.Render(role + ":")
	}
}

// formatContent formats the message content with appropriate styling.
func (p *MessagePrinter) formatContent(role, content string) string {
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
		style = printerUserContent
	case "assistant":
		style = printerAssistantContent
	case "system":
		style = printerSystemContent
	case "notification":
		style = printerNotificationContent
	default:
		style = printerAssistantContent
	}

	wrapped := p.wrapText(content, p.width-4)
	return style.Render(wrapped)
}

// wrapText wraps text to the given width.
func (p *MessagePrinter) wrapText(text string, width int) string {
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
