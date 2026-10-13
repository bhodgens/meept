package vector

// B04 — balanced context selection (IndexRAG, arXiv:2603.16415, Algorithm 1 /
// Appendix B). Bridging facts are short, so in raw similarity ranking they
// dominate top-k and crowd out information-dense source memories. Selection is
// strictly POST-retrieval: retrieval ranking is untouched, and the greedy pass
// below re-admits units in ranked order while capping the bridge share at k_b.

// BridgeMetadataKey / BridgeUnitTypeValue mirror the canonical on-disk
// contract written by memory.BridgeStore (internal/memory/bridges_store.go,
// B01). The literal strings are duplicated here rather than imported because
// internal/memory imports this package — an import cycle would result.
const (
	// BridgeMetadataKey is the metadata key carrying a unit's type tag.
	BridgeMetadataKey = "unit_type"
	// BridgeUnitTypeValue marks a unit as a bridging fact.
	BridgeUnitTypeValue = "bridge"
)

// DefaultBridgeKB is the default bridge cap (paper optimum 2-3, declining at
// 5). Mirrors memory.bridges.kb in the config contract.
const DefaultBridgeKB = 2

// SelectionStats reports the outcome of one SelectBalanced pass for logging /
// metrics (B05's eval consumes these counts).
type SelectionStats struct {
	// Total is the input length.
	Total int
	// Sources is the number of source (non-bridge) units, all admitted.
	Sources int
	// BridgesIncluded is the number of bridges admitted (<= kb).
	BridgesIncluded int
	// BridgesExcluded is the number of bridges dropped by the cap.
	BridgesExcluded int
}

// isBridge reports whether a search result carries the bridge tag in its
// metadata. Units without the tag — including nil or empty metadata — are
// source units (backward compatible with all pre-bridge memories).
func isBridge(r SearchResult) bool {
	v, ok := r.Metadata[BridgeMetadataKey]
	if !ok {
		return false
	}
	s, ok := v.(string)
	return ok && s == BridgeUnitTypeValue
}

// SelectBalanced greedily caps the number of unit_type=bridge units in
// similarity-ranked results at kb. Every source unit passes; a bridge is
// admitted only while bridges_included < kb. Input order is preserved among
// the admitted units (no re-ranking). kb <= 0 resolves to DefaultBridgeKB.
//
// Selection applies regardless of whether bridge GENERATION is enabled: if
// bridge-tagged units exist in a shard they are governed; an empty store makes
// this a natural no-op.
func SelectBalanced(results []SearchResult, kb int) (filtered []SearchResult, stats SelectionStats) {
	if kb <= 0 {
		kb = DefaultBridgeKB
	}

	stats.Total = len(results)
	filtered = make([]SearchResult, 0, len(results))
	for _, r := range results {
		if isBridge(r) {
			if stats.BridgesIncluded < kb {
				stats.BridgesIncluded++
				filtered = append(filtered, r)
			} else {
				stats.BridgesExcluded++
			}
			continue
		}
		stats.Sources++
		filtered = append(filtered, r)
	}
	return filtered, stats
}
