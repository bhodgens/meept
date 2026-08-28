package llm

import (
	"testing"
)

// providersConfigForSchemaMode builds a minimal ProvidersConfig with one
// provider and one model, each optionally carrying a schema_mode override.
func providersConfigForSchemaMode(providerMode, modelMode string) *ProvidersConfig {
	return &ProvidersConfig{
		Model: "prov/alpha",
		Providers: map[string]ProviderConfig{
			"prov": {
				API:     "openai",
				Options: ProviderOptionsConfig{BaseURL: "http://localhost", SchemaMode: providerMode},
				Models: map[string]ModelDef{
					"alpha": {Name: "alpha-model", Capabilities: []string{CapCompletion}, SchemaMode: modelMode},
					"beta":  {Name: "beta-model", Capabilities: []string{CapCompletion}},
				},
			},
		},
	}
}

// TestModelConfigFrom_SchemaModePlumbing mirrors the ToolConstraint plumbing
// test: the resolved model-level value lands on ModelConfig.SchemaMode after
// being resolved per-model-over-provider (leaf 02).
func TestModelConfigFrom_SchemaModePlumbing(t *testing.T) {
	cfg := providersConfigForSchemaMode("indexed", "full")

	got := GetAllModels(cfg)
	var alpha, beta *ModelConfig
	for _, m := range got {
		switch m.CatalogRef {
		case "prov/alpha":
			alpha = m
		case "prov/beta":
			beta = m
		}
	}
	if alpha == nil || beta == nil {
		t.Fatalf("expected both models resolved, got %d", len(got))
	}
	if alpha.SchemaMode != "full" {
		t.Fatalf("alpha.SchemaMode = %q, want %q (model beats provider)", alpha.SchemaMode, "full")
	}
	if beta.SchemaMode != "indexed" {
		t.Fatalf("beta.SchemaMode = %q, want %q (inherits provider)", beta.SchemaMode, "indexed")
	}
}

// TestModelConfigFrom_SchemaModeUnknownWarnedAndIgnored verifies an unknown
// schema_mode string resolves to "" (fall-through to global), mirroring the
// tool_constraint unknown-mode warn+ignore behavior.
func TestModelConfigFrom_SchemaModeUnknownWarnedAndIgnored(t *testing.T) {
	cfg := providersConfigForSchemaMode("lazy", "AUTO")
	models := GetAllModels(cfg)
	for _, m := range models {
		if m.SchemaMode != "" {
			t.Fatalf("%s.SchemaMode = %q, want \"\" for unknown mode", m.CatalogRef, m.SchemaMode)
		}
	}
}

// TestEffectiveSchemaMode_ResolutionOrder covers the full precedence chain:
// resolved per-model SchemaMode > provider block > global mode > "indexed".
func TestEffectiveSchemaMode_ResolutionOrder(t *testing.T) {
	// 1. model beats provider and global
	r := NewResolver(providersConfigForSchemaMode("indexed", "full"), nil)
	if got := r.EffectiveSchemaMode("prov", "alpha", "full"); got != "full" {
		t.Fatalf("model override: got %q, want %q", got, "full")
	}

	// 2. provider beats global
	r = NewResolver(providersConfigForSchemaMode("indexed", ""), nil)
	if got := r.EffectiveSchemaMode("prov", "beta", "full"); got != "indexed" {
		t.Fatalf("provider override: got %q, want %q", got, "indexed")
	}

	// 3. all unset -> global "full"
	r = NewResolver(providersConfigForSchemaMode("", ""), nil)
	if got := r.EffectiveSchemaMode("prov", "beta", "full"); got != "full" {
		t.Fatalf("global fallback: got %q, want %q", got, "full")
	}

	// 4. everything unset -> "indexed" default
	if got := r.EffectiveSchemaMode("prov", "beta", ""); got != "indexed" {
		t.Fatalf("default fallback: got %q, want %q", got, "indexed")
	}
}

// TestEffectiveSchemaMode_UnknownModel verifies lookup of an unknown model or
// provider still resolves through the provider block / global path instead of
// erroring or returning empty.
func TestEffectiveSchemaMode_UnknownModel(t *testing.T) {
	r := NewResolver(providersConfigForSchemaMode("full", ""), nil)

	// Unknown model on a known provider -> provider block wins.
	if got := r.EffectiveSchemaMode("prov", "ghost", "indexed"); got != "full" {
		t.Fatalf("unknown model: got %q, want %q (provider path)", got, "full")
	}

	// Unknown provider entirely -> global path.
	if got := r.EffectiveSchemaMode("nowhere", "ghost", "indexed"); got != "indexed" {
		t.Fatalf("unknown provider: got %q, want %q (global path)", got, "indexed")
	}
}
