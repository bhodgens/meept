package memory

// B04 — balanced context selection tests (plans/context-reduction/09-bridges/
// 04-balanced-recall.md). SelectBalanced runs AFTER retrieval (post
// ConsolidateResults) and greedily caps metadata unit_type=bridge units at k_b
// while every source unit passes through. Order is preserved; untyped units
// (nil/empty metadata) are never capped (backward compatible).

import (
	"fmt"
	"testing"

	"github.com/caimlas/meept/internal/memory/vector"
)

// bridgeResult builds a SearchResult tagged as a bridging fact, mirroring the
// metadata B01's BridgeStore writes (unit_type=bridge).
func bridgeResult(id string, score float64) vector.SearchResult {
	return vector.SearchResult{
		MemoryID:       id,
		Content:        "bridge: " + id,
		Metadata:       map[string]any{"unit_type": "bridge"},
		RelevanceScore: score,
	}
}

// sourceResult builds an untyped source unit (AKU) — the shape of every
// pre-B01 memory row.
func sourceResult(id string, score float64) vector.SearchResult {
	return vector.SearchResult{
		MemoryID:       id,
		Content:        "source: " + id,
		Metadata:       nil,
		RelevanceScore: score,
	}
}

// rankedBridgesThenSources returns n bridges ranked above m source units in
// descending similarity order.
func rankedBridgesThenSources(n, m int) []vector.SearchResult {
	results := make([]vector.SearchResult, 0, n+m)
	score := float64(n + m)
	for i := 0; i < n; i++ {
		results = append(results, bridgeResult(fmt.Sprintf("bridge-%d", i), score))
		score--
	}
	for i := 0; i < m; i++ {
		results = append(results, sourceResult(fmt.Sprintf("source-%d", i), score))
		score--
	}
	return results
}

// TestBalancedSelectionCapsBridges: 5 bridges ranked above 5 source units with
// k_b=2 → result is 2 bridges + all 5 source units, original similarity order
// preserved among the admitted units.
func TestBalancedSelectionCapsBridges(t *testing.T) {
	results := rankedBridgesThenSources(5, 5)

	filtered, stats := vector.SelectBalanced(results, 2)

	if len(filtered) != 7 {
		t.Fatalf("len(filtered) = %d, want 7 (2 bridges + 5 sources)", len(filtered))
	}
	wantOrder := []string{"bridge-0", "bridge-1", "source-0", "source-1", "source-2", "source-3", "source-4"}
	for i, want := range wantOrder {
		if filtered[i].MemoryID != want {
			t.Errorf("filtered[%d] = %q, want %q (order must be preserved)", i, filtered[i].MemoryID, want)
		}
	}
	if stats.BridgesIncluded != 2 {
		t.Errorf("stats.BridgesIncluded = %d, want 2", stats.BridgesIncluded)
	}
	if stats.BridgesExcluded != 3 {
		t.Errorf("stats.BridgesExcluded = %d, want 3", stats.BridgesExcluded)
	}
	if stats.Sources != 5 {
		t.Errorf("stats.Sources = %d, want 5", stats.Sources)
	}
	if stats.Total != 10 {
		t.Errorf("stats.Total = %d, want 10", stats.Total)
	}
}

// TestBalancedSelectionCapLargerThanBridges: k_b above the bridge count admits
// everything — nothing excluded.
func TestBalancedSelectionCapLargerThanBridges(t *testing.T) {
	results := rankedBridgesThenSources(2, 3)

	filtered, stats := vector.SelectBalanced(results, 5)

	if len(filtered) != len(results) {
		t.Fatalf("len(filtered) = %d, want %d", len(filtered), len(results))
	}
	if stats.BridgesIncluded != 2 || stats.BridgesExcluded != 0 {
		t.Errorf("stats = included %d / excluded %d, want 2 / 0", stats.BridgesIncluded, stats.BridgesExcluded)
	}
}

// TestBalancedSelectionDefaultCapOnNonPositiveKB: k_b <= 0 falls back to
// DefaultBridgeKB (2) — the config default can never disable the cap by accident.
func TestBalancedSelectionDefaultCapOnNonPositiveKB(t *testing.T) {
	results := rankedBridgesThenSources(4, 1)

	filtered, stats := vector.SelectBalanced(results, 0)

	if len(filtered) != 3 {
		t.Fatalf("len(filtered) = %d, want 3 (default k_b=2 + 1 source)", len(filtered))
	}
	if stats.BridgesIncluded != 2 || stats.BridgesExcluded != 2 {
		t.Errorf("stats = included %d / excluded %d, want 2 / 2", stats.BridgesIncluded, stats.BridgesExcluded)
	}
}

// TestBalancedSelectionUntypedAreSource: units with zero metadata (and units
// with metadata lacking the bridge tag) are source units — nothing is capped.
func TestBalancedSelectionUntypedAreSource(t *testing.T) {
	results := []vector.SearchResult{
		sourceResult("a", 5),                                          // nil metadata
		{MemoryID: "b", Metadata: map[string]any{}},                   // empty metadata
		{MemoryID: "c", Metadata: map[string]any{"unit_type": "aku"}}, // typed non-bridge
		sourceResult("d", 2),
	}

	filtered, stats := vector.SelectBalanced(results, 0)

	if len(filtered) != len(results) {
		t.Fatalf("len(filtered) = %d, want %d — untyped units must never be capped", len(filtered), len(results))
	}
	if stats.Sources != 4 || stats.BridgesIncluded != 0 || stats.BridgesExcluded != 0 {
		t.Errorf("stats = %+v, want sources 4 / included 0 / excluded 0", stats)
	}
	for i, want := range []string{"a", "b", "c", "d"} {
		if filtered[i].MemoryID != want {
			t.Errorf("filtered[%d] = %q, want %q", i, filtered[i].MemoryID, want)
		}
	}
}

// TestBalancedSelectionZeroBridgesPassthrough: no bridges → the returned list
// is identical to the input (length, order, contents).
func TestBalancedSelectionZeroBridgesPassthrough(t *testing.T) {
	results := rankedBridgesThenSources(0, 5)

	filtered, stats := vector.SelectBalanced(results, 2)

	if len(filtered) != len(results) {
		t.Fatalf("len(filtered) = %d, want %d", len(filtered), len(results))
	}
	for i := range results {
		if filtered[i].MemoryID != results[i].MemoryID ||
			filtered[i].Content != results[i].Content ||
			filtered[i].RelevanceScore != results[i].RelevanceScore ||
			filtered[i].VectorSimilarity != results[i].VectorSimilarity {
			t.Errorf("filtered[%d] = %+v, want %+v — passthrough must be identical", i, filtered[i], results[i])
		}
	}
	if stats.BridgesIncluded != 0 || stats.BridgesExcluded != 0 || stats.Sources != 5 {
		t.Errorf("stats = %+v, want included 0 / excluded 0 / sources 5", stats)
	}
}

// TestBalancedSelectionBridgesBelowSources: bridges ranked after sources are
// admitted in rank order up to k_b — cap counts admission order, not score.
func TestBalancedSelectionBridgesBelowSources(t *testing.T) {
	results := rankedBridgesThenSources(3, 2) // bridges first
	// Flip: sources outrank bridges.
	flipped := []vector.SearchResult{results[3], results[0], results[4], results[1], results[2]}

	filtered, stats := vector.SelectBalanced(flipped, 2)

	wantOrder := []string{"source-0", "bridge-0", "source-1", "bridge-1"}
	if len(filtered) != len(wantOrder) {
		t.Fatalf("len(filtered) = %d, want %d", len(filtered), len(wantOrder))
	}
	for i, want := range wantOrder {
		if filtered[i].MemoryID != want {
			t.Errorf("filtered[%d] = %q, want %q", i, filtered[i].MemoryID, want)
		}
	}
	if stats.BridgesIncluded != 2 || stats.BridgesExcluded != 1 {
		t.Errorf("stats = included %d / excluded %d, want 2 / 1", stats.BridgesIncluded, stats.BridgesExcluded)
	}
}

// TestSearcherAppliesBalancedSelection: the recall-path searcher resolves its
// k_b (default 2, option/setter override) and applies SelectBalanced to
// post-ConsolidateResults output before prompt injection.
func TestSearcherAppliesBalancedSelection(t *testing.T) {
	results := rankedBridgesThenSources(5, 5)

	// Default: k_b resolves to DefaultBridgeKB (2).
	s := NewShardManagerVectorSearcher(nil, nil)
	filtered, stats := s.selectBalanced(results)
	if len(filtered) != 7 || stats.BridgesIncluded != 2 || stats.BridgesExcluded != 3 {
		t.Fatalf("default cap: len=%d stats=%+v, want len 7 / included 2 / excluded 3", len(filtered), stats)
	}

	// Option override.
	s3 := NewShardManagerVectorSearcher(nil, nil, WithBridgeCap(3))
	filtered, stats = s3.selectBalanced(results)
	if len(filtered) != 8 || stats.BridgesIncluded != 3 {
		t.Fatalf("cap 3: len=%d stats=%+v, want len 8 / included 3", len(filtered), stats)
	}

	// Runtime setter (nil-receiver safe per repo rule).
	var nilSearcher *ShardManagerVectorSearcher
	nilSearcher.SetBridgeCap(1) // must not panic
	s3.SetBridgeCap(1)
	filtered, stats = s3.selectBalanced(results)
	if len(filtered) != 6 || stats.BridgesIncluded != 1 {
		t.Fatalf("cap 1: len=%d stats=%+v, want len 6 / included 1", len(filtered), stats)
	}
}
