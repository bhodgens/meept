package daemon

import (
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// memoryModelWiringModelsConfig extends the transcript wiring hermetic
// models config with resolvable memory-model slot targets. NoAuth +
// loopback URL: the clients are constructed lazily and never Chatted with.
func memoryModelWiringModelsConfig() *config.ModelsConfig {
	mc := transcriptWiringModelsConfig()
	mc.Providers["testprov"].Models["memory-small"] = config.Model{Name: "memory-small"}
	return mc
}

// newMemoryModelWiringComponents constructs Components with an injected
// hermetic models config, mirroring newTranscriptWiringComponents so the
// auxiliary client construction runs without touching the host's
// models.json5.
func newMemoryModelWiringComponents(t *testing.T, cfg *config.Config, models *config.ModelsConfig) *Components {
	t.Helper()
	logger := testLogger(t)
	msgBus := bus.New(nil, logger)

	comps, err := NewComponents(t.Context(), cfg, msgBus, logger, models)
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() {
		_ = comps.Stop(t.Context())
	})
	return comps
}

// sameChatter compares two llm.Chatter interface values by underlying
// pointer identity (llm.Client pointers — no == on distinct dynamic
// types).
func sameChatter(a, b llm.Chatter) bool {
	ac, aok := a.(*llm.Client)
	bc, bok := b.(*llm.Client)
	return aok && bok && ac == bc
}

// TestMemoryModelClientConstruction verifies the dedicated memory-model
// client is built EXACTLY when the memory_model slot names a resolvable
// model (same nil semantics as ExtractClient): set+resolvable -> non-nil
// client whose resolved config names the configured model; empty or
// unresolvable slot -> nil field.
func TestMemoryModelClientConstruction(t *testing.T) {
	t.Run("set_builds_dedicated_client", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		models := memoryModelWiringModelsConfig()
		models.MemoryModel = "testprov/memory-small"
		comps := newMemoryModelWiringComponents(t, cfg, models)
		if comps.MemoryModelClient == nil {
			t.Fatal("MemoryModelClient nil with memory_model set and resolvable")
		}
		if got := comps.MemoryModelClient.Config().ModelID; got != "memory-small" {
			t.Errorf("memory model client resolved %q, want %q", got, "memory-small")
		}
	})

	t.Run("empty_leaves_nil", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		comps := newMemoryModelWiringComponents(t, cfg, memoryModelWiringModelsConfig())
		if comps.MemoryModelClient != nil {
			t.Error("MemoryModelClient built without memory_model")
		}
	})

	t.Run("unresolvable_leaves_nil", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		models := memoryModelWiringModelsConfig()
		models.MemoryModel = "testprov/does-not-exist"
		comps := newMemoryModelWiringComponents(t, cfg, models)
		if comps.MemoryModelClient != nil {
			t.Error("MemoryModelClient built with unresolvable memory_model")
		}
	})
}

// TestMemoryModelPreferenceOrder pins the wiring preference order
// end-to-end through NewComponents:
//
//   - memory_model set -> the memory manager's LLM (consolidation +
//     distill summarizer) and the ambient epistemic chatter resolve to
//     the dedicated client (the same pointer, by construction).
//   - memory_model empty -> both stay on the general chat client,
//     byte-identical to the pre-slot behavior.
func TestMemoryModelPreferenceOrder(t *testing.T) {
	t.Run("dedicated_client_wins_when_set", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		models := memoryModelWiringModelsConfig()
		models.MemoryModel = "testprov/memory-small"
		comps := newMemoryModelWiringComponents(t, cfg, models)

		if comps.MemoryModelClient == nil {
			t.Fatal("MemoryModelClient nil; preference-order case not exercisable")
		}
		if comps.LLMProvider == nil {
			t.Fatal("LLMProvider nil; preference-order case not exercisable")
		}
		if comps.MemoryManager == nil {
			t.Fatal("MemoryManager nil; preference-order case not exercisable")
		}
		if !sameChatter(comps.MemoryManager.LLM(), comps.MemoryModelClient) {
			t.Error("memory manager LLM did not prefer the dedicated memory_model client")
		}
	})

	t.Run("general_client_when_empty", func(t *testing.T) {
		cfg, _ := skillToolsTestConfig(t)
		comps := newMemoryModelWiringComponents(t, cfg, memoryModelWiringModelsConfig())

		if comps.MemoryModelClient != nil {
			t.Fatal("MemoryModelClient non-nil with empty slot")
		}
		if comps.LLMProvider == nil {
			t.Fatal("LLMProvider nil; preference-order case not exercisable")
		}
		if comps.MemoryManager == nil {
			t.Fatal("MemoryManager nil; preference-order case not exercisable")
		}
		if !sameChatter(comps.MemoryManager.LLM(), comps.LLMProvider) {
			t.Error("memory manager LLM changed away from the general client with empty memory_model (must be byte-identical fallback)")
		}
	})
}
