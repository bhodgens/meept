package skills

import (
	"sort"
	"strings"
	"sync"
)

// SkillIndexEntry holds skill metadata only (no body) for fast lookup.
//nolint:revive // stutter with package name is intentional for API clarity
type SkillIndexEntry struct {
	// Name is the unique identifier for the skill.
	Name string `json:"name"`
	// Description is a human-readable description.
	Description string `json:"description"`
	// Requires lists capability tags (e.g., ["code", "reasoning"]).
	Requires []string `json:"requires,omitempty"`
	// Tags are categorization labels.
	Tags []string `json:"tags,omitempty"`
	// Path is the filesystem path for lazy loading.
	Path string `json:"path"`
	// Priority indicates the discovery tier (0=project, 1=user, 2=system).
	Priority int `json:"priority"`
	// RiskLevel is the risk classification: "low", "medium", "high".
	RiskLevel string `json:"risk_level"`
	// AllowedTools is a subset of tool names this skill may use.
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// Examples are sample prompts for trigger matching.
	Examples []string `json:"examples,omitempty"`
}

// HasCapability checks if the entry requires a specific capability.
func (e *SkillIndexEntry) HasCapability(capability string) bool {
	for _, c := range e.Requires {
		if strings.EqualFold(c, capability) {
			return true
		}
	}
	return false
}

// HasTag checks if the entry has a specific tag.
func (e *SkillIndexEntry) HasTag(tag string) bool {
	for _, t := range e.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// SkillIndex provides fast lookup of skill metadata without bodies.
//nolint:revive // stutter with package name is intentional for API clarity
type SkillIndex struct {
	mu      sync.RWMutex
	entries map[string]*SkillIndexEntry // normalized name -> entry
	byTag   map[string][]*SkillIndexEntry
	byCap   map[string][]*SkillIndexEntry
}

// NewSkillIndex creates a new empty skill index.
func NewSkillIndex() *SkillIndex {
	return &SkillIndex{
		entries: make(map[string]*SkillIndexEntry),
		byTag:   make(map[string][]*SkillIndexEntry),
		byCap:   make(map[string][]*SkillIndexEntry),
	}
}

// Index adds or updates an entry in the index.
func (idx *SkillIndex) Index(entry *SkillIndexEntry) {
	if entry == nil || entry.Name == "" {
		return
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	key := normalizeName(entry.Name)

	// Remove existing entry from secondary indices if present
	if existing, ok := idx.entries[key]; ok {
		idx.removeFromSecondaryIndices(existing)
	}

	// Add to main index
	idx.entries[key] = entry

	// Add to tag index
	for _, tag := range entry.Tags {
		tagKey := strings.ToLower(tag)
		idx.byTag[tagKey] = append(idx.byTag[tagKey], entry)
	}

	// Add to capability index
	for _, capName := range entry.Requires {
		capKey := strings.ToLower(capName)
		idx.byCap[capKey] = append(idx.byCap[capKey], entry)
	}
}

// removeFromSecondaryIndices removes an entry from tag and capability indices.
func (idx *SkillIndex) removeFromSecondaryIndices(entry *SkillIndexEntry) {
	// Remove from tag index
	for _, tag := range entry.Tags {
		tagKey := strings.ToLower(tag)
		idx.byTag[tagKey] = removeEntry(idx.byTag[tagKey], entry)
	}

	// Remove from capability index
	for _, capName := range entry.Requires {
		capKey := strings.ToLower(capName)
		idx.byCap[capKey] = removeEntry(idx.byCap[capKey], entry)
	}
}

// removeEntry removes a specific entry from a slice.
func removeEntry(entries []*SkillIndexEntry, target *SkillIndexEntry) []*SkillIndexEntry {
	result := make([]*SkillIndexEntry, 0, len(entries))
	for _, e := range entries {
		if e != target {
			result = append(result, e)
		}
	}
	return result
}

// IndexAll adds multiple entries to the index.
func (idx *SkillIndex) IndexAll(entries []*SkillIndexEntry) {
	for _, entry := range entries {
		idx.Index(entry)
	}
}

// Get retrieves an entry by name (case-insensitive).
func (idx *SkillIndex) Get(name string) *SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.entries[normalizeName(name)]
}

// List returns all indexed entries sorted by name.
func (idx *SkillIndex) List() []*SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]*SkillIndexEntry, 0, len(idx.entries))
	for _, entry := range idx.entries {
		result = append(result, entry)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// Names returns sorted list of all indexed skill names.
func (idx *SkillIndex) Names() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	names := make([]string, 0, len(idx.entries))
	for _, entry := range idx.entries {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names
}

// FindByTag returns entries that have a specific tag.
func (idx *SkillIndex) FindByTag(tag string) []*SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tagKey := strings.ToLower(tag)
	entries := idx.byTag[tagKey]
	result := make([]*SkillIndexEntry, len(entries))
	copy(result, entries)
	return result
}

// FindByCapability returns entries that require a specific capability.
func (idx *SkillIndex) FindByCapability(capability string) []*SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	capKey := strings.ToLower(capability)
	entries := idx.byCap[capKey]
	result := make([]*SkillIndexEntry, len(entries))
	copy(result, entries)
	return result
}

// FindByCapabilities returns entries whose requirements are all satisfied by caps.
func (idx *SkillIndex) FindByCapabilities(caps []string) []*SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	// Build capability set
	capSet := make(map[string]bool)
	for _, c := range caps {
		capSet[strings.ToLower(c)] = true
	}

	var results []*SkillIndexEntry
	for _, entry := range idx.entries {
		if idx.entrySatisfiedBy(entry, capSet) {
			results = append(results, entry)
		}
	}

	return results
}

// entrySatisfiedBy checks if an entry's requirements are satisfied by the capability set.
func (idx *SkillIndex) entrySatisfiedBy(entry *SkillIndexEntry, capSet map[string]bool) bool {
	// Entries with no requirements are always satisfied
	if len(entry.Requires) == 0 {
		return true
	}

	for _, req := range entry.Requires {
		if !capSet[strings.ToLower(req)] {
			return false
		}
	}
	return true
}

// Count returns the number of indexed entries.
func (idx *SkillIndex) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.entries)
}

// Clear removes all entries from the index.
func (idx *SkillIndex) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.entries = make(map[string]*SkillIndexEntry)
	idx.byTag = make(map[string][]*SkillIndexEntry)
	idx.byCap = make(map[string][]*SkillIndexEntry)
}

// AllTags returns all unique tags across all indexed entries.
func (idx *SkillIndex) AllTags() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	tags := make([]string, 0, len(idx.byTag))
	for tag := range idx.byTag {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// AllCapabilities returns all unique capability requirements across all entries.
func (idx *SkillIndex) AllCapabilities() []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	caps := make([]string, 0, len(idx.byCap))
	for capName := range idx.byCap {
		caps = append(caps, capName)
	}
	sort.Strings(caps)
	return caps
}

// Match performs fuzzy matching on skill name and description.
func (idx *SkillIndex) Match(query string) *SkillIndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if query == "" {
		return nil
	}

	var bestMatch *SkillIndexEntry
	bestScore := 0

	for _, entry := range idx.entries {
		score := matchEntryScore(entry, query)
		if score > bestScore {
			bestScore = score
			bestMatch = entry
		}
	}

	// Require minimum score
	if bestScore < 1 {
		return nil
	}

	return bestMatch
}

// matchEntryScore calculates a fuzzy match score for an entry.
func matchEntryScore(entry *SkillIndexEntry, query string) int {
	queryLower := strings.ToLower(query)
	score := 0
	nameLower := strings.ToLower(entry.Name)
	descLower := strings.ToLower(entry.Description)

	// Exact name match
	switch {
	case nameLower == queryLower:
		score += 100
	case strings.HasPrefix(nameLower, queryLower):
		score += 50
	case strings.Contains(nameLower, queryLower):
		score += 30
	}

	// Description match
	if strings.Contains(descLower, queryLower) {
		score += 10
	}

	// Word match in name
	queryWords := strings.Fields(queryLower)
	nameWords := strings.Fields(nameLower)
	for _, qw := range queryWords {
		for _, nw := range nameWords {
			if nw == qw {
				score += 5
			}
		}
	}

	// Tag match
	for _, tag := range entry.Tags {
		if strings.EqualFold(tag, query) {
			score += 20
		} else if strings.Contains(strings.ToLower(tag), queryLower) {
			score += 5
		}
	}

	// Example match
	for _, example := range entry.Examples {
		if strings.Contains(strings.ToLower(example), queryLower) {
			score += 15
		}
	}

	return score
}

// SkillIndexMatch holds an entry with its match score.
//nolint:revive // stutter with package name is intentional for API clarity
type SkillIndexMatch struct {
	Entry *SkillIndexEntry
	Score int
}

// MatchAll performs fuzzy matching and returns all entries with scores.
func (idx *SkillIndex) MatchAll(query string) []*SkillIndexMatch {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if query == "" {
		return nil
	}

	var matches []*SkillIndexMatch

	for _, entry := range idx.entries {
		score := matchEntryScore(entry, query)
		if score > 0 {
			matches = append(matches, &SkillIndexMatch{
				Entry: entry,
				Score: score,
			})
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	return matches
}
