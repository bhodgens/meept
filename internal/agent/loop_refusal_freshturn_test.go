package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Bughunt F4/F5 pins (promoted from the parent's read-only audit probes,
// /tmp/meept-week-C-overlay-test.go).

// TestAgentLoop_RefusalPinClearsOnFreshTurn (bughunt F4): a refusal that
// falls back to the refusal model arms a PERSISTENT override. The fresh-turn
// sweep must clear that pin when the fallback turn succeeded — without the
// clear, every later turn silently served the fallback model (sticky pin,
// introduced 1771980f). The pin must clear even when the fallback retry
// never consumed it, and the turn after the clear must serve the base alias.
func TestAgentLoop_RefusalPinClearsOnFreshTurn(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()
	resolver := llm.NewResolver(&llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			testClassifierAlias: {Models: []string{"local/base"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: server.URL},
				Models: map[string]llm.ModelDef{
					"base":     {Name: "base"},
					"fallback": {Name: "fallback"},
				},
			},
		},
	}, nil)
	inner := llm.NewClient(&llm.ModelConfig{BaseURL: server.URL, ProviderID: "local", ModelID: "default"})
	chatter := &refusalChatter{inner: inner, refusals: 1}
	loop := newOverrideLoop(t, resolver, chatter)
	loop.spec = &AgentSpec{RefusalModel: "local/fallback"}

	// Turn 1: the refusal fires, the fallback retry is armed and succeeds.
	_, err := loop.RunOnce(context.Background(), "hello", "conv-refusal-fresh")
	require.NoError(t, err)
	require.Equal(t, 2, chatter.callCount(), "turn 1: refusal + fallback retry")
	assert.Equal(t, "fallback", cap.last(), "turn 1: the fallback model served the retry")

	// Turn 2 (fresh): the pin must NOT survive — the base alias serves, and
	// no override remains armed.
	_, err = loop.RunOnce(context.Background(), "hello again", "conv-refusal-fresh")
	require.NoError(t, err)
	assert.Empty(t, loop.GetModelOverride(), "fresh turn must clear the refusal pin")
	assert.Nil(t, loop.pendingModelOverrideConfig, "staged fallback config must clear with the pin")
	assert.Equal(t, "base", cap.last(), "fresh turn must use the base alias, not the sticky fallback")
	assert.NotContains(t, cap.models[1:], "fallback",
		"the fallback model must not serve any call after its own turn")
}

// TestAgentLoop_GlobalRefusalModelSurvivesClone (bughunt F5): the global
// models.json5 refusal_model slot must propagate through ConfigSnapshot —
// clones of the daemon template previously lost it (globalRefusalModel was
// not in the snapshot), silently disabling the refusal fallback on every
// per-session loop (introduced 1771980f).
func TestAgentLoop_GlobalRefusalModelSurvivesClone(t *testing.T) {
	template := NewAgentLoop("f5-template", t.TempDir())
	template.SetGlobalRefusalModel("local/fallback")

	clone := NewAgentLoop("f5-clone", t.TempDir(), template.ConfigSnapshot()...)
	assert.Equal(t, "local/fallback", clone.refusalFallbackRef(),
		"clone must inherit the global refusal configuration")

	// An empty slot must stay empty (feature off), not resurrect stale state.
	empty := NewAgentLoop("f5-empty-template", t.TempDir())
	emptyClone := NewAgentLoop("f5-empty-clone", t.TempDir(), empty.ConfigSnapshot()...)
	assert.Empty(t, emptyClone.refusalFallbackRef(),
		"clone of a loop with no refusal configuration stays feature-off")
}
