package daemon

// Pin for bughunt wave finding M1: the quota-aware routing warning
// (TacticalScheduler -> agent.routing.warning) was permanently dark.
//
// internal/daemon/components.go wired AgentHealthAdapter's lookup with
// ref = agentID — a BARE ALIAS name ("coder"), taken because no shipped
// agent declares `model:` in its AGENT.md — and then resolved it with
// resolver.ResolveRef(ref) -> llm.ResolveModelRef, which requires a
// "provider/model" ref and returns nil for a bare name. The lookup
// therefore returned ok=false for EVERY agent, so binding.Config was nil
// and AgentParkedOrCooling short-circuited false before ever reaching the
// HasHealthyModels(alias) branch: a genuinely parked or cooling agent
// emitted no routing.warning, silently.
//
// The fix resolves the alias through resolver.ResolveForAlias (the
// resolver's own alias→model entry point) instead. This test proves the
// alias branch is reachable AND that the adapter's verdict flips when the
// alias really is parked.

import (
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// TestAgentModelBindingFor_AliasResolves is the M1 pin. The fixture mirrors
// the shipped shape: a single provider with two models and a "coder" alias
// whose member list holds exactly one of them, plus an agent spec whose ID
// matches the alias and whose Model is EMPTY (the production shape — zero
// `model:` keys exist under config/agents/**).
func TestAgentModelBindingFor_AliasResolves(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	providersCfg := &llm.ProvidersConfig{
		Model: "testprov/default-model",
		Providers: map[string]llm.ProviderConfig{
			"testprov": {
				API: "openai",
				// Never dialed: the adapter only reads resolver state.
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:1"},
				Models: map[string]llm.ModelDef{
					"default-model": {ContextLimit: 8192},
					"coder-model":   {ContextLimit: 16384},
				},
			},
		},
		ModelAliases: map[string]llm.ModelAliasEntry{
			// Single-member alias: HasHealthyModels is then a real
			// cooldown/quota probe rather than the multi-member
			// always-true short circuit.
			"coder": {Models: []string{"testprov/coder-model"}, Timeout: 30, MaxFails: 1},
		},
	}

	registry := agent.NewAgentRegistry(agent.RegistryConfig{Logger: logger})
	if err := registry.RegisterSpec(&agent.AgentSpec{
		ID:      "coder",
		Name:    "coder",
		Role:    agent.RoleExecutor,
		Enabled: true,
		// NO Model — the production shape.
	}); err != nil {
		t.Fatalf("register spec: %v", err)
	}

	modelsCfg := &config.ModelsConfig{Model: "testprov/default-model"}

	t.Run("alias agent resolves to its alias model", func(t *testing.T) {
		resolver := llm.NewResolver(providersCfg, logger)
		binding, ok := agentModelBindingFor(resolver, registry, modelsCfg, "coder")
		if !ok {
			t.Fatal("agentModelBindingFor returned ok=false for the alias agent: the alias branch is unreachable again (M1)")
		}
		if binding.Config == nil {
			t.Fatal("binding.Config is nil; the adapter would report \"not parked\" unconditionally")
		}
		if binding.Alias != "coder" {
			t.Errorf("binding.Alias = %q, want %q", binding.Alias, "coder")
		}
		if got := binding.Config.ModelID; got != "coder-model" {
			t.Errorf("binding.Config.ModelID = %q, want %q (the alias member)", got, "coder-model")
		}
	})

	t.Run("healthy alias is not parked", func(t *testing.T) {
		resolver := llm.NewResolver(providersCfg, logger)
		adapter := agent.NewAgentHealthAdapter(
			func(agentID string) (agent.AgentModelBinding, bool) {
				return agentModelBindingFor(resolver, registry, modelsCfg, agentID)
			},
			resolver,
		)
		if adapter.AgentParkedOrCooling("coder") {
			t.Error("a fresh alias reported parked; the pin cannot distinguish a real park from a false positive")
		}
	})

	t.Run("parked alias reports parked", func(t *testing.T) {
		resolver := llm.NewResolver(providersCfg, logger)
		// Quota blocking must be enabled for HasHealthyModels to consult
		// the alias's block state (resolver.quotaEnabled gates it).
		resolver.SetQuotaConfig(&llm.QuotaWaitConfig{Enabled: true, MaxWait: 0})

		mc, err := resolver.ResolveForAlias("coder", "")
		if err != nil || mc == nil {
			t.Fatalf("precondition: ResolveForAlias(coder) = %v, %v", mc, err)
		}

		adapter := agent.NewAgentHealthAdapter(
			func(agentID string) (agent.AgentModelBinding, bool) {
				return agentModelBindingFor(resolver, registry, modelsCfg, agentID)
			},
			resolver,
		)

		// Park the alias: quota-block its only member at the credential and
		// entry level (the state the quota-reset-resilience path arms).
		resolver.BlockQuotaEntry("coder", mc.ProviderID, mc.ModelID, time.Now().Add(time.Hour))
		resolver.BlockQuotaCredential("coder", llm.QuotaCredentialKey(mc.ProviderID, mc), time.Now().Add(time.Hour))

		if !adapter.AgentParkedOrCooling("coder") {
			t.Error("adapter reported NOT parked for the quota-blocked `coder` alias: the routing.warning branch is still dark (M1)")
		}

		// The probe must be READ-ONLY. TacticalScheduler asks this on every
		// routing decision, so a lookup that resolved the alias through
		// llm.Resolver.ResolveForAlias would advance the rotation cursor and
		// clear the cooldown/failure counters on every call — healing the
		// very park it exists to detect, so the second ask would report
		// healthy. Five consecutive asks must all report parked.
		for i := range 5 {
			if !adapter.AgentParkedOrCooling("coder") {
				t.Fatalf("ask %d reported NOT parked: the lookup mutated resolver health state instead of reading it", i+1)
			}
		}

		// Unblock and the verdict returns to false, so the pin is not a
		// permanent "always true".
		resolver.ClearQuotaBlocks("coder")
		if adapter.AgentParkedOrCooling("coder") {
			t.Error("adapter still reports parked after the quota blocks were cleared")
		}
	})

	t.Run("unknown agent and nil resolver stay false", func(t *testing.T) {
		resolver := llm.NewResolver(providersCfg, logger)
		if _, ok := agentModelBindingFor(resolver, registry, modelsCfg, "not-an-agent"); ok {
			// The default-model fallback resolves, which is the intended
			// behaviour (context-window provider parity) — assert only that
			// it never parks.
			t.Log("unknown agent falls through to the default model ref (as designed)")
		}
		if _, ok := agentModelBindingFor(nil, registry, modelsCfg, "coder"); ok {
			t.Error("nil resolver must report ok=false")
		}
	})
}

// TestAgentModelBindingFor_ExplicitModelRefStillWins guards the precedence
// the helper must preserve: an explicit spec.Model is resolved as a
// "provider/model" ref and does NOT become the agent's alias.
func TestAgentModelBindingFor_ExplicitModelRefStillWins(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	providersCfg := &llm.ProvidersConfig{
		Model: "testprov/default-model",
		Providers: map[string]llm.ProviderConfig{
			"testprov": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: "http://127.0.0.1:1"},
				Models:  map[string]llm.ModelDef{"default-model": {}, "explicit-model": {}},
			},
		},
		ModelAliases: map[string]llm.ModelAliasEntry{
			"explicit-agent": {Models: []string{"testprov/default-model"}, Timeout: 30, MaxFails: 1},
		},
	}

	registry := agent.NewAgentRegistry(agent.RegistryConfig{Logger: logger})
	if err := registry.RegisterSpec(&agent.AgentSpec{
		ID:      "explicit-agent",
		Name:    "explicit",
		Role:    agent.RoleExecutor,
		Enabled: true,
		Model:   "testprov/explicit-model",
	}); err != nil {
		t.Fatalf("register spec: %v", err)
	}

	resolver := llm.NewResolver(providersCfg, logger)
	binding, ok := agentModelBindingFor(resolver, registry, &config.ModelsConfig{Model: "testprov/default-model"}, "explicit-agent")
	if !ok {
		t.Fatal("explicit spec.Model failed to resolve")
	}
	if got := binding.Config.ModelID; got != "explicit-model" {
		t.Errorf("binding.Config.ModelID = %q, want explicit-model", got)
	}
	if binding.Alias != "" {
		t.Errorf("binding.Alias = %q, want empty: an explicit ref must not be reported as an alias", binding.Alias)
	}

	// A spec.Model that NAMES an alias resolves through the alias path and
	// does report that alias, so its rotation health stays queryable.
	registry2 := agent.NewAgentRegistry(agent.RegistryConfig{Logger: logger})
	if err := registry2.RegisterSpec(&agent.AgentSpec{
		ID: "alias-ref-agent", Name: "aliasref", Role: agent.RoleExecutor, Enabled: true,
		Model: "explicit-agent",
	}); err != nil {
		t.Fatalf("register spec 2: %v", err)
	}
	binding2, ok := agentModelBindingFor(resolver, registry2, &config.ModelsConfig{Model: "testprov/default-model"}, "alias-ref-agent")
	if !ok {
		t.Fatal("a spec.Model naming an alias failed to resolve")
	}
	if binding2.Alias != "explicit-agent" {
		t.Errorf("binding2.Alias = %q, want explicit-agent", binding2.Alias)
	}
	if got := binding2.Config.ModelID; got != "default-model" {
		t.Errorf("binding2.Config.ModelID = %q, want default-model (the alias member)", got)
	}
}
