// Package models provides the view models for the TUI.
package models

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// HandleInputMouse handles mouse events for text selection in the input textarea.
func (m *ChatModel) HandleInputMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Action {
	case tea.MouseActionPress:
		return m.handleInputMousePress(msg)
	case tea.MouseActionRelease:
		return m.handleInputMouseRelease(msg)
	case tea.MouseActionMotion:
		if m.inputMouseDown {
			return m.handleInputMouseDrag(msg)
		}
	}
	return nil
}

// handleInputMousePress handles mouse button press for input text selection.
func (m *ChatModel) handleInputMousePress(msg tea.MouseMsg) tea.Cmd {
	m.inputMouseDown = true

	// Calculate position within textarea content
	// Textarea has 1 char left padding for border
	adjustedX := msg.X - 1
	adjustedY := msg.Y

	if adjustedX < 0 {
		adjustedX = 0
	}

	// Check for double/triple click
	now := time.Now()
	if now.Sub(m.inputLastClickTime) < 400*time.Millisecond {
		m.inputClickCount++
		if m.inputClickCount == 2 {
			m.selectInputWordAt(adjustedY, adjustedX)
			return nil
		} else if m.inputClickCount >= 3 {
			m.selectInputLineAt(adjustedY)
			m.inputClickCount = 3
			return nil
		}
	} else {
		m.inputClickCount = 1
	}
	m.inputLastClickTime = now

	// Single click: start selection
	m.inputSelectionStart = m.calculateInputCursorOffset(adjustedY, adjustedX)
	m.inputSelectionEnd = m.inputSelectionStart
	m.inputIsSelecting = true

	return nil
}

// handleInputMouseDrag handles mouse drag for extending input text selection.
func (m *ChatModel) handleInputMouseDrag(msg tea.MouseMsg) tea.Cmd {
	adjustedX := msg.X - 1
	adjustedY := msg.Y

	if adjustedX < 0 {
		adjustedX = 0
	}

	m.inputSelectionEnd = m.calculateInputCursorOffset(adjustedY, adjustedX)
	return nil
}

// handleInputMouseRelease handles mouse button release for input selection.
func (m *ChatModel) handleInputMouseRelease(msg tea.MouseMsg) tea.Cmd {
	m.inputMouseDown = false

	if m.inputIsSelecting && m.inputSelectionStart != m.inputSelectionEnd {
		selectedText := m.extractInputSelectedText()
		if selectedText != "" {
			return func() tea.Msg {
				return CopyToClipboardMsg{Text: selectedText}
			}
		}
	}

	return nil
}

// calculateInputCursorOffset converts textarea Y,X to character offset in the input content.
func (m *ChatModel) calculateInputCursorOffset(y, x int) int {
	content := m.textarea.Value()
	lines := strings.Split(content, "\n")

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

	// Clamp to content length
	if offset > len(content) {
		offset = len(content)
	}

	return offset
}

// selectInputWordAt selects the word at the given coordinates in the input textarea.
func (m *ChatModel) selectInputWordAt(y, x int) {
	offset := m.calculateInputCursorOffset(y, x)
	content := m.textarea.Value()

	if offset >= len(content) {
		return
	}

	start, end := findInputWordBoundaries(content, offset)
	m.inputSelectionStart = start
	m.inputSelectionEnd = end
	m.inputIsSelecting = true
}

// selectInputLineAt selects the entire line at the given Y coordinate in the input textarea.
func (m *ChatModel) selectInputLineAt(y int) {
	content := m.textarea.Value()
	lines := strings.Split(content, "\n")

	if y < 0 || y >= len(lines) {
		return
	}

	// Calculate character offsets for this line
	lineStart := 0
	for i := 0; i < y; i++ {
		lineStart += len(lines[i]) + 1
	}
	lineEnd := lineStart + len(lines[y])

	if lineEnd > len(content) {
		lineEnd = len(content)
	}

	m.inputSelectionStart = lineStart
	m.inputSelectionEnd = lineEnd
	m.inputIsSelecting = true
}

// findInputWordBoundaries finds the start and end offsets of the word at the given offset.
func findInputWordBoundaries(content string, offset int) (start, end int) {
	if offset >= len(content) {
		return offset, offset
	}

	runes := []rune(content)
	runeOffset := utf8.RuneCountInString(content[:offset])

	if runeOffset >= len(runes) {
		runeOffset = len(runes) - 1
	}
	if runeOffset < 0 {
		return 0, 0
	}

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
	wordEnd++

	// Convert rune offsets back to byte offsets
	byteStart := len(string(runes[:wordStart]))
	byteEnd := len(string(runes[:wordEnd]))

	return byteStart, byteEnd
}

// extractInputSelectedText extracts the selected text from the input textarea.
func (m *ChatModel) extractInputSelectedText() string {
	start, end := m.inputSelectionStart, m.inputSelectionEnd
	if start > end {
		start, end = end, start
	}

	content := m.textarea.Value()

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

// clearInputSelection clears the input text selection.
func (m *ChatModel) clearInputSelection() {
	m.inputIsSelecting = false
	m.inputSelectionStart = 0
	m.inputSelectionEnd = 0
	m.inputMouseDown = false
}

// hasInputSelection returns true if there is an active input text selection.
func (m *ChatModel) hasInputSelection() bool {
	return m.inputIsSelecting && m.inputSelectionStart != m.inputSelectionEnd
}

// applyInputSelectionHighlight applies visual highlighting to selected input text.
func (m *ChatModel) applyInputSelectionHighlight(content string, selStyle string) string {
	start, end := m.inputSelectionStart, m.inputSelectionEnd
	if start > end {
		start, end = end, start
	}

	if start < 0 || end > len(content) || start >= end {
		return content
	}

	lines := strings.Split(content, "\n")
	linePositions := make([]int, len(lines)+1)
	offset := 0
	for i, line := range lines {
		linePositions[i] = offset
		offset += len(line) + 1
	}
	linePositions[len(lines)] = offset

	var result []string
	for i, line := range lines {
		lineStart := linePositions[i]
		lineEnd := linePositions[i+1] - 1

		if end > lineStart && start < lineEnd {
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
