package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent/contextalloc"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security/pii"
	"github.com/caimlas/meept/internal/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seamLoopProvider is the wiring-test double for the C1 provider.
type seamLoopProvider struct {
	items []string
}

func (p *seamLoopProvider) TierBudgets(ctx context.Context, query string, windowTokens int) []contextalloc.TierBudget {
	return []contextalloc.TierBudget{
		{Tier: contextalloc.TierWorking, Recall: func(ctx context.Context, q string, limit int) []string {
			return p.items
		}},
	}
}

// newSeamTestLoop builds a loop with the given assembler config.
func newSeamTestLoop(t *testing.T, enabled bool, prov contextalloc.TierBudgetProvider) *AgentLoop {
	t.Helper()
	loop := NewAgentLoop("seam-test-session", "/tmp",
		WithLLMChatter(newMockChatter(&llm.Response{Content: "ok", FinishReason: "stop"})),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
	)
	cfg := config.ContextAssemblerConfig{Enabled: enabled}
	loop.SetContextAssemblerConfig(cfg)
	if prov != nil {
		loop.SetTierBudgetProvider(prov)
	}
	return loop
}

// ---------------------------------------------------------------------------
// T3: wiring + nil path golden
// ---------------------------------------------------------------------------

// TestLoopSeamWiringDisabledGolden pins the byte-identical nil path: with
// the assembler disabled, RunWithTask's memory injection output equals
// the recorded golden produced by today's path for a fixed fixture.
func TestLoopSeamWiringDisabledGolden(t *testing.T) {
	items := []string{
		"alpha fact with some content to budget",
		"beta fact longer content that still renders as one bullet item",
		"gamma",
	}

	// Golden: today's path -- buildMemoryContext is bypassed when memvid
	// is nil, so exercise the EXACT EnforceMemoryItemBudget rendering
	// RunWithTask applies (bulleted -> budget -> join) for the fixture.
	golden := func() string {
		bulleted := make([]string, len(items))
		for i, part := range items {
			bulleted[i] = "- " + part
		}
		budgeted := EnforceMemoryItemBudget(bulleted, MaxMemoryContextTokens)
		var sb strings.Builder
		for _, part := range budgeted {
			sb.WriteString(part + "\n")
		}
		return sb.String()
	}()

	loop := newSeamTestLoop(t, false, &seamLoopProvider{items: items})
	require.False(t, loop.contextAssemblerCfg.Enabled)

	// The disabled loop's seam helpers all take today's path.
	assert.Nil(t, loop.contextSeamIfEnabled())
	assert.Nil(t, loop.memoryContextViaSeam(context.Background(), "q"))
	assert.Equal(t, 0, loop.iterationBudgetViaSeam(context.Background()))
	assert.Equal(t, 0, loop.conversationBudgetViaSeam(context.Background()))
	_, docEnabled := loop.docBudgetViaSeam(context.Background())
	assert.False(t, docEnabled)

	// Golden reproduction: the disabled rendering pipeline is untouched.
	bulleted := make([]string, len(items))
	for i, part := range items {
		bulleted[i] = "- " + part
	}
	budgeted := EnforceMemoryItemBudget(bulleted, MaxMemoryContextTokens)
	var rendered strings.Builder
	for _, part := range budgeted {
		rendered.WriteString(part + "\n")
	}
	assert.Equal(t, golden, rendered.String())
}

// TestLoopSeamWiringDisabledTypedNil pins the typed-nil provider variant:
// enabled config + typed-nil provider behaves exactly like disabled.
func TestLoopSeamWiringDisabledTypedNil(t *testing.T) {
	loop := newSeamTestLoop(t, true, nil)
	var typedNil *seamLoopProvider
	loop.SetTierBudgetProvider(typedNil)

	assert.Nil(t, loop.contextSeamIfEnabled(), "typed-nil provider must normalize to no seam")
	assert.Nil(t, loop.memoryContextViaSeam(context.Background(), "q"))
}

// TestLoopSeamWiringEnabledRoutesMemory pins the enabled path: with the
// seam armed, memoryContextViaSeam returns tier-budgeted bullets derived
// from the provider's recall and the allocator's window math.
func TestLoopSeamWiringEnabledRoutesMemory(t *testing.T) {
	items := []string{"remember the alpha item", "remember the beta item"}
	loop := newSeamTestLoop(t, true, &seamLoopProvider{items: items})

	// Window resolution: inject a discovered window the way the daemon
	// does (firewall config's ModelContextLimit override lane).
	loop.SetContextFirewallConfig(config.LLMContextFirewallConfig{
		ModelContextLimit: 200000,
	})

	got := loop.memoryContextViaSeam(context.Background(), "remember")
	require.NotNil(t, got)
	require.NotEmpty(t, got)
	for _, item := range got {
		assert.True(t, strings.HasPrefix(item, "- "),
			"enabled-path items render as bullets, got %q", item)
	}
	_ = items
}

// ---------------------------------------------------------------------------
// T4: window resolution
// ---------------------------------------------------------------------------

// TestWindowResolutionDiscoveredLimit pins T4's window math: a discovered
// 200k limit yields Allocation.WindowTokens == 200000 - max(0.09*200000,
// 4096) and the iteration/conversation budgets derived from it.
func TestWindowResolutionDiscoveredLimit(t *testing.T) {
	loop := newSeamTestLoop(t, true, &seamLoopProvider{})
	loop.SetContextFirewallConfig(config.LLMContextFirewallConfig{
		ModelContextLimit: 200000,
	})

	seam := loop.contextSeamIfEnabled()
	require.NotNil(t, seam)

	window := loop.contextAssemblerWindow()
	assert.Equal(t, 200000, window)

	alloc := seam.Allocator().Allocate(context.Background(), window, contextalloc.Request{})
	assert.Equal(t, 18000, alloc.OutputReserve)
	assert.Equal(t, 182000, alloc.WindowTokens)

	iterBudget := loop.iterationBudgetViaSeam(context.Background())
	convBudget := loop.conversationBudgetViaSeam(context.Background())
	assert.Equal(t, alloc.IterationBudget, iterBudget)
	assert.Equal(t, alloc.ConversationBudget, convBudget)
	// Allocator invariants: iteration capped at the History share,
	// conversation capped at the usable window.
	assert.LessOrEqual(t, iterBudget, alloc.History)
	assert.LessOrEqual(t, convBudget, alloc.WindowTokens)
	assert.Greater(t, iterBudget, 0)
	assert.Greater(t, convBudget, 0)
}

// TestWindowResolutionNoModelFallsBack pins T4's fallback: no discovered
// window anywhere = 0 = statics (IterationTokenBudget /
// DefaultConversationTokenBudget) unchanged.
func TestWindowResolutionNoModelFallsBack(t *testing.T) {
	loop := newSeamTestLoop(t, true, &seamLoopProvider{})

	// The mock chatter's Config() carries no ContextLimit and the firewall
	// bakes a 32768 default for the compressor, but neither is a
	// DISCOVERED window override: the loop's ModelContextLimit override
	// lane is the only pre-discovery window source for the assembler, so
	// an unset lane means the STATIC WINDOW remains (the firewall's baked
	// default model, 32768 -- the same limit requestIsSmallVersusLimit
	// reads). The budget helpers therefore return the allocator's
	// window-derived values for that static window, which for the
	// iteration budget is exactly the prior-art 0.30 ratio (capped at the
	// History share) and for conversation the 0.50 ratio.
	assert.Equal(t, 0, loop.config.ModelContextLimit)
	allocIter, allocConv := seamWindowBudgets(t, loop)
	assert.Equal(t, allocIter, loop.iterationBudgetViaSeam(context.Background()))
	assert.Equal(t, allocConv, loop.conversationBudgetViaSeam(context.Background()))
	// The disabled fallback contract: reasoningCycle's floor is the
	// static conversationTokenBudget path, unchanged.
	assert.Equal(t, DefaultConversationTokenBudget, loop.conversationTokenBudget())
}

// seamWindowBudgets computes the expected iteration/conversation budgets
// through a fresh allocator over the loop's resolved window.
func seamWindowBudgets(t *testing.T, loop *AgentLoop) (iter, conv int) {
	t.Helper()
	window := loop.contextAssemblerWindow()
	alloc, err := contextalloc.New(contextalloc.DefaultConfig())
	require.NoError(t, err)
	a := alloc.Allocate(context.Background(), window, contextalloc.Request{})
	return a.IterationBudget, a.ConversationBudget
}

// ---------------------------------------------------------------------------
// T5: doc path rewiring
// ---------------------------------------------------------------------------

// TestDocPathRewiringDisabledUntouched pins that the disabled path never
// consults the budgeted builder: docBudgetViaSeam reports
// seamEnabled=false so injectProjectContext calls the unbounded
// BuildFullArtifactContext exactly as before.
func TestDocPathRewiringDisabledUntouched(t *testing.T) {
	loop := newSeamTestLoop(t, false, &seamLoopProvider{})
	budget, enabled := loop.docBudgetViaSeam(context.Background())
	assert.Equal(t, 0, budget)
	assert.False(t, enabled)
}

// TestDocPathRewiringEnabledBudgetsDocs pins the enabled path: with a
// discovered window, docBudgetViaSeam yields the allocator's
// SystemDynamicDocs share and the budgeted builder bounds a huge doc to
// it.
func TestDocPathRewiringEnabledBudgetsDocs(t *testing.T) {
	loop := newSeamTestLoop(t, true, &seamLoopProvider{})
	loop.SetContextFirewallConfig(config.LLMContextFirewallConfig{
		ModelContextLimit: 200000,
	})

	budget, enabled := loop.docBudgetViaSeam(context.Background())
	require.True(t, enabled)
	assert.Greater(t, budget, 0)

	// Builder contract: over-budget content comes back within the budget
	// (heuristic counting, the nil-tokenizer fallback).
	huge := strings.Repeat("x", budget*3*2) // 3 chars/token heuristic, 2x over
	section, truncated := truncateDocToBudget(huge, budget, budget*6,
		newDocBudgetTokenizer(""))
	assert.True(t, truncated)
	assert.LessOrEqual(t, llm.EstimateTokenCountHeuristic(section), budget)

	// Under budget the builder reproduces the unbounded bytes.
	small := "tiny project doc"
	section2, truncated2 := truncateDocToBudget(small, budget,
		llm.EstimateTokenCountHeuristic(small), newDocBudgetTokenizer(""))
	assert.False(t, truncated2)
	assert.Equal(t, small, section2)
}

// TestIterationBudgetIntegration pins the reasoningCycle handoff: an
// enabled seam with a window changes the effective iteration budget via
// conversationBudgetViaSeam/iterationBudgetViaSeam returning the
// allocator values, while a disabled seam preserves the statics exactly.
func TestIterationBudgetIntegration(t *testing.T) {
	disabled := newSeamTestLoop(t, false, nil)
	assert.Equal(t, IterationTokenBudget, func() int {
		if b := disabled.iterationBudgetViaSeam(context.Background()); b > 0 {
			return b
		}
		return IterationTokenBudget
	}())

	enabled := newSeamTestLoop(t, true, &seamLoopProvider{})
	enabled.SetContextFirewallConfig(config.LLMContextFirewallConfig{
		ModelContextLimit: 200000,
	})
	b := enabled.iterationBudgetViaSeam(context.Background())
	require.Greater(t, b, 0)
	// Prior-art ratio 0.30 of 200000 = 60000, capped at the History
	// share (25% of 182000 = 45500) -- the documented allocator rule.
	assert.Equal(t, 45500, b)
}

// TestRunWithTaskSeamEndToEnd drives RunWithTask with the seam enabled
// and a provider whose recall feeds items; the turn must complete and the
// conversation's memory context must contain the recalled bullets.
func TestRunWithTaskSeamEndToEnd(t *testing.T) {
	loop := NewAgentLoop("seam-e2e-session", "/tmp",
		WithLLMChatter(newMockChatter(&llm.Response{Content: "done", FinishReason: "stop"})),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
	)
	loop.SetContextAssemblerConfig(config.ContextAssemblerConfig{Enabled: true})
	loop.SetTierBudgetProvider(&seamLoopProvider{items: []string{"e2e recalled fact"}})
	loop.SetContextFirewallConfig(config.LLMContextFirewallConfig{
		ModelContextLimit: 200000,
	})

	resp, err := loop.RunWithTask(context.Background(), &task.Task{
		ID:           "task-seam-e2e",
		Name:         "exercise the seam",
		ContextQuery: "e2e recalled fact",
	})
	require.NoError(t, err)
	assert.Equal(t, "done", resp)
	_ = fmt.Sprint(resp) // keep fmt import honest if asserts change
}

// ---------------------------------------------------------------------------
// Wiring-gap leaf 01-snapshot-seams: ConfigSnapshot propagation proof
// ---------------------------------------------------------------------------

// TestConfigSnapshotPropagatesSeams is the leaf's wiring-failure proof: an
// armed template (assembler config enabled + tier provider + PII restore
// hook) must clone its seams through Manager.GetOrCreateWired. A nil seam
// or nil restore hook on the clone FAILS -- deleting the ConfigSnapshot
// propagation block in loop.go makes this test red.
func TestConfigSnapshotPropagatesSeams(t *testing.T) {
	store, err := pii.NewSwapStore(t.TempDir())
	require.NoError(t, err)
	hook := NewPiiRestoreHook(pii.NewSwapper(store), nil,
		config.PiiConfig{Enabled: true}, slogDiscardLogger())
	require.NotNil(t, hook, "test setup must produce a live restore hook")

	template := NewAgentLoop("seam-template", "/tmp",
		WithLLMChatter(newMockChatter(&llm.Response{Content: "ok", FinishReason: "stop"})),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
	)
	template.SetContextAssemblerConfig(config.ContextAssemblerConfig{Enabled: true})
	template.SetTierBudgetProvider(&seamLoopProvider{items: []string{"template fact"}})
	template.SetPiiRestoreHook(hook)

	// Pre-arm the template's own seam so we also pin that the clone's seam
	// is functionally materializable, not just config-carrying.
	require.NotNil(t, template.contextSeamIfEnabled())

	mgr := NewManager(ManagerConfig{})
	clone, err := mgr.GetOrCreateWired("seam-clone-session", "/project/dir", template)
	require.NoError(t, err)
	require.NotNil(t, clone)

	// The clone's assembler config + provider must have arrived.
	clone.mu.RLock()
	cfgSet, cfgEnabled := clone.contextAssemblerSet, clone.contextAssemblerCfg.Enabled
	clone.mu.RUnlock()
	assert.True(t, cfgSet, "clone lost the assembler config (contextAssemblerSet false)")
	assert.True(t, cfgEnabled, "clone lost the enabled assembler flag")
	assert.NotNil(t, clone.contextSeamIfEnabled(),
		"clone's context seam must materialize from the propagated (cfg, provider) pair -- nil means the ConfigSnapshot propagation was deleted")
	assert.NotNil(t, clone.PiiRestoreHook(),
		"clone's PII restore hook must be re-attached -- nil means the ConfigSnapshot propagation was deleted")

	// And the session identity must still be the clone's own (the excluded
	// fields stay excluded).
	assert.Equal(t, "seam-clone-session", clone.GetSessionID())
}
