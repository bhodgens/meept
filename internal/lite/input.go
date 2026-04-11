// Package lite provides the lightweight TUI components for meept-lite.
package lite

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// maxHistorySize is the maximum number of sent messages to remember.
const maxHistorySize = 100

// Input is a shell-like input component with history support.
type Input struct {
	textarea textarea.Model // from bubbles
	width    int

	// Per-session history
	sessionID      string              // current session ID
	sessionHistory map[string][]string // history per session
	histIdx        int                 // current history position (-1 = not browsing)
	saved          string              // saved input when browsing history

	// Styles
	promptStyle lipgloss.Style
}

// NewInput creates a new shell-like input component.
func NewInput() *Input {
	ta := textarea.New()
	ta.Placeholder = ""
	ta.Prompt = "> "
	ta.CharLimit = 0 // No limit
	ta.SetWidth(80)
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Focus()

	// Disable default newline insertion - we handle it manually
	ta.KeyMap.InsertNewline.SetEnabled(false)

	// Style the textarea to look like a shell prompt
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#F97316"))
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))

	return &Input{
		textarea:       ta,
		sessionHistory: make(map[string][]string),
		histIdx:        -1,
		width:          80,
		promptStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F97316")),
	}
}

// SetSize updates the input width.
func (i *Input) SetSize(width int) {
	i.width = width
	// Account for prompt "> " (2 chars) and some padding
	i.textarea.SetWidth(width - 4)
}

// Update handles messages and returns a command and whether a message was sent.
func (i *Input) Update(msg tea.Msg) (tea.Cmd, bool) {
	keyMsg, isKey := msg.(tea.KeyMsg)
	if !isKey {
		// Pass non-key messages to textarea
		var cmd tea.Cmd
		i.textarea, cmd = i.textarea.Update(msg)
		return cmd, false
	}

	// Handle our custom keybindings
	switch keyMsg.String() {
	case "enter":
		// Send message if there's content
		value := strings.TrimSpace(i.textarea.Value())
		if value != "" {
			return nil, true
		}
		return nil, false

	case "alt+enter", "ctrl+j":
		// Insert newline for multiline input
		i.textarea.InsertRune('\n')
		return nil, false

	case "ctrl+left":
		// Move cursor to previous word
		i.moveCursorByWord(-1)
		return nil, false

	case "ctrl+right":
		// Move cursor to next word
		i.moveCursorByWord(1)
		return nil, false

	case "ctrl+a", "home":
		// Move to start of line
		i.moveToLineStart()
		return nil, false

	case "ctrl+e", "end":
		// Move to end of line
		i.moveToLineEnd()
		return nil, false

	case "ctrl+k":
		// Delete from cursor to end of line
		i.deleteToEnd()
		return nil, false

	case "ctrl+u":
		// Delete from cursor to start of line
		i.deleteToStart()
		return nil, false

	case "up":
		// Navigate history (previous)
		if i.navigateHistory(-1) {
			return nil, false
		}
		// If no history navigation, let textarea handle it for multiline
		var cmd tea.Cmd
		i.textarea, cmd = i.textarea.Update(msg)
		return cmd, false

	case "down":
		// Navigate history (next)
		if i.navigateHistory(1) {
			return nil, false
		}
		// If no history navigation, let textarea handle it for multiline
		var cmd tea.Cmd
		i.textarea, cmd = i.textarea.Update(msg)
		return cmd, false
	}

	// Pass other keys to textarea
	var cmd tea.Cmd
	i.textarea, cmd = i.textarea.Update(msg)
	return cmd, false
}

// View renders the input component.
func (i *Input) View() string {
	return i.textarea.View()
}

// Value returns the current input value.
func (i *Input) Value() string {
	return i.textarea.Value()
}

// SetValue sets the input value.
func (i *Input) SetValue(s string) {
	i.textarea.SetValue(s)
}

// AddHistory adds a message to the history for the current session.
func (i *Input) AddHistory(s string) {
	if s == "" || i.sessionID == "" {
		return
	}

	history := i.sessionHistory[i.sessionID]

	// Don't add duplicate of last entry
	if len(history) > 0 && history[len(history)-1] == s {
		return
	}

	history = append(history, s)

	// Trim history if too large
	if len(history) > maxHistorySize {
		history = history[1:]
	}

	i.sessionHistory[i.sessionID] = history

	// Reset history browsing
	i.histIdx = -1
	i.saved = ""
}

// Focus gives the input focus.
func (i *Input) Focus() {
	i.textarea.Focus()
}

// Blur removes focus from the input.
func (i *Input) Blur() {
	i.textarea.Blur()
}

// Reset clears the input and resets history navigation.
func (i *Input) Reset() {
	i.textarea.Reset()
	i.histIdx = -1
	i.saved = ""
}

// navigateHistory moves through history. Returns true if navigation occurred.
func (i *Input) navigateHistory(direction int) bool {
	history := i.currentHistory()
	if len(history) == 0 {
		return false
	}

	// Check if we're on a single line - only navigate history on single line
	value := i.textarea.Value()
	if strings.Contains(value, "\n") {
		// Multiline input - let textarea handle up/down
		return false
	}

	if i.histIdx == -1 {
		// Starting to browse - save current input
		i.saved = value
		if direction < 0 {
			// Going up - start at end of history
			i.histIdx = len(history) - 1
		} else {
			// Going down from current input - nothing to do
			return false
		}
	} else {
		newIdx := i.histIdx + direction
		if newIdx < 0 {
			// Already at oldest entry
			return true
		} else if newIdx >= len(history) {
			// Back to current input
			i.histIdx = -1
			i.textarea.SetValue(i.saved)
			return true
		}
		i.histIdx = newIdx
	}

	// Set textarea to history entry
	i.textarea.SetValue(history[i.histIdx])
	// Move cursor to end
	i.moveToLineEnd()
	return true
}

// moveCursorByWord moves the cursor by word in the given direction.
func (i *Input) moveCursorByWord(direction int) {
	value := i.textarea.Value()
	if value == "" {
		return
	}

	// Get current cursor position using Line/LineInfo
	row := i.textarea.Line()
	lineInfo := i.textarea.LineInfo()
	col := lineInfo.CharOffset

	// Convert to linear position
	lines := strings.Split(value, "\n")
	pos := 0
	for lineIdx := 0; lineIdx < row && lineIdx < len(lines); lineIdx++ {
		pos += len(lines[lineIdx]) + 1 // +1 for newline
	}
	pos += col

	runes := []rune(value)
	if direction < 0 {
		// Move backward
		pos--
		// Skip whitespace
		for pos > 0 && isWhitespace(runes[pos]) {
			pos--
		}
		// Skip word characters
		for pos > 0 && !isWhitespace(runes[pos-1]) {
			pos--
		}
	} else {
		// Move forward
		// Skip word characters
		for pos < len(runes) && !isWhitespace(runes[pos]) {
			pos++
		}
		// Skip whitespace
		for pos < len(runes) && isWhitespace(runes[pos]) {
			pos++
		}
	}

	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}

	// Convert back to row/col and set cursor
	i.setCursorPosition(pos)
}

// moveToLineStart moves cursor to start of current line.
func (i *Input) moveToLineStart() {
	value := i.textarea.Value()
	if value == "" {
		return
	}

	row := i.textarea.Line()
	lines := strings.Split(value, "\n")

	// Calculate position at start of current line
	pos := 0
	for lineIdx := 0; lineIdx < row && lineIdx < len(lines); lineIdx++ {
		pos += len(lines[lineIdx]) + 1
	}

	i.setCursorPosition(pos)
}

// moveToLineEnd moves cursor to end of current line.
func (i *Input) moveToLineEnd() {
	value := i.textarea.Value()
	if value == "" {
		return
	}

	row := i.textarea.Line()
	lines := strings.Split(value, "\n")

	// Calculate position at end of current line
	pos := 0
	for lineIdx := 0; lineIdx <= row && lineIdx < len(lines); lineIdx++ {
		if lineIdx == row {
			pos += len(lines[lineIdx])
		} else {
			pos += len(lines[lineIdx]) + 1
		}
	}

	i.setCursorPosition(pos)
}

// deleteToEnd deletes from cursor to end of current line.
func (i *Input) deleteToEnd() {
	value := i.textarea.Value()
	if value == "" {
		return
	}

	row := i.textarea.Line()
	lineInfo := i.textarea.LineInfo()
	col := lineInfo.CharOffset
	lines := strings.Split(value, "\n")

	if row >= len(lines) {
		return
	}

	// Calculate the part to keep
	line := lines[row]
	if col >= len(line) {
		// At end of line, delete the newline if not last line
		if row < len(lines)-1 {
			lines[row] = line + lines[row+1]
			lines = append(lines[:row+1], lines[row+2:]...)
		}
	} else {
		// Delete from cursor to end of line
		lines[row] = line[:col]
	}

	newValue := strings.Join(lines, "\n")
	i.textarea.SetValue(newValue)

	// Restore cursor position
	pos := 0
	for lineIdx := 0; lineIdx < row && lineIdx < len(lines); lineIdx++ {
		pos += len(lines[lineIdx]) + 1
	}
	pos += col
	i.setCursorPosition(pos)
}

// deleteToStart deletes from cursor to start of current line.
func (i *Input) deleteToStart() {
	value := i.textarea.Value()
	if value == "" {
		return
	}

	row := i.textarea.Line()
	lineInfo := i.textarea.LineInfo()
	col := lineInfo.CharOffset
	lines := strings.Split(value, "\n")

	if row >= len(lines) {
		return
	}

	line := lines[row]
	if col == 0 {
		// At start of line, merge with previous line
		if row > 0 {
			prevLineLen := len(lines[row-1])
			lines[row-1] = lines[row-1] + line
			lines = append(lines[:row], lines[row+1:]...)
			// Move cursor to end of previous line
			newValue := strings.Join(lines, "\n")
			i.textarea.SetValue(newValue)
			pos := 0
			for lineIdx := 0; lineIdx < row-1 && lineIdx < len(lines); lineIdx++ {
				pos += len(lines[lineIdx]) + 1
			}
			pos += prevLineLen
			i.setCursorPosition(pos)
			return
		}
	} else {
		// Delete from start of line to cursor
		lines[row] = line[col:]
	}

	newValue := strings.Join(lines, "\n")
	i.textarea.SetValue(newValue)

	// Move cursor to start of line
	pos := 0
	for lineIdx := 0; lineIdx < row && lineIdx < len(lines); lineIdx++ {
		pos += len(lines[lineIdx]) + 1
	}
	i.setCursorPosition(pos)
}

// setCursorPosition sets the cursor to a linear position in the text.
func (i *Input) setCursorPosition(pos int) {
	value := i.textarea.Value()
	runes := []rune(value)

	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}

	// Convert linear position to row/col
	row := 0
	col := 0
	for idx := 0; idx < pos; idx++ {
		if runes[idx] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}

	// Use SetCursor which takes row, col
	i.textarea.SetCursor(col)
	// Move to correct row by setting value and repositioning
	// The textarea.SetCursor only sets column, we need to navigate rows
	lines := strings.Split(value, "\n")
	if row < len(lines) {
		// Reset and manually position
		i.textarea.SetValue(value)
		// Navigate to row using internal positioning
		for r := 0; r < row; r++ {
			i.textarea.CursorDown()
		}
		i.textarea.SetCursor(col)
	}
}

// isWhitespace returns true if the rune is whitespace.
func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// IsFocused returns whether the input has focus.
func (i *Input) IsFocused() bool {
	return i.textarea.Focused()
}

// Height returns the current height of the input (number of lines).
func (i *Input) Height() int {
	value := i.textarea.Value()
	if value == "" {
		return 1
	}
	return strings.Count(value, "\n") + 1
}

// SetHeight sets the display height of the textarea.
func (i *Input) SetHeight(height int) {
	if height < 1 {
		height = 1
	}
	i.textarea.SetHeight(height)
}

// HistoryLen returns the number of entries in history for the current session.
func (i *Input) HistoryLen() int {
	return len(i.currentHistory())
}

// ClearHistory clears the input history for the current session.
func (i *Input) ClearHistory() {
	if i.sessionID != "" {
		delete(i.sessionHistory, i.sessionID)
	}
	i.histIdx = -1
	i.saved = ""
}

// SetSessionID sets the current session ID for per-session history.
func (i *Input) SetSessionID(id string) {
	// Reset history browsing when switching sessions
	if i.sessionID != id {
		i.histIdx = -1
		i.saved = ""
	}
	i.sessionID = id
}

// SessionID returns the current session ID.
func (i *Input) SessionID() string {
	return i.sessionID
}

// IsBrowsingHistory returns whether the user is currently browsing history.
func (i *Input) IsBrowsingHistory() bool {
	return i.histIdx >= 0
}

// HistoryIndicator returns a string like "[history 2/5]" when browsing history.
// Returns empty string when not browsing.
func (i *Input) HistoryIndicator() string {
	if !i.IsBrowsingHistory() {
		return ""
	}
	history := i.currentHistory()
	// histIdx is 0-based, display as 1-based position
	// When browsing, histIdx 0 means oldest (position 1), histIdx len-1 means newest
	position := len(history) - i.histIdx
	return fmt.Sprintf("[history %d/%d]", position, len(history))
}

// currentHistory returns the history for the current session.
func (i *Input) currentHistory() []string {
	if i.sessionID == "" {
		return nil
	}
	return i.sessionHistory[i.sessionID]
}
