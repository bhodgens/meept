package skills

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// Registry holds loaded skills with lookup by name, tag, and capability.
type Registry struct {
	mu       sync.RWMutex
	skills   map[string]*Skill // normalized name -> skill
	logger   *slog.Logger
}

// RegistryOption is a functional option for configuring Registry.
type RegistryOption func(*Registry)

// WithRegistryLogger sets the logger for registry operations.
func WithRegistryLogger(logger *slog.Logger) RegistryOption {
	return func(r *Registry) {
		r.logger = logger
	}
}

// NewRegistry creates a new skill registry.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{
		skills: make(map[string]*Skill),
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Register adds or replaces a skill in the registry.
func (r *Registry) Register(skill *Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := normalizeName(skill.Name)

	if existing, ok := r.skills[key]; ok {
		r.logger.Warn("Replacing existing skill registration",
			"name", skill.Name,
			"old_path", existing.Path,
			"new_path", skill.Path,
		)
	}

	r.skills[key] = skill
	r.logger.Info("Registered skill", "name", skill.Name)
}

// RegisterAll registers multiple skills at once.
func (r *Registry) RegisterAll(skills []*Skill) {
	for _, skill := range skills {
		r.Register(skill)
	}
}

// Unregister removes a skill by name.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := normalizeName(name)
	if _, ok := r.skills[key]; ok {
		delete(r.skills, key)
		r.logger.Info("Unregistered skill", "name", name)
		return true
	}
	return false
}

// Get looks up a skill by name (case-insensitive).
func (r *Registry) Get(name string) *Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.skills[normalizeName(name)]
}

// List returns all registered skills.
func (r *Registry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Skill, 0, len(r.skills))
	for _, skill := range r.skills {
		result = append(result, skill)
	}

	// Sort by name for consistent ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// Names returns sorted list of all registered skill names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.skills))
	for _, skill := range r.skills {
		names = append(names, skill.Name)
	}
	sort.Strings(names)
	return names
}

// FindByTag returns skills that have a specific tag.
func (r *Registry) FindByTag(tag string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*Skill

	for _, skill := range r.skills {
		for _, t := range skill.Tags {
			if strings.EqualFold(t, tag) {
				results = append(results, skill)
				break
			}
		}
	}

	return results
}

// FindByTags returns skills that have all specified tags.
func (r *Registry) FindByTags(tags []string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Normalize tags
	normalizedTags := make([]string, len(tags))
	for i, t := range tags {
		normalizedTags[i] = strings.ToLower(strings.TrimSpace(t))
	}

	var results []*Skill
	for _, skill := range r.skills {
		if hasAllTags(skill, normalizedTags) {
			results = append(results, skill)
		}
	}

	return results
}

// hasAllTags checks if a skill has all the specified normalized tags.
func hasAllTags(skill *Skill, normalizedTags []string) bool {
	skillTags := make(map[string]bool)
	for _, t := range skill.Tags {
		skillTags[strings.ToLower(t)] = true
	}

	for _, tag := range normalizedTags {
		if !skillTags[tag] {
			return false
		}
	}
	return true
}

// FindByCapability returns skills that require a specific capability.
func (r *Registry) FindByCapability(capability string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*Skill

	for _, skill := range r.skills {
		for _, c := range skill.Requires {
			if strings.EqualFold(c, capability) {
				results = append(results, skill)
				break
			}
		}
	}

	return results
}

// FindByCapabilities returns skills that can be satisfied by the given capabilities.
// A skill matches if ALL its requirements are present in the provided capabilities.
func (r *Registry) FindByCapabilities(caps []string) []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Build capability set
	capSet := make(map[string]bool)
	for _, c := range caps {
		capSet[strings.ToLower(strings.TrimSpace(c))] = true
	}

	var results []*Skill
	for _, skill := range r.skills {
		if skillSatisfiedBy(skill, capSet) {
			results = append(results, skill)
		}
	}

	return results
}

// skillSatisfiedBy checks if a skill's requirements are satisfied by the capability set.
func skillSatisfiedBy(skill *Skill, capSet map[string]bool) bool {
	// Skills with no requirements are always satisfied
	if len(skill.Requires) == 0 {
		return true
	}

	for _, req := range skill.Requires {
		if !capSet[strings.ToLower(req)] {
			return false
		}
	}
	return true
}

// Match performs fuzzy matching on skill name and description.
// Returns the best matching skill, or nil if no match.
func (r *Registry) Match(query string) *Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if query == "" {
		return nil
	}

	queryLower := strings.ToLower(query)
	var bestMatch *Skill
	bestScore := 0

	for _, skill := range r.skills {
		score := matchScore(skill, queryLower)
		if score > bestScore {
			bestScore = score
			bestMatch = skill
		}
	}

	// Require minimum score to return a match
	if bestScore < 1 {
		return nil
	}

	return bestMatch
}

// MatchAll performs fuzzy matching and returns all skills with scores.
// Returns skills sorted by score (highest first).
func (r *Registry) MatchAll(query string) []*SkillMatch {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if query == "" {
		return nil
	}

	queryLower := strings.ToLower(query)
	var matches []*SkillMatch

	for _, skill := range r.skills {
		score := matchScore(skill, queryLower)
		if score > 0 {
			matches = append(matches, &SkillMatch{
				Skill: skill,
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

// SkillMatch holds a skill with its match score.
//nolint:revive // stutter with package name is intentional for API clarity
type SkillMatch struct {
	Skill *Skill
	Score int
}

// matchScore calculates a fuzzy match score for a skill against a query.
func matchScore(skill *Skill, queryLower string) int {
	score := 0
	nameLower := strings.ToLower(skill.Name)
	descLower := strings.ToLower(skill.Description)

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
	for _, tag := range skill.Tags {
		if strings.EqualFold(tag, queryLower) {
			score += 20
		} else if strings.Contains(strings.ToLower(tag), queryLower) {
			score += 5
		}
	}

	return score
}

// Count returns the number of registered skills.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}

// Clear removes all skills from the registry.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills = make(map[string]*Skill)
	r.logger.Info("Cleared all skills from registry")
}

// GetRequirements returns the capability requirements for a named skill.
func (r *Registry) GetRequirements(name string) []string {
	skill := r.Get(name)
	if skill == nil {
		return nil
	}
	return skill.Requires
}
