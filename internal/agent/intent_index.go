package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// IntentEntry represents an intent definition for indexing.
type IntentEntry struct {
	IntentType  IntentType
	Description string
	Keywords    []string
}

// SemanticMatch holds a semantic matching result.
type SemanticMatch struct {
	IntentType IntentType `json:"intent_type"`
	Confidence float64    `json:"confidence"`
}

// SemanticIndex provides embedding-based intent matching.
type SemanticIndex struct {
	mu      sync.RWMutex
	client  EmbeddingClient
	entries []IntentEntry
	vectors [][]float64
	ready   bool
}

// NewSemanticIndex creates a new semantic index.
func NewSemanticIndex(client EmbeddingClient) *SemanticIndex {
	return &SemanticIndex{
		client:  client,
		entries: make([]IntentEntry, 0),
		vectors: make([][]float64, 0),
	}
}

func buildIntentText(t IntentType) string {
	keywords := t.Keywords()
	return fmt.Sprintf("Intent %s: handled by %s agent. Keywords: %s",
		string(t),
		t.DefaultAgent(),
		strings.Join(keywords, ", "))
}

// BuildIndex pre-computes embeddings for all intent definitions.
func (idx *SemanticIndex) BuildIndex(ctx context.Context) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	allIntents := []IntentType{
		IntentChat, IntentReport, IntentRecall, IntentPlatform,
		IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit,
		IntentSchedule, IntentAnalyze, IntentSearch, IntentSkill,
	}

	for _, intentType := range allIntents {
		text := buildIntentText(intentType)
		idx.entries = append(idx.entries, IntentEntry{
			IntentType:  intentType,
			Description: text,
			Keywords:    intentType.Keywords(),
		})
	}

	texts := make([]string, len(idx.entries))
	for i, e := range idx.entries {
		texts[i] = e.Description
	}

	vectors, err := idx.client.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("failed to compute intent embeddings: %w", err)
	}

	idx.vectors = vectors
	idx.ready = true
	return nil
}

// Match finds the best matching intent by semantic similarity.
func (idx *SemanticIndex) Match(input string, minConfidence float64) *SemanticMatch {
	if !idx.ready {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	vector, err := idx.client.Embed(context.Background(), input)
	if err != nil {
		return nil
	}

	var bestMatch *SemanticMatch
	bestSimilarity := 0.0

	for i, intentVector := range idx.vectors {
		sim := CosineSimilarity(vector, intentVector)
		if sim > bestSimilarity {
			bestSimilarity = sim
			bestMatch = &SemanticMatch{
				IntentType: idx.entries[i].IntentType,
				Confidence: sim,
			}
		}
	}

	if bestSimilarity >= minConfidence {
		return bestMatch
	}
	return nil
}

// IsReady returns true if the index is built and ready.
func (idx *SemanticIndex) IsReady() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.ready
}
