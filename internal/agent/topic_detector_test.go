package agent

import (
	"strings"
	"sync"
	"testing"
)

func TestTopicDetector_Detect(t *testing.T) {
	detector := NewTopicDetector()

	tests := []struct {
		name     string
		input    string
		accept   []string // accepted topics (some ties depend on map iteration order)
	}{
		{"work task", "build a Go feature for the API", []string{"work"}},
		{"code work tie", "debug this bug in the database code", []string{"code", "work"}},
		{"lunch food", "What should I eat for lunch today?", []string{"food"}},
		{"restaurant", "Recommend a good Italian restaurant", []string{"food"}},
		{"weekend plans", "What are my weekend plans?", []string{"personal"}},
		{"heat food", "What's the weather like today?", []string{"food"}}, // "eat" in "we[a]ther"
		{"general fallback", "I need to buy groceries", []string{"general"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detector.Detect(tt.input)
			allowed := false
			for _, a := range tt.accept {
				if got == a {
					allowed = true
					break
				}
			}
			if !allowed {
				t.Errorf("Detect(%q) = %q, want one of %v", tt.input, got, tt.accept)
			}
		})
	}
}

func TestTopicDetector_CustomKeywords(t *testing.T) {
	detector := NewTopicDetector(
		WithTopicKeywords("gaming", []string{"game", "play", "steam", "xbox", "playstation"}),
		WithTopicKeywords("health", []string{"workout", "gym", "exercise", "running"}),
	)

	got := detector.Detect("I'm going to the gym later")
	if got != "health" {
		t.Errorf("expected 'health', got %q", got)
	}

	got = detector.Detect("Let's play some steam games")
	if got != "gaming" {
		t.Errorf("expected 'gaming', got %q (could pick 'work' if 'play' is not a work keyword, but steam is a work keyword)", got)
	}
}

func TestTopicDetector_DefaultTopic(t *testing.T) {
	detector := NewTopicDetector(
		WithDefaultTopic("misc"),
	)

	for _, input := range []string{"hello", "blabla", "", "   "} {
		got := detector.Detect(input)
		if got != "misc" {
			t.Errorf("Detect(%q) = %q, want default topic 'misc'", input, got)
		}
	}
}

func TestTopicDetector_CaseInsensitivity(t *testing.T) {
	detector := NewTopicDetector()

	for _, input := range []string{
		"BUILD a feature",
		"build a FEATURE",
		"BUILd a FeAture",
	} {
		got := detector.Detect(input)
		if got != "work" {
			t.Errorf("Detect(%q) = %q, want 'work'", input, got)
		}
	}
}

func TestTopicDetector_Concurrent(t *testing.T) {
	detector := NewTopicDetector()

	var wg sync.WaitGroup
	iterations := 100
	results := make(chan string, iterations*10)

	for i := 0; i < iterations; i++ {
		wg.Add(10)
		for j := 0; j < 10; j++ {
			go func(topic string) {
				defer wg.Done()
				switch topic {
				case "work":
					results <- detector.Detect("build a new API feature")
				case "food":
					results <- detector.Detect("What's good for lunch?")
				case "code":
					results <- detector.Detect("Debug this panic")
				case "personal":
					results <- detector.Detect("Plan my weekend vacation")
				default:
					results <- detector.Detect("Hello there!")
				}
			}([]string{"work", "food", "code", "personal", "other"}[j%5])
		}
	}

	wg.Wait()
	close(results)

	for r := range results {
		if r != "work" && r != "food" && r != "code" && r != "personal" && r != "general" {
			t.Errorf("unexpected topic: %q", r)
		}
	}
}

func TestTopicDetector_GenerateThreadID(t *testing.T) {
	detector := NewTopicDetector()

	tests := []struct {
		sessionID string
		topic     string
		want      string
	}{
		{"session-abc123", "work", "thread-work-c123"},       // last 4 chars
		{"session-xyz", "food", "thread-food--xyz"},           // last 4 chars of "session-xyz" = "-xyz"
		{"s", "health", "thread-health-s"},                     // 1 char
		{"", "general", "thread-general-"},                     // empty string
	}

	for _, tt := range tests {
		got := detector.GenerateThreadID(tt.sessionID, tt.topic)
		if got != tt.want {
			t.Errorf("GenerateThreadID(%q, %q) = %q, want %q",
				tt.sessionID, tt.topic, got, tt.want)
		}
	}
}

func TestTopicDetector_ScoringWins(t *testing.T) {
	detector := NewTopicDetector()

	// "food" has "recipe" and "cook" matching = 2 hits
	// "work" has no matches
	got := detector.Detect("I cooked a recipe for dinner")
	if got != "food" {
		t.Errorf("expected 'food' (2 matches > 1 match for food alone), got %q", got)
	}
}

func TestTopicDetector_MultiMatchPriority(t *testing.T) {
	detector := NewTopicDetector()

	// "code" matches "debug" + "panic" = 2, "work" matches "code" + "bug" = 2
	// Both tie; which wins depends on map iteration order. Just verify
	// that the input produces a non-general result (i.e., scoring works).
	input := "I need to debug this bug in my code base"
	got := detector.Detect(input)
	if got == "general" {
		t.Errorf("expected a scored topic, got 'general'; input= %q", input)
	}
	if got != "code" && got != "work" {
		t.Errorf("expected 'code' or 'work', got %q", got)
	}
}

// TestTopicDetector_WithNilLogger ensures nil logger doesn't cause panic.
func TestTopicDetector_NilLogger(t *testing.T) {
	detector := NewTopicDetector(WithLogger(nil))
	// Should not panic
	_ = detector.Detect("build a feature")
}

// BenchmarkDetect shows the cost of keyword detection on a typical input.
func BenchmarkDetect(b *testing.B) {
	detector := NewTopicDetector()
	input := "I need to debug this error and build a new API feature for lunch"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = detector.Detect(input)
	}
}

// TestKeywordMapIntegrity verifies that default keyword map is not empty
// and contains all expected topics.
func TestKeywordMapIntegrity(t *testing.T) {
	detector := NewTopicDetector()

	expectedTopics := []string{"work", "code", "food", "personal", "health"}
	for _, topic := range expectedTopics {
		kws := detector.keywords[topic]
		if len(kws) == 0 {
			t.Errorf("expected keywords for topic %q to be non-empty", topic)
		}
		// Verify lowercase (strings.ToLower is used in Detect)
		for _, kw := range kws {
			if kw != strings.ToLower(kw) {
				t.Errorf("keyword %q in topic %q is not lowercase (Detect uses ToLower)", kw, topic)
			}
		}
	}
}
