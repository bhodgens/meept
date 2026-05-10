package memory

import (
	"context"
	"errors"
	"testing"
)

// mockEmbedder implements EmbeddingProvider for testing.
type mockEmbedder struct {
	// embeddings maps content text to a fixed embedding vector.
	embeddings map[string][]float32
	err        error
}

func (m *mockEmbedder) GenerateEmbedding(_ context.Context, text string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	if emb, ok := m.embeddings[text]; ok {
		return emb, nil
	}
	// Return a zero vector for unknown texts.
	return make([]float32, 4), nil
}

// unitVector returns a unit-length vector in the given dimension with the first
// component set to 1.0 (all others zero).
func unitVector(dim int) []float32 {
	v := make([]float32, dim)
	v[0] = 1.0
	return v
}

// similarVector returns a vector very close to the given vector (cosine
// similarity ~0.99). It shifts a tiny amount from the first component to the
// second.
func similarVector(v []float32) []float32 {
	out := make([]float32, len(v))
	copy(out, v)
	out[0] = 0.995
	out[1] = 0.1
	return out
}

// orthogonalVector returns a vector orthogonal to the given vector (similarity 0).
// It places the magnitude in the second component.
func orthogonalVector(v []float32) []float32 {
	out := make([]float32, len(v))
	copy(out, v)
	out[0] = 0.0
	out[1] = 1.0
	return out
}

func TestClusterBySimilarity_SimilarMemories(t *testing.T) {
	base := unitVector(4)
	sim := similarVector(base)

	embedder := &mockEmbedder{
		embeddings: map[string][]float32{
			"rust programming":  base,
			"go programming":    sim,
			"web development":   orthogonalVector(base),
		},
	}

	memories := []Memory{
		{ID: "mem-1", Content: "rust programming"},
		{ID: "mem-2", Content: "go programming"},
		{ID: "mem-3", Content: "web development"},
	}

	clusters, err := ClusterBySimilarity(context.Background(), memories, 0.8, embedder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// mem-1 and mem-2 should be in one cluster; mem-3 in another.
	if len(clusters) != 2 {
		t.Fatalf("expected 2 clusters, got %d", len(clusters))
	}

	// Find the cluster with 2 items (mem-1 and mem-2).
	var pairCluster, singleCluster []Memory
	for _, c := range clusters {
		if len(c) == 2 {
			pairCluster = c
		} else if len(c) == 1 {
			singleCluster = c
		}
	}

	if pairCluster == nil {
		t.Fatal("expected a cluster with 2 memories, found none")
	}
	if singleCluster == nil {
		t.Fatal("expected a cluster with 1 memory, found none")
	}

	// Verify the pair cluster contains the programming memories.
	ids := map[string]bool{}
	for _, m := range pairCluster {
		ids[m.ID] = true
	}
	if !ids["mem-1"] || !ids["mem-2"] {
		t.Errorf("pair cluster should contain mem-1 and mem-2, got %v", pairCluster)
	}

	// Verify the single cluster is the web development memory.
	if singleCluster[0].ID != "mem-3" {
		t.Errorf("single cluster should contain mem-3, got %v", singleCluster)
	}
}

func TestClusterBySimilarity_DissimilarMemories(t *testing.T) {
	v1 := unitVector(4)
	v2 := orthogonalVector(v1)
	v3 := make([]float32, 4)
	v3[2] = 1.0 // Yet another orthogonal direction.

	embedder := &mockEmbedder{
		embeddings: map[string][]float32{
			"alpha": v1,
			"beta":  v2,
			"gamma": v3,
		},
	}

	memories := []Memory{
		{ID: "m1", Content: "alpha"},
		{ID: "m2", Content: "beta"},
		{ID: "m3", Content: "gamma"},
	}

	// With a high threshold, all three should be in separate clusters.
	clusters, err := ClusterBySimilarity(context.Background(), memories, 0.9, embedder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(clusters) != 3 {
		t.Fatalf("expected 3 separate clusters for dissimilar memories, got %d", len(clusters))
	}

	for i, c := range clusters {
		if len(c) != 1 {
			t.Errorf("cluster %d should have exactly 1 memory, got %d", i, len(c))
		}
	}
}

func TestClusterBySimilarity_EmptyInput(t *testing.T) {
	embedder := &mockEmbedder{}

	clusters, err := ClusterBySimilarity(context.Background(), nil, 0.8, embedder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clusters != nil {
		t.Errorf("expected nil clusters for empty input, got %v", clusters)
	}

	// Also test empty slice explicitly.
	clusters, err = ClusterBySimilarity(context.Background(), []Memory{}, 0.8, embedder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clusters != nil {
		t.Errorf("expected nil clusters for empty slice, got %v", clusters)
	}
}

func TestClusterBySimilarity_NoProvider(t *testing.T) {
	memories := []Memory{
		{ID: "m1", Content: "first"},
		{ID: "m2", Content: "second"},
		{ID: "m3", Content: "third"},
	}

	clusters, err := ClusterBySimilarity(context.Background(), memories, 0.8, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Without a provider, each memory should be in its own cluster.
	if len(clusters) != 3 {
		t.Fatalf("expected 3 individual clusters, got %d", len(clusters))
	}

	for i, c := range clusters {
		if len(c) != 1 {
			t.Errorf("cluster %d should have exactly 1 memory, got %d", i, len(c))
		}
		if c[0].ID != memories[i].ID {
			t.Errorf("cluster %d should contain memory %s, got %s", i, memories[i].ID, c[0].ID)
		}
	}
}

func TestClusterBySimilarity_ProviderError(t *testing.T) {
	embedder := &mockEmbedder{
		err: errors.New("API unavailable"),
	}

	memories := []Memory{
		{ID: "m1", Content: "content"},
	}

	_, err := ClusterBySimilarity(context.Background(), memories, 0.8, embedder)
	if err == nil {
		t.Fatal("expected error when provider fails, got nil")
	}
}

func TestClusterBySimilarity_AllSame(t *testing.T) {
	base := unitVector(4)

	embedder := &mockEmbedder{
		embeddings: map[string][]float32{
			"a": base,
			"b": base,
			"c": base,
		},
	}

	memories := []Memory{
		{ID: "m1", Content: "a"},
		{ID: "m2", Content: "b"},
		{ID: "m3", Content: "c"},
	}

	clusters, err := ClusterBySimilarity(context.Background(), memories, 0.8, embedder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All three have identical embeddings, so they should be in one cluster.
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster for identical embeddings, got %d", len(clusters))
	}
	if len(clusters[0]) != 3 {
		t.Errorf("expected 3 memories in cluster, got %d", len(clusters[0]))
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		wantZero bool
		wantOne  bool
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			wantOne:  true,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0},
			b:        []float32{0.0, 1.0},
			wantZero: true,
		},
		{
			name:     "zero vector",
			a:        []float32{0.0, 0.0},
			b:        []float32{1.0, 0.0},
			wantZero: true,
		},
		{
			name:     "mismatched lengths",
			a:        []float32{1.0},
			b:        []float32{1.0, 2.0},
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim := cosineSimilarity(tt.a, tt.b)
			if tt.wantOne && sim != 1.0 {
				t.Errorf("expected similarity 1.0, got %f", sim)
			}
			if tt.wantZero && sim != 0.0 {
				t.Errorf("expected similarity 0.0, got %f", sim)
			}
		})
	}
}
