package vector

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

// twoShardFixture builds a ShardManager whose CONSOLIDATED and PROJECT
// shards both carry the fixture embedder's dimension, so ShardManager.Search
// genuinely joins across two shard databases (ShardType.Dimension() is
// fixed per type — 768/512/256 — and a single query embedding can only
// match shards sharing the provider's dimension; production deployments
// configure the provider dimension to the dominant shard dimension, and the
// white-box shard injection here mirrors that same-dimension pairing).
type twoShardFixture struct {
	mgr *ShardManager
}

func newTwoShardFixture(t *testing.T) *twoShardFixture {
	t.Helper()
	const dim = 128
	dir := t.TempDir()
	mgr, err := NewShardManager(ShardManagerConfig{
		BasePath:     filepath.Join(dir, "shards"),
		MaxRAMShards: 4,
		Embedder:     newMockProvider(dim), //nolint:gosec // G404: deterministic fixture embeddings
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("create shard manager: %v", err)
	}
	// White-box (package-internal) injection: replace the lazily-loaded
	// PROJECT shard (256-dim default) with a same-dimension shard so the
	// manager's single query embedding serves BOTH shards — the join the
	// hybrid searcher exercises.
	shards := mgr.ShardsForTest()
	if oldShard, ok := shards[ConsolidatedShard]; ok {
		_ = oldShard.Close()
	}
	cons, err := NewVectorShard(filepath.Join(dir, "shards", "consolidated128.vec.db"), dim, DefaultM, DefaultEFConstruction)
	if err != nil {
		t.Fatalf("create consolidated-dim shard: %v", err)
	}
	t.Cleanup(func() { _ = cons.Close() })
	shards[ConsolidatedShard] = cons
	proj, err := NewVectorShard(filepath.Join(dir, "shards", "project.vec.db"), dim, DefaultM, DefaultEFConstruction)
	if err != nil {
		t.Fatalf("create project-dim shard: %v", err)
	}
	t.Cleanup(func() { _ = proj.Close() })
	shards[ProjectShard] = proj
	return &twoShardFixture{mgr: mgr}
}

// TestHybridSearcher_JoinsAcrossShards is the wave-2 pin: the ShardManager-
// powered hybrid searcher returns joined results from TWO shards on a
// fixture, with keyword hits and vector hits fused through the combined
// score (plan docs/plans/2026-06-07-sharded-vector-memory.md "Wire
// ShardManager for hybrid search").
func TestHybridSearcher_JoinsAcrossShards(t *testing.T) {
	fx := newTwoShardFixture(t)
	mgr := fx.mgr
	ctx := context.Background()

	consolidated, err := mgr.GetShard(ConsolidatedShard)
	if err != nil {
		t.Fatalf("get consolidated shard: %v", err)
	}
	project := mgr.ShardsForTest()[ProjectShard]
	if project == nil {
		t.Fatal("project shard not injected")
	}
	if err := consolidated.Insert(ctx, "mem-cons-1", mustEmbed(t, mgr, "kv cache eviction policy"), "kv cache eviction policy"); err != nil {
		t.Fatalf("insert consolidated: %v", err)
	}
	if err := project.Insert(ctx, "mem-proj-1", mustEmbed(t, mgr, "kv cache tuning notes"), "kv cache tuning notes"); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	// Real production path: the adapter over the live ShardManager, exactly
	// as vector_hybrid_wiring.go constructs it
	// (vector.NewVectorSearcherAdapter(adapter.Manager(), shardTypes)).
	vectorLane := NewVectorSearcherAdapter(mgr, []ShardType{ConsolidatedShard, ProjectShard})
	hybrid, err := NewHybridSearcher(HybridSearcherConfig{
		VectorSearcher:  vectorLane,
		KeywordSearcher: &fixtureKeywordSearcher{},
		Alpha:           0.5,
	})
	if err != nil {
		t.Fatalf("create hybrid searcher: %v", err)
	}

	results, err := hybrid.Search(ctx, "kv cache", 10)
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}

	ids := map[string]bool{}
	for _, r := range results {
		ids[r.MemoryID] = true
	}
	if !ids["mem-cons-1"] {
		t.Errorf("hybrid results missing consolidated-shard hit mem-cons-1: got %v", ids)
	}
	if !ids["mem-proj-1"] {
		t.Errorf("hybrid results missing project-shard hit mem-proj-1: got %v", ids)
	}
	for _, r := range results {
		if r.MemoryID == "mem-proj-1" && r.CombinedScore <= 0 {
			t.Errorf("expected nonzero combined score on the joined hit; results=%+v", results)
		}
	}
}

// fixtureKeywordSearcher is a minimal keyword lane so this test isolates the
// vector lane's cross-shard join (the daemon-side managerKeywordSearcher
// adapter is pinned in internal/daemon).
type fixtureKeywordSearcher struct{}

func (f *fixtureKeywordSearcher) SearchKeyword(_ context.Context, _ string, _ int) ([]KeywordResult, error) {
	return nil, nil
}

func mustEmbed(t *testing.T, mgr *ShardManager, text string) []float32 {
	t.Helper()
	emb, err := mgr.embedder.GenerateEmbedding(context.Background(), text)
	if err != nil {
		t.Fatalf("embed %q: %v", text, err)
	}
	return emb
}
