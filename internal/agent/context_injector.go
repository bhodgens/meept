package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// planModePolicySection returns the plan-policy prompt section when plan
	// mode is active (for the prompt's session), else "". When set and the
	// returned text is non-empty, it is appended to BuildSystemPrompt so the
	// plan-mode collaboration policy reaches the model only while active.
	// Nil-safe: unset means no plan-mode injection.
	planModePolicySection func() string
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

// SetPlanModePolicySection wires the plan-mode plan:policy prompt provider. It
// returns the plan-policy section text while plan mode is active, else "". A
// nil arg is a no-op (typed-nil guard); an unset provider disables plan-mode
// injection entirely.
func (c *ContextInjector) SetPlanModePolicySection(fn func() string) {
	if fn != nil {
		c.planModePolicySection = fn
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

	// Plan-mode plan:policy section — injected only while plan mode is active
	// for the prompt's session (provider set and returns non-empty). Unset or
	// inactive leaves the prompt byte-identical to before.
	if c.planModePolicySection != nil {
		if section := c.planModePolicySection(); section != "" {
			sb.WriteString("\n\n")
			sb.WriteString(section)
		}
	}

	return sb.String()
}

// visibleSkillLines returns one C4 listing line per visible cached skill, in
// alphabetical name order (deterministic = cache-stable). Skills whose
// frontmatter sets disable-model-invocation are hidden from the listing.
// The lines are generated through the skillCatalog type so the line shape and
// digest-derived ordering stay single-sourced with the durable catalog.
func (c *ContextInjector) visibleSkillLines() []string {
	if c.skillLoader == nil {
		return nil
	}
	names := c.skillLoader.CachedNames()
	if len(names) == 0 {
		return nil
	}

	var entries []skills.SkillIndexEntry
	for _, name := range names {
		s := c.skillLoader.Get(name)
		if s == nil || s.DisableModelInvocation {
			continue
		}
		entries = append(entries, skills.SkillIndexEntry{
			Name:        s.Name,
			Description: s.Description,
			Path:        s.Path,
		})
	}
	rendered := NewSkillCatalog(entries).Render()
	if rendered == "" {
		return nil
	}
	return strings.Split(rendered, "\n")
}

// ---------------------------------------------------------------------------
// Durable skill catalog (dsh 04-tool-surface/03-skill-catalog, master C9).
//
// Replaces the process-cache-only relist with a digest-stable snapshot of
// every visible model-invocable skill sourced from DISCOVERY — not merely the
// live loader cache. The catalog is rendered once per digest period and is
// meant to be persisted as a session record, so a resume/replay rebuilds the
// same listing from the session store instead of the in-memory cache. The
// digest dedup is what lets materialization fire only when the visible set
// actually changes.
// ---------------------------------------------------------------------------

// skillCatalog is an immutable, digest-stable snapshot of a visible skill set.
// It is source-agnostic: callers build it from a fresh discovery / index
// snapshot OR from reconstructed entries parsed back out of a rendered (and
// thus persisted) catalog message.
type skillCatalog struct {
	entries  []skills.SkillIndexEntry // visible-only, name-sorted
	digest   string
	rendered string
}

// NewSkillCatalog builds a catalog from a discovery snapshot (the full indexed
// / discovered skill set). Skills with disable-model-invocation are omitted
// (visibleSkillLines parity). The digest is stable across input order and is
// sha256 over the name-sorted "name	description" pairs.
func NewSkillCatalog(entries []skills.SkillIndexEntry) *skillCatalog {
	visible := make([]skills.SkillIndexEntry, 0, len(entries))
	for _, e := range entries {
		if e.DisableModelInvocation {
			continue
		}
		visible = append(visible, e)
	}
	// Name-sort the visible set so both the digest and the rendered listing
	// are deterministically ordered regardless of the caller's input order
	// (e.g. an unordered cache map). Without this, a catalog rebuilt from a
	// differently-ordered input hashes differently and the render churns.
	sort.Slice(visible, func(i, j int) bool {
		return visible[i].Name < visible[j].Name
	})
	c := &skillCatalog{entries: visible}
	c.digest = catalogDigest(visible)
	c.rendered = renderCatalog(visible)
	return c
}

// Digest returns the stable sha256 digest of the visible skill set.
func (c *skillCatalog) Digest() string {
	if c == nil {
		return ""
	}
	return c.digest
}

// Render returns the C4-format listing: one "- name: desc [load: file_read
// path]" line per visible skill, name-sorted, joined by newlines.
func (c *skillCatalog) Render() string {
	if c == nil {
		return ""
	}
	return c.rendered
}

// catalogDigest computes sha256 over the name-sorted entries, hashing the
// exact strings a resuming session must reproduce (name + rendered
// description). The load path is intentionally NOT part of the identity: it is
// derivable from the name via current discovery and may legitimately move
// between sessions.
func catalogDigest(entries []skills.SkillIndexEntry) string {
	sorted := make([]skills.SkillIndexEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	h := sha256.New()
	for _, e := range sorted {
		h.Write([]byte(e.Name))
		h.Write([]byte("	"))
		h.Write([]byte(catalogDescription(e.Description)))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// catalogDescription returns the description text a catalog line renders (and
// digests): an empty description renders as "(no description)" so a catalog
// rebuilt from its rendered form hashes identically to the original.
func catalogDescription(desc string) string {
	if desc == "" {
		return "(no description)"
	}
	return desc
}

// renderCatalog emits the C4 listing lines for the (name-sorted) visible
// entries.
func renderCatalog(entries []skills.SkillIndexEntry) string {
	var sb strings.Builder
	for i, e := range entries {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "- %s: %s [load: file_read %s]",
			e.Name, catalogDescription(e.Description), e.Path)
	}
	return sb.String()
}

// ParseSkillCatalogMessage reconstructs a catalog from its rendered form (the
// persisted session record). It re-derives name + description per C4 line and
// recomputes the digest, so a resume that reads the old session record
// reproduces the same digest without the live loader cache.
func ParseSkillCatalogMessage(rendered string) (*skillCatalog, error) {
	entries := make([]skills.SkillIndexEntry, 0)
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "- ") {
			continue
		}
		rest := strings.TrimPrefix(line, "- ")
		// Strip the "[load: file_read <path>]" trailer, if present.
		if i := strings.Index(rest, " [load: file_read "); i >= 0 {
			rest = rest[:i]
		}
		// Split "name: desc" on the first ": ".
		name, desc := rest, ""
		if i := strings.Index(rest, ": "); i >= 0 {
			name, desc = rest[:i], rest[i+2:]
		}
		if name == "" {
			continue
		}
		entries = append(entries, skills.SkillIndexEntry{Name: name, Description: desc})
	}
	return NewSkillCatalog(entries), nil
}

// catalogInjector owns the digest-dedup decision for materializing a durable
// skill-catalog session record: it re-materializes only when the visible skill
// set actually changed (the digest differs from the last materialized one).
// A fresh injector (lastDigest "") always materializes the first catalog so a
// new session gets a baseline even if a zero-length set.
type catalogInjector struct {
	lastDigest string
}

// ShouldMaterialize reports whether a catalog's visible set differs from the
// last one this injector materialized.
func (inj *catalogInjector) ShouldMaterialize(c *skillCatalog) bool {
	if inj == nil || c == nil {
		return false
	}
	return inj.lastDigest != c.Digest()
}

// MarkMaterialized records a catalog's digest as the most recently
// materialized one.
func (inj *catalogInjector) MarkMaterialized(c *skillCatalog) {
	if inj == nil || c == nil {
		return
	}
	inj.lastDigest = c.Digest()
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
