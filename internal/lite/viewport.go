// Package lite provides a lightweight TUI for meept chat.
package lite

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/caimlas/meept/internal/tui/render"
)

// Message represents a chat message in the viewport.
type Message struct {
	Role      string    // "user", "assistant", "system"
	Content   string    // Message content (may contain markdown)
	Timestamp time.Time // When the message was created
}

// Viewport wraps the bubbles viewport with chat message storage and rendering.
type Viewport struct {
	viewport   viewport.Model
	messages   []Message
	width      int
	height     int
	renderer   *render.MarkdownRenderer
	inputFocus bool // Whether input field has focus (disables j/k navigation)

	// Cache for rendered content
	renderedContent string
	dirty           bool // Whether messages need re-rendering
}

// Styles for different message roles.
var (
	userLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F59E0B")). // Amber
			MarginBottom(0)

	assistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#10B981")). // Green
				MarginBottom(0)

	systemLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#6B7280")). // Gray
				Italic(true).
				MarginBottom(0)

	notificationLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#22C55E")). // Bright green
				MarginBottom(0)

	userContentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E5E7EB")).
				PaddingLeft(2)

	assistantContentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E5E7EB")).
				PaddingLeft(2)

	systemContentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#9CA3AF")).
				Italic(true).
				PaddingLeft(2)

	notificationContentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#86EFAC")). // Light green
				PaddingLeft(2)

	timestampStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4B5563")).
			MarginLeft(1)

	messageSeparatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#374151"))
)

// NewViewport creates a new viewport component.
func NewViewport() *Viewport {
	vp := viewport.New(80, 24)
	vp.Style = lipgloss.NewStyle()
	vp.MouseWheelEnabled = true

	md, err := render.NewMarkdownRenderer(78, true)
	if err != nil {
		// Fall back to nil renderer; we'll render plain text
		md = nil
	}

	return &Viewport{
		viewport:   vp,
		messages:   make([]Message, 0),
		width:      80,
		height:     24,
		renderer:   md,
		inputFocus: true, // Start with input focused
		dirty:      true,
	}
}

// SetSize updates the viewport dimensions.
func (v *Viewport) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.viewport.Width = width
	v.viewport.Height = height

	// Update markdown renderer width
	if v.renderer != nil {
		_ = v.renderer.SetWidth(width - 4) // Account for padding
	}

	// Mark dirty to re-render with new width
	v.dirty = true
}

// SetYPosition sets the viewport's vertical position for mouse coordinate calculation.
// This must be set to the row where the viewport starts in the terminal.
func (v *Viewport) SetYPosition(y int) {
	v.viewport.YPosition = y
}

// SetInputFocus sets whether the input field has focus.
// When input is focused, j/k keys are passed through instead of scrolling.
func (v *Viewport) SetInputFocus(focused bool) {
	v.inputFocus = focused
}

// Update handles input events for scrolling.
func (v *Viewport) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "pgup", "page_up":
			v.pageUp()
			return nil
		case "pgdown", "page_down":
			v.pageDown()
			return nil
		case "home":
			v.viewport.GotoTop()
			return nil
		case "end":
			v.ScrollToBottom()
			return nil
		case "k", "up":
			// Only handle if input is not focused
			if !v.inputFocus {
				v.viewport.LineUp(1)
				return nil
			}
		case "j", "down":
			// Only handle if input is not focused
			if !v.inputFocus {
				v.viewport.LineDown(1)
				return nil
			}
		case "ctrl+u":
			// Half page up
			v.viewport.LineUp(v.height / 2)
			return nil
		case "ctrl+d":
			// Half page down
			v.viewport.LineDown(v.height / 2)
			return nil
		case "ctrl+b":
			// Page up (vim style)
			v.pageUp()
			return nil
		case "ctrl+f":
			// Page down (vim style)
			v.pageDown()
			return nil
		case "[":
			// Jump to previous response block
			v.jumpToPreviousBlock()
			return nil
		case "]":
			// Jump to next response block
			v.jumpToNextBlock()
			return nil
		}

		// Pass unhandled keys to viewport for mouse/scroll handling
		v.viewport, cmd = v.viewport.Update(msg)

	case tea.MouseMsg:
		v.viewport, cmd = v.viewport.Update(msg)

	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)
	}

	return cmd
}

// View renders the viewport.
func (v *Viewport) View() string {
	// Re-render if dirty
	if v.dirty {
		v.renderMessages()
		v.dirty = false
	}

	return v.viewport.View()
}

// AddMessage adds a new message to the viewport and scrolls to bottom.
func (v *Viewport) AddMessage(role, content string) {
	v.messages = append(v.messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
	v.dirty = true
	v.renderMessages()
	v.ScrollToBottom()
}

// AppendToLastMessage appends content to the last message (for streaming).
func (v *Viewport) AppendToLastMessage(content string) {
	if len(v.messages) == 0 {
		return
	}
	v.messages[len(v.messages)-1].Content += content
	v.dirty = true
	v.renderMessages()
	v.ScrollToBottom()
}

// UpdateLastMessage replaces the content of the last message.
func (v *Viewport) UpdateLastMessage(content string) {
	if len(v.messages) == 0 {
		return
	}
	v.messages[len(v.messages)-1].Content = content
	v.dirty = true
	v.renderMessages()
	v.ScrollToBottom()
}

// ScrollToBottom scrolls the viewport to the bottom.
func (v *Viewport) ScrollToBottom() {
	v.viewport.GotoBottom()
}

// Messages returns a copy of all messages.
func (v *Viewport) Messages() []Message {
	result := make([]Message, len(v.messages))
	copy(result, v.messages)
	return result
}

// ClearMessages removes all messages from the viewport.
func (v *Viewport) ClearMessages() {
	v.messages = make([]Message, 0)
	v.dirty = true
	v.renderMessages()
}

// MessageCount returns the number of messages.
func (v *Viewport) MessageCount() int {
	return len(v.messages)
}

// pageUp scrolls up by one page.
func (v *Viewport) pageUp() {
	v.viewport.LineUp(v.height)
}

// pageDown scrolls down by one page.
func (v *Viewport) pageDown() {
	v.viewport.LineDown(v.height)
}

// jumpToPreviousBlock jumps to the previous assistant response.
// This mimics bash history navigation by jumping between response blocks.
func (v *Viewport) jumpToPreviousBlock() {
	if len(v.messages) == 0 {
		return
	}

	// Calculate line positions of each message
	linePositions := v.calculateMessageLinePositions()
	if len(linePositions) == 0 {
		return
	}

	currentLine := v.viewport.YOffset

	// Find the previous assistant message that starts before current position
	for i := len(v.messages) - 1; i >= 0; i-- {
		if v.messages[i].Role == "assistant" {
			pos := linePositions[i]
			if pos < currentLine {
				v.viewport.SetYOffset(pos)
				return
			}
		}
	}

	// If no previous block found, go to top
	v.viewport.GotoTop()
}

// jumpToNextBlock jumps to the next assistant response.
func (v *Viewport) jumpToNextBlock() {
	if len(v.messages) == 0 {
		return
	}

	// Calculate line positions of each message
	linePositions := v.calculateMessageLinePositions()
	if len(linePositions) == 0 {
		return
	}

	currentLine := v.viewport.YOffset

	// Find the next assistant message that starts after current position
	for i := 0; i < len(v.messages); i++ {
		if v.messages[i].Role == "assistant" {
			pos := linePositions[i]
			if pos > currentLine {
				v.viewport.SetYOffset(pos)
				return
			}
		}
	}

	// If no next block found, go to bottom
	v.viewport.GotoBottom()
}

// calculateMessageLinePositions returns the starting line number for each message.
func (v *Viewport) calculateMessageLinePositions() []int {
	positions := make([]int, len(v.messages))
	currentLine := 0

	for i, msg := range v.messages {
		positions[i] = currentLine

		// Calculate how many lines this message takes
		rendered := v.renderSingleMessage(msg)
		lines := strings.Count(rendered, "\n") + 1
		currentLine += lines + 1 // +1 for separator
	}

	return positions
}

// renderMessages renders all messages to the viewport content.
func (v *Viewport) renderMessages() {
	if len(v.messages) == 0 {
		v.renderedContent = ""
		v.viewport.SetContent("")
		return
	}

	var sb strings.Builder
	separator := messageSeparatorStyle.Render(strings.Repeat("-", v.width-4))

	for i, msg := range v.messages {
		rendered := v.renderSingleMessage(msg)
		sb.WriteString(rendered)

		// Add separator between messages (but not after the last one)
		if i < len(v.messages)-1 {
			sb.WriteString("\n")
			sb.WriteString(separator)
			sb.WriteString("\n")
		}
	}

	v.renderedContent = sb.String()
	v.viewport.SetContent(v.renderedContent)
}

// renderSingleMessage renders a single message with styling.
func (v *Viewport) renderSingleMessage(msg Message) string {
	var sb strings.Builder

	// Render role label with timestamp
	label := v.renderRoleLabel(msg.Role)
	timestamp := timestampStyle.Render(msg.Timestamp.Format("15:04"))
	sb.WriteString(label + timestamp + "\n")

	// Render content
	content := v.renderContent(msg.Role, msg.Content)
	sb.WriteString(content)

	return sb.String()
}

// renderRoleLabel renders the role label with appropriate styling.
func (v *Viewport) renderRoleLabel(role string) string {
	switch role {
	case "user":
		return userLabelStyle.Render("you:")
	case "assistant":
		return assistantLabelStyle.Render("meept:")
	case "system":
		return systemLabelStyle.Render("system:")
	case "notification":
		return notificationLabelStyle.Render("task:")
	default:
		return systemLabelStyle.Render(role + ":")
	}
}

// renderContent renders the message content with markdown if applicable.
func (v *Viewport) renderContent(role, content string) string {
	// Try markdown rendering for assistant messages
	if role == "assistant" && v.renderer != nil && render.DetectMarkdown(content) {
		rendered, err := v.renderer.Render(content)
		if err == nil {
			// Apply padding to each line
			lines := strings.Split(rendered, "\n")
			for i, line := range lines {
				lines[i] = "  " + line
			}
			return strings.Join(lines, "\n")
		}
	}

	// Plain text rendering with appropriate style
	var style lipgloss.Style
	switch role {
	case "user":
		style = userContentStyle
	case "assistant":
		style = assistantContentStyle
	case "system":
		style = systemContentStyle
	case "notification":
		style = notificationContentStyle
	default:
		style = assistantContentStyle
	}

	// Word wrap content to fit width
	wrapped := v.wrapText(content, v.width-4)
	return style.Render(wrapped)
}

// wrapText wraps text to the given width.
func (v *Viewport) wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}

		// Wrap each line that exceeds width
		for len(line) > width {
			// Find last space before width
			breakPoint := width
			for j := width - 1; j >= 0; j-- {
				if line[j] == ' ' {
					breakPoint = j
					break
				}
			}
			if breakPoint == 0 {
				breakPoint = width // No space found, hard break
			}

			result.WriteString(line[:breakPoint])
			result.WriteString("\n")
			line = strings.TrimLeft(line[breakPoint:], " ")
		}
		result.WriteString(line)
	}

	return result.String()
}

// AtBottom returns true if the viewport is scrolled to the bottom.
func (v *Viewport) AtBottom() bool {
	return v.viewport.AtBottom()
}

// AtTop returns true if the viewport is scrolled to the top.
func (v *Viewport) AtTop() bool {
	return v.viewport.AtTop()
}

// ScrollPercent returns the scroll position as a percentage (0.0 to 1.0).
func (v *Viewport) ScrollPercent() float64 {
	return v.viewport.ScrollPercent()
}

// Height returns the viewport height.
func (v *Viewport) Height() int {
	return v.height
}

// Width returns the viewport width.
func (v *Viewport) Width() int {
	return v.width
}

// StatusLine returns a status line showing scroll position.
func (v *Viewport) StatusLine() string {
	if len(v.messages) == 0 {
		return ""
	}

	percent := v.viewport.ScrollPercent() * 100
	if v.viewport.AtBottom() {
		return fmt.Sprintf("%d messages - bottom", len(v.messages))
	}
	if v.viewport.AtTop() {
		return fmt.Sprintf("%d messages - top", len(v.messages))
	}
	return fmt.Sprintf("%d messages - %d%%", len(v.messages), int(percent))
}
