// Package components provides reusable TUI components.
package components

import (
	"sort"
	"strings"
	"unicode"
)

// FuzzyMatch represents a fuzzy match result.
type FuzzyMatch struct {
	Item    any
	Text    string // Original text that was matched
	Score   float64
	Indices []int // Matched character indices for highlighting
}

// FuzzyMatcher performs fuzzy matching against a list of string items.
type FuzzyMatcher struct {
	items []fuzzyItem
}

type fuzzyItem struct {
	data  any
	text  string
	lower string
	words []string // Pre-split words for scoring
}

// NewFuzzyMatcher creates a new fuzzy matcher with the given items.
// Each item is a (text, data) pair where text is searchable and data is any payload.
func NewFuzzyMatcher(items []struct {
	Text string
	Data any
}) *FuzzyMatcher {
	fm := &FuzzyMatcher{
		items: make([]fuzzyItem, len(items)),
	}
	for i, item := range items {
		lower := strings.ToLower(item.Text)
		fm.items[i] = fuzzyItem{
			data:  item.Data,
			text:  item.Text,
			lower: lower,
			words: splitWords(lower),
		}
	}
	return fm
}

// Match performs fuzzy matching against the query and returns scored results.
func (fm *FuzzyMatcher) Match(query string) []FuzzyMatch {
	if query == "" {
		// Return all items with equal score
		results := make([]FuzzyMatch, len(fm.items))
		for i, item := range fm.items {
			results[i] = FuzzyMatch{
				Item:  item.data,
				Text:  item.text,
				Score: 1.0,
			}
		}
		return results
	}

	qLower := strings.ToLower(query)
	var matches []FuzzyMatch

	for _, item := range fm.items {
		score, indices := fm.score(item, qLower)
		if score > 0 {
			matches = append(matches, FuzzyMatch{
				Item:    item.data,
				Text:    item.text,
				Score:   score,
				Indices: indices,
			})
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	return matches
}

// score calculates a fuzzy match score for an item against a query.
// Returns 0 if no match, otherwise a positive score and matched indices.
func (fm *FuzzyMatcher) score(item fuzzyItem, query string) (float64, []int) {
	// Strategy 1: Exact substring match (highest priority)
	if idx := strings.Index(item.lower, query); idx >= 0 {
		indices := make([]int, len(query))
		for i := range len(query) {
			indices[i] = idx + i
		}
		score := 100.0
		// Bonus for matching at start of string
		if idx == 0 {
			score += 50
		}
		// Bonus for matching at start of a word
		if idx == 0 || isWordBoundary(item.text, idx-1) {
			score += 30
		}
		// Penalty for longer strings (prefer shorter matches)
		score -= float64(len(item.text)) * 0.1
		return score, indices
	}

	// Strategy 2: Sequential character match (fuzzy)
	indices := fuzzyMatchChars(item.lower, query)
	if indices == nil {
		return 0, nil
	}

	score := float64(len(indices)) * 5 // Base score for each matched char

	// Bonus for consecutive matches
	consecutive := 0
	for i := 1; i < len(indices); i++ {
		if indices[i] == indices[i-1]+1 {
			consecutive++
			score += 3 // Bonus for consecutive
		}
	}

	// Bonus for matching at word boundaries
	wordBoundaryBonus := 0
	for _, idx := range indices {
		if idx == 0 || isWordBoundary(item.text, idx-1) {
			wordBoundaryBonus++
			score += 5
		}
	}

	// Bonus for matching first character
	if len(indices) > 0 && indices[0] == 0 {
		score += 10
	}

	// Penalty for gaps between matches
	if len(indices) >= 2 {
		totalSpan := indices[len(indices)-1] - indices[0]
		expectedSpan := len(indices) - 1
		gapPenalty := float64(totalSpan-expectedSpan) * 0.5
		score -= gapPenalty
	}

	// Penalty for longer strings
	score -= float64(len(item.text)) * 0.1

	// Must have minimum score
	if score < 1 {
		return 0, nil
	}

	return score, indices
}

// fuzzyMatchChars finds sequential occurrences of query characters in text.
// Returns matched indices or nil if no match.
func fuzzyMatchChars(text, query string) []int {
	textRunes := []rune(text)
	queryRunes := []rune(query)

	if len(queryRunes) > len(textRunes) {
		return nil
	}

	var indices []int
	ti := 0

	for _, q := range queryRunes {
		found := false
		for ti < len(textRunes) {
			if textRunes[ti] == q {
				indices = append(indices, ti)
				ti++
				found = true
				break
			}
			ti++
		}
		if !found {
			return nil
		}
	}

	if len(indices) != len(queryRunes) {
		return nil
	}

	return indices
}

// isWordBoundary checks if the character at position i in text is a word boundary.
func isWordBoundary(text string, i int) bool {
	if i < 0 || i >= len(text) {
		return true
	}
	ch := rune(text[i])
	return unicode.IsSpace(ch) || ch == '_' || ch == '-' || ch == '/' || ch == '.'
}

// splitWords splits a string into lowercase words for scoring.
func splitWords(s string) []string {
	var words []string
	var current strings.Builder

	for _, ch := range s {
		switch {
		case unicode.IsSpace(ch) || ch == '_' || ch == '-' || ch == '/' || ch == '.':
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		case unicode.IsUpper(ch):
			// CamelCase boundary
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			current.WriteRune(unicode.ToLower(ch))
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}

// HighlightMatch returns the text with matched portions highlighted using ANSI styles.
// The highlight function receives the matched substring and should return styled text.
func HighlightMatch(text string, indices []int, highlight func(string) string) string {
	if len(indices) == 0 {
		return text
	}

	// Build a map of matched positions
	matched := make(map[int]bool)
	for _, idx := range indices {
		matched[idx] = true
	}

	var b strings.Builder
	var matchBuf strings.Builder
	inMatch := false

	for i, ch := range text {
		isMatch := matched[i]

		switch {
		case isMatch && !inMatch:
			// Start of match
			if b.Len() > 0 || matchBuf.Len() == 0 {
				// Flush any previous content
			}
			inMatch = true
			matchBuf.WriteRune(ch)
		case isMatch && inMatch:
			matchBuf.WriteRune(ch)
		case !isMatch && inMatch:
			// End of match - flush match buffer
			b.WriteString(highlight(matchBuf.String()))
			matchBuf.Reset()
			inMatch = false
			b.WriteRune(ch)
		default:
			b.WriteRune(ch)
		}
	}

	// Flush remaining match buffer
	if matchBuf.Len() > 0 {
		b.WriteString(highlight(matchBuf.String()))
	}

	return b.String()
}
