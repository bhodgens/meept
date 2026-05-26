package components

import (
	"strings"
	"testing"
)

func TestFuzzyMatcher_ExactMatch(t *testing.T) {
	fm := NewFuzzyMatcher([]struct {
		Text string
		Data any
	}{
		{Text: "Implement authentication", Data: "task1"},
		{Text: "Fix database connection", Data: "task2"},
		{Text: "Add user registration", Data: "task3"},
	})

	matches := fm.Match("authentication")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Text != "Implement authentication" {
		t.Errorf("expected 'Implement authentication', got %q", matches[0].Text)
	}
	if matches[0].Score < 100 {
		t.Errorf("expected high score for exact match, got %f", matches[0].Score)
	}
}

func TestFuzzyMatcher_FuzzyMatch(t *testing.T) {
	fm := NewFuzzyMatcher([]struct {
		Text string
		Data any
	}{
		{Text: "Implement authentication", Data: "task1"},
		{Text: "Fix database connection", Data: "task2"},
		{Text: "Add user registration", Data: "task3"},
	})

	matches := fm.Match("auth")
	if len(matches) < 1 {
		t.Fatal("expected at least 1 match for 'auth'")
	}
	// "Implement authentication" should be the top match
	if matches[0].Text != "Implement authentication" {
		t.Errorf("expected 'Implement authentication' as top match, got %q", matches[0].Text)
	}
}

func TestFuzzyMatcher_EmptyQuery(t *testing.T) {
	fm := NewFuzzyMatcher([]struct {
		Text string
		Data any
	}{
		{Text: "Task 1", Data: "t1"},
		{Text: "Task 2", Data: "t2"},
	})

	matches := fm.Match("")
	if len(matches) != 2 {
		t.Errorf("expected 2 matches for empty query, got %d", len(matches))
	}
}

func TestFuzzyMatcher_NoMatch(t *testing.T) {
	fm := NewFuzzyMatcher([]struct {
		Text string
		Data any
	}{
		{Text: "Implement authentication", Data: "task1"},
		{Text: "Fix database connection", Data: "task2"},
	})

	matches := fm.Match("zzzzzzz")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestFuzzyMatcher_ScoringOrder(t *testing.T) {
	fm := NewFuzzyMatcher([]struct {
		Text string
		Data any
	}{
		{Text: "auth middleware", Data: "task1"},
		{Text: "implement authentication handler", Data: "task2"},
		{Text: "oauth callback", Data: "task3"},
	})

	matches := fm.Match("auth")
	if len(matches) < 1 {
		t.Fatal("expected at least 1 match")
	}
	// "auth middleware" should score higher than "implement authentication handler"
	// because it's shorter and starts with "auth"
	topMatch := matches[0]
	if topMatch.Text != "auth middleware" {
		t.Errorf("expected 'auth middleware' as top match (shorter, starts with), got %q", topMatch.Text)
	}
}

func TestFuzzyMatcher_DataPayload(t *testing.T) {
	fm := NewFuzzyMatcher([]struct {
		Text string
		Data any
	}{
		{Text: "Task one", Data: 42},
		{Text: "Task two", Data: "hello"},
	})

	matches := fm.Match("one")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Item != 42 {
		t.Errorf("expected data 42, got %v", matches[0].Item)
	}
}

func TestFuzzyMatcher_Indices(t *testing.T) {
	fm := NewFuzzyMatcher([]struct {
		Text string
		Data any
	}{
		{Text: "hello world", Data: nil},
	})

	matches := fm.Match("hlo")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}

	// Should match h(0), l(2), o(4) or similar
	indices := matches[0].Indices
	if len(indices) != 3 {
		t.Errorf("expected 3 matched indices, got %d", len(indices))
	}
}

func TestHighlightMatch(t *testing.T) {
	result := HighlightMatch("hello world", []int{0, 1, 2, 3, 4}, func(s string) string {
		return "[" + s + "]"
	})

	if !strings.Contains(result, "[hello]") {
		t.Errorf("expected highlighted match, got %q", result)
	}
}

func TestHighlightMatch_Discontinuous(t *testing.T) {
	result := HighlightMatch("hello world", []int{0, 6, 7, 8, 9, 10}, func(s string) string {
		return "[" + s + "]"
	})

	if !strings.Contains(result, "[h]") {
		t.Errorf("expected highlighted 'h', got %q", result)
	}
	if !strings.Contains(result, "[world]") {
		t.Errorf("expected highlighted 'world', got %q", result)
	}
}

func TestHighlightMatch_EmptyIndices(t *testing.T) {
	result := HighlightMatch("hello world", nil, func(s string) string {
		return "[" + s + "]"
	})

	if result != "hello world" {
		t.Errorf("expected unmodified text with empty indices, got %q", result)
	}
}

func TestFuzzyMatchChars(t *testing.T) {
	tests := []struct {
		text    string
		query   string
		matches bool
	}{
		{"hello", "hlo", true},
		{"hello", "hlooo", false},
		{"hello world", "hw", true},
		{"hello", "", false}, // empty query returns nil (handled by caller)
		{"", "a", false},     // can't match in empty text
	}

	for _, tt := range tests {
		result := fuzzyMatchChars(tt.text, tt.query)
		if (result != nil) != tt.matches {
			t.Errorf("fuzzyMatchChars(%q, %q): expected matches=%v, got %v",
				tt.text, tt.query, tt.matches, result != nil)
		}
	}
}

func TestIsWordBoundary(t *testing.T) {
	tests := []struct {
		text     string
		idx      int
		expected bool
	}{
		{"hello world", 5, true}, // space
		{"hello_world", 5, true}, // underscore
		{"hello-world", 5, true}, // hyphen
		{"hello.world", 5, true}, // dot
		{"hello/world", 5, true}, // slash
		{"helloworld", 5, false}, // no boundary
		{"hello", -1, true},      // out of bounds
		{"hello", 5, true},       // out of bounds
	}

	for _, tt := range tests {
		result := isWordBoundary(tt.text, tt.idx)
		if result != tt.expected {
			t.Errorf("isWordBoundary(%q, %d): expected %v, got %v",
				tt.text, tt.idx, tt.expected, result)
		}
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"hello_world", []string{"hello", "world"}},
		{"helloWorld", []string{"hello", "world"}},
		{"hello-world", []string{"hello", "world"}},
		{"hello.world", []string{"hello", "world"}},
		{"single", []string{"single"}},
		{"", nil},
	}

	for _, tt := range tests {
		result := splitWords(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("splitWords(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i, w := range result {
			if w != tt.expected[i] {
				t.Errorf("splitWords(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], w)
			}
		}
	}
}
