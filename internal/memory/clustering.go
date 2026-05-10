package memory

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
)

// EmbeddingProvider generates embeddings for text strings.
// This interface mirrors vector.Provider.GenerateEmbedding to allow
// the memory package to use embeddings without importing the vector
// package (which would create an import cycle).
type EmbeddingProvider interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct float32
	var normA float32
	var normB float32

	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// ClusterBySimilarity groups memories by semantic similarity using embeddings.
// When an embeddingProvider is available, it generates embeddings and clusters
// memories whose pairwise cosine similarity exceeds the threshold.
// Falls back to date-based grouping when no provider is available (returns each
// memory in its own cluster, preserving order).
func ClusterBySimilarity(ctx context.Context, memories []Memory, threshold float64, embedder EmbeddingProvider) ([][]Memory, error) {
	if len(memories) == 0 {
		return nil, nil
	}

	// No provider: fall back to individual clusters (each memory is its own group).
	if embedder == nil {
		clusters := make([][]Memory, len(memories))
		for i, m := range memories {
			clusters[i] = []Memory{m}
		}
		return clusters, nil
	}

	// Generate embeddings for all memory contents.
	embeddings := make([][]float32, len(memories))
	for i, mem := range memories {
		emb, err := embedder.GenerateEmbedding(ctx, mem.Content)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for memory %s: %w", mem.ID, err)
		}
		embeddings[i] = emb
	}

	// Build a similarity graph: for each pair, if similarity >= threshold, they are connected.
	// Use union-find to cluster connected memories.
	// Union-find parent array.
	parent := make([]int, len(memories))
	for i := range parent {
		parent[i] = i
	}

	var find func(x int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	union := func(x, y int) {
		px, py := find(x), find(y)
		if px != py {
			parent[px] = py
		}
	}

	// Compare all pairs.
	for i := 0; i < len(memories); i++ {
		for j := i + 1; j < len(memories); j++ {
			sim := cosineSimilarity(embeddings[i], embeddings[j])
			if float64(sim) >= threshold {
				union(i, j)
			}
		}
	}

	// Collect clusters by root.
	clusterMap := make(map[int][]Memory)
	for i := range memories {
		root := find(i)
		clusterMap[root] = append(clusterMap[root], memories[i])
	}

	// Convert to sorted slice for deterministic output.
	roots := make([]int, 0, len(clusterMap))
	for r := range clusterMap {
		roots = append(roots, r)
	}
	sort.Ints(roots)

	clusters := make([][]Memory, 0, len(roots))
	for _, r := range roots {
		clusters = append(clusters, clusterMap[r])
	}

	return clusters, nil
}

// ClusterBySimilarityFromResults is a convenience wrapper that extracts the
// underlying Memory values from MemoryResult slices before clustering.
func ClusterBySimilarityFromResults(ctx context.Context, results []MemoryResult, threshold float64, embedder EmbeddingProvider, logger *slog.Logger) ([][]Memory, error) {
	memories := make([]Memory, len(results))
	for i, r := range results {
		memories[i] = r.Memory
	}

	if embedder == nil {
		logger.Debug("no embedding provider available, returning individual clusters")
		return ClusterBySimilarity(ctx, memories, threshold, nil)
	}

	logger.Debug("clustering memories by semantic similarity",
		"count", len(memories),
		"threshold", threshold,
	)
	return ClusterBySimilarity(ctx, memories, threshold, embedder)
}
