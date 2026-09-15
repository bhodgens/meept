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

// ---------------------------------------------------------------------------
// Per-request model (chat.request "model") — the HTTP chat API surface.
//
// ApplyRequestModel arms the SAME one-shot SetModelOverride seam the
// dispatcher's parsed user directives use, so everything below rides the
// existing precedence contract: request model / user directive > alias
// resolution > default. These tests extend the RunOnce-level pattern above
// with the chat-API entry point.
// ---------------------------------------------------------------------------

// TestAgentLoop_RequestModelReachesWire_ProviderManager: a chat request
// naming local/user-b as its per-request model must serve the turn on
// user-override-b (not the alias-resolved local/a, not the manager default)
// through a ProviderManager chatter — the daemon's real wiring.
func TestAgentLoop_RequestModelReachesWire_ProviderManager(t *testing.T) {
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

	loop.ApplyRequestModel("local/user-b")

	_, err := loop.RunOnce(context.Background(), "do a thing", "conv-req-model")
	require.NoError(t, err)

	require.GreaterOrEqual(t, cap.count(), 1, "the LLM must have been reached")
	assert.Equal(t, "user-override-b", cap.models[0],
		"the per-request model must reach the wire, outranking the alias resolution")
	assert.Empty(t, loop.GetModelOverride(),
		"the per-request model is one-shot: consumed by the turn that carried it")
	assert.Nil(t, loop.pendingModelOverrideConfig, "staged override config must be consumed")
}

// TestAgentLoop_RequestModelBeatsAlias pins precedence at the chat-API
// entry: the request model outranks the loop's configured alias
// (request model > alias resolution) on the same turn.
func TestAgentLoop_RequestModelBeatsAlias(t *testing.T) {
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

	loop := newOverrideLoop(t, resolver, pm) // WithModelRef(testClassifierAlias) → local/a

	loop.ApplyRequestModel("local/user-b")
	_, err := loop.RunOnce(context.Background(), "turn with a request model", "conv-req-beats-alias")
	require.NoError(t, err)

	require.GreaterOrEqual(t, cap.count(), 1)
	assert.Equal(t, "user-override-b", cap.models[0],
		"request model must outrank the alias resolution (alias resolves local/a); "+
			"the FIRST wire call of the turn carries it — later calls in the same turn's "+
			"nudge ladder legitimately fall back to the alias (one-shot = one LLM call)")
}

// TestAgentLoop_RequestModel_NoLeakToNextTurn: turn 1 carries the request
// model; turn 2 (same loop, no new model field) must fall back to the
// alias-resolved model — the one-shot override must not leak forward.
func TestAgentLoop_RequestModel_NoLeakToNextTurn(t *testing.T) {
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

	loop.ApplyRequestModel("local/user-b")
	_, err := loop.RunOnce(context.Background(), "turn one with model", "conv-no-leak")
	require.NoError(t, err)
	require.Equal(t, "user-override-b", cap.models[0])

	// Turn 2: no request model — the alias must serve again.
	_, err = loop.RunOnce(context.Background(), "turn two without model", "conv-no-leak")
	require.NoError(t, err)

	require.GreaterOrEqual(t, cap.count(), 2)
	assert.Equal(t, "alias-model-a", cap.last(),
		"the next turn must fall back to the alias-resolved model (no leak)")
	assert.NotContains(t, cap.models[1:], "user-override-b",
		"the request model must not appear again after its turn (no leak)")
	assert.Empty(t, loop.GetModelOverride())
}

// TestAgentLoop_RequestModel_AbsentUnchangedBehavior: no model field →
// byte-identical legacy behavior (alias resolution serves the turn, no
// override is ever armed).
func TestAgentLoop_RequestModel_AbsentUnchangedBehavior(t *testing.T) {
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

	// No ApplyRequestModel call at all.
	_, err := loop.RunOnce(context.Background(), "plain turn", "conv-absent")
	require.NoError(t, err)

	require.GreaterOrEqual(t, cap.count(), 1)
	assert.Equal(t, "alias-model-a", cap.last(),
		"without a request model the alias-resolved model serves the turn")
	assert.Empty(t, loop.GetModelOverride())

	// An explicit empty ref is an explicit no-op too.
	loop.ApplyRequestModel("")
	assert.Empty(t, loop.GetModelOverride())
}

// TestAgentLoop_RequestModel_UnresolvableRefDropped: a model ref that
// resolves to nothing is dropped with a warn — the turn runs the
// alias/default chain rather than arming a dead override.
func TestAgentLoop_RequestModel_UnresolvableRefDropped(t *testing.T) {
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

	loop.ApplyRequestModel("local/does-not-exist")
	assert.Empty(t, loop.GetModelOverride(),
		"an unresolvable ref must not arm the override")

	_, err := loop.RunOnce(context.Background(), "turn after a bad ref", "conv-bad-ref")
	require.NoError(t, err)
	require.GreaterOrEqual(t, cap.count(), 1)
	assert.Equal(t, "alias-model-a", cap.last(),
		"the alias-resolved model serves the turn after a dropped ref")
}
