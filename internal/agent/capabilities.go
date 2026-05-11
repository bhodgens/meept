package agent

import (
	"slices"
	"sort"
	"strings"
	"sync"
)

// AgentCapabilities holds the aggregated capability view for an agent.
type AgentCapabilities struct {
	// AgentID is the unique identifier for this agent.
	AgentID string `json:"agent_id"`
	// Name is the human-readable name.
	Name string `json:"name"`
	// Role is the agent's role (dispatcher, executor, reviewer).
	Role AgentRole `json:"role"`
	// Purpose is a description of what this agent does.
	Purpose string `json:"purpose"`
	// Tools is the list of tools this agent can use.
	Tools []string `json:"tools"`
	// SkillCapabilities is the union of all skill.Requires for this agent.
	SkillCapabilities map[string]bool `json:"skill_capabilities,omitempty"`
	// SkillTags is the union of all skill.Tags for this agent.
	SkillTags map[string]bool `json:"skill_tags,omitempty"`
	// AvailableSkills lists skill names this agent can invoke.
	AvailableSkills []string `json:"available_skills,omitempty"`
	// IntentTypes are derived intent types for routing.
	IntentTypes []string `json:"intent_types"`
	// Keywords are keywords for fast matching.
	Keywords []string `json:"keywords"`
}

// HasCapability checks if the agent has a specific capability.
func (ac *AgentCapabilities) HasCapability(capability string) bool {
	capLower := strings.ToLower(capability)
	return ac.SkillCapabilities[capLower]
}

// HasTag checks if the agent has a specific skill tag.
func (ac *AgentCapabilities) HasTag(tag string) bool {
	tagLower := strings.ToLower(tag)
	return ac.SkillTags[tagLower]
}

// HasIntentType checks if the agent handles a specific intent type.
func (ac *AgentCapabilities) HasIntentType(intentType string) bool {
	intentLower := strings.ToLower(intentType)
	for _, it := range ac.IntentTypes {
		if strings.ToLower(it) == intentLower {
			return true
		}
	}
	return false
}

// CapabilitiesMap provides a holistic view of all agent capabilities.
type CapabilitiesMap struct {
	mu           sync.RWMutex
	agents       map[string]*AgentCapabilities // agent ID -> capabilities
	byCapability map[string][]string           // capability -> agent IDs
	byIntentType map[string][]string           // intent type -> agent IDs
	byKeyword    map[string][]string           // keyword -> agent IDs
}

// NewCapabilitiesMap creates a new empty capabilities map.
func NewCapabilitiesMap() *CapabilitiesMap {
	return &CapabilitiesMap{
		agents:       make(map[string]*AgentCapabilities),
		byCapability: make(map[string][]string),
		byIntentType: make(map[string][]string),
		byKeyword:    make(map[string][]string),
	}
}

// Add registers an agent's capabilities.
func (cm *CapabilitiesMap) Add(caps *AgentCapabilities) {
	if caps == nil || caps.AgentID == "" {
		return
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Remove existing entry first
	if existing, ok := cm.agents[caps.AgentID]; ok {
		cm.removeFromIndices(existing)
	}

	// Add to main map
	cm.agents[caps.AgentID] = caps

	// Index by capability
	for capability := range caps.SkillCapabilities {
		capLower := strings.ToLower(capability)
		cm.byCapability[capLower] = appendUnique(cm.byCapability[capLower], caps.AgentID)
	}

	// Index by intent type
	for _, intentType := range caps.IntentTypes {
		intentLower := strings.ToLower(intentType)
		cm.byIntentType[intentLower] = appendUnique(cm.byIntentType[intentLower], caps.AgentID)
	}

	// Index by keyword
	for _, keyword := range caps.Keywords {
		kwLower := strings.ToLower(keyword)
		cm.byKeyword[kwLower] = appendUnique(cm.byKeyword[kwLower], caps.AgentID)
	}
}

// removeFromIndices removes an agent from all secondary indices.
func (cm *CapabilitiesMap) removeFromIndices(caps *AgentCapabilities) {
	// Remove from capability index
	for capability := range caps.SkillCapabilities {
		capLower := strings.ToLower(capability)
		cm.byCapability[capLower] = removeString(cm.byCapability[capLower], caps.AgentID)
	}

	// Remove from intent type index
	for _, intentType := range caps.IntentTypes {
		intentLower := strings.ToLower(intentType)
		cm.byIntentType[intentLower] = removeString(cm.byIntentType[intentLower], caps.AgentID)
	}

	// Remove from keyword index
	for _, keyword := range caps.Keywords {
		kwLower := strings.ToLower(keyword)
		cm.byKeyword[kwLower] = removeString(cm.byKeyword[kwLower], caps.AgentID)
	}
}

// appendUnique appends a string to a slice if not already present.
func appendUnique(slice []string, s string) []string {
	if slices.Contains(slice, s) {
		return slice
	}
	return append(slice, s)
}

// removeString removes a string from a slice.
func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
}

// Get retrieves an agent's capabilities by ID.
func (cm *CapabilitiesMap) Get(agentID string) *AgentCapabilities {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.agents[agentID]
}

// List returns all agent capabilities.
func (cm *CapabilitiesMap) List() []*AgentCapabilities {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]*AgentCapabilities, 0, len(cm.agents))
	for _, caps := range cm.agents {
		result = append(result, caps)
	}

	// Sort by agent ID for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].AgentID < result[j].AgentID
	})

	return result
}

// AgentIDs returns all agent IDs.
func (cm *CapabilitiesMap) AgentIDs() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ids := make([]string, 0, len(cm.agents))
	for id := range cm.agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// FindByCapability returns agent IDs that have a specific capability.
func (cm *CapabilitiesMap) FindByCapability(capability string) []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	capLower := strings.ToLower(capability)
	result := make([]string, len(cm.byCapability[capLower]))
	copy(result, cm.byCapability[capLower])
	return result
}

// FindByIntentType returns agent IDs that handle a specific intent type.
func (cm *CapabilitiesMap) FindByIntentType(intentType string) []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	intentLower := strings.ToLower(intentType)
	result := make([]string, len(cm.byIntentType[intentLower]))
	copy(result, cm.byIntentType[intentLower])
	return result
}

// FindByKeyword returns agent IDs associated with a keyword.
func (cm *CapabilitiesMap) FindByKeyword(keyword string) []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	kwLower := strings.ToLower(keyword)
	result := make([]string, len(cm.byKeyword[kwLower]))
	copy(result, cm.byKeyword[kwLower])
	return result
}

// MatchKeywords scans input text for keywords and returns matching agent IDs.
func (cm *CapabilitiesMap) MatchKeywords(input string) map[string]int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	inputLower := strings.ToLower(input)
	matches := make(map[string]int)

	for keyword, agentIDs := range cm.byKeyword {
		if strings.Contains(inputLower, keyword) {
			for _, agentID := range agentIDs {
				matches[agentID]++
			}
		}
	}

	return matches
}

// AllCapabilities returns all unique capabilities across all agents.
func (cm *CapabilitiesMap) AllCapabilities() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	caps := make([]string, 0, len(cm.byCapability))
	for capability := range cm.byCapability {
		caps = append(caps, capability)
	}
	sort.Strings(caps)
	return caps
}

// AllIntentTypes returns all unique intent types across all agents.
func (cm *CapabilitiesMap) AllIntentTypes() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	types := make([]string, 0, len(cm.byIntentType))
	for t := range cm.byIntentType {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// AllKeywords returns all unique keywords across all agents.
func (cm *CapabilitiesMap) AllKeywords() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	kws := make([]string, 0, len(cm.byKeyword))
	for kw := range cm.byKeyword {
		kws = append(kws, kw)
	}
	sort.Strings(kws)
	return kws
}

// Count returns the number of agents in the map.
func (cm *CapabilitiesMap) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.agents)
}

// Clear removes all agents from the map.
func (cm *CapabilitiesMap) Clear() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.agents = make(map[string]*AgentCapabilities)
	cm.byCapability = make(map[string][]string)
	cm.byIntentType = make(map[string][]string)
	cm.byKeyword = make(map[string][]string)
}
