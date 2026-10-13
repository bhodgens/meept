package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// countingBridgeGen records GenerateBridges' view of the world via calls.
type countingBridgeGen struct{ calls int }

func (g *countingBridgeGen) GenerateBridgeFacts(_ context.Context, _ string, _ []Memory) (string, error) {
	g.calls++
	return `[]`, nil
}

// TestConsolidatorBridgePassDisabledByDefault: without SetBridgeComponents
// the consolidation Run must never attempt bridge generation — even with a
// manager present. (The config gate itself is proven by
// TestGenerateBridgesDisabledNoOps; this pins the consolidator wiring.)
func TestConsolidatorBridgePassDisabledByDefault(t *testing.T) {
	gen := &countingBridgeGen{}
	c := NewConsolidator(ConsolidatorConfig{
		Logger: testBridgeSlog(),
	})
	// No SetBridgeComponents call: components nil -> pass dormant.
	c.runBridgeGenerationPass(context.Background())
	if gen.calls != 0 {
		t.Errorf("bridge gen called %d times with no components wired, want 0", gen.calls)
	}
}

// TestConsolidatorBridgePassRespectsConfigGate: with components wired but
// manager config disabled (zero-value manager config = Enabled false), the
// pass makes zero LLM calls.
func TestConsolidatorBridgePassRespectsConfigGate(t *testing.T) {
	gen := &countingBridgeGen{}
	bs, err := NewBridgeStore(filepath.Join(t.TempDir(), "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	defer bs.Close()

	c := NewConsolidator(ConsolidatorConfig{
		Logger: testBridgeSlog(),
	})
	c.SetBridgeComponents(bs, gen)
	// c.manager is nil here; the pass must treat nil manager as disabled
	// (fail-closed: no manager -> no config -> no generation).
	c.runBridgeGenerationPass(context.Background())
	if gen.calls != 0 {
		t.Errorf("bridge gen called %d times with disabled config, want 0", gen.calls)
	}
}

// TestSetBridgeComponentsRoundTrip pins the wiring accessors.
func TestSetBridgeComponentsRoundTrip(t *testing.T) {
	c := NewConsolidator(ConsolidatorConfig{Logger: testBridgeSlog()})
	gen := &countingBridgeGen{}
	bs, err := NewBridgeStore(filepath.Join(t.TempDir(), "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	defer bs.Close()
	c.SetBridgeComponents(bs, gen)
	c.mu.Lock()
	store, wired := c.bridgeStore, c.bridgeGen
	c.mu.Unlock()
	if store != bs || wired != BridgeGenerator(gen) {
		t.Error("SetBridgeComponents did not store both components")
	}
}

// TestRecentMemoriesForBridgesNilManager: nil manager yields no candidates
// and no error (fail-closed).
func TestRecentMemoriesForBridgesNilManager(t *testing.T) {
	c := NewConsolidator(ConsolidatorConfig{Logger: testBridgeSlog()})
	res, err := c.recentMemoriesForBridges(context.Background())
	if err != nil {
		t.Fatalf("recentMemoriesForBridges: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("got %d memories with nil manager, want 0", len(res))
	}
}

// TestBridgeEntityExtractionV1 pins the fallback extractor: entities come
// from metadata when present, from the Content regex otherwise, capped at
// MaxBridgeFactsPerSource.
func TestBridgeEntityExtractionV1(t *testing.T) {
	// Metadata path (comma-separated, normalized lowercase).
	m := bridgeTestMemory("m-1", "content", "Rust", "TOKYO")
	facts := extractBridgeFacts([]Memory{m})
	if got := facts["m-1"]; len(got) != 2 || got[0] != "rust" || got[1] != "tokyo" {
		t.Errorf("metadata extraction = %v, want [rust tokyo]", got)
	}

	// Fallback regex path: CamelCase + snake_case from content.
	m2 := Memory{ID: "m-2", Content: "AliceWrote parse_qc for the TokyoDB migration"}
	facts2 := extractBridgeFacts([]Memory{m2})
	got2 := facts2["m-2"]
	if len(got2) == 0 {
		t.Fatal("v1 extractor found no entities")
	}
	found := map[string]bool{}
	for _, f := range got2 {
		found[f] = true
	}
	if !found["alicewrote"] || !found["parse_qc"] {
		t.Errorf("v1 extractor missed known entities, got %v", got2)
	}

	// Cap: > MaxBridgeFactsPerSource entities trims to the cap.
	var many []string
	for i := 0; i < 20; i++ {
		many = append(many, string(rune('a'+i)))
	}
	m3 := bridgeTestMemory("m-3", "content", many...)
	facts3 := extractBridgeFacts([]Memory{m3})
	if len(facts3["m-3"]) != MaxBridgeFactsPerSource {
		t.Errorf("cap = %d, want %d", len(facts3["m-3"]), MaxBridgeFactsPerSource)
	}
}

// TestBridgeDFWindowBounds pins the window function directly.
func TestBridgeDFWindowBounds(t *testing.T) {
	perMemory := map[string][]string{
		"m-1": {"df1", "df2", "df10", "df11"},
		"m-2": {"df2", "df10", "df11"},
		"m-3": {"df10", "df11"},
	}
	// Extend to 10/11 documents for the boundary entities.
	for i := 3; i < 10; i++ {
		id := string(rune('a' + i))
		perMemory[id] = []string{"df10", "df11"}
	}
	perMemory["m-k"] = []string{"df11"}
	eligible := eligibleBridgeEntities(perMemory, 10)
	want := map[string]bool{"df2": true, "df10": true}
	if len(eligible) != len(want) {
		t.Fatalf("eligible = %v, want exactly df2+df10", eligible)
	}
	for _, e := range eligible {
		if !want[e] {
			t.Errorf("unexpected eligible entity %q", e)
		}
	}
}

// TestHashBridgeEmbeddingDimension: the v1 embedding is 256-dim (the bridge
// shard's dimension) so PutBridge accepts it.
func TestHashBridgeEmbeddingDimension(t *testing.T) {
	v := hashBridgeEmbedding("some bridging fact")
	if len(v) != 256 {
		t.Errorf("embedding dim = %d, want 256", len(v))
	}
}

// TestBridgeRecordTimestampSet: persisted bridges carry a non-zero CreatedAt
// (metadata round-trip through PutBridge).
func TestBridgeRecordTimestampSet(t *testing.T) {
	ctx := context.Background()
	bs := newBridgeTestStore(t)
	gen := &fakeBridgeFactGen{responses: []string{
		`[{"text":"timestamped fact","evidence_ids":["m-t1","m-t2"]}]`,
	}}
	memories := []Memory{
		bridgeTestMemory("m-t1", "t1 shares v", "v"),
		bridgeTestMemory("m-t2", "t2 shares v", "v"),
	}
	generated, _, err := GenerateBridges(ctx, bs, gen, BridgesConfig{Enabled: true, BridgeDFMax: 10}, memories)
	if err != nil || generated != 1 {
		t.Fatalf("generated=%d err=%v, want 1/nil", generated, err)
	}
	recs, err := bs.ListBridgesByEvidence(ctx, "m-t1")
	if err != nil || len(recs) != 1 {
		t.Fatalf("ListBridgesByEvidence: %d recs, err %v", len(recs), err)
	}
	if recs[0].CreatedAt.IsZero() {
		t.Error("bridge CreatedAt is zero; expected generation timestamp")
	}
	if !recs[0].CreatedAt.Before(time.Now().Add(time.Minute)) {
		t.Error("bridge CreatedAt is in the future")
	}
}
