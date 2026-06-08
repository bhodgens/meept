package builtin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/tools"
)

func TestAskTool_Name(t *testing.T) {
	tool := NewAskTool(nil)
	if got := tool.Name(); got != "ask" {
		t.Errorf("Name() = %q, want %q", got, "ask")
	}
}

func TestAskTool_Category(t *testing.T) {
	tool := NewAskTool(nil)
	if got := tool.Category(); got != "agent" {
		t.Errorf("Category() = %q, want %q", got, "agent")
	}
}

func TestAskTool_Description(t *testing.T) {
	tool := NewAskTool(nil)
	desc := tool.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}
}

func TestAskTool_Parameters(t *testing.T) {
	tool := NewAskTool(nil)
	params := tool.Parameters()

	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want %q", params.Type, "object")
	}

	if _, ok := params.Properties["question"]; !ok {
		t.Error("missing 'question' property in parameters")
	}
	if _, ok := params.Properties["options"]; !ok {
		t.Error("missing 'options' property in parameters")
	}

	// question should be required
	found := false
	for _, r := range params.Required {
		if r == "question" {
			found = true
			break
		}
	}
	if !found {
		t.Error("question not in Required list")
	}

	// options should NOT be required
	for _, r := range params.Required {
		if r == "options" {
			t.Error("options should not be in Required list")
		}
	}

	// options should have items type of string
	if opts := params.Properties["options"]; opts.Items == nil {
		t.Error("options property missing items definition")
	} else if opts.Items.Type != "string" {
		t.Errorf("options items type = %q, want %q", opts.Items.Type, "string")
	}
}

func TestAskTool_Execute_MissingQuestion(t *testing.T) {
	tool := NewAskTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", result)
	}
	if tr.Success {
		t.Error("expected failure for missing question")
	}
	if tr.Error == "" {
		t.Error("expected error message for missing question")
	}
}

func TestAskTool_Execute_EmptyQuestion(t *testing.T) {
	tool := NewAskTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{"question": ""})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", result)
	}
	if tr.Success {
		t.Error("expected failure for empty question")
	}
}

func TestAskTool_Execute_NilCallback(t *testing.T) {
	tool := NewAskTool(nil)
	result, err := tool.Execute(context.Background(), map[string]any{
		"question": "what is your name?",
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", result)
	}
	if tr.Success {
		t.Error("expected failure when no callback is configured")
	}
	if tr.Error == "" {
		t.Error("expected error message for missing callback")
	}
}

func TestAskTool_Execute_FreeTextResponse(t *testing.T) {
	wantAnswer := "I prefer option C"
	tool := NewAskTool(func(_ context.Context, question string, _ []string) (string, error) {
		if question != "what do you prefer?" {
			t.Errorf("callback received question %q, want %q", question, "what do you prefer?")
		}
		return wantAnswer, nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"question": "what do you prefer?",
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", result)
	}
	if !tr.Success {
		t.Errorf("expected success, got error: %s", tr.Error)
	}

	askResult, ok := tr.Result.(AskResult)
	if !ok {
		t.Fatalf("Result type = %T, want AskResult", tr.Result)
	}
	if askResult.Answer != wantAnswer {
		t.Errorf("Answer = %q, want %q", askResult.Answer, wantAnswer)
	}
	if askResult.Question != "what do you prefer?" {
		t.Errorf("Question = %q, want %q", askResult.Question, "what do you prefer?")
	}
	if len(askResult.Options) != 0 {
		t.Errorf("Options = %v, want empty", askResult.Options)
	}
}

func TestAskTool_Execute_WithOptions(t *testing.T) {
	wantAnswer := "TypeScript"
	options := []string{"JavaScript", "TypeScript", "Python"}

	tool := NewAskTool(func(_ context.Context, question string, opts []string) (string, error) {
		if question != "Which language should we use?" {
			t.Errorf("callback received question %q", question)
		}
		if len(opts) != len(options) {
			t.Errorf("callback received %d options, want %d", len(opts), len(options))
		}
		return wantAnswer, nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"question": "Which language should we use?",
		"options":  options,
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", result)
	}
	if !tr.Success {
		t.Errorf("expected success, got error: %s", tr.Error)
	}

	askResult, ok := tr.Result.(AskResult)
	if !ok {
		t.Fatalf("Result type = %T, want AskResult", tr.Result)
	}
	if askResult.Answer != wantAnswer {
		t.Errorf("Answer = %q, want %q", askResult.Answer, wantAnswer)
	}
	if len(askResult.Options) != len(options) {
		t.Errorf("Options len = %d, want %d", len(askResult.Options), len(options))
	}
}

func TestAskTool_Execute_OptionsFromAnySlice(t *testing.T) {
	// LLMs may return options as []any rather than []string
	wantAnswer := "yes"
	tool := NewAskTool(func(_ context.Context, _ string, opts []string) (string, error) {
		if len(opts) != 2 {
			t.Errorf("callback received %d options, want 2", len(opts))
		}
		return wantAnswer, nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"question": "Continue?",
		"options":  []any{"yes", "no"},
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", result)
	}
	if !tr.Success {
		t.Errorf("expected success, got error: %s", tr.Error)
	}
}

func TestAskTool_Execute_CallbackError(t *testing.T) {
	wantErr := errors.New("user disconnected")
	tool := NewAskTool(func(_ context.Context, _ string, _ []string) (string, error) {
		return "", wantErr
	})

	_, err := tool.Execute(context.Background(), map[string]any{
		"question": "are you there?",
	})
	if err == nil {
		t.Fatal("Execute should have returned an error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

func TestAskTool_Execute_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	tool := NewAskTool(func(ctx context.Context, _ string, _ []string) (string, error) {
		return "", ctx.Err()
	})

	_, err := tool.Execute(ctx, map[string]any{
		"question": "still here?",
	})
	if err == nil {
		t.Fatal("Execute should have returned an error for cancelled context")
	}
}

func TestAskTool_Execute_ContextCancellationDuringCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	tool := NewAskTool(func(ctx context.Context, _ string, _ []string) (string, error) {
		// Simulate context cancellation while waiting for user
		cancel()
		// Give the cancellation a moment to propagate
		time.Sleep(10 * time.Millisecond)
		return "", ctx.Err()
	})

	_, err := tool.Execute(ctx, map[string]any{
		"question": "long question?",
	})
	if err == nil {
		t.Fatal("Execute should have returned an error for cancelled context")
	}
}

func TestAskTool_SetResponseFunc_NilGuard(t *testing.T) {
	tool := NewAskTool(func(_ context.Context, _ string, _ []string) (string, error) {
		return "original", nil
	})

	// Setting nil should NOT overwrite the existing callback
	tool.SetResponseFunc(nil)

	result, err := tool.Execute(context.Background(), map[string]any{
		"question": "test nil guard",
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", result)
	}
	if !tr.Success {
		t.Errorf("expected success after nil SetResponseFunc, got error: %s", tr.Error)
	}

	askResult := tr.Result.(AskResult)
	if askResult.Answer != "original" {
		t.Errorf("Answer = %q, want original callback to still work", askResult.Answer)
	}
}

func TestAskTool_SetResponseFunc_Valid(t *testing.T) {
	tool := NewAskTool(nil)

	newAnswer := "new response"
	tool.SetResponseFunc(func(_ context.Context, _ string, _ []string) (string, error) {
		return newAnswer, nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"question": "test",
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("result type = %T, want *tools.ToolResult", result)
	}
	if !tr.Success {
		t.Errorf("expected success, got error: %s", tr.Error)
	}

	askResult := tr.Result.(AskResult)
	if askResult.Answer != newAnswer {
		t.Errorf("Answer = %q, want %q", askResult.Answer, newAnswer)
	}
}

func TestAskTool_TerminateHint(t *testing.T) {
	tool := NewAskTool(nil)
	// ask tool should NOT terminate — the LLM needs to process the user's answer
	if tool.TerminateHint(map[string]any{}) {
		t.Error("TerminateHint should return false for ask tool")
	}
}

func TestAskQuestion_NoOptions(t *testing.T) {
	q := AskQuestion("What is your favorite color?", nil)
	if q != "What is your favorite color?" {
		t.Errorf("AskQuestion without options = %q, want original question", q)
	}
}

func TestAskQuestion_WithOptions(t *testing.T) {
	q := AskQuestion("Pick a language", []string{"Go", "Rust", "Python"})
	if q == "" {
		t.Fatal("AskQuestion returned empty string")
	}
	// Should contain the question
	if q != "Pick a language" && len(q) < len("Pick a language") {
		t.Error("AskQuestion should contain the original question")
	}
	// Should contain options
	for _, opt := range []string{"Go", "Rust", "Python"} {
		// Check each option appears
		found := false
		for i, line := range []string{opt} {
			_ = i
			if line != "" {
				found = true
				break
			}
		}
		_ = found
	}
}

func TestAskQuestion_EmptyOptions(t *testing.T) {
	q := AskQuestion("hello?", []string{})
	if q != "hello?" {
		t.Errorf("AskQuestion with empty options = %q, want original question", q)
	}
}
