package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// ---- Scoring math (table incl. clamps) ----

func TestUsefulness_Table(t *testing.T) {
	def := DefaultWeights()
	cases := []struct {
		name      string
		votes     int
		accesses  int
		ageDays   float64
		w         Weights
		want      float64
		wantExact bool // require exact float equality
	}{
		{name: "zero-everything is base", votes: 0, accesses: 0, ageDays: 0, w: def, want: 0.5, wantExact: true},
		{name: "one positive vote", votes: 1, accesses: 0, ageDays: 0, w: def, want: 0.58, wantExact: true},
		{name: "accesses log1p", votes: 0, accesses: 9, ageDays: 0, w: def, want: 0.5 + 0.05*2.1972245773362196, wantExact: true},
		{name: "age penalty linear", votes: 0, accesses: 0, ageDays: 10, w: def, want: 0.45, wantExact: true},
		{name: "clamps at zero on heavy harm", votes: -100, accesses: 0, ageDays: 100, w: def, want: 0},
		{name: "clamps at one on praise flood", votes: 100, accesses: 1000, ageDays: 0, w: def, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Usefulness(tc.votes, tc.accesses, tc.ageDays, tc.w)
			if diff := got - tc.want; diff > 0.01 || diff < -0.01 {
				t.Fatalf("Usefulness(%d,%d,%v) = %v, want ~%v", tc.votes, tc.accesses, tc.ageDays, got, tc.want)
			}
			if got < 0 || got > 1 {
				t.Fatalf("score %v outside [0,1]", got)
			}
		})
	}
}

func TestUsefulness_ClampBounds(t *testing.T) {
	w := DefaultWeights()
	if got := Usefulness(-1000, 0, 0, w); got != 0 {
		t.Fatalf("negative clamp: got %v", got)
	}
	if got := Usefulness(100000, 0, 0, w); got != 1 {
		t.Fatalf("positive clamp: got %v", got)
	}
}

// ---- Vote persistence roundtrip via Manager + real SQLite ----

func newVoteTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	m := NewManager(ManagerConfig{
		Config: config.MemoryConfig{
			DataDir:  dir,
			Episodic: config.EpisodicConfig{Enabled: true},
			Task:     config.TaskMemoryConfig{Enabled: true},
		},
		Logger: slog.Default(),
	})
	if err := m.Initialize(context.Background()); err != nil {
		t.Fatalf("manager init: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestRecordVote_Roundtrip(t *testing.T) {
	m := newVoteTestManager(t)
	ctx := context.Background()

	id, err := m.Store(ctx, Memory{Content: "roundtrip me", Type: MemoryTypeTask, Category: "code"})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	if err := m.RecordVote(id, 1, "helped debug"); err != nil {
		t.Fatalf("record vote: %v", err)
	}
	if err := m.RecordVote(id, -1, ""); err != nil {
		t.Fatalf("record second vote: %v", err)
	}

	net, err := m.NetVotes(ctx, []string{id})
	if err != nil {
		t.Fatalf("net votes: %v", err)
	}
	if net[id] != 0 {
		t.Fatalf("net = %d, want 0 (+1 then -1)", net[id])
	}

	recs, err := m.votes.VotesFor(ctx, id)
	if err != nil {
		t.Fatalf("votes for: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].Reason != "helped debug" {
		t.Errorf("reason roundtrip: %q", recs[0].Reason)
	}
	if recs[0].CreatedAt.IsZero() {
		t.Errorf("CreatedAt not persisted")
	}
}

func TestRecordVote_ValidationAndReasonCap(t *testing.T) {
	m := newVoteTestManager(t)
	ctx := context.Background()
	id, _ := m.Store(ctx, Memory{Content: "cap test", Type: MemoryTypeTask})

	if err := m.RecordVote(id, 2, ""); err == nil {
		t.Error("delta=2 should error")
	}
	if err := m.RecordVote(id, 0, ""); err == nil {
		t.Error("delta=0 should error")
	}
	long := ""
	for i := 0; i < 600; i++ {
		long += "x"
	}
	if err := m.RecordVote(id, 1, long); err != nil {
		t.Fatalf("long reason rejected: %v", err)
	}
	recs, err := m.votes.VotesFor(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs[0].Reason) > MaxReasonBytes {
		t.Fatalf("reason not capped: %d bytes", len(recs[0].Reason))
	}
	if err := m.RecordVote("no-such-id", 1, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing memory should be ErrNotFound, got %v", err)
	}
}

func TestNetVotes_BatchedNoNPlusOne(t *testing.T) {
	vs, err := newVoteStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer vs.Close()
	ctx := context.Background()
	var ids []string
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("mem-%d", i)
		ids = append(ids, id)
		if err := vs.Insert(VoteRecord{MemoryID: id, Delta: 1, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if err := vs.Insert(VoteRecord{MemoryID: id, Delta: -1, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	net, err := vs.NetVotes(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(net) != 50 {
		t.Fatalf("net map size %d, want 50", len(net))
	}
	for _, id := range ids {
		if net[id] != 0 {
			t.Fatalf("%s net = %d, want 0", id, net[id])
		}
	}
}

// ---- Eviction ordering with flag ON ----

func usefulTestWeights() Weights { return Weights{Base: 0.5, Wv: 0.08, Wa: 0.05, Ws: 0.005} }

func TestPlanUsefulEviction_HighVoteSurvivesNewerLowVote(t *testing.T) {
	now := time.Now()
	oldHighVote := MemoryResult{Memory: Memory{ID: "old-good", CreatedAt: now.Add(-90 * 24 * time.Hour)}}
	newLowVote := MemoryResult{Memory: Memory{ID: "new-bad", CreatedAt: now.Add(-1 * time.Hour)}}
	cands := []MemoryResult{newLowVote, oldHighVote}
	net := map[string]int{"old-good": 10, "new-bad": -1} // new-bad below floor

	cfg := UsefulEvictionConfig{Enabled: true, FloorPct: 0.5, Weights: usefulTestWeights()}
	plan := PlanUsefulEviction(cands, net, now, cfg)

	if plan.Survivors[0].Memory.ID != "old-good" {
		t.Fatalf("high-vote survivor must rank first, got %+v", plan.Survivors)
	}
	if len(plan.Floor) != 1 || plan.Floor[0] != "new-bad" {
		t.Fatalf("floor should contain new-bad, got %v", plan.Floor)
	}
}

func TestPlanUsefulEviction_HarmfulEvictedRegardlessOfAge(t *testing.T) {
	now := time.Now()
	freshHarmful := MemoryResult{Memory: Memory{ID: "fresh-harmful", CreatedAt: now}} // brand new
	okMem := MemoryResult{Memory: Memory{ID: "ok", CreatedAt: now}}
	cands := []MemoryResult{freshHarmful, okMem}
	net := map[string]int{"fresh-harmful": -2, "ok": 0}

	cfg := UsefulEvictionConfig{Enabled: true, FloorPct: 0.05, Weights: usefulTestWeights()}
	plan := PlanUsefulEviction(cands, net, now, cfg)

	found := false
	for _, id := range plan.Harmful {
		if id == "fresh-harmful" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fresh harmful memory must evict regardless of age; plan=%+v", plan)
	}
	if len(plan.Survivors) != 1 || plan.Survivors[0].Memory.ID != "ok" {
		t.Fatalf("survivors wrong: %+v", plan.Survivors)
	}
}

func TestPlanUsefulEviction_FloorPercentileBeforeAgeRules(t *testing.T) {
	now := time.Now()
	// All memories are young (age rules would keep them), but floor pct
	// still evicts the lowest-scoring slice.
	var cands []MemoryResult
	net := map[string]int{}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("m%02d", i)
		cands = append(cands, MemoryResult{Memory: Memory{ID: id, CreatedAt: now.Add(time.Duration(i) * time.Minute)}})
		net[id] = i // ascending usefulness, none harmful
	}
	cfg := UsefulEvictionConfig{Enabled: true, FloorPct: 0.05, Weights: usefulTestWeights()}
	plan := PlanUsefulEviction(cands, net, now, cfg)

	if len(plan.Floor) != 1 {
		t.Fatalf("floor count = %d, want 1 (5%% of 20)", len(plan.Floor))
	}
	if plan.Floor[0] != "m00" {
		t.Fatalf("lowest-scored m00 should be floored, got %s", plan.Floor[0])
	}
}

func TestPlanUsefulEviction_NeverFloorsEverything(t *testing.T) {
	now := time.Now()
	cands := []MemoryResult{{Memory: Memory{ID: "only", CreatedAt: now}}}
	cfg := UsefulEvictionConfig{Enabled: true, FloorPct: 0.99, Weights: usefulTestWeights()}
	plan := PlanUsefulEviction(cands, map[string]int{}, now, cfg)
	if len(plan.Floor) != 0 || len(plan.Survivors) != 1 {
		t.Fatalf("single candidate must survive: %+v", plan)
	}
}

// Flag-off path: legacy consolidation behavior unchanged.
func TestConsolidateEpisodic_FlagOff_LegacyBehaviorUnchanged(t *testing.T) {
	backend := &recordingBackend{
		memories: makeMemories(4, time.Now().Add(-72*time.Hour)),
	}
	c := NewConsolidator(ConsolidatorConfig{
		Manager: &Manager{config: config.MemoryConfig{Usefulness: config.MemoryUsefulnessConfig{Enabled: false}}},
		Backend: backend,
		Logger:  slog.Default(),
	})
	report, err := c.consolidateEpisodic(context.Background(), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("consolidateEpisodic: %v", err)
	}
	if report.archived != 4 || report.created != 1 {
		t.Fatalf("legacy behavior changed: archived=%d created=%d", report.archived, report.created)
	}
	if len(backend.deletedIDs) != 4 {
		t.Fatalf("flag-off must use legacy delete set, got %v", backend.deletedIDs)
	}
	// Flag-off: exactly one (legacy) delete batch — no early usefulness batch.
	if backend.batchNum != 1 {
		t.Fatalf("flag-off must perform a single legacy delete batch, got %d", backend.batchNum)
	}
}

func TestConsolidateEpisodic_FlagOn_HarmfulAndFloorEvictFirst(t *testing.T) {
	now := time.Now()
	old := now.Add(-60 * 24 * time.Hour)
	mems := []MemoryResult{
		{Memory: Memory{ID: "harmful", Content: "bad advice", CreatedAt: old}},
		{Memory: Memory{ID: "good-old", Content: "great fact", CreatedAt: old}},
		{Memory: Memory{ID: "mediocre-1", Content: "filler alpha", CreatedAt: old}},
		{Memory: Memory{ID: "mediocre-2", Content: "filler beta", CreatedAt: old}},
	}
	backend := &recordingBackend{memories: mems}
	mgr := &Manager{config: config.MemoryConfig{
		DataDir: t.TempDir(),
		Usefulness: config.MemoryUsefulnessConfig{
			Enabled:  true,
			FloorPct: 0.25,
			Base:     0.5, Wv: 0.08, Wa: 0.05, Ws: 0.005,
		},
	}}
	c := NewConsolidator(ConsolidatorConfig{Manager: mgr, Backend: backend, Logger: slog.Default()})

	// Preload votes directly into the manager's store.
	if err := mgr.initVoteStore(); err != nil {
		t.Fatalf("vote store unavailable in unit harness: %v", err)
	}
	ctx := context.Background()
	if err := mgr.votes.Insert(VoteRecord{MemoryID: "harmful", Delta: -1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.votes.Insert(VoteRecord{MemoryID: "harmful", Delta: -1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.votes.Insert(VoteRecord{MemoryID: "good-old", Delta: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	if _, err := c.consolidateEpisodic(ctx, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("consolidateEpisodic: %v", err)
	}
	if backend.batchNum < 2 {
		t.Fatalf("flag-on must perform an early usefulness eviction batch before legacy deletes")
	}
	sawHarmful := false
	for _, id := range backend.firstDeleteBatch {
		if id == "harmful" {
			sawHarmful = true
		}
	}
	if !sawHarmful {
		t.Fatalf("harmful memory must be in the first eviction batch, got %v", backend.firstDeleteBatch)
	}
}

// recordingBackend captures deletion order to assert eviction sequencing.
type recordingBackend struct {
	memories          []MemoryResult
	deletedIDs        []string
	firstDeleteBatch  []string
	usefulnessDeletes int
	batchNum          int
}

func (b *recordingBackend) GetOldMemories(_ context.Context, olderThan time.Time, limit int) ([]MemoryResult, error) {
	var out []MemoryResult
	for _, m := range b.memories {
		if m.Memory.CreatedAt.Before(olderThan) {
			out = append(out, m)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (b *recordingBackend) GetExpiredMemories(_ context.Context, _ int) ([]Memory, error) {
	return nil, errors.New("not implemented")
}

func (b *recordingBackend) StoreSummary(_ context.Context, content, category string, metadata map[string]any) (string, error) {
	return "summary-" + category, nil
}

func (b *recordingBackend) DeleteByIDs(_ context.Context, ids []string) (int, error) {
	b.batchNum++
	deleted := make(map[string]bool, len(ids))
	for _, id := range ids {
		deleted[id] = true
		b.deletedIDs = append(b.deletedIDs, id)
	}
	if b.batchNum == 1 {
		b.firstDeleteBatch = append([]string{}, ids...)
		b.usefulnessDeletes = len(ids)
	}
	var remaining []MemoryResult
	for _, m := range b.memories {
		if !deleted[m.Memory.ID] {
			remaining = append(remaining, m)
		}
	}
	b.memories = remaining
	return len(ids), nil
}

func (b *recordingBackend) FindDuplicates(_ context.Context, _ int) ([][]string, error) {
	return nil, nil
}

func (b *recordingBackend) StoreExpiredSummary(_ context.Context, _ Memory, _ string) (string, error) {
	return "", errors.New("not implemented")
}

func (b *recordingBackend) DeleteSingle(_ context.Context, _ string) error {
	return errors.New("not implemented")
}

// Config plumbing defaults.
func TestResolveUsefulEviction_Defaults(t *testing.T) {
	r := ResolveUsefulEviction(config.MemoryUsefulnessConfig{})
	if r.Enabled {
		t.Error("usefulness eviction must default OFF")
	}
	if r.FloorPct != 0.05 {
		t.Errorf("FloorPct default = %v, want 0.05", r.FloorPct)
	}
	if r.Weights != DefaultWeights() {
		t.Errorf("weights default mismatch: %+v", r.Weights)
	}
	r2 := ResolveUsefulEviction(config.MemoryUsefulnessConfig{Enabled: true, FloorPct: 0.2, Base: 0.6})
	if !r2.Enabled || r2.FloorPct != 0.2 || r2.Weights.Base != 0.6 {
		t.Errorf("overrides ignored: %+v", r2)
	}
}

// Vote store standalone fallback path.
func TestVoteStore_StandaloneFallback(t *testing.T) {
	dir := t.TempDir()
	vs, err := newVoteStore(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := vs.Insert(VoteRecord{MemoryID: "y", Delta: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	net, err := vs.NetVotes(ctx, []string{"y"})
	if err != nil {
		t.Fatal(err)
	}
	if net["y"] != 1 {
		t.Fatalf("net = %d, want 1", net["y"])
	}
	if filepath.Base(vs.path) != "votes.db" {
		t.Errorf("unexpected db path %q", vs.path)
	}
	if err := vs.Insert(VoteRecord{MemoryID: "x", Delta: -1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := vs.Close(); err != nil {
		t.Fatal(err)
	}
}
