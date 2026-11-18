package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/vector"
	"github.com/caimlas/meept/internal/rpc"
)

// ---------------------------------------------------------------------------
// Wave-2 wiring pins: ShardManager hybrid search (memory.vector_hybrid) and
// HALO Phase 6 atomic compaction (agent.atomic_compaction). Both are opt-in,
// default false (docs/agents/opt-in-defaults.md).
// ---------------------------------------------------------------------------

// TestVectorHybrid_DisabledByDefault pins the config defaults.
func TestVectorHybrid_DisabledByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.Memory.VectorHybrid.Enabled {
		t.Fatal("memory.vector_hybrid.enabled must default to false (opt-in-defaults rule)")
	}
	if cfg.Agent.AtomicCompaction.Enabled {
		t.Fatal("agent.atomic_compaction.enabled must default to false (opt-in-defaults rule)")
	}
}

// TestVectorHybrid_NewWiringWithoutVectorStoreFailsClosed pins real-path
// construction: a manager without a ShardManager vector adapter must NOT
// produce a wiring (the daemon logs and continues with the legacy path).
func TestVectorHybrid_NewWiringWithoutVectorStoreFailsClosed(t *testing.T) {
	mgr := memory.NewManager(memory.ManagerConfig{
		Logger: slog.New(slog.DiscardHandler),
	})
	wiring, err := newVectorHybridWiring(mgr, 0.5, false, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("expected error when the manager has no ShardManager vector searcher")
	}
	if wiring != nil {
		t.Fatal("expected nil wiring on error (fail closed to the legacy path)")
	}
}

// newWiringTestServer builds an RPC server the same way daemon.go does.
func newWiringTestServer(t *testing.T) *rpc.Server {
	t.Helper()
	return rpc.New(&rpc.Config{SocketPath: filepathJoin(t.TempDir(), "test.sock")}, nil, slog.New(slog.DiscardHandler))
}

// filepathJoin avoids importing path/filepath for one call.
func filepathJoin(parts ...string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "/"
		}
		out += p
	}
	return out
}

// TestVectorHybrid_RPCHandlerNotRegisteredWhenDisabled pins the RPC gate:
// registerVectorHybridRPCHandler with nil wiring registers nothing.
func TestVectorHybrid_RPCHandlerNotRegisteredWhenDisabled(t *testing.T) {
	server := newWiringTestServer(t)
	registerVectorHybridRPCHandler(server, nil, 10)
	_, err := server.CallMethod(context.Background(), "memory.search.hybrid", nil)
	if err == nil {
		t.Fatal("memory.search.hybrid must NOT resolve when the hybrid wiring is nil (disabled)")
	}
}

// TestVectorHybrid_RPCHandlerRegisteredWhenEnabled pins the enabled RPC
// surface over a real hybrid searcher (keyword lane only — the vector lane
// cross-shard join has its own functional pin in internal/memory/vector).
func TestVectorHybrid_RPCHandlerRegisteredWhenEnabled(t *testing.T) {
	hybrid, err := vector.NewHybridSearcher(vector.HybridSearcherConfig{
		VectorSearcher:  &stubVectorSearcher{},
		KeywordSearcher: &fixtureKeywordAdapter{},
		Alpha:           0.5,
	})
	if err != nil {
		t.Fatalf("create hybrid searcher: %v", err)
	}
	wiring := &vectorHybridWiring{searcher: hybrid}

	server := newWiringTestServer(t)
	registerVectorHybridRPCHandler(server, wiring, 5)

	params, err := json.Marshal(map[string]any{"query": "alpha", "limit": 3})
	if err != nil {
		t.Fatal(err)
	}
	result, err := server.CallMethod(context.Background(), "memory.search.hybrid", params)
	if err != nil {
		t.Fatalf("handler call: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result shape: %T", result)
	}
	results, ok := m["results"].([]map[string]any)
	if !ok {
		t.Fatalf("unexpected results shape: %T", m["results"])
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 keyword-lane hit, got %d", len(results))
	}
	if results[0]["memory_id"] != "mem-kw-1" {
		t.Errorf("unexpected hit id: %v", results[0]["memory_id"])
	}
	if _, ok := results[0]["combined_score"].(float64); !ok {
		t.Errorf("combined_score missing from result: %v", results[0])
	}
}

type stubVectorSearcher struct{}

func (s *stubVectorSearcher) Search(_ context.Context, _ string, _ int) ([]vector.SearchResult, error) {
	return nil, nil
}

type fixtureKeywordAdapter struct{}

func (f *fixtureKeywordAdapter) SearchKeyword(_ context.Context, _ string, _ int) ([]vector.KeywordResult, error) {
	return []vector.KeywordResult{{
		ID:             "mem-kw-1",
		Content:        "alpha note",
		RelevanceScore: 0.9,
	}}, nil
}

// TestAtomicCompaction_DisabledKeepsRawWindow pins the disabled posture:
// wireL0Distill with atomic compaction off (the DefaultConfig baseline)
// constructs a DistillWorker whose compactor is nil.
func TestAtomicCompaction_DisabledKeepsRawWindow(t *testing.T) {
	memCfg := config.DefaultConfig().Memory
	memCfg.L0Enabled = true
	mgr := newWiringTestManager(t)
	memCfg.DataDir = mgr.DataDir()
	agentCfg := config.DefaultConfig().Agent // AtomicCompaction.Enabled == false
	l0c := &l0Components{dataDir: mgr.DataDir()}
	logger := slog.New(slog.DiscardHandler)

	worker := wireL0Distill(l0c, mgr, nil, memCfg, agentCfg, logger)
	if worker == nil {
		t.Fatal("expected a worker for l0_enabled with a data dir")
	}
	if worker.AtomicCompactor() != nil {
		t.Fatal("compactor must be absent when agent.atomic_compaction.enabled is false")
	}
}

// newWiringTestManager builds an initialized SQLite-backed memory manager
// (the wireL0Distill dependency).
func newWiringTestManager(t *testing.T) *memory.Manager {
	t.Helper()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  t.TempDir(),
			Episodic: config.EpisodicConfig{Enabled: true},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}
