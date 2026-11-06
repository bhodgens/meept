//go:build e2e

// Skill-index refresh e2e gate (branch 01, plans/skills-refresh-origin
// master.md contract C1 - features F1, F2). This file is C1-SURFACE ONLY:
// it exercises skills.SkillIndex.MarkDirty/IsDirty/Refresh/SetRescanSource,
// skills.CapabilityIndex.Rebuild, and the turn-boundary
// CapabilitiesBuilder.refreshIfNeeded (reached through
// PromptBuilder.BuildSystemPromptOrdered, prompt.go:548). It must compile
// the moment C1 lands and must NOT reference any C2/C4 symbols
// (lifecycle.Origin, lifecycle.Writer.WriteSkillWithOrigin, doctor output)
// - those belong to the sibling branch-2 gate (origin_test.go /
// doctor_test.go, same package).
//
//	skills-refresh-01  mid-session skill write visible next turn  - TestRefreshReflectsMidSessionWrite
//	skills-refresh-02  no-op refresh is byte-stable               - TestNoopRefreshByteStable
//
// ACCESSOR GAP (drives in-process, not through the live daemon): the e2e
// harness boots the daemon as a SEPARATE process and exposes no accessor to
// its in-process Components.SkillIndex / Components.CapabilityIndex, and no
// in-tree caller wires SkillIndex.SetRescanSource - so a live chat turn can
// never observe a mid-session refresh landing. Per the leaf's fallback, F1/
// F2 are therefore driven in-process: each test constructs the same index +
// discovery + capability index the daemon builds at components.go:8986,
// installs the local rescan source, and drives the real C1 turn-boundary
// refresh through PromptBuilder.BuildSystemPromptOrdered. The whole world is
// a t.TempDir() skill tier plus in-process objects: hermetic, no network.
package skillsrefreshorigin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/skills"
)

// refreshSkillContent is one minimal SKILL.md (frontmatter + body) for a
// fixture tier; the description is a quoted YAML scalar so the metadata
// parser keeps it valid.
func refreshSkillContent(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: \"" + description + "\"\n---\n\n" + body + "\n"
}

// refreshSeedSkill writes <tierDir>/<name>/SKILL.md into the given
// user-tier directory and returns its path.
func refreshSeedSkill(t *testing.T, tierDir, name, description, body string) string {
	t.Helper()
	dir := filepath.Join(tierDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(refreshSkillContent(name, description, body)), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// refreshWorld bundles the C1 wiring a test drives: the skill index, the
// capability index over it, and the prompt builder whose turn-boundary build
// settles a refresh.
type refreshWorld struct {
	idx     *skills.SkillIndex
	ci      *skills.CapabilityIndex
	pb      *agent.PromptBuilder
	tierDir string
}

// refreshWiredWorld builds the C1 world the way the daemon boots it
// (components.go:8986): user-tier temp dir -> Discovery(WithTiers) ->
// DiscoverMetadataOnly -> NewSkillIndex -> IndexAll -> BuildCapabilityIndex,
// then installs the LOCAL rescan source (SetRescanSource) and wires the
// capability index onto the turn-boundary prompt builder. seedNames are
// written into the tier before the boot index is built, so they are present
// at the start of every test.
func refreshWiredWorld(t *testing.T, seedNames []string) *refreshWorld {
	t.Helper()
	tierDir := t.TempDir()
	for _, n := range seedNames {
		refreshSeedSkill(t, tierDir, n, "e2e refresh fixture "+n, "fixture body for "+n)
	}

	// Mirror the daemon's discovery (components.go:8978-8987): a user-tier
	// discovery whose metadata rescan re-reads the same tier off disk.
	discovery := skills.NewDiscovery(skills.WithTiers([]skills.DiscoveryTier{
		{Path: tierDir, Priority: skills.PriorityUser},
	}))
	entries, err := discovery.DiscoverMetadataOnly()
	if err != nil {
		t.Fatalf("boot metadata discovery failed: %v", err)
	}
	idx := skills.NewSkillIndex()
	idx.IndexAll(entries)

	// The daemon builds the capability index right after the skill index
	// (components.go:8995).
	ci := skills.BuildCapabilityIndex(idx)

	// C1 local rescan source: Refresh re-reads THIS tier off disk, so a
	// mid-session write into it becomes visible on the next refresh.
	idx.SetRescanSource(func() ([]*skills.SkillIndexEntry, error) {
		return discovery.DiscoverMetadataOnly()
	})

	// Wire the capability index + skill index onto the turn-boundary
	// refresh path the way the daemon does (components.go:3660 +
	// prompt.go:548). A non-empty stable section keeps the loop-economics
	// stable prefix non-trivial so the F2 byte-stable assert is meaningful.
	cb := agent.NewCapabilitiesBuilder(idx, nil).WithCapabilityIndex(ci)
	pb := agent.NewPromptBuilder().WithConstitution("e2e-constitution").SetCapabilitiesBuilder(cb)

	return &refreshWorld{idx: idx, ci: ci, pb: pb, tierDir: tierDir}
}

// refreshCapabilitySignature is the byte-stable artifact F2 pins: a
// deterministic hash over the capability index's OBSERVABLE content
// (each skill's keyword set + the index totals). A no-op Refresh + Rebuild
// over an unchanged skill set must reproduce it exactly; any drift in
// extraction, IDF, or the in-place rebuild shows up here.
func refreshCapabilitySignature(ci *skills.CapabilityIndex, idx *skills.SkillIndex) string {
	pairs := make([]string, 0)
	for _, name := range idx.Names() {
		for _, kw := range ci.GetKeywordsForSkill(name) {
			pairs = append(pairs, name+"|"+kw)
		}
	}
	sort.Strings(pairs)
	stats := ci.Stats()
	payload, _ := json.Marshal(map[string]any{
		"skill_count": stats["skills"],
		"kw_count":    stats["keywords"],
		"pairs":       pairs,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// skills-refresh-01 (F1): a skill the agent authors mid-session (a write
// into the user tier that the write-path notifier reports via MarkDirty) is
// not in the boot index; after the next turn's boundary refresh it IS -
// visible in both the skill index and the capability index.
func TestRefreshReflectsMidSessionWrite(t *testing.T) {
	w := refreshWiredWorld(t, []string{"alpha"})

	// Turn 1 (clean boot): the mid-session skill is not yet written, so the
	// refresh is a no-op and the index carries only the boot seed.
	_, _ = w.pb.BuildSystemPromptOrdered(true)
	if w.idx.Get("beta") != nil {
		t.Fatalf("beta visible before any write; index names: %v", w.idx.Names())
	}
	if got := w.ci.SkillCount(); got != 1 {
		t.Fatalf("capability index skill count before write = %d, want 1", got)
	}

	// Mid-session write: author beta into the user tier, then the write-path
	// notifier marks the index dirty (the C1 effect - no restart needed).
	refreshSeedSkill(t, w.tierDir, "beta", "e2e refresh fixture beta", "beta body")
	w.idx.MarkDirty()

	// Turn 2: the turn-boundary refresh (prompt.go:548 -> refreshIfNeeded ->
	// SkillIndex.Refresh + CapabilityIndex.Rebuild) lands the rescan.
	_, _ = w.pb.BuildSystemPromptOrdered(true)

	// F1 assert: beta is now visible - in the skill index AND the capability
	// index - and the refresh settled the dirty flag.
	if got := w.idx.Get("beta"); got == nil {
		t.Fatalf("mid-session skill beta not visible in the index after the next turn's refresh; index names: %v", w.idx.Names())
	}
	if got := w.ci.SkillCount(); got != 2 {
		t.Fatalf("capability index skill count after refresh = %d, want 2 (must reflect the new skill)", got)
	}
	if w.idx.IsDirty() {
		t.Fatal("index still dirty after the turn-boundary refresh; refresh did not settle")
	}
}

// skills-refresh-02 (F2): with an unchanged on-disk skill set, a no-op
// refresh (MarkDirty with nothing new on disk, then the turn-boundary
// Refresh + Rebuild) leaves the loop-economics stable prefix and the
// capability index byte-identical.
func TestNoopRefreshByteStable(t *testing.T) {
	w := refreshWiredWorld(t, []string{"alpha", "gamma"})

	// Turn 1 (clean): capture the stable prompt prefix hash and the
	// capability-index byte-stable signature.
	_, prePrefix := w.pb.BuildSystemPromptOrdered(true)
	if prePrefix == "" {
		t.Fatalf("pre-refresh stable prefix hash is empty; the stable section set is vacuous")
	}
	sigBefore := refreshCapabilitySignature(w.ci, w.idx)

	// Simulate the dirty flag with NO on-disk change: the write-path notifier
	// fired, but the skill set is unchanged.
	w.idx.MarkDirty()

	// Turn 2: the turn-boundary refresh runs but rescans the identical set.
	_, postPrefix := w.pb.BuildSystemPromptOrdered(true)
	sigAfter := refreshCapabilitySignature(w.ci, w.idx)

	// F2 asserts: the loop-economics stable prefix and the capability index
	// are byte-identical across the no-op refresh.
	if prePrefix != postPrefix {
		t.Fatalf("no-op refresh drifted the stable prompt prefix: before %s, after %s", prePrefix, postPrefix)
	}
	if sigBefore != sigAfter {
		t.Fatalf("no-op refresh changed the capability index signature: before %s, after %s", sigBefore, sigAfter)
	}
	if w.idx.IsDirty() {
		t.Fatal("index still dirty after a no-op refresh")
	}
}
