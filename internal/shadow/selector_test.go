package shadow

import (
	"log/slog"
	"testing"
	"time"
)

func TestNewSelector(t *testing.T) {
	cfg := &ExamplesConfig{
		Enabled:          true,
		MaxPerCategory:   100,
		MinQuality:       0.8,
		DefaultCount:     3,
		MaxCount:         5,
		SimilarityWeight: 0.7,
		RecencyWeight:    0.2,
		QualityWeight:    0.1,
		MaxContextTokens: 2000,
	}

	// Use nil store since we're just testing basic functionality
	selector := NewSelector(nil, cfg, WithSelectorLogger(slog.Default()))
	if selector == nil {
		t.Fatal("NewSelector returned nil")
	}
}

func TestSelector_ComputeSimilarity(t *testing.T) {
	cfg := &ExamplesConfig{
		Enabled:          true,
		SimilarityWeight: 0.7,
		RecencyWeight:    0.2,
		QualityWeight:    0.1,
	}

	selector := NewSelector(nil, cfg)

	tests := []struct {
		name     string
		query    string
		example  string
		minScore float64
		maxScore float64
	}{
		{
			name:     "identical",
			query:    "hello world",
			example:  "hello world",
			minScore: 0.9,
			maxScore: 1.0,
		},
		{
			name:     "similar",
			query:    "how to write code",
			example:  "how to write code in python",
			minScore: 0.5,
			maxScore: 1.0,
		},
		{
			name:     "different",
			query:    "hello world",
			example:  "foo bar baz",
			minScore: 0.0,
			maxScore: 0.3,
		},
		{
			name:     "empty_query",
			query:    "",
			example:  "hello world",
			minScore: 0.0,
			maxScore: 0.1,
		},
		{
			name:     "empty_example",
			query:    "hello world",
			example:  "",
			minScore: 0.0,
			maxScore: 0.1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score := selector.computeSimilarity(tc.query, tc.example)
			if score < tc.minScore || score > tc.maxScore {
				t.Errorf("computeSimilarity(%q, %q) = %f, want in range [%f, %f]",
					tc.query, tc.example, score, tc.minScore, tc.maxScore)
			}
		})
	}
}

func TestSelector_ComputeRecencyScore(t *testing.T) {
	cfg := &ExamplesConfig{}
	selector := NewSelector(nil, cfg)

	tests := []struct {
		name     string
		age      time.Duration
		minScore float64
		maxScore float64
	}{
		{
			name:     "very_recent",
			age:      1 * time.Hour,
			minScore: 0.95,
			maxScore: 1.0,
		},
		{
			name:     "one_day",
			age:      24 * time.Hour,
			minScore: 0.85,
			maxScore: 0.95,
		},
		{
			name:     "one_week",
			age:      7 * 24 * time.Hour,
			minScore: 0.45,
			maxScore: 0.55,
		},
		{
			name:     "one_month",
			age:      30 * 24 * time.Hour,
			minScore: 0.0,
			maxScore: 0.1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			score := selector.computeRecencyScore(tc.age)
			if score < tc.minScore || score > tc.maxScore {
				t.Errorf("computeRecencyScore(%v) = %f, want in range [%f, %f]",
					tc.age, score, tc.minScore, tc.maxScore)
			}
		})
	}
}

func TestSelector_FormatForInjection(t *testing.T) {
	cfg := &ExamplesConfig{
		Enabled: true,
	}
	selector := NewSelector(nil, cfg)

	tests := []struct {
		name          string
		examples      []*FewShotExample
		expectLen     int
		expectNil     bool
	}{
		{
			name:      "empty",
			examples:  []*FewShotExample{},
			expectLen: 0,
			expectNil: true,
		},
		{
			name: "single_example",
			examples: []*FewShotExample{
				{
					UserMessage:       "How do I loop in Python?",
					AssistantResponse: "Use a for loop: for i in range(10): print(i)",
				},
			},
			// 1 system intro + 1 user + 1 assistant + 1 system separator = 4
			expectLen: 4,
			expectNil: false,
		},
		{
			name: "multiple_examples",
			examples: []*FewShotExample{
				{
					UserMessage:       "Question 1",
					AssistantResponse: "Answer 1",
				},
				{
					UserMessage:       "Question 2",
					AssistantResponse: "Answer 2",
				},
			},
			// 1 system intro + (2 user + 2 assistant) + 1 system separator = 6
			expectLen: 6,
			expectNil: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := selector.FormatForInjection(tc.examples)
			if tc.expectNil {
				if result != nil {
					t.Errorf("Expected nil, got %d messages", len(result))
				}
			} else {
				if len(result) != tc.expectLen {
					t.Errorf("Expected %d messages, got %d", tc.expectLen, len(result))
				}
			}
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected int // minimum count
	}{
		{"hello world", 2},
		{"the a an", 0}, // All stop words
		{"Hello WORLD", 2},
		{"", 0},
		{"write code function class variable", 5},
	}

	for _, tc := range tests {
		result := tokenize(tc.input)
		if len(result) < tc.expected {
			t.Errorf("tokenize(%q) returned %d tokens, expected at least %d", tc.input, len(result), tc.expected)
		}
	}
}

func TestGetBigrams(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"hello world", 1},       // "hello world"
		{"a b c", 2},             // "a b", "b c"
		{"single", 0},            // Not enough words
		{"", 0},                  // Empty
		{"one two three", 2},     // "one two", "two three"
	}

	for _, tc := range tests {
		result := getBigrams(tc.input)
		if len(result) != tc.expected {
			t.Errorf("getBigrams(%q) returned %d bigrams, expected %d", tc.input, len(result), tc.expected)
		}
	}
}

func TestIsStopWord(t *testing.T) {
	stopWords := []string{"the", "and", "for", "are", "but", "not", "you"}
	nonStopWords := []string{"hello", "code", "function", "program", "error"}

	for _, w := range stopWords {
		if !isStopWord(w) {
			t.Errorf("isStopWord(%q) = false, want true", w)
		}
	}

	for _, w := range nonStopWords {
		if isStopWord(w) {
			t.Errorf("isStopWord(%q) = true, want false", w)
		}
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
		{1000, "1000"},
	}

	for _, tc := range tests {
		result := itoa(tc.input)
		if result != tc.expected {
			t.Errorf("itoa(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}
