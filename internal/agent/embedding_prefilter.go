package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// EmbeddingPrefilter implements STAGE-0 of ClassifyAndRoute
// (classifier-observability follow-up, HANDOFF.md §3): embed the raw input,
// cosine-match against per-intent centroids built from the labeled corpus,
// and return a direct-route Intent when the match is confident. On any miss
// or error it returns nil and the existing analyzer + router LLM chain runs
// unchanged — the prefilter can only skip work, never degrade routing.
type EmbeddingPrefilter struct {
	embedder  PrefilterEmbedder
	threshold float64
	margin    float64
	timeout   time.Duration
	path      string
	dimension int
	logger    *slog.Logger

	mu        sync.RWMutex
	centroids []prefilterCentroid
	storeDim  int
	staleErr  string // last load error, logged once until it changes
	loaded    bool
}

// PrefilterEmbedder produces one embedding vector per text. Satisfied by
// openAIEmbedClient (below); tests inject deterministic fakes.
type PrefilterEmbedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// prefilterCentroid is one labeled intent centroid.
type prefilterCentroid struct {
	Intent string    `json:"intent"`
	Agent  string    `json:"agent"`
	Count  int       `json:"count"`
	Vector []float64 `json:"vector"`
}

// prefilterStore is the on-disk centroid file produced by
// scripts/build_prefilter_centroids.py.
type prefilterStore struct {
	Model    string              `json:"model"`
	Dimension int               `json:"dimension"`
	BuiltAt  string              `json:"built_at"`
	Corpus   string              `json:"corpus"`
	Centroids []prefilterCentroid `json:"centroids"`
}

// Default prefilter tuning. Threshold is overridable via config; margin is a
// package constant to keep the config surface at the one knob the A/B sweep
// tunes (HANDOFF.md §3: τ=0.90 default).
const (
	DefaultPrefilterThreshold = 0.90
	defaultPrefilterMargin    = 0.05
	defaultPrefilterTimeout   = 2 * time.Second
	prefilterMethod           = "embedding_prefilter"
)

// NewEmbeddingPrefilter builds the Stage-0 gate. Centroid load is lazy
// (first Match) so daemon startup never blocks on the store, and a missing
// or corrupt store leaves the prefilter permanently inert rather than
// failing turns.
func NewEmbeddingPrefilter(emb PrefilterEmbedder, cfg config.ClassifierPrefilterConfig, logger *slog.Logger) *EmbeddingPrefilter {
	if logger == nil {
		logger = slog.Default()
	}
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = DefaultPrefilterThreshold
	}
	timeout := defaultPrefilterTimeout
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	path := cfg.CentroidsPath
	if path == "" {
		path = config.MeeptPath("classifier_prefilter_centroids.json")
	}
	return &EmbeddingPrefilter{
		embedder:  emb,
		threshold: threshold,
		margin:    defaultPrefilterMargin,
		timeout:   timeout,
		path:      path,
		dimension: cfg.Dimension,
		logger:    logger.With("component", "classifier_prefilter"),
	}
}

// loadCentroids reads and validates the centroid store if not yet loaded.
// Safe to call on every Match: after the first successful load it is a
// no-op; Reload forces a re-read (eval sweeps rewrite the store).
// I/O happens OUTSIDE the lock (collect-then-operate): read + parse the
// file first, then publish under the lock.
func (p *EmbeddingPrefilter) loadCentroids(force bool) bool {
	p.mu.RLock()
	loaded := p.loaded
	p.mu.RUnlock()
	if loaded && !force {
		return true
	}

	// File I/O under no lock (mutexio rule). Concurrent loads of the same
	// file are benign — last writer wins with identical content.
	data, err := os.ReadFile(p.path)
	if err != nil {
		p.failLoad("centroids store unreadable; prefilter inert", err.Error())
		return false
	}
	var store prefilterStore
	if err := json.Unmarshal(data, &store); err != nil {
		p.failLoad("centroids store corrupt; prefilter inert", err.Error())
		return false
	}
	if len(store.Centroids) == 0 || len(store.Centroids[0].Vector) == 0 {
		p.failLoad("centroids store empty; prefilter inert", p.path)
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded && !force {
		return true // raced with a successful load; keep it
	}
	p.centroids = store.Centroids
	p.storeDim = store.Dimension
	p.staleErr = ""
	p.loaded = true
	p.logger.Info("classifier prefilter loaded",
		"path", p.path,
		"intents", len(p.centroids),
		"dimension", p.storeDim,
		"model", store.Model,
		"threshold", p.threshold,
	)
	return true
}

// failLoad records a load failure without holding the lock (log-once per
// distinct detail, avoiding per-turn warn spam when the store is
// intentionally absent).
func (p *EmbeddingPrefilter) failLoad(msg, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.staleErr != detail {
		p.staleErr = detail
		p.logger.Warn(msg, "path", p.path, "detail", detail)
	}
}

// Reload forces a re-read of the centroid store on the next Match. Used
// after rebuilding centroids without a daemon restart.
func (p *EmbeddingPrefilter) Reload() {
	p.mu.Lock()
	p.loaded = false
	p.mu.Unlock()
}

// Match embeds the input and returns a direct-route Intent when the best
// centroid clears the threshold with the required margin over the runner-up.
// nil means "no opinion" — caller falls through to the LLM chain.
func (p *EmbeddingPrefilter) Match(ctx context.Context, input string) *Intent {
	input = strings.TrimSpace(input)
	if input == "" || !p.loadCentroids(false) {
		return nil
	}

	embCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	vec, err := p.embedder.Embed(embCtx, input)
	if err != nil {
		// Absorb the ctx error when the caller is already cancelling.
		if ctx.Err() != nil {
			return nil
		}
		p.logger.Warn("prefilter embed failed; falling through to LLM chain", "error", err)
		return nil
	}
	if p.dimension > 0 && len(vec) != p.dimension {
		p.logger.Warn("prefilter dimension mismatch; falling through",
			"got", len(vec), "want", p.dimension)
		return nil
	}
	if p.storeDim > 0 && len(vec) != p.storeDim {
		p.logger.Warn("prefilter embedding dimension differs from centroid store; rebuild centroids",
			"embedding", len(vec), "store", p.storeDim)
		return nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	best := -1
	bestScore := 0.0
	second := 0.0
	scores := make([]float64, len(p.centroids))
	for i, c := range p.centroids {
		s := CosineSimilarity(vec, c.Vector)
		scores[i] = s
		if s > bestScore {
			second = bestScore
			bestScore = s
			best = i
		} else if s > second {
			second = s
		}
	}
	if best < 0 || bestScore < p.threshold || bestScore-second < p.margin {
		p.logger.Debug("prefilter below threshold",
			"best_score", bestScore,
			"runner_up", second,
			"threshold", p.threshold,
			"margin", p.margin,
		)
		return nil
	}

	c := p.centroids[best]
	confidence := bestScore
	if confidence > 1 {
		confidence = 1
	}
	p.logger.Info("prefilter direct route",
		"intent", c.Intent,
		"agent", c.Agent,
		"score", bestScore,
		"runner_up", second,
	)
	return &Intent{
		Type:       c.Intent,
		Confidence: confidence,
		AgentType:  c.Agent,
		Summary:    extractSummary(input),
		Method:     prefilterMethod,
	}
}

// openAIEmbedClient calls an OpenAI-compatible /v1/embeddings endpoint
// (e.g. scripts/embed_server.py serving the Qwen3-Embedding weights).
type openAIEmbedClient struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewOpenAIEmbedClient targets an OpenAI-compatible embeddings API rooted
// at baseURL (e.g. "http://127.0.0.1:8090/v1").
func NewOpenAIEmbedClient(baseURL, model string, timeout time.Duration) *openAIEmbedClient {
	return &openAIEmbedClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}
}

type openAIEmbedRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (c *openAIEmbedClient) Embed(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(openAIEmbedRequest{Input: text, Model: c.model})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embed endpoint status %d: %s", resp.StatusCode, string(b))
	}
	var out openAIEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}
	if len(out.Data) == 0 || len(out.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embed endpoint returned no vectors")
	}
	return out.Data[0].Embedding, nil
}
