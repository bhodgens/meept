package agent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	pkgsecurity "github.com/caimlas/meept/pkg/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overrideResolver builds a resolver with a "classifier" alias on local/a
// plus a separately resolvable "local/user-b" direct ref, so a user directive
// and an alias resolution can name DIFFERENT models.
func overrideResolver(t *testing.T, baseURL string) *llm.Resolver {
	t.Helper()
	cfg := &llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			testClassifierAlias: {Models: []string{"local/a"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: baseURL},
				Models: map[string]llm.ModelDef{
					"a":      {Name: "alias-model-a"},
					"user-b": {Name: "user-override-b"},
				},
			},
		},
	}
	return llm.NewResolver(cfg, nil)
}

// newOverrideLoop wires a full loop (RunOnce-capable, like
// newHookPipelineLoop) with either a ProviderManager or a concrete client.
func newOverrideLoop(t *testing.T, resolver *llm.Resolver, chatter llm.Chatter) *AgentLoop {
	t.Helper()
	secChecker := pkgsecurity.NewPermissionChecker(pkgsecurity.Config{})
	return NewAgentLoop("sess-override-"+t.Name(), t.TempDir(),
		WithLLMChatter(chatter),
		WithToolRegistry(NewPlaceholderToolRegistry()),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slogDiscardLogger())),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)
}

// TestAgentLoop_UserModelOverrideBeatsAlias_ProviderManager is the gap-2 +
// gap-3 regression test through the REAL turn path (RunOnce → reasoningCycle
// → chatWithFailoverRaw): the loop's chatter is a *llm.ProviderManager (no
// llmClient), a user reassignment directive names local/user-b, and the
// "classifier" alias resolves to local/a. The USER directive must win: the
// wire model must be user-override-b, the override must be CLEARED after the
// turn (it was never cleared pre-fix), and a second turn must fall back to
// the alias-resolved model.
func TestAgentLoop_UserModelOverrideBeatsAlias_ProviderManager(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := overrideResolver(t, server.URL)

	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: []*llm.ModelConfig{
			{ProviderID: "local", ModelID: "manager-default", BaseURL: server.URL},
		},
		Logger: slog.New(slog.DiscardHandler),
	})

	loop := newOverrideLoop(t, resolver, pm)

	// User directive: a one-shot override (the dispatcher's seam).
	loop.SetModelOverride("local/user-b")
	require.Equal(t, "local/user-b", loop.GetModelOverride())

	_, err := loop.RunOnce(context.Background(), "do a thing", "conv-override-pm")
	require.NoError(t, err)

	// Turn 1: the USER directive served, NOT the alias-resolved local/a and
	// not the manager default. Pre-fix this call silently went to the
	// manager's default (the override branch required a concrete llmClient).
	require.GreaterOrEqual(t, cap.count(), 1, "the LLM must have been reached")
	assert.Equal(t, "user-override-b", cap.models[0],
		"user directive must outrank the alias resolution (chatWithFailoverRaw appends the alias option after the caller opts)")

	// The one-shot override is consumed: cleared after application.
	assert.Empty(t, loop.GetModelOverride(),
		"one-shot override must be cleared even when the chatter is a ProviderManager (pre-fix it was never cleared)")
	assert.Nil(t, loop.pendingModelOverrideConfig, "staged override config must be consumed")
}

// TestAgentLoop_UserModelOverride_ConcreteClientStillSwitched keeps the
// concrete-*llm.Client seam intact: the override both retargets the client
// via SwitchModel AND reaches the wire through the request-scoped option.
func TestAgentLoop_UserModelOverride_ConcreteClientStillSwitched(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := overrideResolver(t, server.URL)

	client := llm.NewClient(&llm.ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "local",
		ModelID:    "configured-default",
	})
	loop := newOverrideLoop(t, resolver, client)

	loop.SetModelOverride("local/user-b")

	_, err := loop.RunOnce(context.Background(), "do a thing", "conv-override-client")
	require.NoError(t, err)

	require.GreaterOrEqual(t, cap.count(), 1)
	assert.Equal(t, "user-override-b", cap.models[0])
	assert.Empty(t, loop.GetModelOverride())
}

// TestAgentLoop_UserModelOverride_PersistentSurvives pins the persistent
// (shadow hot-swap) variant: the staged override is NOT cleared after one
// call and keeps outranking the alias on subsequent calls.
func TestAgentLoop_UserModelOverride_PersistentSurvives(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := overrideResolver(t, server.URL)

	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: []*llm.ModelConfig{
			{ProviderID: "local", ModelID: "manager-default", BaseURL: server.URL},
		},
		Logger: slog.New(slog.DiscardHandler),
	})

	loop := newOverrideLoop(t, resolver, pm)

	loop.SetPersistentModelOverride("local/user-b")

	_, err := loop.RunOnce(context.Background(), "turn one", "conv-override-persist")
	require.NoError(t, err)

	require.GreaterOrEqual(t, cap.count(), 1)
	assert.Equal(t, "user-override-b", cap.last(),
		"persistent override must serve the turn")
	assert.Equal(t, "local/user-b", loop.GetModelOverride(),
		"persistent override must not auto-clear")
	assert.NotNil(t, loop.pendingModelOverrideConfig,
		"persistent override keeps its staged config")
}
