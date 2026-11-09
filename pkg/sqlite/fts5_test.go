package sqlite

import (
	"testing"
)

func TestSanitizeQuery(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", `"hello" AND "world"`},
		{"test", `"test"`},
		{"", ""},
		{`"quotes" and *stars*`, `"quotes" AND "and" AND "stars"`},
	}

	for _, tt := range tests {
		result := SanitizeQuery(tt.input)
		if result != tt.expected {
			t.Errorf("SanitizeQuery(%q): expected %q, got %q", tt.input, tt.expected, result)
		}
	}
}

func TestSanitizeQueryOpPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		operator string
		expected string
	}{
		{"prefix and", "hello world", "AND", `"hello"* AND "world"*`},
		{"prefix or", "hello world", "OR", `"hello"* OR "world"*`},
		{"empty", "", "AND", ""},
		{"special chars stripped", `hello "world"*`, "AND", `"hello"* AND "world"*`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeQueryOpPrefixes(tt.input, tt.operator)
			if result != tt.expected {
				t.Errorf("SanitizeQueryOpPrefixes(%q, %q): expected %q, got %q", tt.input, tt.operator, tt.expected, result)
			}
		})
	}
}

func TestSanitizeQueryOp(t *testing.T) {
	if got := SanitizeQueryOp("hello world", "OR"); got != `"hello" OR "world"` {
		t.Errorf(`SanitizeQueryOp("hello world", "OR") = %q, want %q`, got, `"hello" OR "world"`)
	}
}

func TestSanitizeTerm(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{`"hello"`, "hello"},
		{"test*", "test"},
		{"foo:bar", "foobar"},
		{"(test)", "test"},
		{"  spaces  ", "spaces"},
		{"", ""},
	}

	for _, tt := range tests {
		result := sanitizeTerm(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeTerm(%q): expected %q, got %q", tt.input, tt.expected, result)
		}
	}
}

func TestBM25(t *testing.T) {
	// Without weights
	result := BM25("my_fts")
	expected := "bm25(my_fts)"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}

	// With weights
	result = BM25("my_fts", 1.0, 2.0, 3.0)
	expected = "bm25(my_fts, 1.00, 2.00, 3.00)"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestNormalizeRank(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{0.0, 0.0},
		{1.0, 0.0},        // Positive ranks should return 0
		{-1.0, 0.5},       //nolint:gocritic // test explanation: 1/(1+1)=0.5
		{-4.0, 0.8},       //nolint:gocritic // test explanation: 4/(1+4)=0.8
		{-9.0, 0.9},       //nolint:gocritic // test explanation: 9/(1+9)=0.9
		{-0.5, 1.0 / 3.0}, // 0.5 / (1 + 0.5) = 0.333...
	}

	for _, tt := range tests {
		result := NormalizeRank(tt.input)
		// Use approximate comparison for floating point
		diff := result - tt.expected
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.001 {
			t.Errorf("NormalizeRank(%f): expected %f, got %f", tt.input, tt.expected, result)
		}
	}
}
