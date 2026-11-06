package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// gateTool is a stub tool carrying a fixed category and posture so gate
// tests can exercise per-category and per-posture behavior.
type gateTool struct {
	name       string
	category   string
	readOnly   bool
	concSafe   bool
	execCalled *bool
	execErr    error
}

func (t *gateTool) Name() string        { return t.name }
func (t *gateTool) Description() string { return "gate stub " + t.name }
func (t *gateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       "object",
		Properties: map[string]llm.ParameterProperty{},
	}
}
func (t *gateTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	if t.execCalled != nil {
		*t.execCalled = true
	}
	if t.execErr != nil {
		return nil, t.execErr
	}
	return map[string]any{"ok": true}, nil
}
func (t *gateTool) IsReadOnly(map[string]any) bool        { return t.readOnly }
func (t *gateTool) IsConcurrencySafe(map[string]any) bool { return t.concSafe }
func (t *gateTool) Category() string                      { return t.category }

// recordingGate is a stub ExecGate that records every Before/After call.
type recordingGate struct {
	before     []string // tool name per BeforeExecute call
	after      []string // "tool:success" or "tool:error:<msg>" per AfterExecute
	beforeErr  error    // when set, BeforeExecute blocks with this error
	observedRO map[string]bool
}

func (g *recordingGate) BeforeExecute(_ context.Context, tool Tool, _ map[string]any) error {
	g.before = append(g.before, tool.Name())
	if g.observedRO == nil {
		g.observedRO = map[string]bool{}
	}
	g.observedRO[tool.Name()] = tool.IsReadOnly(map[string]any{})
	if g.beforeErr != nil {
		return g.beforeErr
	}
	return nil
}

func (g *recordingGate) AfterExecute(_ context.Context, tool Tool, _ map[string]any, _ *ToolResult, err error) {
	label := tool.Name() + ":success"
	if err != nil {
		label = tool.Name() + ":error:" + err.Error()
	}
	g.after = append(g.after, label)
}

// TestExecGateAppliesToAllCategories pins F2: the injected gate's
// BeforeExecute and AfterExecute fire for EVERY tool category, not just
// shell. RED until 02-gate.md adds ExecGate + SetGate + the Execute
// invocation points.
func TestExecGateAppliesToAllCategories(t *testing.T) {
	categories := []string{"shell", "file", "mcp", "custom"}

	reg := NewRegistry(nil)
	gate := &recordingGate{}
	execFlags := make(map[string]*bool, len(categories))
	for _, cat := range categories {
		called := false
		execFlags[cat] = &called
		reg.Register(&gateTool{name: "tool_" + cat, category: cat, execCalled: &called})
	}

	reg.SetGate(gate)

	for _, cat := range categories {
		if _, err := reg.Execute(context.Background(), "tool_"+cat, map[string]any{}); err != nil {
			t.Fatalf("Execute(tool_%s) returned error: %v", cat, err)
		}
	}

	if len(gate.before) != len(categories) {
		t.Fatalf("BeforeExecute fired %d times, want %d (one per category %v); got %v",
			len(gate.before), len(categories), categories, gate.before)
	}
	if len(gate.after) != len(categories) {
		t.Fatalf("AfterExecute fired %d times, want %d; got %v",
			len(gate.after), len(categories), gate.after)
	}
	for _, cat := range categories {
		if !*execFlags[cat] {
			t.Errorf("tool_%s: gate ran but the tool body never executed", cat)
		}
	}
}

// TestExecGateBlocksAndTypedError pins the second half of F2: a
// BeforeExecute error blocks tool.Execute and surfaces a typed error
// result naming the reason; the tool body never runs.
func TestExecGateBlocksAndTypedError(t *testing.T) {
	reg := NewRegistry(nil)
	blockErr := errors.New("blocked by gate: forbidden verb")
	bodyRan := false
	reg.Register(&gateTool{name: "blocked_tool", category: "custom", execCalled: &bodyRan})

	gate := &recordingGate{beforeErr: blockErr}
	reg.SetGate(gate)

	res, err := reg.Execute(context.Background(), "blocked_tool", map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned Go error %v; a blocked call must surface as an error RESULT, not an error", err)
	}
	if res == nil {
		t.Fatal("Execute returned nil result for a blocked call")
	}
	if res.Success {
		t.Error("blocked call result reports Success=true; want a failure result")
	}
	if !strings.Contains(res.Error, "blocked by gate: forbidden verb") {
		t.Errorf("error result %q does not name the gate reason", res.Error)
	}
	if bodyRan {
		t.Error("tool body executed despite BeforeExecute blocking; the block must prevent Execute")
	}
}

// TestExecGateAfterFiresOnError pins F3's interface-side shape: AfterExecute
// observes a FAILING execution (Go error and error-result shapes), so the
// audit emitter behind the real gate sees every execution, not only
// successes.
func TestExecGateAfterFiresOnError(t *testing.T) {
	t.Run("AfterExecute observes a Go-error execution", func(t *testing.T) {
		reg := NewRegistry(nil)
		called := false
		reg.Register(&gateTool{name: "goerr_tool", category: "custom", execCalled: &called,
			execErr: errors.New("boom")})

		gate := &recordingGate{}
		reg.SetGate(gate)

		res, err := reg.Execute(context.Background(), "goerr_tool", map[string]any{})
		if err != nil {
			t.Fatalf("Execute returned Go error %v; failures surface as error results", err)
		}
		if res.Success {
			t.Fatal("failing tool produced Success=true result")
		}
		found := false
		for _, label := range gate.after {
			if strings.HasPrefix(label, "goerr_tool:error:") {
				found = true
			}
		}
		if !found {
			t.Errorf("AfterExecute did not observe the failing execution; after = %v", gate.after)
		}
	})

	t.Run("AfterExecute observes an error-result execution", func(t *testing.T) {
		// A tool returning a *ToolResult failure envelope (not a Go error)
		// must still reach AfterExecute so the audit chain records it.
		reg := NewRegistry(nil)
		reg.Register(&gateTool{name: "errres_tool", category: "custom"})
		// Wrap: registry Execute path with a tool whose Execute returns an
		// error ToolResult directly. gateTool returns success payloads, so
		// use a dedicated inline tool here.
		reg2 := NewRegistry(nil)
		reg2.Register(&errResTool{gateTool: gateTool{name: "errres_tool", category: "custom"}})

		gate := &recordingGate{}
		reg2.SetGate(gate)

		res, err := reg2.Execute(context.Background(), "errres_tool", map[string]any{})
		if err != nil {
			t.Fatalf("Execute returned Go error %v", err)
		}
		if res.Success {
			t.Fatal("expected a failure result from errres_tool")
		}
		found := false
		for _, label := range gate.after {
			if strings.HasPrefix(label, "errres_tool:") {
				found = true
			}
		}
		if !found {
			t.Errorf("AfterExecute never fired for the error-result execution; after = %v", gate.after)
		}
	})
}

// errResTool returns a failure ToolResult envelope from Execute.
type errResTool struct {
	gateTool
}

func (t *errResTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	res := NewErrorResult("envelope failure")
	return res, nil
}

// TestUndeclaredPostureFailClosed pins F5 (G4): a tool whose provider
// declared no posture is reported to the gate as read-only=false regardless
// of the tool's own claim - the MCPTool fail-closed inheritance.
func TestUndeclaredPostureFailClosed(t *testing.T) {
	reg := NewRegistry(nil)
	called := false
	// The tool CLAIMS read-only; an undeclared posture must override that
	// claim to false before the gate sees it.
	reg.Register(&gateTool{name: "claiming_tool", category: "custom", readOnly: true, execCalled: &called})

	gate := &recordingGate{}
	reg.SetGate(gate)

	if _, err := reg.Execute(context.Background(), "claiming_tool", map[string]any{}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	ro, ok := gate.observedRO["claiming_tool"]
	if !ok {
		t.Fatal("gate never observed claiming_tool; cannot check posture")
	}
	if ro {
		t.Fatal("undeclared-posture tool was reported read-only=true to the gate; fail-closed posture inheritance (G4) missing - must report read-only=false regardless of the tool claim")
	}
}

// TestNilGatePreservesBehavior pins F6: a registry with NO gate (or a nil
// gate) behaves exactly as today - success and failure paths unchanged.
// This is the compatibility pin; it must be GREEN immediately and stay
// green after 02-gate.md lands.
func TestNilGatePreservesBehavior(t *testing.T) {
	t.Run("success path unchanged", func(t *testing.T) {
		reg := NewRegistry(nil)
		called := false
		reg.Register(&gateTool{name: "plain_tool", category: "general", execCalled: &called})

		res, err := reg.Execute(context.Background(), "plain_tool", map[string]any{})
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
		if res == nil || !res.Success {
			t.Errorf("success path broken: res=%+v err=%v", res, err)
		}
		if !called {
			t.Error("tool body did not run on the nil-gate path")
		}
	})

	t.Run("nil SetGate keeps legacy behavior", func(t *testing.T) {
		reg := NewRegistry(nil)
		reg.SetGate(nil) // must be a legal no-op restoring today's behavior
		called := false
		reg.Register(&gateTool{name: "plain_tool2", category: "general", execCalled: &called})

		res, err := reg.Execute(context.Background(), "plain_tool2", map[string]any{})
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
		if res == nil || !res.Success {
			t.Errorf("nil-gate success path broken: res=%+v err=%v", res, err)
		}
	})

	t.Run("missing tool error unchanged", func(t *testing.T) {
		reg := NewRegistry(nil)
		_, err := reg.Execute(context.Background(), "no_such_tool", map[string]any{})
		if err == nil {
			t.Fatal("expected the legacy not-found error")
		}
		if !strings.Contains(err.Error(), "tool not found: no_such_tool") {
			t.Errorf("not-found error changed: %v", err)
		}
	})

	t.Run("validation failure unchanged", func(t *testing.T) {
		reg := NewRegistry(nil)
		reg.Register(&mockTool{
			name: "strict_tool",
			params: llm.FunctionParameters{
				Type: "object",
				Properties: map[string]llm.ParameterProperty{
					"name": {Type: "string"},
				},
				Required: []string{"name"},
			},
		})
		res, err := reg.Execute(context.Background(), "strict_tool", map[string]any{})
		if err != nil || res == nil {
			t.Fatalf("validation-path Execute returned err=%v res=%v; want a handled error result", err, res)
		}
		if res.Success || res.ErrCode != ErrCodeInvalidArgs {
			t.Errorf("expected invalid_args error result, got success=%v errcode=%q", res.Success, res.ErrCode)
		}
	})
}
