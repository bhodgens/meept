package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// mockBeforeToolCallHook is a test hook for BeforeToolCall.
type mockBeforeToolCallHook struct {
	block  bool
	reason string
	called bool
}

func (m *mockBeforeToolCallHook) BeforeToolCall(_ context.Context, _ llm.ToolCall) BlockResult {
	m.called = true
	return BlockResult{Block: m.block, Reason: m.reason}
}

// mockAfterToolCallHook is a test hook for AfterToolCall.
type mockAfterToolCallHook struct {
	override bool
	result   *ExecutionResult
	reason   string
	called   bool
}

func (m *mockAfterToolCallHook) AfterToolCall(_ context.Context, _ llm.ToolCall, _ *ExecutionResult) OverrideResult {
	m.called = true
	return OverrideResult{Override: m.override, Result: m.result, Reason: m.reason}
}

// mockShouldStopHook is a test hook for ShouldStopAfterTurn.
type mockShouldStopHook struct {
	stop   bool
	reason string
	called bool
}

func (m *mockShouldStopHook) ShouldStopAfterTurn(_ context.Context, _ TurnState) StopDecision {
	m.called = true
	return StopDecision{Stop: m.stop, Reason: m.reason}
}

// mockTransformContextHook is a test hook for TransformContext.
type mockTransformContextHook struct {
	modified bool
	reason   string
	called   bool
}

func (m *mockTransformContextHook) TransformContext(_ context.Context, msgs []llm.ChatMessage, _ []llm.ToolDefinition) ContextTransform {
	m.called = true
	return ContextTransform{
		Messages: msgs,
		Modified: m.modified,
		Reason:   m.reason,
	}
}

func TestHookRegistry_Empty(t *testing.T) {
	reg := NewHookRegistry(nil)

	// All Run methods on empty registry should return zero-value results
	toolCall := llm.ToolCall{ID: "tc1", Function: llm.ToolCallFunction{Name: "shell", Arguments: "{}"}}

	blockResult := reg.RunBeforeToolCalls(context.Background(), toolCall)
	if blockResult.Block {
		t.Error("expected no block from empty registry")
	}

	overrideResult := reg.RunAfterToolCalls(context.Background(), toolCall, &ExecutionResult{Success: true})
	if overrideResult.Override {
		t.Error("expected no override from empty registry")
	}

	stopDecision := reg.RunShouldStopAfterTurn(context.Background(), TurnState{})
	if stopDecision.Stop {
		t.Error("expected no stop from empty registry")
	}

	transform := reg.RunTransformContext(context.Background(), nil, nil)
	if transform.Modified {
		t.Error("expected no modification from empty registry")
	}

	mod := reg.RunPrepareNextTurn(context.Background(), TurnState{})
	if mod.Modified {
		t.Error("expected no modification from empty registry")
	}
}

func TestHookRegistry_BeforeToolCall_Single(t *testing.T) {
	reg := NewHookRegistry(nil)
	hook := &mockBeforeToolCallHook{block: false, reason: ""}

	reg.RegisterBeforeToolCall("test", HookPriorityNormal, hook)

	toolCall := llm.ToolCall{ID: "tc1", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command":"ls"}`}}
	result := reg.RunBeforeToolCalls(context.Background(), toolCall)

	if !hook.called {
		t.Error("expected hook to be called")
	}
	if result.Block {
		t.Error("expected no block")
	}
}

func TestHookRegistry_BeforeToolCall_Block(t *testing.T) {
	reg := NewHookRegistry(nil)
	hook := &mockBeforeToolCallHook{block: true, reason: "security risk"}

	reg.RegisterBeforeToolCall("security", HookPriorityCritical, hook)

	toolCall := llm.ToolCall{ID: "tc1", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{"command":"rm -rf /"}`}}
	result := reg.RunBeforeToolCalls(context.Background(), toolCall)

	if !hook.called {
		t.Error("expected hook to be called")
	}
	if !result.Block {
		t.Error("expected block")
	}
	if result.Reason != "security risk" {
		t.Errorf("expected reason 'security risk', got %q", result.Reason)
	}
}

func TestHookRegistry_BeforeToolCall_PriorityOrder(t *testing.T) {
	reg := NewHookRegistry(nil)

	var order []string
	reg.RegisterBeforeToolCall("low", HookPriorityLow, &orderTrackingHook{name: "low", order: &order})
	reg.RegisterBeforeToolCall("critical", HookPriorityCritical, &orderTrackingHook{name: "critical", order: &order})
	reg.RegisterBeforeToolCall("normal", HookPriorityNormal, &orderTrackingHook{name: "normal", order: &order})

	toolCall := llm.ToolCall{ID: "tc1", Function: llm.ToolCallFunction{Name: "test", Arguments: "{}"}}
	reg.RunBeforeToolCalls(context.Background(), toolCall)

	if len(order) != 3 {
		t.Fatalf("expected 3 hooks called, got %d", len(order))
	}
	if order[0] != "critical" {
		t.Errorf("expected first hook 'critical', got %q", order[0])
	}
	if order[1] != "normal" {
		t.Errorf("expected second hook 'normal', got %q", order[1])
	}
	if order[2] != "low" {
		t.Errorf("expected third hook 'low', got %q", order[2])
	}
}

func TestHookRegistry_BeforeToolCall_ShortCircuit(t *testing.T) {
	reg := NewHookRegistry(nil)

	blockingHook := &mockBeforeToolCallHook{block: true, reason: "blocked"}
	monitorHook := &mockBeforeToolCallHook{block: false, reason: ""}

	// Critical priority should run first and block the monitor
	reg.RegisterBeforeToolCall("monitor", HookPriorityMonitor, monitorHook)
	reg.RegisterBeforeToolCall("security", HookPriorityCritical, blockingHook)

	toolCall := llm.ToolCall{ID: "tc1", Function: llm.ToolCallFunction{Name: "test", Arguments: "{}"}}
	result := reg.RunBeforeToolCalls(context.Background(), toolCall)

	if !result.Block {
		t.Error("expected block from critical hook")
	}
	if monitorHook.called {
		t.Error("expected monitor hook NOT called (short-circuited)")
	}
}

func TestHookRegistry_AfterToolCall_Override(t *testing.T) {
	reg := NewHookRegistry(nil)
	overriddenResult := &ExecutionResult{ToolCallID: "tc1", Success: true, Result: "overridden"}

	hook := &mockAfterToolCallHook{override: true, result: overriddenResult, reason: "audit"}
	reg.RegisterAfterToolCall("audit", HookPriorityHigh, hook)

	toolCall := llm.ToolCall{ID: "tc1", Function: llm.ToolCallFunction{Name: "test", Arguments: "{}"}}
	originalResult := &ExecutionResult{ToolCallID: "tc1", Success: true, Result: "original"}

	result := reg.RunAfterToolCalls(context.Background(), toolCall, originalResult)

	if !hook.called {
		t.Error("expected hook to be called")
	}
	if !result.Override {
		t.Error("expected override")
	}
	if result.Result.Result != "overridden" {
		t.Errorf("expected overridden result, got %v", result.Result.Result)
	}
}

func TestHookRegistry_TransformContext(t *testing.T) {
	reg := NewHookRegistry(nil)
	hook := &mockTransformContextHook{modified: true, reason: "sanitization"}

	reg.RegisterTransformContext("security", HookPriorityCritical, hook)

	msgs := []llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}
	transform := reg.RunTransformContext(context.Background(), msgs, nil)

	if !hook.called {
		t.Error("expected hook to be called")
	}
	if !transform.Modified {
		t.Error("expected modification")
	}
	if transform.Reason != "sanitization" {
		t.Errorf("expected reason 'sanitization', got %q", transform.Reason)
	}
}

func TestHookRegistry_ShouldStopAfterTurn(t *testing.T) {
	reg := NewHookRegistry(nil)
	hook := &mockShouldStopHook{stop: true, reason: "budget exhausted"}

	reg.RegisterShouldStopAfterTurn("budget", HookPriorityHigh, hook)

	decision := reg.RunShouldStopAfterTurn(context.Background(), TurnState{})

	if !hook.called {
		t.Error("expected hook to be called")
	}
	if !decision.Stop {
		t.Error("expected stop")
	}
	if decision.Reason != "budget exhausted" {
		t.Errorf("expected reason 'budget exhausted', got %q", decision.Reason)
	}
}

func TestHookRegistry_Unregister(t *testing.T) {
	reg := NewHookRegistry(nil)

	hook := &mockBeforeToolCallHook{block: true, reason: "blocked"}
	reg.RegisterBeforeToolCall("removable", HookPriorityNormal, hook)

	toolCall := llm.ToolCall{ID: "tc1", Function: llm.ToolCallFunction{Name: "test", Arguments: "{}"}}

	// Before unregister, hook should block
	result := reg.RunBeforeToolCalls(context.Background(), toolCall)
	if !result.Block {
		t.Error("expected block before unregister")
	}

	// Unregister
	reg.Unregister("removable")

	// After unregister, should not block
	result = reg.RunBeforeToolCalls(context.Background(), toolCall)
	if result.Block {
		t.Error("expected no block after unregister")
	}
}

// orderTrackingHook tracks call order for testing priority sorting.
type orderTrackingHook struct {
	name  string
	order *[]string
}

func (h *orderTrackingHook) BeforeToolCall(_ context.Context, _ llm.ToolCall) BlockResult {
	*h.order = append(*h.order, h.name)
	return BlockResult{}
}
