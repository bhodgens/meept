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
		reasoningOnly(4), // second breach -> disable-thinking RESCUE turn
		&llm.Response{Content: "recovered after rescue"}, // rescue succeeds
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
	require.NoError(t, err, "rescue succeeds, no error")
	assert.Contains(t, response, "recovered after rescue", "rescue turn's visible content becomes the reply")
	// Streak reaches 3 at iteration 3 -> first breach nudge. Iteration 4 is
	// the second breach -> disable-thinking rescue (iteration 5) -> visible
	// content ends the loop.
	assert.Equal(t, 5, chatter.callCount, "rescue turn runs after second breach")

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

// TestGuards_ReasoningWatchdog_RescueFails_Terminates pins the bound: when
// the disable-thinking rescue ALSO produces a reasoning-only reply, the
// watchdog terminates gracefully instead of rescuing forever.
func TestGuards_ReasoningWatchdog_RescueFails_Terminates(t *testing.T) {
	reasoningOnly := func(id int) *llm.Response {
		return &llm.Response{
			Content:      "",
			Reasoning:    strings.Repeat("deliberating ", 400),
			FinishReason: "stop",
			Usage:        llm.TokenUsage{TotalTokens: 50},
		}
	}
	chatter := newMockChatter(
		reasoningOnly(1),
		reasoningOnly(2),
		reasoningOnly(3), // first breach -> nudge
		reasoningOnly(4), // second breach -> rescue turn
		reasoningOnly(5), // rescue also reasoning-only -> terminate
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

	response, err := loop.RunOnce(context.Background(), "think about it", "conv-guards-rescue-fail")
	require.NoError(t, err, "rescue failure terminates gracefully, not with an error")
	assert.Contains(t, response, "extended thinking", "expected watchdog graceful wrap-up after failed rescue")
	assert.Equal(t, 5, chatter.callCount, "no calls after the failed rescue (call 5 was the rescue)")
}

// ---------------------------------------------------------------------------
// D-H1 (bughunt 2026-09-04): rollback spin cap
// ---------------------------------------------------------------------------

// TestGuards_DuplicateSearchRollback_SpinCap: a model that keeps re-emitting
// the IDENTICAL web_search used to spin the loop forever (each rollback
// decremented the iteration counter). After maxDuplicateSearchRollbacks
// rollbacks in one turn, the duplicate must fall through to normal execution
// and the turn must terminate via the max-iterations path.
func TestGuards_DuplicateSearchRollback_SpinCap(t *testing.T) {
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
		searchCall(1), // iteration 1: fresh -> executes
		searchCall(2), // duplicate #1 -> rollback (same iteration re-sample)
		searchCall(3), // duplicate #2 -> rollback
		searchCall(4), // duplicate #3 -> rollback (cap now reached)
		searchCall(5), // duplicate #4 -> NO rollback, executes normally
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
			MaxIterations: 3, // it1: fresh exec; it2: 3 rollbacks + post-cap exec; it3: final text
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.executor = NewExecutor(registry, secChecker)

	response, err := loop.RunOnce(context.Background(), "research", "conv-guards-spin-cap")
	// Without the cap this never terminates: the loop spins on the same
	// iteration forever. With the cap the 4th duplicate executes and the
	// turn completes deterministically.
	require.NoError(t, err, "capped duplicate loop must terminate, not spin")
	assert.Equal(t, "done researching", response)

	// Exactly 6 LLM samples: 1 fresh execution + 3 rollbacks + 1 post-cap
	// execution + final text. A spin would keep sampling past the script.
	assert.Equal(t, 6, chatter.callCount)

	// Exactly 1 executed-search pair remains: the first rollback pops the
	// FRESH pair, and only the post-cap execution's pair stays in the
	// transcript. This pins that rollbacks fired exactly
	// maxDuplicateSearchRollbacks times (3 pops happened along the way).
	conv := loop.conversations.Get("conv-guards-spin-cap")
	require.NotNil(t, conv)
	pairs := 0
	for _, m := range conv.GetMessages() {
		if m.Role == llm.RoleTool {
			pairs++
		}
	}
	assert.Equal(t, 1, pairs, "fresh pair consumed by first rollback; only post-cap pair remains")
}

// ---------------------------------------------------------------------------
// D-H3 (bughunt 2026-09-04): guard reset between turns
// ---------------------------------------------------------------------------

// TestSearchRollback_Reset: Reset clears the ring; the ring accepts new
// observations afterwards.
func TestSearchRollback_Reset(t *testing.T) {
	r := NewSearchRollback(2)
	r.Observe("h1")
	require.True(t, r.ShouldRollback("h1"))

	r.Reset()
	assert.False(t, r.ShouldRollback("h1"), "reset must clear the ring")
	assert.Equal(t, 0, len(r.ring), "ring slice must be empty after reset")

	r.Observe("h1")
	assert.True(t, r.ShouldRollback("h1"), "ring must accept observations after reset")
}

// TestResetTurnGuards_ClearsGuardState: warming every guard then resetting
// leaves the loop with clean per-turn state.
func TestResetTurnGuards_ClearsGuardState(t *testing.T) {
	loop := NewAgentLoop("test-session", "/tmp")

	// Warm every guard.
	loop.noProgress.Track("web_search", `{"q":"x"}`, 3, 5)
	loop.searchRollbk.Observe("h1")
	loop.reasonWatch.RecordTurn(false, false, 500)
	loop.mu.Lock()
	loop.reasonWatchStreakBreach = true
	loop.mu.Unlock()

	loop.resetTurnGuards()

	// After reset the same call must restart the ladder streak from 1 (OK)
	// instead of continuing an old streak toward warn/veto.
	assert.Equal(t, GuardOK, loop.noProgress.Track("web_search", `{"q":"x"}`, 3, 5),
		"ladder streak must restart after reset")
	assert.False(t, loop.searchRollbk.ShouldRollback("h1"), "rollback ring must be empty after reset")
	assert.Equal(t, 0, loop.reasonWatch.Streak(), "watchdog streak must be zero after reset")
	loop.mu.Lock()
	breach := loop.reasonWatchStreakBreach
	loop.mu.Unlock()
	assert.False(t, breach, "streak-breach flag must clear")
}

// TestGuards_ResetBetweenTurns: guard state warm from turn 1 (rollback ring
// holds the search hash, ladder streak in progress) must NOT carry into
// turn 2 — the same search executes normally instead of rolling back, and
// the ladder does not escalate to a warn nudge.
func TestGuards_ResetBetweenTurns(t *testing.T) {
	// Alternate JSON key order between calls: the normalized no-progress
	// ladder still tracks them as the SAME call, but the byte-level cycle
	// detector (hashArgs) sees distinct args — so a cross-turn 3-in-a-row
	// cycle abort cannot mask the ladder behavior this test pins.
	const searchArgsA = `{"query":"identical query","limit":5}`
	const searchArgsB = `{"limit":5,"query":"identical query"}`
	searchCall := func(id int) *llm.Response {
		args := searchArgsA
		if id%2 == 0 {
			args = searchArgsB
		}
		return &llm.Response{
			Content:      "searching",
			FinishReason: "tool_calls",
			Usage:        llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{
				ID:   fmt.Sprintf("tc-%d", id),
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "web_search",
					Arguments: args,
				},
			}},
		}
	}
	chatter := newMockChatter(
		searchCall(1), // turn 1, iter 1: fresh -> executes (ladder streak 1)
		searchCall(2), // turn 1: duplicate #1 -> rollback
		searchCall(3), // turn 1: duplicate #2 -> rollback
		searchCall(4), // turn 1: duplicate #3 -> rollback (cap now reached)
		searchCall(5), // turn 1: duplicate #4 -> cap -> executes (ladder streak 2)
		&llm.Response{Content: "done turn one", Usage: llm.TokenUsage{TotalTokens: 5}},
		searchCall(6), // turn 2: identical search — must execute cleanly
		&llm.Response{Content: "done turn two", Usage: llm.TokenUsage{TotalTokens: 5}},
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
			MaxIterations: 8,
			Guards:        DefaultGuardConfig(),
		}),
	)
	loop.executor = NewExecutor(registry, secChecker)

	// --- Turn 1 ---
	_, err := loop.RunOnce(context.Background(), "research", "conv-guards-reset")
	require.NoError(t, err)
	require.Equal(t, 6, chatter.callCount, "turn 1: 5 searches + 1 final text")

	// Precondition: turn 1 left the guards hot.
	argsHash := HashToolCall("web_search", searchArgsA)
	require.True(t, loop.searchRollbk.ShouldRollback(argsHash),
		"precondition: rollback ring must be warm after turn 1")
	require.Equal(t, 2, loop.noProgress.streak,
		"precondition: ladder streak must be in progress after turn 1")

	// --- Turn 2 ---
	response, err := loop.RunOnce(context.Background(), "research again", "conv-guards-reset")
	require.NoError(t, err)
	require.Equal(t, "done turn two", response)
	require.Equal(t, 8, chatter.callCount, "turn 2 must consume exactly its 2 scripted samples")

	// Without the reset the turn-2 duplicate would roll back (ring warm) and
	// the ladder streak would hit warn@3 -> nudge. With the reset it executes
	// cleanly: turn 1 leaves its post-cap pair, turn 2 adds its own — 2 pairs
	// total (turn 1's fresh pair was consumed by its first rollback)...
	conv := loop.conversations.Get("conv-guards-reset")
	require.NotNil(t, conv)
	pairs := 0
	for _, m := range conv.GetMessages() {
		if m.Role == llm.RoleTool {
			pairs++
		}
	}
	assert.Equal(t, 2, pairs, "turn 2's duplicate must execute (ring reset), not roll back")

	// ...and no no-progress nudge from a persisted streak.
	for _, m := range conv.GetMessages() {
		assert.NotContains(t, m.Content, "no measurable progress",
			"ladder reset must prevent cross-turn warn escalation")
	}
}
