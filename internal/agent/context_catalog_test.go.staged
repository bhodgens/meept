package agent

// F11 gate T3/T3b catalog arm (04-tool-surface/00T-tests.md): the durable
// skill catalog replaces the process-cache relist in visibleSkillLines
// (context_injector.go:115-135) with a session message materialized at turn
// start listing every visible model-invocable skill, digest-deduplicated
// (sha256 over sorted name+description) so the catalog is reconstructable
// after resume/restart. Pins contract B3.
//
// RED until leaf 03-skill-catalog lands (SkillCatalog/Digest undefined).

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/skills"
)

func skillEntries() []skills.SkillIndexEntry {
	return []skills.SkillIndexEntry{
		{Name: "b-skill", Description: "second"},
		{Name: "a-skill", Description: "first"},
	}
}

func TestSkillCatalog_DigestStableAcrossOrder(t *testing.T) {
	// sha256 over SORTED name+description: the same visible set in a
	// different input order yields an identical digest.
	c1 := NewSkillCatalog(skillEntries())
	reordered := []skills.SkillIndexEntry{
		{Name: "a-skill", Description: "first"},
		{Name: "b-skill", Description: "second"},
	}
	c2 := NewSkillCatalog(reordered)
	if c1.Digest() != c2.Digest() {
		t.Errorf("digest unstable across input order: %q vs %q", c1.Digest(), c2.Digest())
	}
	if c1.Digest() == "" {
		t.Fatal("digest must not be empty")
	}
}

func TestSkillCatalog_DigestChangesWithContent(t *testing.T) {
	c1 := NewSkillCatalog(skillEntries())
	changed := skillEntries()
	changed[0].Description = "second (edited)"
	c2 := NewSkillCatalog(changed)
	if c1.Digest() == c2.Digest() {
		t.Error("digest must change when a description changes")
	}
}

func TestSkillCatalog_DedupeByDigest(t *testing.T) {
	// Re-materializing the SAME visible set must not append a duplicate
	// catalog message: the injector compares the digest of the last
	// materialized catalog.
	c := NewSkillCatalog(skillEntries())
	inj := &catalogInjector{lastDigest: ""}
	if !inj.ShouldMaterialize(c) {
		t.Error("first materialization must be allowed")
	}
	inj.MarkMaterialized(c)
	if inj.ShouldMaterialize(NewSkillCatalog(skillEntries())) {
		t.Error("identical catalog re-materialized: digest dedupe failed")
	}
	// A changed set MUST re-materialize.
	changed := skillEntries()
	changed = append(changed, skills.SkillIndexEntry{Name: "c-skill", Description: "third"})
	if !inj.ShouldMaterialize(NewSkillCatalog(changed)) {
		t.Error("changed catalog must re-materialize")
	}
}

func TestSkillCatalog_RebuildFromRecord(t *testing.T) {
	// The catalog is reconstructable from the persisted session record
	// after resume/restart: Render -> parse -> Render round-trip equality.
	c := NewSkillCatalog(skillEntries())
	rendered := c.Render()
	rebuilt, err := ParseSkillCatalogMessage(rendered)
	if err != nil {
		t.Fatalf("ParseSkillCatalogMessage: %v", err)
	}
	if rebuilt.Digest() != c.Digest() {
		t.Errorf("rebuilt digest %q != original %q", rebuilt.Digest(), c.Digest())
	}
}

func TestSkillCatalog_ExcludesNonModelInvocable(t *testing.T) {
	// Skills with disable-model-invocation are hidden from the listing
	// (visibleSkillLines parity).
	entries := []skills.SkillIndexEntry{
		{Name: "visible", Description: "v"},
		{Name: "hidden", Description: "h", DisableModelInvocation: true},
	}
	c := NewSkillCatalog(entries)
	rendered := c.Render()
	if strings.Contains(rendered, "hidden") {
		t.Error("catalog rendered a disable-model-invocation skill")
	}
	if !strings.Contains(rendered, "visible") {
		t.Error("catalog dropped a visible skill")
	}
}
