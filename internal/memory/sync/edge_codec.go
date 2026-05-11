package sync

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/caimlas/meept/internal/memory"
)

// MetadataKeyEdgesOut is the metadata key for outgoing edges.
const MetadataKeyEdgesOut = "edges_out"

// MetadataKeyEdgesIn is the metadata key for incoming edges.
const MetadataKeyEdgesIn = "edges_in"

// MetadataKeyDistilled marks a memory as distilled.
const MetadataKeyDistilled = "distilled"

// MetadataKeyDistilledAt is when the memory was distilled.
const MetadataKeyDistilledAt = "distilled_at"

// MetadataKeyPromotionReason is why the memory was promoted.
const MetadataKeyPromotionReason = "promotion_reason"

// MetadataKeyPageRank is the PageRank score at distillation time.
const MetadataKeyPageRank = "page_rank"

// EdgeCodec handles serialization and deserialization of graph edges
// to and from memvid metadata format.
type EdgeCodec struct{}

// NewEdgeCodec creates a new edge codec.
func NewEdgeCodec() *EdgeCodec {
	return &EdgeCodec{}
}

// EncodeEdges converts memory edges to EdgeRef slices for metadata storage.
// It separates edges into outgoing (where memoryID is the source) and
// incoming (where memoryID is the target).
func (c *EdgeCodec) EncodeEdges(memoryID string, edges []memory.MemoryEdge) (out []EdgeRef, in []EdgeRef) {
	for _, edge := range edges {
		ref := EdgeRef{
			ID:     edge.ID,
			Type:   string(edge.EdgeType),
			Weight: edge.Weight,
		}

		if edge.SourceID == memoryID {
			ref.TargetID = edge.TargetID
			out = append(out, ref)
		} else if edge.TargetID == memoryID {
			ref.SourceID = edge.SourceID
			in = append(in, ref)
		}
	}
	return out, in
}

// DecodeEdges converts EdgeRef slices from metadata back to MemoryEdge format.
// The memoryID is used to reconstruct the full edge with proper source/target.
func (c *EdgeCodec) DecodeEdges(memoryID string, out []EdgeRef, in []EdgeRef) []memory.MemoryEdge {
	edges := make([]memory.MemoryEdge, 0, len(out)+len(in))

	for _, ref := range out {
		edges = append(edges, memory.MemoryEdge{
			ID:       ref.ID,
			SourceID: memoryID,
			TargetID: ref.TargetID,
			EdgeType: memory.EdgeType(ref.Type),
			Weight:   ref.Weight,
		})
	}

	for _, ref := range in {
		edges = append(edges, memory.MemoryEdge{
			ID:       ref.ID,
			SourceID: ref.SourceID,
			TargetID: memoryID,
			EdgeType: memory.EdgeType(ref.Type),
			Weight:   ref.Weight,
		})
	}

	return edges
}

// InjectEdgesIntoMetadata adds edge references to memory metadata.
// Creates new metadata map if nil.
func (c *EdgeCodec) InjectEdgesIntoMetadata(metadata map[string]any, out []EdgeRef, in []EdgeRef) map[string]any {
	if metadata == nil {
		metadata = make(map[string]any)
	}

	if len(out) > 0 {
		metadata[MetadataKeyEdgesOut] = out
	}
	if len(in) > 0 {
		metadata[MetadataKeyEdgesIn] = in
	}

	return metadata
}

// ExtractEdgesFromMetadata retrieves edge references from memory metadata.
// Returns empty slices if no edges are present.
func (c *EdgeCodec) ExtractEdgesFromMetadata(metadata map[string]any) (out []EdgeRef, in []EdgeRef, err error) {
	if metadata == nil {
		return nil, nil, nil
	}

	// Extract outgoing edges
	if outRaw, ok := metadata[MetadataKeyEdgesOut]; ok {
		out, err = c.parseEdgeRefs(outRaw)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse edges_out: %w", err)
		}
	}

	// Extract incoming edges
	if inRaw, ok := metadata[MetadataKeyEdgesIn]; ok {
		in, err = c.parseEdgeRefs(inRaw)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse edges_in: %w", err)
		}
	}

	return out, in, nil
}

// parseEdgeRefs converts a raw metadata value to EdgeRef slice.
// Handles both []EdgeRef (direct) and []any (from JSON unmarshal).
func (c *EdgeCodec) parseEdgeRefs(raw any) ([]EdgeRef, error) {
	// Direct type assertion
	if refs, ok := raw.([]EdgeRef); ok {
		return refs, nil
	}

	// Handle JSON-unmarshaled data (comes as []any or []interface{})
	if arr, ok := raw.([]any); ok {
		refs := make([]EdgeRef, 0, len(arr))
		for _, item := range arr {
			ref, err := c.parseEdgeRef(item)
			if err != nil {
				return nil, err
			}
			refs = append(refs, ref)
		}
		return refs, nil
	}

	// Try JSON re-marshal/unmarshal as fallback
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal edge refs: %w", err)
	}

	var refs []EdgeRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return nil, fmt.Errorf("cannot unmarshal edge refs: %w", err)
	}

	return refs, nil
}

// parseEdgeRef converts a single raw value to EdgeRef.
func (c *EdgeCodec) parseEdgeRef(raw any) (EdgeRef, error) {
	// Handle map[string]any (from JSON unmarshal)
	if m, ok := raw.(map[string]any); ok {
		ref := EdgeRef{}
		if v, ok := m["id"].(string); ok {
			ref.ID = v
		}
		if v, ok := m["target_id"].(string); ok {
			ref.TargetID = v
		}
		if v, ok := m["source_id"].(string); ok {
			ref.SourceID = v
		}
		if v, ok := m["type"].(string); ok {
			ref.Type = v
		}
		if v, ok := m["weight"].(float64); ok {
			ref.Weight = v
		}
		return ref, nil
	}

	// Try JSON re-marshal/unmarshal
	data, err := json.Marshal(raw)
	if err != nil {
		return EdgeRef{}, fmt.Errorf("cannot marshal edge ref: %w", err)
	}

	var ref EdgeRef
	if err := json.Unmarshal(data, &ref); err != nil {
		return EdgeRef{}, fmt.Errorf("cannot unmarshal edge ref: %w", err)
	}

	return ref, nil
}

// CleanEdgesFromMetadata removes edge references from metadata.
// Useful when storing locally where edges are in the graph DB.
func (c *EdgeCodec) CleanEdgesFromMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}

	// Create a copy to avoid mutating original
	clean := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if k != MetadataKeyEdgesOut && k != MetadataKeyEdgesIn {
			clean[k] = v
		}
	}

	return clean
}

// BuildDistilledMetadata creates complete metadata for a distilled memory.
func (c *EdgeCodec) BuildDistilledMetadata(
	original map[string]any,
	out []EdgeRef,
	in []EdgeRef,
	pageRank float64,
	reason string,
	distilledAt string,
) map[string]any {
	metadata := make(map[string]any)

	// Copy original metadata
	maps.Copy(metadata, original)

	// Add distillation metadata
	metadata[MetadataKeyDistilled] = true
	metadata[MetadataKeyDistilledAt] = distilledAt
	metadata[MetadataKeyPageRank] = pageRank
	metadata[MetadataKeyPromotionReason] = reason

	// Add edges
	if len(out) > 0 {
		metadata[MetadataKeyEdgesOut] = out
	}
	if len(in) > 0 {
		metadata[MetadataKeyEdgesIn] = in
	}

	return metadata
}

// IsDistilled checks if a memory's metadata indicates it was distilled.
func (c *EdgeCodec) IsDistilled(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	if v, ok := metadata[MetadataKeyDistilled].(bool); ok {
		return v
	}
	return false
}
