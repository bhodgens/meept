package agent

import (
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/skills"
)

// CapabilitiesBuilder constructs a CapabilitiesMap from agent specs and skill metadata.
// All keywords and intent types are derived from actual metadata - no hardcoded mappings.
type CapabilitiesBuilder struct {
	skillIndex      *skills.SkillIndex
	capabilityIndex *skills.CapabilityIndex
	extractor       *skills.KeywordExtractor
	logger          *slog.Logger
}

// NewCapabilitiesBuilder creates a new builder.
func NewCapabilitiesBuilder(skillIndex *skills.SkillIndex, logger *slog.Logger) *CapabilitiesBuilder {
	if logger == nil {
		logger = slog.Default()
	}
	return &CapabilitiesBuilder{
		skillIndex: skillIndex,
		extractor:  skills.NewKeywordExtractor(),
		logger:     logger,
	}
}

// WithCapabilityIndex sets a pre-built capability index.
func (b *CapabilitiesBuilder) WithCapabilityIndex(ci *skills.CapabilityIndex) *CapabilitiesBuilder {
	b.capabilityIndex = ci
	return b
}

// Build constructs a CapabilitiesMap from agent specs.
func (b *CapabilitiesBuilder) Build(specs []*AgentSpec) (*CapabilitiesMap, error) {
	capMap := NewCapabilitiesMap()

	for _, spec := range specs {
		caps := b.buildAgentCapabilities(spec)
		capMap.Add(caps)
		b.logger.Debug("Built capabilities for agent",
			"agent_id", caps.AgentID,
			"intent_types", caps.IntentTypes,
			"keywords", len(caps.Keywords),
			"skills", len(caps.AvailableSkills),
		)
	}

	b.logger.Info("Built capabilities map",
		"agents", capMap.Count(),
		"intent_types", len(capMap.AllIntentTypes()),
		"keywords", len(capMap.AllKeywords()),
	)

	return capMap, nil
}

// buildAgentCapabilities constructs capabilities for a single agent.
// All intent types and keywords are derived from metadata.
func (b *CapabilitiesBuilder) buildAgentCapabilities(spec *AgentSpec) *AgentCapabilities {
	caps := &AgentCapabilities{
		AgentID:           spec.ID,
		Name:              spec.Name,
		Role:              spec.Role,
		Purpose:           spec.Purpose,
		Tools:             spec.AllTools(),
		SkillCapabilities: make(map[string]bool),
		SkillTags:         make(map[string]bool),
		AvailableSkills:   spec.AvailableSkills,
		IntentTypes:       make([]string, 0),
		Keywords:          make([]string, 0),
	}

	// Derive intent types from agent ID and role
	caps.IntentTypes = b.deriveIntentTypesFromSpec(spec)

	// Derive keywords from agent purpose
	caps.Keywords = b.deriveKeywordsFromPurpose(spec.Purpose)

	// Aggregate capabilities and keywords from assigned skills
	if b.skillIndex != nil {
		for _, skillName := range spec.AvailableSkills {
			entry := b.skillIndex.Get(skillName)
			if entry == nil {
				continue
			}

			// Add skill capabilities (requires)
			for _, capName := range entry.Requires {
				caps.SkillCapabilities[strings.ToLower(capName)] = true
			}

			// Add skill tags
			for _, tag := range entry.Tags {
				caps.SkillTags[strings.ToLower(tag)] = true
				// Tags also serve as intent type hints
				caps.IntentTypes = append(caps.IntentTypes, strings.ToLower(tag))
			}

			// Extract keywords from skill metadata
			skillKeywords := b.extractor.ExtractFromEntry(entry)
			for _, kw := range skillKeywords {
				caps.Keywords = append(caps.Keywords, kw.Keyword)
			}
		}
	}

	// Add skill triggers as keywords
	for trigger := range spec.SkillTriggers {
		caps.Keywords = append(caps.Keywords, strings.ToLower(trigger))
	}

	// Deduplicate
	caps.IntentTypes = uniqueStrings(caps.IntentTypes)
	caps.Keywords = uniqueStrings(caps.Keywords)

	return caps
}

// deriveIntentTypesFromSpec derives intent types from agent specification.
// Uses agent ID, role, and purpose - no hardcoded mappings.
func (b *CapabilitiesBuilder) deriveIntentTypesFromSpec(spec *AgentSpec) []string {
	purposeIntents := b.extractIntentsFromPurpose(spec.Purpose)
	intents := make([]string, 0, 1+len(purposeIntents))

	// Agent ID is always an intent type
	intents = append(intents, spec.ID)

	// Extract potential intent hints from purpose
	intents = append(intents, purposeIntents...)

	return intents
}

// extractIntentsFromPurpose extracts intent-like words from purpose description.
func (b *CapabilitiesBuilder) extractIntentsFromPurpose(purpose string) []string {
	// Look for action verbs that indicate capabilities
	intentVerbs := map[string]bool{
		"code":        true,
		"debug":       true,
		"plan":        true,
		"analyze":     true,
		"research":    true,
		"schedule":    true,
		"commit":      true,
		"review":      true,
		"test":        true,
		"deploy":      true,
		"search":      true,
		"write":       true,
		"read":        true,
		"execute":     true,
		"manage":      true,
		"create":      true,
		"modify":      true,
		"delete":      true,
		"diagnose":    true,
		"troubleshoot": true,
		"fix":         true,
		"summarize":   true,
		"explain":     true,
	}

	purposeLower := strings.ToLower(purpose)
	words := strings.Fields(purposeLower)

	var intents []string
	for _, word := range words {
		// Clean punctuation
		word = strings.Trim(word, ".,;:!?()[]{}\"'")
		if intentVerbs[word] {
			intents = append(intents, word)
		}
	}

	return intents
}

// deriveKeywordsFromPurpose extracts keywords from agent purpose description.
func (b *CapabilitiesBuilder) deriveKeywordsFromPurpose(purpose string) []string {
	if purpose == "" {
		return nil
	}

	// Use the keyword extractor on the purpose
	purposeEntry := &skills.SkillIndexEntry{
		Name:        "",
		Description: purpose,
	}

	extracted := b.extractor.ExtractFromEntry(purposeEntry)

	keywords := make([]string, 0, len(extracted))
	for _, kw := range extracted {
		keywords = append(keywords, kw.Keyword)
	}

	return keywords
}

// uniqueStrings removes duplicates from a string slice.
func uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))

	for _, s := range input {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// BuildFromRegistry builds capabilities from an agent registry.
func (b *CapabilitiesBuilder) BuildFromRegistry(registry *AgentRegistry) (*CapabilitiesMap, error) {
	specs := registry.ListSpecs()
	return b.Build(specs)
}
