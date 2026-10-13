package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// seedMemoryStore returns an initialized SQLite-backed Manager plus a wired
// BridgeStore — the invalidation fixtures all need both sides of the
// provenance edge.
func seedMemoryStore(t *testing.T) (*Manager, *BridgeStore) {
	t.Helper()
	tmpDir := t.TempDir()
	mgr := NewManager(ManagerConfig{
		Config: config.MemoryConfig{
			Backend: config.MemoryBackendSQLite,
			DataDir: tmpDir,
			Episodic: config.EpisodicConfig{
				Enabled: true,
			},
			Task: config.TaskMemoryConfig{
				Enabled: true,
				Domains: []string{DomainGeneral, DomainCode},
			},
			Personality: config.PersonalityConfig{
				Enabled: false,
			},
		},
		Logger: testBridgeSlog(),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	bs, err := NewBridgeStore(filepath.Join(tmpDir, "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	t.Cleanup(func() { _ = bs.Close() })
	mgr.SetBridgeStore(bs)
	return mgr, bs
}

// seedEvidenceBridge stores one episodic memory and one bridge citing it.
func seedEvidenceBridge(t *testing.T, mgr *Manager, bs *BridgeStore, bridgeID string) string {
	t.Helper()
	ctx := context.Background()
	memID, err := mgr.Store(ctx, Memory{
		Content:  "alice leads the projectx migration",
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
	})
	if err != nil {
		t.Fatalf("Store source memory: %v", err)
	}
	rec := BridgeRecord{
		ID:          bridgeID,
		Content:     "projectx is led by alice",
		Embedding:   hashBridgeEmbedding("projectx is led by alice"),
		EvidenceIDs: []string{memID},
		CreatedAt:   time.Now().UTC(),
		Verified:    true,
	}
	if err := bs.PutBridge(ctx, rec); err != nil {
		t.Fatalf("PutBridge: %v", err)
	}
	return memID
}

// requireBridgeGone asserts the bridge is fully absent (search + count).
func requireBridgeGone(t *testing.T, bs *BridgeStore, bridgeID string) {
	t.Helper()
	ctx := context.Background()
	if n, err := bs.Count(ctx); err != nil {
		t.Fatalf("Count: %v", err)
	} else if n != 0 {
		t.Errorf("bridge %s still counted (n=%d), want 0", bridgeID, n)
	}
	hits, err := bs.SearchByEmbedding(ctx, hashBridgeEmbedding("projectx is led by alice"), 10)
	if err != nil {
		t.Fatalf("SearchByEmbedding: %v", err)
	}
	for _, h := range hits {
		if h.ID == bridgeID {
			t.Errorf("bridge %s still retrievable after invalidation", bridgeID)
		}
	}
}

// TestSourceMemoryDeleteInvalidatesBridge: deleting a source memory via
// Manager.Delete removes every bridge citing it as evidence.
func TestSourceMemoryDeleteInvalidatesBridge(t *testing.T) {
	mgr, bs := seedMemoryStore(t)
	memID := seedEvidenceBridge(t, mgr, bs, "bridge-del")
	ctx := context.Background()

	if err := mgr.Delete(ctx, memID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	requireBridgeGone(t, bs, "bridge-del")
}

// TestSourceMemoryUpdateInvalidatesBridge: StoreVersioned with CreateVersion
// (the repo's update-by-new-version path) supersedes the old source memory
// (is_current=0) — bridges citing the superseded version are deleted, since
// their provenance is no longer a current fact.
func TestSourceMemoryUpdateInvalidatesBridge(t *testing.T) {
	mgr, bs := seedMemoryStore(t)
	memID := seedEvidenceBridge(t, mgr, bs, "bridge-upd")
	ctx := context.Background()

	// Update = new version (StoreVersioned marks the old row non-current).
	updated := Memory{
		ID:       memID,
		Content:  "alice leads the projecty migration now",
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
	}
	if _, err := mgr.StoreVersioned(ctx, updated, StoreOptions{CreateVersion: true, ParentID: memID}); err != nil {
		t.Fatalf("StoreVersioned: %v", err)
	}
	requireBridgeGone(t, bs, "bridge-upd")
}

// TestSourceMemorySupersedeInvalidatesBridge: MarkSuperseded (the epistemic
// supersede path) also flips is_current=0 and must cascade.
func TestSourceMemorySupersedeInvalidatesBridge(t *testing.T) {
	mgr, bs := seedMemoryStore(t)
	memID := seedEvidenceBridge(t, mgr, bs, "bridge-sup")
	ctx := context.Background()

	newID, err := mgr.Store(ctx, Memory{
		Content:  "bob leads the projectx migration",
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
	})
	if err != nil {
		t.Fatalf("Store successor: %v", err)
	}
	if _, _, err := mgr.MarkSuperseded(ctx, memID, newID); err != nil {
		t.Fatalf("MarkSuperseded: %v", err)
	}
	requireBridgeGone(t, bs, "bridge-sup")
}

// TestSourceMemoryExpiryInvalidatesBridge: access-based expiration deletes
// expired source memories via the consolidation backend; the expired bridge
// evidence cascade must fire on that path too.
func TestSourceMemoryExpiryInvalidatesBridge(t *testing.T) {
	mgr, bs := seedMemoryStore(t)
	memID := seedEvidenceBridge(t, mgr, bs, "bridge-exp")
	ctx := context.Background()

	// Age the memory past the expiration cutoff by rewriting its
	// last_accessed_at directly in the episodic store's DB.
	oldISO := time.Now().Add(-120 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := mgr.Episodic().sharedStore().GetDB().ExecContext(ctx,
		`UPDATE episodic_memories SET last_accessed_at = ? WHERE id = ?`, oldISO, memID); err != nil {
		t.Fatalf("age memory: %v", err)
	}

	backend := NewSQLiteConsolidationBackend(mgr.Episodic(), mgr.Task(), mgr)
	expired, err := backend.GetExpiredMemories(ctx, 90)
	if err != nil {
		t.Fatalf("GetExpiredMemories: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != memID {
		t.Fatalf("expired = %v, want [%s]", expired, memID)
	}
	if err := backend.DeleteSingle(ctx, memID); err != nil {
		t.Fatalf("DeleteSingle: %v", err)
	}
	requireBridgeGone(t, bs, "bridge-exp")
}

// TestInvalidateBridgesForMemoryIdempotent: running the helper twice is safe
// (ListBridgesByEvidence returns nothing on the second pass).
func TestInvalidateBridgesForMemoryIdempotent(t *testing.T) {
	mgr, bs := seedMemoryStore(t)
	memID := seedEvidenceBridge(t, mgr, bs, "bridge-idem")
	ctx := context.Background()

	if err := invalidateBridgesForMemory(ctx, bs, memID); err != nil {
		t.Fatalf("first invalidate: %v", err)
	}
	requireBridgeGone(t, bs, "bridge-idem")
	if err := invalidateBridgesForMemory(ctx, bs, memID); err != nil {
		t.Fatalf("second invalidate (idempotent pass): %v", err)
	}
}

// TestInvalidateBridgesForMemoryNoopGuards: nil store and empty ID are no-ops,
// not errors (every mutation seam calls the helper unconditionally).
func TestInvalidateBridgesForMemoryNoopGuards(t *testing.T) {
	ctx := context.Background()
	if err := invalidateBridgesForMemory(ctx, nil, "mem-1"); err != nil {
		t.Errorf("nil store: %v", err)
	}
	mgr, bs := seedMemoryStore(t)
	_ = mgr
	if err := invalidateBridgesForMemory(ctx, bs, ""); err != nil {
		t.Errorf("empty id: %v", err)
	}
}
