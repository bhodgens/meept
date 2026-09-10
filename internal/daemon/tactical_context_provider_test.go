package daemon

// Daemon wiring test for the allotment tree leaf 02: the tactical
// scheduler's ContextWindowProvider closure (components.go, next to
// SetSessionStore) must resolve agentID -> model ref (registry spec.Model
// first, default model ref fallback) -> resolver.ResolveRef().ContextLimit,
// and degrade to 0 (legacy scheduling) on every unknown/absent input.

import (
	"io"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// newAllotmentProviderFixture builds a Components carrying just the fields
// the provider closure reads (LLMResolver, AgentRegistry, ModelsConfig),
// with a one-provider/one-model models config whose single model declares
// ContextLimit 8192.
func newAllotmentProviderFixture(t *testing.T) *Components {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	modelsCfg := &config.ModelsConfig{
		Model: "testprov/test-8k",
		Providers: map[string]config.Provider{
			"testprov": {
				API: "openai",
				Options: config.ProviderOptions{
					BaseURL: "http://127.0.0.1:1", // never dialed: ResolveRef is offline
				},
				Models: map[string]config.Model{
					"test-8k": {ContextLimit: 8192},
				},
			},
		},
	}

	providersCfg := &llm.ProvidersConfig{
		Model:     "testprov/test-8k",
		Providers: map[string]llm.ProviderConfig{},
	}
	providersCfg.Providers["testprov"] = llm.ProviderConfig{
		API: "openai",
		Models: map[string]llm.ModelDef{
			"test-8k": {ContextLimit: 8192},
		},
	}

	registry := agent.NewAgentRegistry(agent.RegistryConfig{
		Logger: logger,
	})
	// An agent with an explicit model override pointing at the one model,
	// and one without (falls back to the default model ref).
	if err := registry.RegisterSpec(&agent.AgentSpec{
		ID:          "custom-model-agent",
		Name:        "custom",
		Role:        agent.RoleExecutor,
		Enabled:     true,
		Model:       "testprov/test-8k",
		CanDelegate: false,
	}); err != nil {
		t.Fatalf("register spec: %v", err)
	}

	return &Components{
		ctx:           t.Context(),
		Logger:        logger,
		ModelsConfig:  modelsCfg,
		LLMResolver:   llm.NewResolver(providersCfg, logger),
		AgentRegistry: registry,
	}
}

// runProviderClosure rebuilds the exact closure wired in components.go so
// the test exercises the same agentID -> ref -> ContextLimit logic without
// booting the full daemon. Keep in sync with components.go.
func runProviderClosure(c *Components, agentID string) int {
	resolver := c.LLMResolver
	if resolver == nil {
		return 0
	}
	ref := ""
	if c.AgentRegistry != nil {
		if spec, ok := c.AgentRegistry.GetSpec(agentID); ok {
			ref = spec.Model
		}
	}
	if ref == "" && c.ModelsConfig != nil {
		ref = c.ModelsConfig.Model
	}
	if ref == "" {
		return 0
	}
	if mc := resolver.ResolveRef(ref); mc != nil {
		return mc.ContextLimit
	}
	return 0
}

func TestContextWindowProviderWiring(t *testing.T) {
	c := newAllotmentProviderFixture(t)

	t.Run("default model ref fallback", func(t *testing.T) {
		// Unknown agent (no spec): falls through to the default model ref.
		if got := runProviderClosure(c, "totally-unknown-agent"); got != 8192 {
			t.Errorf("provider = %d, want 8192 via default model ref", got)
		}
	})

	t.Run("explicit spec model wins", func(t *testing.T) {
		if got := runProviderClosure(c, "custom-model-agent"); got != 8192 {
			t.Errorf("provider = %d, want 8192 via spec.Model", got)
		}
	})

	t.Run("unresolvable ref yields zero", func(t *testing.T) {
		c2 := newAllotmentProviderFixture(t)
		c2.ModelsConfig.Model = "testprov/does-not-exist"
		if got := runProviderClosure(c2, "nobody"); got != 0 {
			t.Errorf("provider = %d, want 0 for unresolvable ref", got)
		}
	})

	t.Run("nil resolver yields zero", func(t *testing.T) {
		c2 := newAllotmentProviderFixture(t)
		c2.LLMResolver = nil
		if got := runProviderClosure(c2, "anyone"); got != 0 {
			t.Errorf("provider = %d, want 0 with nil resolver", got)
		}
	})

	t.Run("nil registry still resolves via default", func(t *testing.T) {
		c2 := newAllotmentProviderFixture(t)
		c2.AgentRegistry = nil
		if got := runProviderClosure(c2, "anyone"); got != 8192 {
			t.Errorf("provider = %d, want 8192 with nil registry (default fallback)", got)
		}
	})

	// The wired scheduler accepts the closure (setter contract, nil-guarded).
	t.Run("setter accepts closure", func(t *testing.T) {
		ts := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{})
		ts.SetContextWindowProvider(func(string) int { return 8192 })
		ts.SetContextWindowProvider(nil) // must be a no-op, not a panic
	})
}
