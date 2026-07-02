package resources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Evict removes eligible blobs from the CAS store to reclaim space.
// Eligibility: refcount == 0 AND not pinned.
//
// Per spec section 2.4, the CAS is a transit cache: "Files leave when no
// active task references them." Therefore Evict reclaims ALL eligible
// entries — there is no reason to keep unreferenced blobs in a transit
// cache. The eviction ORDER (lowest-refcount first, oldest added_at
// breaking ties) matters for the periodic sweep's telemetry and for
// cap-pressure scenarios where context cancellation may interrupt
// mid-sweep, but the goal is always to evict everything eligible.
//
// Pinned entries are never evicted.
//
// Returns the number of entries evicted.
//
// (spec sections 2.4 and 7)
func (s *CASStore) Evict(ctx context.Context) (int, error) {
	// Snapshot all entries under RLock. Sort and evict outside lock.
	allEntries := s.index.All()

	// Filter to eligible (refcount==0 AND not pinned).
	var eligible []*metaRecord
	for _, rec := range allEntries {
		if rec.Refcount == 0 && !rec.Pinned && !s.pinnedSet[rec.Hash] {
			eligible = append(eligible, rec)
		}
	}

	if len(eligible) == 0 {
		return 0, nil
	}

	// Sort by refcount ascending, then by added_at ascending (oldest first).
	// All eligible entries have refcount=0, so the effective sort is by
	// added_at (oldest first = LRU).
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].Refcount != eligible[j].Refcount {
			return eligible[i].Refcount < eligible[j].Refcount
		}
		return eligible[i].AddedAt.Before(eligible[j].AddedAt)
	})

	var evicted int
	var reclaimedBytes int64

	for _, rec := range eligible {
		// Check context cancellation.
		if ctx.Err() != nil {
			break
		}

		// Evict this entry: remove data file, meta.json, and index record.
		if err := s.evictEntry(rec.Hash); err != nil {
			s.logger.Warn("resources: eviction failed for entry", "hash", rec.Hash, "err", err)
			continue
		}

		evicted++
		reclaimedBytes += rec.Size
	}

	if evicted > 0 {
		s.metrics.IncCASBytesEvicted(reclaimedBytes)
		s.logger.Debug("resources: eviction complete",
			"evicted", evicted,
			"reclaimed_bytes", reclaimedBytes)
	}

	return evicted, nil
}

// evictEntry removes a single CAS entry from disk and the index. It is safe
// to call on already-removed entries (returns nil).
func (s *CASStore) evictEntry(hashHex string) error {
	entryDir := HashEntryPath(s.cfg.StoreDir, hashHex)

	// Remove data and meta files, then the directory.
	// Use RemoveAll on the entry dir to clean up any stray temp files.
	if err := os.RemoveAll(entryDir); err != nil {
		// If the directory doesn't exist, that's fine.
		if !os.IsNotExist(err) {
			return fmt.Errorf("resources: remove entry dir: %w", err)
		}
	}

	// Remove from parent dirs if they're now empty (best-effort, ignore errors).
	parentDir := filepath.Dir(entryDir)
	os.Remove(parentDir) // only succeeds if empty
	grandparentDir := filepath.Dir(parentDir)
	os.Remove(grandparentDir)

	// Remove from index.
	if err := s.index.Delete(hashHex); err != nil {
		return fmt.Errorf("resources: delete index entry: %w", err)
	}

	return nil
}

// EligibleCount returns the number of entries that are currently eligible
// for eviction (refcount==0 AND not pinned). This is exposed for telemetry
// and operator inspection.
func (s *CASStore) EligibleCount() int {
	all := s.index.All()
	var n int
	for _, rec := range all {
		if rec.Refcount == 0 && !rec.Pinned && !s.pinnedSet[rec.Hash] {
			n++
		}
	}
	return n
}
