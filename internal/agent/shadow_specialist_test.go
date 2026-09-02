package agent

// Leaf 06-shadow-capture: prove that specialist loops created through
// AgentRegistry receive the shadow manager (RegistryConfig.ShadowManager ->
// AgentRegistry.shadowMgr -> createLoop's WithShadowManager option) and that
// a tool-call turn performed by such a loop is captured via
// CaptureToolInteraction, landing a task_type=tool_use record in the shadow
// training store.
//
// docs/implementation-gaps.md item 2 claimed registry loops never received
// the shadow manager; these tests pin the wiring so the gap cannot silently
// reopen. RED was demonstrated by temporarily disabling the
// WithShadowManager option in createLoop (both assertions failed), then
// restored — the tests discriminate the wiring, not the compiler.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/shadow"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shadowProbeTool is a trivially-successful terminating tool so a single
// tool-call turn ends the reasoning cycle after exactly one LLM call. It is
// named "platform_status" — a baseline tool that maps to the RiskSafe
// platform_read security action (BuiltinRules) — so createLoop's filtered
// registry passes it and the executor's permission check allows it.
type shadowProbeTool struct{}

func (shadowProbeTool) Name() string        { return ToolPlatformStatus }
func (shadowProbeTool) Description() string { return "leaf 06 shadow capture test tool" }
func (shadowProbeTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (shadowProbeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return &tools.ToolResult{Success: true, Result: "final answer from shadow probe", Terminate: true}, nil
}
func (shadowProbeTool) IsReadOnly(map[string]any) bool        { return true }
func (shadowProbeTool) IsConcurrencySafe(map[string]any) bool { return true }

// shadowBrief exceeds the 500-char threshold shadow.estimateComplexity uses
// to classify a conversation as at least moderate complexity
// (DefaultConfig's MinComplexity), so ShouldShadow passes the gate.
var shadowBrief = "Investigate the failing test in the meept repository. " +
	"Run the scoped suite, capture the failure output, and report the " +
	"minimal reproduction steps. " +
	strings.Repeat("The harness must stay deterministic and race-clean. ", 14)

// newShadowTestConfig builds an enabled shadow config that is deterministic
// and makes no LLM calls: sync mode (capture runs inline), full sampling,
// heuristic scoring. The teacher model name only satisfies IsEnabled —
// tool captures never consult the teacher.
func newShadowTestConfig(t *testing.T) *shadow.Config {
	t.Helper()
	cfg := shadow.DefaultConfig()
	cfg.Enabled = true
	cfg.DataDir = t.TempDir()
	cfg.Teacher.Model = "shadow-test-teacher"
	cfg.Shadowing.Mode = shadow.ModeSync
	cfg.Shadowing.SampleRate = 1.0
	cfg.Quality.Method = shadow.MethodHeuristic
	return cfg
}

// newShadowSpecialistRegistry builds a registry around one executor spec
// ("shadow-spec-coder") with a probe tool and the given shadow manager —
// the same shape components.go uses when cfg.MultiAgent.Enabled.
func newShadowSpecialistRegistry(t *testing.T, mgr *shadow.Manager) *AgentRegistry {
	t.Helper()
	toolReg := NewPlaceholderToolRegistry()
	toolReg.Register(shadowProbeTool{})
	r := NewAgentRegistry(RegistryConfig{
		ToolRegistry:    toolReg,
		SecurityChecker: security.NewPermissionChecker(security.Config{}),
		MessageBus:      bus.New(nil, slogDiscardLogger()),
		ShadowManager:   mgr,
		Logger:          slogDiscardLogger(),
	})
	require.NoError(t, r.RegisterSpec(&AgentSpec{
		ID:      "shadow-spec-coder",
		Name:    "shadow-spec-coder",
		Role:    RoleExecutor,
		Enabled: true,
		// createLoop reads loop limits straight from spec constraints;
		// an unset MaxIterations yields a zero-iteration loop.
		Constraints: AgentConstraints{MaxIterations: 5},
	}))
	return r
}

// shadowToolCallResponse is the first (and only) LLM response: a tool call.
func shadowToolCallResponse() *llm.Response {
	return &llm.Response{
		Content:      "probing",
		FinishReason: "tool_calls",
		Usage:        llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		ToolCalls: []llm.ToolCall{{
			ID:   "tc-shadow-1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      ToolPlatformStatus,
				Arguments: `{"target":"meept"}`,
			},
		}},
	}
}

// TestSpecialistShadow_RegistryPlumbsShadowManagerIntoLoop pins the
// components.go -> RegistryConfig -> createLoop wiring: a loop created by
// the registry must hold exactly the manager given to the registry config.
func TestSpecialistShadow_RegistryPlumbsShadowManagerIntoLoop(t *testing.T) {
	t.Parallel()

	mgr, err := shadow.NewManager(shadow.ManagerConfig{
		Config: newShadowTestConfig(t),
		Logger: slogDiscardLogger(),
	})
	require.NoError(t, err)
	defer mgr.Close()

	r := newShadowSpecialistRegistry(t, mgr)
	loop, err := r.Get("shadow-spec-coder")
	require.NoError(t, err)
	require.NotNil(t, loop)
	assert.Same(t, mgr, loop.shadowMgr,
		"createLoop must pass the registry's shadow manager into specialist loops")

	// Control: a registry built without a shadow manager must produce
	// loops with none (no accidental default injection).
	rNone := newShadowSpecialistRegistry(t, nil)
	loopNone, err := rNone.Get("shadow-spec-coder")
	require.NoError(t, err)
	assert.Nil(t, loopNone.shadowMgr)
}

// TestSpecialistShadow_ToolTurnCapturedViaRegistryLoop runs a tool-call turn
// through a registry-created specialist loop and asserts the turn lands in
// the shadow training store as a task_type=tool_use record.
func TestSpecialistShadow_ToolTurnCapturedViaRegistryLoop(t *testing.T) {
	t.Parallel()

	mgr, err := shadow.NewManager(shadow.ManagerConfig{
		Config: newShadowTestConfig(t),
		Logger: slogDiscardLogger(),
	})
	require.NoError(t, err)
	defer mgr.Close()

	r := newShadowSpecialistRegistry(t, mgr)
	chatter := newMockChatter(shadowToolCallResponse())
	loop, err := r.Get("shadow-spec-coder")
	require.NoError(t, err)
	// RegistryConfig only accepts a concrete *llm.Client, so tests inject a
	// fake Chatter onto the loop directly (same-package pattern used by the
	// guards tests for loop.executor).
	loop.llm = chatter

	resp, err := loop.RunOnce(context.Background(), shadowBrief, "conv-shadow-specialist")
	require.NoError(t, err)
	assert.Contains(t, resp, "final answer from shadow probe")
	assert.Equal(t, 1, chatter.callCount,
		"terminating tool must end the turn after exactly one LLM call")

	// The loop captures tool turns on a fire-and-forget goroutine
	// (loop.go: l.wg.Add(1) + go func). loop.Stop() waits on that same
	// WaitGroup, so after it returns the capture is GUARANTEED complete —
	// no polling, no scheduling race. (The previous Eventually-based
	// version raced real async behavior and failed under full-sweep load
	// when the 3s window expired before the goroutine ran; forest F3
	// follow-up.)
	loop.Stop()

	stats, err := mgr.GetStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, stats.TotalRecords,
		"specialist loop tool turn must be captured as a shadow record")
	assert.Equal(t, 1, stats.RecordsByTaskType[string(shadow.TaskTypeToolUse)],
		"the captured record must be task_type=tool_use")
}

// TestSpecialistShadow_NilManagerTurnStillCompletes is the negative control:
// without a shadow manager the specialist loop must run the tool turn
// normally (capture site is nil-guarded, no panic, response intact).
func TestSpecialistShadow_NilManagerTurnStillCompletes(t *testing.T) {
	t.Parallel()

	r := newShadowSpecialistRegistry(t, nil)
	chatter := newMockChatter(shadowToolCallResponse())
	loop, err := r.Get("shadow-spec-coder")
	require.NoError(t, err)
	loop.llm = chatter

	resp, err := loop.RunOnce(context.Background(), shadowBrief, "conv-shadow-nil")
	require.NoError(t, err)
	assert.Contains(t, resp, "final answer from shadow probe")
	assert.Equal(t, 1, chatter.callCount)
}
