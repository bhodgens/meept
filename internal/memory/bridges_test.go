package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// fakeBridgeFactGen is a BridgeGenerator stub recording each call so tests
// can assert on call counts, the entity, the candidate sources, and the
// returned-facts flow.
type fakeBridgeFactGen struct {
	responses []string // canned JSON responses, cycled on exhaustion
	calls     int
	entities  []string   // entity of each call, in order
	srcIDs    [][]string // candidate source IDs of each call
}

func (g *fakeBridgeFactGen) GenerateBridgeFacts(_ context.Context, entity string, sources []Memory) (string, error) {
	g.calls++
	g.entities = append(g.entities, entity)
	ids := make([]string, len(sources))
	for i, s := range sources {
		ids[i] = s.ID
	}
	g.srcIDs = append(g.srcIDs, ids)
	return g.responses[(g.calls-1)%len(g.responses)], nil
}

// failingBridgeGen always errors.
type failingBridgeGen struct{}

func (failingBridgeGen) GenerateBridgeFacts(context.Context, string, []Memory) (string, error) {
	return "", errors.New("llm down")
}

// bridgeTestMemory builds a memory whose metadata lists the given entities
// (the extraction input the v1 extractor reads).
func bridgeTestMemory(id, content string, ents ...string) Memory {
	return Memory{
		ID:      id,
		Content: content,
		Type:    MemoryTypeEpisodic,
		Metadata: map[string]any{
			BridgeMetaEntities: strings.Join(ents, ","),
		},
	}
}

// TestGenerateBridgesRespectsDFWindow pins the document-frequency gate:
// an entity with df=1 is never bridged; df=3 (inside [2, BridgeDFMax]) is;
// df=15 (above the default max of 10) is skipped.
func TestGenerateBridgesRespectsDFWindow(t *testing.T) {
	ctx := context.Background()
	bs := newBridgeTestStore(t)
	gen := &fakeBridgeFactGen{responses: []string{
		`[{"text":"bridge fact alpha","evidence_ids":["m-df1","m-df2","m-df3"]}]`,
	}}

	cfg := BridgesConfig{Enabled: true, BridgeDFMax: 10}

	// df=1: "solo" appears in exactly one memory -> no generation attempt.
	df1 := []Memory{
		bridgeTestMemory("m-solo", "talks about solo alone", "solo"),
	}
	generated, rejected, err := GenerateBridges(ctx, bs, gen, cfg, df1)
	if err != nil {
		t.Fatalf("GenerateBridges(df=1): %v", err)
	}
	if generated != 0 || rejected != 0 {
		t.Errorf("df=1: generated=%d rejected=%d, want 0/0", generated, rejected)
	}
	if gen.calls != 0 {
		t.Errorf("df=1: gen called %d times, want 0", gen.calls)
	}

	// df=3: "shared" appears in three memories -> bridge attempted.
	df3 := []Memory{
		bridgeTestMemory("m-df1", "first shared memory", "shared"),
		bridgeTestMemory("m-df2", "second shared memory", "shared"),
		bridgeTestMemory("m-df3", "third shared memory", "shared"),
	}
	generated, rejected, err = GenerateBridges(ctx, bs, gen, cfg, df3)
	if err != nil {
		t.Fatalf("GenerateBridges(df=3): %v", err)
	}
	if generated != 1 {
		t.Errorf("df=3: generated=%d, want 1", generated)
	}
	if rejected != 0 {
		t.Errorf("df=3: rejected=%d, want 0", rejected)
	}
	if gen.calls != 1 {
		t.Errorf("df=3: gen called %d times, want 1", gen.calls)
	}
	n, err := bs.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Errorf("persisted %d bridges, want 1", n)
	}

	// df=15: above the default max -> skipped, no additional LLM call.
	var df15 []Memory
	for i := 0; i < 15; i++ {
		df15 = append(df15, bridgeTestMemory(
			fmt.Sprintf("m-hot%d", i), "hot topic memory", "hot"))
	}
	generated, rejected, err = GenerateBridges(ctx, bs, gen, cfg, df15)
	if err != nil {
		t.Fatalf("GenerateBridges(df=15): %v", err)
	}
	if generated != 0 || rejected != 0 {
		t.Errorf("df=15: generated=%d rejected=%d, want 0/0", generated, rejected)
	}
	if gen.calls != 1 {
		t.Errorf("df=15: gen called %d times total, want still 1", gen.calls)
	}
}

// TestGenerateBridgesDisabledNoOps: enabled=false must make zero calls to the
// generation client and persist nothing.
func TestGenerateBridgesDisabledNoOps(t *testing.T) {
	ctx := context.Background()
	bs := newBridgeTestStore(t)
	gen := &fakeBridgeFactGen{responses: []string{`[]`}}

	memories := []Memory{
		bridgeTestMemory("m-a", "alpha shares gamma", "gamma"),
		bridgeTestMemory("m-b", "beta shares gamma", "gamma"),
	}
	generated, rejected, err := GenerateBridges(ctx, bs, gen, BridgesConfig{Enabled: false}, memories)
	if err != nil {
		t.Fatalf("GenerateBridges(disabled): %v", err)
	}
	if generated != 0 || rejected != 0 {
		t.Errorf("disabled: generated=%d rejected=%d, want 0/0", generated, rejected)
	}
	if gen.calls != 0 {
		t.Errorf("disabled: gen called %d times, want 0", gen.calls)
	}
}

// TestGenerateBridgesCapsSourcesAndFacts pins both caps: at most
// MaxBridgeSourceMemories source memories feed one bridge entity's prompt,
// and at most MaxBridgeFactsPerSource facts survive per memory.
func TestGenerateBridgesCapsSourcesAndFacts(t *testing.T) {
	ctx := context.Background()
	bs := newBridgeTestStore(t)
	gen := &fakeBridgeFactGen{responses: []string{
		`[{"text":"bridge fact caps","evidence_ids":[]}]`,
	}}

	// 8 candidate memories sharing entity "capped" -> only 5 in the prompt.
	var memories []Memory
	for i := 0; i < 8; i++ {
		memories = append(memories, bridgeTestMemory(
			fmt.Sprintf("m-cap%d", i), fmt.Sprintf("capped memory %d", i), "capped"))
	}
	// Two memories share the eligible entity "zshared" but list it 11th —
	// past the per-memory top-8 extraction cap — so it must be capped away
	// and never bridged.
	flooded := bridgeTestMemory("m-flood", "flood memory",
		"f0", "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "zshared")
	floodedB := bridgeTestMemory("m-floodb", "flood memory b",
		"g0", "g1", "g2", "g3", "g4", "g5", "g6", "g7", "g8", "g9", "zshared")
	memories = append(memories, flooded, floodedB)

	_, _, err := GenerateBridges(ctx, bs, gen, BridgesConfig{Enabled: true, BridgeDFMax: 10}, memories)
	if err != nil {
		t.Fatalf("GenerateBridges(caps): %v", err)
	}

	if gen.calls != 1 {
		t.Fatalf("gen called %d times, want 1 (zshared must be capped away per memory)", gen.calls)
	}
	if gen.entities[0] != "capped" {
		t.Errorf("bridged entity %q, want \"capped\"", gen.entities[0])
	}
	// Source-memory cap: at most 5 candidates reach the generator.
	if got := len(gen.srcIDs[0]); got > MaxBridgeSourceMemories {
		t.Errorf("prompt used %d source memories, want <= %d", got, MaxBridgeSourceMemories)
	}
	if len(gen.srcIDs[0]) == 0 {
		t.Error("prompt used no source memories; caps test is vacuous")
	}
}

// TestGenerateBridgesMalformedJSONRejected: garbage LLM output increments the
// rejected count and persists nothing.
func TestGenerateBridgesMalformedJSONRejected(t *testing.T) {
	ctx := context.Background()
	bs := newBridgeTestStore(t)
	gen := &fakeBridgeFactGen{responses: []string{
		"this is not json at all <<<broken>>>",
	}}

	memories := []Memory{
		bridgeTestMemory("m-x", "x shares y", "y"),
		bridgeTestMemory("m-y", "y shares x", "y"),
	}
	generated, rejected, err := GenerateBridges(ctx, bs, gen, BridgesConfig{Enabled: true, BridgeDFMax: 10}, memories)
	if err != nil {
		t.Fatalf("GenerateBridges(malformed): %v", err)
	}
	if generated != 0 {
		t.Errorf("generated=%d, want 0", generated)
	}
	if rejected != 1 {
		t.Errorf("rejected=%d, want 1", rejected)
	}
	n, err := bs.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("persisted %d bridges, want 0", n)
	}
}

// TestGenerateBridgesEvidenceOverrideFromSources: when the model cites no
// evidence_ids, the pass supplies the candidate source-memory IDs itself —
// the bridge's provenance is the sources it was generated FROM.
func TestGenerateBridgesEvidenceOverrideFromSources(t *testing.T) {
	ctx := context.Background()
	bs := newBridgeTestStore(t)
	gen := &fakeBridgeFactGen{responses: []string{
		`[{"text":"bridge with implicit evidence","evidence_ids":[]}]`,
	}}

	memories := []Memory{
		bridgeTestMemory("m-imp1", "imp1 shares w", "w"),
		bridgeTestMemory("m-imp2", "imp2 shares w", "w"),
	}
	generated, rejected, err := GenerateBridges(ctx, bs, gen, BridgesConfig{Enabled: true, BridgeDFMax: 10}, memories)
	if err != nil {
		t.Fatalf("GenerateBridges(implicit evidence): %v", err)
	}
	if generated != 1 || rejected != 0 {
		t.Fatalf("generated=%d rejected=%d, want 1/0", generated, rejected)
	}

	recs, err := bs.ListBridgesByEvidence(ctx, "m-imp1")
	if err != nil {
		t.Fatalf("ListBridgesByEvidence: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d bridges citing m-imp1, want 1", len(recs))
	}
	if len(recs[0].EvidenceIDs) != 2 {
		t.Errorf("evidence_ids = %v, want both source IDs", recs[0].EvidenceIDs)
	}
}

// TestGenerateBridgesUnknownEvidenceRejected: the model citing evidence IDs
// that are not among the candidate sources leaves nothing real to pin the
// fact to — filtered to empty, the bridge is rejected (no provenance, no ship).
func TestGenerateBridgesUnknownEvidenceRejected(t *testing.T) {
	ctx := context.Background()
	bs := newBridgeTestStore(t)
	gen := &fakeBridgeFactGen{responses: []string{
		`[{"text":"hallucinated provenance","evidence_ids":["m-does-not-exist"]}]`,
	}}

	memories := []Memory{
		bridgeTestMemory("m-e1", "e1 shares z", "z"),
		bridgeTestMemory("m-e2", "e2 shares z", "z"),
	}
	generated, rejected, err := GenerateBridges(ctx, bs, gen, BridgesConfig{Enabled: true, BridgeDFMax: 10}, memories)
	if err != nil {
		t.Fatalf("GenerateBridges(unknown evidence): %v", err)
	}
	if generated != 0 {
		t.Errorf("generated=%d, want 0", generated)
	}
	if rejected != 1 {
		t.Errorf("rejected=%d, want 1", rejected)
	}
	n, err := bs.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("persisted %d bridges, want 0", n)
	}
}

// TestGenerateBridgesGenError: an LLM transport error is returned to the
// caller (the consolidator logs and continues) — not silently swallowed.
func TestGenerateBridgesGenError(t *testing.T) {
	ctx := context.Background()
	bs := newBridgeTestStore(t)
	gen := failingBridgeGen{}

	memories := []Memory{
		bridgeTestMemory("m-f1", "f1 shares q", "q"),
		bridgeTestMemory("m-f2", "f2 shares q", "q"),
	}
	_, _, err := GenerateBridges(ctx, bs, gen, BridgesConfig{Enabled: true, BridgeDFMax: 10}, memories)
	if err == nil {
		t.Fatal("GenerateBridges returned nil error on gen failure, want error")
	}
	if !errors.Is(err, ErrBridgeGenerate) {
		t.Errorf("error = %v, want ErrBridgeGenerate wrap", err)
	}
}

// TestBridgeGenerator_InjectsGrammar pins the grammar wiring: the default
// llmBridgeGenerator must carry llm.WithRawGrammar with llm.BridgeGrammar()
// on every Chat call, mirroring the distill grammar wiring pattern.
func TestBridgeGenerator_InjectsGrammar(t *testing.T) {
	chatter := &recordingDistillChatter{}
	g := &llmBridgeGenerator{client: chatter}
	_, err := g.GenerateBridgeFacts(context.Background(), "rust", []Memory{
		{ID: "m-1", Content: "alice prefers rust"},
		{ID: "m-2", Content: "bob's team ships rust services"},
	})
	if err != nil {
		t.Fatalf("GenerateBridgeFacts: %v", err)
	}
	if chatter.called != 1 {
		t.Fatalf("called %d times, want 1", chatter.called)
	}
	if len(chatter.opts) == 0 {
		t.Fatal("generation call carried no chat options")
	}
	if got := llm.RawGrammarOf(chatter.opts); got != llm.BridgeGrammar() {
		t.Errorf("RawGrammarOf(opts) != BridgeGrammar()")
	}

	// The grammar must force a bare JSON ARRAY of {text, evidence_ids}
	// objects — not a wrapper object, not a bare string.
	gm := llm.BridgeGrammar()
	if !strings.Contains(gm, `root ::= "["`) {
		t.Error("bridge grammar does not force a bare JSON array")
	}
	for _, want := range []string{"text", "evidence_ids"} {
		if !strings.Contains(gm, want) {
			t.Errorf("bridge grammar missing required member %q", want)
		}
	}
}

// newBridgeTestStore opens a BridgeStore in a temp dir with a quiet logger.
func newBridgeTestStore(t *testing.T) *BridgeStore {
	t.Helper()
	bs, err := NewBridgeStore(t.TempDir() + "/bridge.vec.db")
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	t.Cleanup(func() { _ = bs.Close() })
	return bs.WithLogger(testBridgeSlog())
}
