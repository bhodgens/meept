// Package vector provides semantic memory search using vector embeddings.
package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/caimlas/meept/internal/config"
)

// EmbeddingDimension is the dimension of the embedding vector.
const EmbeddingDimension = 1536 // OpenAI text-embedding-3-small

// Provider is an embedding generation provider.
type Provider interface {
	// GenerateEmbedding generates an embedding for the given text.
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
	// GenerateEmbeddings generates embeddings for multiple texts.
	GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
	// Dimension returns the dimension of the embeddings.
	Dimension() int
}

// OpenAIProvider generates embeddings using OpenAI's API.
type OpenAIProvider struct {
	apiKey     string
	baseURL    string
	model      string
	dimension  int
	httpClient *http.Client
	logger     *slog.Logger
}

// OpenAIProviderConfig holds configuration for the OpenAI embedding provider.
type OpenAIProviderConfig struct {
	APIKey    string
	BaseURL   string
	Model     string
	Dimension int
	Logger    *slog.Logger
}

// NewOpenAIProvider creates a new OpenAI embedding provider.
func NewOpenAIProvider(cfg OpenAIProviderConfig) *OpenAIProvider {
	if cfg.Model == "" {
		cfg.Model = "text-embedding-3-small"
	}
	if cfg.Dimension == 0 {
		cfg.Dimension = 1536 // text-embedding-3-small default
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &OpenAIProvider{
		apiKey:     cfg.APIKey,
		baseURL:    cfg.BaseURL,
		model:      cfg.Model,
		dimension:  cfg.Dimension,
		httpClient: &http.Client{},
		logger:     cfg.Logger,
	}
}

// openAIEmbeddingRequest is the request body for OpenAI embeddings API.
type openAIEmbeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// openAIEmbeddingResponse is the response from OpenAI embeddings API.
type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// GenerateEmbedding generates an embedding for the given text.
func (p *OpenAIProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.GenerateEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// GenerateEmbeddings generates embeddings for multiple texts.
func (p *OpenAIProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := openAIEmbeddingRequest{
		Input: texts,
		Model: p.model,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Body = io.NopCloser(stringsReader(string(reqJSON)))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		p.logger.Warn("OpenAI API error", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("OpenAI API returned status %d: %s", resp.StatusCode, string(body))
	}

	var respData openAIEmbeddingResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if respData.Error != nil {
		return nil, fmt.Errorf("OpenAI API error: %s", respData.Error.Message)
	}

	if len(respData.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(respData.Data))
	}

	embeddings := make([][]float32, len(respData.Data))
	for i, data := range respData.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}

// Dimension returns the dimension of the embeddings.
func (p *OpenAIProvider) Dimension() int {
	return p.dimension
}

// OllamaProvider generates embeddings using a local Ollama instance.
type OllamaProvider struct {
	baseURL    string
	model      string
	dimension  int
	httpClient *http.Client
	logger     *slog.Logger
}

// OllamaProviderConfig holds configuration for the Ollama embedding provider.
type OllamaProviderConfig struct {
	BaseURL   string
	Model     string
	Dimension int
	Logger    *slog.Logger
}

// NewOllamaProvider creates a new Ollama embedding provider.
func NewOllamaProvider(cfg OllamaProviderConfig) *OllamaProvider {
	if cfg.Model == "" {
		cfg.Model = "nomic-embed-text"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Dimension == 0 {
		cfg.Dimension = 768 // nomic-embed-text default
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &OllamaProvider{
		baseURL:    cfg.BaseURL,
		model:      cfg.Model,
		dimension:  cfg.Dimension,
		httpClient: &http.Client{},
		logger:     cfg.Logger,
	}
}

// ollamaEmbeddingRequest is the request body for Ollama embeddings API.
type ollamaEmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbeddingResponse is the response from Ollama embeddings API.
type ollamaEmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error"`
}

// GenerateEmbedding generates an embedding for the given text.
func (p *OllamaProvider) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	reqBody := ollamaEmbeddingRequest{
		Model:  p.model,
		Prompt: text,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/embeddings", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = io.NopCloser(stringsReader(string(reqJSON)))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		p.logger.Warn("Ollama API error", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("ollama API returned status %d: %s", resp.StatusCode, string(body))
	}

	var respData ollamaEmbeddingResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if respData.Error != "" {
		return nil, fmt.Errorf("ollama API error: %s", respData.Error)
	}

	return respData.Embedding, nil
}

// GenerateEmbeddings generates embeddings for multiple texts.
// Ollama doesn't support batch requests, so we make individual calls.
func (p *OllamaProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := p.GenerateEmbedding(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embedding for text %d: %w", i, err)
		}
		embeddings[i] = emb
	}
	return embeddings, nil
}

// Dimension returns the dimension of the embeddings.
func (p *OllamaProvider) Dimension() int {
	return p.dimension
}

// NewProviderFromConfig creates an embedding provider from configuration.
func NewProviderFromConfig(cfg config.EmbeddingConfig) (Provider, error) {
	switch cfg.Provider {
	case "openai", "":
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key not provided")
		}
		return NewOpenAIProvider(OpenAIProviderConfig{
			APIKey:    apiKey,
			BaseURL:   cfg.BaseURL,
			Model:     cfg.Model,
			Dimension: cfg.Dimension,
		}), nil

	case "ollama":
		return NewOllamaProvider(OllamaProviderConfig{
			BaseURL:   cfg.BaseURL,
			Model:     cfg.Model,
			Dimension: cfg.Dimension,
		}), nil

	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", cfg.Provider)
	}
}

// stringsReader is a helper to avoid importing strings.
func stringsReader(s string) io.Reader {
	return &stringReader{s}
}

type stringReader struct {
	s string
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p, r.s)
	r.s = r.s[n:]
	return n, nil
}
