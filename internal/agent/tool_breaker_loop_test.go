package agent

import (
	"context"
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
// harness-eval F14/F15 pin: a failing tool call trips the ToolRetryBreaker
// through the REAL loop path (RunOnce -> executeToolCalls -> observeToolBreaker).
//
// The e2e pin lives at e2e/suites/breakers TestBreakers04PersistentToolFailure
// HaltsBounded; this unit pin keeps the same contract cheap and local: with the
// loop-level repeat-error budget raised above the tool breaker's 5-strike veto
// and the no-progress ladder thresholds raised so the nudge/veto ladder stays
// out of the way (its abort is gated on nudgeInFlight), the 5th identical
// failing call is annotated with the tool-retry-breaker veto note by
// observeToolBreaker and the breaker logs the veto.
// ---------------------------------------------------------------------------

func TestLoop_ToolRetryBreakerVetoAnnotatesResult(t *testing.T) {
	probe := &failingCountTool{name: "file_write", errMsg: "not a directory"}

	responses := make([]*llm.Response, 0, 7)
	for i := range 6 {
		responses = append(responses, &llm.Response{
			Content:      "trying",
			FinishReason: "tool_calls",
			Usage:        llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{
				ID:   fmt.Sprintf("tc-%d", i+1),
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      probe.name,
					Arguments: `{"path":"doomed"}`,
				},
			}},
		})
	}
	responses = append(responses, &llm.Response{Content: "gave up", Usage: llm.TokenUsage{TotalTokens: 1}})

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
			Guards: GuardConfig{
				// Keep the no-progress ladder quiet: its abort is gated on
				// nudgeInFlight, so raised thresholds keep it out of the way
				// while the byte-level cycle detector cannot abort either
				// (abort requires nudgeInFlight || noProgress == nil).
				NoProgressWarnAt: 99,
				NoProgressVetoAt: 99,
				// Raise the loop-level repeat-ERROR breaker budget above the
				// tool breaker's veto-at-5 (same reason as e2e breakers-04).
				RepeatErrorLimit: 10,
			},
		}),
	)
	loop.executor = NewExecutor(registry, secChecker)

	const convID = "conv-tool-breaker-veto"
	// 6 identical failing calls; the 5th and 6th carry the veto annotation.
	_, err := loop.RunOnce(context.Background(), "doom the path", convID)
	require.NoError(t, err)

	// The breaker observed every outcome: 6 consecutive failures recorded
	// for the identical key.
	warnAt, vetoAt := loop.toolBreaker.thresholds()
	assert.Equal(t, 3, warnAt)
	assert.Equal(t, 5, vetoAt)
	counts := loop.toolBreaker.snapshotCounts()
	require.Len(t, counts, 1, "one distinct failing key")
	for _, c := range counts {
		assert.Equal(t, 6, c, "all 6 outcomes must be observed")
	}

	// The veto note reached the conversation: the 5th/6th tool results carry
	// the annotation appended by observeToolBreaker.
	conv := loop.conversations.Get(convID)
	require.NotNil(t, conv)
	vetoed := 0
	for _, m := range conv.GetMessages() {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "tool-retry breaker") {
			vetoed++
		}
	}
	assert.GreaterOrEqual(t, vetoed, 2,
		"the 5th and 6th identical failures must carry the breaker veto annotation")

	// The tool itself executed 6 times (veto annotates; it does not block).
	assert.Equal(t, 6, probe.execCalls)
}
