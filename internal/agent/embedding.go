package agent

import (
	"context"
	"math"
	"time"

	"github.com/caimlas/meept/internal/skills"
)

// EmbeddingClient generates vector embeddings for text.
type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
	Dimension() int
}

// CosineSimilarity computes cosine similarity between two vectors.
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// skillRankingEmbedder adapts the in-tree OpenAI-compatible embeddings
// client (openAIEmbedClient) to the skills.Embedder seam used by
// SkillIndex.RankedNames for the model-facing skill listing
// (token-efficiency branch 04 leaf 02). The single-text client is
// sequentialized for batches; conversion is float64 -> float32.
type skillRankingEmbedder struct {
	client *openAIEmbedClient
}

// NewSkillRankingEmbedder returns a skills.Embedder backed by an
// OpenAI-compatible /v1/embeddings endpoint rooted at baseURL (e.g.
// "http://127.0.0.1:8090/v1"). The daemon wires it only when
// [skills.ranking].enabled — nil config never reaches ranking.
func NewSkillRankingEmbedder(baseURL, model string, timeout time.Duration) skills.Embedder {
	return &skillRankingEmbedder{client: NewOpenAIEmbedClient(baseURL, model, timeout)}
}

// Embed implements skills.Embedder. One HTTP call per text (the in-tree
// client is single-input); a failure on any text fails the batch so
// RankedNames degrades to fallback order rather than ranking a subset.
func (e *skillRankingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, t := range texts {
		vec, err := e.client.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		f32 := make([]float32, len(vec))
		for i, v := range vec {
			f32[i] = float32(v)
		}
		out = append(out, f32)
	}
	return out, nil
}
