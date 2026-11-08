package agent

// DSH-4 wiring remediation: the production prompt path sources the skill
// listing from the DURABLE discovery snapshot (SkillIndex.List), not merely
// the loader cache. A skill present in discovery but never cache-loaded must
// still be listed (leaf 03 T4 PASS state), and the materialized catalog is
// digest-deduplicated per session (T3). Pins the wiring contract through the
// real injector + loader.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

// TestPromptListsUncachedDiscoverySkill pins T4: an indexed (discovered)
// skill with a COLD cache still appears in the prompt listing — the
// cache-only CachedNames() source dropped it until a fuzzy hit loaded it.
func TestPromptListsUncachedDiscoverySkill(t *testing.T) {
	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "durable-skill",
		Description: "discovered but never cache-loaded",
		Path:        "/skills/durable-skill/SKILL.md",
	})
	loader := skills.NewLazySkillLoader(idx) // nothing loaded -> cache empty

	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	if names := loader.CachedNames(); len(names) != 0 {
		t.Fatalf("fixture leak: cache should be empty, got %v", names)
	}

	prompt := ci.BuildSystemPrompt(context.Background(), "base")
	if !strings.Contains(prompt, "- durable-skill: discovered but never cache-loaded [load: file_read /skills/durable-skill/SKILL.md]") {
		t.Errorf("discovered-but-uncached skill missing from the prompt listing:\n%s", prompt)
	}
}

// TestCatalogMaterializeDigestDedup pins T3 through the production seam:
// the first materialization fires (baseline), identical re-materializations
// are suppressed, and a changed visible set re-materializes.
func TestCatalogMaterializeDigestDedup(t *testing.T) {
	idx := skills.NewSkillIndex()
	idx.Index(&skills.SkillIndexEntry{
		Name:        "alpha",
		Description: "Alpha skill",
		Path:        "/skills/alpha/SKILL.md",
	})
	loader := skills.NewLazySkillLoader(idx)
	ci := NewContextInjector(nil, nil)
	ci.SetSkillLoader(loader)

	rendered, changed := ci.MaterializeSkillCatalog()
	if !changed {
		t.Fatal("first materialization must fire (baseline catalog)")
	}
	if !strings.Contains(rendered, "- alpha: Alpha skill") {
		t.Errorf("baseline catalog missing alpha:\n%s", rendered)
	}

	// Same visible set: deduped.
	if _, changed := ci.MaterializeSkillCatalog(); changed {
		t.Error("identical visible set re-materialized: digest dedup failed")
	}

	// A new skill lands in discovery: the set changed -> re-materializes.
	idx.Index(&skills.SkillIndexEntry{
		Name:        "bravo",
		Description: "Bravo skill",
		Path:        "/skills/bravo/SKILL.md",
	})
	rendered2, changed := ci.MaterializeSkillCatalog()
	if !changed {
		t.Fatal("changed visible set must re-materialize")
	}
	if !strings.Contains(rendered2, "- bravo: Bravo skill") {
		t.Errorf("re-materialized catalog missing bravo:\n%s", rendered2)
	}

	// Catalog is reconstructable from the rendered form (durability): the
	// digest of the persisted record equals the live digest.
	live := NewSkillCatalog(func() []skills.SkillIndexEntry {
		listed := idx.List()
		out := make([]skills.SkillIndexEntry, 0, len(listed))
		for _, e := range listed {
			out = append(out, *e)
		}
		return out
	}())
	rebuilt, err := ParseSkillCatalogMessage(rendered2)
	if err != nil {
		t.Fatalf("ParseSkillCatalogMessage: %v", err)
	}
	if rebuilt.Digest() != live.Digest() {
		t.Errorf("rebuilt digest %q != live digest %q", rebuilt.Digest(), live.Digest())
	}
}

// TestCatalogMaterializeNilSafe pins the nil-safety of the production seam:
// a nil injector and a loader-less injector are no-ops.
func TestCatalogMaterializeNilSafe(t *testing.T) {
	var ci *ContextInjector
	if rendered, changed := ci.MaterializeSkillCatalog(); changed || rendered != "" {
		t.Errorf("nil injector must no-op, got rendered=%q changed=%v", rendered, changed)
	}
	bare := NewContextInjector(nil, nil)
	if rendered, changed := bare.MaterializeSkillCatalog(); changed || rendered != "" {
		t.Errorf("loader-less injector must no-op, got rendered=%q changed=%v", rendered, changed)
	}
}
