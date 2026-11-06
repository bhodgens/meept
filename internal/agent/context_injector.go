package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

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

	// Episode-start skill ranking (token-efficiency branch 04 leaf 02,
	// Q4: always rank). mu guards the episode state below; it is never
	// held across the embedding I/O inside visibleSkillLinesForEpisode.
	mu sync.Mutex
	// episodeTask is the latest task text, set once per episode via
	// SetEpisodeTask. Empty = no ranking signal (alphabetical listing).
	episodeTask string
	// rankedFor / rankedNames cache the ranked order for episodeTask so
	// every prompt build within one episode reuses the EXACT same order —
	// the skills section stays byte-stable within the episode even if the
	// embedder's cache or timings change underneath.
	rankedFor   string
	rankedNames []string
	rankedSet   bool
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

// SetEpisodeTask marks the start of a new episode and records its task text
// as the ranking signal for the skill listing (leaf 02: "rank at episode
// start"). Called once per episode by the agent loop; subsequent
// BuildSystemPrompt calls in the same episode reuse the frozen order, so
// per-turn churn never invalidates the prompt cache. Empty taskText keeps
// the listing alphabetical (no signal). Setting the SAME text again does not
// invalidate the frozen order (idempotent within an episode). Nil-safe.
func (c *ContextInjector) SetEpisodeTask(taskText string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.episodeTask == taskText {
		return
	}
	c.episodeTask = taskText
	c.rankedFor = ""
	c.rankedNames = nil
	c.rankedSet = false
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

	// No-cap listing of every visible cached skill. Order: embedding-rank
	// by the episode's task text when a ranker is wired (leaf 02, Q4:
	// always rank); alphabetical otherwise (cache-stable default).
	var skillLines []string
	if c.skillLoader != nil {
		skillLines = c.visibleSkillLinesForEpisode()
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

// visibleSkillLinesForEpisode returns one C4 listing line per visible cached
// skill in the episode's frozen order. The order is computed ONCE per
// episode (first build after SetEpisodeTask) and cached, so every
// subsequent build in the same episode emits byte-identical listing bytes —
// the prompt-cache stability contract. Ranking is order-only: the SET of
// lines equals the alphabetical set exactly (no cap, no hiding below the
// disable-model-invocation rule from leaf 01).
func (c *ContextInjector) visibleSkillLinesForEpisode() []string {
	names := c.skillLoader.CachedNames()
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names) // deterministic fallback (alphabetical)

	c.mu.Lock()
	task := c.episodeTask
	ordered := c.rankedNames
	frozen := c.rankedSet && c.rankedFor == task
	c.mu.Unlock()

	if !frozen {
		// Compute the episode's order OUTSIDE every lock: RankedNames may
		// hit the embedding endpoint, and the mutexio invariant forbids
		// holding a mutex across I/O. Concurrent first builds of one
		// episode may each compute, but the ranking is deterministic, so
		// whichever store wins produces byte-identical listings.
		ranked := c.rankSkillNames(task, names)
		c.mu.Lock()
		c.rankedFor = task
		c.rankedNames = ranked
		c.rankedSet = true
		c.mu.Unlock()
		ordered = ranked
	}

	// Order-only merge: the listing SET must equal the current visible
	// cache. Skills cached AFTER the episode's order froze (a mid-episode
	// refresh landing new names) are appended in alphabetical order —
	// ranking never hides a skill, and a stale frozen order must never
	// drop one. Names in the frozen order that left the cache resolve to
	// nil below and vanish with them, which is the cache's own contract.
	if len(ordered) != len(names) {
		inOrdered := make(map[string]bool, len(ordered))
		for _, n := range ordered {
			inOrdered[n] = true
		}
		merged := make([]string, 0, len(names))
		merged = append(merged, ordered...)
		for _, n := range names {
			if !inOrdered[n] {
				merged = append(merged, n)
			}
		}
		ordered = merged
	}

	var out []string
	for _, name := range ordered {
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

// rankSkillNames asks the skill index for an embedding-ranked order of the
// alphabetical fallback, if an embedder is wired. Without a ranker (no
// index, no embedder, empty task) the fallback order is returned untouched.
// RankedNames itself degrades silently on any embedding failure.
func (c *ContextInjector) rankSkillNames(taskText string, fallback []string) []string {
	if strings.TrimSpace(taskText) == "" || c.skillLoader == nil {
		return fallback
	}
	idx := c.skillLoader.Index()
	if idx == nil {
		return fallback
	}
	return idx.RankedNames(taskText, fallback)
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
