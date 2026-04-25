package agent

import (
	"context"
	"testing"
)

// mockEmbeddingClient implements EmbeddingClient for testing.
type mockEmbeddingClient struct {
	embedFunc      func(ctx context.Context, text string) ([]float64, error)
	embedBatchFunc func(ctx context.Context, texts []string) ([][]float64, error)
	dimension      int
}

func (m *mockEmbeddingClient) Embed(ctx context.Context, text string) ([]float64, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float64{0.5, 0.5}, nil
}

func (m *mockEmbeddingClient) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if m.embedBatchFunc != nil {
		return m.embedBatchFunc(ctx, texts)
	}
	result := make([][]float64, len(texts))
	for i := range texts {
		result[i] = []float64{0.5, 0.5}
	}
	return result, nil
}

func (m *mockEmbeddingClient) Dimension() int {
	return m.dimension
}

func TestBuildIndex(t *testing.T) {
	client := &mockEmbeddingClient{dimension: 2}
	idx := NewSemanticIndex(client)

	ctx := context.Background()
	err := idx.BuildIndex(ctx)
	if err != nil {
		t.Fatalf("BuildIndex() error = %v", err)
	}
	if !idx.IsReady() {
		t.Error("IsReady() = false after BuildIndex()")
	}
	if len(idx.entries) == 0 {
		t.Error("entries is empty after BuildIndex()")
	}
}

func TestMatch(t *testing.T) {
	client := &mockEmbeddingClient{dimension: 2}
	idx := NewSemanticIndex(client)

	ctx := context.Background()
	if err := idx.BuildIndex(ctx); err != nil {
		t.Fatalf("BuildIndex() error = %v", err)
	}

	match := idx.Match("write some code", 0.0)
	if match == nil {
		t.Fatal("Match() returned nil")
	}
	if match.IntentType == "" {
		t.Error("Match() returned empty IntentType")
	}
	if match.Confidence < 0 {
		t.Errorf("Match() confidence = %v, want >= 0", match.Confidence)
	}
}

func TestMatchBelowThreshold(t *testing.T) {
	client := &mockEmbeddingClient{
		dimension: 2,
		embedFunc: func(ctx context.Context, text string) ([]float64, error) {
			return []float64{-0.9, -0.9}, nil
		},
		embedBatchFunc: func(ctx context.Context, texts []string) ([][]float64, error) {
			// Index entries use positive vectors
			result := make([][]float64, len(texts))
			for i := range texts {
				result[i] = []float64{0.5, 0.5}
			}
			return result, nil
		},
	}
	idx := NewSemanticIndex(client)

	ctx := context.Background()
	if err := idx.BuildIndex(ctx); err != nil {
		t.Fatalf("BuildIndex() error = %v", err)
	}

	match := idx.Match("irrelevant query", 0.99)
	if match != nil {
		t.Errorf("Match() with high threshold returned %v, want nil", match)
	}
}

func TestMatchUnready(t *testing.T) {
	client := &mockEmbeddingClient{dimension: 2}
	idx := NewSemanticIndex(client)

	match := idx.Match("test", 0.5)
	if match != nil {
		t.Errorf("Match() on unready index returned %v, want nil", match)
	}
}
