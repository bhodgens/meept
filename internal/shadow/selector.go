package shadow

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"strings"
	"time"
)

// Selector chooses relevant few-shot examples for injection.
type Selector struct {
	store  ExamplesStore
	config *ExamplesConfig
	logger *slog.Logger

	// IDF weights for TF-IDF embeddings (computed lazily)
	idfWeights map[string]float64
	docCount   int
}

// TFIDFEmbedding represents a sparse TF-IDF embedding.
type TFIDFEmbedding struct {
	Terms   map[string]float64 `json:"terms"`
	Norm    float64            `json:"norm"`
}

// SelectorOption is a functional option for Selector.
type SelectorOption func(*Selector)

// WithSelectorLogger sets the logger.
func WithSelectorLogger(logger *slog.Logger) SelectorOption {
	return func(s *Selector) {
		s.logger = logger
	}
}

// NewSelector creates a new example selector.
func NewSelector(store ExamplesStore, config *ExamplesConfig, opts ...SelectorOption) *Selector {
	s := &Selector{
		store:  store,
		config: config,
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ScoredExample pairs an example with its selection score.
type ScoredExample struct {
	Example         *FewShotExample
	SimilarityScore float64
	RecencyScore    float64
	QualityScore    float64
	TotalScore      float64
	MMRScore        float64 // Maximal Marginal Relevance score
}

// SelectExamples selects the most relevant examples for the given query.
func (s *Selector) SelectExamples(ctx context.Context, query string, domain Domain, taskType TaskType, count int) ([]*FewShotExample, error) {
	if !s.config.Enabled {
		return nil, nil
	}

	if count <= 0 {
		count = s.config.DefaultCount
	}
	if count > s.config.MaxCount {
		count = s.config.MaxCount
	}

	// Get candidate examples
	candidates, err := s.getCandidates(ctx, query, domain, taskType)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Score each candidate
	scored := s.scoreExamples(candidates, query)

	// Apply MMR for diversity
	selected := s.selectWithMMR(scored, query, count)

	// Update usage counts asynchronously
	go s.updateUsageCounts(context.Background(), selected)

	return selected, nil
}

func (s *Selector) getCandidates(ctx context.Context, query string, domain Domain, taskType TaskType) ([]*FewShotExample, error) {
	// First try similarity search
	candidates, err := s.store.SearchSimilar(ctx, query, domain, taskType, s.config.MaxPerCategory)
	if err != nil {
		s.logger.Warn("Similarity search failed, falling back to list", "error", err)
		// Fall back to listing examples
		candidates, err = s.store.ListExamples(ctx, domain, taskType)
		if err != nil {
			return nil, err
		}
	}

	// If domain-specific search returned few results, also search without domain filter
	if len(candidates) < s.config.DefaultCount {
		allCandidates, err := s.store.SearchSimilar(ctx, query, "", "", s.config.MaxPerCategory)
		if err == nil {
			// Add unique candidates
			seen := make(map[string]bool)
			for _, c := range candidates {
				seen[c.ID] = true
			}
			for _, c := range allCandidates {
				if !seen[c.ID] {
					candidates = append(candidates, c)
				}
			}
		}
	}

	return candidates, nil
}

func (s *Selector) scoreExamples(candidates []*FewShotExample, query string) []*ScoredExample {
	scored := make([]*ScoredExample, len(candidates))
	now := time.Now()

	for i, example := range candidates {
		se := &ScoredExample{Example: example}

		// Try embedding-based similarity first, fall back to text-based
		embSim := s.computeEmbeddingSimilarity(query, example)
		textSim := s.computeSimilarity(query, example.UserMessage)

		// Combine embedding and text similarity (prefer embedding if available)
		if embSim > 0 {
			se.SimilarityScore = embSim*0.7 + textSim*0.3
		} else {
			se.SimilarityScore = textSim
		}

		// Recency score (decay over time)
		age := now.Sub(example.CreatedAt)
		se.RecencyScore = s.computeRecencyScore(age)

		// Quality score (normalized)
		se.QualityScore = example.QualityScore

		// Compute weighted total
		se.TotalScore = se.SimilarityScore*s.config.SimilarityWeight +
			se.RecencyScore*s.config.RecencyWeight +
			se.QualityScore*s.config.QualityWeight

		scored[i] = se
	}

	return scored
}

func (s *Selector) computeSimilarity(query, example string) float64 {
	// Jaccard similarity on word sets
	queryWords := tokenize(query)
	exampleWords := tokenize(example)

	if len(queryWords) == 0 || len(exampleWords) == 0 {
		return 0
	}

	// Count intersection and union
	intersection := 0
	querySet := make(map[string]bool)
	for _, w := range queryWords {
		querySet[w] = true
	}

	exampleSet := make(map[string]bool)
	for _, w := range exampleWords {
		exampleSet[w] = true
		if querySet[w] {
			intersection++
		}
	}

	union := len(querySet) + len(exampleSet) - intersection
	if union == 0 {
		return 0
	}

	jaccard := float64(intersection) / float64(union)

	// Also consider n-gram similarity for partial matches
	bigramSim := s.computeBigramSimilarity(query, example)

	// Combine with more weight on bigram similarity
	return jaccard*0.4 + bigramSim*0.6
}

func (s *Selector) computeBigramSimilarity(a, b string) float64 {
	aBigrams := getBigrams(strings.ToLower(a))
	bBigrams := getBigrams(strings.ToLower(b))

	if len(aBigrams) == 0 || len(bBigrams) == 0 {
		return 0
	}

	// Count matching bigrams
	matches := 0
	bSet := make(map[string]bool)
	for _, bg := range bBigrams {
		bSet[bg] = true
	}

	for _, bg := range aBigrams {
		if bSet[bg] {
			matches++
		}
	}

	// Dice coefficient
	return 2.0 * float64(matches) / float64(len(aBigrams)+len(bBigrams))
}

func (s *Selector) computeRecencyScore(age time.Duration) float64 {
	// Exponential decay with half-life of 7 days
	halfLife := 7 * 24 * time.Hour
	decayRate := math.Log(2) / float64(halfLife)
	return math.Exp(-decayRate * float64(age))
}

// selectWithMMR uses Maximal Marginal Relevance to select diverse examples.
// MMR balances relevance to the query with diversity from already-selected examples.
// lambda controls the trade-off: 1.0 = pure relevance, 0.0 = pure diversity
func (s *Selector) selectWithMMR(scored []*ScoredExample, query string, maxCount int) []*FewShotExample {
	if len(scored) == 0 {
		return nil
	}

	lambda := 0.7 // Balance between relevance and diversity
	maxTokens := s.config.MaxContextTokens
	totalTokens := 0

	var selected []*FewShotExample
	var selectedEmbeddings []*TFIDFEmbedding
	remaining := make([]*ScoredExample, len(scored))
	copy(remaining, scored)

	// Compute query embedding once (used for diversity calculation)
	_ = s.ComputeEmbedding(query)

	for len(selected) < maxCount && len(remaining) > 0 {
		bestIdx := -1
		bestMMR := -1.0

		for i, candidate := range remaining {
			if candidate == nil {
				continue
			}

			// Check token budget
			exampleTokens := (len(candidate.Example.UserMessage) + len(candidate.Example.AssistantResponse)) / 4
			if totalTokens+exampleTokens > maxTokens {
				continue
			}

			// Relevance score (similarity to query)
			relevance := candidate.TotalScore

			// Diversity score (max similarity to any already selected)
			maxSim := 0.0
			candEmb := s.ComputeEmbedding(candidate.Example.UserMessage)
			for _, selEmb := range selectedEmbeddings {
				sim := CosineSimilarity(candEmb, selEmb)
				if sim > maxSim {
					maxSim = sim
				}
			}

			// MMR score: lambda * relevance - (1 - lambda) * max_similarity_to_selected
			mmr := lambda*relevance - (1-lambda)*maxSim

			if mmr > bestMMR {
				bestMMR = mmr
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break // No more candidates fit
		}

		// Add the best candidate
		best := remaining[bestIdx]
		selected = append(selected, best.Example)
		selectedEmbeddings = append(selectedEmbeddings, s.ComputeEmbedding(best.Example.UserMessage))

		exampleTokens := (len(best.Example.UserMessage) + len(best.Example.AssistantResponse)) / 4
		totalTokens += exampleTokens

		// Remove from remaining
		remaining[bestIdx] = nil
	}

	return selected
}

func (s *Selector) updateUsageCounts(ctx context.Context, examples []*FewShotExample) {
	for _, example := range examples {
		if err := s.store.IncrementUsage(ctx, example.ID); err != nil {
			s.logger.Warn("Failed to update usage count", "id", example.ID, "error", err)
		}
	}
}

// FormatForInjection formats examples for injection into the prompt.
func (s *Selector) FormatForInjection(examples []*FewShotExample) []Message {
	if len(examples) == 0 {
		return nil
	}

	var messages []Message

	// Add a system message introducing the examples
	messages = append(messages, Message{
		Role: "system",
		Content: "Here are some relevant example interactions that demonstrate the expected response style and quality:\n",
	})

	for i, example := range examples {
		// Add user message
		messages = append(messages, Message{
			Role:    "user",
			Content: formatExampleHeader(i+1, example.UserMessage),
		})

		// Add assistant response
		messages = append(messages, Message{
			Role:    "assistant",
			Content: example.AssistantResponse,
		})
	}

	// Add separator
	messages = append(messages, Message{
		Role:    "system",
		Content: "---\nNow respond to the actual user query:",
	})

	return messages
}

// Helper functions

func tokenize(text string) []string {
	// Lowercase and split on non-alphanumeric
	lower := strings.ToLower(text)
	words := strings.FieldsFunc(lower, func(c rune) bool {
		return !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'))
	})

	// Filter short words and stopwords
	var filtered []string
	for _, w := range words {
		if len(w) >= 3 && !isStopWord(w) {
			filtered = append(filtered, w)
		}
	}

	return filtered
}

func getBigrams(text string) []string {
	words := strings.Fields(text)
	if len(words) < 2 {
		return nil
	}

	bigrams := make([]string, len(words)-1)
	for i := 0; i < len(words)-1; i++ {
		bigrams[i] = words[i] + " " + words[i+1]
	}

	return bigrams
}

func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true, "but": true,
		"not": true, "you": true, "all": true, "can": true, "had": true,
		"her": true, "was": true, "one": true, "our": true, "out": true,
		"has": true, "have": true, "been": true, "would": true, "could": true,
		"this": true, "that": true, "with": true, "from": true, "they": true,
		"what": true, "when": true, "where": true, "which": true, "there": true,
	}
	return stopWords[word]
}

func formatExampleHeader(num int, content string) string {
	return "Example " + itoa(num) + ":\n" + content
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// ComputeEmbedding computes a TF-IDF embedding for the given text.
func (s *Selector) ComputeEmbedding(text string) *TFIDFEmbedding {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return &TFIDFEmbedding{Terms: make(map[string]float64)}
	}

	// Compute term frequencies
	tf := make(map[string]int)
	for _, t := range tokens {
		tf[t]++
	}

	// Compute TF-IDF weights
	embedding := &TFIDFEmbedding{Terms: make(map[string]float64)}
	var normSquared float64

	for term, count := range tf {
		// TF: log-normalized frequency
		termFreq := 1.0 + math.Log(float64(count))

		// IDF: use precomputed or default
		idf := 1.0
		if s.idfWeights != nil {
			if w, ok := s.idfWeights[term]; ok {
				idf = w
			}
		}

		weight := termFreq * idf
		embedding.Terms[term] = weight
		normSquared += weight * weight
	}

	embedding.Norm = math.Sqrt(normSquared)
	return embedding
}

// CosineSimilarity computes the cosine similarity between two embeddings.
func CosineSimilarity(a, b *TFIDFEmbedding) float64 {
	if a == nil || b == nil || a.Norm == 0 || b.Norm == 0 {
		return 0
	}

	// Dot product
	var dot float64
	for term, weightA := range a.Terms {
		if weightB, ok := b.Terms[term]; ok {
			dot += weightA * weightB
		}
	}

	return dot / (a.Norm * b.Norm)
}

// EmbeddingToJSON serializes an embedding to JSON.
func EmbeddingToJSON(e *TFIDFEmbedding) string {
	if e == nil {
		return ""
	}
	data, err := json.Marshal(e)
	if err != nil {
		return ""
	}
	return string(data)
}

// EmbeddingFromJSON deserializes an embedding from JSON.
func EmbeddingFromJSON(data string) *TFIDFEmbedding {
	if data == "" {
		return nil
	}
	var e TFIDFEmbedding
	if err := json.Unmarshal([]byte(data), &e); err != nil {
		return nil
	}
	return &e
}

// UpdateIDF updates the IDF weights based on a corpus of documents.
func (s *Selector) UpdateIDF(documents []string) {
	if len(documents) == 0 {
		return
	}

	// Count document frequency for each term
	docFreq := make(map[string]int)
	for _, doc := range documents {
		seen := make(map[string]bool)
		for _, term := range tokenize(doc) {
			if !seen[term] {
				docFreq[term]++
				seen[term] = true
			}
		}
	}

	// Compute IDF weights
	s.docCount = len(documents)
	s.idfWeights = make(map[string]float64)
	for term, df := range docFreq {
		// IDF with smoothing: log((N+1)/(df+1)) + 1
		s.idfWeights[term] = math.Log(float64(s.docCount+1)/float64(df+1)) + 1.0
	}
}

// computeEmbeddingSimilarity computes similarity using embeddings if available.
func (s *Selector) computeEmbeddingSimilarity(query string, example *FewShotExample) float64 {
	// Compute query embedding
	queryEmb := s.ComputeEmbedding(query)

	// Try to use stored embedding
	var exampleEmb *TFIDFEmbedding
	if example.EmbeddingJSON != "" {
		exampleEmb = EmbeddingFromJSON(example.EmbeddingJSON)
	}

	// Fall back to computing embedding if not stored
	if exampleEmb == nil {
		exampleEmb = s.ComputeEmbedding(example.UserMessage)
	}

	return CosineSimilarity(queryEmb, exampleEmb)
}
