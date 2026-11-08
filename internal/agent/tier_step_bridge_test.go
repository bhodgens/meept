package agent

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TIER-1 (llm-serving-tiers): the internal/llm tier metadata keys
// (llm_tier / llm_tier_degraded / llm_tier_depth, stamped by the
// ProviderManager) must reach the agent-loop step surface
// (AfterProviderResponseData -> llm.tokens.used bus payload) with the key
// names PINNED to the internal/llm constants.

func TestTierMetadataFromResponse_BridgesKeys(t *testing.T) {
	resp := &llm.Response{
		Metadata: map[string]any{
			llm.MetaKeyLLMTier:         "ollama/llama3:8b",
			llm.MetaKeyLLMTierDegraded: true,
			llm.MetaKeyLLMTierDepth:    2,
		},
	}
	tier, degraded, depth := tierMetadataFromResponse(resp)
	if tier != "ollama/llama3:8b" {
		t.Errorf("llm_tier = %q, want ollama/llama3:8b", tier)
	}
	if !degraded {
		t.Error("llm_tier_degraded = false, want true")
	}
	if depth != 2 {
		t.Errorf("llm_tier_depth = %d, want 2", depth)
	}
}

func TestTierMetadataFromResponse_NoMetadataIsZeroTriple(t *testing.T) {
	// Feature-off posture: nil metadata -> zero triple, no panic.
	tier, degraded, depth := tierMetadataFromResponse(&llm.Response{})
	if tier != "" || degraded || depth != 0 {
		t.Errorf("nil metadata triple = (%q, %v, %d), want zero", tier, degraded, depth)
	}
	if _, degraded, _ := tierMetadataFromResponse(nil); degraded {
		t.Error("nil response must not report degraded")
	}
}

// The event payload carries the pinned key names: serializing
// AfterProviderResponseData must produce exactly llm_tier /
// llm_tier_degraded / llm_tier_depth (matching llm.MetaKey*).
func TestAfterProviderResponseData_TierKeyNamesPinned(t *testing.T) {
	data := AfterProviderResponseData{
		ModelID:         "m",
		LLMTier:         "prov/model-x",
		LLMTierDegraded: true,
		LLMTierDepth:    1,
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{llm.MetaKeyLLMTier, llm.MetaKeyLLMTierDegraded, llm.MetaKeyLLMTierDepth} {
		if _, ok := got[key]; !ok {
			t.Errorf("payload missing pinned key %q: %s", key, raw)
		}
	}
	// Values survive the wire shape.
	if got[llm.MetaKeyLLMTier] != "prov/model-x" {
		t.Errorf("llm_tier = %v, want prov/model-x", got[llm.MetaKeyLLMTier])
	}
	if degraded, ok := got[llm.MetaKeyLLMTierDegraded].(bool); !ok || !degraded {
		t.Errorf("llm_tier_degraded = %v, want true", got[llm.MetaKeyLLMTierDegraded])
	}
	if depth, ok := got[llm.MetaKeyLLMTierDepth].(float64); !ok || int(depth) != 1 {
		t.Errorf("llm_tier_depth = %v, want 1", got[llm.MetaKeyLLMTierDepth])
	}
}

// Feature-off: omitempty drops llm_tier from the payload so disabled-config
// consumers see no tier keys at all.
func TestAfterProviderResponseData_TierKeysAbsentWhenOff(t *testing.T) {
	raw, err := json.Marshal(AfterProviderResponseData{ModelID: "m"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := got[llm.MetaKeyLLMTier]; ok {
		t.Errorf("llm_tier present on a feature-off payload: %s", raw)
	}
}
