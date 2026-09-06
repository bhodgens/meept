package agent

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGuards_NudgeOrdering_ToolPairingPinned pins the bughunt round-2
// HIGH-1 fix: a warn-level no-progress nudge must land AFTER the tool
// result of the call that triggered it — never between the assistant
// tool_calls message and its tool result. The old order
// assistant(tool_calls) → user(nudge) → tool(result) violates the
// provider tool-calling protocol (HTTP 400 on strict vendors:
// GLM/Qwen documented in models.go, OpenAI/Anthropic likewise).
//
// Scenario: two identical memory_read calls with NoProgressWarnAt=2.
// The second call's Track returns GuardWarn; the nudge is deferred and
// must appear after msg(tc-2 result), before the final assistant text.
func TestGuards_NudgeOrdering_ToolPairingPinned(t *testing.T) {
	g := DefaultGuardConfig()
	g.NoProgressWarnAt = 2

	responses := []*llm.Response{
		{Content: "s1", FinishReason: "tool_calls", Usage: llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{ID: "tc-1", Type: "function",
				Function: llm.ToolCallFunction{Name: "memory_read", Arguments: `{"query":"alpha"}`}}}},
		{Content: "s2", FinishReason: "tool_calls", Usage: llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{ID: "tc-2", Type: "function",
				Function: llm.ToolCallFunction{Name: "memory_read", Arguments: `{"query":"alpha"}`}}}},
		{Content: "final answer", Usage: llm.TokenUsage{TotalTokens: 5}},
	}
	chatter := newMockChatter(responses...)
	registry := NewPlaceholderToolRegistry()
	registry.Register(&mockGuardTool{name: "memory_read"})
	secChecker := security.NewPermissionChecker(security.Config{})

	loop := NewAgentLoop("test-session", "/tmp",
		WithLLMChatter(chatter),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithAgentConfig(AgentConfig{MaxIterations: 10, Guards: g}),
	)
	loop.executor = NewExecutor(registry, secChecker)
	loop.logger = slog.New(slog.DiscardHandler)

	_, err := loop.RunOnce(context.Background(), "find alpha", "conv-nudge-order")
	require.NoError(t, err)

	conv := loop.conversations.Get("conv-nudge-order")
	require.NotNil(t, conv)
	msgs := conv.GetMessages()

	// 1. Every assistant tool_calls message must be IMMEDIATELY followed
	// by its tool result — no nudge interleaved into the pair.
	for i, m := range msgs {
		if len(m.ToolCalls) == 0 {
			continue
		}
		require.True(t, i+1 < len(msgs), "assistant tool_calls at %d has no following message", i)
		next := msgs[i+1]
		require.Equal(t, llm.RoleTool, next.Role,
			"message after assistant tool_calls must be the tool result (got %s: %.40q) — a nudge interleaved into the tool pair",
			next.Role, next.Content)
		require.Equal(t, m.ToolCalls[0].ID, next.ToolCallID,
			"tool result does not match the preceding tool call %s", m.ToolCalls[0].ID)
	}

	// 2. The warn nudge IS delivered — after the tool result of the
	// triggering call, not inside the pair.
	nudgeIdx := -1
	for i, m := range msgs {
		if strings.Contains(m.Content, "no measurable progress") {
			nudgeIdx = i
			assert.Equal(t, llm.RoleUser, m.Role, "nudge must be a user-role message")
		}
	}
	require.NotEqual(t, -1, nudgeIdx, "expected the deferred nudge to be delivered after the tool result")

	// 3. The nudge must sit after tc-2's tool result, not between
	// assistant(tool_calls tc-2) and its result.
	for i, m := range msgs {
		if len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == "tc-2" {
			assert.Greater(t, nudgeIdx, i+1,
				"nudge must come after tc-2's tool result, not between the pair")
		}
	}
}
