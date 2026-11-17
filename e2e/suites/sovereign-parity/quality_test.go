//go:build e2e

// Gate leaf 00T — R1 quality dimension (T2, T3, T4; Contract C2, leaf 01)
// + the R1 e2e scenarios over the live resolver seam.
//
// This file references llm.Quality / ModelDef.Quality / ErrQualityFloor /
// llm.QualityFloorFor — undefined until leaf 01 lands, so the package's
// compile failure IS the documented RED gate for C2.
//
// Scenario drive (runtime-RED once the tree compiles): a two-provider
// world where the CHEAP model scores low on quality.code and the DEAR
// model scores high; a code-classified turn must pick the dear model
// (T2), a difficulty-5 request with only sub-floor candidates must
// surface ErrQualityFloor (T3), and models with NO quality block must
// keep the legacy pure-cost order (T4).
package sovereignparity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// buildModelsJSON serializes a parityConfig into models.json5 bytes for
// the in-process resolver drives (quality/pricegroup files).
func buildModelsJSON(t *testing.T, cfg parityConfig) []byte {
	t.Helper()
	providers := map[string]any{}
	for _, p := range cfg.providers {
		models := map[string]any{}
		for _, m := range p.models {
			entry := map[string]any{
				"name":         m.name,
				"capabilities": m.caps,
				"input_cost":   m.inCost,
				"output_cost":  m.outCost,
				"max_output":   m.maxOutput,
			}
			if m.ctxLimit > 0 {
				entry["context_limit"] = m.ctxLimit
			}
			for k, v := range m.extra {
				entry[k] = v
			}
			models[m.name] = entry
		}
		providers[p.id] = map[string]any{
			"api":     "openai",
			"options": map[string]any{"baseURL": "http://127.0.0.1:1/v1", "apiKey": p.apiKey},
			"models":  models,
		}
	}
	aliases := map[string]any{}
	for _, a := range cfg.aliases {
		entry := map[string]any{"models": a.members, "timeout": a.timeout, "max_fails": a.maxFails}
		if a.priceGroup != "" {
			entry["price_group"] = a.priceGroup
		}
		aliases[a.name] = entry
	}
	doc := map[string]any{
		"model":            "provcheap/model-cheap",
		"small_model":      "provcheap/model-cheap",
		"classifier_model": "classifier",
		"model_aliases":    aliases,
		"providers":        providers,
	}
	raw, err := jsonMarshalIndent(doc)
	if err != nil {
		t.Fatalf("marshal gate models config: %v", err)
	}
	return raw
}

// writeAndLoadGateConfig writes the serialized config to a temp file and
// loads it through the real loader (the JSON5 parse seam).
func writeAndLoadGateConfig(t *testing.T, cfg parityConfig) *llm.ProvidersConfig {
	t.Helper()
	path := filepathJoinTemp(t, "gate-models.json5")
	if err := os.WriteFile(path, buildModelsJSON(t, cfg), 0o600); err != nil {
		t.Fatalf("write gate models config: %v", err)
	}
	res, err := llm.LoadProvidersConfig(path)
	if err != nil {
		t.Fatalf("LoadProvidersConfig: %v", err)
	}
	return res
}

// jsonMarshalIndent wraps json.MarshalIndent (single seam, keeps helper
// bodies small).
func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// filepathJoinTemp returns a temp-file path for gate config writes.
func filepathJoinTemp(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

// qualityConfig builds the cheap-low/dear-high world. quality blocks ride
// the raw extra map (they round-trip pre-landing; leaf 01 gives them
// meaning).
func qualityConfig(t *testing.T) parityConfig {
	t.Helper()
	stubA := newParityStub(t)
	stubB := newParityStub(t)
	return parityConfig{
		providers: []parityProvider{
			{
				id:     "provcheap",
				stub:   stubA,
				apiKey: "key-cheap",
				models: []parityModel{
					{
						name: "model-cheap", caps: []string{"completion", "code", "reasoning", "tool_use"},
						inCost: 1.0, outCost: 1.0, ctxLimit: 32768, maxOutput: 4096,
						extra: map[string]any{
							"quality": map[string]any{
								"chat": 3, "code": 1, "reasoning": 1, "tooling": 1, "long-context": 1,
							},
							"quality_source":      "gate-fixture:cheap",
							"quality_verified_at": "2026-11-15",
						},
					},
				},
			},
			{
				id:     "provdear",
				stub:   stubB,
				apiKey: "key-dear",
				models: []parityModel{
					{
						name: "model-dear", caps: []string{"completion", "code", "reasoning", "tool_use"},
						inCost: 5.0, outCost: 5.0, ctxLimit: 32768, maxOutput: 4096,
						extra: map[string]any{
							"quality": map[string]any{
								"chat": 3, "code": 5, "reasoning": 4, "tooling": 5, "long-context": 3,
							},
							"quality_source":      "gate-fixture:dear",
							"quality_verified_at": "2026-11-15",
						},
					},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provcheap/model-cheap", "provdear/model-dear"}, timeout: 5, maxFails: 3},
		},
		chatAlias: "general",
	}
}

// TestQuality_CodePrefersHigherScore (T2): a code-classified turn picks
// the high quality.code model even though the cheap model is cheaper.
// Drive: in-process resolver over the same configs the daemon world uses,
// pinning ResolveForSkill — the exact seam leaf 01 T6/T7 modify.
func TestQuality_CodePrefersHigherScore(t *testing.T) {
	cfg := qualityConfig(t)
	_ = cfg // daemon drive below; the resolver drive asserts the pick.

	// In-process drive through the REAL resolver construction seam.
	r := llm.NewResolver(writeAndLoadGateConfig(t, qualityConfig(t)), nil)

	got, rerr := r.ResolveForSkill(&llm.SkillRequirements{Name: "gate-code", Requires: []string{"code"}}, nil)
	if rerr != nil {
		t.Fatalf("T2 R1 RED (expected): ResolveForSkill errored: %v — the quality-aware comparator does not exist on main", rerr)
	}
	if got == nil || got.ProviderID != "provdear" || got.ModelID != "model-dear" {
		t.Fatalf("T2 R1 RED (expected): code turn picked %v — want provdear/model-dear (cost order still wins; quality is not a selection axis on main)", got)
	}
}

// TestQuality_FloorBlocks (T3): difficulty 5 (floor 4 per the frozen C2
// table) with only score-2 candidates must surface ErrQualityFloor, never
// a completed selection.
func TestQuality_FloorBlocks(t *testing.T) {
	stub := newParityStub(t)
	cfg := parityConfig{
		providers: []parityProvider{
			{
				id:     "provlow",
				stub:   stub,
				apiKey: "key-low",
				models: []parityModel{
					{
						name: "model-low", caps: []string{"completion", "code", "reasoning", "tool_use"},
						inCost: 1.0, outCost: 1.0, ctxLimit: 32768, maxOutput: 4096,
						extra: map[string]any{
							"quality": map[string]any{
								"chat": 1, "code": 2, "reasoning": 2, "tooling": 2, "long-context": 2,
							},
						},
					},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provlow/model-low"}, timeout: 5, maxFails: 3},
		},
	}
	res := writeAndLoadGateConfig(t, cfg)
	r := llm.NewResolver(res, nil)
	// The floor needs a difficulty channel (FilterCtx.Difficulty); before
	// leaf 01/07 the only difficulty-bearing entry is ResolveForSkill
	// deriving from the capability+floor path via WithDifficulty. The
	// gate calls the contract function directly.
	floor := llm.QualityFloorFor(5, "code")
	if floor != 4 {
		t.Fatalf("T3 R1 RED (expected): QualityFloorFor(5, code) = %d, want 4 (frozen table) — leaf 01 not landed", floor)
	}
	_, rerr := r.ResolveForSkill(&llm.SkillRequirements{Name: "gate-floor", Requires: []string{"code"}}, nil)
	if rerr == nil || !errors.Is(rerr, llm.ErrQualityFloor) {
		t.Fatalf("T3 R1 RED (expected): sub-floor candidates served (%v); want errors.Is(err, llm.ErrQualityFloor) — the floor does not exist on main", rerr)
	}
	var asFloor *llm.QualityFloorError
	if !errors.As(rerr, &asFloor) {
		t.Fatalf("T3: floor error is not *llm.QualityFloorError: %T", rerr)
	}
}

// TestQuality_AbsentIsInert (T4): models with NO quality block keep the
// legacy pure-cost order — byte-identical selection.
func TestQuality_AbsentIsInert(t *testing.T) {
	stubA := newParityStub(t)
	stubB := newParityStub(t)
	cfg := parityConfig{
		providers: []parityProvider{
			{
				id:     "provfree",
				stub:   stubA,
				apiKey: "key-free",
				models: []parityModel{
					// NO quality key anywhere: zero Quality must leave pure
					// cost order untouched.
					{name: "model-cheapest", caps: []string{"completion", "code"}, inCost: 0.0, outCost: 0.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
			{
				id:     "provpricy",
				stub:   stubB,
				apiKey: "key-pricy",
				models: []parityModel{
					{name: "model-pricier", caps: []string{"completion", "code"}, inCost: 9.0, outCost: 9.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provfree/model-cheapest", "provpricy/model-pricier"}, timeout: 5, maxFails: 3},
		},
	}
	r := llm.NewResolver(writeAndLoadGateConfig(t, cfg), nil)

	got, rerr := r.ResolveForSkill(&llm.SkillRequirements{Name: "gate-inert", Requires: []string{"code"}}, nil)
	if rerr != nil {
		t.Fatalf("T4: quality-absent escalation errored: %v", rerr)
	}
	if got == nil || got.ModelID != "model-cheapest" {
		t.Fatalf("T4: quality-absent selection changed the cost order: picked %v; want model-cheapest (IsZero quality must be inert per C2)", got)
	}
}

// TestQualityFloorFor_FrozenTable pins the C2 difficulty->floor table
// verbatim (compile-RED until leaf 01 lands).
func TestQualityFloorFor_FrozenTable(t *testing.T) {
	cases := []struct {
		difficulty int
		capability string
		want       int
	}{
		{0, "code", 0}, {1, "code", 1}, {2, "code", 1}, {3, "code", 2},
		{4, "code", 3}, {5, "code", 4}, {5, "not-a-class", 0},
	}
	for _, c := range cases {
		if got := llm.QualityFloorFor(c.difficulty, c.capability); got != c.want {
			t.Fatalf("QualityFloorFor(%d, %q) = %d, want %d (frozen C2 table)", c.difficulty, c.capability, got, c.want)
		}
	}
}

// TestQuality_ClassShape pins QualityFor over the five frozen classes and
// the zero-value inertness contract (compile-RED until leaf 01).
func TestQuality_ClassShape(t *testing.T) {
	q := llm.Quality{Chat: 1, Code: 2, Reasoning: 3, Tooling: 4, LongContext: 5}
	if q.QualityFor(llm.QualityChat) != 1 ||
		q.QualityFor(llm.QualityCode) != 2 ||
		q.QualityFor(llm.QualityReasoning) != 3 ||
		q.QualityFor(llm.QualityTooling) != 4 ||
		q.QualityFor(llm.QualityLongContext) != 5 {
		t.Fatalf("QualityFor projections drifted: %+v", q)
	}
	if q.QualityFor("unknown-class") != 0 {
		t.Fatal("QualityFor(unknown) must return 0")
	}
	var zero llm.Quality
	if !zero.IsZero() {
		t.Fatal("zero Quality must report IsZero() (feature-inert default)")
	}
}
