package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/debug"
)

func TestDebugToolName(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)
	if tool.Name() != "debug" {
		t.Fatalf("expected name 'debug', got %q", tool.Name())
	}
}

func TestDebugToolParameters(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()
	if params.Type != "object" {
		t.Fatalf("expected type 'object', got %v", params.Type)
	}

	if _, ok := params.Properties["action"]; !ok {
		t.Fatal("expected 'action' property")
	}

	// Check that 'action' is required.
	found := false
	for _, r := range params.Required {
		if r == "action" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'action' to be required")
	}
}

func TestDebugToolMissingAction(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing action")
	}
}

func TestDebugToolUnknownAction(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "unknown"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestDebugToolLaunchMissingProgram(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "launch"})
	if err == nil {
		t.Fatal("expected error for missing program")
	}
}

func TestDebugToolLaunchInvalidAdapter(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":  "launch",
		"program": "main.go",
		"adapter": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for invalid adapter")
	}
}

func TestDebugToolNoActiveSession(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	actions := []string{
		"continue", "step_over", "step_in", "step_out",
		"evaluate", "stack_trace", "threads", "scopes", "variables",
		"terminate",
	}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), map[string]any{"action": action})
			if err == nil {
				t.Fatalf("expected error for %s with no active session", action)
			}
		})
	}
}

func TestDebugToolSetBreakpointMissingFile(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	// No active session - should error.
	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "set_breakpoint",
	})
	if err == nil {
		t.Fatal("expected error for set_breakpoint with no active session")
	}
}

func TestDebugToolEvaluateMissingExpression(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	// No active session - should error before checking expression.
	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "evaluate",
	})
	if err == nil {
		t.Fatal("expected error for evaluate with no active session")
	}
}

func TestDebugToolSessionsEmpty(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	result, err := tool.Execute(context.Background(), map[string]any{"action": "sessions"})
	if err != nil {
		t.Fatalf("sessions action failed: %v", err)
	}

	// The result is a tools.ToolResult, extract the inner result.
	tr, ok := result.(interface {
		GetResult() any
	})
	_ = tr
	_ = ok

	// Just verify it doesn't crash.
	_ = result
}

func TestDebugToolDescription(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	desc := tool.Description()
	if desc == "" {
		t.Fatal("expected non-empty description")
	}
	if len(desc) < 50 {
		t.Fatalf("description seems too short: %q", desc)
	}
}

func TestDebugToolIntArg(t *testing.T) {
	tests := []struct {
		args     map[string]any
		key      string
		expected int
	}{
		{map[string]any{"x": float64(42)}, "x", 42},
		{map[string]any{"x": 7}, "x", 7},
		{map[string]any{"x": json.Number("99")}, "x", 99},
		{map[string]any{"x": "not a number"}, "x", 0},
		{map[string]any{}, "x", 0},
	}

	for _, tt := range tests {
		got := intArg(tt.args, tt.key)
		if got != tt.expected {
			t.Errorf("intArg(%v, %q) = %d, want %d", tt.args, tt.key, got, tt.expected)
		}
	}
}

func TestDebugToolRawToMap(t *testing.T) {
	// Empty data.
	m := rawToMap(nil)
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %v", m)
	}
	m = rawToMap(json.RawMessage{})
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %v", m)
	}

	// Valid JSON object.
	m = rawToMap(json.RawMessage(`{"key": "value"}`))
	if m["key"] != "value" {
		t.Fatalf("expected key=value, got %v", m)
	}

	// Non-object JSON (should fall back to raw string).
	m = rawToMap(json.RawMessage(`"hello"`))
	raw, ok := m["raw"].(string)
	if !ok {
		t.Fatalf("expected raw to be string, got %T", m["raw"])
	}
	if raw != `"hello"` {
		t.Fatalf("expected raw=%q, got %q", `"hello"`, raw)
	}
}
