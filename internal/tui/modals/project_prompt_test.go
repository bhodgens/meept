package modals

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewProjectPromptModal(t *testing.T) {
	m := NewProjectPromptModal("sess-123", "/tmp/myproject")

	if m.sessionID != "sess-123" {
		t.Errorf("sessionID = %q, want %q", m.sessionID, "sess-123")
	}
	if m.defaultPath != "/tmp/myproject" {
		t.Errorf("defaultPath = %q, want %q", m.defaultPath, "/tmp/myproject")
	}
	if m.selected != 0 {
		t.Errorf("selected = %d, want 0 (Yes)", m.selected)
	}
}

func TestProjectPromptModalInit(t *testing.T) {
	m := NewProjectPromptModal("s1", "/p")
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init should return nil, got %v", cmd)
	}
}

func TestProjectPromptModalViewContainsPathAndBindings(t *testing.T) {
	m := NewProjectPromptModal("s1", "/home/user/myproject")
	view := m.View().Content

	if !strings.Contains(view, "/home/user/myproject") {
		t.Errorf("view should contain defaultPath, got: %s", view)
	}
	if !strings.Contains(view, "y/n/p") {
		t.Errorf("view should mention key bindings (y/n/p), got: %s", view)
	}
}

func TestProjectPromptModalViewShowsOptions(t *testing.T) {
	m := NewProjectPromptModal("s1", "/p")
	view := m.View().Content

	for _, want := range []string{"Yes", "No", "Pick"} {
		if !strings.Contains(view, want) {
			t.Errorf("view should contain %q option, got: %s", want, view)
		}
	}
}

func TestProjectPromptModalUpdateY(t *testing.T) {
	m := NewProjectPromptModal("sess-y", "/p")
	_, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	if cmd == nil {
		t.Fatal("pressing 'y' should return a non-nil command")
	}
	msg := cmd()
	pc, ok := msg.(ProjectConfirmedMsg)
	if !ok {
		t.Fatalf("expected ProjectConfirmedMsg, got %T: %v", msg, msg)
	}
	if !pc.Accepted {
		t.Error("Accepted should be true for 'y'")
	}
	if pc.SessionID != "sess-y" {
		t.Errorf("SessionID = %q, want %q", pc.SessionID, "sess-y")
	}
}

func TestProjectPromptModalUpdateN(t *testing.T) {
	m := NewProjectPromptModal("sess-n", "/p")
	_, cmd := m.Update(tea.KeyPressMsg{Text: "n"})
	if cmd == nil {
		t.Fatal("pressing 'n' should return a non-nil command")
	}
	msg := cmd()
	pc, ok := msg.(ProjectConfirmedMsg)
	if !ok {
		t.Fatalf("expected ProjectConfirmedMsg, got %T: %v", msg, msg)
	}
	if pc.Accepted {
		t.Error("Accepted should be false for 'n'")
	}
	if pc.SessionID != "sess-n" {
		t.Errorf("SessionID = %q, want %q", pc.SessionID, "sess-n")
	}
}

func TestProjectPromptModalUpdateP(t *testing.T) {
	m := NewProjectPromptModal("sess-p", "/p")
	_, cmd := m.Update(tea.KeyPressMsg{Text: "p"})
	if cmd == nil {
		t.Fatal("pressing 'p' should return a non-nil command")
	}
	msg := cmd()
	pp, ok := msg.(ProjectPickRequestedMsg)
	if !ok {
		t.Fatalf("expected ProjectPickRequestedMsg, got %T: %v", msg, msg)
	}
	if pp.SessionID != "sess-p" {
		t.Errorf("SessionID = %q, want %q", pp.SessionID, "sess-p")
	}
}

func TestProjectPromptModalUpdateEnterDefaultYes(t *testing.T) {
	// Default selection is 0 (Yes), so Enter should confirm with Accepted=true.
	m := NewProjectPromptModal("sess-enter", "/p")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("pressing Enter should return a non-nil command")
	}
	msg := cmd()
	pc, ok := msg.(ProjectConfirmedMsg)
	if !ok {
		t.Fatalf("expected ProjectConfirmedMsg, got %T: %v", msg, msg)
	}
	if !pc.Accepted {
		t.Error("Accepted should be true when Enter on default selection (Yes)")
	}
}

func TestProjectPromptModalUpdateDownClampsAtTwo(t *testing.T) {
	m := NewProjectPromptModal("s1", "/p")

	// Move down from 0 to 1.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.selected != 1 {
		t.Errorf("after first down, selected = %d, want 1", m.selected)
	}

	// Move down from 1 to 2.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.selected != 2 {
		t.Errorf("after second down, selected = %d, want 2", m.selected)
	}

	// Move down from 2 -- should clamp at 2.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.selected != 2 {
		t.Errorf("after third down, selected = %d, want 2 (clamped)", m.selected)
	}
}

func TestProjectPromptModalUpdateUpClampsAtZero(t *testing.T) {
	m := NewProjectPromptModal("s1", "/p")

	// Move to 1 then back to 0.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.selected != 0 {
		t.Errorf("after down+up, selected = %d, want 0", m.selected)
	}

	// Move up from 0 -- should clamp at 0.
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.selected != 0 {
		t.Errorf("after up from 0, selected = %d, want 0 (clamped)", m.selected)
	}
}

func TestProjectPromptModalUpdateEsc(t *testing.T) {
	m := NewProjectPromptModal("s1", "/p")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("pressing esc should return a non-nil command (tea.Quit)")
	}
	// tea.Quit returns a command that produces a tea.QuitMsg.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from esc, got %T: %v", msg, msg)
	}
}

func TestProjectPromptModalUpdateJKAliases(t *testing.T) {
	t.Run("j moves down", func(t *testing.T) {
		m := NewProjectPromptModal("s1", "/p")
		m.Update(tea.KeyPressMsg{Text: "j"})
		if m.selected != 1 {
			t.Errorf("after 'j', selected = %d, want 1", m.selected)
		}
	})

	t.Run("k moves up", func(t *testing.T) {
		m := NewProjectPromptModal("s1", "/p")
		m.Update(tea.KeyPressMsg{Text: "j"}) // down to 1
		m.Update(tea.KeyPressMsg{Text: "k"}) // back to 0
		if m.selected != 0 {
			t.Errorf("after 'j'+'k', selected = %d, want 0", m.selected)
		}
	})
}

func TestProjectPromptModalEnterOnNoSelection(t *testing.T) {
	m := NewProjectPromptModal("sess-no-enter", "/p")
	// Move down once to select "No" (index 1).
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("pressing Enter on 'No' should return a non-nil command")
	}
	msg := cmd()
	pc, ok := msg.(ProjectConfirmedMsg)
	if !ok {
		t.Fatalf("expected ProjectConfirmedMsg, got %T: %v", msg, msg)
	}
	if pc.Accepted {
		t.Error("Accepted should be false when Enter on 'No' selection")
	}
}

func TestProjectPromptModalEnterOnPickSelection(t *testing.T) {
	m := NewProjectPromptModal("sess-pick-enter", "/p")
	// Move down twice to select "Pick" (index 2).
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("pressing Enter on 'Pick' should return a non-nil command")
	}
	msg := cmd()
	pp, ok := msg.(ProjectPickRequestedMsg)
	if !ok {
		t.Fatalf("expected ProjectPickRequestedMsg, got %T: %v", msg, msg)
	}
	if pp.SessionID != "sess-pick-enter" {
		t.Errorf("SessionID = %q, want %q", pp.SessionID, "sess-pick-enter")
	}
}

func TestProjectPromptModalViewMarksSelectedOption(t *testing.T) {
	m := NewProjectPromptModal("s1", "/p")

	// Default: Yes selected.
	view0 := m.View().Content
	if !strings.Contains(view0, m.styles.Selected) {
		t.Errorf("Yes should be marked selected by default, got: %s", view0)
	}

	// Move down to No.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	view1 := m.View().Content
	// Count selected markers: exactly one option should be selected.
	if strings.Count(view1, m.styles.Selected) != 1 {
		t.Errorf("exactly one option should be selected after moving to No, got: %s", view1)
	}

	// Move down to Pick.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	view2 := m.View().Content
	if strings.Count(view2, m.styles.Selected) != 1 {
		t.Errorf("exactly one option should be selected after moving to Pick, got: %s", view2)
	}
}

func TestProjectPromptModalUpdateUnknownKey(t *testing.T) {
	m := NewProjectPromptModal("s1", "/p")
	_, cmd := m.Update(tea.KeyPressMsg{Text: "x"})
	if cmd != nil {
		t.Errorf("pressing 'x' should return nil cmd, got %v", cmd)
	}
	if m.selected != 0 {
		t.Errorf("selected should remain 0 after unknown key, got %d", m.selected)
	}
}

func TestProjectPromptModalUpdateNonKeyMsg(t *testing.T) {
	m := NewProjectPromptModal("s1", "/p")
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if cmd != nil {
		t.Errorf("non-key msg should return nil cmd, got %v", cmd)
	}
}
