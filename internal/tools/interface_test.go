package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
)

func TestToolDefaults(t *testing.T) {
	d := ToolDefaults{}
	assert.False(t, d.IsReadOnly(nil), "default IsReadOnly should be false")
	assert.False(t, d.IsConcurrencySafe(nil), "default IsConcurrencySafe should be false")
	assert.False(t, d.IsReadOnly(map[string]any{"key": "val"}), "default IsReadOnly with input should be false")
	assert.False(t, d.IsConcurrencySafe(map[string]any{"key": "val"}), "default IsConcurrencySafe with input should be false")
}

// resultSizerStub implements ResultSizer with a declared floor.
type resultSizerStub struct {
	ToolDefaults
	floor int
}

func (s resultSizerStub) Name() string                       { return "result_sizer_stub" }
func (s resultSizerStub) Description() string                { return "stub" }
func (s resultSizerStub) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (s resultSizerStub) Execute(ctx context.Context, args map[string]any) (any, error) {
	return nil, nil //nolint:nilnil // test stub: never invoked, only satisfies the interface
}
func (s resultSizerStub) MaxResultTokens() int { return s.floor }

// plainToolStub does NOT implement ResultSizer.
type plainToolStub struct {
	ToolDefaults
}

func (s plainToolStub) Name() string                       { return "plain_tool_stub" }
func (s plainToolStub) Description() string                { return "stub" }
func (s plainToolStub) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (s plainToolStub) Execute(ctx context.Context, args map[string]any) (any, error) {
	return nil, nil //nolint:nilnil // test stub: never invoked, only satisfies the interface
}

func TestGetMaxResultTokens(t *testing.T) {
	t.Run("tool without ResultSizer returns 0", func(t *testing.T) {
		assert.Equal(t, 0, GetMaxResultTokens(plainToolStub{}))
	})

	t.Run("negative floor returns 0", func(t *testing.T) {
		assert.Equal(t, 0, GetMaxResultTokens(resultSizerStub{floor: -5}))
	})

	t.Run("zero floor returns 0", func(t *testing.T) {
		assert.Equal(t, 0, GetMaxResultTokens(resultSizerStub{floor: 0}))
	})

	t.Run("declared floor 1400 returned as-is", func(t *testing.T) {
		// The raw declared value is returned uncapped: ToolResultMaxTokens
		// lives in internal/agent and the tools package MUST NOT import
		// agent (import cycle), so the caller caps, not this helper.
		assert.Equal(t, 1400, GetMaxResultTokens(resultSizerStub{floor: 1400}))
	})
}

// ---------------------------------------------------------------------------
// Per-tool cooperative timeout (contract B1, 04-tool-surface/01-tool-timeout).
// Activated from the parked gate file (interface_test.go.staged, 762dc9d3);
// merged into the existing interface_test.go home.
// ---------------------------------------------------------------------------

// defaultTimeoutTool embeds ToolDefaults and declares nothing: the zero case.
type defaultTimeoutTool struct {
	ToolDefaults
}

func (defaultTimeoutTool) Name() string                       { return "default_timeout_probe" }
func (defaultTimeoutTool) Description() string                { return "probe" }
func (defaultTimeoutTool) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (defaultTimeoutTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return map[string]any{"ok": true}, nil
}

// timeoutTool declares a 1s cooperative budget (T1b worked-example shape).
type timeoutTool struct {
	ToolDefaults
	sleep time.Duration
}

func (timeoutTool) Name() string                       { return "timeout_probe" }
func (timeoutTool) Description() string                { return "probe" }
func (timeoutTool) Parameters() llm.FunctionParameters { return llm.FunctionParameters{} }
func (t timeoutTool) TimeoutMs() time.Duration         { return time.Second }
func (t timeoutTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	select {
	case <-time.After(t.sleep):
		return map[string]any{"ok": true}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestToolDefaults_TimeoutMsZero(t *testing.T) {
	// ToolDefaults must yield 0: no declared budget, executor arms nothing.
	var tool Tool = defaultTimeoutTool{}
	if got := tool.TimeoutMs(); got != 0 {
		t.Errorf("ToolDefaults.TimeoutMs() = %v, want 0 (opt-in per tool)", got)
	}
}

func TestTimeoutMs_Declared(t *testing.T) {
	var tool Tool = timeoutTool{sleep: 0}
	if got := tool.TimeoutMs(); got != time.Second {
		t.Errorf("TimeoutMs() = %v, want 1s", got)
	}
}

func TestErrToolTimeout_Sentinel(t *testing.T) {
	// The sentinel must exist and be wrappable so callers use errors.Is.
	wrapped := errors.Join(errors.New("tool call timed out after 1000ms"), ErrToolTimeout)
	if !errors.Is(wrapped, ErrToolTimeout) {
		t.Error("errors.Is(wrapped, ErrToolTimeout) = false, want true")
	}
	if ErrToolTimeout == nil {
		t.Fatal("ErrToolTimeout sentinel is nil")
	}
}

func TestToolTimeout_ResultShape(t *testing.T) {
	// The structured result the executor substitutes on expiry (B1 machine
	// gear): Success=false, ErrCode="TOOL_TIMEOUT", message names the budget.
	res := ToolResult{
		Success: false,
		ErrCode: "TOOL_TIMEOUT",
		Error:   "tool call timed out after 1000ms",
		Err:     ErrToolTimeout,
	}
	if res.Success {
		t.Error("timeout result Success = true, want false")
	}
	if res.ErrCode != "TOOL_TIMEOUT" {
		t.Errorf("ErrCode = %q, want TOOL_TIMEOUT", res.ErrCode)
	}
	if !strings.Contains(res.Error, "timed out after 1s") && !strings.Contains(res.Error, "timed out after 1000ms") {
		t.Errorf("Error = %q, must name the declared budget", res.Error)
	}
	if strings.Contains(res.Error, context.DeadlineExceeded.Error()) {
		t.Error("raw context.DeadlineExceeded string leaked into the user-facing message")
	}
	if !errors.Is(res.Err, ErrToolTimeout) {
		t.Error("res.Err must satisfy errors.Is(_, ErrToolTimeout)")
	}
}
