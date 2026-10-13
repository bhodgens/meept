package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory/vector"
	"github.com/caimlas/meept/pkg/models"
)

// testBridgeEmbedding returns a simple deterministic embedding of the given
// dimension with a unit value on the first axis plus per-index offsets, so
// near-identical variants stay top hits in a flat cosine scan.
func testBridgeEmbedding(dim int, salt float32) []float32 {
	v := make([]float32, dim)
	v[0] = 1
	for i := 1; i < dim; i++ {
		v[i] = salt * float32(i) / float32(dim)
	}
	return v
}

// TestBridgeShardRoundTrip: Put a bridge (text, embedding, metadata) ->
// search by near-identical embedding -> hit returns all four metadata keys
// intact (unit_type, evidence_ids, created_at, verified).
func TestBridgeShardRoundTrip(t *testing.T) {
	ctx := context.Background()
	bs, err := NewBridgeStore(filepath.Join(t.TempDir(), "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	defer bs.Close()

	createdAt := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	rec := BridgeRecord{
		ID:          "bridge-1",
		Content:     "alice prefers rust; bob's team ships rust services",
		Embedding:   testBridgeEmbedding(vector.BridgeShard.Dimension(), 0.5),
		EvidenceIDs: []string{"mem-aaa", "mem-bbb"},
		CreatedAt:   createdAt,
		Verified:    true,
	}
	if err := bs.PutBridge(ctx, rec); err != nil {
		t.Fatalf("PutBridge: %v", err)
	}

	near := testBridgeEmbedding(vector.BridgeShard.Dimension(), 0.5)
	near[len(near)-1] += 0.01 // near-identical, not identical
	hits, err := bs.SearchByEmbedding(ctx, near, 5)
	if err != nil {
		t.Fatalf("SearchByEmbedding: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if hits[0].ID != "bridge-1" {
		t.Fatalf("top hit ID = %q, want %q", hits[0].ID, "bridge-1")
	}

	md := hits[0].Metadata
	if md == nil {
		t.Fatal("hit metadata is nil")
	}
	if got := md[BridgeMetaUnitType]; got != BridgeUnitTypeValue {
		t.Errorf("metadata %s = %v, want %q", BridgeMetaUnitType, got, BridgeUnitTypeValue)
	}
	evRaw, ok := md[BridgeMetaEvidenceIDs]
	if !ok {
		t.Fatalf("metadata missing %s", BridgeMetaEvidenceIDs)
	}
	var ev []string
	switch v := evRaw.(type) {
	case []string:
		ev = v
	case []any:
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				t.Fatalf("evidence entry %T is not a string", e)
			}
			ev = append(ev, s)
		}
	default:
		t.Fatalf("metadata %s has unexpected type %T", BridgeMetaEvidenceIDs, evRaw)
	}
	if len(ev) != 2 || ev[0] != "mem-aaa" || ev[1] != "mem-bbb" {
		t.Errorf("evidence_ids = %v, want [mem-aaa mem-bbb]", ev)
	}
	createdRaw, ok := md[BridgeMetaCreatedAt]
	if !ok {
		t.Fatalf("metadata missing %s", BridgeMetaCreatedAt)
	}
	createdStr, _ := createdRaw.(string)
	if _, err := time.Parse(time.RFC3339, createdStr); err != nil {
		t.Errorf("metadata %s = %q is not RFC3339: %v", BridgeMetaCreatedAt, createdStr, err)
	}
	verifiedRaw, ok := md[BridgeMetaVerified]
	if !ok {
		t.Fatalf("metadata missing %s", BridgeMetaVerified)
	}
	if got := verifiedRaw; got != "true" && got != "false" {
		t.Errorf("metadata %s = %v, want \"true\" or \"false\"", BridgeMetaVerified, got)
	}
}

// TestBridgeWriteRequiresEvidence: empty evidence_ids -> error, nothing
// persisted (branch rule: no provenance, no ship).
func TestBridgeWriteRequiresEvidence(t *testing.T) {
	ctx := context.Background()
	bs, err := NewBridgeStore(filepath.Join(t.TempDir(), "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	defer bs.Close()

	err = bs.PutBridge(ctx, BridgeRecord{
		ID:          "bridge-no-evidence",
		Content:     "orphan bridge",
		Embedding:   testBridgeEmbedding(8, 0.1),
		EvidenceIDs: nil,
	})
	if err == nil {
		t.Fatal("expected error for empty evidence_ids, got nil")
	}

	n, err := bs.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("persisted bridge count = %d, want 0 (rejected write must not persist)", n)
	}
}

// TestBridgeShardExcludedFromGossip: ShardTypeBridge must never enter
// dual_store replication. Verified at both exclusion surfaces:
//  1. the shard-type level (GossipEligible),
//  2. the DualStore gossip publish path (unit_type=bridge is skipped).
func TestBridgeShardExcludedFromGossip(t *testing.T) {
	if vector.BridgeShard.GossipEligible() {
		t.Fatal("BridgeShard.GossipEligible() = true, want false")
	}
	for _, st := range []vector.ShardType{
		vector.ConsolidatedShard, vector.RecentShard, vector.ProjectShard,
		vector.CodeShard, vector.ArchiveShard,
	} {
		if !st.GossipEligible() {
			t.Errorf("%s.GossipEligible() = false, want true", st)
		}
	}

	dir := t.TempDir()
	ds, err := NewDualStore(dir, "test-node", testBridgeSlog())
	if err != nil {
		t.Fatalf("NewDualStore: %v", err)
	}
	defer ds.Close()

	pub := &mockGossipPublisher{}
	ds.SetGossipPublisher(pub)

	bridgeMem := &Memory{
		ID:        "mem-bridge-gossip",
		Type:      MemoryTypeEpisodic,
		Category:  "bridge_test",
		Content:   "derived bridging fact, node-local only",
		CreatedAt: time.Now().UTC(),
		Metadata: map[string]any{
			BridgeMetaUnitType: BridgeUnitTypeValue,
		},
	}
	if err := ds.StoreMemory(context.Background(), bridgeMem); err != nil {
		t.Fatalf("StoreMemory (bridge): %v", err)
	}

	// Prove the mock is live: a non-bridge memory publishes.
	normalMem := &Memory{
		ID:        "mem-normal-gossip",
		Type:      MemoryTypeEpisodic,
		Category:  "bridge_test",
		Content:   "ordinary memory, does replicate",
		CreatedAt: time.Now().UTC(),
	}
	if err := ds.StoreMemory(context.Background(), normalMem); err != nil {
		t.Fatalf("StoreMemory (normal): %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var sawNormal bool
	for time.Now().Before(deadline) {
		for _, ev := range pub.getEvents() {
			if id := gossipPayloadID(ev.payload); id == "mem-normal-gossip" {
				sawNormal = true
			}
		}
		if sawNormal {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sawNormal {
		t.Fatal("control failed: ordinary memory was not gossiped; mock publisher not exercised")
	}
	for _, ev := range pub.getEvents() {
		if id := gossipPayloadID(ev.payload); id == "mem-bridge-gossip" {
			t.Fatalf("bridge memory entered gossip replication (event %s, id %s)", ev.eventType, id)
		}
	}
}

// gossipPayloadID extracts the memory ID from a gossip payload.
func gossipPayloadID(payload any) string {
	if p, ok := payload.(models.MemoryStoredPayload); ok {
		return p.ID
	}
	return ""
}
