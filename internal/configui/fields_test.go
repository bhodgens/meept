// internal/configui/fields_test.go
package configui

import (
	"testing"
)

func TestTextFieldDefaultValue(t *testing.T) {
	f := NewTextField("socket_path", "Socket Path", "/tmp/meept.sock")
	if f.Key() != "socket_path" {
		t.Errorf("expected key socket_path, got %s", f.Key())
	}
	if f.Label() != "Socket Path" {
		t.Errorf("expected label Socket Path, got %s", f.Label())
	}
	if f.Get() != "/tmp/meept.sock" {
		t.Errorf("expected value /tmp/meept.sock, got %s", f.Get())
	}
}

func TestTextFieldSetDirty(t *testing.T) {
	f := NewTextField("socket_path", "Socket Path", "/tmp/meept.sock")
	if f.IsDirty() {
		t.Error("new field should not be dirty")
	}
	f.Set("/tmp/other.sock")
	if !f.IsDirty() {
		t.Error("field should be dirty after set")
	}
	if f.Get() != "/tmp/other.sock" {
		t.Errorf("expected /tmp/other.sock, got %s", f.Get())
	}
}

func TestTextFieldReset(t *testing.T) {
	f := NewTextField("socket_path", "Socket Path", "/tmp/meept.sock")
	f.Set("/tmp/other.sock")
	f.Reset()
	if f.IsDirty() {
		t.Error("field should not be dirty after reset")
	}
	if f.Get() != "/tmp/meept.sock" {
		t.Errorf("expected original value after reset, got %s", f.Get())
	}
}

func TestToggleField(t *testing.T) {
	f := NewToggleField("enabled", "Enabled", true)
	if f.Get() != "true" {
		t.Errorf("expected true, got %s", f.Get())
	}
	f.Set("false")
	if f.Get() != "false" {
		t.Errorf("expected false, got %s", f.Get())
	}
}

func TestSelectField(t *testing.T) {
	f := NewSelectField("log_level", "Log Level", "info", []string{"debug", "info", "warn", "error"})
	if f.Get() != "info" {
		t.Errorf("expected info, got %s", f.Get())
	}
	f.Set("debug")
	if f.Get() != "debug" {
		t.Errorf("expected debug, got %s", f.Get())
	}
}

func TestSelectFieldRejectsInvalid(t *testing.T) {
	f := NewSelectField("log_level", "Log Level", "info", []string{"debug", "info", "warn", "error"})
	err := f.Set("bogus")
	if err == nil {
		t.Error("expected error setting invalid option")
	}
}

func TestMultiSelectField(t *testing.T) {
	f := NewMultiSelectField("capabilities", "Capabilities", []string{"code", "reasoning"}, []string{"code", "reasoning", "tool_use", "vision"})
	selected := f.GetStrings()
	if len(selected) != 2 || selected[0] != "code" || selected[1] != "reasoning" {
		t.Errorf("expected [code, reasoning], got %v", selected)
	}
	f.SetStrings([]string{"code", "tool_use"})
	selected = f.GetStrings()
	if len(selected) != 2 || selected[0] != "code" || selected[1] != "tool_use" {
		t.Errorf("expected [code, tool_use], got %v", selected)
	}
}

func TestNumberField(t *testing.T) {
	f := NewNumberField("pool_size", "Pool Size", 4)
	if f.Get() != "4" {
		t.Errorf("expected 4, got %s", f.Get())
	}
	f.Set("8")
	if f.Get() != "8" {
		t.Errorf("expected 8, got %s", f.Get())
	}
}

func TestNumberFieldRejectsNonNumeric(t *testing.T) {
	f := NewNumberField("pool_size", "Pool Size", 4)
	err := f.Set("abc")
	if err == nil {
		t.Error("expected error setting non-numeric value")
	}
}

func TestMaskedField(t *testing.T) {
	f := NewMaskedField("api_key", "API Key", "sk-12345")
	if f.Get() != "sk-12345" {
		t.Errorf("expected sk-12345, got %s", f.Get())
	}
	if f.Display() != "••••••••" {
		t.Errorf("expected masked display, got %s", f.Display())
	}
}

func TestFieldHelp(t *testing.T) {
	f := NewTextField("socket_path", "Socket Path", "/tmp/meept.sock")
	f.SetHelp("Unix domain socket path for RPC communication")
	if f.Help() != "Unix domain socket path for RPC communication" {
		t.Errorf("help text mismatch")
	}
}
