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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// EmbeddingPrefilter implements STAGE-0 of ClassifyAndRoute
// (classifier-observability follow-up, HANDOFF-STAGE0.md §5/§9): embed the
// raw input, kNN-match against labeled example vectors, and return a
// direct-route Intent when the vote is unanimous. On any miss or error it
// returns nil and the existing analyzer + router LLM chain runs unchanged —
// the prefilter can only skip work, never degrade routing.
//
// User invariant (2026-09-07): a wrong answer with overstated confidence is
// worse than a low-confidence correct one. Everything here is tuned for
// precision-first: unanimous top-k vote, no vote → nil, any failure → nil.
//
// AssertOnly mode (config) inverts nothing in the gate — it only changes
// what the CALLER does with the result. In assert mode the dispatcher logs
// the prefilter verdict and runs the LLM chain anyway, accumulating
// real-traffic agreement data with zero routing risk.
type EmbeddingPrefilter struct {
	embedder   PrefilterEmbedder
	threshold  float64
	k          int
	assertOnly bool
	timeout    time.Duration
	path       string
	dimension  int
	logger     *slog.Logger

	mu        sync.RWMutex
	examples  []prefilterExample
	storeDim  int
	staleErr  string // last load error, logged once until it changes
	loaded    bool
}

// PrefilterEmbedder produces one embedding vector per text. Satisfied by
// openAIEmbedClient (below); tests inject deterministic fakes.
type PrefilterEmbedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// prefilterExample is one labeled example vector (kNN index entry).
type prefilterExample struct {
	Intent string    `json:"intent"`
	Agent  string    `json:"agent"`
	Text   string    `json:"text,omitempty"`
	Vector []float64 `json:"vector"`
}

// prefilterStore is the on-disk kNN index produced by
// scripts/build_prefilter_centroids.py (which writes per-example vectors;
// legacy centroid-only stores load too — each centroid then acts as one
// pseudo-example).
type prefilterStore struct {
	Model     string             `json:"model"`
	Dimension int                `json:"dimension"`
	BuiltAt   string             `json:"built_at"`
	Corpus    string             `json:"corpus"`
	Examples  []prefilterExample `json:"examples"`
	// Legacy centroid fields — read only when Examples is empty.
	Centroids []prefilterExample `json:"centroids"`
}

// Default prefilter tuning. Threshold is overridable via config; k and
// margin are package constants to keep the config surface at the one knob
// the A/B sweep tunes.
const (
	// DefaultPrefilterThreshold is the minimum cosine for a neighbor to
	// count toward the vote. Cosine-neighborhood floors are sharper than
	// centroid scores (HANDOFF-STAGE0.md §9: flat margin distribution),
	// so this gates membership, not the final decision.
	DefaultPrefilterThreshold = 0.70
	// defaultPrefilterK is the vote size: ALL k nearest examples must
	// agree on one intent, else nil. Unanimity is the precision
	// instrument — mixed neighborhoods are exactly the ambiguous inputs
	// the LLM chain should see.
	defaultPrefilterK = 5
	defaultPrefilterTimeout = 2 * time.Second
	prefilterMethod         = "embedding_prefilter"
)

// NewEmbeddingPrefilter builds the Stage-0 gate. Index load is lazy
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
		embedder:   emb,
		threshold:  threshold,
		k:          defaultPrefilterK,
		assertOnly: cfg.AssertOnly,
		timeout:    timeout,
		path:       path,
		dimension:  cfg.Dimension,
		logger:     logger.With("component", "classifier_prefilter"),
	}
}

// loadIndex reads and validates the example store if not yet loaded.
// Safe to call on every Match: after the first successful load it is a
// no-op; Reload forces a re-read (eval sweeps rewrite the store).
// I/O happens OUTSIDE the lock (collect-then-operate): read + parse the
// file first, then publish under the lock.
func (p *EmbeddingPrefilter) loadIndex(force bool) bool {
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
		p.failLoad("prefilter index unreadable; prefilter inert", err.Error())
		return false
	}
	var store prefilterStore
	if err := json.Unmarshal(data, &store); err != nil {
		p.failLoad("prefilter index corrupt; prefilter inert", err.Error())
		return false
	}
	examples := store.Examples
	if len(examples) == 0 {
		// Legacy centroid-only store: each centroid becomes one
		// pseudo-example so old files keep working after upgrade.
		examples = store.Centroids
	}
	if len(examples) == 0 || len(examples[0].Vector) == 0 {
		p.failLoad("prefilter index empty; prefilter inert", p.path)
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.loaded && !force {
		return true // raced with a successful load; keep it
	}
	p.examples = examples
	p.storeDim = store.Dimension
	p.staleErr = ""
	p.loaded = true
	p.logger.Info("classifier prefilter loaded",
		"path", p.path,
		"examples", len(p.examples),
		"intents", p.countIntentsLocked(),
		"dimension", p.storeDim,
		"model", store.Model,
		"threshold", p.threshold,
		"k", p.k,
	)
	return true
}

// countIntentsLocked counts distinct intents in the loaded index. Caller
// holds p.mu.
func (p *EmbeddingPrefilter) countIntentsLocked() int {
	seen := make(map[string]struct{}, len(p.examples))
	for _, e := range p.examples {
		seen[e.Intent] = struct{}{}
	}
	return len(seen)
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

// Reload forces a re-read of the example store on the next Match. Used
// after rebuilding the index without a daemon restart.
func (p *EmbeddingPrefilter) Reload() {
	p.mu.Lock()
	p.loaded = false
	p.mu.Unlock()
}

// kNNVote is the internal match result: winning intent (empty when no
// unanimous vote), its unanimity confidence, and the margin over the best
// non-winning neighbor score.
type kNNVote struct {
	Intent     string
	Agent      string
	Confidence float64 // min cosine among the k winners (unanimity floor)
	Margin     float64 // winner-floor minus best losing neighbor score
}

// selfMatchCutoff: neighbors at or above this cosine are exact/near-exact
// duplicates of the query itself (e.g. verbatim corpus repeats). They occupy
// a vote slot while carrying no independent class evidence, so they are
// excluded from the vote. Without this, verbatim repeats abstain (self
// crowds out a real neighbor) — the daemon behaves worse than the LOO
// sweep predicts.
const selfMatchCutoff = 0.999

// vote runs the kNN unanimity scan on vec against the loaded index.
// Caller holds p.mu (read). Returns ok=false when no unanimous vote.
func (p *EmbeddingPrefilter) vote(vec []float64) (kNNVote, bool) {
	type scored struct {
		idx   int
		score float64
	}
	neighbors := make([]scored, 0, len(p.examples))
	for i, e := range p.examples {
		s := CosineSimilarity(vec, e.Vector)
		if s >= selfMatchCutoff {
			continue // self/near-self match: not evidence, skip
		}
		if s >= p.threshold {
			neighbors = append(neighbors, scored{i, s})
		}
	}
	if len(neighbors) < p.k {
		return kNNVote{}, false
	}
	sort.Slice(neighbors, func(a, b int) bool { return neighbors[a].score > neighbors[b].score })
	neighbors = neighbors[:p.k]

	first := p.examples[neighbors[0].idx].Intent
	floor := neighbors[0].score
	for _, nb := range neighbors {
		if p.examples[nb.idx].Intent != first {
			return kNNVote{}, false // any dissenter kills the vote
		}
		if nb.score < floor {
			floor = nb.score
		}
	}
	// Best score among examples OUTSIDE the winning k (for margin): the
	// highest-scoring example whose intent disagrees with the vote.
	// Agreeing outsiders don't threaten the vote.
	topKSet := make(map[int]struct{}, p.k)
	for _, nb := range neighbors {
		topKSet[nb.idx] = struct{}{}
	}
	bestLosing := 0.0
	for i, e := range p.examples {
		if _, inTop := topKSet[i]; inTop {
			continue
		}
		if e.Intent == first {
			continue
		}
		s := CosineSimilarity(vec, e.Vector)
		if s > bestLosing {
			bestLosing = s
		}
	}
	var agent string
	for _, nb := range neighbors {
		if e := p.examples[nb.idx]; e.Agent != "" {
			agent = e.Agent
			break
		}
	}
	return kNNVote{
		Intent:     first,
		Agent:      agent,
		Confidence: floor,
		Margin:     floor - bestLosing,
	}, true
}

// Match embeds the input and returns a direct-route Intent when the k
// nearest examples vote unanimously for one intent. nil means "no opinion"
// — caller falls through to the LLM chain.
func (p *EmbeddingPrefilter) Match(ctx context.Context, input string) *Intent {
	input = strings.TrimSpace(input)
	if input == "" || !p.loadIndex(false) {
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

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.storeDim > 0 && len(vec) != p.storeDim {
		p.logger.Warn("prefilter embedding dimension differs from index; rebuild index",
			"embedding", len(vec), "store", p.storeDim)
		return nil
	}

	v, ok := p.vote(vec)
	if !ok {
		p.logger.Debug("prefilter no unanimous vote",
			"k", p.k,
			"threshold", p.threshold,
		)
		return nil
	}
	confidence := v.Confidence
	if confidence > 1 {
		confidence = 1
	}
	p.logger.Info("prefilter direct route",
		"intent", v.Intent,
		"agent", v.Agent,
		"k", p.k,
		"unanimity_floor", v.Confidence,
		"margin", v.Margin,
	)
	return &Intent{
		Type:       v.Intent,
		Confidence: confidence,
		AgentType:  v.Agent,
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
