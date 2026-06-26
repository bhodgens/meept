package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/skills"
)

// maxRelevantSkills limits how many relevance-matched skills appear in the
// system prompt to avoid context bloat.
const maxRelevantSkills = 5

// ContextInjector merges learning patterns and user instructions
// into system prompts for context enrichment.
type ContextInjector struct {
	learning     *selfimprove.LearningPipeline
	instructions *preferences.Store
	skillLoader  *skills.LazySkillLoader
}

// NewContextInjector creates a new context injector.
func NewContextInjector(
	learning *selfimprove.LearningPipeline,
	instructions *preferences.Store,
) *ContextInjector {
	return &ContextInjector{
		learning:     learning,
		instructions: instructions,
	}
}

// SetSkillLoader wires the lazy skill loader for system-prompt skill injection.
// Nil-safe: passing nil is a no-op.
func (c *ContextInjector) SetSkillLoader(loader *skills.LazySkillLoader) {
	if loader != nil {
		c.skillLoader = loader
	}
}

// BuildSystemPrompt builds a system prompt with active user instructions
// and skills injected. The base prompt is used as the relevance query for
// skill filtering: skills whose name, description, tags, or examples match
// the task context in base are prioritized. If no relevance matches are
// found (or the base is empty), all cached skills are included as fallback.
//
// Per Phase 4 spec 4.2 + turbo Thread E self-reflection:
// - Standing instructions from preferences.Store
// - Active skills filtered by relevance to base
// - Learned patterns section removed (patterns deprecated; skills replace)
func (c *ContextInjector) BuildSystemPrompt(ctx context.Context, base string) string {
	var sb strings.Builder
	sb.WriteString(base)

	// Get active instructions
	var instructions []*preferences.UserInstruction
	if c.instructions != nil {
		instructions = c.instructions.GetActive()
	}

	// Compute relevant skill names: use relevance filtering when possible,
	// fall back to all cached skills when no query signal or no matches.
	var skillNames []string
	if c.skillLoader != nil {
		skillNames = c.relevantSkillNames(base)
	}

	// Inject context if we have instructions or active skills.
	if len(instructions) > 0 || len(skillNames) > 0 {
		sb.WriteString("\n\n# Active Context\n")

		// Standing instructions section
		if len(instructions) > 0 {
			sb.WriteString("\n## Standing Instructions\n")
			sb.WriteString("The following automated actions are configured and will execute when their triggers match:\n\n")
			for i, instr := range instructions {
				sb.WriteString(fmt.Sprintf("%d. **%s** (trigger: `%s`, action: `%s`)\n",
					i+1, instr.ID, instr.Trigger, instr.Action))
				if instr.Scope == "project" {
					sb.WriteString("   _Scope: This project only_\n")
				}
			}
		}

		// Active skills section: relevance-filtered subset of cached skills.
		if len(skillNames) > 0 {
			sb.WriteString("\n## Active Skills\n")
			sb.WriteString("The following skills are loaded and may be relevant to this task:\n\n")
			for _, name := range skillNames {
				if s := c.skillLoader.Get(name); s != nil {
					desc := s.Description
					if desc == "" {
						desc = "(no description)"
					}
					sb.WriteString(fmt.Sprintf("- **%s**: %s\n", s.Name, desc))
				}
			}
		}

		sb.WriteString("\nWhen triggers occur, execute associated actions automatically.\n")
	}

	return sb.String()
}

// relevantSkillNames returns a relevance-filtered list of cached skill names.
// When base is non-empty and the skill index is available, MatchAll ranks
// skills by fuzzy match score. Only skills with score > 0 that are also in
// cache are returned, capped at maxRelevantSkills. If no relevance matches
// are found (or base is empty, or no index), all cached names are returned
// as fallback so skills are never silently dropped.
func (c *ContextInjector) relevantSkillNames(base string) []string {
	cached := c.skillLoader.CachedNames()
	if len(cached) == 0 {
		return nil
	}

	if base == "" {
		return cached
	}

	idx := c.skillLoader.Index()
	if idx == nil {
		return cached
	}

	matches := idx.MatchAll(base)
	if len(matches) == 0 {
		return cached
	}

	// Build a set of cached skill names for O(1) lookup.
	cachedSet := make(map[string]bool, len(cached))
	for _, name := range cached {
		cachedSet[strings.ToLower(name)] = true
	}

	// Collect matched skills that are also cached, up to maxRelevantSkills.
	var result []string
	for _, m := range matches {
		if len(result) >= maxRelevantSkills {
			break
		}
		nameLower := strings.ToLower(m.Entry.Name)
		if cachedSet[nameLower] {
			result = append(result, m.Entry.Name)
		}
	}

	// If no matched skills are cached, fall back to all cached skills.
	if len(result) == 0 {
		return cached
	}

	return result
}

// HasActiveInstructions returns true if there are active user instructions.
func (c *ContextInjector) HasActiveInstructions() bool {
	if c.instructions == nil {
		return false
	}
	return len(c.instructions.GetActive()) > 0
}

// HasLearnedPatterns returns true if there are learned patterns available.
func (c *ContextInjector) HasLearnedPatterns(ctx context.Context) bool {
	if c.learning == nil {
		return false
	}
	patterns, _ := c.learning.Retrieve(ctx, "general", "all", 1)
	return len(patterns) > 0
}

// GetActiveInstructions returns all active user instructions.
func (c *ContextInjector) GetActiveInstructions() []*preferences.UserInstruction {
	if c.instructions == nil {
		return nil
	}
	return c.instructions.GetActive()
}
