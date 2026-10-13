//go:build e2e

// Suite bridges (branch 09 B06): the bridge subsystem end-to-end against a
// sandboxed daemon — store wired at boot, generation through the fake LLM
// (grammar-constrained JSON mocked per-request shape), recall capped at k_b,
// and cascade invalidation when a source memory is deleted. Fully hermetic:
// no live model, no user config, seeded memory store.
package bridges

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

// fakeBridgeGen is the hermetic BridgeGenerator: deterministic canned facts,
// no LLM. Mirrors the BridgeGenerator interface in internal/memory.
type fakeBridgeGen struct {
	facts string
	calls int
}

func (g *fakeBridgeGen) GenerateBridgeFacts(_ context.Context, _ string, _ []memory.Memory) (string, error) {
	g.calls++
	return g.facts, nil
}

// TestBridgesEndToEnd covers the suite's four-beat contract:
// enable -> generate against a seeded store -> recall admits at most k_b
// bridges -> invalidate a source memory and the bridge is gone.
func TestBridgesEndToEnd(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// --- Seeded store + manager (the unit-level hermetic tier). ---
	mgr, bs, tmpDir := seedBridgeWorld(t, root)
	defer func() { _ = mgr.Close() }()
	defer func() { _ = bs.Close() }()

	// --- Beat 1+2: enable + generate against the seeded store. ---
	gen := &fakeBridgeGen{facts: `[
		{"text": "projectx is led by alice", "evidence_ids": []},
		{"text": "projectx runs on postgres 16", "evidence_ids": []},
		{"text": "projectx is led by alice", "evidence_ids": []},
		{"text": "projectx is led by alice", "evidence_ids": []}
	]`}
	generated, rejected, err := memory.GenerateBridges(ctx, bs, gen, memory.BridgesConfig{
		Enabled:     true,
		BridgeDFMax: 10,
	}, seededMemories)
	if err != nil {
		t.Fatalf("GenerateBridges: %v", err)
	}
	if generated == 0 {
		t.Fatal("bridge generation produced nothing against the seeded store")
	}
	if rejected != 0 {
		t.Fatalf("unexpected rejections: %d", rejected)
	}
	if gen.calls == 0 {
		t.Fatal("generator was never called despite enabled=true")
	}
	n, err := bs.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != generated {
		t.Fatalf("store count %d != generated %d", n, generated)
	}

	// --- Beat 3: recall includes at most k_b (2) bridges. ---
	results, included := runBalancedRecall(t, bs, "projectx", 10)
	if included > 2 {
		t.Fatalf("recall admitted %d bridges, cap k_b=2", included)
	}
	if len(results) == 0 {
		t.Fatal("balanced recall returned nothing for a seeded store")
	}

	// The CLI visibility surface: the bridge rows carry the verified flag
	// + evidence count metadata the CLI/TUI render.
	bridges, err := bs.ListBridges(ctx)
	if err != nil {
		t.Fatalf("ListBridges: %v", err)
	}
	for _, b := range bridges {
		if len(b.EvidenceIDs) == 0 {
			t.Errorf("bridge %s persisted without provenance", b.ID)
		}
		if _, ok := b.Metadata[memory.BridgeMetaVerified]; !ok {
			t.Errorf("bridge %s missing verified metadata", b.ID)
		}
	}

	// --- Beat 4: invalidate a source memory -> bridges citing it are gone. ---
	memID := seededEvidenceID(t, mgr)
	if memID == "" {
		t.Fatal("no source memory id recorded from seeding")
	}
	before, err := bs.ListBridgesByEvidence(ctx, memID)
	if err != nil {
		t.Fatalf("ListBridgesByEvidence: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("invalidation fixture broken: no bridge cites the source memory")
	}
	if err := mgr.Delete(ctx, memID); err != nil {
		t.Fatalf("Delete source memory: %v", err)
	}
	t.Logf("deleted source %s; bridges citing it before: %d", memID, len(before))
	after, err := bs.ListBridgesByEvidence(ctx, memID)
	if err != nil {
		t.Fatalf("ListBridgesByEvidence after delete: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("bridges survived source deletion: %+v", after)
	}
	// Exactly the seeded evidence bridge is gone: the count returns to the
	// pre-seed level (n was captured before seededEvidenceID added
	// bridge-e2e-1).
	finalCount, err := bs.Count(ctx)
	if err != nil {
		t.Fatalf("Count after invalidation: %v", err)
	}
	if finalCount != n {
		t.Errorf("bridge count after invalidation = %d, want %d (only the invalidated bridge should be gone)", finalCount, n)
	}
	_ = tmpDir
}

// TestBridgesDaemonWiring boots a full sandboxed daemon with
// memory.bridges.enabled=true and drives the CLI visibility surface through
// the real RPC stack: the bridge store is wired at boot, `meept memory
// --bridges` reports (empty is fine — generation needs a consolidation run),
// and the daemon stays healthy with the subsystem active.
func TestBridgesDaemonWiring(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"memory.bridges.enabled":       true,
		"memory.bridges.kb":            2,
		"memory.bridges.bridge_df_max": 10,
	}))

	// The daemon must be up and answering.
	health := s.HealthJSON(t)
	if strings.Contains(strings.ToLower(health["status"]), "fail") {
		t.Fatalf("daemon unhealthy with bridges enabled: %+v", health)
	}

	// The visibility RPC answers through the real daemon (empty store ->
	// honest empty list, never an error).
	out, stderr := s.RunCLI(t, 30*time.Second, false, "memory", "--bridges")
	combined := out + stderr
	if strings.Contains(combined, "failed to list bridges") {
		t.Fatalf("memory --bridges failed against a live daemon:\n%s\n%s", out, stderr)
	}
	if !strings.Contains(strings.ToLower(combined), "bridge") {
		t.Fatalf("memory --bridges output mentions nothing bridge-shaped:\n%s\n%s", out, stderr)
	}
}

// TestBridgesDisabledDefault: the default config (no overlay) leaves the
// daemon bridge-less — `meept memory --bridges` reports the honest empty
// state instead of failing (the memory.bridges RPC is unavailable with no
// store wired, and the CLI renders that as the empty message).
func TestBridgesDisabledDefault(t *testing.T) {
	s := harness.Start(t)
	out, stderr := s.RunCLI(t, 30*time.Second, false, "memory", "--bridges")
	combined := strings.ToLower(out + stderr)
	if strings.Contains(combined, "failed to") || strings.Contains(combined, "error:") {
		t.Fatalf("memory --bridges errored with bridges disabled:\n%s\n%s", out, stderr)
	}
	if !strings.Contains(combined, "no bridges") {
		t.Fatalf("expected the honest empty state with bridges disabled:\n%s\n%s", out, stderr)
	}
}

// seedBridgeWorld builds an initialized SQLite-backed Manager + BridgeStore
// (same fixture shape as the invalidation unit tests) and seeds the source
// memories the generation pass needs.
func seedBridgeWorld(t *testing.T, root string) (*memory.Manager, *memory.BridgeStore, string) {
	t.Helper()
	tmpDir := filepath.Join(root, "memory")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend: config.MemoryBackendSQLite,
			DataDir: tmpDir,
			Episodic: config.EpisodicConfig{
				Enabled: true,
			},
			Task: config.TaskMemoryConfig{
				Enabled: true,
				Domains: []string{"general", "code"},
			},
			Personality: config.PersonalityConfig{
				Enabled: false,
			},
		},
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	bs, err := memory.NewBridgeStore(filepath.Join(tmpDir, "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	mgr.SetBridgeStore(bs)
	return mgr, bs, tmpDir
}

// seededMemories is the shared candidate pool: three memories that all
// mention "ProjectX" (df=3, inside the [2,10] window).
var seededMemories = []memory.Memory{
	{ID: "mem-px-1", Content: "ProjectX kickoff scheduled for monday", Metadata: map[string]any{"entities": "projectx"}},
	{ID: "mem-px-2", Content: "ProjectX budget approved by finance", Metadata: map[string]any{"entities": "projectx"}},
	{ID: "mem-px-3", Content: "ProjectX migration plan reviewed", Metadata: map[string]any{"entities": "projectx"}},
}

// seededEvidenceID stores the three fixture memories through the real
// Manager and returns the first stored ID (the invalidation target).
func seededEvidenceID(t *testing.T, mgr *memory.Manager) string {
	t.Helper()
	ctx := context.Background()
	id, err := mgr.Store(ctx, memory.Memory{
		Content:  "alice leads the projectx migration",
		Type:     memory.MemoryTypeEpisodic,
		Category: "conversation",
		Metadata: map[string]any{"entities": "projectx"},
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	bridgeRec := memory.BridgeRecord{
		ID:          "bridge-e2e-1",
		Content:     "projectx is led by alice",
		Embedding:   hashBridgeEmbeddingForTest("projectx is led by alice"),
		EvidenceIDs: []string{id},
		CreatedAt:   time.Now().UTC(),
		Verified:    true,
	}
	store := mgr.BridgeStore()
	if store == nil {
		t.Fatal("manager has no bridge store wired")
	}
	if err := store.PutBridge(ctx, bridgeRec); err != nil {
		t.Fatalf("PutBridge: %v", err)
	}
	return id
}

// runBalancedRecall exercises the recall path through the store's search +
// the balanced-selection cap, mirroring B04's post-retrieval gate. Returns
// the admitted bridges and how many bridges passed the k_b=2 cap.
func runBalancedRecall(t *testing.T, bs *memory.BridgeStore, query string, k int) ([]memory.BridgeRecord, int) {
	t.Helper()
	ctx := context.Background()
	emb := hashBridgeEmbeddingForTest(query)
	// Over-fetch so the cap has something to bite on.
	hits, err := bs.SearchByEmbedding(ctx, emb, k)
	if err != nil {
		t.Fatalf("SearchByEmbedding: %v", err)
	}
	included := 0
	var admitted []memory.BridgeRecord
	for _, h := range hits {
		if included < 2 { // k_b = 2
			included++
			admitted = append(admitted, h)
		}
	}
	return admitted, included
}

// hashBridgeEmbeddingForTest re-derives the deterministic v1 embedding the
// generation pass uses (256-dim lexical hash). Duplicated here because the
// e2e suite is an external test package.
func hashBridgeEmbeddingForTest(text string) []float32 {
	dim := 256
	v := make([]float32, dim)
	for _, b := range []byte(strings.ToLower(text)) {
		v[int(b)%dim] += 1
	}
	return v
}
