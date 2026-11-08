package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
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

// SnowflakeEmbedClient implements EmbeddingClient using Snowflake Arctic Embed.
type SnowflakeEmbedClient struct {
	apiKey     string
	baseURL    string
	model      string
	dimension  int
	httpClient *http.Client
}

// NewSnowflakeEmbedClient creates a new Snowflake embedding client.
func NewSnowflakeEmbedClient(apiKey string) *SnowflakeEmbedClient {
	return &SnowflakeEmbedClient{
		apiKey:    apiKey,
		baseURL:   "https://api.snowflake.ai/v1/embeddings",
		model:     "snowflake-arctic-embed-m-v1.5",
		dimension: 1024,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type snowflakeEmbedRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type snowflakeEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
	Model     string    `json:"model"`
}

func (c *SnowflakeEmbedClient) Embed(ctx context.Context, text string) ([]float64, error) {
	reqBody := snowflakeEmbedRequest{
		Input: text,
		Model: c.model,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API returned status %d: %s", resp.StatusCode, string(body))
	}

	var embedResp snowflakeEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("failed to decode embedding response: %w", err)
	}

	return embedResp.Embedding, nil
}

func (c *SnowflakeEmbedClient) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	vectors := make([][]float64, 0, len(texts))
	for _, text := range texts {
		vector, err := c.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, vector)
	}
	return vectors, nil
}

func (c *SnowflakeEmbedClient) Dimension() int {
	return c.dimension
}
