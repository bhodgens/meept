package agent

import (
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/skills"
)

// MatchResult holds the result of a capability match.
type MatchResult struct {
	// AgentID is the matched agent.
	AgentID string `json:"agent_id"`
	// Confidence is the match confidence [0.0, 1.0].
	Confidence float64 `json:"confidence"`
	// MatchType indicates how the match was made.
	MatchType string `json:"match_type"`
	// IntentType is the detected intent type.
	IntentType string `json:"intent_type"`
	// MatchedKeywords are the keywords that contributed to the match.
	MatchedKeywords []string `json:"matched_keywords,omitempty"`
	// MatchedSkill is the skill that matched (if any).
	MatchedSkill string `json:"matched_skill,omitempty"`
}

// CapabilityMatcher provides fast routing using metadata-driven matching.
// Uses CapabilityIndex for skill-based matching and minimal platform patterns.
type CapabilityMatcher struct {
	capMap          *CapabilitiesMap
	capabilityIndex *skills.CapabilityIndex
	logger          *slog.Logger
}

// CapabilityMatcherConfig holds configuration for the matcher.
type CapabilityMatcherConfig struct {
	CapabilitiesMap *CapabilitiesMap
	CapabilityIndex *skills.CapabilityIndex
	Logger          *slog.Logger
}

// NewCapabilityMatcher creates a new capability matcher.
func NewCapabilityMatcher(cfg CapabilityMatcherConfig) *CapabilityMatcher {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &CapabilityMatcher{
		capMap:          cfg.CapabilitiesMap,
		capabilityIndex: cfg.CapabilityIndex,
		logger:          logger,
	}
}

// Match finds the best agent for an input without using LLM.
// Returns nil if no confident match is found.
func (m *CapabilityMatcher) Match(input string) *MatchResult {
	inputLower := strings.ToLower(input)

	// Step 1: Platform introspection patterns (minimal, kept for system-level queries)
	if result := m.matchPlatformPatterns(inputLower); result != nil {
		return result
	}

	// Step 2: Skill-based matching via CapabilityIndex (metadata-driven)
	if result := m.matchByCapabilityIndex(inputLower); result != nil {
		return result
	}

	// Step 3: Agent capabilities matching
	if result := m.matchByAgentCapabilities(inputLower); result != nil {
		return result
	}

	// No confident match - return nil to fall through to LLM
	return nil
}

// matchPlatformPatterns handles system-level queries that aren't skill-based.
// This is the ONLY hardcoded matching - for platform introspection.
func (m *CapabilityMatcher) matchPlatformPatterns(inputLower string) *MatchResult {
	platformPatterns := []string{
		"what can you do",
		"what are your capabilities",
		"what tools do you have",
		"what agents are available",
		"platform status",
		"system status",
		"help me understand",
		"what are you",
	}

	for _, pattern := range platformPatterns {
		if strings.Contains(inputLower, pattern) {
			return &MatchResult{
				AgentID:         "chat",
				Confidence:      0.9,
				MatchType:       "platform",
				IntentType:      "platform",
				MatchedKeywords: []string{pattern},
			}
		}
	}

	return nil
}

// matchByCapabilityIndex uses the skill capability index for matching.
func (m *CapabilityMatcher) matchByCapabilityIndex(inputLower string) *MatchResult {
	if m.capabilityIndex == nil {
		return nil
	}

	// Get top matches from capability index
	match := m.capabilityIndex.GetTopMatch(inputLower, 0.5)
	if match == nil {
		return nil
	}

	// Find which agent has this skill
	agentID := m.findAgentForSkill(match.Entry.Name)
	if agentID == "" {
		agentID = "chat" // Fallback
	}

	// Extract matched keywords
	var matchedKeywords []string
	for _, km := range match.Matches {
		matchedKeywords = append(matchedKeywords, km.Keyword)
	}

	m.logger.Debug("Capability index match",
		"skill", match.Entry.Name,
		"agent", agentID,
		"confidence", match.Confidence,
		"keywords", matchedKeywords,
	)

	return &MatchResult{
		AgentID:         agentID,
		Confidence:      match.Confidence,
		MatchType:       "skill",
		IntentType:      "skill",
		MatchedSkill:    match.Entry.Name,
		MatchedKeywords: matchedKeywords,
	}
}

// matchByAgentCapabilities matches against agent-level keywords from CapabilitiesMap.
func (m *CapabilityMatcher) matchByAgentCapabilities(inputLower string) *MatchResult {
	if m.capMap == nil {
		return nil
	}

	// Score each agent by keyword matches
	type agentScore struct {
		agentID         string
		score           int
		matchedKeywords []string
	}

	scores := make(map[string]*agentScore)

	for _, agentID := range m.capMap.AgentIDs() {
		caps := m.capMap.Get(agentID)
		if caps == nil {
			continue
		}

		var matched []string
		totalScore := 0

		for _, keyword := range caps.Keywords {
			kwLower := strings.ToLower(keyword)
			if strings.Contains(inputLower, kwLower) {
				matched = append(matched, keyword)
				totalScore += len(keyword)
				if strings.HasPrefix(inputLower, kwLower) {
					totalScore += 10
				}
			}
		}

		if totalScore > 0 {
			scores[agentID] = &agentScore{
				agentID:         agentID,
				score:           totalScore,
				matchedKeywords: matched,
			}
		}
	}

	if len(scores) == 0 {
		return nil
	}

	// Find best match
	var best *agentScore
	for _, s := range scores {
		if best == nil || s.score > best.score {
			best = s
		}
	}

	if best == nil || best.score < 5 {
		return nil
	}

	// Calculate confidence
	confidence := calculateKeywordConfidence(best.score, len(inputLower))

	// Get intent type
	intentType := m.getDefaultIntentType(best.agentID)

	m.logger.Debug("Agent capability match",
		"agent", best.agentID,
		"score", best.score,
		"confidence", confidence,
		"keywords", best.matchedKeywords,
	)

	return &MatchResult{
		AgentID:         best.agentID,
		Confidence:      confidence,
		MatchType:       "agent",
		IntentType:      intentType,
		MatchedKeywords: best.matchedKeywords,
	}
}

// findAgentForSkill returns the agent ID that has a skill available.
func (m *CapabilityMatcher) findAgentForSkill(skillName string) string {
	if m.capMap == nil {
		return ""
	}

	for _, agentID := range m.capMap.AgentIDs() {
		caps := m.capMap.Get(agentID)
		if caps == nil {
			continue
		}

		for _, sk := range caps.AvailableSkills {
			if strings.EqualFold(sk, skillName) {
				return agentID
			}
		}
	}
	return ""
}

// getDefaultIntentType returns the default intent type for an agent.
func (m *CapabilityMatcher) getDefaultIntentType(agentID string) string {
	if m.capMap == nil {
		return agentID
	}

	caps := m.capMap.Get(agentID)
	if caps != nil && len(caps.IntentTypes) > 0 {
		return caps.IntentTypes[0]
	}
	return agentID
}

// calculateKeywordConfidence converts a keyword score to confidence [0.0, 1.0].
func calculateKeywordConfidence(score, inputLen int) float64 {
	base := float64(score) / 50.0

	lengthFactor := 1.0 - (float64(inputLen) / 500.0)
	if lengthFactor < 0.5 {
		lengthFactor = 0.5
	}

	confidence := base * lengthFactor

	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}

	return confidence
}

// MatchWithFallback matches and provides a fallback if confidence is too low.
func (m *CapabilityMatcher) MatchWithFallback(input string, minConfidence float64) *MatchResult {
	result := m.Match(input)

	if result != nil && result.Confidence >= minConfidence {
		return result
	}

	return &MatchResult{
		AgentID:    "chat",
		Confidence: 0.3,
		MatchType:  "fallback",
		IntentType: "chat",
	}
}

// MatchAll returns all potential matches ranked by confidence.
func (m *CapabilityMatcher) MatchAll(input string) []*MatchResult {
	var results []*MatchResult

	inputLower := strings.ToLower(input)

	// Collect skill matches
	if m.capabilityIndex != nil {
		matches := m.capabilityIndex.Match(inputLower, 5)
		for _, match := range matches {
			agentID := m.findAgentForSkill(match.Entry.Name)
			if agentID == "" {
				agentID = "chat"
			}

			var keywords []string
			for _, km := range match.Matches {
				keywords = append(keywords, km.Keyword)
			}

			results = append(results, &MatchResult{
				AgentID:         agentID,
				Confidence:      match.Confidence,
				MatchType:       "skill",
				IntentType:      "skill",
				MatchedSkill:    match.Entry.Name,
				MatchedKeywords: keywords,
			})
		}
	}

	// Collect agent capability matches
	if m.capMap != nil {
		for _, agentID := range m.capMap.AgentIDs() {
			caps := m.capMap.Get(agentID)
			if caps == nil {
				continue
			}

			var matched []string
			totalScore := 0

			for _, keyword := range caps.Keywords {
				kwLower := strings.ToLower(keyword)
				if strings.Contains(inputLower, kwLower) {
					matched = append(matched, keyword)
					totalScore += len(keyword)
				}
			}

			if totalScore > 0 {
				confidence := calculateKeywordConfidence(totalScore, len(inputLower))
				results = append(results, &MatchResult{
					AgentID:         agentID,
					Confidence:      confidence,
					MatchType:       "agent",
					IntentType:      m.getDefaultIntentType(agentID),
					MatchedKeywords: matched,
				})
			}
		}
	}

	// Sort by confidence descending (simple bubble sort for small lists)
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Confidence > results[i].Confidence {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results
}

// SetCapabilityIndex updates the capability index.
func (m *CapabilityMatcher) SetCapabilityIndex(ci *skills.CapabilityIndex) {
	m.capabilityIndex = ci
}

// CapabilityIndex returns the current capability index.
func (m *CapabilityMatcher) CapabilityIndex() *skills.CapabilityIndex {
	return m.capabilityIndex
}
