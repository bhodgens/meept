package memory

// L15 of the 2026-10-05 bughunt: checkDistillDuplicate keyed purely on
// canonicalDedupeText(kind, content) — no id, no session, no source hash.
// Root AGENTS.md names "Content comparison for deduplication" verbatim as the
// canonical hacky pattern, so this contradicted a stated repo convention.
//
// The fix is a SCOPING KEY derived from the source lineage the ledger already
// carries (the source memory ids, which normalizeDistillPayload writes into the
// stored payload's evidence_ids / trigger_hints). These pins hold both halves
// of the contract:
//   - two DISTINCT lessons that restate the same principle do NOT collapse;
//   - the SAME lesson re-imported (same source ids) still dedupes.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// distillSource builds a distill source memory carrying the evidence ids the
// ledger would attach.
func distillSource(change string, evidenceIDs ...string) Memory {
	m := Memory{Category: DomainLesson, Content: change}
	if len(evidenceIDs) > 0 {
		m.Metadata = map[string]any{"evidence_ids": evidenceIDs}
	}
	return m
}

// TestDistill_DistinctLessonsRestatingTheSamePrincipleDoNotCollapse is the L15
// behavioral pin. Two DISTINCT observations produce the same principle text
// (the fake summarizer is deterministic), but they come from different source
// memories. Content comparison alone classified the second as a duplicate of
// the first and threw it away — a real lesson silently lost.
func TestDistill_DistinctLessonsRestatingTheSamePrincipleDoNotCollapse(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()
	mgr.config.Distill.SimilarityThreshold = 0.8

	// A threshold loose enough that pure content comparison would collapse
	// these: the principles are byte-identical.
	mgr.SetDistillSummarizer(&fakeDistillSummarizer{
		lessonPayload: `{"principle":"` + strings.Repeat("always run the full test suite ", 4) + `"}`,
	})

	first, err := mgr.Distill(context.Background(),
		[]Memory{distillSource("first observation about the build", "mem-a")})
	if err != nil {
		t.Fatalf("first Distill: %v", err)
	}
	if first == nil {
		t.Fatal("first Distill returned nil")
	}

	// A DIFFERENT source memory, same principle. Must be stored, not deduped.
	second, err := mgr.Distill(context.Background(),
		[]Memory{distillSource("second, unrelated observation about the build", "mem-b")})
	if err != nil {
		t.Fatalf("second Distill from a DIFFERENT source must not be classified a duplicate: %v", err)
	}
	if second == nil {
		t.Fatal("second Distill returned nil — the distinct lesson was dropped")
	}
	if second.ID == first.ID {
		t.Errorf("distinct lessons collapsed onto one row (%s)", second.ID)
	}
}

// TestDistill_SameLessonReimportedStillDedupes is the other half: a re-import
// of the SAME evidence is the re-run of one derivation, and must dedupe. The
// scoping key narrows the comparison, it does not disable it.
func TestDistill_SameLessonReimportedStillDedupes(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()
	mgr.config.Distill.SimilarityThreshold = 0.8

	mgr.SetDistillSummarizer(&fakeDistillSummarizer{
		lessonPayload: `{"principle":"` + strings.Repeat("always run the full test suite ", 4) + `"}`,
	})

	src := []Memory{distillSource("observation about the build", "mem-a")}
	if _, err := mgr.Distill(context.Background(), src); err != nil {
		t.Fatalf("first Distill: %v", err)
	}
	// Same source ids, a re-run (evolver cycle revisiting the same evidence).
	if _, err := mgr.Distill(context.Background(), src); err == nil {
		t.Fatal("re-import of the SAME source must dedupe, got no error")
	}
}

// TestDistill_DisjointSourcesAllStored: several sources whose distilled
// principles are near-identical must ALL land. This is the bulk form of the
// L15 pin — pre-fix, only the first survived.
func TestDistill_DisjointSourcesAllStored(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()
	mgr.config.Distill.SimilarityThreshold = 0.5

	mgr.SetDistillSummarizer(&fakeDistillSummarizer{
		lessonPayload: `{"principle":"` + strings.Repeat("pin dependency versions ", 6) + `"}`,
	})

	ids := []string{"mem-1", "mem-2", "mem-3"}
	for i, id := range ids {
		mem, err := mgr.Distill(context.Background(),
			[]Memory{distillSource("observation set "+id, id)})
		if err != nil {
			t.Fatalf("Distill for source %d (%s) must not dedupe against a disjoint source: %v", i, id, err)
		}
		if mem == nil {
			t.Fatalf("Distill for source %s returned nil", id)
		}
	}
}

// TestDistillScopeKey_ReadsBothCarriers pins the key derivation itself: ids
// arrive via source metadata for a CANDIDATE and inside the encoded payload for
// a STORED row, and both must produce the same key (otherwise the scoping would
// never match anything and dedupe would be dead).
func TestDistillScopeKey_ReadsBothCarriers(t *testing.T) {
	fromMeta := distillScopeKey([]Memory{distillSource("observation", "mem-a", "mem-b")})
	if fromMeta == "" {
		t.Fatal("scope key from source metadata is empty")
	}

	// The stored shape: storeDistilled writes the normalized payload (which
	// carries evidence_ids) as the row's Content.
	stored := `{"principle":"p","evidence_ids":["mem-a","mem-b"]}`
	fromPayload := distillScopeKey([]Memory{{Category: DomainLesson, Content: stored}})
	if fromPayload != fromMeta {
		t.Errorf("scope key mismatch between carriers: metadata=%q payload=%q", fromMeta, fromPayload)
	}

	// Order-independence: the same evidence set in a different order is the
	// same scope.
	reordered := distillScopeKey([]Memory{distillSource("observation", "mem-b", "mem-a")})
	if reordered != fromMeta {
		t.Errorf("scope key is order-sensitive: %q vs %q", reordered, fromMeta)
	}

	// A different evidence set is a different scope.
	other := distillScopeKey([]Memory{distillSource("observation", "mem-a", "mem-c")})
	if other == fromMeta {
		t.Errorf("different evidence sets produced the same scope key %q", other)
	}

	// No provenance at all: empty key, which means "compare on content".
	if got := distillScopeKey([]Memory{distillSource("observation")}); got != "" {
		t.Errorf("scope key with no evidence ids = %q, want empty (content decides)", got)
	}
}

// TestDistill_NoEvidenceIDsStillDedupesOnContent keeps the no-provenance path
// behaving as it did: a candidate with no lineage at all has no scope to key
// on, so content similarity is the only available (conservative) rule.
func TestDistill_NoEvidenceIDsStillDedupesOnContent(t *testing.T) {
	mgr := mustDistillManager(t)
	defer mgr.Close()
	mgr.config.Distill = config.MemoryDistillConfig{Enabled: true, SimilarityThreshold: 0.8}

	mgr.SetDistillSummarizer(&fakeDistillSummarizer{
		lessonPayload: `{"principle":"` + strings.Repeat("always verify the migration plan ", 4) + `"}`,
	})

	src := []Memory{{Category: DomainLesson, Content: "bare observation"}} // no metadata, no ids
	if _, err := mgr.Distill(context.Background(), src); err != nil {
		t.Fatalf("first Distill: %v", err)
	}
	if _, err := mgr.Distill(context.Background(), src); err == nil {
		t.Fatal("a no-provenance re-import must still dedupe on content")
	}
}
