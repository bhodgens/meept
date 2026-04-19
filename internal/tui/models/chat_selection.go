// Package models provides the view models for the TUI.
package models

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// MessagePosition tracks where a message appears in rendered content.
type MessagePosition struct {
	MsgIdx       int
	LineStart    int // Line number in rendered content
	LineCount    int // Number of lines this message spans
	ContentStart int // Character offset in viewport content
}

// buildPositionIndex builds a position index for the rendered content.
func (m *ChatModel) buildPositionIndex() []MessagePosition {
	var positions []MessagePosition
	currentLine := 0
	currentOffset := 0

	content := m.viewport.View()
	lines := strings.Split(content, "\n")

	for i, msg := range m.messages {
		msgContent := m.getMessageContent(msg)
		msgLines := strings.Count(msgContent, "\n") + 1

		// Account for timestamp header line (non-system/pending messages)
		if msg.Role != "system" && msg.Role != "pending" {
			msgLines++ // timestamp header
		}

		// Account for state indicator line
		if msg.State == MessageCollapsed || msg.State == MessageExpanded {
			msgLines++
		}

		// Account for separator line
		if i < len(m.messages)-1 {
			msgLines++
		}

		positions = append(positions, MessagePosition{
			MsgIdx:       i,
			LineStart:    currentLine,
			LineCount:    msgLines,
			ContentStart: currentOffset,
		})

		// Update counters
		for j := 0; j < msgLines && currentLine+j < len(lines); j++ {
			currentOffset += len(lines[currentLine+j]) + 1 // +1 for newline
		}
		currentLine += msgLines
	}

	return positions
}

// messageAtY finds the message at a given viewport Y coordinate.
// Returns message index, line within message, and character offset.
func (m *ChatModel) messageAtY(y int) (msgIdx int, lineInMsg int, charOffset int) {
	positions := m.buildPositionIndex()

	// Adjust y for viewport scroll offset
	adjustedY := y + m.viewport.YOffset()

	for _, pos := range positions {
		if adjustedY >= pos.LineStart && adjustedY < pos.LineStart+pos.LineCount {
			return pos.MsgIdx, adjustedY - pos.LineStart, pos.ContentStart
		}
	}

	// Default to last message if not found
	if len(positions) > 0 {
		last := positions[len(positions)-1]
		return last.MsgIdx, 0, last.ContentStart
	}
	return -1, 0, 0
}

// calculateCursorOffset converts viewport Y,X to character offset in rendered content.
// This accounts for line wrapping and viewport scrolling.
func (m *ChatModel) calculateCursorOffset(y, x int) int {
	content := ansi.Strip(m.viewport.View())
	lines := strings.Split(content, "\n")

	// Adjust y for viewport scroll offset
	adjustedY := y + m.viewport.YOffset()

	// Calculate offset up to the target line
	offset := 0
	for i := 0; i < adjustedY && i < len(lines); i++ {
		offset += len(lines[i]) + 1 // +1 for newline
	}

	// Add x offset within the target line
	if adjustedY >= 0 && adjustedY < len(lines) {
		lineLen := len(lines[adjustedY])
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

	// Adjust y for viewport scroll offset
	adjustedY := y + m.viewport.YOffset()

	if adjustedY < 0 || adjustedY >= len(lines) {
		return
	}

	// Calculate character offsets for this line
	lineStart := 0
	for i := 0; i < adjustedY; i++ {
		lineStart += len(lines[i]) + 1 // +1 for newline
	}
	lineEnd := lineStart + len(lines[adjustedY])

	// Ensure lineEnd doesn't exceed content length
	if lineEnd > len(content) {
		lineEnd = len(content)
	}

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
func (m *ChatModel) applySelectionHighlight(content string, selStyle string) string {
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
