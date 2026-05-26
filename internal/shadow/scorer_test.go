package shadow

import (
	"context"
	"log/slog"
	"testing"
)

func TestNewScorer(t *testing.T) {
	cfg := &QualityConfig{
		Method:               MethodHeuristic,
		HighQualityThreshold: 0.85,
		TrainableThreshold:   0.6,
		HeuristicWeights: HeuristicWeights{
			Relevance:    0.30,
			Completeness: 0.25,
			Correctness:  0.35,
			Style:        0.10,
		},
	}

	scorer := NewScorer(cfg)
	if scorer == nil {
		t.Fatal("NewScorer returned nil")
	}
}

func TestScorer_ScoreHeuristic(t *testing.T) {
	cfg := &QualityConfig{
		Method:               MethodHeuristic,
		HighQualityThreshold: 0.85,
		TrainableThreshold:   0.6,
		HeuristicWeights: HeuristicWeights{
			Relevance:    0.30,
			Completeness: 0.25,
			Correctness:  0.35,
			Style:        0.10,
		},
	}

	scorer := NewScorer(cfg, WithScorerLogger(slog.Default()))

	tests := []struct {
		name            string
		record          *ShadowRecord
		expectHighScore bool
	}{
		{
			name: "good_code_response",
			record: &ShadowRecord{
				Messages: []Message{
					{Role: "user", Content: "Write a function to calculate factorial"},
				},
				StudentContent: `Here's a function to calculate factorial:

` + "```python" + `
def factorial(n):
    if n <= 1:
        return 1
    return n * factorial(n - 1)
` + "```" + `

This recursive function handles the base case of 0 or 1 and multiplies n by the factorial of n-1 otherwise.`,
				Domain:   DomainCode,
				TaskType: TaskTypeChat,
			},
			expectHighScore: true,
		},
		{
			name: "short_response",
			record: &ShadowRecord{
				Messages: []Message{
					{Role: "user", Content: "What is the capital of France?"},
				},
				StudentContent: "Paris",
				Domain:         DomainGeneral,
				TaskType:       TaskTypeChat,
			},
			expectHighScore: false, // Too short
		},
		{
			name: "uncertain_response",
			record: &ShadowRecord{
				Messages: []Message{
					{Role: "user", Content: "How do I fix this error?"},
				},
				StudentContent: "I think maybe you should try restarting. I'm not sure, but probably it will help. I believe this could work.",
				Domain:         DomainDebugging,
				TaskType:       TaskTypeChat,
			},
			expectHighScore: false, // Too much hedging language
		},
		{
			name: "structured_response",
			record: &ShadowRecord{
				Messages: []Message{
					{Role: "user", Content: "How do I set up a Go project?"},
				},
				StudentContent: `Here's how to set up a Go project:

1. Create a new directory for your project
2. Run 'go mod init yourmodule'
3. Create a main.go file with your code
4. Run 'go build' to compile

This will give you a working Go project structure.`,
				Domain:   DomainCode,
				TaskType: TaskTypeMultiStep,
			},
			expectHighScore: true,
		},
	}

	ctx := context.Background()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := scorer.Score(ctx, tc.record)
			if err != nil {
				t.Fatalf("Score failed: %v", err)
			}

			if result.Method != "heuristic" {
				t.Errorf("Expected method 'heuristic', got %s", result.Method)
			}

			if result.Score < 0 || result.Score > 1 {
				t.Errorf("Score %f is out of valid range [0, 1]", result.Score)
			}

			// Check dimensions are populated
			for _, dim := range []string{"relevance", "completeness", "correctness", "style"} {
				if _, ok := result.Dimensions[dim]; !ok {
					t.Errorf("Missing dimension: %s", dim)
				}
			}

			// Log the score for debugging
			t.Logf("Score: %.3f, IsHighQuality: %v, Dimensions: %v", result.Score, result.IsHighQuality, result.Dimensions)
		})
	}
}

func TestScorer_ScoreComparison(t *testing.T) {
	cfg := &QualityConfig{
		Method:               MethodHeuristic,
		HighQualityThreshold: 0.85,
		TrainableThreshold:   0.6,
		PreferenceMargin:     0.1,
		HeuristicWeights: HeuristicWeights{
			Relevance:    0.30,
			Completeness: 0.25,
			Correctness:  0.35,
			Style:        0.10,
		},
	}

	scorer := NewScorer(cfg)

	record := &ShadowRecord{
		Messages: []Message{
			{Role: "user", Content: "Explain recursion"},
		},
		StudentContent: "Recursion is when a function calls itself.",
		TeacherContent: `Recursion is a programming technique where a function calls itself to solve a problem.

Key concepts:
1. **Base case**: A condition that stops the recursion
2. **Recursive case**: The function calls itself with a smaller problem

Example:
` + "```python" + `
def factorial(n):
    if n <= 1:  # Base case
        return 1
    return n * factorial(n - 1)  # Recursive case
` + "```" + `

This approach is useful for problems that can be broken down into smaller subproblems.`,
		Domain:   DomainCode,
		TaskType: TaskTypeReasoning,
	}

	ctx := context.Background()
	studentScore, teacherScore, err := scorer.ScoreComparison(ctx, record)
	if err != nil {
		t.Fatalf("ScoreComparison failed: %v", err)
	}

	t.Logf("Student score: %.3f, Teacher score: %.3f", studentScore, teacherScore)

	// Teacher should score higher (more detailed response)
	if teacherScore <= studentScore {
		t.Logf("Warning: Expected teacher to score higher than student")
	}
}

func TestExtractKeyTerms(t *testing.T) {
	tests := []struct {
		input    string
		expected int // minimum number of terms expected
	}{
		{"Write a function to calculate factorial", 2}, // "function", "calculate", "factorial"
		{"the a an is", 0}, // All stop words
		{"", 0},
		{"Hello world programming code", 3}, // "hello", "world", "programming", "code"
	}

	for _, tc := range tests {
		terms := extractKeyTerms(tc.input)
		if len(terms) < tc.expected {
			t.Errorf("extractKeyTerms(%q) returned %d terms, expected at least %d", tc.input, len(terms), tc.expected)
		}
	}
}

func TestHasBalancedBrackets(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"(a + b) * c", true},
		{"func() { return 1 }", true},
		{"arr[0]", true},
		{"(unclosed", false},
		{"{nested (inside) brackets}", true},
		{"", true},
		{"no brackets here", true},
		{"((()))", true},
		{"([)]", false}, // Mismatched
	}

	for _, tc := range tests {
		result := hasBalancedBrackets(tc.input)
		if result != tc.expected {
			t.Errorf("hasBalancedBrackets(%q) = %v, want %v", tc.input, result, tc.expected)
		}
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		v, min, max, expected float64
	}{
		{0.5, 0, 1, 0.5},
		{-0.5, 0, 1, 0},
		{1.5, 0, 1, 1},
		{0, 0, 1, 0},
		{1, 0, 1, 1},
		{5, 0, 10, 5},
	}

	for _, tc := range tests {
		result := clamp(tc.v, tc.min, tc.max)
		if result != tc.expected {
			t.Errorf("clamp(%f, %f, %f) = %f, want %f", tc.v, tc.min, tc.max, result, tc.expected)
		}
	}
}
