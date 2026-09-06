package skills

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// countingToolAvailability is a ToolAvailabilityFunc that records every
// invocation so tests can assert whether the checker was called at all.
type countingToolAvailability struct {
	available map[string]bool
	calls     int
}

func (c *countingToolAvailability) fn(toolName string) bool {
	c.calls++
	return c.available[toolName]
}

func toolCheckExecutor(t *testing.T, checker ToolAvailabilityFunc) *Executor {
	t.Helper()
	mock := &mockChatter{
		response: &llm.Response{
			Content: "ok",
			Model:   "provider1/model-a",
			Usage:   llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	}
	return NewExecutor(testResolver(), WithClient(mock), WithValidatePrerequisites(true), WithToolAvailability(checker))
}

func TestExecutor_RequiresTools(t *testing.T) {
	const wantMsg = "requires unavailable tool(s)"

	t.Run("no requires-tools skips checker entirely", func(t *testing.T) {
		counter := &countingToolAvailability{available: map[string]bool{}}
		exec := toolCheckExecutor(t, counter.fn)

		skill := &Skill{Name: "no-tools-skill", Requires: []string{"code"}, Body: "Do something."}
		result, err := exec.Execute(context.Background(), skill, "test")
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if result == nil || result.Content != "ok" {
			t.Fatalf("expected successful execution, got %+v", result)
		}
		if counter.calls != 0 {
			t.Errorf("checker should never be called without requires-tools, got %d calls", counter.calls)
		}
	})

	t.Run("all required tools available executes", func(t *testing.T) {
		counter := &countingToolAvailability{available: map[string]bool{
			"web_fetch":          true,
			"cua-driver.capture": true,
		}}
		exec := toolCheckExecutor(t, counter.fn)

		skill := &Skill{
			Name:          "all-tools-skill",
			Requires:      []string{"code"},
			Body:          "Do something.",
			RequiresTools: []string{"web_fetch", "cua-driver.capture"},
		}
		result, err := exec.Execute(context.Background(), skill, "test")
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if result == nil || result.Content != "ok" {
			t.Fatalf("expected successful execution, got %+v", result)
		}
		if counter.calls < 2 {
			t.Errorf("checker should have been consulted for each tool, got %d calls", counter.calls)
		}
	})

	t.Run("one missing tool fails with exact message", func(t *testing.T) {
		counter := &countingToolAvailability{available: map[string]bool{
			"web_fetch": true,
		}}
		exec := toolCheckExecutor(t, counter.fn)

		skill := &Skill{
			Name:          "partial-skill",
			Requires:      []string{"code"},
			Body:          "Do something.",
			RequiresTools: []string{"web_fetch", "cua-driver.capture"},
		}
		_, err := exec.Execute(context.Background(), skill, "test")
		if err == nil {
			t.Fatal("expected ExecutorError for missing tool")
		}
		var execErr *ExecutorError
		if !asExecutorError(err, &execErr) {
			t.Fatalf("expected *ExecutorError, got %T: %v", err, err)
		}
		want := "skill partial-skill requires unavailable tool(s): cua-driver.capture — run 'meept doctor' to diagnose"
		if got := err.Error(); got != want {
			t.Errorf("message mismatch:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("multiple missing tools joined with comma", func(t *testing.T) {
		counter := &countingToolAvailability{available: map[string]bool{}}
		exec := toolCheckExecutor(t, counter.fn)

		skill := &Skill{
			Name:          "multi-missing-skill",
			Requires:      []string{"code"},
			Body:          "Do something.",
			RequiresTools: []string{"alpha.one", "beta.two", "gamma.three"},
		}
		_, err := exec.Execute(context.Background(), skill, "test")
		if err == nil {
			t.Fatal("expected ExecutorError for missing tools")
		}
		want := "skill multi-missing-skill requires unavailable tool(s): alpha.one, beta.two, gamma.three — run 'meept doctor' to diagnose"
		if got := err.Error(); got != want {
			t.Errorf("message mismatch:\n got: %s\nwant: %s", got, want)
		}
		if !strings.Contains(err.Error(), wantMsg) {
			t.Errorf("message should contain %q, got %q", wantMsg, err.Error())
		}
	})

	t.Run("nil checker with requires-tools skips check", func(t *testing.T) {
		// Executor constructed without the setter must not regress existing
		// skill execution even when the skill declares requires-tools.
		mock := &mockChatter{
			response: &llm.Response{
				Content: "ok",
				Model:   "provider1/model-a",
				Usage:   llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		}
		exec := NewExecutor(testResolver(), WithClient(mock), WithValidatePrerequisites(true))

		skill := &Skill{
			Name:          "legacy-skill",
			Requires:      []string{"code"},
			Body:          "Do something.",
			RequiresTools: []string{"definitely.missing"},
		}
		result, err := exec.Execute(context.Background(), skill, "test")
		if err != nil {
			t.Fatalf("Execute should skip unavailable-tool check when no checker set: %v", err)
		}
		if result == nil || result.Content != "ok" {
			t.Fatalf("expected successful execution, got %+v", result)
		}
	})

	t.Run("validatePrerequisites disabled skips check even with checker", func(t *testing.T) {
		counter := &countingToolAvailability{available: map[string]bool{}}
		mock := &mockChatter{
			response: &llm.Response{
				Content: "ok",
				Model:   "provider1/model-a",
				Usage:   llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		}
		// WithValidatePrerequisites deliberately omitted (default false).
		exec := NewExecutor(testResolver(), WithClient(mock), WithToolAvailability(counter.fn))

		skill := &Skill{
			Name:          "gated-off-skill",
			Requires:      []string{"code"},
			Body:          "Do something.",
			RequiresTools: []string{"missing.tool"},
		}
		result, err := exec.Execute(context.Background(), skill, "test")
		if err != nil {
			t.Fatalf("Execute should skip check when validatePrerequisites disabled: %v", err)
		}
		if result == nil || result.Content != "ok" {
			t.Fatalf("expected successful execution, got %+v", result)
		}
		if counter.calls != 0 {
			t.Errorf("checker should never be called when gate disabled, got %d calls", counter.calls)
		}
	})

	t.Run("typed-nil guard retains previously set checker", func(t *testing.T) {
		real := &countingToolAvailability{available: map[string]bool{"real.tool": true}}
		exec := toolCheckExecutor(t, real.fn)

		// SetToolAvailability(nil) after a real fn must refuse the nil and
		// retain the previously set checker (AGENTS.md Set* nil-guard rule).
		exec.SetToolAvailability(nil)
		if exec.toolAvailability == nil {
			t.Fatal("nil checker should be refused; previously set checker must be retained")
		}

		// Verify the retained checker still works: missing tool triggers the error.
		skill := &Skill{
			Name:          "guard-skill",
			Requires:      []string{"code"},
			Body:          "Do something.",
			RequiresTools: []string{"real.tool", "absent.tool"},
		}
		_, err := exec.Execute(context.Background(), skill, "test")
		if err == nil {
			t.Fatal("retained checker should report absent.tool as missing")
		}
		want := "skill guard-skill requires unavailable tool(s): absent.tool — run 'meept doctor' to diagnose"
		if got := err.Error(); got != want {
			t.Errorf("message mismatch:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("ExecuteWithMessages also enforces tool availability", func(t *testing.T) {
		counter := &countingToolAvailability{available: map[string]bool{}}
		exec := toolCheckExecutor(t, counter.fn)

		skill := &Skill{
			Name:          "multi-turn-skill",
			Requires:      []string{"code"},
			Body:          "Do something.",
			RequiresTools: []string{"missing.tool"},
		}
		_, err := exec.ExecuteWithMessages(context.Background(), skill, nil)
		if err == nil {
			t.Fatal("expected ExecutorError from ExecuteWithMessages for missing tool")
		}
		var execErr *ExecutorError
		if !asExecutorError(err, &execErr) {
			t.Fatalf("expected *ExecutorError, got %T: %v", err, err)
		}
	})
}

// asExecutorError is a tiny wrapper so the test file does not import errors
// separately in every subtest.
func asExecutorError(err error, target **ExecutorError) bool {
	for e := err; e != nil; {
		if execErr, ok := e.(*ExecutorError); ok {
			*target = execErr
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
