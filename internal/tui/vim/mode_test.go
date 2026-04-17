package vim

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestZeroKeyMovesToStartOfLine verifies that pressing "0" in normal mode
// (with no accumulated count prefix) emits an ActionMoveStartOfLine.
func TestZeroKeyMovesToStartOfLine(t *testing.T) {
	s := NewState()
	s.Enabled = true
	s.Mode = ModeNormal

	action, handled := s.handleNormalMode("0", tea.KeyMsg{})

	if !handled {
		t.Fatalf("expected '0' to be handled in normal mode")
	}
	if action.Type != ActionMoveStartOfLine {
		t.Errorf("action.Type = %v, want ActionMoveStartOfLine", action.Type)
	}
	if action.Count != 1 {
		t.Errorf("action.Count = %d, want 1", action.Count)
	}
}

// TestZeroKeyContinuesCountPrefix verifies that "0" pressed mid-count
// (e.g. "1","0" -> count 10) accumulates rather than jumping to the line
// start.
func TestZeroKeyContinuesCountPrefix(t *testing.T) {
	s := NewState()
	s.Enabled = true
	s.Mode = ModeNormal

	// First: "1" starts a count
	_, _ = s.handleNormalMode("1", tea.KeyMsg{})
	if s.Count != 1 {
		t.Fatalf("after '1', s.Count = %d, want 1", s.Count)
	}

	// Then: "0" should extend the count to 10, not emit an action.
	action, handled := s.handleNormalMode("0", tea.KeyMsg{})
	if !handled {
		t.Fatalf("expected '0' to be handled")
	}
	if action.Type != ActionNone {
		t.Errorf("action.Type = %v, want ActionNone (count prefix)", action.Type)
	}
	if s.Count != 10 {
		t.Errorf("after '1','0', s.Count = %d, want 10", s.Count)
	}
}
