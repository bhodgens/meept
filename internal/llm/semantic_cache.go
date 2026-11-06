package llm

// Semantic response cache tier (llm-serving-tiers leaf 02, Contract C5).
//
// An opt-in SECOND-CHANCE lookup that sits strictly BEHIND the exact-key
// response cache (token_cache.go): the client consults the exact key first
// and only on a MISS hands the prompt here. LookupBehindMiss embeds the
// prompt, compares cosine similarity against stored prompt vectors, and
// serves the stored response when the best similarity clears the
// configured threshold.
//
// Default threshold is 0.95 cosine (docs/research/
// 20261031-llm-serving-tiers-findings.md Section 1.2, the dated addendum
// superseding master.md C4's 0.92): AWS Well-Architected, Redis LangCache,
// and Vercel all converge on 0.95+ for false-positive-sensitive workloads,
// and agent traffic (tool calls, code, one-operand differences) is exactly
// the traffic loose thresholds get wrong. The over-tight direction
// (missed reuse) is cheap and bounded; the over-loose direction (wrong
// answers) is unbounded.
//
// HARD constraints (all pinned by tests):
//   - A semantic hit NEVER serves a response carrying tool calls --
//     tool-call-bearing responses are neither STORED nor SERVED (the
//     tool call IS the answer; a semantic hit would replace the whole
//     turn including the tool never running). Mirrors the
//     classifyAnthropicEmpty tool_use exemption shape.
//   - Embedder failure degrades to exact-only: LookupBehindMiss returns
//     (nil, false, nil) on ANY embedder error and the caller proceeds to
//     the live path -- never an error, never a parked turn.
//   - ErrEmptyResponse accounting is untouched: a lookup or store attempt
//     never suppresses empty-response rotation.
//   - Partition: entries key on (model, instruction fingerprint) --
//     answers never leak across models or capability scopes
//     (findings Section 1.3 item 5).
//   - Disabled (llm.cache.semantic.enabled=false, the default) means the
//     code path is structurally unreachable: no embedder call, no vector
//     math, immediate miss.
//
// No mutex is held across I/O: the embedder call happens OUTSIDE the
// store lock (mutexio analyzer). Stdlib-only vector math: float32 dot
// products + norms, no new dependencies.

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"sync"
	"time"
)

// Embedder produces prompt vectors for semantic-cache lookups (Contract
// C5). Minimal seam: the daemon adapts the existing memory/vector
// embedding provider (the vector.Provider / memory.EmbeddingProvider
// GenerateEmbedding shape) onto this -- no new embedding path.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// SemanticCacheConfig is the llm-side shape of Contract C4 (json5 key
// llm.cache.semantic). internal/config carries the identical JSON5 shape
// (config.SemanticCacheConfig) and converts at wiring time.
type SemanticCacheConfig struct {
	// Enabled turns the tier on. Default FALSE (opt-in per
	// docs/agents/opt-in-defaults.md): when false every method returns
	// the miss immediately without touching the embedder.
	Enabled bool
	// Threshold is the cosine similarity floor for serving. Default 0.95
	// (research addendum; supersedes master C4's 0.92).
	Threshold float64
	// MaxEntries bounds the vector store (LRU eviction). Default 1024.
	MaxEntries int
	// TTL is the entry lifetime; entries older than this are never
	// served. Default mirrors llm.cache.default_ttl_min at the wiring
	// site (components.go) so the two tiers expire together.
	TTL time.Duration
	// PartitionKey further scopes entries beyond the model id (the
	// daemon may stamp an instruction fingerprint here). Empty means
	// model-only partitioning.
	PartitionKey string
	// VerifyFloor is the lower bound of the two-stage verifier band
	// (wiring-followups-final leaf 03). A candidate scoring in
	// [VerifyFloor, Threshold) is re-embedded (stage 2) and direct-cosine
	// compared against its stored embedding: >= Threshold serves, below
	// misses. Zero (the default) disables the band = pre-band behavior.
	// Values >= Threshold are clamped to 0 (a useless band) at
	// construction; negative values are clamped to 0 as well.
	VerifyFloor float64
}

// Defaults for the semantic tier (C4, threshold per the research
// addendum dated before dispatch).
const (
	// DefaultSemanticThreshold is the 0.95 cosine floor.
	DefaultSemanticThreshold = 0.95
	// DefaultSemanticMaxEntries bounds the in-memory vector store.
	DefaultSemanticMaxEntries = 1024
	// DefaultSemanticTTL is the fallback entry lifetime when the wiring
	// site does not mirror llm.cache.default_ttl_min.
	DefaultSemanticTTL = time.Hour
)

// semanticEntry is one stored prompt vector + its response.
type semanticEntry struct {
	vector    []float32
	response  *Response
	partition string
	expiresAt time.Time
}

// SemanticCache is the in-memory vector store behind the exact-key miss.
type SemanticCache struct {
	embedder Embedder
	cfg      SemanticCacheConfig

	mu      sync.Mutex
	entries map[string]*semanticEntry // key: partition + " " + text hash
	lru     *list.List                // front = most recently used; values are keys
}

// NewSemanticCache builds the tier. A nil embedder or a disabled config
// yields a structurally-dead cache: every method returns miss without any
// embedder call or vector work.
func NewSemanticCache(embedder Embedder, cfg SemanticCacheConfig) *SemanticCache {
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultSemanticThreshold
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = DefaultSemanticMaxEntries
	}
	// TTL is taken as configured, INCLUDING a negative or zero value
	// (a test/ops escape hatch meaning "everything expires immediately");
	// only an unset TTL (time.Duration zero would be ambiguous with a
	// deliberate 0) is defaulted -- NewSemanticCache callers that want
	// the default pass DefaultSemanticTTL explicitly via the wiring
	// site. To keep the default posture for plain zero-value configs we
	// distinguish: exactly zero means "not set" -> default; negative
	// means "expire now" and is honored.
	if cfg.TTL == 0 {
		cfg.TTL = DefaultSemanticTTL
	}
	// Two-stage verifier band clamp (leaf 03): negative, or a floor at/above
	// the serving threshold, is no band at all -- clamp to 0 (disabled) so
	// the band can never swallow legitimate hits into an empty verify set.
	if cfg.VerifyFloor < 0 || cfg.VerifyFloor >= cfg.Threshold {
		cfg.VerifyFloor = 0
	}
	return &SemanticCache{
		embedder: embedder,
		cfg:      cfg,
		entries:  make(map[string]*semanticEntry),
		lru:      list.New(),
	}
}

// Enabled reports whether the tier is live. Nil-receiver safe (a nil
// cache is the disabled posture at every call site).
func (s *SemanticCache) Enabled() bool {
	return s != nil && s.cfg.Enabled && s.embedder != nil
}

// LookupBehindMiss is the only lookup entry point the client calls
// (Contract C5): (response, true, nil) hit | (nil, false, nil) miss |
// (nil, false, nil) on ANY embedder failure (exact-only degradation,
// never an error, never a parked turn).
func (s *SemanticCache) LookupBehindMiss(ctx context.Context, modelID, prompt string) (*Response, bool, error) {
	if !s.Enabled() {
		return nil, false, nil
	}
	// Embed OUTSIDE the lock (mutexio): the embedder is an LLM-tier
	// dependency that can be slow or quota-blocked.
	vec, err := s.embedder.Embed(ctx, semanticPromptText(modelID, prompt))
	if err != nil || len(vec) == 0 {
		// Exact-only degradation: embedder failure (including a
		// quota-blocked embedder) never surfaces and never parks a turn.
		return nil, false, nil
	}

	now := time.Now()
	part := s.partition(modelID)
	// Strict-greater compare against the floor: exact ties stay out.
	bestScore := float32(s.cfg.Threshold)
	var bestKey string
	var bestResp *Response
	// Two-stage verifier state (leaf 03): the best in-band candidate's
	// stored embedding. The stage-2 re-embed happens OUTSIDE the lock
	// (mutexio) below.
	verifyFloor := float32(s.cfg.VerifyFloor)
	var inBandVec []float32

	s.mu.Lock()
	defer s.mu.Unlock()

	// Pure in-memory scan under the store lock (no I/O): nearest-neighbor
	// over a bounded (MaxEntries) set, expiring stale rows as encountered.
	// Selection prefers the strictly-best scorer; an in-band candidate is
	// tracked only when NO candidate already cleared the threshold (the
	// verify pass costs an embed call -- never spent when stage 1 already
	// decided).
	for key, e := range s.entries {
		if e.partition != part || len(e.vector) != len(vec) {
			continue
		}
		if now.After(e.expiresAt) {
			s.removeLocked(key)
			continue
		}
		if score := cosineSimilarity(vec, e.vector); score > bestScore {
			// Serve-side tool-call exemption (leaf 02 hard
			// constraint): never serve a response carrying tool
			// calls, even if a dirty/legacy row made it into the
			// store. The candidate is skipped, not just deprioritized.
			if e.response == nil || e.response.HasToolCalls() {
				continue
			}
			bestScore = score
			bestKey = key
			bestResp = e.response
		} else if bestResp == nil && verifyFloor > 0 && score >= verifyFloor && score < float32(s.cfg.Threshold) && len(e.vector) == len(vec) {
			// Stage-1 in-band candidate: remember the embedding for the
			// stage-2 direct verify. Ties resolve by the map range; the
			// bound on entries keeps the ambiguity cost-free.
			inBandVec = e.vector
			bestKey = key
		}
	}
	if bestResp == nil {
		if inBandVec == nil {
			return nil, false, nil
		}
		// STAGE 2 (leaf 03 two-stage verifier): the re-embed must run
		// OUTSIDE the lock (mutexio -- no I/O under mutex). verifyBehindBand
		// unlocks, embeds, re-locks, and re-checks the candidate before
		// serving (entries may change during the window).
		return s.verifyBehindBand(ctx, modelID, prompt, part, bestKey)
	}
	s.touchLocked(bestKey)
	return bestResp, true, nil
}

// verifyBehindBand is the lock-free stage-2 window of the two-stage
// verifier: re-embeds the query (same embedder, same embedded text),
// direct-cosines it against the candidate's stored embedding, and serves
// only when the direct score clears the threshold. ANY failure shape
// (embedder error, empty vector, candidate evicted/expired mid-verify)
// degrades to a miss -- never an error, never wrong content, never a
// parked turn.
func (s *SemanticCache) verifyBehindBand(ctx context.Context, modelID, prompt, part string, key string) (*Response, bool, error) {
	s.mu.Unlock()
	verifyVec, err := s.embedder.Embed(ctx, semanticPromptText(modelID, prompt))
	s.mu.Lock()
	if err != nil || len(verifyVec) == 0 {
		return nil, false, nil
	}
	e, ok := s.entries[key]
	if !ok || e.partition != part {
		return nil, false, nil
	}
	now := time.Now()
	if now.After(e.expiresAt) {
		s.removeLocked(key)
		return nil, false, nil
	}
	if e.response == nil || e.response.HasToolCalls() {
		return nil, false, nil
	}
	// Direct cosine against the STORED embedding, no cache traversal.
	if cosineSimilarity(verifyVec, e.vector) < float32(s.cfg.Threshold) {
		return nil, false, nil
	}
	s.touchLocked(key)
	return e.response, true, nil
}

// Store records the prompt vector + response after a LIVE exact-miss
// response completes. Tool-call-bearing responses are skipped entirely so
// they can never be served semantically later. Best-effort: an embedder
// failure here is silent (storage is a bonus, never a correctness path)
// and never disturbs the response/rotation flow.
func (s *SemanticCache) Store(ctx context.Context, modelID, prompt string, response *Response) {
	if !s.Enabled() || response == nil || response.HasToolCalls() {
		return
	}
	vec, err := s.embedder.Embed(ctx, semanticPromptText(modelID, prompt))
	if err != nil || len(vec) == 0 {
		return
	}

	part := s.partition(modelID)
	key := part + " " + semanticTextHash(modelID, prompt)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.entries[key]; exists {
		s.removeLocked(key)
	}
	s.entries[key] = &semanticEntry{
		vector:    vec,
		response:  response,
		partition: part,
		expiresAt: now.Add(s.cfg.TTL),
	}
	s.lru.PushFront(key)
	// Evict AFTER the insert: evicting first runs while len(entries) is
	// still at the bound (loop no-op) and the insert then pushes past it,
	// leaving MaxEntries permanently exceeded (pinned by
	// TestSemanticCacheMaxEntriesLRU).
	s.evictLocked(now)
}

// partition is the (model, instruction-fingerprint) partition key
// (findings 1.3 item 5: answers never leak across models or scopes).
func (s *SemanticCache) partition(modelID string) string {
	if s.cfg.PartitionKey == "" {
		return modelID
	}
	return modelID + "|" + s.cfg.PartitionKey
}

// evictLocked drops expired entries then enforces MaxEntries by LRU.
// Callers hold s.mu.
func (s *SemanticCache) evictLocked(now time.Time) {
	for key, e := range s.entries {
		if now.After(e.expiresAt) {
			s.removeLocked(key)
		}
	}
	for len(s.entries) > s.cfg.MaxEntries {
		if tail := s.lru.Back(); tail != nil {
			if key, ok := tail.Value.(string); ok {
				s.removeLocked(key)
				continue
			}
		}
		break
	}
}

// removeLocked deletes an entry and its LRU node. Callers hold s.mu.
func (s *SemanticCache) removeLocked(key string) {
	if _, ok := s.entries[key]; !ok {
		return
	}
	delete(s.entries, key)
	for el := s.lru.Front(); el != nil; el = el.Next() {
		if k, ok := el.Value.(string); ok && k == key {
			s.lru.Remove(el)
			break
		}
	}
}

// touchLocked moves an entry's LRU node to front. Callers hold s.mu.
func (s *SemanticCache) touchLocked(key string) {
	for el := s.lru.Front(); el != nil; el = el.Next() {
		if k, ok := el.Value.(string); ok && k == key {
			s.lru.MoveToFront(el)
			return
		}
	}
}

// Len reports the live entry count (wiring/metrics helper). Nil-safe.
func (s *SemanticCache) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// semanticPromptText is the text embedded for a lookup/store. The model id
// is PREPENDED so identical prompts under different models land in
// different vector neighborhoods (defense in depth behind the partition).
func semanticPromptText(modelID, prompt string) string {
	return modelID + "\n" + strings.TrimSpace(prompt)
}

// semanticTextHash is the deterministic storage key for a (model, prompt)
// pair.
func semanticTextHash(modelID, prompt string) string {
	sum := sha256.Sum256([]byte(semanticPromptText(modelID, prompt)))
	return hex.EncodeToString(sum[:])
}

// cosineSimilarity computes cosine similarity between two float32 vectors
// (stdlib-only dot products + norms).
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
