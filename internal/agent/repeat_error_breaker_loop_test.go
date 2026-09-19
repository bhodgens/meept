package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Task 2/3: breaker wired into the loop tool-call failure seam
// ---------------------------------------------------------------------------

// failingCountTool always errors with the SAME message and counts executions.
type failingCountTool struct {
	name      string
	errMsg    string
	execCalls int
}

func (f *failingCountTool) Name() string        { return f.name }
func (f *failingCountTool) Description() string { return "always-failing breaker probe tool" }
func (f *failingCountTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (f *failingCountTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	f.execCalls++
	// Fail via the Go-error path: Executor.Execute marks the
	// ExecutionResult failed ("tool execution failed: ...") only when the
	// tool call itself errors — a ToolResult{Success:false} is treated as
	// a valid result envelope.
	return nil, errors.New(f.errMsg)
}
func (f *failingCountTool) IsReadOnly(map[string]any) bool        { return true }
func (f *failingCountTool) IsConcurrencySafe(map[string]any) bool { return true }

// newBreakerLoopHarness builds a loop whose chatter emits `n` identical
// tool-call responses (same tool, same args) followed by filler responses.
func newBreakerLoopHarness(t *testing.T, probe *failingCountTool, n int) (*AgentLoop, *mockChatter) {
	t.Helper()
	responses := make([]*llm.Response, 0, n+2)
	for i := 0; i < n; i++ {
		responses = append(responses, &llm.Response{
			Content:      "trying",
			FinishReason: "tool_calls",
			Usage:        llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{
				ID:   fmt.Sprintf("tc-%d", i+1),
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      probe.name,
					Arguments: `{"name":"doomed"}`,
				},
			}},
		})
	}
	responses = append(responses, &llm.Response{Content: "unreachable filler", Usage: llm.TokenUsage{TotalTokens: 1}})

	chatter := newMockChatter(responses...)
	registry := NewPlaceholderToolRegistry()
	registry.Register(probe)
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{
			MaxIterations: 20,
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.executor = NewExecutor(registry, secChecker)
	return loop, chatter
}

// TestLoop_IdenticalToolErrorTerminalizesStep pins the core contract: the 4th
// identical failing call is refused WITHOUT executing (the tool's own counter
// pins exactly 3 executions) and the turn ends with the honest terminal
// message instead of another model round.
func TestLoop_IdenticalToolErrorTerminalizesStep(t *testing.T) {
	probe := &failingCountTool{name: "task_create", errMsg: "name is required"}
	loop, chatter := newBreakerLoopHarness(t, probe, 4)

	response, err := loop.RunOnce(context.Background(), "create the doomed task", "conv-breaker-terminal")
	require.Error(t, err, "the exhausted breaker must terminalize the turn with an error")
	assert.ErrorIs(t, err, ErrToolRepeatExhausted, "the sentinel must be classifiable")

	assert.Equal(t, 3, probe.execCalls,
		"the 4th identical call must NEVER reach the tool (counter pin)")
	assert.Contains(t, response,
		"tool task_create rejected the identical input 3 times",
		"the honest terminal message must reach the user-visible reply, not just logs")
	assert.Contains(t, response, "name is required",
		"the first error line must be embedded in the terminal message")
	assert.Contains(t, response, "; giving up")
	assert.NotContains(t, response, "unreachable filler")

	// The chatter got exactly 4 calls (4 iterations); the 4th iteration's
	// tool refusal terminated the loop rather than inviting a 5th model call.
	assert.Equal(t, 4, chatter.callCount)
}

// TestLoop_BreakerSurvivesGuardResetWithinScope pins the load-bearing design
// fact: resetTurnGuards (the per-turn / replan-generation reset) must NOT
// clear the breaker — its counts continue across the reset, which is exactly
// what makes the breaker hold across abort→escalation→replan rounds that all
// re-enter the same loop instance.
func TestLoop_BreakerSurvivesGuardResetWithinScope(t *testing.T) {
	probe := &failingCountTool{name: "task_create", errMsg: "name is required"}
	loop, _ := newBreakerLoopHarness(t, probe, 1)

	hash := repeatErrorArgsHash(map[string]any{"name": "doomed"})
	exhausted, _ := loop.repeatErr.Observe("task_create", hash, "name is required")
	require.False(t, exhausted)

	// Simulate the replan-generation boundary: every within-turn guard is
	// wiped here (cycle detector, ladder, watchdog...), the breaker must not be.
	loop.resetTurnGuards()

	// Two more failures exhaust the key ACROSS the reset (counts continued).
	exhausted, _ = loop.repeatErr.Observe("task_create", hash, "name is required")
	assert.False(t, exhausted, "second failure post-reset")
	exhausted, summary := loop.repeatErr.Observe("task_create", hash, "name is required")
	require.True(t, exhausted, "the 3rd failure post-reset must exhaust — counts survived the guard reset")
	assert.Contains(t, summary, "rejected the identical input 3 times")
	assert.False(t, loop.repeatErr.Allow("task_create", hash))
}

// TestLoop_DifferentArgsGetFreshBudget: a model that varies its arguments (or
// whose tool starts failing differently) is never blocked by a dead key —
// only the identical triple is.
func TestLoop_DifferentArgsGetFreshBudget(t *testing.T) {
	probe := &failingCountTool{name: "task_create", errMsg: "name is required"}
	loop, _ := newBreakerLoopHarness(t, probe, 1)

	hashDoomed := repeatErrorArgsHash(map[string]any{"name": "doomed"})
	for i := 0; i < maxIdenticalToolErrors; i++ {
		loop.repeatErr.Observe("task_create", hashDoomed, "name is required")
	}
	require.False(t, loop.repeatErr.Allow("task_create", hashDoomed))

	hashOther := repeatErrorArgsHash(map[string]any{"name": "other"})
	assert.True(t, loop.repeatErr.Allow("task_create", hashOther),
		"different args must get a fresh budget")
	assert.True(t, loop.repeatErr.Allow("web_search", hashDoomed),
		"a different tool must get a fresh budget")
}

// TestBreaker_ScopeSurvivesReplanCycle is the Task 3 placement proof: the
// escalation→replan path re-enters the SAME AgentLoop instance, so the
// breaker's memory holds across the replan attempt; a genuinely new scope
// (new loop instance) starts clean.
//
// LIFETIME CHAIN VERIFIED (file:line):
//   - Planner replan: orchestrator.go handlePlanRequest (:325) →
//     strategic.ReplanFailedTask (strategic.go:1369) → Plan (:402) →
//     planSinglePhase (:1008) → sp.registry.Get(config.AgentIDPlanner)
//     (strategic.go:1010) → registry.Get → GetForTask(planner, "_default")
//     (registry.go:297→308) — the SAME loop pointer every time.
//   - Step-job replan: daemon/components.go:8018
//     p.registry.GetForTask(job.AgentID, job.TaskID) — the task-scoped loop
//     is reused for every job/attempt of one task until
//     orchestrator.releaseTaskLoopsIfComplete (orchestrator.go:583) releases
//     it, which only happens when EVERY step is terminal.
//   - The cycle-abort inside a turn returns (msg, cycleAbortError(...)) near
//     loop.go:4850 — the loop object itself survives; the escalation replan
//     then runs its next attempt on this same instance.
//   - NewAgentLoop constructs the breaker once per instance (loop.go, next
//     to loop.toolBreaker, :2249 region); resetTurnGuards (:3885) — called
//     from :2503, :3187, :6358 — deliberately does not touch it.
//
// Deviation from the plan's alternative: the plan suggested the breaker might
// need to move to a shared executor/step-job scope if replan created a NEW
// loop per attempt. It does not — loops are cached per (agentID, taskID) in
// AgentRegistry.loops and reused, so the per-loop placement is correct.
func TestBreaker_ScopeSurvivesReplanCycle(t *testing.T) {
	// (1) Plan attempt: the failing tool burns its budget on the loop.
	probe := &failingCountTool{name: "task_create", errMsg: "name is required"}
	loop, chatter := newBreakerLoopHarness(t, probe, 4)

	response, err := loop.RunOnce(context.Background(), "plan work", "conv-replan-cycle")
	require.Error(t, err)
	assert.Equal(t, 3, probe.execCalls, "attempt 1 executed the call exactly 3 times")
	assert.Contains(t, response, "rejected the identical input 3 times")

	// (2) The replan cycle re-enters the SAME logical work scope: the same
	// loop instance, a fresh conversation (fresh turn generation), the same
	// doomed call. Simulated by a new conversation id on the same loop —
	// exactly what planSinglePhase does with
	// conversationID := fmt.Sprintf("plan-%s-%s", taskID, id.Generate("")).
	// resetTurnGuards runs at every turn start; the breaker must hold.
	// Drop the harness's filler response so the replan tool call is the
	// very next model response.
	chatter.responses = chatter.responses[:4]
	chatter.responses = append(chatter.responses,
		&llm.Response{
			Content:      "trying again after replan",
			FinishReason: "tool_calls",
			Usage:        llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{
				ID:   "tc-replan-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      probe.name,
					Arguments: `{"name":"doomed"}`,
				},
			}},
		},
	)

	response, err = loop.RunOnce(context.Background(), "replan attempt", "conv-replan-cycle-2")
	require.Error(t, err, "the replan attempt must be refused for the same doomed call")
	assert.ErrorIs(t, err, ErrToolRepeatExhausted)
	assert.Equal(t, 3, probe.execCalls,
		"the replan attempt's call must be REFUSED WITHOUT executing — breaker held across the cycle")
	assert.Contains(t, response, "tool task_create rejected the identical input 3 times")

	// (3) A genuinely NEW scope (new loop instance — a new task/session)
	// starts clean: the identical call executes again (2 iterations: one
	// execution, one follow-up).
	probe2 := &failingCountTool{name: "task_create", errMsg: "name is required"}
	loop2, _ := newBreakerLoopHarness(t, probe2, 1)
	_, _ = loop2.RunOnce(context.Background(), "fresh task", "conv-fresh-scope")
	assert.Equal(t, 1, probe2.execCalls,
		"a new loop instance must start with a clean breaker (fresh scope)")
}

// TestLoop_BreakerRefusalIsHonestToolResult: the refused 4th call still gets
// a tool result in the conversation (no dangling tool_call_id, which would
// HTTP-400 strict providers) carrying the honest refusal text, mirroring
// flushDeferredToolResults' HIGH-2 contract.
func TestLoop_BreakerRefusalIsHonestToolResult(t *testing.T) {
	probe := &failingCountTool{name: "task_create", errMsg: "name is required"}
	loop, _ := newBreakerLoopHarness(t, probe, 4)

	_, err := loop.RunOnce(context.Background(), "doomed", "conv-breaker-refusal")
	require.Error(t, err)

	conv := loop.conversations.Get("conv-breaker-refusal")
	require.NotNil(t, conv)
	found := false
	for _, m := range conv.GetMessages() {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "refused without execution") {
			found = true
		}
	}
	assert.True(t, found, "the refusal must land in the conversation as a tool result; messages: %+v", conv.GetMessages())
}
