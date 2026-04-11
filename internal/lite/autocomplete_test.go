package lite

import (
	"testing"
)

func TestNewAutocompleter(t *testing.T) {
	a := NewAutocompleter()
	if a == nil {
		t.Fatal("NewAutocompleter returned nil")
	}
	if len(a.commands) != 0 {
		t.Errorf("expected empty commands, got %d", len(a.commands))
	}
	if len(a.skills) != 0 {
		t.Errorf("expected empty skills, got %d", len(a.skills))
	}
	if len(a.matches) != 0 {
		t.Errorf("expected empty matches, got %d", len(a.matches))
	}
}

func TestSetCommands(t *testing.T) {
	a := NewAutocompleter()
	cmds := []string{"help", "quit", "status"}
	a.SetCommands(cmds)

	if len(a.commands) != 3 {
		t.Errorf("expected 3 commands, got %d", len(a.commands))
	}

	// Verify sorting
	if a.commands[0] != "help" || a.commands[1] != "quit" || a.commands[2] != "status" {
		t.Errorf("commands not sorted correctly: %v", a.commands)
	}

	// Verify it's a copy, not a reference
	cmds[0] = "modified"
	if a.commands[0] == "modified" {
		t.Error("SetCommands should copy the slice, not store reference")
	}
}

func TestSetSkills(t *testing.T) {
	a := NewAutocompleter()
	skills := []string{"code-review", "analyze", "summarize"}
	a.SetSkills(skills)

	if len(a.skills) != 3 {
		t.Errorf("expected 3 skills, got %d", len(a.skills))
	}

	// Verify sorting
	if a.skills[0] != "analyze" || a.skills[1] != "code-review" || a.skills[2] != "summarize" {
		t.Errorf("skills not sorted correctly: %v", a.skills)
	}
}

func TestCompleteNoSlash(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"help", "quit"})

	result, completed := a.Complete("help")
	if completed {
		t.Error("should not complete input without leading /")
	}
	if result != "help" {
		t.Errorf("expected 'help', got %q", result)
	}
}

func TestCompleteJustSlash(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"help", "quit"})

	result, completed := a.Complete("/")
	if completed {
		t.Error("should not complete single /")
	}
	if result != "/" {
		t.Errorf("expected '/', got %q", result)
	}
}

func TestCompleteSingleMatch(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"help", "quit", "status"})

	result, completed := a.Complete("/he")
	if !completed {
		t.Error("should complete /he to /help")
	}
	if result != "/help" {
		t.Errorf("expected '/help', got %q", result)
	}
}

func TestCompleteMultipleMatches(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"session", "skills", "status"})

	// First Tab: complete to first match
	result, completed := a.Complete("/s")
	if !completed {
		t.Error("should complete /s")
	}
	if result != "/session" {
		t.Errorf("expected '/session', got %q", result)
	}

	// Second Tab: cycle to next match (with same input /s)
	result, completed = a.Complete("/s")
	if !completed {
		t.Error("should cycle to next match")
	}
	if result != "/skills" {
		t.Errorf("expected '/skills', got %q", result)
	}

	// Third Tab: cycle to next match
	result, completed = a.Complete("/s")
	if !completed {
		t.Error("should cycle to next match")
	}
	if result != "/status" {
		t.Errorf("expected '/status', got %q", result)
	}

	// Fourth Tab: wrap around to first match
	result, completed = a.Complete("/s")
	if !completed {
		t.Error("should wrap around")
	}
	if result != "/session" {
		t.Errorf("expected '/session' (wrap), got %q", result)
	}
}

func TestCompleteNoMatch(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"help", "quit"})

	result, completed := a.Complete("/xyz")
	if completed {
		t.Error("should not complete with no matches")
	}
	if result != "/xyz" {
		t.Errorf("expected '/xyz', got %q", result)
	}
}

func TestCompleteCommandsBeforeSkills(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"session"})
	a.SetSkills([]string{"summarize"})

	// Both match /s, but commands should come first
	result, completed := a.Complete("/s")
	if !completed {
		t.Error("should complete /s")
	}
	if result != "/session" {
		t.Errorf("expected command '/session' first, got %q", result)
	}

	// Cycle to skill
	result, completed = a.Complete("/s")
	if !completed {
		t.Error("should cycle to skill")
	}
	if result != "/summarize" {
		t.Errorf("expected skill '/summarize', got %q", result)
	}
}

func TestCompleteCaseInsensitive(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"Help", "QUIT"})

	// Lowercase input should match uppercase command
	result, completed := a.Complete("/he")
	if !completed {
		t.Error("should complete case-insensitively")
	}
	if result != "/Help" {
		t.Errorf("expected '/Help', got %q", result)
	}

	// Reset and try uppercase input
	a.Reset()
	result, completed = a.Complete("/HE")
	if !completed {
		t.Error("should complete case-insensitively")
	}
	if result != "/Help" {
		t.Errorf("expected '/Help', got %q", result)
	}
}

func TestReset(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"session", "skills"})

	// Build up completion state
	a.Complete("/s")
	if len(a.matches) == 0 {
		t.Fatal("expected matches after Complete")
	}

	// Reset should clear state
	a.Reset()
	if len(a.matches) != 0 {
		t.Error("Reset should clear matches")
	}
	if a.matchIdx != 0 {
		t.Error("Reset should reset matchIdx")
	}
	if a.prefix != "" {
		t.Error("Reset should clear prefix")
	}
}

func TestCompletePrefixChange(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"session", "skills", "status"})

	// First completion
	result, _ := a.Complete("/se")
	if result != "/session" {
		t.Errorf("expected '/session', got %q", result)
	}

	// Change prefix - should restart completion
	result, completed := a.Complete("/sk")
	if !completed {
		t.Error("should complete new prefix")
	}
	if result != "/skills" {
		t.Errorf("expected '/skills', got %q", result)
	}
}

func TestGetMatches(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"session", "skills"})

	a.Complete("/s")
	matches := a.GetMatches()

	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(matches))
	}

	// Verify it's a copy
	matches[0] = "modified"
	originalMatches := a.GetMatches()
	if originalMatches[0] == "modified" {
		t.Error("GetMatches should return a copy")
	}
}

func TestGetMatchCount(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"session", "skills", "status"})

	if a.GetMatchCount() != 0 {
		t.Error("expected 0 matches before Complete")
	}

	a.Complete("/s")
	if a.GetMatchCount() != 3 {
		t.Errorf("expected 3 matches, got %d", a.GetMatchCount())
	}
}

func TestGetCurrentMatchIndex(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"session", "skills"})

	if a.GetCurrentMatchIndex() != -1 {
		t.Error("expected -1 when no matches")
	}

	a.Complete("/s")
	if a.GetCurrentMatchIndex() != 0 {
		t.Error("expected index 0 after first Complete")
	}

	a.Complete("/s")
	if a.GetCurrentMatchIndex() != 1 {
		t.Error("expected index 1 after second Complete")
	}
}

func TestHasMatches(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"help"})

	if a.HasMatches() {
		t.Error("should not have matches before Complete")
	}

	a.Complete("/h")
	if !a.HasMatches() {
		t.Error("should have matches after Complete")
	}

	a.Complete("/xyz")
	if a.HasMatches() {
		t.Error("should not have matches with no completions")
	}
}

func TestCompleteExactMatch(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"help"})

	// Typing the full command should still "complete" (returns true with same text)
	result, completed := a.Complete("/help")
	if !completed {
		t.Error("exact match should still complete")
	}
	if result != "/help" {
		t.Errorf("expected '/help', got %q", result)
	}
}

func TestCompleteEmptyCommandsAndSkills(t *testing.T) {
	a := NewAutocompleter()
	// No commands or skills set

	result, completed := a.Complete("/h")
	if completed {
		t.Error("should not complete with no commands or skills")
	}
	if result != "/h" {
		t.Errorf("expected '/h', got %q", result)
	}
}

func TestCompleteSkillOnly(t *testing.T) {
	a := NewAutocompleter()
	a.SetSkills([]string{"code-review", "analyze"})

	result, completed := a.Complete("/co")
	if !completed {
		t.Error("should complete skill")
	}
	if result != "/code-review" {
		t.Errorf("expected '/code-review', got %q", result)
	}
}

func TestCyclingPreservesOrder(t *testing.T) {
	a := NewAutocompleter()
	a.SetCommands([]string{"status", "session", "skills"})

	// Should cycle in sorted order
	expected := []string{"/session", "/skills", "/status"}

	for i := 0; i < 6; i++ { // Go through twice to test wrap
		result, _ := a.Complete("/s")
		expectedIdx := i % 3
		if result != expected[expectedIdx] {
			t.Errorf("iteration %d: expected %q, got %q", i, expected[expectedIdx], result)
		}
	}
}
