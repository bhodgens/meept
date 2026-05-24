// internal/configui/editor_test.go
package configui

import (
	"testing"
)

// --- Toggle ---

func TestFieldEditorToggleFlipsValue(t *testing.T) {
	f := NewToggleField("enabled", "Enabled", false)
	ed := NewFieldEditor(f)

	ed.Toggle()
	if f.Get() != "true" {
		t.Errorf("expected true after toggle, got %s", f.Get())
	}

	ed.Toggle()
	if f.Get() != "false" {
		t.Errorf("expected false after second toggle, got %s", f.Get())
	}
}

func TestFieldEditorToggleCancel(t *testing.T) {
	f := NewToggleField("enabled", "Enabled", false)
	ed := NewFieldEditor(f)

	ed.Toggle()
	if f.Get() != "true" {
		t.Fatalf("expected true after toggle, got %s", f.Get())
	}

	ed.Cancel()
	if f.Get() != "false" {
		t.Errorf("expected false after cancel, got %s", f.Get())
	}
}

// --- Select ---

func TestFieldEditorSelectNavigateAndConfirm(t *testing.T) {
	opts := []string{"debug", "info", "warn", "error"}
	f := NewSelectField("log_level", "Log Level", "info", opts)
	ed := NewFieldEditor(f)

	// Initial cursor should be on current value "info" (index 1)
	if ed.SelectCursor() != 1 {
		t.Errorf("expected initial cursor at 1, got %d", ed.SelectCursor())
	}

	ed.SelectDown() // info -> warn
	if ed.SelectCursor() != 2 {
		t.Errorf("expected cursor at 2 after down, got %d", ed.SelectCursor())
	}

	ed.SelectDown() // warn -> error
	if ed.SelectCursor() != 3 {
		t.Errorf("expected cursor at 3 after down, got %d", ed.SelectCursor())
	}

	ed.SelectDown() // clamp at max
	if ed.SelectCursor() != 3 {
		t.Errorf("expected cursor clamped at 3, got %d", ed.SelectCursor())
	}

	ed.SelectUp() // error -> warn
	if ed.SelectCursor() != 2 {
		t.Errorf("expected cursor at 2 after up, got %d", ed.SelectCursor())
	}

	ed.SelectUp() // warn -> info
	ed.SelectUp() // info -> debug
	if ed.SelectCursor() != 0 {
		t.Errorf("expected cursor at 0 after up, got %d", ed.SelectCursor())
	}

	ed.SelectUp() // clamp at min
	if ed.SelectCursor() != 0 {
		t.Errorf("expected cursor clamped at 0, got %d", ed.SelectCursor())
	}

	// Confirm selection at debug
	ed.ConfirmSelect()
	if f.Get() != "debug" {
		t.Errorf("expected debug after confirm, got %s", f.Get())
	}
}

func TestFieldEditorSelectCancel(t *testing.T) {
	opts := []string{"debug", "info", "warn", "error"}
	f := NewSelectField("log_level", "Log Level", "info", opts)
	ed := NewFieldEditor(f)

	ed.SelectDown() // info -> warn
	ed.SelectDown() // warn -> error
	ed.ConfirmSelect()
	if f.Get() != "error" {
		t.Fatalf("expected error after confirm, got %s", f.Get())
	}

	ed.Cancel()
	if f.Get() != "info" {
		t.Errorf("expected info after cancel, got %s", f.Get())
	}
}

// --- MultiSelect ---

func TestFieldEditorMultiSelectToggleOptions(t *testing.T) {
	opts := []string{"code", "reasoning", "tool_use", "vision"}
	f := NewMultiSelectField("capabilities", "Capabilities", []string{"code"}, opts)
	ed := NewFieldEditor(f)

	// Initially: code=selected (index 0)
	state := ed.MultiSelectState()
	if !state[0] {
		t.Error("expected index 0 to be selected initially")
	}
	if state[1] {
		t.Error("expected index 1 to not be selected initially")
	}

	// Toggle on index 2 (tool_use)
	ed.ToggleMultiSelectOption(2)
	state = ed.MultiSelectState()
	if !state[0] {
		t.Error("expected index 0 still selected")
	}
	if !state[2] {
		t.Error("expected index 2 now selected")
	}

	// Verify field was updated
	selected := f.GetStrings()
	found := map[string]bool{}
	for _, s := range selected {
		found[s] = true
	}
	if !found["code"] || !found["tool_use"] {
		t.Errorf("expected [code, tool_use] selected, got %v", selected)
	}

	// Toggle off index 0 (code)
	ed.ToggleMultiSelectOption(0)
	state = ed.MultiSelectState()
	if state[0] {
		t.Error("expected index 0 deselected")
	}
}

func TestFieldEditorMultiSelectCancel(t *testing.T) {
	opts := []string{"code", "reasoning", "tool_use"}
	f := NewMultiSelectField("capabilities", "Capabilities", []string{"code"}, opts)
	ed := NewFieldEditor(f)

	ed.ToggleMultiSelectOption(0) // deselect code
	ed.ToggleMultiSelectOption(2) // select tool_use

	ed.Cancel()
	selected := f.GetStrings()
	if len(selected) != 1 || selected[0] != "code" {
		t.Errorf("expected [code] after cancel, got %v", selected)
	}
}

// --- Text ---

func TestFieldEditorTextInputAndConfirm(t *testing.T) {
	f := NewTextField("socket_path", "Socket Path", "/tmp/meept.sock")
	ed := NewFieldEditor(f)

	if ed.InputValue() != "/tmp/meept.sock" {
		t.Errorf("expected input initialized to current value, got %s", ed.InputValue())
	}

	ed.SetInput("/tmp/other.sock")
	if ed.InputValue() != "/tmp/other.sock" {
		t.Errorf("expected input /tmp/other.sock, got %s", ed.InputValue())
	}

	ed.ConfirmInput()
	if f.Get() != "/tmp/other.sock" {
		t.Errorf("expected field value /tmp/other.sock after confirm, got %s", f.Get())
	}
}

func TestFieldEditorTextCancel(t *testing.T) {
	f := NewTextField("socket_path", "Socket Path", "/tmp/meept.sock")
	ed := NewFieldEditor(f)

	ed.SetInput("/tmp/changed.sock")
	ed.ConfirmInput()
	if f.Get() != "/tmp/changed.sock" {
		t.Fatalf("expected changed value, got %s", f.Get())
	}

	ed.Cancel()
	if f.Get() != "/tmp/meept.sock" {
		t.Errorf("expected original value after cancel, got %s", f.Get())
	}
}

// --- Number ---

func TestFieldEditorNumberInputAndConfirm(t *testing.T) {
	f := NewNumberField("pool_size", "Pool Size", 4)
	ed := NewFieldEditor(f)

	if ed.InputValue() != "4" {
		t.Errorf("expected input initialized to 4, got %s", ed.InputValue())
	}

	ed.SetInput("16")
	ed.ConfirmInput()
	if f.Get() != "16" {
		t.Errorf("expected field value 16 after confirm, got %s", f.Get())
	}
}

func TestFieldEditorNumberCancel(t *testing.T) {
	f := NewNumberField("pool_size", "Pool Size", 4)
	ed := NewFieldEditor(f)

	ed.SetInput("99")
	ed.ConfirmInput()
	ed.Cancel()
	if f.Get() != "4" {
		t.Errorf("expected original value after cancel, got %s", f.Get())
	}
}

// --- Masked ---

func TestFieldEditorMaskedInputAndConfirm(t *testing.T) {
	f := NewMaskedField("api_key", "API Key", "sk-old")
	ed := NewFieldEditor(f)

	if ed.InputValue() != "sk-old" {
		t.Errorf("expected input initialized to sk-old, got %s", ed.InputValue())
	}

	ed.SetInput("sk-new-123")
	ed.ConfirmInput()
	if f.Get() != "sk-new-123" {
		t.Errorf("expected field value sk-new-123 after confirm, got %s", f.Get())
	}
}

func TestFieldEditorMaskedCancel(t *testing.T) {
	f := NewMaskedField("api_key", "API Key", "sk-original")
	ed := NewFieldEditor(f)

	ed.SetInput("sk-changed")
	ed.ConfirmInput()
	ed.Cancel()
	if f.Get() != "sk-original" {
		t.Errorf("expected original value after cancel, got %s", f.Get())
	}
}
