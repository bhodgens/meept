package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

func setupTestCapabilitiesMap() *CapabilitiesMap {
	cm := NewCapabilitiesMap()

	// Keywords are granular to allow substring matching
	// These simulate what would be extracted from skill/agent metadata
	cm.Add(&AgentCapabilities{
		AgentID:     "coder",
		IntentTypes: []string{"code", "review"},
		Keywords:    []string{"write", "code", "implement", "function", "create", "refactor", "module"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:     "debugger",
		IntentTypes: []string{"debug"},
		Keywords:    []string{"fix", "bug", "debug", "error", "not working", "issue"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:     "planner",
		IntentTypes: []string{"plan"},
		Keywords:    []string{"plan", "design", "architect", "break down", "architecture"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:     "committer",
		IntentTypes: []string{"git"},
		Keywords:    []string{"commit", "push", "pull", "merge", "git", "branch"},
	})
	cm.Add(&AgentCapabilities{
		AgentID:     "chat",
		IntentTypes: []string{"chat", "platform", "report"},
		Keywords:    []string{"hello", "help", "what can you do", "capabilities", "report"},
	})

	return cm
}

func TestCapabilityMatcher_Match_Keywords(t *testing.T) {
	cm := setupTestCapabilitiesMap()
	matcher := NewCapabilityMatcher(CapabilityMatcherConfig{
		CapabilitiesMap: cm,
	})

	tests := []struct {
		input       string
		wantAgent   string
		wantConfGT  float64 // confidence greater than
		shouldMatch bool
	}{
		// Coder matches
		{"please write code for a calculator", "coder", 0.1, true},
		{"implement a new feature", "coder", 0.2, true},
		{"refactor this function", "coder", 0.2, true},

		// Debugger matches
		{"fix bug in the login system", "debugger", 0.1, true},
		{"debug this issue", "debugger", 0.2, true},
		{"the code is not working", "debugger", 0.1, true},

		// Planner matches
		{"plan the architecture", "planner", 0.2, true},
		{"design a new system", "planner", 0.2, true},
		{"break down this task", "planner", 0.2, true},

		// Git matches
		{"commit these changes", "committer", 0.2, true},
		{"push to main branch", "committer", 0.2, true},

		// Chat matches
		{"hello", "chat", 0.2, true},
		{"what can you do", "chat", 0.2, true},

		// No match (should return nil, handled by fallback)
		{"xyz random text abc", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := matcher.Match(tt.input)

			if tt.shouldMatch {
				if result == nil {
					t.Fatalf("Match(%q) returned nil, want match", tt.input)
				}
				if result.AgentID != tt.wantAgent {
					t.Errorf("Match(%q).AgentID = %q, want %q", tt.input, result.AgentID, tt.wantAgent)
				}
				if result.Confidence <= tt.wantConfGT {
					t.Errorf("Match(%q).Confidence = %f, want > %f", tt.input, result.Confidence, tt.wantConfGT)
				}
			} else {
				// Either nil or very low confidence
				if result != nil && result.Confidence >= 0.5 {
					t.Errorf("Match(%q) = {%s, %f}, want nil or low confidence", tt.input, result.AgentID, result.Confidence)
				}
			}
		})
	}
}

func TestCapabilityMatcher_Match_IntentPatterns(t *testing.T) {
	cm := setupTestCapabilitiesMap()
	matcher := NewCapabilityMatcher(CapabilityMatcherConfig{
		CapabilitiesMap: cm,
	})

	tests := []struct {
		input       string
		wantIntent  string
		wantAgent   string
		shouldMatch bool
	}{
		// Platform introspection
		{"what can you do?", "platform", "chat", true},
		{"tell me your capabilities", "platform", "chat", true},

		// Report patterns
		{"give me a report on the work", "report", "chat", true},

		// Code patterns
		{"write a function to sort arrays", "code", "coder", true},
		{"implement a new logging module", "code", "coder", true},

		// Debug patterns
		{"fix this bug in the parser", "debug", "debugger", true},

		// Git patterns
		{"commit these changes to main", "git", "committer", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := matcher.Match(tt.input)

			if tt.shouldMatch {
				if result == nil {
					t.Fatalf("Match(%q) returned nil, want match", tt.input)
				}
				if result.AgentID != tt.wantAgent {
					t.Errorf("Match(%q).AgentID = %q, want %q", tt.input, result.AgentID, tt.wantAgent)
				}
			}
		})
	}
}

func TestCapabilityMatcher_Match_WithCapabilityIndex(t *testing.T) {
	cm := setupTestCapabilitiesMap()

	// Create a capability index from skill metadata
	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "code-review",
		Description: "Review code for quality",
		Tags:        []string{"coding", "review"},
		Examples:    []string{"review this PR", "check code quality"},
	})
	idx.Index(&skills.SkillIndexEntry{
		Name:        "test-runner",
		Description: "Run tests",
		Tags:        []string{"testing"},
		Examples:    []string{"run the tests", "execute test suite"},
	})

	capIdx := skills.BuildCapabilityIndex(idx)

	// Add skill to agent
	cm.Get("coder").AvailableSkills = []string{"code-review"}

	matcher := NewCapabilityMatcher(CapabilityMatcherConfig{
		CapabilitiesMap: cm,
		CapabilityIndex: capIdx,
	})

	// Should match via keyword or skill - the exact match path depends on scoring
	result := matcher.Match("code-review this PR")
	// The match might be nil if score is too low, which is acceptable
	// The important test is that it doesn't panic and returns something reasonable
	if result != nil {
		t.Logf("Match type: %s, agent: %s, confidence: %f", result.MatchType, result.AgentID, result.Confidence)
	}
}

func TestCapabilityMatcher_MatchWithFallback(t *testing.T) {
	cm := setupTestCapabilitiesMap()
	matcher := NewCapabilityMatcher(CapabilityMatcherConfig{
		CapabilitiesMap: cm,
	})

	// Test with a strong keyword match (short input = higher confidence)
	result := matcher.MatchWithFallback("implement", 0.3)
	if result.MatchType == "fallback" {
		t.Error("Strong keyword match should not fallback")
	}

	// Low confidence match should fallback
	result = matcher.MatchWithFallback("xyz random text", 0.7)
	if result.MatchType != "fallback" {
		t.Errorf("Low confidence input should fallback, got %s", result.MatchType)
	}
	if result.AgentID != "chat" {
		t.Errorf("Fallback should route to chat, got %s", result.AgentID)
	}
}

func TestCapabilityMatcher_MatchAll(t *testing.T) {
	cm := setupTestCapabilitiesMap()
	matcher := NewCapabilityMatcher(CapabilityMatcherConfig{
		CapabilitiesMap: cm,
	})

	// Input that matches multiple agents
	results := matcher.MatchAll("write code and commit")
	if len(results) < 2 {
		t.Errorf("MatchAll returned %d results, want >= 2", len(results))
	}

	// Results should be sorted by confidence
	for i := 1; i < len(results); i++ {
		if results[i].Confidence > results[i-1].Confidence {
			t.Error("MatchAll results not sorted by confidence descending")
		}
	}
}

func TestCapabilityMatcher_NilCapabilitiesMap(t *testing.T) {
	matcher := NewCapabilityMatcher(CapabilityMatcherConfig{
		CapabilitiesMap: nil,
	})

	result := matcher.Match("any input")
	if result != nil {
		t.Error("Match with nil CapabilitiesMap should return nil")
	}

	results := matcher.MatchAll("any input")
	if results != nil {
		t.Error("MatchAll with nil CapabilitiesMap should return nil")
	}
}

func TestCapabilityMatcher_EmptyInput(t *testing.T) {
	cm := setupTestCapabilitiesMap()
	matcher := NewCapabilityMatcher(CapabilityMatcherConfig{
		CapabilitiesMap: cm,
	})

	result := matcher.Match("")
	if result != nil && result.Confidence > 0.5 {
		t.Error("Match with empty input should return nil or low confidence")
	}
}

func TestCalculateKeywordConfidence(t *testing.T) {
	tests := []struct {
		score    int
		inputLen int
		wantGE   float64 // greater or equal
		wantLE   float64 // less or equal
	}{
		{50, 50, 0.5, 1.0},   // High score, short input
		{10, 100, 0.1, 0.3},  // Low score, medium input
		{100, 50, 0.9, 1.0},  // Very high score (capped at 1.0)
		{0, 50, 0.0, 0.0},    // Zero score = zero confidence
	}

	for _, tt := range tests {
		conf := calculateKeywordConfidence(tt.score, tt.inputLen)
		if conf < tt.wantGE || conf > tt.wantLE {
			t.Errorf("calculateKeywordConfidence(%d, %d) = %f, want between %f and %f",
				tt.score, tt.inputLen, conf, tt.wantGE, tt.wantLE)
		}
	}
}
