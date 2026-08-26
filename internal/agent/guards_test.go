package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Unit tests: hash normalization
// ---------------------------------------------------------------------------

func TestHashToolCall_ReorderedKeysEqual(t *testing.T) {
	a := HashToolCall("web_search", `{"query": "rust books", "limit": 5}`)
	b := HashToolCall("web_search", `{"limit":5,"query":"rust books"}`)
	assert.Equal(t, a, b, "reordered/whitespace-differing JSON must hash equal")
}

func TestHashToolCall_DistinctArgsDiffer(t *testing.T) {
	a := HashToolCall("web_search", `{"query": "rust books"}`)
	b := HashToolCall("web_search", `{"query": "go books"}`)
	assert.NotEqual(t, a, b, "distinct arguments must not collide")

	c := HashToolCall("read_file", `{"query": "rust books"}`)
	assert.NotEqual(t, a, c, "different tool names must not collide even with same args")
}

func TestHashToolCall_InvalidJSONFallsBackToWhitespaceCollapse(t *testing.T) {
	a := HashToolCall("t", `{not json  at  all}`)
	b := HashToolCall("t", `{not json at all}`)
	assert.Equal(t, a, b, "non-JSON args should still compare equal modulo whitespace")
}

// ---------------------------------------------------------------------------
// Unit tests: no-progress ladder
// ---------------------------------------------------------------------------

func TestNoProgressLadder_WarnVetoLadder(t *testing.T) {
	l := NewNoProgressLadder()

	// Same logical call repeated: ok@1, ok@2, warn@3, warn@4, veto@5+
	args := []string{
		`{"q":"x","n":1}`,
		`{"n":1,"q":"x"}`, // reordered keys — same normalized call
		`{"q":"x","n":1}`,
		`{ "q": "x", "n": 1 }`,
		`{"n":1, "q":"x"}`,
		`{"q":"x","n":1}`,
		`{"q":"x","n":1}`,
	}
	want := []GuardTrackResult{"ok", "ok", "warn", "warn", GuardVeto, GuardVeto, GuardVeto}
	for i, a := range args {
		got := l.Track("search", a, 3, 5)
		assert.Equal(t, want[i], got, "call %d args=%s", i+1, a)
	}
}

func TestNoProgressLadder_ResetsOnDistinctCall(t *testing.T) {
	l := NewNoProgressLadder()
	assert.Equal(t, GuardOK, l.Track("search", `{"q":"a"}`, 3, 5))
	assert.Equal(t, GuardOK, l.Track("search", `{"q":"a"}`, 3, 5))
	assert.Equal(t, GuardWarn, l.Track("search", `{"q":"a"}`, 3, 5))
	// Distinct call breaks the streak.
	assert.Equal(t, GuardOK, l.Track("search", `{"q":"b"}`, 3, 5))
	assert.Equal(t, GuardOK, l.Track("search", `{"q":"b"}`, 3, 5))
	assert.Equal(t, GuardWarn, l.Track("search", `{"q":"b"}`, 3, 5))
}

func TestNoProgressLadder_ConsecutiveVetoes(t *testing.T) {
	l := NewNoProgressLadder()
	for i := 0; i < 5; i++ {
		l.Track("search", `{"q":"a"}`, 3, 5)
	}
	// Streak: ok,ok,warn,warn,veto -> exactly one veto so far.
	assert.Equal(t, 1, l.ConsecutiveVetoes())
	assert.Equal(t, GuardVeto, l.Track("search", `{"q":"a"}`, 3, 5))
	assert.Equal(t, GuardVeto, l.Track("search", `{"q":"a"}`, 3, 5))
	assert.Equal(t, 3, l.ConsecutiveVetoes())
	// Distinct call resets vetoes.
	l.Track("other", `{}`, 3, 5)
	assert.Equal(t, 0, l.ConsecutiveVetoes())
}

// ---------------------------------------------------------------------------
// Unit tests: search rollback ring
// ---------------------------------------------------------------------------

func TestSearchRollback_RingWindow(t *testing.T) {
	r := NewSearchRollback(2)

	assert.False(t, r.ShouldRollback("h1"), "empty ring never rolls back")

	r.Observe("h1")
	assert.True(t, r.ShouldRollback("h1"), "observed hash within window triggers rollback on repeat")

	r.Observe("h2")
	assert.True(t, r.ShouldRollback("h1"), "h1 within window")
	assert.True(t, r.ShouldRollback("h2"))

	r.Observe("h3") // evicts h1 (window=2)
	assert.False(t, r.ShouldRollback("h1"), "h1 evicted from ring")
	assert.True(t, r.ShouldRollback("h2"))
	assert.True(t, r.ShouldRollback("h3"))
}

func TestSearchRollback_DefaultWindowNormalized(t *testing.T) {
	r := NewSearchRollback(0)
	require.Equal(t, 10, r.window, "zero window must normalize to default 10")
}

// ---------------------------------------------------------------------------
// Unit tests: reasoning watchdog streak counting
// ---------------------------------------------------------------------------

func TestReasoningWatchdog_StreakAndBreach(t *testing.T) {
	w := NewReasoningWatchdog()

	w.RecordTurn(false, false, 1000) // reasoning-only
	w.RecordTurn(false, false, 1000)
	assert.False(t, w.Breach(16384, 3), "streak 2 < cap 3, tokens below cap")

	w.RecordTurn(true, true, 0) // tool call resets streak
	assert.Equal(t, 0, w.Streak())

	w.RecordTurn(false, false, 15000)
	assert.False(t, w.Breach(16384, 3))
	w.RecordTurn(false, false, 2000)
	assert.True(t, w.Breach(16384, 3), "streak reached 3")
}

func TestReasoningWatchdog_TokenCapBreach(t *testing.T) {
	w := NewReasoningWatchdog()
	w.RecordTurn(false, false, 17000)
	assert.True(t, w.Breach(16384, 3), "token cap breach even at streak 1")
}

func TestReasoningWatchdog_TextTurnResets(t *testing.T) {
	w := NewReasoningWatchdog()
	w.RecordTurn(false, false, 100)
	w.RecordTurn(false, true, 100) // produced text
	assert.Equal(t, 0, w.Streak())
}

func TestGuardConfig_Normalize(t *testing.T) {
	g := GuardConfig{}.Normalized()
	assert.Equal(t, 3, g.NoProgressWarnAt)
	assert.Equal(t, 5, g.NoProgressVetoAt)
	assert.Equal(t, 3, g.GracefulAfterVetoes)
	assert.True(t, g.DuplicateSearchRollback, "default must be ship-on")
	assert.Equal(t, 10, g.RollbackWindow)
	assert.Equal(t, 16384, g.ReasoningTokenCap)
	assert.Equal(t, 3, g.ReasoningStreakTurns)
}

// ---------------------------------------------------------------------------
// Integration tests (fake-LLM harness patterns from loop_test.go)
// ---------------------------------------------------------------------------

// mockGuardTool is a trivially-successful tool for guard integration tests.
type mockGuardTool struct {
	name string
}

func (m *mockGuardTool) Name() string        { return m.name }
func (m *mockGuardTool) Description() string { return "guard test tool" }
func (m *mockGuardTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object", Properties: map[string]llm.ParameterProperty{}}
}
func (m *mockGuardTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return &tools.ToolResult{Success: true, Result: "ok"}, nil
}
func (m *mockGuardTool) IsReadOnly(map[string]any) bool        { return true }
func (m *mockGuardTool) IsConcurrencySafe(map[string]any) bool { return true }

func TestGuards_NoProgressLadder_Integration(t *testing.T) {
	// 7 near-same calls (alternating key order so the byte-level cycle
	// detector stays quiet while the normalized ladder climbs):
	// warn@3 (nudge), vetoes @5,6,7 -> graceful end after 3rd veto.
	responses := make([]*llm.Response, 0, 8)
	variants := []string{`{"q":"rust","n":5}`, `{"n":5,"q":"rust"}`}
	callID := 0
	newSearchResp := func() *llm.Response {
		callID++
		return &llm.Response{
			Content:      "searching",
			FinishReason: "tool_calls",
			Usage:        llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{
				ID:   fmt.Sprintf("tc-%d", callID),
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "probe_search",
					Arguments: variants[callID%len(variants)],
				},
			}},
		}
	}
	for i := 0; i < 7; i++ {
		responses = append(responses, newSearchResp())
	}
	responses = append(responses, &llm.Response{Content: "should never be reached", Usage: llm.TokenUsage{TotalTokens: 1}})

	chatter := newMockChatter(responses...)
	registry := NewPlaceholderToolRegistry()
	registry.Register(&mockGuardTool{name: "probe_search"})
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

	response, err := loop.RunOnce(context.Background(), "find rust books", "conv-guards-ladder")
	require.NoError(t, err, "graceful termination should not produce an error")

	assert.Contains(t, response, "without measurable progress", "expected graceful wrap-up message")

	// Iterations 1..7; terminated on the 3rd consecutive veto at iteration 7.
	assert.Equal(t, 7, chatter.callCount, "expected graceful end after 3rd veto (iteration 7)")

	// Nudge must have been injected into the conversation via the
	// system-injection (user-message anchor) path.
	conv := loop.conversations.Get("conv-guards-ladder")
	require.NotNil(t, conv)
	foundNudge := false
	for _, m := range conv.GetMessages() {
		if strings.Contains(m.Content, "no measurable progress") {
			foundNudge = true
		}
	}
	assert.True(t, foundNudge, "expected 'no measurable progress' nudge in conversation")
}

func TestGuards_DuplicateSearchRollback_Integration(t *testing.T) {
	searchCall := func(id int) *llm.Response {
		return &llm.Response{
			Content:      "searching",
			FinishReason: "tool_calls",
			Usage:        llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{
				ID:   fmt.Sprintf("tc-%d", id),
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "web_search",
					Arguments: `{"query":"identical query","limit":5}`,
				},
			}},
		}
	}
	chatter := newMockChatter(
		searchCall(1),
		searchCall(2), // exact duplicate within window -> rollback, same iteration re-sampled
		&llm.Response{Content: "done researching", Usage: llm.TokenUsage{TotalTokens: 5}},
	)

	registry := NewPlaceholderToolRegistry()
	registry.Register(&mockGuardTool{name: "web_search"})
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{
			MaxIterations: 2, // tight: rollback must NOT consume an iteration
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.executor = NewExecutor(registry, secChecker)

	response, err := loop.RunOnce(context.Background(), "research", "conv-guards-rollback")
	require.NoError(t, err)
	assert.Equal(t, "done researching", response)

	// 3 LLM samples across only 2 iterations proves the rollback re-sample
	// did not consume iteration budget (without rollback this would hit
	// max-iterations with the duplicate pair left in the transcript).
	assert.Equal(t, 3, chatter.callCount)

	// Both duplicate pairs were popped from the conversation: no web_search
	// tool results remain, and the conversation rewinds to the original user
	// message before re-sampling.
	conv := loop.conversations.Get("conv-guards-rollback")
	require.NotNil(t, conv)
	pairs := 0
	for _, m := range conv.GetMessages() {
		if m.Role == llm.RoleTool {
			pairs++
		}
	}
	assert.Equal(t, 0, pairs, "rolled-back pairs should leave no tool results in the transcript")
}

func TestGuards_DuplicateSearchRollback_DisabledFlag(t *testing.T) {
	searchCall := func(id int) *llm.Response {
		return &llm.Response{
			Content:      "searching",
			FinishReason: "tool_calls",
			Usage:        llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{
				ID:   fmt.Sprintf("tc-%d", id),
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "web_search",
					Arguments: `{"query":"identical query","limit":5}`,
				},
			}},
		}
	}
	chatter := newMockChatter(
		searchCall(1),
		searchCall(2), // duplicate executes normally (flag off)
		&llm.Response{Content: "done researching", Usage: llm.TokenUsage{TotalTokens: 5}},
	)

	registry := NewPlaceholderToolRegistry()
	registry.Register(&mockGuardTool{name: "web_search"})
	secChecker := security.NewPermissionChecker(security.Config{})
	guards := DefaultGuardConfig()
	guards.DuplicateSearchRollback = false
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{MaxIterations: 5, Guards: guards}),
	)
	loop.executor = NewExecutor(registry, secChecker)

	_, err := loop.RunOnce(context.Background(), "research", "conv-guards-rollback-off")
	require.NoError(t, err)

	conv := loop.conversations.Get("conv-guards-rollback-off")
	require.NotNil(t, conv)
	pairs := 0
	for _, m := range conv.GetMessages() {
		if m.Role == llm.RoleTool {
			pairs++
		}
	}
	assert.Equal(t, 2, pairs, "with rollback disabled both search executions remain")
}

func TestGuards_ReasoningWatchdog_Integration(t *testing.T) {
	reasoningOnly := func(id int) *llm.Response {
		return &llm.Response{
			Content:      "",
			Reasoning:    strings.Repeat("deliberating ", 400), // ~1000 approx tokens
			FinishReason: "stop",
			Usage:        llm.TokenUsage{TotalTokens: 50},
		}
	}
	chatter := newMockChatter(
		reasoningOnly(1),
		reasoningOnly(2),
		reasoningOnly(3), // streak hits 3 -> nudge
		reasoningOnly(4),
		reasoningOnly(5),
		reasoningOnly(6), // second breach -> graceful termination
		&llm.Response{Content: "unreachable"},
	)

	registry := NewPlaceholderToolRegistry()
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{
			MaxIterations: 10,
			Guards:        DefaultGuardConfig(),
		}),
	)

	response, err := loop.RunOnce(context.Background(), "think about it", "conv-guards-watchdog")
	require.NoError(t, err, "second breach terminates gracefully, not with an error")
	assert.Contains(t, response, "extended thinking", "expected watchdog graceful wrap-up message")
	// Streak reaches 3 at iteration 3 -> first breach nudge. Iteration 4 is
	// the second breach -> graceful termination.
	assert.Equal(t, 4, chatter.callCount, "terminate on second breach (iteration 4)")

	conv := loop.conversations.Get("conv-guards-watchdog")
	require.NotNil(t, conv)
	foundNudge := false
	for _, m := range conv.GetMessages() {
		if strings.Contains(m.Content, "provide your answer") {
			foundNudge = true
		}
	}
	assert.True(t, foundNudge, "expected watchdog forcing nudge in conversation")
}
