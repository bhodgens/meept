package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/id"
)

// Bridge generation caps (IndexRAG paper §3.2.2; branch 09 contract 2).
const (
	// MaxBridgeSourceMemories is the maximum number of candidate source
	// memories fed to the generator per bridge entity.
	MaxBridgeSourceMemories = 5
	// MaxBridgeFactsPerSource is the maximum number of extracted facts
	// (entities) kept per source memory when building the prompt.
	MaxBridgeFactsPerSource = 8
)

// BridgeMetaEntities is the memory-metadata key carrying the comma-separated
// entity list a bridge candidate was extracted from. Stage-1 AKU extraction
// (when it exists) writes this key; the v1 fallback extractor reads it and,
// when absent, extracts from Content directly.
const BridgeMetaEntities = "entities"

// BridgeMetaEvidenceContentPrefix prefixes a source memory ID to form the
// bridge-record metadata key carrying that source's content
// ("evidence_content_<id>"). Generation writes it (persistBridgeFacts); the
// B03 entailment verifier reads it (bridgeEvidenceContent). Evidence text is
// capped by maxBridgeEvidenceContentRunes at write time so a large source
// memory cannot bloat the shard metadata.
const BridgeMetaEvidenceContentPrefix = "evidence_content_"

// maxBridgeEvidenceContentRunes caps each evidence_content_<id> metadata
// value. Long enough for a full paragraph; the verifier's purpose is
// entailment checking, not full-document replay.
const maxBridgeEvidenceContentRunes = 2000

// BridgesConfig carries the bridge-generation knob values (memory.bridges.*
// in internal/config). Mirrored here so GenerateBridges stays decoupled from
// the config package's full schema.
type BridgesConfig struct {
	// Enabled gates the entire pass: false (the default) makes
	// GenerateBridges a no-op with zero LLM calls.
	Enabled bool
	// BridgeDFMax is the inclusive upper bound of the bridge-entity
	// document-frequency window (lower bound fixed at 2).
	BridgeDFMax int
	// Verify reports whether entailment verification (B03) should run on
	// generated bridges. Generation only records it on the record.
	Verify bool
}

// ErrBridgeGenerate wraps every generator transport failure so callers can
// distinguish "LLM unavailable" from per-bridge rejections.
var ErrBridgeGenerate = errors.New("bridge generation: llm call failed")

// BridgeGenerator produces grammar-constrained bridging-fact JSON for one
// bridge entity over its candidate sources. Pluggable for tests, mirroring
// the DistillSummarizer pattern.
type BridgeGenerator interface {
	// GenerateBridgeFacts returns the raw model output (a JSON array of
	// {"text", "evidence_ids"} objects) for the given entity.
	GenerateBridgeFacts(ctx context.Context, entity string, sources []Memory) (string, error)
}

// llmBridgeGenerator is the production BridgeGenerator: it wraps the
// manager's llm.Chatter and attaches llm.BridgeGrammar() via WithRawGrammar
// so llama.cpp-style local endpoints constrain output to the decodable shape.
type llmBridgeGenerator struct {
	client llm.Chatter
}

var _ BridgeGenerator = (*llmBridgeGenerator)(nil)

// NewBridgeGenerator builds the production generator over a chatter.
func NewBridgeGenerator(client llm.Chatter) BridgeGenerator {
	return &llmBridgeGenerator{client: client}
}

// GenerateBridgeFacts assembles the capped prompt and calls the chatter with
// the bridge grammar attached.
func (g *llmBridgeGenerator) GenerateBridgeFacts(ctx context.Context, entity string, sources []Memory) (string, error) {
	var sb strings.Builder
	sb.WriteString("You write cross-memory bridging facts. Given several memories that all mention the same entity, produce the connecting facts that link them — information only true or meaningful because the memories share that entity.\n")
	sb.WriteString("Respond with ONLY a JSON array. Each element is an object with keys \"text\" (the bridging fact sentence) and \"evidence_ids\" (the source memory IDs the fact derives from).\n\n")
	sb.WriteString(fmt.Sprintf("Entity: %s\n\nMemories:\n", entity))
	for _, m := range sources {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", m.ID, m.Content))
	}

	msgs := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "You generate bridging facts between memories as constrained JSON."},
		{Role: llm.RoleUser, Content: sb.String()},
	}
	resp, err := g.client.Chat(ctx, msgs, llm.WithRawGrammar(llm.BridgeGrammar()))
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// bridgeFact is one decoded element of the generator's JSON array.
type bridgeFact struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids"`
}

// GenerateBridges runs the bridge-generation pass over the given candidate
// memories (branch 09 leaf B02):
//
//  1. No-op (zero LLM calls) unless cfg.Enabled.
//  2. Extract per-memory facts (entities), cap at MaxBridgeFactsPerSource.
//  3. Compute document frequency per entity; bridge-eligible entities are
//     those with 2 <= df <= cfg.BridgeDFMax.
//  4. Per eligible entity, cap candidates at MaxBridgeSourceMemories, call
//     the generator, decode the JSON array, and persist via PutBridge
//     (evidence validation inherited). When the model cites no evidence_ids,
//     the candidate source IDs are supplied as provenance.
//
// Returns the number of bridges persisted and the number of generator
// responses rejected (malformed JSON, empty after evidence filtering, or
// failed writes). A generator transport error aborts the pass with an error
// wrapping ErrBridgeGenerate.
func GenerateBridges(ctx context.Context, store *BridgeStore, gen BridgeGenerator, cfg BridgesConfig, memories []Memory) (generated int, rejected int, err error) {
	if !cfg.Enabled || store == nil || gen == nil {
		return 0, 0, nil
	}
	maxDF := cfg.BridgeDFMax
	if maxDF <= 0 {
		maxDF = 10
	}

	perMemoryFacts := extractBridgeFacts(memories)
	for _, entity := range eligibleBridgeEntities(perMemoryFacts, maxDF) {
		candidates := bridgeCandidates(memories, perMemoryFacts, entity)
		if len(candidates) == 0 {
			continue
		}
		out, gerr := gen.GenerateBridgeFacts(ctx, entity, candidates)
		if gerr != nil {
			return generated, rejected, fmt.Errorf("%w: entity %q: %w", ErrBridgeGenerate, entity, gerr)
		}
		got, rej := persistBridgeFacts(ctx, store, entity, candidates, out)
		generated += got
		rejected += rej
	}
	return generated, rejected, nil
}

// extractBridgeFacts returns the capped fact list per memory. It prefers the
// Stage-1 `entities` metadata key (comma-separated); memories without it fall
// back to a light v1 extractor over Content.
func extractBridgeFacts(memories []Memory) map[string][]string {
	out := make(map[string][]string, len(memories))
	for _, m := range memories {
		facts := bridgeFactsFromMetadata(m)
		if len(facts) == 0 {
			facts = extractEntitiesV1(m.Content)
		}
		if len(facts) > MaxBridgeFactsPerSource {
			facts = facts[:MaxBridgeFactsPerSource]
		}
		out[m.ID] = facts
	}
	return out
}

// bridgeFactsFromMetadata reads BridgeMetaEntities ("a,b,c" form).
func bridgeFactsFromMetadata(m Memory) []string {
	raw, ok := m.Metadata[BridgeMetaEntities]
	if !ok {
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil
	}
	var facts []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(strings.ToLower(part)); part != "" {
			facts = append(facts, part)
		}
	}
	return facts
}

// v1EntityPattern is the light fallback extractor: capitalized words,
// camelCase identifiers, and snake_case tokens count as candidate entities.
// Accepted per the leaf spec ("a regex/light extractor is acceptable for v1").
var v1EntityPattern = regexp.MustCompile(`[A-Z][a-zA-Z0-9_]*|_[a-z][a-z0-9_]*|\b[a-z]+_[a-z0-9_]+\b`)

// extractEntitiesV1 runs the fallback extractor and dedupes case-insensitively.
func extractEntitiesV1(content string) []string {
	matches := v1EntityPattern.FindAllString(content, -1)
	seen := make(map[string]bool, len(matches))
	var facts []string
	for _, m := range matches {
		key := strings.ToLower(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		facts = append(facts, key)
	}
	return facts
}

// eligibleBridgeEntities returns entities whose document frequency is inside
// [2, maxDF], sorted for deterministic pass ordering.
func eligibleBridgeEntities(perMemory map[string][]string, maxDF int) []string {
	df := make(map[string]int)
	for _, facts := range perMemory {
		seen := make(map[string]bool, len(facts))
		for _, f := range facts {
			if !seen[f] {
				seen[f] = true
				df[f]++
			}
		}
	}
	var eligible []string
	for e, n := range df {
		if n >= 2 && n <= maxDF {
			eligible = append(eligible, e)
		}
	}
	sort.Strings(eligible)
	return eligible
}

// bridgeCandidates returns the (capped) memories mentioning entity, in input
// order for determinism.
func bridgeCandidates(memories []Memory, perMemory map[string][]string, entity string) []Memory {
	var out []Memory
	for _, m := range memories {
		for _, f := range perMemory[m.ID] {
			if f == entity {
				out = append(out, m)
				break
			}
		}
		if len(out) == MaxBridgeSourceMemories {
			break
		}
	}
	return out
}

// persistBridgeFacts decodes the generator output and persists each fact via
// PutBridge. Empty/missing evidence_ids fall back to the candidate source
// IDs; evidence IDs unknown to the candidate set are dropped, and a fact left
// with none is rejected. Returns (persisted, rejected) counts.
func persistBridgeFacts(ctx context.Context, store *BridgeStore, entity string, candidates []Memory, output string) (int, int) {
	generated, rejected := 0, 0
	var facts []bridgeFact
	if err := json.Unmarshal([]byte(output), &facts); err != nil {
		return 0, 1
	}
	candidateIDs := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		candidateIDs[c.ID] = true
	}
	// evidence_content_<id> carries each source's text so the B03 entailment
	// verifier (bridgeEvidenceContent) can show the model what the fact must
	// be grounded in. Without it, verify=true (the default) prompts with
	// empty excerpts and deletes every bridge as unsupported
	// (bughunt 2026-10-14).
	evidenceContent := make(map[string]any, len(candidates))
	for _, c := range candidates {
		evidenceContent[BridgeMetaEvidenceContentPrefix+c.ID] = truncateBridgeEvidenceContent(c.Content)
	}
	for _, fact := range facts {
		text := strings.TrimSpace(fact.Text)
		if text == "" {
			rejected++
			continue
		}
		evIDs := filterBridgeEvidence(fact.EvidenceIDs, candidateIDs)
		if len(evIDs) == 0 {
			if len(strings.Join(fact.EvidenceIDs, "")) == 0 {
				// Model cited nothing: provenance falls back to the
				// sources the fact was generated FROM (branch rule:
				// no provenance, no ship — the fallback preserves it).
				for _, c := range candidates {
					evIDs = append(evIDs, c.ID)
				}
			} else {
				// Model cited only IDs outside the candidate set:
				// hallucinated provenance — reject rather than silently
				// rewrite what the model claimed.
				rejected++
				continue
			}
		}
		md := make(map[string]any, 1+len(evidenceContent))
		md["entity"] = entity
		for k, v := range evidenceContent {
			md[k] = v
		}
		rec := BridgeRecord{
			ID:          id.Generate("bridge-"),
			Content:     text,
			Embedding:   hashBridgeEmbedding(text),
			EvidenceIDs: evIDs,
			CreatedAt:   time.Now().UTC(),
			Verified:    false, // B03 entailment flips this
			Metadata:    md,
		}
		if err := store.PutBridge(ctx, rec); err != nil {
			rejected++
			continue
		}
		generated++
	}
	return generated, rejected
}

// truncateBridgeEvidenceContent caps evidence text at
// maxBridgeEvidenceContentRunes (rune-safe) for the bridge metadata.
func truncateBridgeEvidenceContent(content string) string {
	runes := []rune(content)
	if len(runes) <= maxBridgeEvidenceContentRunes {
		return content
	}
	return string(runes[:maxBridgeEvidenceContentRunes])
}

// filterBridgeEvidence keeps only IDs present in the candidate set, trimmed.
func filterBridgeEvidence(ids []string, candidates map[string]bool) []string {
	var out []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && candidates[id] {
			out = append(out, id)
		}
	}
	return out
}

// hashBridgeEmbedding derives a deterministic 256-dim embedding from the
// fact text for v1 (B02). IndexRAG's recall path (B04) treats bridges as a
// capped supplementary pool, so a lexical-hash embedding is adequate until a
// semantic embedder is wired here; the record contract (embedding present,
// correct dimension) is what B04 depends on.
func hashBridgeEmbedding(text string) []float32 {
	dim := 256
	v := make([]float32, dim)
	for _, b := range []byte(strings.ToLower(text)) {
		v[int(b)%dim] += 1
	}
	return v
}
