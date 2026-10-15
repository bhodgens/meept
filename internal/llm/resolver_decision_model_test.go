package llm

import (
	"testing"
)

// Decision-model tree leaf 01 (docs/plans/decision-model/01-config-slot.md):
// pins the resolver-side decision_model wiring — resolved ONLY when the slot
// is set, nil-when-unset, accessor semantics, and merge overlay behavior.
func TestResolver_DecisionModel(t *testing.T) {
	t.Run("unset_slot", func(t *testing.T) {
		r := NewResolver(&ProvidersConfig{
			Model:     "prov/main",
			Providers: map[string]ProviderConfig{},
		}, nil)
		if r.DecisionModel() != nil {
			t.Fatalf("DecisionModel() = %+v, want nil (empty slot = features disabled, never small_model fallback)", r.DecisionModel())
		}
		if got := r.DecisionModelName(); got != "" {
			t.Fatalf("DecisionModelName() = %q, want %q", got, "")
		}
		// Explicit never-falls-back-to-small_model pin: a configured small
		// model must not bleed into the decision slot.
		r2 := NewResolver(&ProvidersConfig{
			Model:      "prov/main",
			SmallModel: "prov/small",
			Providers:  map[string]ProviderConfig{},
		}, nil)
		if r2.DecisionModel() != nil {
			t.Fatal("DecisionModel() != nil with small_model set; slot must never default to small_model")
		}
	})

	t.Run("set_slot", func(t *testing.T) {
		r := NewResolver(&ProvidersConfig{
			Model:         "prov/main",
			SmallModel:    "prov/small",
			DecisionModel: "prov/decision",
			Providers: map[string]ProviderConfig{
				"prov": {
					API: "openai",
					Models: map[string]ModelDef{
						"main":     {Name: "main"},
						"small":    {Name: "small"},
						"decision": {Name: "decision"},
					},
				},
			},
		}, nil)
		dm := r.DecisionModel()
		if dm == nil {
			t.Fatal("DecisionModel() = nil, want resolved config")
		}
		if dm.ModelID != "decision" {
			t.Fatalf("DecisionModel().ModelID = %q, want %q", dm.ModelID, "decision")
		}
		if got := r.DecisionModelName(); got != "prov/decision" {
			t.Fatalf("DecisionModelName() = %q, want %q", got, "prov/decision")
		}
	})

	t.Run("unresolvable_ref", func(t *testing.T) {
		// A ref pointing at no configured model must disable the feature
		// (nil), not panic or fall back — warn-and-disable at construction.
		r := NewResolver(&ProvidersConfig{
			Model:         "prov/main",
			DecisionModel: "prov/does-not-exist",
			Providers: map[string]ProviderConfig{
				"prov": {
					API:    "openai",
					Models: map[string]ModelDef{"main": {Name: "main"}},
				},
			},
		}, nil)
		if r.DecisionModel() != nil {
			t.Fatal("DecisionModel() != nil for unresolvable ref; want nil (feature disabled)")
		}
		if got := r.DecisionModelName(); got != "" {
			t.Fatalf("DecisionModelName() = %q, want empty for unresolvable ref", got)
		}
	})
}

// TestMergeProvidersConfig_DecisionKnobs pins the overlay semantics: a
// non-empty user models.json5 wins on every decision knob; an absent knob in
// the overlay keeps the bundled value.
func TestMergeProvidersConfig_DecisionKnobs(t *testing.T) {
	base := &ProvidersConfig{
		Model:                       "a/b",
		DecisionModel:               "bundled/decision",
		DecisionEscalationThreshold: 0.7,
		DecisionGateEnabled:         false,
		DecisionGateTools:           []string{"bundled_tool"},
	}
	overlay := &ProvidersConfig{
		DecisionModel:               "user/decision",
		DecisionEscalationThreshold: 0.85,
		DecisionGateEnabled:         true,
		DecisionGateTools:           []string{"shell", "file_delete"},
	}
	got := MergeProvidersConfig(base, overlay)
	if got.DecisionModel != "user/decision" {
		t.Fatalf("DecisionModel = %q, want %q (overlay wins)", got.DecisionModel, "user/decision")
	}
	if got.DecisionEscalationThreshold != 0.85 {
		t.Fatalf("DecisionEscalationThreshold = %v, want 0.85 (overlay wins)", got.DecisionEscalationThreshold)
	}
	if !got.DecisionGateEnabled {
		t.Fatal("DecisionGateEnabled = false, want true (overlay wins)")
	}
	if len(got.DecisionGateTools) != 2 || got.DecisionGateTools[0] != "shell" || got.DecisionGateTools[1] != "file_delete" {
		t.Fatalf("DecisionGateTools = %v, want [shell file_delete]", got.DecisionGateTools)
	}

	// Absent overlay knobs keep base values.
	got2 := MergeProvidersConfig(base, &ProvidersConfig{})
	if got2.DecisionModel != "bundled/decision" {
		t.Fatalf("DecisionModel = %q, want %q (empty overlay keeps base)", got2.DecisionModel, "bundled/decision")
	}
	if got2.DecisionEscalationThreshold != 0.7 {
		t.Fatalf("DecisionEscalationThreshold = %v, want 0.7 (empty overlay keeps base)", got2.DecisionEscalationThreshold)
	}
	if got2.DecisionGateEnabled {
		t.Fatal("DecisionGateEnabled = true, want false (empty overlay keeps base)")
	}
	if len(got2.DecisionGateTools) != 1 || got2.DecisionGateTools[0] != "bundled_tool" {
		t.Fatalf("DecisionGateTools = %v, want [bundled_tool] (empty overlay keeps base)", got2.DecisionGateTools)
	}
}
