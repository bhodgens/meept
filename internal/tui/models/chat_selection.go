// Package models provides the view models for the TUI.
package models

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// isClickInViewportArea checks whether the mouse event coordinates fall within the
// viewport content area. It accounts for the screen Y offset (header/chrome above
// the chat area), the viewport border, and the viewport dimensions.
func (m *ChatModel) isClickInViewportArea(mouse tea.Mouse) bool {
	// Viewport content starts after screenYOffset + viewport top border (1 line)
	viewportTop := m.screenYOffset + 1
	viewportBottom := viewportTop + m.viewport.Height()

	// X range: 1 (left border) to 1 + viewport width
	viewportLeft := 1
	viewportRight := viewportLeft + m.viewport.Width()

	return mouse.Y >= viewportTop && mouse.Y < viewportBottom &&
		mouse.X >= viewportLeft && mouse.X < viewportRight
}

// viewportAdjustedCoords converts screen mouse coordinates to viewport-relative
// coordinates, accounting for screen Y offset and borders.
func (m *ChatModel) viewportAdjustedCoords(mouse tea.Mouse) (y, x int) {
	y = mouse.Y - m.screenYOffset - 1 // screenYOffset + border top
	x = mouse.X - 1                   // left border
	return y, x
}

// calculateCursorOffset converts viewport Y,X to character offset in rendered content.
// This accounts for line wrapping and viewport scrolling.
func (m *ChatModel) calculateCursorOffset(y, x int) int {
	content := ansi.Strip(m.viewport.View())
	lines := strings.Split(content, "\n")

	// Y is already relative to visible content (no YOffset adjustment needed)

	// Calculate offset up to the target line
	offset := 0
	for i := 0; i < y && i < len(lines); i++ {
		offset += len(lines[i]) + 1 // +1 for newline
	}

	// Add x offset within the target line
	if y >= 0 && y < len(lines) {
		lineLen := len(lines[y])
		if x > lineLen {
			x = lineLen
		}
		offset += x
	}

	return offset
}

// calculateLinePositions returns the character positions where each line starts.
func (m *ChatModel) calculateLinePositions(lines []string) []int {
	positions := make([]int, len(lines)+1)
	offset := 0
	for i, line := range lines {
		positions[i] = offset
		offset += len(line) + 1 // +1 for newline
	}
	positions[len(lines)] = offset
	return positions
}

// selectWordAt selects the word at the given viewport coordinates.
func (m *ChatModel) selectWordAt(y, x int) {
	offset := m.calculateCursorOffset(y, x)
	content := ansi.Strip(m.viewport.View())

	if offset >= len(content) {
		return
	}

	// Find word boundaries
	start, end := findWordBoundaries(content, offset)

	m.selectionStart = start
	m.selectionEnd = end
	m.isSelecting = true
}

// selectLineAt selects the entire line at the given viewport Y coordinate.
func (m *ChatModel) selectLineAt(y int) {
	content := ansi.Strip(m.viewport.View())
	lines := strings.Split(content, "\n")

	// Y is already relative to visible content (no YOffset adjustment needed)

	if y < 0 || y >= len(lines) {
		return
	}

	// Calculate character offsets for this line
	lineStart := 0
	for i := range y {
		lineStart += len(lines[i]) + 1 // +1 for newline
	}
	lineEnd := min(
		// Ensure lineEnd doesn't exceed content length
		lineStart+len(lines[y]), len(content))

	m.selectionStart = lineStart
	m.selectionEnd = lineEnd
	m.isSelecting = true
}

// findWordBoundaries finds the start and end offsets of the word at the given offset.
func findWordBoundaries(content string, offset int) (start, end int) {
	if offset >= len(content) {
		return offset, offset
	}

	runes := []rune(content)

	// Convert byte offset to rune offset correctly using utf8.RuneCountInString
	// This handles multi-byte UTF-8 characters properly
	runeOffset := utf8.RuneCountInString(content[:offset])

	// Adjust if we're past the end
	if runeOffset >= len(runes) {
		runeOffset = len(runes) - 1
	}
	if runeOffset < 0 {
		return 0, 0
	}

	// Check if we're on a word character
	isWordChar := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
	}

	// If not on a word character, select just this character
	if !isWordChar(runes[runeOffset]) {
		byteStart := len(string(runes[:runeOffset]))
		byteEnd := len(string(runes[:runeOffset+1]))
		return byteStart, byteEnd
	}

	// Find word start
	wordStart := runeOffset
	for wordStart > 0 && isWordChar(runes[wordStart-1]) {
		wordStart--
	}

	// Find word end
	wordEnd := runeOffset
	for wordEnd < len(runes)-1 && isWordChar(runes[wordEnd+1]) {
		wordEnd++
	}
	wordEnd++ // Include the last character

	// Convert rune offsets back to byte offsets
	byteStart := len(string(runes[:wordStart]))
	byteEnd := len(string(runes[:wordEnd]))

	return byteStart, byteEnd
}

// extractSelectedText extracts the selected text from the viewport content.
func (m *ChatModel) extractSelectedText() string {
	start, end := m.selectionStart, m.selectionEnd
	if start > end {
		start, end = end, start
	}

	// Get viewport content (stripped of ANSI codes)
	content := ansi.Strip(m.viewport.View())

	if start < 0 {
		start = 0
	}
	if end > len(content) {
		end = len(content)
	}
	if start >= end || start >= len(content) {
		return ""
	}

	return strings.TrimSpace(content[start:end])
}

// clearSelection clears the current text selection.
func (m *ChatModel) clearSelection() {
	m.isSelecting = false
	m.selectionStart = 0
	m.selectionEnd = 0
	m.mouseDown = false
}

// hasSelection returns true if there is an active text selection.
func (m *ChatModel) hasSelection() bool {
	return m.isSelecting && m.selectionStart != m.selectionEnd
}

// applySelectionHighlight applies visual highlighting to the selected text region.
func (m *ChatModel) applySelectionHighlight(content, selStyle string) string {
	start, end := m.selectionStart, m.selectionEnd
	if start > end {
		start, end = end, start
	}

	// Strip ANSI for position calculation
	stripped := ansi.Strip(content)

	if start < 0 || end > len(stripped) || start >= end {
		return content
	}

	// For simplicity, we'll work on stripped content and apply highlighting
	// This loses existing styling in the selected region but is simpler
	lines := strings.Split(stripped, "\n")
	linePositions := m.calculateLinePositions(lines)

	var result []string
	for i, line := range lines {
		lineStart := linePositions[i]
		lineEnd := linePositions[i+1] - 1 // -1 to exclude newline

		// Check if selection overlaps this line
		if end > lineStart && start < lineEnd {
			// Calculate highlight region for this line
			highlightStart := 0
			if start > lineStart {
				highlightStart = start - lineStart
			}

			highlightEnd := len(line)
			if end < lineEnd {
				highlightEnd = end - lineStart
			}

			if highlightStart < len(line) && highlightStart < highlightEnd {
				if highlightEnd > len(line) {
					highlightEnd = len(line)
				}

				// Apply highlight style
				before := line[:highlightStart]
				highlighted := selStyle + line[highlightStart:highlightEnd] + "\033[0m"
				after := ""
				if highlightEnd < len(line) {
					after = line[highlightEnd:]
				}
				result = append(result, before+highlighted+after)
				continue
			}
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// HasSelection returns true if there is an active text selection in the viewport.
// This is used by the parent app to intercept ctrl+c for copying selected text.
func (m *ChatModel) HasSelection() bool {
	return m.isSelecting && m.selectionStart != m.selectionEnd
}

// CopySelection copies the currently selected text to the system clipboard.
// Returns a command that sends CopyToClipboardMsg.
func (m *ChatModel) CopySelection() tea.Cmd {
	if !m.hasSelection() {
		return nil
	}

	text := m.extractSelectedText()
	if text == "" {
		return nil
	}

	// Clear selection after copying
	m.clearSelection()

	return func() tea.Msg {
		return CopyToClipboardMsg{Text: text}
	}
}
