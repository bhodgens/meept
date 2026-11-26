package tierpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

// H7/A3-1 + owner ruling D3 pins: the compress payload carries the
// engine's EXACT target IDs, and the REAL distill worker processes
// them (pre-fix, the payload had no IDs and the worker rejected every
// job with "distill: l1 payload missing l0_path").

// TestExecuteCompressPayloadCarriesExactIDs: enqueueCompress must
// serialize Action.TargetIDs into the payload's memory_ids field.
func TestExecuteCompressPayloadCarriesExactIDs(t *testing.T) {
	t.Parallel()
	enq := &stubEnqueuer{}
	e := New(enabledCfg(), Deps{Enqueuer: enq})
	ids := []string{"r1", "r2", "r3", "r4", "r5"}
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindCompress, TargetIDs: ids, Reason: "tier=episodic;tokens=95;ceiling=100"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.CompressionEnqueued != len(ids) {
		t.Fatalf("enqueued = %d, want %d", rep.CompressionEnqueued, len(ids))
	}
	if len(enq.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(enq.jobs))
	}
	var p memory.L1ExtractPayload
	if err := json.Unmarshal(enq.jobs[0].Payload, &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if fmt.Sprint(p.MemoryIDs) != fmt.Sprint(ids) {
		t.Fatalf("MemoryIDs = %v, want exactly %v", p.MemoryIDs, ids)
	}
	if p.L0Path != "" {
		t.Fatalf("row shape must not reuse L0Path, got %q", p.L0Path)
	}
	// Mutating the action's slice afterward must not corrupt the job.
	ids[0] = "mutated"
	var p2 memory.L1ExtractPayload
	if err := json.Unmarshal(enq.jobs[0].Payload, &p2); err != nil {
		t.Fatal(err)
	}
	if p2.MemoryIDs[0] != "r1" {
		t.Fatalf("payload aliased the caller slice: %v", p2.MemoryIDs)
	}
}

// --- e2e: engine payload -> REAL DistillWorker.Process -------------

// tierFakeLLM is the minimal DistillLLM fake (memory's fake is in an
// internal test file, unreachable from here).
type tierFakeLLM struct {
	mu      sync.Mutex
	calls   int
	prompts []string
}

func (f *tierFakeLLM) DistillComplete(_ context.Context, prompt string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.prompts = append(f.prompts, prompt)
	return []byte(`[{"type":"claim","text":"distilled from tier selection","source":"conversation","confidence":0.7,"premises":[],"category":"technical"}]`), nil
}

// tierTestManager builds an initialized SQLite-backed memory manager.
func tierTestManager(t *testing.T) *memory.Manager {
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

// TestCompressJobEndToEndThroughDistillWorker: the FULL engine->worker
// roundtrip. Seed episodic rows, run Evaluate(hard band) + Execute to
// produce the queue job, feed the job's payload through
// memory.DistillWorker.Process, and assert it distills the exact
// selected ids. NON-VACUITY: pre-fix this Process call returns
// "distill: l1 payload missing l0_path".
func TestCompressJobEndToEndThroughDistillWorker(t *testing.T) {
	mgr := tierTestManager(t)
	ctx := context.Background()
	seed := []string{}
	for i := range 8 {
		id, err := mgr.Episodic().Store(ctx,
			fmt.Sprintf("tier e2e row %d content padding padding", i), "conversation", nil)
		if err != nil {
			t.Fatalf("seed row: %v", err)
		}
		seed = append(seed, id)
	}
	cands := &stubCandidates{byTier: map[string][]memory.MemoryResult{}}
	for _, id := range seed {
		cands.byTier["episodic"] = append(cands.byTier["episodic"],
			memory.MemoryResult{Memory: memory.Memory{ID: id, Content: "row", CreatedAt: time.Now().Add(-time.Hour)}})
	}
	enq := &stubEnqueuer{}
	e := New(enabledCfg(), Deps{Candidates: cands, Votes: stubVotes{}, Enqueuer: enq})
	states := []TierState{{Tier: "episodic", Tokens: 95, Items: 8, Ceiling: 100}}
	actions, err := e.Evaluate(ctx, states)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	rep, err := e.Execute(ctx, actions)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.CompressionEnqueued == 0 {
		t.Fatalf("no compression enqueued (actions=%+v)", actions)
	}
	if len(enq.jobs) != 1 {
		t.Fatalf("jobs = %d, want the one compress job", len(enq.jobs))
	}

	var compressIDs []string
	for _, a := range actions {
		if a.Kind == KindCompress {
			compressIDs = a.TargetIDs
		}
	}
	llm := &tierFakeLLM{}
	w := memory.NewDistillWorker(memory.DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: t.TempDir(),
		Logger:  slog.New(slog.DiscardHandler),
	})
	res, err := w.Process(ctx, enq.jobs[0])
	if err != nil {
		t.Fatalf("worker Process on the ENGINE payload (pre-fix: 'missing l0_path'): %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["status"] != "ok" {
		t.Fatalf("worker result = %+v, want status ok", res)
	}
	if m["rows_total"] != len(compressIDs) {
		t.Fatalf("rows_total = %v, want %d", m["rows_total"], len(compressIDs))
	}

	// Every selected row is now flagged compressed; the worker saw
	// their contents, nothing else.
	llm.mu.Lock()
	prompt := llm.prompts[0]
	llm.mu.Unlock()
	for _, id := range compressIDs {
		r, err := mgr.Episodic().GetByID(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if !rowFlagged(r) {
			t.Fatalf("engine-selected row %s not flagged after worker distill", id)
		}
		if !strings.Contains(prompt, r.Memory.Content) {
			t.Fatalf("prompt missing selected row content %q", r.Memory.Content)
		}
	}
}

func rowFlagged(r *memory.MemoryResult) bool {
	switch v := r.Memory.Metadata["l1_compressed"].(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v == "true"
	}
	return false
}

// TestEvaluateHardBandNoEvictOverlapForCompressed: the hard band must
// NOT hand planEvict the IDs the same pass selected for compression
// (R-B4: compression precedes destruction; the worker reads rows
// asynchronously, an evict now would delete them first). NON-VACUITY:
// pre-fix both lanes selected from the same oldest-first pool and the
// overlap was complete; reverting the exclusion fails this test with
// the compress ids present in the evict batch.
func TestEvaluateHardBandNoEvictOverlapForCompressed(t *testing.T) {
	t.Parallel()
	byTier := map[string][]memory.MemoryResult{}
	for i := range 40 {
		byTier["working"] = append(byTier["working"],
			mem(fmt.Sprintf("m%02d", i), "tiny", 40-i))
	}
	e := New(enabledCfg(), Deps{Candidates: &stubCandidates{byTier: byTier}, Votes: stubVotes{}})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 95, Items: 40, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	var compress, evict *Action
	for i := range actions {
		switch actions[i].Kind {
		case KindCompress:
			compress = &actions[i]
		case KindEvict:
			evict = &actions[i]
		}
	}
	if compress == nil || evict == nil {
		t.Fatalf("want both band actions, got %+v", actions)
	}
	inCompress := map[string]bool{}
	for _, id := range compress.TargetIDs {
		inCompress[id] = true
	}
	for _, id := range evict.TargetIDs {
		if inCompress[id] {
			t.Fatalf("evict batch re-selects compress-target %q - same-pass data loss (A3-1)", id)
		}
	}
}

// TestEvaluateHardBandHarmfulStillEvicted: the compress-overlap
// exclusion never outranks a harmful vote (R-B2 evicts harmful
// regardless) - the harmful rows ARE the compression's problem, the
// engine must not protect them.
func TestEvaluateHardBandHarmfulStillEvicted(t *testing.T) {
	t.Parallel()
	byTier := map[string][]memory.MemoryResult{}
	for i := range 40 {
		id := fmt.Sprintf("m%02d", i)
		byTier["working"] = append(byTier["working"], mem(id, "tiny", 40-i))
	}
	e := New(enabledCfg(), Deps{
		Candidates: &stubCandidates{byTier: byTier},
		Votes:      stubVotes{"m00": -5, "m01": -5},
	})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 95, Items: 40, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	var evict *Action
	for i := range actions {
		if actions[i].Kind == KindEvict {
			evict = &actions[i]
		}
	}
	if evict == nil {
		t.Fatal("want evict action with harmful rows")
	}
	found := map[string]bool{}
	for _, id := range evict.TargetIDs {
		found[id] = true
	}
	if !found["m00"] || !found["m01"] {
		t.Fatalf("harmful ids must survive the compress-overlap exclusion, got %v", evict.TargetIDs)
	}
}

// TestCandidatesExcludeDemotedCopies: demotion's episodic copy (the
// row stamped demoted_from, A3-2) must never enter the
// compress/evict selection, or the engine destroys its own lossless
// copy after retention_days. NON-VACUITY: remove the
// excludeDemotedCopies call and the copy id appears in the evict
// batch.
func TestCandidatesExcludeDemotedCopies(t *testing.T) {
	t.Parallel()
	copyMem := mem("copy-1", "demoted claim lossless copy", 60)
	copyMem.Memory.Metadata = map[string]any{MetaDemotedFrom: "claim-1", "source_type": "claim"}
	byTier := map[string][]memory.MemoryResult{}
	byTier["working"] = append(byTier["working"], copyMem)
	for i := range 40 {
		byTier["working"] = append(byTier["working"],
			mem(fmt.Sprintf("m%02d", i), fmt.Sprintf("normal old row content %02d", i), 40-i))
	}
	e := New(enabledCfg(), Deps{Candidates: &stubCandidates{byTier: byTier}, Votes: stubVotes{}})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 95, Items: 41, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	for _, a := range actions {
		for _, id := range a.TargetIDs {
			if id == "copy-1" {
				t.Fatalf("demoted copy selected by %q action - A3-2 eviction of the lossless copy", a.Kind)
			}
		}
	}
	// Control: the pool minus the copy still plans normally.
	var planned int
	for _, a := range actions {
		planned += len(a.TargetIDs)
	}
	if planned == 0 {
		t.Fatal("expected band actions from the non-copy rows")
	}
}

// TestExecuteCompressFailureNotCounted: enqueue error -> count stays
// zero (D3(d): CompressionEnqueued counts ONLY successful enqueues).
func TestExecuteCompressFailureNotCounted(t *testing.T) {
	t.Parallel()
	enq := &stubEnqueuer{err: errors.New("queue full")}
	e := New(enabledCfg(), Deps{Enqueuer: enq})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindCompress, TargetIDs: []string{"a", "b"}, Reason: "tier=episodic"},
	})
	if err == nil {
		t.Fatal("want error")
	}
	if rep.CompressionEnqueued != 0 {
		t.Fatalf("failed enqueue counted %d, want 0", rep.CompressionEnqueued)
	}
	if rep.CompressRequested != 2 {
		t.Fatalf("requested = %d, want 2", rep.CompressRequested)
	}
}
