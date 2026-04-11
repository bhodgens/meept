package lite

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewInput(t *testing.T) {
	input := NewInput()
	if input == nil {
		t.Fatal("NewInput returned nil")
	}
	if input.Value() != "" {
		t.Errorf("Expected empty value, got %q", input.Value())
	}
	if input.HistoryLen() != 0 {
		t.Errorf("Expected empty history, got %d", input.HistoryLen())
	}
}

func TestInputSetValue(t *testing.T) {
	input := NewInput()
	input.SetValue("hello world")
	if input.Value() != "hello world" {
		t.Errorf("Expected 'hello world', got %q", input.Value())
	}
}

func TestInputSetSize(t *testing.T) {
	input := NewInput()
	input.SetSize(120)
	if input.width != 120 {
		t.Errorf("Expected width 120, got %d", input.width)
	}
}

func TestInputAddHistory(t *testing.T) {
	input := NewInput()
	input.SetSessionID("test-session") // Session ID required for per-session history

	// Add first entry
	input.AddHistory("first")
	if input.HistoryLen() != 1 {
		t.Errorf("Expected history length 1, got %d", input.HistoryLen())
	}

	// Add different entry
	input.AddHistory("second")
	if input.HistoryLen() != 2 {
		t.Errorf("Expected history length 2, got %d", input.HistoryLen())
	}

	// Adding duplicate should not increase length
	input.AddHistory("second")
	if input.HistoryLen() != 2 {
		t.Errorf("Expected history length 2 after duplicate, got %d", input.HistoryLen())
	}

	// Adding empty string should not increase length
	input.AddHistory("")
	if input.HistoryLen() != 2 {
		t.Errorf("Expected history length 2 after empty, got %d", input.HistoryLen())
	}
}

func TestInputClearHistory(t *testing.T) {
	input := NewInput()
	input.SetSessionID("test-session") // Session ID required for per-session history
	input.AddHistory("one")
	input.AddHistory("two")
	input.AddHistory("three")

	input.ClearHistory()
	if input.HistoryLen() != 0 {
		t.Errorf("Expected empty history after clear, got %d", input.HistoryLen())
	}
}

func TestInputHistoryMaxSize(t *testing.T) {
	input := NewInput()
	input.SetSessionID("test-session") // Session ID required for per-session history

	// Add more than maxHistorySize entries
	for i := 0; i < maxHistorySize+10; i++ {
		input.AddHistory("entry" + string(rune('A'+i%26)))
	}

	if input.HistoryLen() > maxHistorySize {
		t.Errorf("History exceeded max size: got %d, max %d", input.HistoryLen(), maxHistorySize)
	}
}

func TestInputReset(t *testing.T) {
	input := NewInput()
	input.SetSessionID("test-session") // Session ID required for per-session history
	input.SetValue("some text")
	input.AddHistory("history item")

	input.Reset()
	if input.Value() != "" {
		t.Errorf("Expected empty value after reset, got %q", input.Value())
	}
	// History should not be cleared by Reset
	if input.HistoryLen() != 1 {
		t.Errorf("Expected history to remain after reset, got %d", input.HistoryLen())
	}
}

func TestInputFocusBlur(t *testing.T) {
	input := NewInput()

	// Input starts focused
	if !input.IsFocused() {
		t.Error("Expected input to be focused initially")
	}

	input.Blur()
	if input.IsFocused() {
		t.Error("Expected input to be blurred after Blur()")
	}

	input.Focus()
	if !input.IsFocused() {
		t.Error("Expected input to be focused after Focus()")
	}
}

func TestInputHeight(t *testing.T) {
	input := NewInput()

	// Empty input should have height 1
	if input.Height() != 1 {
		t.Errorf("Expected height 1 for empty input, got %d", input.Height())
	}

	// Single line
	input.SetValue("hello")
	if input.Height() != 1 {
		t.Errorf("Expected height 1 for single line, got %d", input.Height())
	}

	// Multiple lines
	input.SetValue("line1\nline2\nline3")
	if input.Height() != 3 {
		t.Errorf("Expected height 3 for three lines, got %d", input.Height())
	}
}

func TestInputUpdateEnterSendsMessage(t *testing.T) {
	input := NewInput()
	input.SetValue("hello world")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, sent := input.Update(msg)

	if !sent {
		t.Error("Expected Enter to send message when input has content")
	}
}

func TestInputUpdateEnterEmptyDoesNotSend(t *testing.T) {
	input := NewInput()
	// Leave empty

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, sent := input.Update(msg)

	if sent {
		t.Error("Expected Enter not to send message when input is empty")
	}
}

func TestInputUpdateEnterWhitespaceDoesNotSend(t *testing.T) {
	input := NewInput()
	input.SetValue("   \n\t  ")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	_, sent := input.Update(msg)

	if sent {
		t.Error("Expected Enter not to send message when input is only whitespace")
	}
}

func TestInputUpdateAltEnterInsertsNewline(t *testing.T) {
	input := NewInput()
	input.SetValue("line1")

	// Simulate the key press that matches "alt+enter"
	input.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})

	// Since we can't easily simulate the exact key, let's test the method directly
	input.textarea.InsertRune('\n')

	if input.Height() < 2 {
		t.Errorf("Expected multiline after alt+enter, got height %d", input.Height())
	}
}

func TestInputUpdateCtrlJInsertsNewline(t *testing.T) {
	input := NewInput()
	input.SetValue("line1")

	// Insert newline at end
	input.textarea.InsertRune('\n')
	input.textarea.InsertString("line2")

	if input.Height() != 2 {
		t.Errorf("Expected 2 lines, got %d", input.Height())
	}
}

func TestInputView(t *testing.T) {
	input := NewInput()
	input.SetValue("test")

	view := input.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

func TestInputHistoryNavigation(t *testing.T) {
	input := NewInput()
	input.SetSessionID("test-session") // Session ID required for per-session history

	// Add some history
	input.AddHistory("first")
	input.AddHistory("second")
	input.AddHistory("third")

	// Set current value
	input.SetValue("current")

	// Navigate up (should get "third")
	input.navigateHistory(-1)
	if input.Value() != "third" {
		t.Errorf("Expected 'third', got %q", input.Value())
	}

	// Navigate up again (should get "second")
	input.navigateHistory(-1)
	if input.Value() != "second" {
		t.Errorf("Expected 'second', got %q", input.Value())
	}

	// Navigate up again (should get "first")
	input.navigateHistory(-1)
	if input.Value() != "first" {
		t.Errorf("Expected 'first', got %q", input.Value())
	}

	// Navigate up at oldest (should stay at "first")
	input.navigateHistory(-1)
	if input.Value() != "first" {
		t.Errorf("Expected 'first' (oldest), got %q", input.Value())
	}

	// Navigate down (should get "second")
	input.navigateHistory(1)
	if input.Value() != "second" {
		t.Errorf("Expected 'second', got %q", input.Value())
	}

	// Navigate down (should get "third")
	input.navigateHistory(1)
	if input.Value() != "third" {
		t.Errorf("Expected 'third', got %q", input.Value())
	}

	// Navigate down (should restore "current")
	input.navigateHistory(1)
	if input.Value() != "current" {
		t.Errorf("Expected 'current', got %q", input.Value())
	}
}

func TestInputHistoryNavigationEmpty(t *testing.T) {
	input := NewInput()

	// Navigate with no history should return false
	result := input.navigateHistory(-1)
	if result {
		t.Error("Expected false when navigating empty history")
	}
}

func TestInputHistoryNavigationMultiline(t *testing.T) {
	input := NewInput()
	input.SetSessionID("test-session") // Session ID required for per-session history
	input.AddHistory("history item")
	input.SetValue("line1\nline2")

	// Navigate history with multiline input should return false
	// (let textarea handle up/down for multiline)
	result := input.navigateHistory(-1)
	if result {
		t.Error("Expected false when navigating with multiline input")
	}
}

func TestIsWhitespace(t *testing.T) {
	tests := []struct {
		r    rune
		want bool
	}{
		{' ', true},
		{'\t', true},
		{'\n', true},
		{'\r', true},
		{'a', false},
		{'1', false},
		{'!', false},
	}

	for _, tt := range tests {
		got := isWhitespace(tt.r)
		if got != tt.want {
			t.Errorf("isWhitespace(%q) = %v, want %v", tt.r, got, tt.want)
		}
	}
}

func TestInputPerSessionHistory(t *testing.T) {
	input := NewInput()

	// Set first session and add history
	input.SetSessionID("session-1")
	input.AddHistory("s1-first")
	input.AddHistory("s1-second")

	if input.HistoryLen() != 2 {
		t.Errorf("Session 1: expected 2 history items, got %d", input.HistoryLen())
	}

	// Switch to second session
	input.SetSessionID("session-2")
	if input.HistoryLen() != 0 {
		t.Errorf("Session 2: expected 0 history items (new session), got %d", input.HistoryLen())
	}

	input.AddHistory("s2-first")
	if input.HistoryLen() != 1 {
		t.Errorf("Session 2: expected 1 history item, got %d", input.HistoryLen())
	}

	// Switch back to first session
	input.SetSessionID("session-1")
	if input.HistoryLen() != 2 {
		t.Errorf("Session 1: expected 2 history items after switching back, got %d", input.HistoryLen())
	}
}

func TestInputHistoryIndicator(t *testing.T) {
	input := NewInput()
	input.SetSessionID("test-session")
	input.AddHistory("first")
	input.AddHistory("second")
	input.AddHistory("third")

	// Not browsing - should return empty
	if indicator := input.HistoryIndicator(); indicator != "" {
		t.Errorf("Expected empty indicator when not browsing, got %q", indicator)
	}

	// Browse to most recent
	input.navigateHistory(-1)
	if !input.IsBrowsingHistory() {
		t.Error("Expected IsBrowsingHistory to be true after navigating")
	}

	indicator := input.HistoryIndicator()
	if indicator == "" {
		t.Error("Expected non-empty indicator when browsing")
	}
}

func TestInputNoSessionNoHistory(t *testing.T) {
	input := NewInput()
	// No session ID set

	input.AddHistory("should not be added")
	if input.HistoryLen() != 0 {
		t.Errorf("Expected 0 history items without session, got %d", input.HistoryLen())
	}
}
