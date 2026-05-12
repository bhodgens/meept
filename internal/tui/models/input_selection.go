// Package models provides the view models for the TUI.
package models

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// HandleInputMouse handles mouse events for text selection in the input textarea.
// HandleInputMouse handles mouse events for text selection in the input textarea.
func (m *ChatModel) HandleInputMouse(msg tea.MouseMsg) tea.Cmd {
	// Check if mouse event is within textarea bounds
	inputStartY, inputEndY := m.getTextareaBounds()
	if inputStartY < 0 {
		return nil
	}

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		// Only handle if within textarea vertical bounds
		if mouse.Y >= inputStartY && mouse.Y <= inputEndY {
			return m.handleInputMousePress(msg)
		}
		return nil
	case tea.MouseReleaseMsg:
		if m.inputMouseDown {
			return m.handleInputMouseRelease(msg)
		}
		return nil
	case tea.MouseMotionMsg:
		if m.inputMouseDown {
			return m.handleInputMouseDrag(msg)
		}
		return nil
	}
	return nil
}

// handleInputMousePress handles mouse button press for input text selection.
func (m *ChatModel) handleInputMousePress(msg tea.MouseClickMsg) tea.Cmd {
	m.inputMouseDown = true

	mouse := msg.Mouse()

	// Calculate position within textarea content
	// Need to convert screen coordinates to textarea-relative coordinates
	inputStartY, _ := m.getTextareaBounds()

	// Adjust Y to be relative to textarea content area
	// Adjust X to account for left border/padding
	adjustedX := mouse.X - 1
	adjustedY := mouse.Y - inputStartY

	if adjustedX < 0 {
		adjustedX = 0
	}
	if adjustedY < 0 {
		adjustedY = 0
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
func (m *ChatModel) handleInputMouseDrag(msg tea.MouseMotionMsg) tea.Cmd {
	mouse := msg.Mouse()

	// Convert screen coordinates to textarea-relative coordinates
	inputStartY, _ := m.getTextareaBounds()
	adjustedX := mouse.X - 1
	adjustedY := mouse.Y - inputStartY

	if adjustedX < 0 {
		adjustedX = 0
	}
	if adjustedY < 0 {
		adjustedY = 0
	}

	m.inputSelectionEnd = m.calculateInputCursorOffset(adjustedY, adjustedX)
	return nil
}

// handleInputMouseRelease handles mouse button release for input selection.
// Text is NOT automatically copied on release - the user must explicitly
// request a copy via keyboard.
func (m *ChatModel) handleInputMouseRelease(_ tea.MouseReleaseMsg) tea.Cmd {
	m.inputMouseDown = false
	return nil
}

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
	for i := range y {
		lineStart += len(lines[i]) + 1
	}
	lineEnd := min(lineStart+len(lines[y]), len(content))

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
