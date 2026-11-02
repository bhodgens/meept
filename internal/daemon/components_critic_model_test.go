package daemon

import (
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// criticModelWiringModelsConfig extends the transcript wiring hermetic
// models config with resolvable critic-model slot targets. NoAuth +
// loopback URL: the clients are constructed lazily and never Chatted
// with (plan-20261101-fresh-context-critic leaf 07, contract C9).
func criticModelWiringModelsConfig() *config.ModelsConfig {
	mc := transcriptWiringModelsConfig()
	mc.Providers["testprov"].Models["critic-small"] = config.Model{Name: "critic-small"}
	mc.Providers["testprov"].Models["hostile-distinct"] = config.Model{Name: "hostile-distinct"}
	return mc
}

// TestCriticModelClientConstruction verifies the two fresh-context critic
// clients are built EXACTLY when their slots name a resolvable model
// (same nil semantics as PlannerModelClient): set+resolvable -> non-nil
// client whose resolved config names the configured model; empty or
// unresolvable slot -> nil field, never a boot failure. An EMPTY
// hostile_critic_model must NOT reuse any other client: nil hostile
// chatter = the pass is skipped (C5), never a planner fallback.
func TestCriticModelClientConstruction(t *testing.T) {
	t.Run("set_builds_dedicated_clients", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		models := criticModelWiringModelsConfig()
		models.CriticModel = "testprov/critic-small"
		models.HostileCriticModel = "testprov/hostile-distinct"
		comps := newMemoryModelWiringComponents(t, cfg, models)
		if comps.CriticModelClient == nil {
			t.Fatal("CriticModelClient nil with critic_model set and resolvable")
		}
		if got := comps.CriticModelClient.Config().ModelID; got != "critic-small" {
			t.Errorf("critic model client resolved %q, want %q", got, "critic-small")
		}
		if comps.HostileCriticModelClient == nil {
			t.Fatal("HostileCriticModelClient nil with hostile_critic_model set and resolvable")
		}
		if got := comps.HostileCriticModelClient.Config().ModelID; got != "hostile-distinct" {
			t.Errorf("hostile critic client resolved %q, want %q", got, "hostile-distinct")
		}
	})

	t.Run("empty_leaves_nil", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		comps := newMemoryModelWiringComponents(t, cfg, criticModelWiringModelsConfig())
		if comps.CriticModelClient != nil {
			t.Error("CriticModelClient built without critic_model")
		}
		// C5: the empty hostile slot is a nil client (nil chatter at the
		// wiring site), NOT a reuse of the planner or critic client.
		if comps.HostileCriticModelClient != nil {
			t.Error("HostileCriticModelClient built without hostile_critic_model")
		}
	})

	t.Run("unresolvable_leaves_nil", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		models := criticModelWiringModelsConfig()
		models.CriticModel = "testprov/does-not-exist"
		models.HostileCriticModel = "testprov/also-missing"
		comps := newMemoryModelWiringComponents(t, cfg, models)
		if comps.CriticModelClient != nil {
			t.Error("CriticModelClient built with unresolvable critic_model")
		}
		if comps.HostileCriticModelClient != nil {
			t.Error("HostileCriticModelClient built with unresolvable hostile_critic_model")
		}
	})
}
