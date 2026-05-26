package sync

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

func TestEdgeCodec_EncodeDecodeRoundTrip(t *testing.T) {
	codec := NewEdgeCodec()

	// Create test edges
	edges := []memory.MemoryEdge{
		{
			ID:       "edge-1",
			SourceID: "mem-source-123",
			TargetID: "mem-target-456",
			EdgeType: memory.EdgeTypeReference,
			Weight:   0.8,
		},
		{
			ID:       "edge-2",
			SourceID: "mem-other-789",
			TargetID: "mem-source-123",
			EdgeType: memory.EdgeTypeSimilar,
			Weight:   0.6,
		},
		{
			ID:       "edge-3",
			SourceID: "mem-source-123",
			TargetID: "mem-another-abc",
			EdgeType: memory.EdgeTypeTemporal,
			Weight:   0.9,
		},
	}

	memoryID := "mem-source-123"

	// Encode
	out, in := codec.EncodeEdges(memoryID, edges)

	// Should have 2 outgoing and 1 incoming
	if len(out) != 2 {
		t.Errorf("Expected 2 outgoing edges, got %d", len(out))
	}
	if len(in) != 1 {
		t.Errorf("Expected 1 incoming edge, got %d", len(in))
	}

	// Verify outgoing edges
	for _, ref := range out {
		if ref.SourceID != "" {
			t.Errorf("Outgoing edge should not have SourceID set, got %s", ref.SourceID)
		}
		if ref.TargetID == "" {
			t.Error("Outgoing edge should have TargetID set")
		}
	}

	// Verify incoming edges
	for _, ref := range in {
		if ref.TargetID != "" {
			t.Errorf("Incoming edge should not have TargetID set, got %s", ref.TargetID)
		}
		if ref.SourceID == "" {
			t.Error("Incoming edge should have SourceID set")
		}
	}

	// Decode back
	decoded := codec.DecodeEdges(memoryID, out, in)

	if len(decoded) != 3 {
		t.Errorf("Expected 3 decoded edges, got %d", len(decoded))
	}

	// Verify round-trip
	for _, edge := range decoded {
		if edge.SourceID == "" || edge.TargetID == "" {
			t.Error("Decoded edge missing SourceID or TargetID")
		}
	}
}

func TestEdgeCodec_InjectExtractMetadata(t *testing.T) {
	codec := NewEdgeCodec()

	out := []EdgeRef{
		{ID: "e1", TargetID: "t1", Type: "reference", Weight: 0.5},
	}
	in := []EdgeRef{
		{ID: "e2", SourceID: "s1", Type: "similar", Weight: 0.7},
	}

	// Inject into metadata
	metadata := codec.InjectEdgesIntoMetadata(nil, out, in)

	if metadata == nil {
		t.Fatal("Metadata should not be nil")
	}

	// Extract back
	extractedOut, extractedIn, err := codec.ExtractEdgesFromMetadata(metadata)
	if err != nil {
		t.Fatalf("Failed to extract edges: %v", err)
	}

	if len(extractedOut) != 1 {
		t.Errorf("Expected 1 outgoing edge, got %d", len(extractedOut))
	}
	if len(extractedIn) != 1 {
		t.Errorf("Expected 1 incoming edge, got %d", len(extractedIn))
	}
}

func TestEdgeCodec_BuildDistilledMetadata(t *testing.T) {
	codec := NewEdgeCodec()

	original := map[string]any{
		"category": "test",
		"agent_id": "test-agent",
	}

	out := []EdgeRef{{ID: "e1", TargetID: "t1", Type: "reference", Weight: 0.5}}
	in := []EdgeRef{}

	distilled := codec.BuildDistilledMetadata(
		original,
		out,
		in,
		0.75,
		"high PageRank importance",
		time.Now().Format(time.RFC3339),
	)

	// Check original fields preserved
	if distilled["category"] != "test" {
		t.Error("Original category not preserved")
	}
	if distilled["agent_id"] != "test-agent" {
		t.Error("Original agent_id not preserved")
	}

	// Check distillation fields added
	if !distilled[MetadataKeyDistilled].(bool) {
		t.Error("Distilled flag not set")
	}
	if distilled[MetadataKeyPageRank].(float64) != 0.75 {
		t.Error("PageRank not set correctly")
	}
	if distilled[MetadataKeyPromotionReason].(string) != "high PageRank importance" {
		t.Error("Promotion reason not set correctly")
	}

	// Check edges preserved
	if distilled[MetadataKeyEdgesOut] == nil {
		t.Error("Edges out not included")
	}
}

func TestEdgeCodec_CleanEdges(t *testing.T) {
	codec := NewEdgeCodec()

	metadata := map[string]any{
		"category":           "test",
		MetadataKeyEdgesOut:  []EdgeRef{{ID: "e1"}},
		MetadataKeyEdgesIn:   []EdgeRef{{ID: "e2"}},
		MetadataKeyDistilled: true,
	}

	cleaned := codec.CleanEdgesFromMetadata(metadata)

	if cleaned[MetadataKeyEdgesOut] != nil {
		t.Error("edges_out should be removed")
	}
	if cleaned[MetadataKeyEdgesIn] != nil {
		t.Error("edges_in should be removed")
	}
	if cleaned["category"] != "test" {
		t.Error("category should be preserved")
	}
	if cleaned[MetadataKeyDistilled] != true {
		t.Error("distilled flag should be preserved")
	}
}

func TestEdgeCodec_IsDistilled(t *testing.T) {
	codec := NewEdgeCodec()

	tests := []struct {
		name     string
		metadata map[string]any
		expected bool
	}{
		{
			name:     "nil metadata",
			metadata: nil,
			expected: false,
		},
		{
			name:     "empty metadata",
			metadata: map[string]any{},
			expected: false,
		},
		{
			name: "distilled=true",
			metadata: map[string]any{
				MetadataKeyDistilled: true,
			},
			expected: true,
		},
		{
			name: "distilled=false",
			metadata: map[string]any{
				MetadataKeyDistilled: false,
			},
			expected: false,
		},
		{
			name: "distilled=wrong type",
			metadata: map[string]any{
				MetadataKeyDistilled: "yes",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := codec.IsDistilled(tt.metadata)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestPromotionCandidate_Sorting(t *testing.T) {
	candidates := []PromotionCandidate{
		{MemoryID: "low", Score: 0.2},
		{MemoryID: "high", Score: 0.9},
		{MemoryID: "medium", Score: 0.5},
	}

	sortCandidatesByScore(candidates)

	if candidates[0].MemoryID != "high" {
		t.Errorf("Expected 'high' first, got %s", candidates[0].MemoryID)
	}
	if candidates[1].MemoryID != "medium" {
		t.Errorf("Expected 'medium' second, got %s", candidates[1].MemoryID)
	}
	if candidates[2].MemoryID != "low" {
		t.Errorf("Expected 'low' third, got %s", candidates[2].MemoryID)
	}
}

func TestHydrationResult_Fields(t *testing.T) {
	result := &HydrationResult{
		MemoriesHydrated: 5,
		EdgesRestored:    10,
		Duration:         100 * time.Millisecond,
		FromCache:        true,
	}

	if result.MemoriesHydrated != 5 {
		t.Error("MemoriesHydrated not set correctly")
	}
	if result.EdgesRestored != 10 {
		t.Error("EdgesRestored not set correctly")
	}
	if !result.FromCache {
		t.Error("FromCache not set correctly")
	}
}

func TestDistillationResult_Fields(t *testing.T) {
	result := &DistillationResult{
		MemoriesEvaluated: 100,
		MemoriesPromoted:  10,
		EdgesPreserved:    25,
		Duration:          500 * time.Millisecond,
		Failures:          []string{"mem-1", "mem-2"},
	}

	if result.MemoriesEvaluated != 100 {
		t.Error("MemoriesEvaluated not set correctly")
	}
	if result.MemoriesPromoted != 10 {
		t.Error("MemoriesPromoted not set correctly")
	}
	if len(result.Failures) != 2 {
		t.Errorf("Expected 2 failures, got %d", len(result.Failures))
	}
}

func TestSyncStatus_Fields(t *testing.T) {
	now := time.Now()
	status := SyncStatus{
		Enabled:            true,
		Mode:               "distributed",
		MemvidAvailable:    true,
		LastHydration:      &now,
		PendingRetries:     3,
		TotalHydrations:    100,
		TotalDistillations: 50,
	}

	if !status.Enabled {
		t.Error("Enabled not set correctly")
	}
	if status.Mode != "distributed" {
		t.Error("Mode not set correctly")
	}
	if status.PendingRetries != 3 {
		t.Error("PendingRetries not set correctly")
	}
}

func TestDefaultDistillationConfig(t *testing.T) {
	cfg := DefaultDistillationConfig()

	if cfg.PageRankThreshold <= 0 {
		t.Error("PageRankThreshold should be positive")
	}
	if cfg.HubConnectivityThreshold <= 0 {
		t.Error("HubConnectivityThreshold should be positive")
	}
	if !cfg.PromoteTaskCompletions {
		t.Error("PromoteTaskCompletions should be true by default")
	}
}
