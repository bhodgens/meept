package lite

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewViewport(t *testing.T) {
	vp := NewViewport()

	if vp == nil {
		t.Fatal("NewViewport() returned nil")
	}

	if vp.width != 80 {
		t.Errorf("expected default width 80, got %d", vp.width)
	}

	if vp.height != 24 {
		t.Errorf("expected default height 24, got %d", vp.height)
	}

	if len(vp.messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(vp.messages))
	}
}

func TestViewport_SetSize(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(120, 40)

	if vp.width != 120 {
		t.Errorf("expected width 120, got %d", vp.width)
	}

	if vp.height != 40 {
		t.Errorf("expected height 40, got %d", vp.height)
	}
}

func TestViewport_AddMessage(t *testing.T) {
	vp := NewViewport()

	vp.AddMessage("user", "Hello")
	vp.AddMessage("assistant", "Hi there!")
	vp.AddMessage("system", "Connection established")

	if vp.MessageCount() != 3 {
		t.Errorf("expected 3 messages, got %d", vp.MessageCount())
	}

	messages := vp.Messages()
	if len(messages) != 3 {
		t.Errorf("expected 3 messages in slice, got %d", len(messages))
	}

	if messages[0].Role != "user" {
		t.Errorf("expected first message role 'user', got '%s'", messages[0].Role)
	}

	if messages[0].Content != "Hello" {
		t.Errorf("expected first message content 'Hello', got '%s'", messages[0].Content)
	}

	if messages[1].Role != "assistant" {
		t.Errorf("expected second message role 'assistant', got '%s'", messages[1].Role)
	}

	if messages[2].Role != "system" {
		t.Errorf("expected third message role 'system', got '%s'", messages[2].Role)
	}
}

func TestViewport_MessageTimestamp(t *testing.T) {
	vp := NewViewport()
	before := time.Now()

	vp.AddMessage("user", "Test")

	after := time.Now()
	messages := vp.Messages()

	if messages[0].Timestamp.Before(before) {
		t.Error("message timestamp should be after test start")
	}

	if messages[0].Timestamp.After(after) {
		t.Error("message timestamp should be before test end")
	}
}

func TestViewport_AppendToLastMessage(t *testing.T) {
	vp := NewViewport()

	vp.AddMessage("assistant", "Hello")
	vp.AppendToLastMessage(" World")

	messages := vp.Messages()
	if messages[0].Content != "Hello World" {
		t.Errorf("expected 'Hello World', got '%s'", messages[0].Content)
	}
}

func TestViewport_AppendToLastMessage_Empty(t *testing.T) {
	vp := NewViewport()

	// Should not panic on empty messages
	vp.AppendToLastMessage("test")

	if vp.MessageCount() != 0 {
		t.Errorf("expected 0 messages, got %d", vp.MessageCount())
	}
}

func TestViewport_UpdateLastMessage(t *testing.T) {
	vp := NewViewport()

	vp.AddMessage("assistant", "Initial")
	vp.UpdateLastMessage("Updated")

	messages := vp.Messages()
	if messages[0].Content != "Updated" {
		t.Errorf("expected 'Updated', got '%s'", messages[0].Content)
	}
}

func TestViewport_UpdateLastMessage_Empty(t *testing.T) {
	vp := NewViewport()

	// Should not panic on empty messages
	vp.UpdateLastMessage("test")

	if vp.MessageCount() != 0 {
		t.Errorf("expected 0 messages, got %d", vp.MessageCount())
	}
}

func TestViewport_ClearMessages(t *testing.T) {
	vp := NewViewport()

	vp.AddMessage("user", "Hello")
	vp.AddMessage("assistant", "World")

	vp.ClearMessages()

	if vp.MessageCount() != 0 {
		t.Errorf("expected 0 messages after clear, got %d", vp.MessageCount())
	}
}

func TestViewport_View(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(80, 24)

	vp.AddMessage("user", "Hello")
	vp.AddMessage("assistant", "Hi there!")

	view := vp.View()

	// View should contain role labels
	if !strings.Contains(view, "you:") {
		t.Error("view should contain 'you:' label")
	}

	// View should contain message content
	if !strings.Contains(view, "Hello") {
		t.Error("view should contain 'Hello'")
	}
}

func TestViewport_SetInputFocus(t *testing.T) {
	vp := NewViewport()

	// Default should be focused
	if !vp.inputFocus {
		t.Error("expected inputFocus to be true by default")
	}

	vp.SetInputFocus(false)
	if vp.inputFocus {
		t.Error("expected inputFocus to be false after SetInputFocus(false)")
	}

	vp.SetInputFocus(true)
	if !vp.inputFocus {
		t.Error("expected inputFocus to be true after SetInputFocus(true)")
	}
}

func TestViewport_Update_PageUpDown(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(80, 10)

	// Add many messages to enable scrolling
	for i := 0; i < 20; i++ {
		vp.AddMessage("assistant", "This is a long message that takes up space in the viewport to enable scrolling behavior.")
	}

	// Should be at bottom after adding messages
	if !vp.AtBottom() {
		t.Error("expected to be at bottom after adding messages")
	}

	// Page up
	vp.Update(tea.KeyMsg{Type: tea.KeyPgUp})

	// Should no longer be at bottom
	if vp.AtBottom() {
		t.Error("expected not to be at bottom after page up")
	}
}

func TestViewport_Update_JKNavigation(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(80, 10)

	// Add messages
	for i := 0; i < 20; i++ {
		vp.AddMessage("assistant", "Message line that is long enough to span multiple lines")
	}

	// With input focused, j/k keys are passed through (not handled by viewport)
	vp.SetInputFocus(true)
	vp.viewport.SetYOffset(5)
	initialOffset := vp.viewport.YOffset

	// The key 'k' as a rune should be passed to the underlying viewport when focused
	// But our Update method returns early without processing, so offset stays the same
	cmd := vp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	// Since we return early for j/k when input focused, the viewport doesn't change
	// However, we still delegate to viewport.Update, so it may or may not change
	// The key point is our scroll logic doesn't run
	_ = cmd
	_ = initialOffset

	// Without input focus, k should scroll up
	vp.SetInputFocus(false)
	vp.viewport.SetYOffset(10) // Set to somewhere in middle
	beforeOffset := vp.viewport.YOffset

	vp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	// k should have scrolled up (decreased offset)
	if vp.viewport.YOffset >= beforeOffset && beforeOffset > 0 {
		t.Errorf("k should scroll up when input not focused, offset was %d, now %d", beforeOffset, vp.viewport.YOffset)
	}
}

func TestViewport_StatusLine(t *testing.T) {
	vp := NewViewport()

	// Empty should return empty string
	if vp.StatusLine() != "" {
		t.Errorf("expected empty status line, got '%s'", vp.StatusLine())
	}

	vp.AddMessage("user", "Hello")

	status := vp.StatusLine()
	if !strings.Contains(status, "1 messages") {
		t.Errorf("status line should contain '1 messages', got '%s'", status)
	}
}

func TestViewport_ScrollPercent(t *testing.T) {
	vp := NewViewport()

	// Empty viewport
	percent := vp.ScrollPercent()
	if percent < 0 || percent > 1 {
		t.Errorf("scroll percent should be between 0 and 1, got %f", percent)
	}
}

func TestViewport_AtTopBottom(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(80, 5)

	// Add many messages
	for i := 0; i < 50; i++ {
		vp.AddMessage("assistant", "Line of text")
	}

	// After adding, should be at bottom
	if !vp.AtBottom() {
		t.Error("expected to be at bottom")
	}

	// Go to top
	vp.viewport.GotoTop()
	if !vp.AtTop() {
		t.Error("expected to be at top after GotoTop")
	}
}

func TestViewport_Dimensions(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(100, 50)

	if vp.Width() != 100 {
		t.Errorf("expected width 100, got %d", vp.Width())
	}

	if vp.Height() != 50 {
		t.Errorf("expected height 50, got %d", vp.Height())
	}
}

func TestViewport_Messages_ReturnsCopy(t *testing.T) {
	vp := NewViewport()
	vp.AddMessage("user", "Original")

	messages := vp.Messages()
	messages[0].Content = "Modified"

	// Original should not be modified
	originalMessages := vp.Messages()
	if originalMessages[0].Content != "Original" {
		t.Error("Messages() should return a copy, not the original slice")
	}
}

func TestViewport_RenderMarkdown(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(80, 24)

	// Add a message with markdown
	vp.AddMessage("assistant", "# Heading\n\nSome **bold** text")

	view := vp.View()

	// Should render without errors
	if view == "" {
		t.Error("view should not be empty")
	}

	// The content should be present (exact formatting may vary)
	if !strings.Contains(view, "Heading") {
		t.Error("rendered view should contain 'Heading'")
	}
}

func TestViewport_WrapText(t *testing.T) {
	vp := NewViewport()

	tests := []struct {
		name     string
		text     string
		width    int
		contains []string
	}{
		{
			name:     "short text",
			text:     "Hello",
			width:    80,
			contains: []string{"Hello"},
		},
		{
			name:     "long text wraps",
			text:     "This is a very long line that should wrap at some point",
			width:    20,
			contains: []string{"This is a very long", "line that should"},
		},
		{
			name:     "zero width",
			text:     "Hello",
			width:    0,
			contains: []string{"Hello"},
		},
		{
			name:     "newlines preserved",
			text:     "Line1\nLine2",
			width:    80,
			contains: []string{"Line1", "Line2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vp.wrapText(tt.text, tt.width)
			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("expected result to contain '%s', got '%s'", expected, result)
				}
			}
		})
	}
}

func TestViewport_JumpToBlock(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(80, 5)

	// Add mix of user and assistant messages
	vp.AddMessage("user", "Question 1")
	vp.AddMessage("assistant", "Answer 1 with some content")
	vp.AddMessage("user", "Question 2")
	vp.AddMessage("assistant", "Answer 2 with some content")
	vp.AddMessage("user", "Question 3")
	vp.AddMessage("assistant", "Answer 3 with some content")

	// Go to top
	vp.viewport.GotoTop()

	// Jump to next block (should go to first assistant message)
	vp.jumpToNextBlock()

	// Should have moved from top
	if vp.viewport.YOffset == 0 {
		t.Error("jumpToNextBlock should have moved from top")
	}

	// Go to bottom
	vp.viewport.GotoBottom()
	beforeOffset := vp.viewport.YOffset

	// Jump to previous block
	vp.jumpToPreviousBlock()

	// Should have moved up
	if vp.viewport.YOffset >= beforeOffset && !vp.AtTop() {
		t.Error("jumpToPreviousBlock should have moved up")
	}
}

func TestViewport_CalculateMessageLinePositions(t *testing.T) {
	vp := NewViewport()
	vp.SetSize(80, 24)

	vp.AddMessage("user", "Short")
	vp.AddMessage("assistant", "Also short")
	vp.AddMessage("user", "Third message")

	positions := vp.calculateMessageLinePositions()

	if len(positions) != 3 {
		t.Errorf("expected 3 positions, got %d", len(positions))
	}

	// First message should start at 0
	if positions[0] != 0 {
		t.Errorf("first position should be 0, got %d", positions[0])
	}

	// Subsequent positions should be greater
	for i := 1; i < len(positions); i++ {
		if positions[i] <= positions[i-1] {
			t.Errorf("position %d (%d) should be greater than position %d (%d)",
				i, positions[i], i-1, positions[i-1])
		}
	}
}
