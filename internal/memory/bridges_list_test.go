package memory

import (
	"context"
	"testing"
)

// TestListBridgesExportedContract: the CLI/visibility surface (branch 09 B06)
// needs to enumerate persisted bridges with their verified flag + evidence
// counts. The store must expose the full-bridge listing on its EXPORTED
// method set (ListBridges); listAllBridges stays the internal verify-path
// alias.
func TestListBridgesExportedContract(t *testing.T) {
	ctx := context.Background()
	bs, err := NewBridgeStore(t.TempDir() + "/bridge.vec.db")
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	t.Cleanup(func() { _ = bs.Close() })

	// Empty store: empty list, no error.
	bridges, err := bs.ListBridges(ctx)
	if err != nil {
		t.Fatalf("ListBridges (empty): %v", err)
	}
	if len(bridges) != 0 {
		t.Fatalf("expected 0 bridges in an empty store, got %d", len(bridges))
	}

	// Seed two bridges with different evidence/verified shapes.
	seed := []BridgeRecord{
		{
			ID:          "bridge-a",
			Content:     "projectx is led by alice",
			Embedding:   hashBridgeEmbedding("projectx is led by alice"),
			EvidenceIDs: []string{"mem-1", "mem-2"},
			Verified:    true,
		},
		{
			ID:          "bridge-b",
			Content:     "projectx uses postgres 16",
			Embedding:   hashBridgeEmbedding("projectx uses postgres 16"),
			EvidenceIDs: []string{"mem-1"},
			Verified:    false,
		},
	}
	for _, rec := range seed {
		if err := bs.PutBridge(ctx, rec); err != nil {
			t.Fatalf("PutBridge %s: %v", rec.ID, err)
		}
	}

	bridges, err = bs.ListBridges(ctx)
	if err != nil {
		t.Fatalf("ListBridges: %v", err)
	}
	if len(bridges) != 2 {
		t.Fatalf("expected 2 bridges, got %d", len(bridges))
	}

	byID := make(map[string]BridgeRecord, len(bridges))
	for _, b := range bridges {
		byID[b.ID] = b
	}
	a, ok := byID["bridge-a"]
	if !ok {
		t.Fatal("bridge-a missing from ListBridges output")
	}
	if !a.Verified || len(a.EvidenceIDs) != 2 {
		t.Errorf("bridge-a typed fields wrong: verified=%t evidence=%v", a.Verified, a.EvidenceIDs)
	}
	if a.Content != "projectx is led by alice" {
		t.Errorf("bridge-a content mismatch: %q", a.Content)
	}
	b, ok := byID["bridge-b"]
	if !ok {
		t.Fatal("bridge-b missing from ListBridges output")
	}
	if b.Verified {
		t.Error("bridge-b should be unverified (false)")
	}

	// Invalidation interplay: after deleting a bridge, ListBridges reflects it.
	if err := bs.DeleteBridge(ctx, "bridge-a"); err != nil {
		t.Fatalf("DeleteBridge: %v", err)
	}
	bridges, err = bs.ListBridges(ctx)
	if err != nil {
		t.Fatalf("ListBridges after delete: %v", err)
	}
	if len(bridges) != 1 || bridges[0].ID != "bridge-b" {
		t.Fatalf("expected only bridge-b after delete, got %+v", bridges)
	}
}
