package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// newAutonomousProbeLoop builds a loop whose single tool records whether the
// execution context it received was marked autonomous.
func newAutonomousProbeLoop(t *testing.T, seen *[]bool) *AgentLoop {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_write", "write files", func(ctx context.Context, _ map[string]any) (any, error) {
		*seen = append(*seen, tools.AutonomousFromContext(ctx))
		return map[string]any{"ok": true}, nil
	}))
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
	)
	loop.executor = NewExecutor(registry, secChecker)
	return loop
}

var autonomousProbeCalls = []llm.ToolCall{
	{ID: "tc-1", Function: llm.ToolCallFunction{Name: "file_write", Arguments: `{"path":"/tmp/x","content":"y"}`}},
}

// TestAutonomousMarkerIsContextScopedAndDoesNotLatch pins F14 (bughunt
// 2026-09-12 wave). The daemon used to mark the LOOP autonomous for step jobs
// (`agentLoop.SetAutonomous(true)`) on a loop that could be the process-wide
// interactive loop ChatHandler serves, and never cleared it — so after one step
// job every later interactive chat turn wrote files directly, bypassing the
// pending-change preview/accept workflow.
//
// The invariant now: the autonomous marker reaches tools from the TURN's
// context only, and a later interactive turn on the SAME loop is interactive
// again. Both directions are asserted, so re-introducing a loop-level latch in
// the job path fails here.
func TestAutonomousMarkerIsContextScopedAndDoesNotLatch(t *testing.T) {
	var seen []bool
	loop := newAutonomousProbeLoop(t, &seen)

	// A step job's turn: the daemon passes tools.ContextWithAutonomous (see
	// internal/daemon stepJobTurnContext).
	loop.executeToolCalls(tools.ContextWithAutonomous(context.Background()), autonomousProbeCalls)
	if len(seen) != 1 {
		t.Fatalf("probe invocations = %d, want 1", len(seen))
	}
	if !seen[0] {
		t.Fatal("step-job turn context was not reported autonomous: staging tools would stage a change nothing accepts")
	}

	// The very next interactive turn on the SAME loop must be interactive:
	// no marker may survive the job.
	seen = nil
	loop.executeToolCalls(context.Background(), autonomousProbeCalls)
	if len(seen) != 1 {
		t.Fatalf("probe invocations = %d, want 1", len(seen))
	}
	if seen[0] {
		t.Fatal("an interactive turn inherited the autonomous marker: the pending-change preview/accept workflow is bypassed")
	}

	// And the loop-level field was never touched by either turn.
	loop.mu.RLock()
	latch := loop.autonomous
	loop.mu.RUnlock()
	if latch {
		t.Error("loop.autonomous is set after context-scoped turns; the marker must live on the turn's context")
	}
}

// TestSetAutonomousClearsBackToInteractive pins the escape hatch of the same
// finding: IF a caller still uses the loop-level marker, clearing it must
// restore interactive staging (the old code never called it with false).
func TestSetAutonomousClearsBackToInteractive(t *testing.T) {
	var seen []bool
	loop := newAutonomousProbeLoop(t, &seen)

	loop.SetAutonomous(true)
	loop.executeToolCalls(context.Background(), autonomousProbeCalls)
	if len(seen) != 1 || !seen[0] {
		t.Fatalf("autonomous loop turn reported %v, want [true]", seen)
	}

	loop.SetAutonomous(false)
	seen = nil
	loop.executeToolCalls(context.Background(), autonomousProbeCalls)
	if len(seen) != 1 || seen[0] {
		t.Fatalf("after SetAutonomous(false) the turn reported %v, want [false]", seen)
	}
}
