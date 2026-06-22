package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/tools/builtin"
)

// fakeTool is a minimal executeRepeater that returns a canned result.
type fakeTool struct {
	lastArgs map[string]any
	result   any
	err      error
}

func (t *fakeTool) Execute(_ context.Context, args map[string]any) (any, error) {
	t.lastArgs = args
	return t.result, t.err
}

// stubRunner returns a canned model state regardless of input.
func stubRunner(confirmed bool) func(ConfirmationModel) ConfirmationModel {
	return func(m ConfirmationModel) ConfirmationModel {
		m.confirmed = confirmed
		m.cancelled = !confirmed
		return m
	}
}

func TestHandleConfirmationResult_NotAMap(t *testing.T) {
	i := NewConfirmationInterceptor()
	out, intercepted, err := i.HandleConfirmationResult("not a map")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intercepted {
		t.Error("should not intercept non-map result")
	}
	if out != "not a map" {
		t.Errorf("expected pass-through, got %v", out)
	}
}

func TestHandleConfirmationResult_NotConfirmation(t *testing.T) {
	i := NewConfirmationInterceptor()
	result := map[string]any{"success": true}
	out, intercepted, err := i.HandleConfirmationResult(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if intercepted {
		t.Error("should not intercept non-confirmation result")
	}
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map passthrough, got %T", out)
	}
	if !outMap["success"].(bool) {
		t.Error("passthrough result was mutated")
	}
}

func TestHandleConfirmationResult_Confirmed(t *testing.T) {
	i := NewConfirmationInterceptor()
	i.SetRunner(stubRunner(true))
	resp := builtin.ConfirmationResponse("mark_superseded", false, "supersede a with b", nil)
	out, intercepted, err := i.HandleConfirmationResult(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !intercepted {
		t.Error("confirmation should be intercepted")
	}
	if out != nil {
		t.Errorf("confirmed path returns nil final result so caller re-executes, got %v", out)
	}
}

func TestHandleConfirmationResult_Declined(t *testing.T) {
	i := NewConfirmationInterceptor()
	i.SetRunner(stubRunner(false))
	resp := builtin.ConfirmationResponse("mark_superseded", false, "supersede a with b", nil)
	out, intercepted, err := i.HandleConfirmationResult(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !intercepted {
		t.Error("confirmation should be intercepted")
	}
	declined, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected declined map, got %T", out)
	}
	if flag, _ := declined["declined"].(bool); !flag {
		t.Errorf("expected declined=true, got %v", declined["declined"])
	}
}

func TestHandleConfirmationResult_NilRunnerDeclines(t *testing.T) {
	i := &ConfirmationInterceptor{runModal: nil}
	resp := builtin.ConfirmationResponse("mark_superseded", false, "summary", nil)
	out, intercepted, err := i.HandleConfirmationResult(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !intercepted {
		t.Error("should intercept when no runner configured (decline path)")
	}
	declined, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected declined map, got %T", out)
	}
	if flag, _ := declined["declined"].(bool); !flag {
		t.Errorf("nil runner should decline, got %v", declined["declined"])
	}
}

func TestConfirmWithReexecute_Confirmed(t *testing.T) {
	i := NewConfirmationInterceptor()
	i.SetRunner(stubRunner(true))
	resp := builtin.ConfirmationResponse("mark_superseded", false, "summary", nil)
	tool := &fakeTool{result: map[string]any{"success": true}}
	final, err := i.ConfirmWithReexecute(context.Background(), tool, map[string]any{"old_id": "a", "new_id": "b"}, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outMap, ok := final.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", final)
	}
	if !outMap["success"].(bool) {
		t.Error("expected re-executed success result")
	}
	if v, _ := tool.lastArgs["confirmed"].(bool); !v {
		t.Errorf("expected re-execution with confirmed=true, got args=%v", tool.lastArgs)
	}
}

func TestConfirmWithReexecute_Declined(t *testing.T) {
	i := NewConfirmationInterceptor()
	i.SetRunner(stubRunner(false))
	resp := builtin.ConfirmationResponse("mark_superseded", false, "summary", nil)
	tool := &fakeTool{result: map[string]any{"success": true}}
	final, err := i.ConfirmWithReexecute(context.Background(), tool, map[string]any{"old_id": "a"}, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	declined, ok := final.(map[string]any)
	if !ok {
		t.Fatalf("expected declined map, got %T", final)
	}
	if flag, _ := declined["declined"].(bool); !flag {
		t.Errorf("expected declined=true, got %v", declined)
	}
	if tool.lastArgs != nil {
		t.Errorf("tool should not be re-executed on decline, got args=%v", tool.lastArgs)
	}
}

func TestConfirmWithReexecute_NoConfirmation(t *testing.T) {
	i := NewConfirmationInterceptor()
	i.SetRunner(stubRunner(true)) // would confirm if asked
	plainResult := map[string]any{"success": true}
	tool := &fakeTool{result: plainResult}
	final, err := i.ConfirmWithReexecute(context.Background(), tool, map[string]any{"k": "v"}, plainResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-confirmation results pass through unchanged.
	if final != plainResult {
		t.Errorf("expected passthrough, got %v", final)
	}
}

func TestConfirmWithReexecute_ToolError(t *testing.T) {
	i := NewConfirmationInterceptor()
	i.SetRunner(stubRunner(true))
	resp := builtin.ConfirmationResponse("mark_superseded", false, "summary", nil)
	tool := &fakeTool{err: errors.New("boom")}
	_, err := i.ConfirmWithReexecute(context.Background(), tool, map[string]any{"x": "y"}, resp)
	if err == nil || err.Error() != "boom" {
		t.Errorf("expected boom error, got %v", err)
	}
}

func TestSetRunner_NilSafety(t *testing.T) {
	i := NewConfirmationInterceptor()
	original := i.runModal
	i.SetRunner(nil)
	if i.runModal != original {
		t.Error("SetRunner(nil) should not replace the runner")
	}
}
