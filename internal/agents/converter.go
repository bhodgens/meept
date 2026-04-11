package agents

import (
	"time"
)

// ToAgentSpec converts a MergedSpec to the format expected by the agent package.
// This bridges the new agents package with the existing internal/agent package.
type AgentSpecCompat struct {
	// ID is the unique identifier for this agent specification.
	ID string

	// Name is a human-readable name for the agent.
	Name string

	// Role is the agent's role: dispatcher, executor, reviewer.
	Role string

	// Purpose is the full instruction body (from AGENT.md body).
	Purpose string

	// Model can be an alias name or direct model reference.
	Model string

	// AdditionalTools are tools beyond baseline.
	AdditionalTools []string

	// Capabilities for model selection.
	Capabilities []string

	// AvailableSkills lists accessible skill names.
	AvailableSkills []string

	// SkillTriggers maps keywords to skills.
	SkillTriggers map[string]string

	// Constraints
	MaxIterations         int
	Timeout               time.Duration
	MaxTokensPerTurn      int
	MaxConversationTokens int
	MaxMemoryRefs         int

	// Inference parameters
	Temperature *float64
	TopP        *float64

	// Source indicates where this spec came from.
	Source string
}

// ToCompatSpec converts a MergedSpec to AgentSpecCompat.
func ToCompatSpec(m *MergedSpec) *AgentSpecCompat {
	return &AgentSpecCompat{
		ID:                    m.ID,
		Name:                  m.Name,
		Role:                  m.Role,
		Purpose:               m.Purpose,
		Model:                 m.Model,
		AdditionalTools:       copyStrings(m.AdditionalTools),
		Capabilities:          copyStrings(m.Capabilities),
		AvailableSkills:       copyStrings(m.AvailableSkills),
		SkillTriggers:         copyMap(m.SkillTriggers),
		MaxIterations:         m.MaxIterations,
		Timeout:               m.Timeout,
		MaxTokensPerTurn:      m.MaxTokensPerTurn,
		MaxConversationTokens: m.MaxConversationTokens,
		MaxMemoryRefs:         m.MaxMemoryRefs,
		Temperature:           copyFloat64Ptr(m.Temperature),
		TopP:                  copyFloat64Ptr(m.TopP),
		Source:                m.Source,
	}
}

// ToProgrammaticSpec converts an AgentSpecCompat to ProgrammaticSpec for merging.
func ToProgrammaticSpec(s *AgentSpecCompat) *ProgrammaticSpec {
	return &ProgrammaticSpec{
		ID:                    s.ID,
		Name:                  s.Name,
		Role:                  s.Role,
		Purpose:               s.Purpose,
		Model:                 s.Model,
		AdditionalTools:       copyStrings(s.AdditionalTools),
		Capabilities:          copyStrings(s.Capabilities),
		AvailableSkills:       copyStrings(s.AvailableSkills),
		SkillTriggers:         copyMap(s.SkillTriggers),
		MaxIterations:         s.MaxIterations,
		Timeout:               s.Timeout,
		MaxTokensPerTurn:      s.MaxTokensPerTurn,
		MaxConversationTokens: s.MaxConversationTokens,
		MaxMemoryRefs:         s.MaxMemoryRefs,
		Temperature:           copyFloat64Ptr(s.Temperature),
		TopP:                  copyFloat64Ptr(s.TopP),
	}
}

// AgentLoader provides a unified interface for loading agent specifications
// from both programmatic defaults and AGENT.md files.
type AgentLoader struct {
	discovery *Discovery
	merger    *Merger
	rules     *RulesDiscovery
}

// NewAgentLoader creates a new AgentLoader with the given options.
func NewAgentLoader(opts ...DiscoveryOption) *AgentLoader {
	return &AgentLoader{
		discovery: NewDiscovery(opts...),
		rules:     NewRulesDiscovery(nil),
	}
}

// SetProgrammaticDefaults sets the programmatic defaults for merging.
func (l *AgentLoader) SetProgrammaticDefaults(defaults []*ProgrammaticSpec) {
	l.merger = NewMerger(defaults)
}

// Load discovers AGENT.md files and merges with programmatic defaults.
func (l *AgentLoader) Load() ([]*MergedSpec, error) {
	// Discover AGENT.md files
	discovered, err := l.discovery.Discover()
	if err != nil {
		return nil, err
	}

	// If no merger set (no programmatic defaults), just convert definitions
	if l.merger == nil {
		result := make([]*MergedSpec, len(discovered))
		for i, def := range discovered {
			result[i] = l.definitionToMergedSpec(def)
		}
		return result, nil
	}

	// Merge with programmatic defaults
	return l.merger.Merge(discovered), nil
}

// LoadGlobalRules loads the global rules content.
func (l *AgentLoader) LoadGlobalRules() string {
	return l.rules.DiscoverGlobalRules()
}

// definitionToMergedSpec converts an AgentDefinition to MergedSpec.
func (l *AgentLoader) definitionToMergedSpec(def *AgentDefinition) *MergedSpec {
	defaults := DefaultMetadata()

	spec := &MergedSpec{
		ID:              def.ID,
		Name:            def.Name,
		Role:            def.Role,
		Purpose:         def.Body,
		Model:           def.Model,
		AdditionalTools: copyStrings(def.AdditionalTools),
		Capabilities:    copyStrings(def.Capabilities),
		AvailableSkills: copyStrings(def.AvailableSkills),
		SkillTriggers:   copyMap(def.SkillTriggers),
		Temperature:     copyFloat64Ptr(def.Temperature),
		TopP:            copyFloat64Ptr(def.TopP),
		Source:          "agent.md",
	}

	// Apply defaults for zero values
	if spec.Name == "" {
		spec.Name = def.ID
	}
	if spec.Role == "" {
		spec.Role = defaults.Role
	}

	spec.MaxIterations = def.MaxIterations
	if spec.MaxIterations == 0 {
		spec.MaxIterations = defaults.MaxIterations
	}

	spec.Timeout = def.Timeout()
	if def.TimeoutSeconds == 0 {
		spec.Timeout = time.Duration(defaults.TimeoutSeconds) * time.Second
	}

	spec.MaxTokensPerTurn = def.MaxTokensPerTurn
	if spec.MaxTokensPerTurn == 0 {
		spec.MaxTokensPerTurn = defaults.MaxTokensPerTurn
	}

	spec.MaxConversationTokens = def.MaxConversationTokens

	spec.MaxMemoryRefs = def.MaxMemoryRefs
	if spec.MaxMemoryRefs == 0 {
		spec.MaxMemoryRefs = defaults.MaxMemoryRefs
	}

	return spec
}
