package llm

import (
	"testing"
)

// storedCacheResponse builds a shared cache entry with one mutable slice
// (ToolCalls, cap > len so an append writes the shared backing array) and
// one mutable map (Metadata). bughunt 2026-11-24 P4: cacheHitResponse was a
// shallow copy, so a consumer that appended to or mutated a cache-hit
// response corrupted the STORED entry for every later hit.
func storedCacheResponse() *Response {
	toolCalls := make([]ToolCall, 1, 4) // cap 4: three slots shared with the copy
	toolCalls[0] = ToolCall{
		ID:       "call-1",
		Type:     "function",
		Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`},
	}
	metadata := map[string]any{
		"llm_tier":         float64(1),
		"llm_tier_serving": "medium",
	}
	return &Response{
		Content:      "stored answer",
		ToolCalls:    toolCalls,
		Usage:        TokenUsage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18},
		Model:        "model-under-test",
		FinishReason: "stop",
		CostUSD:      0.25,
		Metadata:     metadata,
	}
}

// TestCacheHitResponseDoesNotShareToolCallBackingArray pins that an append
// to a cache-hit response's ToolCalls cannot write through to the stored
// entry. The stored slice is built with cap 4 and len 1, so the copy's first
// append lands in the SHARED slots and the stored entry's element changes.
func TestCacheHitResponseDoesNotShareToolCallBackingArray(t *testing.T) {
	stored := storedCacheResponse()

	first := cacheHitResponse(stored)
	if first == stored {
		t.Fatal("cacheHitResponse returned the stored pointer itself")
	}

	// Mutate only the copy: append a second call, then rewrite element 0.
	first.ToolCalls = append(first.ToolCalls, ToolCall{ID: "call-extra"})
	first.ToolCalls[0] = ToolCall{ID: "call-mutated", Function: ToolCallFunction{Name: "mutated"}}

	if got := len(stored.ToolCalls); got != 1 {
		t.Errorf("stored ToolCalls grew to %d; the cache-hit copy shares the backing array: %v",
			got, stored.ToolCalls)
	}
	if got := stored.ToolCalls[0].ID; got != "call-1" {
		t.Errorf("stored ToolCalls[0].ID = %q, want %q (the copy overwrote the stored element)",
			got, "call-1")
	}
	if got := first.ToolCalls[0].ID; got != "call-mutated" {
		t.Errorf("copy ToolCalls[0].ID = %q, want %q (the copy must own its elements)",
			got, "call-mutated")
	}
}

// TestCacheHitResponseDoesNotShareMetadataMap pins that a write to a
// cache-hit response's Metadata map cannot write through to the stored entry.
func TestCacheHitResponseDoesNotShareMetadataMap(t *testing.T) {
	stored := storedCacheResponse()

	first := cacheHitResponse(stored)
	first.Metadata["llm_tier"] = float64(9)
	delete(first.Metadata, "llm_tier_serving")

	if got := stored.Metadata["llm_tier"]; got != float64(1) {
		t.Errorf("stored Metadata[llm_tier] = %v, want %v (the copy shares the stored map)",
			got, float64(1))
	}
	if _, ok := stored.Metadata["llm_tier_serving"]; !ok {
		t.Error("stored Metadata lost llm_tier_serving; the copy deleted through the shared map")
	}
	if got := first.Metadata["llm_tier"]; got != float64(9) {
		t.Errorf("copy Metadata[llm_tier] = %v, want 9", got)
	}
}

// TestCacheHitResponseTwoCopiesAreIndependent proves two sequential cache
// hits of the same entry do not see each other's mutations. This is the
// production shape: the cache is read by every later call.
func TestCacheHitResponseTwoCopiesAreIndependent(t *testing.T) {
	stored := storedCacheResponse()

	first := cacheHitResponse(stored)
	second := cacheHitResponse(stored)

	first.ToolCalls[0].Function.Name = "first-writer"
	first.Metadata["writer"] = "first"

	if got := second.ToolCalls[0].Function.Name; got != "read_file" {
		t.Errorf("second copy ToolCalls[0].Function.Name = %q, want %q (copies alias each other)",
			got, "read_file")
	}
	if _, ok := second.Metadata["writer"]; ok {
		t.Error("second copy sees first copy's Metadata write; the copies share one map")
	}
}

// TestCacheHitResponseKeepsScalarSemantics pins the existing contract the
// shallow copy already satisfied: CostUSD zeroed, tokens and text intact,
// nil passes through nil, and a nil slice/map stays nil (callers may assert
// nil, so the deep copy must not allocate empty where nil was).
func TestCacheHitResponseKeepsScalarSemantics(t *testing.T) {
	if got := cacheHitResponse(nil); got != nil {
		t.Errorf("cacheHitResponse(nil) = %v, want nil", got)
	}

	stored := &Response{
		Content: "plain",
		Model:   "m",
		CostUSD: 1.5,
		Usage:   TokenUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
	}
	cp := cacheHitResponse(stored)
	if cp.CostUSD != 0 {
		t.Errorf("copy CostUSD = %v, want 0 (cache hits must not re-charge the budget)", cp.CostUSD)
	}
	if stored.CostUSD != 1.5 {
		t.Errorf("stored CostUSD = %v, want 1.5 (the stored entry keeps its own cost)", stored.CostUSD)
	}
	if cp.Content != "plain" || cp.Usage.TotalTokens != 7 || cp.Model != "m" {
		t.Errorf("scalar fields changed: %+v", cp)
	}
	if cp.ToolCalls != nil {
		t.Errorf("nil ToolCalls became %+v; the copy must preserve nil", cp.ToolCalls)
	}
	if cp.Metadata != nil {
		t.Errorf("nil Metadata became %+v; the copy must preserve nil", cp.Metadata)
	}
}
