package daemon

import (
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// plannerModelWiringModelsConfig extends the transcript wiring hermetic
// models config with a resolvable planner-model slot target. NoAuth +
// loopback URL: the client is constructed lazily and never Chatted with.
func plannerModelWiringModelsConfig() *config.ModelsConfig {
	mc := transcriptWiringModelsConfig()
	mc.Providers["testprov"].Models["planner-strong"] = config.Model{Name: "planner-strong"}
	return mc
}

// TestPlannerModelClientConstruction verifies the dedicated planner-model
// client is built EXACTLY when the planner_model slot names a resolvable
// model (same nil semantics as ExtractClient / MemoryModelClient):
// set+resolvable -> non-nil client whose resolved config names the
// configured model; empty or unresolvable slot -> nil field.
func TestPlannerModelClientConstruction(t *testing.T) {
	t.Run("set_builds_dedicated_client", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		models := plannerModelWiringModelsConfig()
		models.PlannerModel = "testprov/planner-strong"
		comps := newMemoryModelWiringComponents(t, cfg, models)
		if comps.PlannerModelClient == nil {
			t.Fatal("PlannerModelClient nil with planner_model set and resolvable")
		}
		if got := comps.PlannerModelClient.Config().ModelID; got != "planner-strong" {
			t.Errorf("planner model client resolved %q, want %q", got, "planner-strong")
		}
	})

	t.Run("empty_leaves_nil", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		comps := newMemoryModelWiringComponents(t, cfg, plannerModelWiringModelsConfig())
		if comps.PlannerModelClient != nil {
			t.Error("PlannerModelClient built without planner_model")
		}
	})

	t.Run("unresolvable_leaves_nil", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		models := plannerModelWiringModelsConfig()
		models.PlannerModel = "testprov/does-not-exist"
		comps := newMemoryModelWiringComponents(t, cfg, models)
		if comps.PlannerModelClient != nil {
			t.Error("PlannerModelClient built with unresolvable planner_model")
		}
	})
}

// plannerLoopChatter returns the concrete chatter serving the loop: the
// chatter itself, or the inner chatter when the loop wrapped it in the
// context firewall (createLoop builds loops with the firewall enabled by
// default — the pointer identity lives one wrapper down).
func plannerLoopChatter(loop *agent.AgentLoop) llm.Chatter {
	ch := loop.Chatter()
	if fw, ok := ch.(*llm.ContextFirewall); ok {
		return fw.Inner()
	}
	return ch
}

// TestPlannerModelServesPlannerLoop pins the registry wiring end to end:
// when planner_model is set and resolvable, the planner agent loop created
// by the registry is built on the dedicated client (its chatter IS the
// dedicated client pointer, and its modelRef is blanked so the planner
// alias resolution cannot SwitchModel the dedicated client onto the
// planner alias chain). With the slot empty, the loop stays on the general
// chat client and keeps its alias modelRef — byte-identical to the
// pre-slot behavior.
func TestPlannerModelServesPlannerLoop(t *testing.T) {
	t.Run("dedicated_client_when_set", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		cfg.MultiAgent.Enabled = true
		models := plannerModelWiringModelsConfig()
		models.PlannerModel = "testprov/planner-strong"
		comps := newMemoryModelWiringComponents(t, cfg, models)

		if comps.PlannerModelClient == nil {
			t.Fatal("PlannerModelClient nil; planner-loop case not exercisable")
		}
		if comps.AgentRegistry == nil {
			t.Fatal("AgentRegistry nil; MultiAgent wiring did not run")
		}
		loop, err := comps.AgentRegistry.Get(config.AgentIDPlanner)
		if err != nil {
			t.Fatalf("registry.Get(planner): %v", err)
		}
		if !sameChatter(plannerLoopChatter(loop), comps.PlannerModelClient) {
			t.Error("planner loop chatter is not the dedicated planner_model client")
		}
		if ref := loop.GetModelRef(); ref != "" {
			t.Errorf("planner loop modelRef = %q, want empty (alias resolution must not retarget the dedicated client)", ref)
		}
		// The dedicated client must NOT have been retargeted onto the
		// planner alias chain: it still names the slot model.
		if got := comps.PlannerModelClient.Config().ModelID; got != "planner-strong" {
			t.Errorf("dedicated planner client resolved to %q, want planner-strong (slot model must survive loop construction)", got)
		}
		// The firewall (when present) must be built on the dedicated
		// client's config, i.e. the slot model is what actually serves.
		if ch := loop.Chatter(); ch != nil {
			if fw, ok := ch.(*llm.ContextFirewall); ok {
				if got := fw.Config().ModelID; got != "planner-strong" {
					t.Errorf("planner loop firewall model = %q, want planner-strong", got)
				}
			}
		}
	})

	t.Run("general_client_when_empty", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		cfg.MultiAgent.Enabled = true
		comps := newMemoryModelWiringComponents(t, cfg, plannerModelWiringModelsConfig())

		if comps.PlannerModelClient != nil {
			t.Fatal("PlannerModelClient non-nil with empty slot")
		}
		if comps.AgentRegistry == nil {
			t.Fatal("AgentRegistry nil; MultiAgent wiring did not run")
		}
		loop, err := comps.AgentRegistry.Get(config.AgentIDPlanner)
		if err != nil {
			t.Fatalf("registry.Get(planner): %v", err)
		}
		if comps.LLMProvider == nil {
			t.Fatal("LLMProvider nil; preference-order case not exercisable")
		}
		if loop.Chatter() == nil {
			t.Fatal("planner loop chatter nil; pre-slot behavior changed")
		}
		// Byte-identical fallback: with the slot empty the loop serves on
		// the general chat client (the same LLMProvider/LLMClient chain)
		// and keeps the planner alias modelRef exactly as before.
		if !sameChatter(plannerLoopChatter(loop), comps.LLMProvider) {
			t.Errorf("planner loop chatter = %T, want the general chat client with empty planner_model (must be byte-identical fallback)",
				plannerLoopChatter(loop))
		}
	})
}
