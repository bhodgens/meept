package skills

import (
	"regexp"
	"strings"
	"unicode"
)

// KeywordSource indicates where a keyword was extracted from.
type KeywordSource string

const (
	SourceName        KeywordSource = "name"
	SourceTag         KeywordSource = "tag"
	SourceExample     KeywordSource = "example"
	SourceDescription KeywordSource = "description"
)

// SourceWeights defines the relevance weight for each keyword source.
var SourceWeights = map[KeywordSource]float64{
	SourceName:        1.0,
	SourceTag:         0.9,
	SourceExample:     0.8,
	SourceDescription: 0.5,
}

// ExtractedKeyword holds a keyword with its source and weight.
type ExtractedKeyword struct {
	Keyword string
	Source  KeywordSource
	Weight  float64
}

// KeywordExtractor extracts routing keywords from skill metadata.
type KeywordExtractor struct {
	stopWords    map[string]bool
	minWordLen   int
	maxPhraseLen int
}

// NewKeywordExtractor creates a new keyword extractor.
func NewKeywordExtractor() *KeywordExtractor {
	return &KeywordExtractor{
		stopWords:    defaultStopWords(),
		minWordLen:   3,
		maxPhraseLen: 4, // Max words in a phrase
	}
}

// ExtractFromEntry extracts all keywords from a skill index entry.
func (ke *KeywordExtractor) ExtractFromEntry(entry *SkillIndexEntry) []ExtractedKeyword {
	var keywords []ExtractedKeyword

	// Extract from name (highest weight)
	nameKeywords := ke.extractFromName(entry.Name)
	for _, kw := range nameKeywords {
		keywords = append(keywords, ExtractedKeyword{
			Keyword: kw,
			Source:  SourceName,
			Weight:  SourceWeights[SourceName],
		})
	}

	// Extract from tags
	for _, tag := range entry.Tags {
		tagLower := strings.ToLower(strings.TrimSpace(tag))
		if tagLower != "" {
			keywords = append(keywords, ExtractedKeyword{
				Keyword: tagLower,
				Source:  SourceTag,
				Weight:  SourceWeights[SourceTag],
			})
		}
	}

	// Extract from examples
	for _, example := range entry.Examples {
		exampleKeywords := ke.extractFromText(example)
		for _, kw := range exampleKeywords {
			keywords = append(keywords, ExtractedKeyword{
				Keyword: kw,
				Source:  SourceExample,
				Weight:  SourceWeights[SourceExample],
			})
		}
		// Also extract the full example as a phrase (for exact matching)
		phrase := strings.ToLower(strings.TrimSpace(example))
		if len(phrase) > 0 && len(strings.Fields(phrase)) <= ke.maxPhraseLen {
			keywords = append(keywords, ExtractedKeyword{
				Keyword: phrase,
				Source:  SourceExample,
				Weight:  SourceWeights[SourceExample] * 1.2, // Boost for full phrase
			})
		}
	}

	// Extract from description
	descKeywords := ke.extractFromText(entry.Description)
	for _, kw := range descKeywords {
		keywords = append(keywords, ExtractedKeyword{
			Keyword: kw,
			Source:  SourceDescription,
			Weight:  SourceWeights[SourceDescription],
		})
	}

	// Deduplicate, keeping highest weight for each keyword
	return ke.deduplicateKeywords(keywords)
}

// extractFromName extracts keywords from a skill name (e.g., "code-review" -> ["code", "review", "code-review"])
func (ke *KeywordExtractor) extractFromName(name string) []string {
	var keywords []string

	nameLower := strings.ToLower(name)
	keywords = append(keywords, nameLower) // Full name

	// Split by common separators
	parts := regexp.MustCompile(`[-_\s]+`).Split(nameLower, -1)
	for _, part := range parts {
		if len(part) >= ke.minWordLen && !ke.stopWords[part] {
			keywords = append(keywords, part)
		}
	}

	return keywords
}

// extractFromText extracts meaningful keywords from text.
func (ke *KeywordExtractor) extractFromText(text string) []string {
	if text == "" {
		return nil
	}

	// Normalize text
	textLower := strings.ToLower(text)

	// Remove punctuation except hyphens within words
	var normalized strings.Builder
	for i, r := range textLower {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r):
			normalized.WriteRune(r)
		case r == '-' && i > 0 && i < len(textLower)-1:
			// Keep hyphens that are between letters
			normalized.WriteRune(r)
		default:
			normalized.WriteRune(' ')
		}
	}

	// Split into words
	words := strings.Fields(normalized.String())

	// Filter and collect keywords
	var keywords []string
	for _, word := range words {
		word = strings.Trim(word, "-")
		if len(word) >= ke.minWordLen && !ke.stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	// Also extract bigrams (two-word phrases) for common patterns
	for i := range len(words) - 1 {
		w1, w2 := words[i], words[i+1]
		if len(w1) >= 2 && len(w2) >= 2 && !ke.stopWords[w1] && !ke.stopWords[w2] {
			bigram := w1 + " " + w2
			keywords = append(keywords, bigram)
		}
	}

	return keywords
}

// deduplicateKeywords removes duplicates, keeping the highest weight for each.
func (ke *KeywordExtractor) deduplicateKeywords(keywords []ExtractedKeyword) []ExtractedKeyword {
	seen := make(map[string]ExtractedKeyword)

	for _, kw := range keywords {
		if existing, ok := seen[kw.Keyword]; ok {
			// Keep the one with higher weight
			if kw.Weight > existing.Weight {
				seen[kw.Keyword] = kw
			}
		} else {
			seen[kw.Keyword] = kw
		}
	}

	result := make([]ExtractedKeyword, 0, len(seen))
	for _, kw := range seen {
		result = append(result, kw)
	}

	return result
}

// defaultStopWords returns common English stop words that shouldn't be keywords.
func defaultStopWords() map[string]bool {
	words := []string{
		// Articles
		"a", "an", "the",
		// Pronouns
		"i", "me", "my", "you", "your", "he", "she", "it", "its", "we", "they",
		"this", "that", "these", "those", "who", "what", "which",
		// Prepositions
		"in", "on", "at", "to", "for", "of", "with", "by", "from", "as", "into",
		"about", "after", "before", "between", "under", "over", "through",
		// Conjunctions
		"and", "or", "but", "if", "then", "so", "than", "because", "while",
		// Verbs (common/auxiliary)
		"is", "are", "was", "were", "be", "been", "being",
		"have", "has", "had", "do", "does", "did", "will", "would", "could", "should",
		"may", "might", "must", "shall", "can",
		// Adverbs
		"not", "very", "just", "also", "only", "now", "here", "there",
		// Other common words
		"please", "want", "need", "like", "use", "using", "used",
		"make", "makes", "made", "get", "gets", "got",
		"all", "any", "some", "no", "yes", "more", "most", "other",
		"how", "when", "where", "why",
	}

	stopWords := make(map[string]bool)
	for _, w := range words {
		stopWords[w] = true
	}
	return stopWords
}

// AddStopWord adds a custom stop word.
func (ke *KeywordExtractor) AddStopWord(word string) {
	ke.stopWords[strings.ToLower(word)] = true
}

// SetMinWordLength sets the minimum word length for extraction.
func (ke *KeywordExtractor) SetMinWordLength(length int) {
	if length > 0 {
		ke.minWordLen = length
	}
}
