package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKnowledgeGraph_Initialize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Should be idempotent
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("second Initialize failed: %v", err)
	}
}

func TestKnowledgeGraph_AddAndGetEdges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Add edges
	edge1 := MemoryEdge{
		SourceID: "mem-001-aaaa",
		TargetID: "mem-002-bbbb",
		EdgeType: EdgeTypeReference,
		Weight:   0.8,
	}
	edge2 := MemoryEdge{
		SourceID: "mem-002-bbbb",
		TargetID: "mem-003-cccc",
		EdgeType: EdgeTypeSimilar,
		Weight:   0.6,
	}

	if err := g.AddEdge(ctx, edge1); err != nil {
		t.Fatalf("AddEdge failed: %v", err)
	}
	if err := g.AddEdge(ctx, edge2); err != nil {
		t.Fatalf("AddEdge failed: %v", err)
	}

	// Get edges for mem-002
	edges, err := g.GetEdges(ctx, "mem-002-bbbb")
	if err != nil {
		t.Fatalf("GetEdges failed: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(edges))
	}
}

func TestKnowledgeGraph_AddEdgesBatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	edges := []MemoryEdge{
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 0.9},
		{SourceID: "mem-002-bbbb", TargetID: "mem-003-cccc", EdgeType: EdgeTypeSimilar, Weight: 0.7},
		{SourceID: "mem-003-cccc", TargetID: "mem-004-dddd", EdgeType: EdgeTypeTemporal, Weight: 0.5},
	}

	if err := g.AddEdges(ctx, edges); err != nil {
		t.Fatalf("AddEdges failed: %v", err)
	}

	// Verify
	stats, err := g.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.EdgeCount != 3 {
		t.Errorf("expected 3 edges, got %d", stats.EdgeCount)
	}
	if stats.NodeCount != 4 {
		t.Errorf("expected 4 nodes, got %d", stats.NodeCount)
	}
}

func TestKnowledgeGraph_GetRelatedMemoryIDs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Create a small graph
	edges := []MemoryEdge{
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 0.9},
		{SourceID: "mem-001-aaaa", TargetID: "mem-003-cccc", EdgeType: EdgeTypeSimilar, Weight: 0.7},
		{SourceID: "mem-004-dddd", TargetID: "mem-001-aaaa", EdgeType: EdgeTypeTemporal, Weight: 0.5},
	}

	if err := g.AddEdges(ctx, edges); err != nil {
		t.Fatalf("AddEdges failed: %v", err)
	}

	// Get related to mem-001
	related, err := g.GetRelatedMemoryIDs(ctx, "mem-001-aaaa", 10)
	if err != nil {
		t.Fatalf("GetRelatedMemoryIDs failed: %v", err)
	}

	if len(related) != 3 {
		t.Errorf("expected 3 related IDs, got %d", len(related))
	}

	// Should be ordered by score (weight * confidence)
	if len(related) > 0 && related[0] != "mem-002-bbbb" {
		t.Errorf("expected mem-002 first (highest weight), got %s", related[0])
	}
}

func TestKnowledgeGraph_PageRank(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir:       tmpDir,
		DampingFactor: 0.85,
		MaxIterations: 100,
		Tolerance:     1e-6,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Create a simple graph where mem-002 has most incoming links from sources
	// (mem-001, mem-003, mem-004 all point to mem-002)
	edges := []MemoryEdge{
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 1.0},
		{SourceID: "mem-003-cccc", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 1.0},
		{SourceID: "mem-004-dddd", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 1.0},
		{SourceID: "mem-002-bbbb", TargetID: "mem-005-eeee", EdgeType: EdgeTypeReference, Weight: 1.0},
	}

	if err := g.AddEdges(ctx, edges); err != nil {
		t.Fatalf("AddEdges failed: %v", err)
	}

	// Compute PageRank
	if err := g.ComputePageRank(ctx); err != nil {
		t.Fatalf("ComputePageRank failed: %v", err)
	}

	// mem-002 should have higher PageRank than source nodes (mem-001, mem-003, mem-004)
	// which only receive from dangling redistributions
	pr1, _ := g.GetPageRank(ctx, "mem-001-aaaa")
	pr2, _ := g.GetPageRank(ctx, "mem-002-bbbb")
	pr3, _ := g.GetPageRank(ctx, "mem-003-cccc")

	if pr2 <= pr1 {
		t.Errorf("mem-002 should have higher PageRank than mem-001: %f <= %f", pr2, pr1)
	}
	if pr2 <= pr3 {
		t.Errorf("mem-002 should have higher PageRank than mem-003: %f <= %f", pr2, pr3)
	}

	// Verify all scores are positive
	if pr1 <= 0 || pr2 <= 0 || pr3 <= 0 {
		t.Error("PageRank scores should be positive")
	}
}

func TestKnowledgeGraph_CommunityDetection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Create two disconnected clusters
	edges := []MemoryEdge{
		// Cluster 1
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeSimilar, Weight: 0.9},
		{SourceID: "mem-002-bbbb", TargetID: "mem-003-cccc", EdgeType: EdgeTypeSimilar, Weight: 0.9},
		{SourceID: "mem-003-cccc", TargetID: "mem-001-aaaa", EdgeType: EdgeTypeSimilar, Weight: 0.9},
		// Cluster 2
		{SourceID: "mem-004-dddd", TargetID: "mem-005-eeee", EdgeType: EdgeTypeSimilar, Weight: 0.9},
		{SourceID: "mem-005-eeee", TargetID: "mem-006-ffff", EdgeType: EdgeTypeSimilar, Weight: 0.9},
		{SourceID: "mem-006-ffff", TargetID: "mem-004-dddd", EdgeType: EdgeTypeSimilar, Weight: 0.9},
	}

	if err := g.AddEdges(ctx, edges); err != nil {
		t.Fatalf("AddEdges failed: %v", err)
	}

	// Detect communities
	communities, err := g.DetectCommunities(ctx)
	if err != nil {
		t.Fatalf("DetectCommunities failed: %v", err)
	}

	if len(communities) != 6 {
		t.Errorf("expected 6 nodes in communities, got %d", len(communities))
	}

	// Nodes in same cluster should have same community
	c1 := communities["mem-001-aaaa"]
	c2 := communities["mem-002-bbbb"]
	c3 := communities["mem-003-cccc"]

	if c1 != c2 || c2 != c3 {
		t.Errorf("cluster 1 nodes should have same community: %s, %s, %s", c1, c2, c3)
	}

	c4 := communities["mem-004-dddd"]
	c5 := communities["mem-005-eeee"]

	if c4 != c5 {
		t.Errorf("cluster 2 nodes should have same community: %s, %s", c4, c5)
	}

	// Clusters should be different
	if c1 == c4 {
		t.Errorf("different clusters should have different community IDs")
	}
}

func TestKnowledgeGraph_RankResults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Create graph with different PageRank scores
	edges := []MemoryEdge{
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 1.0},
		{SourceID: "mem-003-cccc", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 1.0},
	}
	g.AddEdges(ctx, edges)
	g.ComputePageRank(ctx)

	// Create results where mem-001 has higher relevance but mem-002 has higher PageRank
	results := []MemoryResult{
		{
			Memory:         Memory{ID: "mem-001-aaaa", Content: "test 1"},
			RelevanceScore: 0.9,
		},
		{
			Memory:         Memory{ID: "mem-002-bbbb", Content: "test 2"},
			RelevanceScore: 0.5,
		},
	}

	// Re-rank with high PageRank influence
	ranked, err := g.RankResults(ctx, results, 0.7)
	if err != nil {
		t.Fatalf("RankResults failed: %v", err)
	}

	// With 70% PageRank influence, mem-002 should move up
	// (0.3 * 0.9 + 0.7 * low_pr) vs (0.3 * 0.5 + 0.7 * high_pr)
	if len(ranked) != 2 {
		t.Errorf("expected 2 results, got %d", len(ranked))
	}
}

func TestKnowledgeGraph_CreateTemporalEdges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	memoryIDs := []string{"mem-001-aaaa", "mem-002-bbbb", "mem-003-cccc", "mem-004-dddd"}

	if err := g.CreateTemporalEdges(ctx, "session-123", memoryIDs); err != nil {
		t.Fatalf("CreateTemporalEdges failed: %v", err)
	}

	stats, err := g.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// Should create n-1 edges for n nodes
	if stats.EdgeCount != 3 {
		t.Errorf("expected 3 temporal edges, got %d", stats.EdgeCount)
	}
}

func TestKnowledgeGraph_CreateSimilarityEdges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	memories := []Memory{
		{ID: "mem-001-aaaa", Content: "The quick brown fox jumps over the lazy dog"},
		{ID: "mem-002-bbbb", Content: "A quick brown fox jumped over a sleeping dog"},
		{ID: "mem-003-cccc", Content: "Programming in Go is productive and efficient"},
	}

	if err := g.CreateSimilarityEdges(ctx, memories, 0.2); err != nil {
		t.Fatalf("CreateSimilarityEdges failed: %v", err)
	}

	// mem-001 and mem-002 should be similar (share many words)
	// mem-003 should not be similar to the others
	edges, err := g.GetEdges(ctx, "mem-001-aaaa")
	if err != nil {
		t.Fatalf("GetEdges failed: %v", err)
	}

	foundSimilarToMem2 := false
	for _, e := range edges {
		if e.TargetID == "mem-002-bbbb" || e.SourceID == "mem-002-bbbb" {
			foundSimilarToMem2 = true
		}
	}

	if !foundSimilarToMem2 {
		t.Error("expected similarity edge between mem-001 and mem-002")
	}
}

func TestKnowledgeGraph_DeleteMemoryEdges(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	edges := []MemoryEdge{
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 0.9},
		{SourceID: "mem-002-bbbb", TargetID: "mem-003-cccc", EdgeType: EdgeTypeSimilar, Weight: 0.7},
	}
	g.AddEdges(ctx, edges)

	// Delete mem-002's edges
	if err := g.DeleteMemoryEdges(ctx, "mem-002-bbbb"); err != nil {
		t.Fatalf("DeleteMemoryEdges failed: %v", err)
	}

	stats, err := g.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.EdgeCount != 0 {
		t.Errorf("expected 0 edges after deletion, got %d", stats.EdgeCount)
	}
}

func TestJaccardSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        map[string]bool
		b        map[string]bool
		expected float64
	}{
		{
			name:     "empty sets",
			a:        map[string]bool{},
			b:        map[string]bool{},
			expected: 0,
		},
		{
			name:     "identical sets",
			a:        map[string]bool{"a": true, "b": true},
			b:        map[string]bool{"a": true, "b": true},
			expected: 1.0,
		},
		{
			name:     "disjoint sets",
			a:        map[string]bool{"a": true, "b": true},
			b:        map[string]bool{"c": true, "d": true},
			expected: 0,
		},
		{
			name:     "partial overlap",
			a:        map[string]bool{"a": true, "b": true, "c": true},
			b:        map[string]bool{"b": true, "c": true, "d": true},
			expected: 0.5, // 2 / 4
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jaccardSimilarity(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestKnowledgeGraph_CacheInvalidation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir:  tmpDir,
		CacheTTL: 1 * time.Second,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Add edges and compute PageRank
	edges := []MemoryEdge{
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 1.0},
	}
	g.AddEdges(ctx, edges)
	g.ComputePageRank(ctx)

	// Get initial PageRank
	pr1, _ := g.GetPageRank(ctx, "mem-002-bbbb")

	// Add new edge (should invalidate cache)
	g.AddEdge(ctx, MemoryEdge{
		SourceID: "mem-003-cccc",
		TargetID: "mem-002-bbbb",
		EdgeType: EdgeTypeReference,
		Weight:   1.0,
	})

	// Recompute and verify the scores changed
	g.ComputePageRank(ctx)
	pr2, _ := g.GetPageRank(ctx, "mem-002-bbbb")

	// After adding a new node, scores get redistributed.
	// The key assertion is that scores changed (cache invalidation worked)
	if pr1 == pr2 {
		t.Errorf("expected PageRank to change after adding edge, but stayed at %f", pr1)
	}

	// Also verify that new node has a score
	pr3, _ := g.GetPageRank(ctx, "mem-003-cccc")
	if pr3 <= 0 {
		t.Error("new node should have positive PageRank")
	}
}

func TestKnowledgeGraph_GetCommunitySiblings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: tmpDir,
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Create a connected cluster
	edges := []MemoryEdge{
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeSimilar, Weight: 0.9},
		{SourceID: "mem-002-bbbb", TargetID: "mem-003-cccc", EdgeType: EdgeTypeSimilar, Weight: 0.9},
		{SourceID: "mem-003-cccc", TargetID: "mem-001-aaaa", EdgeType: EdgeTypeSimilar, Weight: 0.9},
	}
	g.AddEdges(ctx, edges)

	// First compute PageRank to populate the pagerank table
	g.ComputePageRank(ctx)

	// Then detect communities (this updates the community_id column)
	communities, err := g.DetectCommunities(ctx)
	if err != nil {
		t.Fatalf("DetectCommunities failed: %v", err)
	}

	// Verify all three are in same community
	c1 := communities["mem-001-aaaa"]
	c2 := communities["mem-002-bbbb"]
	c3 := communities["mem-003-cccc"]

	if c1 != c2 || c2 != c3 {
		t.Errorf("expected same community for all nodes: %s, %s, %s", c1, c2, c3)
	}

	// Get siblings
	siblings, err := g.GetCommunitySiblings(ctx, "mem-001-aaaa", 10)
	if err != nil {
		t.Fatalf("GetCommunitySiblings failed: %v", err)
	}

	if len(siblings) != 2 {
		t.Errorf("expected 2 siblings, got %d (communities: %v)", len(siblings), communities)
	}
}

func TestKnowledgeGraph_ExpandResults(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "graph_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	g := NewKnowledgeGraph(KnowledgeGraphConfig{
		DataDir: filepath.Join(tmpDir, "graph"),
	})

	ctx := context.Background()
	if err := g.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	defer g.Close()

	// Create graph with some related memories
	edges := []MemoryEdge{
		{SourceID: "mem-001-aaaa", TargetID: "mem-002-bbbb", EdgeType: EdgeTypeReference, Weight: 0.9},
		{SourceID: "mem-001-aaaa", TargetID: "mem-003-cccc", EdgeType: EdgeTypeSimilar, Weight: 0.7},
	}
	g.AddEdges(ctx, edges)

	results := []MemoryResult{
		{Memory: Memory{ID: "mem-001-aaaa"}, RelevanceScore: 0.9},
	}

	expanded, err := g.ExpandResults(ctx, results, 5)
	if err != nil {
		t.Fatalf("ExpandResults failed: %v", err)
	}

	if len(expanded) != 2 {
		t.Errorf("expected 2 expanded IDs, got %d", len(expanded))
	}
}
