package tierpolicy

// L15 (bughunt 2026-11-27, T-6) pins: rows the distill worker already
// compressed (memory.RowCompressedKey = l1_compressed) must leave the
// compress selection. Before the fix the flag was write-only for the
// selector: the hard band re-selected the same oldest flag-set rows every
// pass (the worker answered "suppressed"), and the same-pass exclude set
// kept those IDs out of the evict pool — so the band never reclaimed
// anything through them and the tier stayed over ceiling forever.
//
// The pins assert the three observable consequences:
//  1. compress actions never target flag-set rows;
//  2. a pool that contains compressed rows produces a DIFFERENT candidate
//     set than the same pool without the flag (T-6's exact defect shape:
//     the pre-fix band re-selected identical rows every pass);
//  3. with compressed rows excluded from compress, the hard band's evict
//     action can reach them, reducing the over-band amount (reclamation
//     actually happens across passes).

import (
	"context"
	"fmt"
	"testing"

	"github.com/caimlas/meept/internal/memory"
)

func compressed(id, content string, ageDays int) memory.MemoryResult {
	m := mem(id, content, ageDays)
	m.Memory.Metadata = map[string]any{memory.RowCompressedKey: true}
	return m
}

// TestL15_CompressSkipsCompressedRows: planCompress must never select a
// row whose metadata carries memory.RowCompressedKey — the distill worker
// would only answer "suppressed" for it, so selecting it burns the batch
// budget without reclaiming anything.
func TestL15_CompressSkipsCompressedRows(t *testing.T) {
	cands := []memory.MemoryResult{
		compressed("c1", "tiny", 12),
		compressed("c2", "tiny", 11),
		mem("live1", "tiny", 10),
		mem("live2", "tiny", 9),
		mem("live3", "tiny", 8),
		mem("live4", "tiny", 7),
		mem("live5", "tiny", 6),
		mem("live6", "tiny", 5),
	}
	e := New(enabledCfg(), Deps{
		Candidates: &stubCandidates{byTier: map[string][]memory.MemoryResult{
			"episodic": cands,
		}},
		Votes: stubVotes{},
	})
	act, err := e.planCompress(context.Background(),
		TierState{Tier: "episodic", Tokens: 95, Items: 8, Ceiling: 100}, 6)
	if err != nil {
		t.Fatalf("planCompress: %v", err)
	}
	if act == nil {
		t.Fatalf("planCompress returned no action for a pool with uncompressed rows")
	}
	for _, id := range act.TargetIDs {
		if id == "c1" || id == "c2" {
			t.Errorf("compress action targets already-compressed row %s (T-6 defect)", id)
		}
	}
	if len(act.TargetIDs) == 0 {
		t.Fatalf("compress action selected nothing from a pool with uncompressed rows: %v", act)
	}
}

// TestL15_HardBandSelectsDifferentCandidatesWithCompressedRows pins the
// T-6 defect shape directly: the same pool WITH the l1_compressed flags
// must yield a different compress selection than the identical pool
// WITHOUT them. Pre-fix both runs selected the same oldest rows — the
// flag changed nothing, which is exactly why the hard band never moved.
func TestL15_HardBandSelectsDifferentCandidatesWithCompressedRows(t *testing.T) {
	pool := func() []memory.MemoryResult {
		var out []memory.MemoryResult
		for i := 0; i < 10; i++ {
			out = append(out, mem(fmt.Sprintf("m%02d", i), "tiny", 20-i))
		}
		return out
	}

	// Baseline: no flags — oldest-first selection takes the front of the pool.
	ePlain := New(enabledCfg(), Deps{
		Candidates: &stubCandidates{byTier: map[string][]memory.MemoryResult{"episodic": pool()}},
		Votes:      stubVotes{},
	})
	actPlain, err := ePlain.planCompress(context.Background(),
		TierState{Tier: "episodic", Tokens: 95, Items: 10, Ceiling: 100}, 6)
	if err != nil {
		t.Fatalf("planCompress(plain): %v", err)
	}
	if actPlain == nil || len(actPlain.TargetIDs) == 0 {
		t.Fatalf("planCompress(plain) returned no selection")
	}

	// Flagged: the SAME rows, oldest ones carrying l1_compressed. The
	// selection must move past them.
	flagged := pool()
	for i := 0; i < 4; i++ { // m00..m03 (the oldest four) are distilled
		flagged[i].Memory.Metadata = map[string]any{memory.RowCompressedKey: true}
	}
	eFlagged := New(enabledCfg(), Deps{
		Candidates: &stubCandidates{byTier: map[string][]memory.MemoryResult{"episodic": flagged}},
		Votes:      stubVotes{},
	})
	actFlagged, err := eFlagged.planCompress(context.Background(),
		TierState{Tier: "episodic", Tokens: 95, Items: 10, Ceiling: 100}, 6)
	if err != nil {
		t.Fatalf("planCompress(flagged): %v", err)
	}
	if actFlagged == nil {
		t.Fatalf("planCompress(flagged) returned no action for a pool with 4 uncompressed rows")
	}
	same := len(actPlain.TargetIDs) == len(actFlagged.TargetIDs)
	if same {
		same = true
		for i := range actPlain.TargetIDs {
			if actPlain.TargetIDs[i] != actFlagged.TargetIDs[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatalf("flagged pool selected identical rows %v as plain pool %v — the l1_compressed flag changed nothing (T-6 defect shape)",
			actFlagged.TargetIDs, actPlain.TargetIDs)
	}
	for _, id := range actFlagged.TargetIDs {
		if id == "m00" || id == "m01" || id == "m02" || id == "m03" {
			t.Errorf("flagged selection re-picked compressed row %s", id)
		}
	}
}

// TestL15_HardBandEvictReachesCompressedRows pins the reclamation
// consequence: with the compressed rows out of the compress selection,
// the hard band's evict action's exclude set no longer contains them, so
// the eviction pass can reclaim them (reducing the over-band amount).
// This is the "distilled rows become evictable on a LATER pass" contract
// the hard band always documented but no selector implemented.
func TestL15_HardBandEvictReachesCompressedRows(t *testing.T) {
	var cands []memory.MemoryResult
	// Oldest rows are already distilled; the rest are not.
	for i := 0; i < 12; i++ {
		m := mem(fmt.Sprintf("m%02d", i), "tiny", 30-i)
		if i < 5 {
			m.Memory.Metadata = map[string]any{memory.RowCompressedKey: true}
		}
		cands = append(cands, m)
	}
	e := New(enabledCfg(), Deps{
		Candidates: &stubCandidates{byTier: map[string][]memory.MemoryResult{"episodic": cands}},
		Votes:      stubVotes{},
	})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "episodic", Tokens: 95, Items: 12, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	var compressSet, evictSet map[string]bool
	for _, a := range actions {
		switch a.Kind {
		case KindCompress:
			compressSet = map[string]bool{}
			for _, id := range a.TargetIDs {
				compressSet[id] = true
				if id == "m00" || id == "m01" || id == "m02" || id == "m03" || id == "m04" {
					t.Errorf("compress re-targeted already-compressed row %s", id)
				}
			}
		case KindEvict:
			evictSet = map[string]bool{}
			for _, id := range a.TargetIDs {
				evictSet[id] = true
			}
		}
	}
	if compressSet == nil {
		t.Fatalf("hard band produced no compress action")
	}
	for id := range compressSet {
		if evictSet[id] {
			t.Errorf("same-pass overlap: %s in both compress and evict (H7/A3-1 violation)", id)
		}
	}
	if evictSet == nil {
		t.Fatalf("hard band produced no evict action; compressed rows are never reclaimed (T-6 defect)")
	}
	// The evict pool must be able to include the compressed rows: assert
	// at least one evicted id is one of the distilled rows (the oldest of
	// the pool), proving reclamation of the previously-shielded set.
	reached := false
	for _, id := range []string{"m00", "m01", "m02", "m03", "m04"} {
		if evictSet[id] {
			reached = true
		}
	}
	if !reached {
		t.Errorf("evict action %v never reaches any compressed row m00..m04 — reclamation still blocked (T-6 defect)", evictKeys(evictSet))
	}
}

func evictKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}
