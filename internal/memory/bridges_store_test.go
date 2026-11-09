package memory

import (
	"context"
	"fmt"
	"log/slog"
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

// fixedEmbedder returns one embedding for every text. Used to search the
// bridge shard in its own vector space (ShardManager.Search embeds the query).
type fixedEmbedder struct {
	v []float32
}

func (f fixedEmbedder) GenerateEmbedding(context.Context, string) ([]float32, error) {
	return append([]float32(nil), f.v...), nil
}

func (f fixedEmbedder) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		emb, err := f.GenerateEmbedding(ctx, texts[i])
		if err != nil {
			return nil, err
		}
		out[i] = emb
	}
	return out, nil
}

func (f fixedEmbedder) Dimension() int { return len(f.v) }

// TestGenericShardSearchReturnsBridgeUnitType pins issue 62: a bridge written
// by PutBridge and read back through VectorShard.Search (not the bridge_meta
// side path) must carry unit_type=bridge, and SelectBalanced must count it as
// a bridge so the k_b cap applies. Untyped inserts stay source units.
func TestGenericShardSearchReturnsBridgeUnitType(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	bs, err := NewBridgeStore(filepath.Join(dir, "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}

	const n = 5
	emb := testBridgeEmbedding(vector.BridgeShard.Dimension(), 0.5)
	for i := range n {
		row := append([]float32(nil), emb...)
		row[1] = float32(i) * 0.001
		if err := bs.PutBridge(ctx, BridgeRecord{
			ID:          fmt.Sprintf("cap-bridge-%d", i),
			Content:     fmt.Sprintf("bridging fact %d", i),
			Embedding:   row,
			EvidenceIDs: []string{"mem-src"},
		}); err != nil {
			t.Fatalf("PutBridge %d: %v", i, err)
		}
	}

	hits, err := bs.shard.Search(ctx, emb, n, vector.DefaultEFSearch)
	if err != nil {
		t.Fatalf("generic shard search: %v", err)
	}
	if len(hits) != n {
		t.Fatalf("generic shard search hits = %d, want %d", len(hits), n)
	}
	for _, h := range hits {
		if h.Metadata == nil {
			t.Fatalf("hit %s metadata is nil; generic search must return unit_type", h.MemoryID)
		}
		if got := h.Metadata[vector.BridgeMetadataKey]; got != vector.BridgeUnitTypeValue {
			t.Fatalf("hit %s unit_type = %v, want %q", h.MemoryID, got, vector.BridgeUnitTypeValue)
		}
	}

	filtered, stats := vector.SelectBalanced(hits, 2)
	if stats.BridgesIncluded != 2 || stats.BridgesExcluded != n-2 || stats.Sources != 0 {
		t.Fatalf("SelectBalanced stats = %+v, want included 2 excluded %d sources 0", stats, n-2)
	}
	if len(filtered) != 2 {
		t.Fatalf("capped len = %d, want 2", len(filtered))
	}

	// Recall path: ShardManagerVectorSearcher.Search applies the same cap to
	// generic results. Close the writer first so the manager opens the file
	// the way boot does (a second connection, not the BridgeStore handle).
	if err := bs.Close(); err != nil {
		t.Fatalf("close bridge store: %v", err)
	}

	mgr, err := vector.NewShardManager(vector.ShardManagerConfig{
		BasePath:     dir,
		Embedder:     fixedEmbedder{v: emb},
		MaxRAMShards: 5,
		Logger:       slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewShardManager: %v", err)
	}
	t.Cleanup(func() {
		for _, st := range []vector.ShardType{vector.ConsolidatedShard, vector.RecentShard, vector.BridgeShard} {
			if sh, err := mgr.GetShard(st); err == nil && sh != nil {
				_ = sh.Close()
			}
		}
	})

	searcher := NewShardManagerVectorSearcher(mgr, []vector.ShardType{vector.BridgeShard}, WithBridgeCap(2))
	got, err := searcher.Search(ctx, "bridging fact", 10)
	if err != nil {
		t.Fatalf("ShardManagerVectorSearcher.Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("searcher hits = %d, want 2 (k_b cap); untyped hits would return all %d", len(got), n)
	}
	for _, h := range got {
		if h.Metadata == nil {
			t.Fatalf("searcher hit %s metadata is nil", h.MemoryID)
		}
		if ut := h.Metadata[vector.BridgeMetadataKey]; ut != vector.BridgeUnitTypeValue {
			t.Fatalf("searcher hit %s unit_type = %v, want %q", h.MemoryID, ut, vector.BridgeUnitTypeValue)
		}
	}

	// A plain insert on a non-bridge shard must not pick up the tag.
	src, err := vector.NewVectorShard(filepath.Join(dir, "source.vec.db"), 8, vector.DefaultM, vector.DefaultEFConstruction)
	if err != nil {
		t.Fatalf("source shard: %v", err)
	}
	defer src.Close()
	srcEmb := testBridgeEmbedding(8, 0.2)
	if err := src.Insert(ctx, "source-1", srcEmb, "a source memory"); err != nil {
		t.Fatalf("source insert: %v", err)
	}
	srcHits, err := src.Search(ctx, srcEmb, 5, vector.DefaultEFSearch)
	if err != nil {
		t.Fatalf("source search: %v", err)
	}
	if len(srcHits) != 1 {
		t.Fatalf("source hits = %d, want 1", len(srcHits))
	}
	if srcHits[0].Metadata != nil {
		t.Fatalf("source hit metadata = %v, want nil", srcHits[0].Metadata)
	}
	_, srcStats := vector.SelectBalanced(srcHits, 0)
	if srcStats.Sources != 1 || srcStats.BridgesIncluded != 0 {
		t.Fatalf("source stats = %+v, want sources 1 included 0", srcStats)
	}
}

// TestGenericShardSearchBackfillsLegacyBridgeTag: a bridge written before
// metadata.unit_type existed still has the tag in bridge_meta. Reopening the
// store must stamp the shard row so generic Search returns unit_type=bridge.
func TestGenericShardSearchBackfillsLegacyBridgeTag(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bridge.vec.db")
	bs, err := NewBridgeStore(path)
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	emb := testBridgeEmbedding(vector.BridgeShard.Dimension(), 0.3)
	if err := bs.PutBridge(ctx, BridgeRecord{
		ID:          "legacy-bridge",
		Content:     "legacy bridging fact",
		Embedding:   emb,
		EvidenceIDs: []string{"mem-src"},
	}); err != nil {
		t.Fatalf("PutBridge: %v", err)
	}
	if _, err := bs.db.ExecContext(ctx, `UPDATE metadata SET unit_type = NULL WHERE memory_id = ?`, "legacy-bridge"); err != nil {
		t.Fatalf("clear unit_type: %v", err)
	}
	if err := bs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := NewBridgeStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	hits, err := reopened.shard.Search(ctx, emb, 5, vector.DefaultEFSearch)
	if err != nil {
		t.Fatalf("generic shard search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].Metadata == nil || hits[0].Metadata[vector.BridgeMetadataKey] != vector.BridgeUnitTypeValue {
		t.Fatalf("legacy hit metadata = %v, want unit_type=bridge", hits[0].Metadata)
	}
	_, stats := vector.SelectBalanced(hits, 0)
	if stats.BridgesIncluded != 1 || stats.Sources != 0 {
		t.Fatalf("stats = %+v, want included 1 sources 0", stats)
	}
}
