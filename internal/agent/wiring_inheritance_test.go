package agent

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decisionLaneResolver builds a Resolver whose decision_model slot
// resolves, so llm.NewDecisionClient(resolver).Available() is true — the
// exact arm condition selectSummarizerForCompressor checks (internal/llm
// decision_compactor_test.go pins condition→DecisionCompactor; these tests
// pin loop wiring→condition for every production loop class).
func decisionLaneResolver(t *testing.T) *llm.Resolver {
	t.Helper()
	cfg := &llm.ProvidersConfig{
		DecisionModel: "dp/dm",
		Providers: map[string]llm.ProviderConfig{
			"dp": {
				API:     "http",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://decision.invalid/v1"},
				Models:  map[string]llm.ModelDef{"dm": {Name: "dm"}},
			},
		},
	}
	r := llm.NewResolver(cfg, nil)
	require.NotNil(t, r.DecisionModel(), "fixture: decision_model must resolve")
	return r
}

func decisionCompactionConfig() config.CompactionConfig {
	return config.CompactionConfig{
		Enabled:         true,
		DecisionEnabled: true,
		DecisionKeepBar: 3,
	}
}

// assertDecisionLaneArmed asserts the loop's construction-time firewall
// config satisfies BOTH selectSummarizerForCompressor arm conditions:
// DecisionCompaction true AND DecisionClient.Available() (non-nil
// resolver with a resolvable decision_model). Before the fix every
// production loop class failed at least one leg: the daemon template had
// no resolver, the firewall config was baked before SetCompactionConfig
// ran, and registry-built loops carried no Compaction config at all.
func assertDecisionLaneArmed(t *testing.T, loop *AgentLoop, label string) {
	t.Helper()
	require.NotNil(t, loop, label+": loop must exist")
	fwCfg := loop.contextFirewallConfig()
	assert.True(t, fwCfg.DecisionCompaction, label+": DecisionCompaction must reach the firewall config")
	assert.Equal(t, 3, fwCfg.DecisionKeepBar, label+": DecisionKeepBar must reach the firewall config")
	require.NotNil(t, fwCfg.DecisionClient, label+": DecisionClient must be constructed")
	assert.True(t, fwCfg.DecisionClient.Available(),
		label+": DecisionClient must be Available (resolver wired with a decision_model)")
	// The firewall itself must exist on the loop (llm chatter was wired).
	assert.NotNil(t, loop.contextFirewall, label+": ContextFirewall must be baked at construction")
}

// TestDecisionCompactionLane_DaemonTemplateAndClone pins legs (a)+(b): a
// template loop built the daemon way — WithResolver + WithCompactionConfig
// BEFORE NewAgentLoop — arms the decision lane, and a ConfigSnapshot clone
// inherits it (clones inherit config + resolver, and bake their own
// firewall at construction).
func TestDecisionCompactionLane_DaemonTemplateAndClone(t *testing.T) {
	resolver := decisionLaneResolver(t)
	client := llm.NewClient(&llm.ModelConfig{
		BaseURL: "http://template.invalid/v1", ModelID: "m", APIKey: "dummy-" + "key",
	})

	template := NewAgentLoop("template", t.TempDir(),
		WithLLMClient(client),
		WithResolver(resolver),
		WithCompactionConfig(decisionCompactionConfig()),
	)
	assertDecisionLaneArmed(t, template, "daemon template")

	clone := NewAgentLoop("clone-session", t.TempDir(), template.ConfigSnapshot()...)
	assertDecisionLaneArmed(t, clone, "template clone")
}

// TestDecisionCompactionLane_RegistryBuilt pins leg (c): RegistryConfig
// carries the Compaction block + Resolver into createLoop, so
// registry-built specialist loops arm the decision lane too.
func TestDecisionCompactionLane_RegistryBuilt(t *testing.T) {
	resolver := decisionLaneResolver(t)
	client := llm.NewClient(&llm.ModelConfig{
		BaseURL: "http://registry.invalid/v1", ModelID: "m", APIKey: "dummy-" + "key",
	})
	reg := NewAgentRegistry(RegistryConfig{
		LLMClient:  client,
		Resolver:   resolver,
		Compaction: decisionCompactionConfig(),
	})
	require.NoError(t, reg.RegisterSpec(&AgentSpec{
		ID:   "decision-test-agent",
		Name: "decision test",
		Role: RoleExecutor,
	}))
	loop := reg.createLoop(reg.mustSpec(t, "decision-test-agent"))
	assertDecisionLaneArmed(t, loop, "registry-built specialist")
}

// mustSpec is a test helper: fetch the registered spec directly.
func (r *AgentRegistry) mustSpec(t *testing.T, id string) *AgentSpec {
	t.Helper()
	spec, ok := r.GetSpec(id)
	require.True(t, ok, "spec %s must be registered", id)
	return spec
}

// --- Finding 3 pins: hookRegistry inheritance ---

// TestRegistryBuiltLoop_InheritsHookRegistry pins that a registry-built
// loop executes tool calls through the daemon-wide hook registry: a
// blocking BeforeToolCall hook must deny the call (previously every
// dispatcher-routed turn ran with l.hookRegistry == nil and the security
// hardening / decision gate were inert).
func TestRegistryBuiltLoop_InheritsHookRegistry(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	var executed int
	registry.Register(NewMockTool("shell", "run commands",
		func(ctx context.Context, _ map[string]any) (any, error) {
			executed++
			return map[string]any{"ok": true}, nil
		}))

	hooks := NewHookRegistry(nil)
	hooks.RegisterBeforeToolCall("blocking-test-hook", HookPriorityCritical,
		&alwaysBlockHook{reason: "test deny"})

	reg := NewAgentRegistry(RegistryConfig{
		ToolRegistry: registry,
		HookRegistry: hooks,
	})
	require.NoError(t, reg.RegisterSpec(&AgentSpec{
		ID:   "hook-test-agent",
		Name: "hook test",
		Role: RoleExecutor,
	}))
	loop := reg.createLoop(reg.mustSpec(t, "hook-test-agent"))
	require.NotNil(t, loop)
	require.NotNil(t, loop.hookRegistry, "registry-built loop must inherit the hook registry")
	loop.executor = NewExecutor(registry, nil)

	results := loop.executeToolCalls(context.Background(), []llm.ToolCall{
		{ID: "tc-1", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{}`}},
	})

	assert.Equal(t, 0, executed, "blocked tool must not execute")
	require.Len(t, results, 1)
	require.NotNil(t, results[0])
	require.NotNil(t, results[0].Result)
	m, ok := results[0].Result.(map[string]any)
	require.True(t, ok, "denied result payload must be a map, got %T", results[0].Result)
	// The alwaysBlockHook is a soft deny: the loop converts it to the
	// phase-1 confirmation shape carrying the hook's reason as summary.
	summary, _ := m["summary"].(string)
	assert.Equal(t, "test deny", summary,
		"denied result must carry the hook's block reason, got %+v", m)
}

// TestVerifierChildLoop_InheritsHookRegistry pins the verification_hook
// childOpts gap: SpawnVerifier's child loop must carry the parent's hook
// registry so its (filtered) tool executions pass through the same
// security/decision hooks.
func TestVerifierChildLoop_InheritsHookRegistry(t *testing.T) {
	registry := NewPlaceholderToolRegistry()
	registry.Register(NewMockTool("file_read", "read files",
		func(ctx context.Context, _ map[string]any) (any, error) {
			return map[string]any{"content": "x"}, nil
		}))
	hooks := NewHookRegistry(nil)
	parent := NewAgentLoop("parent-loop", t.TempDir(),
		WithToolRegistry(registry),
		WithHookRegistry(hooks),
	)
	require.NotNil(t, parent)

	// verifierChildOptions is the extracted child-wiring seam SpawnVerifier
	// builds from: apply it to a fresh child and assert the inheritance.
	child := NewAgentLoop("verifier-child", t.TempDir(), parent.verifierChildOptions("")...)
	require.NotNil(t, child)
	assert.Same(t, hooks, child.hookRegistry,
		"verifier child loop must inherit the parent's hook registry")
}

// TestHardErrorDeny_SetsCanonicalErrorField pins the finding-5 shape: a
// HardError deny must set ExecutionResult.Error (the canonical failure
// field every other shape populates) while KEEPING the Result map shape a
// pinned test asserts (decision_gate_loop_test.go reads m["error"]). Denies
// stay budget-free (never fed to l.repeatErr.Observe) per the
// post-failure contract in executeToolCalls.
func TestHardErrorDeny_SetsCanonicalErrorField(t *testing.T) {
	var executed int
	loop := newGateDenyProbeLoop(t, &executed)
	// Reuses hardDenyHook from decision_gate_loop_test.go: HardError deny
	// on "shell" with reason "command contains potentially injected
	// external data".
	loop.hookRegistry.RegisterBeforeToolCall("hard-deny", HookPriorityCritical, &hardDenyHook{})

	results := loop.executeToolCalls(context.Background(), []llm.ToolCall{
		{ID: "tc-hard", Function: llm.ToolCallFunction{Name: "shell", Arguments: `{}`}},
	})

	assert.Equal(t, 0, executed)
	require.Len(t, results, 1)
	r := results[0]
	require.NotNil(t, r)
	assert.False(t, r.Success, "HardError deny must be a failed result")
	assert.NotEmpty(t, r.Error, "HardError deny must set the canonical Error field")
	assert.Contains(t, r.Error, "injected external data")
	m, ok := r.Result.(map[string]any)
	require.True(t, ok, "Result map shape must be preserved")
	assert.Equal(t, "blocked by security policy", m["error"])
	assert.Equal(t, "command contains potentially injected external data", m["reason"])
}

// alwaysBlockHook is a BeforeToolCall hook that blocks every call.
type alwaysBlockHook struct{ reason string }

func (h *alwaysBlockHook) BeforeToolCall(_ context.Context, _ llm.ToolCall) BlockResult {
	return BlockResult{Block: true, Reason: h.reason}
}
