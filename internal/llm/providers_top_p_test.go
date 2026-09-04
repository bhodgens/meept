package llm

import (
	"testing"
)

// TestModelDefTopPFlowsToModelConfig pins the models.json5 top_p contract:
// the field is documented (docs/reference/models.md, docs/configuration/llm.md)
// and user configs set it, so it must reach ModelConfig.TopP (the value
// buildChatRequest sends on the wire). Regression for the dormant-field bug
// where ModelDef lacked the field entirely and user top_p values were
// silently dropped.
func TestModelDefTopPFlowsToModelConfig(t *testing.T) {
	provider := ProviderConfig{
		API:     "openai",
		Options: ProviderOptionsConfig{BaseURL: "https://example.test/v1"},
		Models: map[string]ModelDef{
			"m1": {Name: "m1", TopP: 0.9},
			"m2": {Name: "m2"}, // absent → zero (omitempty keeps it off the wire)
		},
	}
	mc := modelConfigFrom("prov", "m1", provider, provider.Models["m1"])
	if mc.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9 (models.json5 top_p must not be dropped)", mc.TopP)
	}
	mc2 := modelConfigFrom("prov", "m2", provider, provider.Models["m2"])
	if mc2.TopP != 0 {
		t.Errorf("TopP = %v, want 0 when unset", mc2.TopP)
	}
}

// TestCloneProviderConfigDeepCopiesExtraHeaders pins the aliasing guard:
// cloneProviderConfig must deep-copy Options.ExtraHeaders and per-model
// ExtraHeaders so an in-place write through a clone never mutates the
// original provider config.
func TestCloneProviderConfigDeepCopiesExtraHeaders(t *testing.T) {
	src := ProviderConfig{
		API: "openai",
		Options: ProviderOptionsConfig{
			BaseURL:      "https://example.test/v1",
			ExtraHeaders: map[string]string{"X-A": "1"},
		},
		Models: map[string]ModelDef{
			"m1": {Name: "m1", ExtraHeaders: map[string]string{"X-B": "2"}},
		},
	}
	clone := cloneProviderConfig(src)

	clone.Options.ExtraHeaders["X-A"] = "mutated"
	if src.Options.ExtraHeaders["X-A"] != "1" {
		t.Fatalf("clone mutated source Options.ExtraHeaders: %v", src.Options.ExtraHeaders)
	}

	m := clone.Models["m1"]
	m.ExtraHeaders["X-B"] = "mutated"
	if src.Models["m1"].ExtraHeaders["X-B"] != "2" {
		t.Fatalf("clone mutated source ModelDef.ExtraHeaders: %v", src.Models["m1"].ExtraHeaders)
	}

	// Nil maps stay nil-safe through the clone.
	empty := cloneProviderConfig(ProviderConfig{API: "openai"})
	if empty.Options.ExtraHeaders != nil {
		t.Errorf("nil Options.ExtraHeaders became %v", empty.Options.ExtraHeaders)
	}
}
