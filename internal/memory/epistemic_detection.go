package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// DefaultDetectionThreshold is the minimum LLM confidence for an epistemic
// edge to be persisted as a confirmed relationship (contradicts, superseded,
// etc.).
const DefaultDetectionThreshold = 0.7

// PotentialContradictionThreshold is the lower bound below
// DefaultDetectionThreshold. Candidates in
// [PotentialContradictionThreshold, DefaultDetectionThreshold) are written
// as potential_contradicts edges with low weight for review surfacing.
const PotentialContradictionThreshold = 0.4

// ClassifierLLM is the interface the detector uses to classify candidate
// memory pairs. Defined locally to avoid an import cycle on internal/llm.
type ClassifierLLM interface {
	// ClassifyRelationships inspects a new memory against each candidate and
	// returns one EdgeVerdict per relevant pair.
	ClassifyRelationships(ctx context.Context, newMem Memory, candidates []Memory) ([]EdgeVerdict, error)
}

// EdgeVerdict is the classifier's verdict on one (newMem, candidate) pair.
// Exported so adapters in other packages (e.g., daemon classifierAdapter)
// can implement ClassifierLLM.
type EdgeVerdict struct {
	Relation    string  // contradicts, superseded, evidence_for, evidence_against, derives_from, supports, unrelated
	TargetID    string  // candidate memory ID
	Confidence  float64 // 0.0-1.0
	Explanation string  // human-readable rationale
}

// EpistemicDetectorConfig holds construction parameters for EpistemicDetector.
type EpistemicDetectorConfig struct {
	Graph      *KnowledgeGraph
	Manager    *Manager
	Classifier ClassifierLLM
	Embedder   EmbeddingProvider
	Threshold  float64
	AutoWeight float64
	Logger     *slog.Logger
}

// EpistemicDetector identifies relationships between memories using
// embedding similarity and LLM classification.
type EpistemicDetector struct {
	graph      *KnowledgeGraph
	manager    *Manager
	classifier ClassifierLLM
	embedder   EmbeddingProvider
	threshold  float64
	autoWeight float64
	logger     *slog.Logger
}

// NewEpistemicDetector constructs a detector from the given configuration.
// Zero-value fields yield safe defaults.
func NewEpistemicDetector(cfg EpistemicDetectorConfig) *EpistemicDetector {
	d := &EpistemicDetector{
		graph:      cfg.Graph,
		manager:    cfg.Manager,
		classifier: cfg.Classifier,
		embedder:   cfg.Embedder,
		threshold:  cfg.Threshold,
		autoWeight: cfg.AutoWeight,
		logger:     cfg.Logger,
	}
	if d.threshold == 0 {
		d.threshold = DefaultDetectionThreshold
	}
	if d.autoWeight == 0 {
		d.autoWeight = DefaultAutoClaimTrustWeight
	}
	if d.logger == nil {
		d.logger = slog.Default()
	}
	return d
}

// DetectRelationships examines a new memory against existing memories and
// returns candidate edges. Does not write edges; caller decides.
//
// Pipeline:
//  1. Gate: classifier/manager nil → return nil
//  2. Gate: non-epistemic type → return nil
//  3. Search top-K similar memories via the manager
//  4. Filter out rejected claims
//  5. Call classifier
//  6. Build edges with threshold/potential routing
func (d *EpistemicDetector) DetectRelationships(ctx context.Context, newMem Memory) ([]MemoryEdge, error) {
	if d == nil || d.classifier == nil || d.manager == nil {
		return nil, nil
	}
	if !IsEpistemicType(newMem.Type) {
		return nil, nil
	}

	// Gather candidates via free-text search over the memory store.
	results, err := d.manager.Search(ctx, MemoryQuery{
		Type:  newMem.Type,
		Query: newMem.Content,
		Limit: 10,
	})
	if err != nil {
		return nil, fmt.Errorf("detector search: %w", err)
	}

	var candidates []Memory
	for _, r := range results {
		if r.Memory.ID == "" || r.Memory.ID == newMem.ID {
			continue
		}
		// Exclude rejected claims — they can't be relationship targets.
		if r.Memory.Type == MemoryTypeClaim {
			if ClaimStatus(asString(r.Memory.Metadata["status"])).IsRejected() {
				continue
			}
		}
		candidates = append(candidates, r.Memory)
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	verdicts, err := d.classifier.ClassifyRelationships(ctx, newMem, candidates)
	if err != nil {
		return nil, fmt.Errorf("classifier: %w", err)
	}

	var edges []MemoryEdge
	for _, v := range verdicts {
		if v.Relation == "" || v.Relation == "unrelated" {
			continue
		}
		edgeType := EdgeType(v.Relation)
		weight := v.Confidence
		finalType := edgeType

		if v.Confidence < d.threshold {
			// Below threshold: only surface as potential_contradicts if it's
			// a contradicts verdict above the lower potential threshold.
			if edgeType == EdgeTypeContradicts && v.Confidence >= PotentialContradictionThreshold {
				finalType = EdgeTypePotentialContradicts
				weight = 0.2
			} else {
				continue
			}
		}

		edges = append(edges, MemoryEdge{
			SourceID:   newMem.ID,
			TargetID:   v.TargetID,
			EdgeType:   finalType,
			Weight:     weight,
			Confidence: v.Confidence,
			Metadata: map[string]any{
				"explanation": v.Explanation,
				"detector":    "epistemic",
			},
		})
	}
	return edges, nil
}

// PersistCandidateEdges writes edges via the detector's graph reference.
// No-op when graph is nil.
func (d *EpistemicDetector) PersistCandidateEdges(ctx context.Context, edges []MemoryEdge) error {
	if d == nil || d.graph == nil || len(edges) == 0 {
		return nil
	}
	for _, e := range edges {
		if err := d.graph.AddEdge(ctx, e); err != nil {
			d.logger.Warn("failed to persist epistemic edge", "error", err, "target", e.TargetID)
		}
	}
	return nil
}

// classifierJSONResponse is the expected JSON shape from the LLM classifier.
type classifierJSONResponse struct {
	Relation    string  `json:"relation"`
	TargetID    string  `json:"target_id"`
	Confidence  float64 `json:"confidence"`
	Explanation string  `json:"explanation"`
}

// ParseClassifierJSON parses a JSON array of verdicts from a raw LLM body.
// Useful for adapter implementations.
func ParseClassifierJSON(raw []byte) ([]EdgeVerdict, error) {
	trimmed := stripCodeFences(string(raw))
	if strings.TrimSpace(trimmed) == "" {
		return nil, errors.New("empty classifier response")
	}
	var out []classifierJSONResponse
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, fmt.Errorf("parse classifier json: %w", err)
	}
	verdicts := make([]EdgeVerdict, len(out))
	for i, r := range out {
		verdicts[i] = EdgeVerdict(r)
	}
	return verdicts, nil
}

// stripCodeFences removes ```json ... ``` wrappers if the LLM added them.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// drop first line (fence + optional language tag)
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}
