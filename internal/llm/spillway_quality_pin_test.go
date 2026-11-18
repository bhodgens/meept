package llm

import (
	"context"
	"log/slog"
	"testing"
)

// The R9 spillway tiebreak uses capability-specific quality, not the sum
// across classes. Two equal-cost aliases: one chat-heavy (high total, low
// code), one code-specialized (lower total, high code). A code turn must
// pick the code-specialized alias.
func TestResolveSpillway_CapabilitySpecificQuality(t *testing.T) {
	r := NewResolver(createTestConfig(), slog.New(slog.DiscardHandler))
	chatHeavy := &ModelConfig{
		ProviderID: "zai", ModelID: "chat-heavy",
		Quality: Quality{Chat: 100, Code: 1, Reasoning: 1, Tooling: 1, LongContext: 1},
	}
	codeSpec := &ModelConfig{
		ProviderID: "zai", ModelID: "code-spec",
		Quality: Quality{Chat: 1, Code: 10, Reasoning: 1, Tooling: 1, LongContext: 1},
	}
	r.aliases["chat-tier"] = &AliasEntry{Models: []*ModelConfig{chatHeavy}, PriceGroup: "paid"}
	r.aliases["code-tier"] = &AliasEntry{Models: []*ModelConfig{codeSpec}, PriceGroup: "paid"}

	// Equal cost: force the quality tiebreak by pinning TotalCost inputs.
	chatHeavy.CostPerMillionInput, codeSpec.CostPerMillionInput = 0, 0
	chatHeavy.CostPerMillionOutput, codeSpec.CostPerMillionOutput = 0, 0

	_, name, err := r.ResolveSpillway(context.Background(), []string{"paid"},
		&FilterCtx{Capability: "code"})
	if err != nil {
		t.Fatalf("ResolveSpillway: %v", err)
	}
	if name != "code-tier" {
		t.Fatalf("code turn spillway = %q, want code-tier (capability-specific quality)", name)
	}
}
