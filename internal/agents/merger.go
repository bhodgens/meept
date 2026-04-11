package agents

import (
	"time"
)

// MergedSpec represents the final merged agent specification.
// It combines programmatic defaults with AGENT.md overrides.
type MergedSpec struct {
	// ID is the unique identifier for this agent.
	ID string

	// Name is a human-readable name.
	Name string

	// Role defines the agent's role.
	Role string

	// Purpose is the full instruction body from AGENT.md or defaults.
	Purpose string

	// Model can be an alias name or direct model reference.
	Model string

	// AdditionalTools are tools beyond baseline.
	AdditionalTools []string

	// Capabilities are tags for model selection.
	Capabilities []string

	// AvailableSkills lists accessible skill names.
	AvailableSkills []string

	// SkillTriggers maps keywords to skills.
	SkillTriggers map[string]string

	// MaxIterations is the maximum reasoning cycles.
	MaxIterations int

	// Timeout is the maximum request duration.
	Timeout time.Duration

	// MaxTokensPerTurn limits tokens per turn.
	MaxTokensPerTurn int

	// MaxConversationTokens is the token budget per conversation.
	MaxConversationTokens int

	// MaxMemoryRefs limits memory references.
	MaxMemoryRefs int

	// Temperature for LLM (nil = use default).
	Temperature *float64

	// TopP for LLM (nil = use default).
	TopP *float64

	// Source indicates where this spec came from.
	Source string // "programmatic", "agent.md", "merged"
}

// ProgrammaticSpec represents an agent spec defined in code.
// This mirrors the structure from internal/agent/spec.go.
type ProgrammaticSpec struct {
	ID                    string
	Name                  string
	Role                  string
	Purpose               string
	Model                 string
	AdditionalTools       []string
	Capabilities          []string
	AvailableSkills       []string
	SkillTriggers         map[string]string
	MaxIterations         int
	Timeout               time.Duration
	MaxTokensPerTurn      int
	MaxConversationTokens int
	MaxMemoryRefs         int
	Temperature           *float64
	TopP                  *float64
}

// Merger handles merging of programmatic and AGENT.md definitions.
type Merger struct {
	programmaticDefaults map[string]*ProgrammaticSpec
}

// NewMerger creates a new Merger with programmatic defaults.
func NewMerger(defaults []*ProgrammaticSpec) *Merger {
	m := &Merger{
		programmaticDefaults: make(map[string]*ProgrammaticSpec),
	}
	for _, spec := range defaults {
		m.programmaticDefaults[normalizeID(spec.ID)] = spec
	}
	return m
}

// Merge combines discovered AGENT.md definitions with programmatic defaults.
// AGENT.md fields override programmatic defaults when non-empty.
// Tools are merged (union) rather than replaced.
func (m *Merger) Merge(discovered []*AgentDefinition) []*MergedSpec {
	results := make(map[string]*MergedSpec)

	// Start with all programmatic defaults
	for id, prog := range m.programmaticDefaults {
		results[id] = m.fromProgrammatic(prog)
	}

	// Overlay AGENT.md definitions
	for _, def := range discovered {
		key := normalizeID(def.ID)
		existing, hasDefault := results[key]

		if hasDefault {
			// Merge with existing programmatic default
			results[key] = m.mergeDefinitions(existing, def)
		} else {
			// New agent from AGENT.md only
			results[key] = m.fromDefinition(def)
		}
	}

	// Convert to slice
	merged := make([]*MergedSpec, 0, len(results))
	for _, spec := range results {
		merged = append(merged, spec)
	}

	return merged
}

// fromProgrammatic converts a programmatic spec to a merged spec.
func (m *Merger) fromProgrammatic(prog *ProgrammaticSpec) *MergedSpec {
	return &MergedSpec{
		ID:                    prog.ID,
		Name:                  prog.Name,
		Role:                  prog.Role,
		Purpose:               prog.Purpose,
		Model:                 prog.Model,
		AdditionalTools:       copyStrings(prog.AdditionalTools),
		Capabilities:          copyStrings(prog.Capabilities),
		AvailableSkills:       copyStrings(prog.AvailableSkills),
		SkillTriggers:         copyMap(prog.SkillTriggers),
		MaxIterations:         prog.MaxIterations,
		Timeout:               prog.Timeout,
		MaxTokensPerTurn:      prog.MaxTokensPerTurn,
		MaxConversationTokens: prog.MaxConversationTokens,
		MaxMemoryRefs:         prog.MaxMemoryRefs,
		Temperature:           copyFloat64Ptr(prog.Temperature),
		TopP:                  copyFloat64Ptr(prog.TopP),
		Source:                "programmatic",
	}
}

// fromDefinition converts an AGENT.md definition to a merged spec.
func (m *Merger) fromDefinition(def *AgentDefinition) *MergedSpec {
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

// mergeDefinitions merges an AGENT.md definition into an existing spec.
// Non-empty AGENT.md fields override defaults; tools are merged.
func (m *Merger) mergeDefinitions(base *MergedSpec, def *AgentDefinition) *MergedSpec {
	merged := &MergedSpec{
		ID:     base.ID,
		Source: "merged",
	}

	// Name: prefer AGENT.md if set
	if def.Name != "" {
		merged.Name = def.Name
	} else {
		merged.Name = base.Name
	}

	// Role: prefer AGENT.md if set
	if def.Role != "" {
		merged.Role = def.Role
	} else {
		merged.Role = base.Role
	}

	// Purpose: prefer AGENT.md body if non-empty
	if def.Body != "" {
		merged.Purpose = def.Body
	} else {
		merged.Purpose = base.Purpose
	}

	// Model: prefer AGENT.md if set
	if def.Model != "" {
		merged.Model = def.Model
	} else {
		merged.Model = base.Model
	}

	// Tools: MERGE (union) - AGENT.md tools add to base tools
	merged.AdditionalTools = mergeStrings(base.AdditionalTools, def.AdditionalTools)

	// Capabilities: prefer AGENT.md if set, otherwise inherit
	if len(def.Capabilities) > 0 {
		merged.Capabilities = copyStrings(def.Capabilities)
	} else {
		merged.Capabilities = copyStrings(base.Capabilities)
	}

	// Skills: MERGE (union)
	merged.AvailableSkills = mergeStrings(base.AvailableSkills, def.AvailableSkills)

	// SkillTriggers: MERGE (AGENT.md overrides base for same key)
	merged.SkillTriggers = mergeMaps(base.SkillTriggers, def.SkillTriggers)

	// Constraints: prefer AGENT.md if non-zero
	if def.MaxIterations > 0 {
		merged.MaxIterations = def.MaxIterations
	} else {
		merged.MaxIterations = base.MaxIterations
	}

	if def.TimeoutSeconds > 0 {
		merged.Timeout = def.Timeout()
	} else {
		merged.Timeout = base.Timeout
	}

	if def.MaxTokensPerTurn > 0 {
		merged.MaxTokensPerTurn = def.MaxTokensPerTurn
	} else {
		merged.MaxTokensPerTurn = base.MaxTokensPerTurn
	}

	if def.MaxConversationTokens > 0 {
		merged.MaxConversationTokens = def.MaxConversationTokens
	} else {
		merged.MaxConversationTokens = base.MaxConversationTokens
	}

	if def.MaxMemoryRefs > 0 {
		merged.MaxMemoryRefs = def.MaxMemoryRefs
	} else {
		merged.MaxMemoryRefs = base.MaxMemoryRefs
	}

	// Temperature/TopP: prefer AGENT.md if set
	if def.Temperature != nil {
		merged.Temperature = copyFloat64Ptr(def.Temperature)
	} else {
		merged.Temperature = copyFloat64Ptr(base.Temperature)
	}

	if def.TopP != nil {
		merged.TopP = copyFloat64Ptr(def.TopP)
	} else {
		merged.TopP = copyFloat64Ptr(base.TopP)
	}

	return merged
}

// Helper functions for safe copying

func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	c := make([]string, len(s))
	copy(c, s)
	return c
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func copyFloat64Ptr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func mergeStrings(base, overlay []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(base)+len(overlay))

	for _, s := range base {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}

	for _, s := range overlay {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}

	return result
}

func mergeMaps(base, overlay map[string]string) map[string]string {
	if base == nil && overlay == nil {
		return nil
	}

	result := make(map[string]string)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}
