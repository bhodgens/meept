package skills

import (
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
)

// CapabilityMatch holds a skill match result with confidence scoring.
type CapabilityMatch struct {
	Entry      *SkillIndexEntry
	Score      float64
	Confidence float64 // Normalized to [0.0, 1.0]
	Matches    []KeywordMatch
}

// KeywordMatch describes how a keyword matched.
type KeywordMatch struct {
	Keyword string
	Source  KeywordSource
	Weight  float64
}

// ScoredSkillEntry holds a skill entry with its keyword weight.
type ScoredSkillEntry struct {
	Entry  *SkillIndexEntry
	Weight float64
	Source KeywordSource
}

// CapabilityIndex provides fast, metadata-driven skill lookup.
// All indices are built from skill metadata - no hardcoded mappings.
type CapabilityIndex struct {
	mu sync.RWMutex

	// Inverted index: keyword -> skills with scores
	byKeyword map[string][]*ScoredSkillEntry

	// IDF weights for keywords (inverse document frequency)
	idfWeights map[string]float64

	// Source index reference
	skillIndex *SkillIndex

	// Extractor for keyword generation
	extractor *KeywordExtractor

	// Total number of indexed skills (for IDF calculation)
	totalSkills int

	logger *slog.Logger
}

// CapabilityIndexOption is a functional option for CapabilityIndex.
type CapabilityIndexOption func(*CapabilityIndex)

// WithCapabilityLogger sets the logger.
func WithCapabilityLogger(logger *slog.Logger) CapabilityIndexOption {
	return func(ci *CapabilityIndex) {
		ci.logger = logger
	}
}

// NewCapabilityIndex creates a new empty capability index.
func NewCapabilityIndex(opts ...CapabilityIndexOption) *CapabilityIndex {
	ci := &CapabilityIndex{
		byKeyword:  make(map[string][]*ScoredSkillEntry),
		idfWeights: make(map[string]float64),
		extractor:  NewKeywordExtractor(),
		logger:     slog.Default(),
	}

	for _, opt := range opts {
		opt(ci)
	}

	return ci
}

// BuildFromIndex constructs a capability index from a skill index.
func BuildCapabilityIndex(skillIndex *SkillIndex, opts ...CapabilityIndexOption) *CapabilityIndex {
	ci := NewCapabilityIndex(opts...)
	ci.skillIndex = skillIndex

	entries := skillIndex.List()
	ci.totalSkills = len(entries)

	// First pass: extract keywords and count document frequency
	docFreq := make(map[string]int) // keyword -> number of skills containing it

	entryKeywords := make(map[string][]ExtractedKeyword) // entry name -> keywords

	for _, entry := range entries {
		keywords := ci.extractor.ExtractFromEntry(entry)
		entryKeywords[entry.Name] = keywords

		// Count unique keywords per entry
		seen := make(map[string]bool)
		for _, kw := range keywords {
			if !seen[kw.Keyword] {
				docFreq[kw.Keyword]++
				seen[kw.Keyword] = true
			}
		}
	}

	// Calculate IDF weights
	for keyword, df := range docFreq {
		// IDF = log(N / df) where N is total docs, df is docs containing term
		ci.idfWeights[keyword] = math.Log(float64(ci.totalSkills+1) / float64(df+1))
	}

	// Second pass: build inverted index with TF-IDF-like weights
	for _, entry := range entries {
		keywords := entryKeywords[entry.Name]

		for _, kw := range keywords {
			// Combine source weight with IDF
			idf := ci.idfWeights[kw.Keyword]
			if idf == 0 {
				idf = 1.0
			}
			combinedWeight := kw.Weight * idf

			ci.byKeyword[kw.Keyword] = append(ci.byKeyword[kw.Keyword], &ScoredSkillEntry{
				Entry:  entry,
				Weight: combinedWeight,
				Source: kw.Source,
			})
		}
	}

	ci.logger.Info("Built capability index",
		"skills", ci.totalSkills,
		"keywords", len(ci.byKeyword),
	)

	return ci
}

// Match finds skills relevant to the input, ranked by confidence.
func (ci *CapabilityIndex) Match(input string, limit int) []*CapabilityMatch {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	if len(ci.byKeyword) == 0 {
		return nil
	}

	inputLower := strings.ToLower(input)
	inputWords := strings.Fields(inputLower)

	// Score accumulator per skill
	scores := make(map[string]*CapabilityMatch)

	// Check each keyword in the index
	for keyword, entries := range ci.byKeyword {
		// Check if keyword appears in input
		if !ci.keywordMatches(inputLower, keyword) {
			continue
		}

		// Add scores for matching skills
		for _, se := range entries {
			skillKey := se.Entry.Name

			if scores[skillKey] == nil {
				scores[skillKey] = &CapabilityMatch{
					Entry:   se.Entry,
					Matches: make([]KeywordMatch, 0),
				}
			}

			scores[skillKey].Score += se.Weight
			scores[skillKey].Matches = append(scores[skillKey].Matches, KeywordMatch{
				Keyword: keyword,
				Source:  se.Source,
				Weight:  se.Weight,
			})
		}
	}

	if len(scores) == 0 {
		return nil
	}

	// Convert to slice and calculate confidence
	results := make([]*CapabilityMatch, 0, len(scores))
	maxScore := 0.0

	for _, match := range scores {
		if match.Score > maxScore {
			maxScore = match.Score
		}
		results = append(results, match)
	}

	// Normalize confidence based on max score and input length
	for _, match := range results {
		// Confidence factors:
		// 1. Relative score (vs max)
		// 2. Number of matched keywords
		// 3. Input length penalty (longer inputs need more matches)
		relativeScore := match.Score / maxScore
		matchRatio := float64(len(match.Matches)) / float64(len(inputWords)+1)

		match.Confidence = (relativeScore*0.7 + matchRatio*0.3)

		// Cap at 0.95 - leave room for uncertainty
		if match.Confidence > 0.95 {
			match.Confidence = 0.95
		}
	}

	// Sort by confidence descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Confidence > results[j].Confidence
	})

	// Apply limit
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

// keywordMatches checks if a keyword appears in the input.
func (ci *CapabilityIndex) keywordMatches(inputLower, keyword string) bool {
	// For multi-word keywords (phrases), check exact containment
	if strings.Contains(keyword, " ") {
		return strings.Contains(inputLower, keyword)
	}

	// For single words, check word boundaries (avoid partial matches)
	// Simple approach: check if it's a substring (could be improved with word boundaries)
	return strings.Contains(inputLower, keyword)
}

// MatchWithThreshold returns matches above a confidence threshold.
func (ci *CapabilityIndex) MatchWithThreshold(input string, threshold float64, limit int) []*CapabilityMatch {
	matches := ci.Match(input, 0) // Get all matches first

	var filtered []*CapabilityMatch
	for _, m := range matches {
		if m.Confidence >= threshold {
			filtered = append(filtered, m)
		}
	}

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered
}

// GetTopMatch returns the best match if confidence is above threshold.
func (ci *CapabilityIndex) GetTopMatch(input string, minConfidence float64) *CapabilityMatch {
	matches := ci.Match(input, 1)
	if len(matches) == 0 {
		return nil
	}

	if matches[0].Confidence >= minConfidence {
		return matches[0]
	}

	return nil
}

// KeywordCount returns the number of indexed keywords.
func (ci *CapabilityIndex) KeywordCount() int {
	ci.mu.RLock()
	defer ci.mu.RUnlock()
	return len(ci.byKeyword)
}

// SkillCount returns the number of indexed skills.
func (ci *CapabilityIndex) SkillCount() int {
	return ci.totalSkills
}

// GetKeywordsForSkill returns all keywords indexed for a skill.
func (ci *CapabilityIndex) GetKeywordsForSkill(skillName string) []string {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	var keywords []string

	for keyword, entries := range ci.byKeyword {
		for _, e := range entries {
			if strings.EqualFold(e.Entry.Name, skillName) {
				keywords = append(keywords, keyword)
				break
			}
		}
	}

	return keywords
}

// Rebuild reconstructs the index from the skill index.
func (ci *CapabilityIndex) Rebuild() {
	if ci.skillIndex == nil {
		return
	}

	ci.mu.Lock()
	defer ci.mu.Unlock()

	// Clear existing data
	ci.byKeyword = make(map[string][]*ScoredSkillEntry)
	ci.idfWeights = make(map[string]float64)

	// Rebuild (similar to BuildFromIndex but in-place)
	entries := ci.skillIndex.List()
	ci.totalSkills = len(entries)

	docFreq := make(map[string]int)
	entryKeywords := make(map[string][]ExtractedKeyword)

	for _, entry := range entries {
		keywords := ci.extractor.ExtractFromEntry(entry)
		entryKeywords[entry.Name] = keywords

		seen := make(map[string]bool)
		for _, kw := range keywords {
			if !seen[kw.Keyword] {
				docFreq[kw.Keyword]++
				seen[kw.Keyword] = true
			}
		}
	}

	for keyword, df := range docFreq {
		ci.idfWeights[keyword] = math.Log(float64(ci.totalSkills+1) / float64(df+1))
	}

	for _, entry := range entries {
		keywords := entryKeywords[entry.Name]
		for _, kw := range keywords {
			idf := ci.idfWeights[kw.Keyword]
			if idf == 0 {
				idf = 1.0
			}
			combinedWeight := kw.Weight * idf

			ci.byKeyword[kw.Keyword] = append(ci.byKeyword[kw.Keyword], &ScoredSkillEntry{
				Entry:  entry,
				Weight: combinedWeight,
				Source: kw.Source,
			})
		}
	}

	ci.logger.Debug("Rebuilt capability index",
		"skills", ci.totalSkills,
		"keywords", len(ci.byKeyword),
	)
}

// Stats returns index statistics.
func (ci *CapabilityIndex) Stats() map[string]int {
	ci.mu.RLock()
	defer ci.mu.RUnlock()

	return map[string]int{
		"skills":   ci.totalSkills,
		"keywords": len(ci.byKeyword),
	}
}
