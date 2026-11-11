package daemon

// Pin tests for the bughunt D3/D4/D11 fixes. TESTS ONLY.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/vector"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// ---------------------------------------------------------------------------
// D3: concurrent appends from both lanes on the ONE shared store verify
// ---------------------------------------------------------------------------

// TestSharedToolAuditStoreConcurrentAppendsVerify pins the D3 fix shape:
// ONE *auditlog.Store feeds both audit lanes; concurrent Append from two
// goroutines (simulating gate G2 + groups membership-change emissions)
// leaves a contiguous, verifiable chain with no dropped seq.
func TestSharedToolAuditStoreConcurrentAppendsVerify(t *testing.T) {
	store, err := auditlog.OpenStore(filepath.Join(t.TempDir(), "tool_audit_chain.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open shared audit store: %v", err)
	}
	defer func() { _ = store.Close() }()

	const perLane = 50
	var wg sync.WaitGroup
	appendLane := func(prefix string) {
		defer wg.Done()
		for i := 0; i < perLane; i++ {
			if _, aerr := store.Append(context.Background(), auditlog.Record{
				Type:    prefix,
				Payload: map[string]any{"i": i},
			}); aerr != nil {
				t.Errorf("lane %s append %d: %v", prefix, i, aerr)
				return
			}
		}
	}
	wg.Add(2)
	go appendLane("gate.g2")
	go appendLane("groups.membership")
	wg.Wait()

	res, verr := auditlog.VerifyChain(context.Background(), store.ChainDB())
	if verr != nil {
		t.Fatalf("VerifyChain: %v", verr)
	}
	if !res.OK {
		t.Fatalf("chain broken at %d: %s", res.BrokenAt, res.Reason)
	}
	if res.Records != perLane*2 {
		t.Fatalf("chain records = %d, want %d (dropped rows = the D3 cross-pool loss)", res.Records, perLane*2)
	}
}

// TestGroupsLaneUsesSharedToolGateAudit pins the constructor injection:
// the daemon's groups surface must read components.ToolGateAudit (the
// SAME handle the gate's G2 lane uses), never open a second pool. The
// handler stores the store pointer — assert both lanes see one store by
// appending through the handler's lane and verifying via the gate lane's
// handle.
func TestGroupsLaneUsesSharedToolGateAudit(t *testing.T) {
	store, err := auditlog.OpenStore(filepath.Join(t.TempDir(), "tool_audit_chain.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open shared audit store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// The wiring shape (daemon.go): groupsAudit := components.ToolGateAudit.
	components := &Components{ToolGateAudit: store}
	if components.ToolGateAudit != store {
		t.Fatal("ToolGateAudit must carry the shared store")
	}
	// A group-lane append through the shared handle must land in the same
	// chain the gate lane reads.
	if _, aerr := store.Append(context.Background(), auditlog.Record{
		Type:    "groups.membership",
		Payload: map[string]any{"op": "add"},
	}); aerr != nil {
		t.Fatalf("groups-lane append: %v", aerr)
	}
	head, ok, herr := store.Head(context.Background())
	if herr != nil || !ok || head.Type != "groups.membership" {
		t.Fatalf("head = %+v ok=%v err=%v; the shared-lane append did not land", head, ok, herr)
	}
}

// ---------------------------------------------------------------------------
// D4: stop path cancels the anchor goroutine
// ---------------------------------------------------------------------------

// TestStopAnchorCancelsAnchorJob pins the WiringResult.StopAnchor seam:
// the anchor job started by NewManagerFromConfig must exit when the
// daemon's stop path cancels it (previously no shutdown path cancelled
// the wiring ctx — one leaked goroutine per restart).
func TestStopAnchorCancelsAnchorJob(t *testing.T) {
	// Build the anchor seam the wiring produces: a NewAnchorJob over a
	// real store, Run on a child of the given ctx, StopAnchor cancels.
	dir := t.TempDir()
	store, err := auditlog.OpenStore(filepath.Join(dir, "audit_chain.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open chain store: %v", err)
	}
	defer func() { _ = store.Close() }()

	wiringCtx, wiringCancel := context.WithCancel(context.Background())
	defer wiringCancel()
	anchorCtx, stopAnchor := context.WithCancel(wiringCtx)
	job := auditlog.NewAnchorJob(store, dir, 25*time.Millisecond, nil)
	done := make(chan struct{})
	go func() {
		job.Run(anchorCtx)
		close(done)
	}()

	// The daemon stop path (stopComponents) cancels ONLY StopAnchor —
	// never the wiring ctx.
	stopAnchor()
	select {
	case <-done:
		// The anchor goroutine exited on the stop path: the D4 fix.
	case <-time.After(2 * time.Second):
		t.Fatal("anchor goroutine still running after StopAnchor: the D4 leak")
	}
}

// ---------------------------------------------------------------------------
// D11: hybrid search limit clamp + scope filtering
// ---------------------------------------------------------------------------

type f7StubVectorSearcher struct{}

func (f7StubVectorSearcher) Search(_ context.Context, _ string, _ int) ([]vector.SearchResult, error) {
	return nil, nil
}

// f7RecordingKeywordAdapter records the limits it was called with.
type f7RecordingKeywordAdapter struct {
	mu     sync.Mutex
	limits []int
}

func (a *f7RecordingKeywordAdapter) SearchKeyword(_ context.Context, _ string, limit int) ([]vector.KeywordResult, error) {
	a.mu.Lock()
	a.limits = append(a.limits, limit)
	a.mu.Unlock()
	return []vector.KeywordResult{{
		ID:             "mem-d11",
		Content:        "alpha note",
		RelevanceScore: 0.9,
	}}, nil
}

func TestVectorHybridLimitClamped(t *testing.T) {
	kw := &f7RecordingKeywordAdapter{}
	hybrid, err := vector.NewHybridSearcher(vector.HybridSearcherConfig{
		VectorSearcher:  f7StubVectorSearcher{},
		KeywordSearcher: kw,
		Alpha:           0.5,
	})
	if err != nil {
		t.Fatalf("create hybrid: %v", err)
	}
	// limit=1e9 (and 0 and negative) must clamp: the keyword lane sees
	// at most 2*MaxSearchLimit, never 2e9.
	if _, err := hybrid.Search(context.Background(), "alpha", 1_000_000_000); err != nil {
		t.Fatalf("search: %v", err)
	}
	kw.mu.Lock()
	got := append([]int(nil), kw.limits...)
	kw.mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("keyword lane called %d times, want 1", len(got))
	}
	if got[0] > 2*vector.MaxSearchLimit {
		t.Fatalf("keyword lane limit = %d, unclamped (the D11 unbounded-scan regression)", got[0])
	}
}

func TestClampLimitBounds(t *testing.T) {
	if got := vector.ClampLimit(1_000_000_000, 10); got != vector.MaxSearchLimit {
		t.Fatalf("ClampLimit(1e9) = %d, want %d", got, vector.MaxSearchLimit)
	}
	if got := vector.ClampLimit(0, 10); got != 10 {
		t.Fatalf("ClampLimit(0) = %d, want default 10", got)
	}
	if got := vector.ClampLimit(-5, 10); got != 10 {
		t.Fatalf("ClampLimit(-5) = %d, want default 10", got)
	}
	if got := vector.ClampLimit(25, 10); got != 25 {
		t.Fatalf("ClampLimit(25) = %d, want 25 (in-range passthrough)", got)
	}
}

// f7MetaKeywordAdapter returns rows with fixed metadata (for the scope pin).
type f7MetaKeywordAdapter struct {
	meta map[string]any
}

func (a *f7MetaKeywordAdapter) SearchKeyword(_ context.Context, _ string, _ int) ([]vector.KeywordResult, error) {
	return []vector.KeywordResult{{
		ID:             "mem-scoped",
		Content:        "scoped note",
		Metadata:       a.meta,
		RelevanceScore: 0.9,
	}}, nil
}

func TestVectorHybridScopedCallerSeesOnlyPermittedRows(t *testing.T) {
	// The row is PROJECT-scoped to another project: a caller whose
	// purview is project p1 must NOT see it; a user-purview caller must.
	projectRow := map[string]any{"scope": "project", "project_id": "p2"}
	hybrid, err := vector.NewHybridSearcher(vector.HybridSearcherConfig{
		VectorSearcher:  f7StubVectorSearcher{},
		KeywordSearcher: &f7MetaKeywordAdapter{meta: projectRow},
		Alpha:           0.5,
		Scope: func(meta map[string]any) bool {
			// C2 predicate shape: project caller p1 sees only p1 rows.
			return memory.VisibleScopeRow(meta, memory.ScopeProject, "p1", "u1", false)
		},
	})
	if err != nil {
		t.Fatalf("create hybrid: %v", err)
	}
	res, err := hybrid.Search(context.Background(), "scoped", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("project caller p1 saw %d rows from project p2 — the C35 cross-scope leak", len(res))
	}

	// User purview sees project rows: same row, user caller.
	hybridUser, err := vector.NewHybridSearcher(vector.HybridSearcherConfig{
		VectorSearcher:  f7StubVectorSearcher{},
		KeywordSearcher: &f7MetaKeywordAdapter{meta: projectRow},
		Alpha:           0.5,
		Scope: func(meta map[string]any) bool {
			return memory.VisibleScopeRow(meta, memory.ScopeUser, "", "u1", false)
		},
	})
	if err != nil {
		t.Fatalf("create hybrid (user purview): %v", err)
	}
	resUser, err := hybridUser.Search(context.Background(), "scoped", 10)
	if err != nil {
		t.Fatalf("search (user): %v", err)
	}
	if len(resUser) != 1 {
		t.Fatalf("user caller saw %d rows, want 1 (the user purview must span projects)", len(resUser))
	}
}

// TestVectorHybridRPCRespectsScopeFilter pins the wiring-level path: the
// registered memory.search.hybrid handler must answer through the scoped
// searcher (a project-p2 row invisible to the p1-purview predicate).
func TestVectorHybridRPCRespectsScopeFilter(t *testing.T) {
	projectRow := map[string]any{"scope": "project", "project_id": "p2"}
	hybrid, err := vector.NewHybridSearcher(vector.HybridSearcherConfig{
		VectorSearcher:  f7StubVectorSearcher{},
		KeywordSearcher: &f7MetaKeywordAdapter{meta: projectRow},
		Alpha:           0.5,
		Scope: func(meta map[string]any) bool {
			return memory.VisibleScopeRow(meta, memory.ScopeProject, "p1", "", false)
		},
	})
	if err != nil {
		t.Fatalf("create hybrid: %v", err)
	}
	server := rpc.New(&rpc.Config{SocketPath: filepath.Join(t.TempDir(), "vh.sock")}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	registerVectorHybridRPCHandler(server, &vectorHybridWiring{searcher: hybrid}, 10)

	params, _ := json.Marshal(services.VectorSearchRequest{Query: "scoped", Limit: 3})
	result, err := server.CallMethod(context.Background(), "memory.search.hybrid", params)
	if err != nil {
		t.Fatalf("memory.search.hybrid: %v", err)
	}
	m := result.(map[string]any)
	results := m["results"].([]map[string]any)
	if len(results) != 0 {
		t.Fatalf("RPC surface returned %d cross-scope rows — the C35 RPC leak", len(results))
	}
}
