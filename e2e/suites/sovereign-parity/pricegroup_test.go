//go:build e2e

// Gate leaf 00T — R9 price groups + spillway (T23, T24; Contract C10,
// leaf 09) and the R9 e2e scenarios over the live resolver seam.
//
// Compile-RED until leaf 09 declares llm.PriceGroup / ParsePriceGroup /
// AliasesByPriceGroup / ResolveSpillway (and leaf 01 the PriceGroup
// field). The signatures here follow leaf 09 (the 3-return
// ResolveSpillway), which is also what leaf 00T's consumed-symbols list
// pins — see the report note on the master.md C10 divergence.
package sovereignparity

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// priceGroupConfig: a free-tagged alias (cheap) and a paid-tagged alias
// (dearer), both healthy. T23 asserts free serves the ["free"] step and
// paid serves the ["paid"] step.
func priceGroupConfig(t *testing.T) parityConfig {
	t.Helper()
	stubA := newParityStub(t)
	stubB := newParityStub(t)
	return parityConfig{
		providers: []parityProvider{
			{
				id:     "provfree",
				stub:   stubA,
				apiKey: "key-free",
				models: []parityModel{
					{name: "model-free", caps: []string{"completion", "code"}, inCost: 0.0, outCost: 0.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
			{
				id:     "provpaid",
				stub:   stubB,
				apiKey: "key-paid",
				models: []parityModel{
					{name: "model-paid", caps: []string{"completion", "code"}, inCost: 4.0, outCost: 4.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
		},
		aliases: []parityAlias{
			{name: "free-alias", members: []string{"provfree/model-free"}, timeout: 5, maxFails: 3, priceGroup: "free"},
			{name: "paid-alias", members: []string{"provpaid/model-paid"}, timeout: 5, maxFails: 3, priceGroup: "paid"},
			// Untagged legacy alias: invisible to every spillway step.
			{name: "general", members: []string{"provfree/model-free"}, timeout: 5, maxFails: 3},
		},
	}
}

// TestPriceGroup_SpillwayTierStep (T23): spillway(["free"]) selects the
// free alias while healthy; spillway(["paid"]) selects the paid alias —
// the caller steps tiers, free first.
func TestPriceGroup_SpillwayTierStep(t *testing.T) {
	r := llm.NewResolver(writeAndLoadGateConfig(t, priceGroupConfig(t)), nil)
	fc := &llm.FilterCtx{Capability: "completion", Now: time.Now()}

	m, alias, err := r.ResolveSpillway(context.Background(), []string{"free"}, fc)
	if err != nil {
		t.Fatalf("T23 R9 RED (expected): ResolveSpillway errored: %v — leaf 09 not landed", err)
	}
	if alias != "free-alias" || m == nil || m.ModelID != "model-free" {
		t.Fatalf("T23: free step picked alias %q model %v; want free-alias/model-free", alias, m)
	}

	m2, alias2, err2 := r.ResolveSpillway(context.Background(), []string{"paid", "premium"}, fc)
	if err2 != nil {
		t.Fatalf("T23: paid step errored: %v", err2)
	}
	if alias2 != "paid-alias" || m2 == nil || m2.ModelID != "model-paid" {
		t.Fatalf("T23: paid step picked alias %q model %v; want paid-alias/model-paid", alias2, m2)
	}
}

// TestPriceGroup_AliasesByPriceGroup pins the group filter: only tagged
// aliases, untagged never participates.
func TestPriceGroup_AliasesByPriceGroup(t *testing.T) {
	if llm.ParsePriceGroup("free") != llm.PriceGroupFree ||
		llm.ParsePriceGroup("paid") != llm.PriceGroupPaid ||
		llm.ParsePriceGroup("premium") != llm.PriceGroupPremium {
		t.Fatal("ParsePriceGroup lost a frozen constant")
	}
	if llm.ParsePriceGroup("") != llm.PriceGroup("") || llm.ParsePriceGroup("garbage") != llm.PriceGroup("") {
		t.Fatal("T24: unknown/empty price_group must parse untagged (PriceGroup(\"\") = legacy)")
	}

	var entry llm.ModelAliasEntry
	entry.PriceGroup = "free"
	if entry.PriceGroup != "free" {
		t.Fatal("ModelAliasEntry.PriceGroup round-trip failed (leaf 01 schema)")
	}

	r := llm.NewResolver(writeAndLoadGateConfig(t, priceGroupConfig(t)), nil)
	free := r.AliasesByPriceGroup(llm.PriceGroupFree)
	paid := r.AliasesByPriceGroup(llm.PriceGroupPaid)
	premium := r.AliasesByPriceGroup(llm.PriceGroupPremium)
	if len(free) != 1 || free[0] != "free-alias" {
		t.Fatalf("AliasesByPriceGroup(free) = %v; want [free-alias]", free)
	}
	if len(paid) != 1 || paid[0] != "paid-alias" {
		t.Fatalf("AliasesByPriceGroup(paid) = %v; want [paid-alias]", paid)
	}
	if len(premium) != 0 {
		t.Fatalf("AliasesByPriceGroup(premium) = %v; want empty", premium)
	}
}

// TestPriceGroup_NoMutation (T24): untagged aliases keep legacy
// resolution — the spillway never reorders rotation, and an explicit
// alias resolve of the untagged alias resolves to its own member exactly
// as main does.
func TestPriceGroup_NoMutation(t *testing.T) {
	r := llm.NewResolver(writeAndLoadGateConfig(t, priceGroupConfig(t)), nil)

	got, err := r.ResolveForAlias("general", "")
	if err != nil {
		t.Fatalf("T24: untagged alias resolve errored: %v", err)
	}
	if got == nil || got.ModelID != "model-free" {
		t.Fatalf("T24: untagged alias resolved %v; want model-free (legacy behavior untouched by price groups)", got)
	}

	// Spillway steps never disturb rotation: a post-spillway resolve of
	// general still lands on model-free.
	fc := &llm.FilterCtx{Capability: "completion", Now: time.Now()}
	_, _, _ = r.ResolveSpillway(context.Background(), []string{"free"}, fc) //nolint:dogsled // RED-path call; result asserted in TestPriceGroup_SpillwayTierStep
	got2, err2 := r.ResolveForAlias("general", "")
	if err2 != nil || got2 == nil || got2.ModelID != "model-free" {
		t.Fatalf("T24: post-spillway resolve of the untagged alias changed: %v (%v)", got2, err2)
	}
}
