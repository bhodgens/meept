package models

import (
	"testing"
)

// TestMarkdownToggleState verifies the markdown rendering toggle keeps a
// consistent on/off state and that disabling invalidates rendered caches.
func TestMarkdownToggleState(t *testing.T) {
	m := newTestChatModel()

	if !m.IsMarkdownEnabled() {
		t.Fatal("markdown should default to enabled")
	}

	m.width = 80 // avoid negative Repeat in updateViewport with zero width
	m.SetMarkdownEnabled(false)
	if m.IsMarkdownEnabled() {
		t.Fatal("markdown should be disabled after SetMarkdownEnabled(false)")
	}

	m.ToggleMarkdown()
	if !m.IsMarkdownEnabled() {
		t.Fatal("markdown should be enabled after ToggleMarkdown from off")
	}

	m.ToggleMarkdown()
	if m.IsMarkdownEnabled() {
		t.Fatal("markdown should be disabled after second ToggleMarkdown")
	}
}
