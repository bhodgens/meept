package models

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newFindTestModel builds a chat model pre-populated with messages for find tests.
func newFindTestModel() *ChatModel {
	m := newTestChatModel()
	m.SetSize(80, 24)
	m.addMessage(RoleUser, "hello world")
	m.addMessage(RoleAssistant, "Hello! World says hello back.")
	m.addMessage(RoleUser, "HELLO again")
	return m
}

func TestOpenFindBar(t *testing.T) {
	m := newFindTestModel()
	if m.findBarVisible {
		t.Fatal("find bar should start hidden")
	}

	// Press ctrl+f
	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if !m.findBarVisible {
		t.Error("find bar should be visible after ctrl+f")
	}
	if !m.findInput.Focused() {
		t.Error("find input should be focused after open")
	}

	// ctrl+f while open re-focuses and clears value.
	m.findInput.SetValue("stale")
	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if m.findInput.Value() != "" {
		t.Errorf("expected cleared input, got %q", m.findInput.Value())
	}
}

func TestFindBarEscCloses(t *testing.T) {
	m := newFindTestModel()
	m.Update(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m.findInput.SetValue("hello")
	m.recomputeFindMatches()
	if m.findBarVisible == false {
		t.Fatal("expected bar visible")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.findBarVisible {
		t.Error("find bar should be hidden after esc")
	}
	if m.findInput.Value() != "" {
		t.Errorf("expected cleared input, got %q", m.findInput.Value())
	}
	if len(m.findMatches) != 0 {
		t.Errorf("expected matches cleared, got %d", len(m.findMatches))
	}
}

func TestRecomputeMatches(t *testing.T) {
	m := newFindTestModel()
	m.openFindBar()
	m.findInput.SetValue("hello")
	m.recomputeFindMatches()

	// "hello" appears in: user[0] "hello world" (1), assistant[1] "Hello! World says hello back"
	// (case-insensitive: "Hello" + "hello" = 2), user[2] "HELLO again" (1) => 4 matches.
	if got := len(m.findMatches); got != 4 {
		t.Errorf("expected 4 case-insensitive matches for 'hello', got %d", got)
	}
	if m.findCursor != 0 {
		t.Errorf("expected cursor 0 after recompute, got %d", m.findCursor)
	}

	// Empty query clears matches.
	m.findInput.SetValue("")
	m.recomputeFindMatches()
	if len(m.findMatches) != 0 {
		t.Errorf("expected matches cleared on empty, got %d", len(m.findMatches))
	}
	if m.findCursor != -1 {
		t.Errorf("expected cursor -1 on empty, got %d", m.findCursor)
	}
}

func TestFindBarCaseSensitivity(t *testing.T) {
	m := newFindTestModel()
	m.openFindBar()
	m.findInput.SetValue("hello")
	m.recomputeFindMatches()
	if got := len(m.findMatches); got != 4 {
		t.Errorf("case-insensitive: expected 4 matches, got %d", got)
	}

	// Toggle case-sensitive.
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if !m.findCaseSensitive {
		t.Fatal("expected case-sensitive flag set")
	}
	// Lowercase "hello" only: user[0] "hello world" + assistant[1] "...says hello back" => 2 matches.
	if got := len(m.findMatches); got != 2 {
		t.Errorf("case-sensitive: expected 2 matches, got %d", got)
	}

	// Toggle back.
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	if m.findCaseSensitive {
		t.Error("expected case-insensitive again")
	}
	if got := len(m.findMatches); got != 4 {
		t.Errorf("case-insensitive: expected 4 matches again, got %d", got)
	}
}

func TestFindBarRegex(t *testing.T) {
	m := newFindTestModel()
	m.openFindBar()
	// Toggle regex on.
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
	if !m.findRegex {
		t.Fatal("expected regex flag set")
	}

	m.findInput.SetValue("h[el]+o")
	m.recomputeFindMatches()
	// "hello" in user[0], "Hello" in assistant[1], "HELLO" in user[2] - regex
	// is case-sensitive by default unless findCaseSensitive is false (we use (?i)).
	if got := len(m.findMatches); got < 2 {
		t.Errorf("expected at least 2 regex matches, got %d", got)
	}

	// Invalid regex should set error state and clear matches.
	m.findInput.SetValue("(unclosed")
	m.recomputeFindMatches()
	if m.findRegexError == "" {
		t.Error("expected regex error set for invalid pattern")
	}
	if len(m.findMatches) != 0 {
		t.Errorf("expected no matches on regex error, got %d", len(m.findMatches))
	}
}

func TestFindBarNavigation(t *testing.T) {
	m := newFindTestModel()
	m.openFindBar()
	m.findInput.SetValue("hello")
	m.recomputeFindMatches()
	if len(m.findMatches) == 0 {
		t.Fatal("need matches to test navigation")
	}
	initial := m.findCursor

	// Next match.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.findCursor != (initial+1)%len(m.findMatches) {
		t.Errorf("expected cursor %d after next, got %d", (initial+1)%len(m.findMatches), m.findCursor)
	}

	// Down arrow should also advance.
	downCursor := m.findCursor
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.findCursor != (downCursor+1)%len(m.findMatches) {
		t.Errorf("expected cursor advance on down, got %d (from %d)", m.findCursor, downCursor)
	}

	// Previous (wraps around).
	prevCursor := m.findCursor
	expected := prevCursor - 1
	if expected < 0 {
		expected = len(m.findMatches) - 1
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.findCursor != expected {
		t.Errorf("expected cursor %d after prev, got %d", expected, m.findCursor)
	}
}

func TestFindBarSessionChange(t *testing.T) {
	m := newFindTestModel()
	m.openFindBar()
	m.findInput.SetValue("hello")
	m.recomputeFindMatches()

	// Simulate session switch via SetSession(nil).
	cmd := m.SetSession(nil)
	if cmd != nil {
		// SetSession can return a cmd; that's fine, we care about state.
	}
	if m.findBarVisible {
		t.Error("find bar should close on session change")
	}
	if m.findInput.Value() != "" {
		t.Errorf("expected find input cleared on session change, got %q", m.findInput.Value())
	}
}

func TestFindBarMatchIndicator(t *testing.T) {
	m := newFindTestModel()
	m.openFindBar()
	m.findInput.SetValue("hello")
	m.recomputeFindMatches()
	bar := m.renderFindBar()
	// Should show "1/N" where N is total matches.
	want := "1/4"
	if !strings.Contains(bar, want) {
		t.Errorf("expected bar to contain %q, got:\n%s", want, bar)
	}

	// Move to next - indicator should advance.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	bar = m.renderFindBar()
	want = "2/4"
	if !strings.Contains(bar, want) {
		t.Errorf("expected bar to contain %q after next, got:\n%s", want, bar)
	}
}

func TestFindBarNoMatches(t *testing.T) {
	m := newFindTestModel()
	m.openFindBar()
	m.findInput.SetValue("zzzznomatch")
	m.recomputeFindMatches()
	if len(m.findMatches) != 0 {
		t.Errorf("expected zero matches, got %d", len(m.findMatches))
	}
	if m.findCursor != -1 {
		t.Errorf("expected cursor -1, got %d", m.findCursor)
	}
	bar := m.renderFindBar()
	if !strings.Contains(bar, "0/0") {
		t.Errorf("expected 0/0 indicator, got:\n%s", bar)
	}
}
