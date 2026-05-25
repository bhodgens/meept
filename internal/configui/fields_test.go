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

func TestFloatField(t *testing.T) {
	f := NewFloatField("aggressiveness", "Aggressiveness", 0.75)
	if f.Key() != "aggressiveness" {
		t.Errorf("expected key aggressiveness, got %s", f.Key())
	}
	if f.Label() != "Aggressiveness" {
		t.Errorf("expected label Aggressiveness, got %s", f.Label())
	}
	if f.Get() != "0.75" {
		t.Errorf("expected 0.75, got %s", f.Get())
	}
	if f.Type() != FieldFloat {
		t.Errorf("expected FieldFloat type, got %v", f.Type())
	}
}

func TestFloatFieldSetDirty(t *testing.T) {
	f := NewFloatField("aggressiveness", "Aggressiveness", 0.75)
	if f.IsDirty() {
		t.Error("new field should not be dirty")
	}
	f.Set("0.5")
	if !f.IsDirty() {
		t.Error("field should be dirty after set")
	}
	if f.Get() != "0.5" {
		t.Errorf("expected 0.5, got %s", f.Get())
	}
}

func TestFloatFieldReset(t *testing.T) {
	f := NewFloatField("aggressiveness", "Aggressiveness", 0.75)
	f.Set("0.5")
	f.Reset()
	if f.IsDirty() {
		t.Error("field should not be dirty after reset")
	}
	if f.Get() != "0.75" {
		t.Errorf("expected original value 0.75 after reset, got %s", f.Get())
	}
}

func TestFloatFieldRejectsNonNumeric(t *testing.T) {
	f := NewFloatField("aggressiveness", "Aggressiveness", 0.75)
	err := f.Set("abc")
	if err == nil {
		t.Error("expected error setting non-numeric value")
	}
}

func TestFloatFieldDisplay(t *testing.T) {
	tests := []struct {
		value    float64
		expected string
	}{
		{0.75, "0.75"},
		{1.0, "1"},
		{0.001, "0.001"},
		{3.14159, "3.14159"},
	}
	for _, tt := range tests {
		f := NewFloatField("test", "Test", tt.value)
		if f.Display() != tt.expected {
			t.Errorf("FloatField(%v).Display() = %q, want %q", tt.value, f.Display(), tt.expected)
		}
	}
}

func TestFloatFieldIntegerValue(t *testing.T) {
	f := NewFloatField("ratio", "Ratio", 1.0)
	if err := f.Set("0.85"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if f.Get() != "0.85" {
		t.Errorf("expected 0.85, got %s", f.Get())
	}
}

func TestDrilldownFieldWithItems(t *testing.T) {
	items := []DrilldownItem{
		{Name: "item1", Fields: []Field{NewTextField("k", "K", "v")}},
		{Name: "item2", Fields: []Field{NewTextField("k", "K", "v"), NewToggleField("b", "B", true)}},
	}
	f := NewDrilldownField("test", "Test", items)
	if f.Type() != FieldDrilldown {
		t.Errorf("expected FieldDrilldown, got %v", f.Type())
	}
	if f.Display() != "[2 items]" {
		t.Errorf("expected '[2 items]', got %q", f.Display())
	}
	if len(f.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(f.Items))
	}
	if f.Items[0].Name != "item1" {
		t.Errorf("expected item1, got %s", f.Items[0].Name)
	}
	if len(f.Items[1].Fields) != 2 {
		t.Errorf("expected 2 fields in item2, got %d", len(f.Items[1].Fields))
	}
	// Set should error
	if err := f.Set("anything"); err == nil {
		t.Error("expected error setting drilldown field")
	}
}

func TestDrilldownFieldCount(t *testing.T) {
	f := NewDrilldownFieldCount("test", "Test", 3)
	if f.Display() != "[3 items]" {
		t.Errorf("expected '[3 items]', got %q", f.Display())
	}
	if len(f.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(f.Items))
	}
	// Items are empty shells (no Name, no Fields)
	for i, item := range f.Items {
		if item.Name != "" {
			t.Errorf("item %d: expected empty name, got %q", i, item.Name)
		}
		if len(item.Fields) != 0 {
			t.Errorf("item %d: expected empty fields, got %d", i, len(item.Fields))
		}
	}
}
