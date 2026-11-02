package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/preferences"
	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/caimlas/meept/internal/skills"
)

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
// and skills injected.
//
// Per Phase 4 spec 4.2 + turbo Thread E self-reflection:
//   - Standing instructions from preferences.Store
//   - Skills: a no-cap listing of every visible cached skill, one C4 line
//     each, in alphabetical order. The per-turn base text is NOT a relevance
//     signal (C4: the listing is built once per episode so it is cache-stable
//     and never churns the cache with per-turn base).
//
// Line shape (C4, byte-exact):
//
//   - <name>: <description> [load: file_read <absolute SKILL.md path>]
//
// A skill whose frontmatter sets `disable-model-invocation: true` is omitted
// from the listing; it stays reachable via the skill executor path.
func (c *ContextInjector) BuildSystemPrompt(ctx context.Context, base string) string {
	var sb strings.Builder
	sb.WriteString(base)

	// Get active instructions. Re-scan disk so YAML files dropped since
	// daemon start (or written by handleAddRPC in another process) reach
	// the agent prompt — GetActive alone reads a map that starts empty.
	var instructions []*preferences.UserInstruction
	if c.instructions != nil {
		// A Discovery error is deliberately ignored: scanTier treats
		// missing dirs as empty, and a real walk error leaves the
		// previous in-memory map in place.
		_, _ = c.instructions.Discovery()
		instructions = c.instructions.GetActive()
	}

	// No-cap listing of every visible cached skill, alphabetical (cache-stable).
	var skillLines []string
	if c.skillLoader != nil {
		skillLines = c.visibleSkillLines()
	}

	// Inject context if we have instructions or visible skills.
	if len(instructions) > 0 || len(skillLines) > 0 {
		sb.WriteString("\n\n# Active Context\n")

		// Standing instructions section
		if len(instructions) > 0 {
			sb.WriteString("\n## Standing Instructions\n")
			sb.WriteString("The following automated actions are configured and will execute when their triggers match:\n\n")
			for i, instr := range instructions {
				fmt.Fprintf(&sb, "%d. **%s** (trigger: `%s`, action: `%s`)\n",
					i+1, instr.ID, instr.Trigger, instr.Action)
				if instr.Scope == "project" {
					sb.WriteString("   _Scope: This project only_\n")
				}
			}
		}

		// Active skills section: one line per visible cached skill, no cap.
		if len(skillLines) > 0 {
			sb.WriteString("\n## Active Skills\n")
			sb.WriteString("The following skills provide specialized instructions. Load a skill's file when the task matches its description.\n\n")
			for _, line := range skillLines {
				sb.WriteString(line)
				sb.WriteByte('\n')
			}
		}

		sb.WriteString("\nWhen triggers occur, execute associated actions automatically.\n")
	}

	return sb.String()
}

// visibleSkillLines returns one C4 listing line per visible cached skill, in
// alphabetical name order (deterministic = cache-stable). Skills whose
// frontmatter sets disable-model-invocation are hidden from the listing.
func (c *ContextInjector) visibleSkillLines() []string {
	names := c.skillLoader.CachedNames()
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)

	var out []string
	for _, name := range names {
		s := c.skillLoader.Get(name)
		if s == nil || s.DisableModelInvocation {
			continue
		}
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		out = append(out, fmt.Sprintf("- %s: %s [load: file_read %s]", s.Name, desc, s.Path))
	}
	return out
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
