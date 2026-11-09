package builtin

// TimeoutMs contract pins (B1, 04-tool-surface/01-tool-timeout): the real
// builtin tools declare exactly the budgets the contract names, and the
// probe tool's zero-arg default honors the declared budget (never a bare
// context.DeadlineExceeded leak on expiry). Complements the package-level
// ToolDefaults.TimeoutMs()==0 pin in internal/tools/interface_test.go —
// these pin the BUILTIN tools' declarations, which the staged gate files
// (762dc9d3) did not cover.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/code/lsp"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/tools"
)

// TestTimeoutProbe_DeclaresCooperativeBudget pins the probe tool's declared
// budget: non-zero (opt-in arm) and equal to the exported contract constant.
func TestTimeoutProbe_DeclaresCooperativeBudget(t *testing.T) {
	tool := NewTimeoutProbeTool()
	var _ tools.Tool = tool
	if got := tool.TimeoutMs(); got != TimeoutProbeTimeout {
		t.Errorf("TimeoutProbeTool.TimeoutMs() = %v, want the contract constant %v", got, TimeoutProbeTimeout)
	}
	if TimeoutProbeTimeout <= 0 {
		t.Fatal("TimeoutProbeTimeout must be positive: a non-positive budget arms nothing")
	}
	if !tool.IsReadOnly(map[string]any{}) {
		t.Error("timeout_probe must be read-only (sleep has no side effects)")
	}
}

// TestTimeoutProbe_ZeroSleepHonorsBudget drives the probe with sleep_ms=0:
// the tool defaults to a 1s sleep INSIDE its 1.5s budget and must succeed —
// the zero-budget (undeclared) path in the executor is pinned by
// internal/tools/interface_test.go (ToolDefaults.TimeoutMsZero) and the
// executor arm by internal/agent/executor_timeout_test.go; this pins the
// real tool's success path inside its own declared budget.
func TestTimeoutProbe_ZeroSleepHonorsBudget(t *testing.T) {
	tool := NewTimeoutProbeTool()
	res, err := tool.Execute(context.Background(), map[string]any{"sleep_ms": float64(0)})
	if err != nil {
		t.Fatalf("Execute(sleep_ms=0) inside the declared budget: %v", err)
	}
	if res == nil {
		t.Fatal("Execute returned no result")
	}
}

// TestTimeoutProbe_ExpiryIsPlainDeadlineInsideTool pins the tool side of the
// substitution contract: a sleep beyond its own declared budget surfaces
// ctx.Err() from Execute, which the executor recognizes (derived timer fired,
// upstream alive) and substitutes into the structured TOOL_TIMEOUT error —
// the executor side of that substitution is pinned in
// internal/agent/executor_timeout_test.go.
func TestTimeoutProbe_ExpiryIsPlainDeadlineInsideTool(t *testing.T) {
	tool := NewTimeoutProbeTool()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := tool.Execute(ctx, map[string]any{"sleep_ms": float64(5000)})
	if err == nil {
		t.Fatal("sleep beyond the ctx deadline must error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("tool must surface ctx.Err() verbatim (the executor classifies), got %v", err)
	}
	if errors.Is(err, tools.ErrToolTimeout) {
		t.Error("the TOOL_TIMEOUT substitution belongs to the executor, not the tool")
	}
}

// TestLSPNavigate_DeclaredBudget pins the lsp_navigate budget at its
// contract value (60s) — a hung LSP query expires into a structured
// TOOL_TIMEOUT instead of hanging the turn (B2 consumer of B1).
func TestLSPNavigate_DeclaredBudget(t *testing.T) {
	tool, err := NewLSPNavigateTool(lsp.NewManager(config.LSPConfig{}))
	if err != nil {
		t.Fatalf("NewLSPNavigateTool: %v", err)
	}
	if got := tool.TimeoutMs(); got != lspTimeout {
		t.Errorf("TimeoutMs() = %v, want lspTimeout (%v)", got, lspTimeout)
	}
	if lspTimeout != 60*time.Second {
		t.Errorf("lspTimeout = %v, want the 60s contract budget", lspTimeout)
	}
}
