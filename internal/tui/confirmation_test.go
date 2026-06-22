package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewConfirmationModel(t *testing.T) {
	resp := map[string]any{
		"requires_confirmation": true,
		"action":                "mark_superseded",
		"summary":               "supersede claim_a1b2 with claim_c3d4",
		"reversible":            true,
	}
	m := NewConfirmationModel(resp)
	if m.IsConfirmed() {
		t.Error("new model should not be confirmed")
	}
	if m.IsCancelled() {
		t.Error("new model should not be cancelled")
	}
}

func TestConfirmationModelInit(t *testing.T) {
	m := NewConfirmationModel(map[string]any{})
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init should return nil, got %v", cmd)
	}
}

func TestConfirmationModelUpdateY(t *testing.T) {
	m := NewConfirmationModel(map[string]any{"action": "test"})
	newM, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	if !newM.(ConfirmationModel).IsConfirmed() {
		t.Error("pressing 'y' should set confirmed=true")
	}
	if cmd == nil {
		t.Error("pressing 'y' should return tea.Quit command")
	}
}

func TestConfirmationModelUpdateN(t *testing.T) {
	m := NewConfirmationModel(map[string]any{"action": "test"})
	newM, _ := m.Update(tea.KeyPressMsg{Text: "n"})
	if !newM.(ConfirmationModel).IsCancelled() {
		t.Error("pressing 'n' should set cancelled=true")
	}
}

func TestConfirmationModelUpdateEsc(t *testing.T) {
	m := NewConfirmationModel(map[string]any{"action": "test"})
	newM, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !newM.(ConfirmationModel).IsCancelled() {
		t.Error("pressing 'esc' should set cancelled=true")
	}
}

func TestConfirmationModelViewRendersAction(t *testing.T) {
	resp := map[string]any{
		"action":     "mark_superseded",
		"summary":    "test summary",
		"reversible": true,
	}
	m := NewConfirmationModel(resp)
	view := m.View().Content
	if !strings.Contains(view, "mark_superseded") {
		t.Errorf("view should contain action name, got: %s", view)
	}
	if !strings.Contains(view, "confirm action") {
		t.Errorf("view should contain 'confirm action' header, got: %s", view)
	}
}

func TestConfirmationModelViewRendersPreviews(t *testing.T) {
	resp := map[string]any{
		"action":  "mark_superseded",
		"summary": "test summary",
		"details": map[string]any{
			"old_preview":    "old claim text here",
			"new_preview":    "new claim text here",
			"affected_edges": "3",
		},
	}
	m := NewConfirmationModel(resp)
	view := m.View().Content
	if !strings.Contains(view, "old claim text here") {
		t.Errorf("view should render old_preview, got: %s", view)
	}
	if !strings.Contains(view, "new claim text here") {
		t.Errorf("view should render new_preview, got: %s", view)
	}
	if !strings.Contains(view, "3 edges will be redirected") {
		t.Errorf("view should render affected_edges count, got: %s", view)
	}
}
