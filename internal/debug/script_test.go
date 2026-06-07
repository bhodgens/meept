package debug

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestParseScriptValid(t *testing.T) {
	input := `{"action": "set_breakpoint", "file": "main.go", "line": 42}
{"action": "continue"}
{"action": "evaluate", "expression": "x"}`
	commands, err := ParseScript(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(commands))
	}

	// First command.
	if commands[0].Action != "set_breakpoint" {
		t.Errorf("command 0: expected action 'set_breakpoint', got %q", commands[0].Action)
	}
	if commands[0].Line != 1 {
		t.Errorf("command 0: expected line 1, got %d", commands[0].Line)
	}
	if commands[0].Params["file"] != "main.go" {
		t.Errorf("command 0: expected file 'main.go', got %v", commands[0].Params["file"])
	}
	if int(commands[0].Params["line"].(float64)) != 42 {
		t.Errorf("command 0: expected line 42, got %v", commands[0].Params["line"])
	}

	// Second command (minimal).
	if commands[1].Action != "continue" {
		t.Errorf("command 1: expected action 'continue', got %q", commands[1].Action)
	}
	if commands[1].Line != 2 {
		t.Errorf("command 1: expected line 2, got %d", commands[1].Line)
	}
	if len(commands[1].Params) != 0 {
		t.Errorf("command 1: expected no params, got %v", commands[1].Params)
	}

	// Third command.
	if commands[2].Action != "evaluate" {
		t.Errorf("command 2: expected action 'evaluate', got %q", commands[2].Action)
	}
	if commands[2].Line != 3 {
		t.Errorf("command 2: expected line 3, got %d", commands[2].Line)
	}
}

func TestParseScriptSkipsEmptyAndComments(t *testing.T) {
	input := `// This is a comment
{"action": "continue"}

# Another comment
{"action": "step_over"}`
	commands, err := ParseScript(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(commands))
	}
	if commands[0].Action != "continue" {
		t.Errorf("command 0: expected 'continue', got %q", commands[0].Action)
	}
	if commands[1].Action != "step_over" {
		t.Errorf("command 1: expected 'step_over', got %q", commands[1].Action)
	}
}

func TestParseScriptMissingAction(t *testing.T) {
	input := `{"file": "main.go", "line": 42}`
	_, err := ParseScript(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for missing action")
	}
	if !strings.Contains(err.Error(), "missing or empty 'action'") {
		t.Errorf("expected 'missing or empty action' error, got: %v", err)
	}
}

func TestParseScriptInvalidJSON(t *testing.T) {
	input := `{not valid json}`
	_, err := ParseScript(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' error, got: %v", err)
	}
}

func TestParseScriptEmpty(t *testing.T) {
	input := `// only comments`
	_, err := ParseScript(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for empty script")
	}
	if !strings.Contains(err.Error(), "no commands") {
		t.Errorf("expected 'no commands' error, got: %v", err)
	}
}

func TestParseScriptFile(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := tmpDir + "/test_script.jsonl"

	content := `{"action": "continue"}
{"action": "step_over"}
{"action": "evaluate", "expression": "x"}`
	if err := os.WriteFile(scriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	commands, err := ParseScriptFile(scriptPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(commands))
	}
}

func TestParseScriptFileNotFound(t *testing.T) {
	_, err := ParseScriptFile("/nonexistent/path/script.jsonl")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestExecuteScriptAllSuccess(t *testing.T) {
	commands := []ScriptCommand{
		{Action: "step_over", Line: 1},
		{Action: "step_in", Line: 2},
		{Action: "continue", Line: 3},
	}

	executor := func(ctx context.Context, args map[string]any) (any, error) {
		return map[string]string{"action": args["action"].(string)}, nil
	}

	summary := ExecuteScript(context.Background(), commands, executor, ScriptOptions{
		FilePath: "test.jsonl",
	})

	if summary.Total != 3 {
		t.Errorf("expected total 3, got %d", summary.Total)
	}
	if summary.Succeeded != 3 {
		t.Errorf("expected succeeded 3, got %d", summary.Succeeded)
	}
	if summary.Failed != 0 {
		t.Errorf("expected failed 0, got %d", summary.Failed)
	}
	if len(summary.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(summary.Results))
	}
	for i, r := range summary.Results {
		if !r.Success {
			t.Errorf("result %d: expected success", i)
		}
		if r.Error != "" {
			t.Errorf("result %d: expected no error, got %q", i, r.Error)
		}
	}
}

func TestExecuteScriptWithErrorsContinue(t *testing.T) {
	commands := []ScriptCommand{
		{Action: "step_over", Line: 1},
		{Action: "bad_action", Line: 2},
		{Action: "continue", Line: 3},
	}

	executor := func(ctx context.Context, args map[string]any) (any, error) {
		if args["action"] == "bad_action" {
			return nil, errors.New("unknown action")
		}
		return map[string]string{"action": args["action"].(string)}, nil
	}

	summary := ExecuteScript(context.Background(), commands, executor, ScriptOptions{
		FilePath:    "test.jsonl",
		StopOnError: false,
	})

	if summary.Total != 3 {
		t.Errorf("expected total 3, got %d", summary.Total)
	}
	if summary.Succeeded != 2 {
		t.Errorf("expected succeeded 2, got %d", summary.Succeeded)
	}
	if summary.Failed != 1 {
		t.Errorf("expected failed 1, got %d", summary.Failed)
	}
	if len(summary.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(summary.Results))
	}

	// Second command should have failed.
	if summary.Results[1].Success {
		t.Error("result 1: expected failure")
	}
	if summary.Results[1].Error != "unknown action" {
		t.Errorf("result 1: expected 'unknown action' error, got %q", summary.Results[1].Error)
	}

	// Third command should still have executed.
	if !summary.Results[2].Success {
		t.Error("result 2: expected success (should have continued)")
	}
}

func TestExecuteScriptStopOnError(t *testing.T) {
	commands := []ScriptCommand{
		{Action: "step_over", Line: 1},
		{Action: "bad_action", Line: 2},
		{Action: "continue", Line: 3},
	}

	executor := func(ctx context.Context, args map[string]any) (any, error) {
		if args["action"] == "bad_action" {
			return nil, errors.New("unknown action")
		}
		return map[string]string{"action": args["action"].(string)}, nil
	}

	summary := ExecuteScript(context.Background(), commands, executor, ScriptOptions{
		FilePath:    "test.jsonl",
		StopOnError: true,
	})

	if summary.Total != 3 {
		t.Errorf("expected total 3, got %d", summary.Total)
	}
	if summary.Succeeded != 1 {
		t.Errorf("expected succeeded 1, got %d", summary.Succeeded)
	}
	if summary.Failed != 1 {
		t.Errorf("expected failed 1, got %d", summary.Failed)
	}
	// Third command should NOT have executed.
	if len(summary.Results) != 2 {
		t.Fatalf("expected 2 results (stopped after error), got %d", len(summary.Results))
	}
}

func TestExecuteScriptContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	commands := []ScriptCommand{
		{Action: "step_over", Line: 1},
		{Action: "step_in", Line: 2},
	}

	callCount := 0
	executor := func(ctx context.Context, args map[string]any) (any, error) {
		callCount++
		if callCount == 1 {
			cancel() // Cancel after first command.
		}
		return map[string]string{"action": args["action"].(string)}, nil
	}

	summary := ExecuteScript(ctx, commands, executor, ScriptOptions{
		FilePath: "test.jsonl",
	})

	// First command executed, second should have been skipped due to cancellation.
	if len(summary.Results) != 1 {
		t.Fatalf("expected 1 result (cancelled), got %d", len(summary.Results))
	}
	if summary.Succeeded != 1 {
		t.Errorf("expected succeeded 1, got %d", summary.Succeeded)
	}
}
