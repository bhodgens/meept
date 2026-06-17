package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/debug"
	"github.com/caimlas/meept/internal/tools"
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
		"goroutines", "set_goroutine",
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

func TestDebugToolAttachMissingParams(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "attach"})
	if err == nil {
		t.Fatal("expected error for attach with no process_id or process_name")
	}
}

func TestDebugToolAttachInvalidAdapter(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":     "attach",
		"process_id": float64(os.Getpid()),
		"adapter":    "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for invalid adapter on attach")
	}
}

func TestDebugToolAttachIncludesMode(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()
	actionProp, ok := params.Properties["action"]
	if !ok {
		t.Fatal("expected 'action' property in parameters")
	}

	// Verify 'attach' is in the enum list.
	found := false
	for _, e := range actionProp.Enum {
		if e == "attach" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'attach' to be in the action enum")
	}

	// Verify process_id and process_name parameters exist.
	if _, ok := params.Properties["process_id"]; !ok {
		t.Fatal("expected 'process_id' property in parameters")
	}
	if _, ok := params.Properties["process_name"]; !ok {
		t.Fatal("expected 'process_name' property in parameters")
	}
}

func TestDebugToolDescriptionMentionsAttach(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	desc := tool.Description()
	if !strings.Contains(desc, "attach") {
		t.Fatal("description should mention 'attach' capability")
	}
}

func TestDebugToolNoActiveSessionMentionsAttach(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "continue"})
	if err == nil {
		t.Fatal("expected error for no active session")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "attach") {
		t.Fatalf("error message should mention 'attach': %s", errMsg)
	}
}

func TestDebugToolGoroutinesNoActiveSession(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "goroutines"})
	if err == nil {
		t.Fatal("expected error for goroutines with no active session")
	}
}

func TestDebugToolSetGoroutineNoActiveSession(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "set_goroutine"})
	if err == nil {
		t.Fatal("expected error for set_goroutine with no active session")
	}
}

func TestDebugToolGoroutinesInEnum(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()
	actionProp, ok := params.Properties["action"]
	if !ok {
		t.Fatal("expected 'action' property in parameters")
	}

	for _, want := range []string{"goroutines", "set_goroutine"} {
		found := false
		for _, e := range actionProp.Enum {
			if e == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q to be in the action enum", want)
		}
	}
}

func TestDebugToolGoroutineIDParameter(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()
	if _, ok := params.Properties["goroutine_id"]; !ok {
		t.Fatal("expected 'goroutine_id' property in parameters")
	}
}

func TestDebugToolDescriptionMentionsGoroutines(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	desc := tool.Description()
	if !strings.Contains(desc, "goroutine") {
		t.Fatal("description should mention 'goroutine' capability")
	}
}

func TestDebugToolNoActiveSessionMentionsGoroutines(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	// Both goroutines and set_goroutine should be in the "no active session" actions list.
	goActions := []string{"goroutines", "set_goroutine"}
	for _, action := range goActions {
		t.Run(action, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), map[string]any{"action": action})
			if err == nil {
				t.Fatalf("expected error for %s with no active session", action)
			}
		})
	}
}

// --- load_core action tests ---

func TestDebugToolLoadCoreInEnum(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()
	actionProp, ok := params.Properties["action"]
	if !ok {
		t.Fatal("expected 'action' property in parameters")
	}

	found := false
	for _, e := range actionProp.Enum {
		if e == "load_core" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'load_core' to be in the action enum")
	}
}

func TestDebugToolLoadCoreParameterExists(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()

	// core_file parameter should exist.
	if _, ok := params.Properties["core_file"]; !ok {
		t.Fatal("expected 'core_file' property in parameters")
	}

	// program parameter should exist.
	if _, ok := params.Properties["program"]; !ok {
		t.Fatal("expected 'program' property in parameters")
	}
}

func TestDebugToolLoadCoreMissingCoreFile(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":  "load_core",
		"program": "/usr/bin/test",
	})
	if err == nil {
		t.Fatal("expected error for load_core with no core_file")
	}
	if !strings.Contains(err.Error(), "core_file is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDebugToolLoadCoreMissingProgram(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":    "load_core",
		"core_file": "/tmp/core.12345",
	})
	if err == nil {
		t.Fatal("expected error for load_core with no program")
	}
	if !strings.Contains(err.Error(), "program path is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDebugToolLoadCoreMissingBoth(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "load_core"})
	if err == nil {
		t.Fatal("expected error for load_core with no params")
	}
}

func TestDebugToolDescriptionMentionsLoadCore(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	desc := tool.Description()
	if !strings.Contains(desc, "core dump") {
		t.Fatal("description should mention 'core dump'")
	}
	if !strings.Contains(desc, "load_core") {
		t.Fatal("description should mention 'load_core' action")
	}
	if !strings.Contains(desc, "post-mortem") {
		t.Fatal("description should mention 'post-mortem' analysis")
	}
}

func TestDebugToolLoadCoreNoDebugger(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	// Use a nonexistent adapter to trigger a clear error.
	// The nonexistent_core_adapter should fail adapter detection.
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":    "load_core",
		"core_file": "/tmp/core.12345",
		"program":   "/usr/bin/test",
		"adapter":   "nonexistent_core_tool",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent core adapter")
	}
}

func TestDebugToolLoadCoreWithDummyFiles(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	// Create a dummy core file and a real program (the test binary).
	tmpDir := t.TempDir()
	coreFile := filepath.Join(tmpDir, "core.test")
	program, _ := os.Executable()

	// Write minimal data to the "core" file.
	if err := os.WriteFile(coreFile, []byte("not a real core dump"), 0644); err != nil {
		t.Fatal(err)
	}

	// This will fail because the core file isn't a real core dump,
	// but we just want to verify the tool dispatches to load_core correctly.
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":    "load_core",
		"core_file": coreFile,
		"program":   program,
	})
	// Error is expected because the core file isn't real.
	if err == nil {
		// If it succeeded, the debugger accepted the fake core file.
		// That's OK too — just verify the result.
		t.Log("debugger accepted fake core file (unusual but not an error)")
	} else {
		// The error should be about core dump analysis, not about dispatch.
		t.Logf("expected analysis error (fake core file): %v", err)
		if !strings.Contains(err.Error(), "core dump") && !strings.Contains(err.Error(), "core file") &&
			!strings.Contains(err.Error(), "failed to analyze") && !strings.Contains(err.Error(), "not found") {
			t.Errorf("error message should mention core analysis, got: %v", err)
		}
	}
}

func TestDebugToolLoadCoreActionInNoSessionCheck(t *testing.T) {
	// load_core should NOT require an active session (it creates one).
	// This test verifies it is not in the "no active session" error set.
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	// This should not fail with "no active session" error.
	// It will fail for other reasons (no core file), but not for missing session.
	_, err := tool.Execute(context.Background(), map[string]any{"action": "load_core"})
	if err == nil {
		t.Fatal("expected error (no core_file), but not a 'no active session' error")
	}
	if strings.Contains(err.Error(), "no active debug session") {
		t.Fatal("load_core should not require an active session")
	}
}

func TestDebugToolLoadCoreSessionMode(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()
	actionProp, ok := params.Properties["action"]
	if !ok {
		t.Fatal("expected 'action' property")
	}

	// Verify the action description mentions load_core capabilities.
	actionDesc := actionProp.Description
	if !strings.Contains(actionDesc, "load_core") {
		t.Fatal("action description should mention load_core")
	}
}

// --- script action tests ---

func TestDebugToolScriptInEnum(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()
	actionProp, ok := params.Properties["action"]
	if !ok {
		t.Fatal("expected 'action' property in parameters")
	}

	found := false
	for _, e := range actionProp.Enum {
		if e == "script" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'script' to be in the action enum")
	}
}

func TestDebugToolScriptParametersExist(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	params := tool.Parameters()

	if _, ok := params.Properties["script_file"]; !ok {
		t.Fatal("expected 'script_file' property in parameters")
	}
	if _, ok := params.Properties["stop_on_error"]; !ok {
		t.Fatal("expected 'stop_on_error' property in parameters")
	}
}

func TestDebugToolScriptMissingFile(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "script"})
	if err == nil {
		t.Fatal("expected error for script with no script_file")
	}
	if !strings.Contains(err.Error(), "script_file is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDebugToolScriptFileNotFound(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":      "script",
		"script_file": "/nonexistent/path/script.jsonl",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent script file")
	}
	if !strings.Contains(err.Error(), "failed to parse script") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDebugToolScriptInvalidJSON(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	tmpDir := t.TempDir()
	badScript := filepath.Join(tmpDir, "bad.jsonl")
	if err := os.WriteFile(badScript, []byte("{not valid json}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":      "script",
		"script_file": badScript,
	})
	if err == nil {
		t.Fatal("expected error for invalid script file")
	}
}

func TestDebugToolScriptEmptyScript(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	tmpDir := t.TempDir()
	emptyScript := filepath.Join(tmpDir, "empty.jsonl")
	if err := os.WriteFile(emptyScript, []byte("// only comments\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":      "script",
		"script_file": emptyScript,
	})
	if err == nil {
		t.Fatal("expected error for empty script")
	}
}

func TestDebugToolScriptMissingActionInCommand(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	tmpDir := t.TempDir()
	badScript := filepath.Join(tmpDir, "no_action.jsonl")
	if err := os.WriteFile(badScript, []byte(`{"file": "main.go"}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":      "script",
		"script_file": badScript,
	})
	if err == nil {
		t.Fatal("expected error for command missing action")
	}
}

func TestDebugToolScriptValidFile(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "valid.jsonl")
	// Write a script with sessions (which doesn't need a debug session).
	content := `{"action": "sessions"}
{"action": "sessions"}`
	if err := os.WriteFile(scriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":      "script",
		"script_file": scriptPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr, ok := result.(tools.ToolResult)
	if !ok {
		t.Fatal("expected ToolResult")
	}
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	rMap, ok := tr.Result.(map[string]any)
	if !ok {
		t.Fatal("expected map result")
	}
	if rMap["total"] != 2 {
		t.Errorf("expected total 2, got %v", rMap["total"])
	}
	if rMap["succeeded"] != 2 {
		t.Errorf("expected succeeded 2, got %v", rMap["succeeded"])
	}
	if rMap["failed"] != 0 {
		t.Errorf("expected failed 0, got %v", rMap["failed"])
	}

	results, ok := rMap["results"].([]any)
	if !ok {
		t.Fatalf("expected results array, got %T", rMap["results"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestDebugToolScriptWithErrorsContinue(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "mixed.jsonl")
	// First command: sessions (will succeed).
	// Second command: unknown_action (will fail).
	// Third command: sessions (should still execute since stop_on_error defaults to false).
	content := `{"action": "sessions"}
{"action": "unknown_action"}
{"action": "sessions"}`
	if err := os.WriteFile(scriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":        "script",
		"script_file":   scriptPath,
		"stop_on_error": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr, ok := result.(tools.ToolResult)
	if !ok {
		t.Fatal("expected ToolResult")
	}
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}

	rMap := tr.Result.(map[string]any)
	if rMap["total"] != 3 {
		t.Errorf("expected total 3, got %v", rMap["total"])
	}
	if rMap["succeeded"] != 2 {
		t.Errorf("expected succeeded 2, got %v", rMap["succeeded"])
	}
	if rMap["failed"] != 1 {
		t.Errorf("expected failed 1, got %v", rMap["failed"])
	}
}

func TestDebugToolScriptStopOnError(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "stop.jsonl")
	content := `{"action": "sessions"}
{"action": "unknown_action"}
{"action": "sessions"}`
	if err := os.WriteFile(scriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":        "script",
		"script_file":   scriptPath,
		"stop_on_error": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	rMap := tr.Result.(map[string]any)

	// With stop_on_error=true, only 2 commands should have been attempted.
	if rMap["total"] != 3 {
		t.Errorf("expected total 3, got %v", rMap["total"])
	}
	if rMap["succeeded"] != 1 {
		t.Errorf("expected succeeded 1, got %v", rMap["succeeded"])
	}
	if rMap["failed"] != 1 {
		t.Errorf("expected failed 1, got %v", rMap["failed"])
	}

	results, ok := rMap["results"].([]any)
	if !ok {
		t.Fatalf("expected results array, got %T", rMap["results"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (stopped after error), got %d", len(results))
	}
}

func TestDebugToolScriptWithCommentsAndBlankLines(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "comments.jsonl")
	content := `// First, list sessions
{"action": "sessions"}

# Second, list sessions again
{"action": "sessions"}`
	if err := os.WriteFile(scriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":      "script",
		"script_file": scriptPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	rMap := tr.Result.(map[string]any)
	if rMap["total"] != 2 {
		t.Errorf("expected total 2 (comments/blank lines skipped), got %v", rMap["total"])
	}
	if rMap["succeeded"] != 2 {
		t.Errorf("expected succeeded 2, got %v", rMap["succeeded"])
	}
}

func TestDebugToolScriptResultStructure(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "struct.jsonl")
	content := `{"action": "sessions"}`
	if err := os.WriteFile(scriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":      "script",
		"script_file": scriptPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := result.(tools.ToolResult)
	rMap := tr.Result.(map[string]any)
	results := rMap["results"].([]any)

	// Check individual result structure.
	firstResult := results[0].(map[string]any)
	if _, ok := firstResult["index"]; !ok {
		t.Error("expected 'index' field in result")
	}
	if _, ok := firstResult["line"]; !ok {
		t.Error("expected 'line' field in result")
	}
	if _, ok := firstResult["action"]; !ok {
		t.Error("expected 'action' field in result")
	}
	if _, ok := firstResult["success"]; !ok {
		t.Error("expected 'success' field in result")
	}
	if _, ok := firstResult["output"]; !ok {
		t.Error("expected 'output' field in result")
	}
}

func TestDebugToolDescriptionMentionsScript(t *testing.T) {
	mgr := debug.NewManager()
	tool := NewDebugTool(mgr, nil)

	desc := tool.Description()
	if !strings.Contains(desc, "script") {
		t.Fatal("description should mention 'script' capability")
	}
}
