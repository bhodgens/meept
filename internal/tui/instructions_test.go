package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sampleInstructionData() InstructionConfirmationData {
	return InstructionConfirmationData{
		RiskLevel:     RiskHigh,
		Action:        "shell",
		ActionDetail:  "rm -rf /tmp/build",
		Trigger:       "cron: 0 * * * *",
		Scope:         "project",
		Priority:      "high",
		RawInput:      "every hour delete temp files",
	}
}

func TestNewInstructionConfirmationModel(t *testing.T) {
	m := NewInstructionConfirmationModel(sampleInstructionData(), nil)
	if m.IsConfirmed() {
		t.Error("new model should not be confirmed")
	}
	if m.IsCancelled() {
		t.Error("new model should not be cancelled")
	}
}

func TestInstructionConfirmationInit(t *testing.T) {
	m := NewInstructionConfirmationModel(sampleInstructionData(), nil)
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init should return nil, got %v", cmd)
	}
}

func TestInstructionConfirmationViewNoPanic(t *testing.T) {
	m := NewInstructionConfirmationModel(sampleInstructionData(), nil)

	// View and Init must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View panicked: %v", r)
		}
	}()

	v := m.View()
	content := v.Content
	if content == "" {
		t.Error("View content should not be empty")
	}
}

func TestInstructionConfirmationUpdateY(t *testing.T) {
	m := NewInstructionConfirmationModel(sampleInstructionData(), nil)
	newM, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	im, ok := newM.(InstructionConfirmationModel)
	if !ok {
		t.Fatalf("expected InstructionConfirmationModel, got %T", newM)
	}
	if !im.IsConfirmed() {
		t.Error("pressing 'y' should set confirmed=true")
	}
	if cmd == nil {
		t.Error("pressing 'y' should return tea.Quit command")
	}
}

func TestInstructionConfirmationUpdateN(t *testing.T) {
	m := NewInstructionConfirmationData_Default()
	newM, _ := m.Update(tea.KeyPressMsg{Text: "n"})
	im, ok := newM.(InstructionConfirmationModel)
	if !ok {
		t.Fatalf("expected InstructionConfirmationModel, got %T", newM)
	}
	if !im.IsCancelled() {
		t.Error("pressing 'n' should set cancelled=true")
	}
}

func TestInstructionConfirmationUpdateEsc(t *testing.T) {
	m := NewInstructionConfirmationModel(sampleInstructionData(), nil)
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	im, ok := newM.(InstructionConfirmationModel)
	if !ok {
		t.Fatalf("expected InstructionConfirmationModel, got %T", newM)
	}
	if !im.IsCancelled() {
		t.Error("pressing 'esc' should set cancelled=true")
	}
}

func TestInstructionConfirmationUpdateCtrlC(t *testing.T) {
	m := NewInstructionConfirmationModel(sampleInstructionData(), nil)
	newM, _ := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	im, ok := newM.(InstructionConfirmationModel)
	if !ok {
		t.Fatalf("expected InstructionConfirmationModel, got %T", newM)
	}
	if !im.IsCancelled() {
		t.Error("pressing 'ctrl+c' should set cancelled=true")
	}
}

func TestInstructionConfirmationViewRendersFields(t *testing.T) {
	data := sampleInstructionData()
	m := NewInstructionConfirmationModel(data, nil)
	content := m.View().Content

	checks := map[string]string{
		"action":   data.Action,
		"command":  data.ActionDetail,
		"trigger":  data.Trigger,
		"scope":    data.Scope,
		"priority": data.Priority,
	}
	for label, val := range checks {
		if !strings.Contains(content, val) {
			t.Errorf("view should contain %s %q, got: %s", label, val, content)
		}
	}
}

func TestInstructionConfirmationViewRendersRiskLevel(t *testing.T) {
	data := sampleInstructionData()
	m := NewInstructionConfirmationModel(data, nil)
	content := m.View().Content
	if !strings.Contains(content, string(data.RiskLevel)) {
		t.Errorf("view should contain risk level %q, got: %s", data.RiskLevel, content)
	}
}

func TestInstructionConfirmationViewAllLowercase(t *testing.T) {
	// Per CLAUDE.md UI conventions: all UI element text must be lowercase.
	// We check the footer button labels and header text.
	data := sampleInstructionData()
	m := NewInstructionConfirmationModel(data, nil)
	content := m.View().Content

	// The footer should contain lowercase labels.
	lowercasePhrases := []string{
		"confirm instruction",
		"[y] confirm",
		"[n] cancel",
		"[esc] cancel",
	}
	for _, phrase := range lowercasePhrases {
		if !strings.Contains(content, phrase) {
			t.Errorf("view should contain lowercase phrase %q, got: %s", phrase, content)
		}
	}

	// Ensure no capitalized button labels appear.
	// We check that "Confirm", "Cancel", "OK", "Yes", "No" (capitalized)
	// do NOT appear as standalone UI labels.
	// Note: action/trigger content may legitimately be capitalized,
	// so we only check the structural UI labels.
	badCapitalized := []string{
		"Confirm Instruction",
		"[Y] Confirm",
		"[N] Cancel",
		"[ESC] Cancel",
	}
	for _, bad := range badCapitalized {
		if strings.Contains(content, bad) {
			t.Errorf("view should not contain capitalized label %q (CLAUDE.md requires lowercase)", bad)
		}
	}
}

func TestInstructionConfirmationDataAccessor(t *testing.T) {
	data := sampleInstructionData()
	m := NewInstructionConfirmationModel(data, nil)
	got := m.Data()
	if got.Action != data.Action {
		t.Errorf("Data().Action = %q, want %q", got.Action, data.Action)
	}
	if got.RiskLevel != data.RiskLevel {
		t.Errorf("Data().RiskLevel = %q, want %q", got.RiskLevel, data.RiskLevel)
	}
}

func TestInstructionConfirmationAllRiskLevels(t *testing.T) {
	levels := []RiskLevel{RiskLow, RiskMedium, RiskHigh, RiskCritical}
	for _, level := range levels {
		data := InstructionConfirmationData{
			RiskLevel: level,
			Action:    "test",
			Trigger:   "manual",
		}
		m := NewInstructionConfirmationModel(data, nil)

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("View panicked for risk level %s: %v", level, r)
			}
		}()

		content := m.View().Content
		if !strings.Contains(content, string(level)) {
			t.Errorf("view for risk=%s should contain the risk level string", level)
		}
	}
}

// NewInstructionConfirmationData_Default is a test helper that creates a model
// with default data. Named to avoid collision with sampleInstructionData.
func NewInstructionConfirmationData_Default() InstructionConfirmationModel {
	return NewInstructionConfirmationModel(InstructionConfirmationData{
		RiskLevel: RiskMedium,
		Action:    "shell",
	}, nil)
}
