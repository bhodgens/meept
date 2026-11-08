package tierpolicy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/working"
)

// stubCandidates serves a fixed candidate list per tier, oldest first.
type stubCandidates struct {
	byTier map[string][]memory.MemoryResult
	calls  int
}

func (s *stubCandidates) Candidates(_ context.Context, tier string, limit int) ([]memory.MemoryResult, error) {
	s.calls++
	cands := s.byTier[tier]
	if limit < len(cands) {
		cands = cands[:limit]
	}
	return cands, nil
}

// stubVotes serves a fixed net-vote map.
type stubVotes map[string]int

func (v stubVotes) NetVotes(_ context.Context, ids []string) (map[string]int, error) {
	out := make(map[string]int, len(ids))
	for _, id := range ids {
		out[id] = v[id]
	}
	return out, nil
}

func mem(id string, content string, ageDays int) memory.MemoryResult {
	return memory.MemoryResult{
		Memory: memory.Memory{
			ID:        id,
			Content:   content,
			CreatedAt: time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour),
		},
		Source: "test",
	}
}

func enabledCfg() Config {
	return Config{
		WorkingConfig: working.Config{MaxTokens: 100, MaxItems: 200, TTL: 24 * time.Hour},
		Enabled:       true,
	}
}

// TestEvaluateNoopUnderCeiling: a tier at or under the compress
// threshold is the no-op sentinel - zero Deps calls (R-B5).
func TestEvaluateNoopUnderCeiling(t *testing.T) {
	cands := &stubCandidates{byTier: map[string][]memory.MemoryResult{
		"working": {mem("m1", "hello world content here", 1)},
	}}
	e := New(enabledCfg(), Deps{Candidates: cands})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 70, Items: 3, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("want 0 actions under compress threshold, got %d", len(actions))
	}
	if cands.calls != 0 {
		t.Fatalf("noop sentinel must make zero Deps calls, got %d", cands.calls)
	}
	rec, ok := e.LastPass("working")
	if !ok || rec.Outcome != OutcomeNoop {
		t.Fatalf("want noop watermark, got %+v ok=%v", rec, ok)
	}
}

// TestEvaluateBands: table-driven band coverage per the leaf contract
// (Tokens=95 Ceiling=100 Evict=0.9 Compress=0.8 -> compress band first,
// evict remainder, deterministic order).
func TestEvaluateBands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		tokens       int
		cands        int
		wantCompress bool
		wantEvict    bool
	}{
		// Candidates use 1-token content, so batch selections track the
		// R-B5 MinBatch floor (5) exactly.
		{name: "under compress", tokens: 80, cands: 0},
		{name: "middle band compress only", tokens: 85, cands: 10, wantCompress: true},
		{name: "hard band compress then evict", tokens: 95, cands: 20, wantCompress: true, wantEvict: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			byTier := map[string][]memory.MemoryResult{}
			for i := 0; i < tc.cands; i++ {
				byTier["working"] = append(byTier["working"],
					mem(fmt.Sprintf("m%02d", i), "tiny", tc.cands-i))
			}
			e := New(enabledCfg(), Deps{Candidates: &stubCandidates{byTier: byTier}, Votes: stubVotes{}})
			actions, err := e.Evaluate(context.Background(), []TierState{
				{Tier: "working", Tokens: tc.tokens, Items: tc.cands, Ceiling: 100},
			})
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			var gotCompress, gotEvict bool
			for i, a := range actions {
				switch a.Kind {
				case KindCompress:
					gotCompress = true
					if gotEvict {
						t.Fatalf("compress must precede evict (R-B4), action %d is compress after evict", i)
					}
				case KindEvict:
					gotEvict = true
				}
				if a.Reason == "" {
					t.Fatalf("action reason must be machine-readable, got empty")
				}
			}
			if gotCompress != tc.wantCompress || gotEvict != tc.wantEvict {
				t.Fatalf("tokens=%d: want compress=%v evict=%v, got compress=%v evict=%v (%v)",
					tc.tokens, tc.wantCompress, tc.wantEvict, gotCompress, gotEvict, actions)
			}
		})
	}
}

// TestEvaluateCompressBatchBounds: MaxBatch caps selection; under
// MinBatch the compress action is skipped (R-B5).
func TestEvaluateCompressBatchBounds(t *testing.T) {
	t.Parallel()
	byTier := map[string][]memory.MemoryResult{}
	for i := 0; i < 3; i++ { // 3 candidates < MinBatch 5
		byTier["working"] = append(byTier["working"], mem(fmt.Sprintf("m%d", i), "tiny", 10-i))
	}
	e := New(enabledCfg(), Deps{Candidates: &stubCandidates{byTier: byTier}, Votes: stubVotes{}})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 85, Items: 3, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("under-MinBatch compress must be skipped, got %v", actions)
	}

	// MaxBatch cap: 50 candidates, MaxBatch 10 -> exactly 10 IDs.
	byTier2 := map[string][]memory.MemoryResult{}
	for i := 0; i < 50; i++ {
		byTier2["working"] = append(byTier2["working"], mem(fmt.Sprintf("m%02d", i), "some content to compress", 50-i))
	}
	cfg := enabledCfg()
	e2 := New(cfg, Deps{Candidates: &stubCandidates{byTier: byTier2}, Votes: stubVotes{}})
	actions, err = e2.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 99, Items: 50, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("want at least one action")
	}
	if a := actions[0]; a.Kind == KindCompress && len(a.TargetIDs) != 10 {
		t.Fatalf("MaxBatch cap: want 10 compress targets, got %d", len(a.TargetIDs))
	}
}

// TestEvaluateEvictRanking: eviction selection reuses the
// PlanUsefulEviction shape - harmful IDs are evicted regardless of the
// token target (consolidation.go:551 pattern).
func TestEvaluateEvictRanking(t *testing.T) {
	t.Parallel()
	byTier := map[string][]memory.MemoryResult{
		"working": {
			mem("harm1", "harmful content padding", 1),
			mem("harm2", "harmful content padding", 2),
			mem("ok1", "fine content padding padding", 3),
			mem("ok2", "fine content padding padding", 4),
			mem("ok3", "fine content padding padding", 5),
			mem("ok4", "fine content padding padding", 6),
			mem("ok5", "fine content padding padding", 7),
		},
	}
	e := New(enabledCfg(), Deps{Candidates: &stubCandidates{byTier: byTier}, Votes: stubVotes{
		"harm1": -2, "harm2": -5, // at/below HarmfulVoteThreshold
	}})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 95, Items: 7, Ceiling: 100},
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
		t.Fatalf("want an evict action, got %v", actions)
	}
	found := map[string]bool{}
	for _, id := range evict.TargetIDs {
		found[id] = true
	}
	if !found["harm1"] || !found["harm2"] {
		t.Fatalf("harmful IDs must be evicted regardless of target size, got %v", evict.TargetIDs)
	}
}

// TestEvaluateDisabled: a disabled engine is fully inert.
func TestEvaluateDisabled(t *testing.T) {
	t.Parallel()
	cands := &stubCandidates{byTier: map[string][]memory.MemoryResult{}}
	e := New(Config{}, Deps{Candidates: cands})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 10000, Ceiling: 10},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if actions != nil {
		t.Fatalf("disabled engine must emit no actions, got %v", actions)
	}
	if cands.calls != 0 {
		t.Fatalf("disabled engine must make zero Deps calls, got %d", cands.calls)
	}
}

// TestEvaluateDeterministic: identical inputs yield identical action
// order (leaf contract: deterministic order).
func TestEvaluateDeterministic(t *testing.T) {
	t.Parallel()
	build := func() *stubCandidates {
		byTier := map[string][]memory.MemoryResult{}
		for i := 0; i < 30; i++ {
			byTier["working"] = append(byTier["working"],
				mem(fmt.Sprintf("m%02d", i), fmt.Sprintf("deterministic content %02d", i), 30-i))
		}
		return &stubCandidates{byTier: byTier}
	}
	states := []TierState{{Tier: "working", Tokens: 95, Items: 30, Ceiling: 100}}
	e1 := New(enabledCfg(), Deps{Candidates: build(), Votes: stubVotes{}})
	e2 := New(enabledCfg(), Deps{Candidates: build(), Votes: stubVotes{}})
	a1, err := e1.Evaluate(context.Background(), states)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	a2, err := e2.Evaluate(context.Background(), states)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(a1) != len(a2) {
		t.Fatalf("action count mismatch: %d vs %d", len(a1), len(a2))
	}
	for i := range a1 {
		if a1[i].Kind != a2[i].Kind || fmt.Sprint(a1[i].TargetIDs) != fmt.Sprint(a2[i].TargetIDs) {
			t.Fatalf("action %d diverged: %+v vs %+v", i, a1[i], a2[i])
		}
	}
}

// TestEvaluateCeilingFallback: a zero Ceiling falls back to the
// working-ring MaxTokens.
func TestEvaluateCeilingFallback(t *testing.T) {
	t.Parallel()
	e := New(enabledCfg(), Deps{}) // WorkingConfig.MaxTokens = 100
	// Tokens 95 of an implicit 100 ceiling = hard band; with no
	// candidates wired the pass records compress+evict outcomes as noop
	// but must NOT be the under-ceiling noop.
	_, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 95, Items: 0, Ceiling: 0},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	rec, ok := e.LastPass("working")
	if !ok {
		t.Fatal("want a watermark record")
	}
	if rec.Outcome == OutcomeNoop && rec.Tokens <= rec.Ceiling {
		t.Fatalf("zero ceiling should not make an over-band pass a noop: %+v", rec)
	}
}

// TestEvaluateTierOrder: tiers are processed in input order.
func TestEvaluateTierOrder(t *testing.T) {
	t.Parallel()
	byTier := map[string][]memory.MemoryResult{
		"a": {mem("a1", "content a one", 1), mem("a2", "content a two", 2),
			mem("a3", "content a three", 3), mem("a4", "content a four", 4),
			mem("a5", "content a five", 5)},
		"b": {mem("b1", "content b one", 1), mem("b2", "content b two", 2),
			mem("b3", "content b three", 3), mem("b4", "content b four", 4),
			mem("b5", "content b five", 5)},
	}
	e := New(enabledCfg(), Deps{Candidates: &stubCandidates{byTier: byTier}, Votes: stubVotes{}})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "b", Tokens: 95, Ceiling: 100},
		{Tier: "a", Tokens: 95, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	var tiers []string
	for _, a := range actions {
		tiers = append(tiers, sessionFromReason(a.Reason))
	}
	if len(tiers) < 2 {
		t.Fatalf("want actions from both tiers, got %v", actions)
	}
	if tiers[0] != "b" {
		t.Fatalf("tiers must be processed in input order, got %v", tiers)
	}
}

// TestEvaluateCandidateError surfaces candidate source failures.
func TestEvaluateCandidateError(t *testing.T) {
	t.Parallel()
	e := New(enabledCfg(), Deps{
		Candidates: failingCandidates{},
		Votes:      stubVotes{},
	})
	_, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 95, Ceiling: 100},
	})
	if err == nil {
		t.Fatal("want an error from a failing candidate source")
	}
	if !errors.Is(err, errStubFailure) {
		t.Fatalf("error should wrap the source failure, got %v", err)
	}
}

var errStubFailure = errors.New("stub failure")

type failingCandidates struct{}

func (failingCandidates) Candidates(context.Context, string, int) ([]memory.MemoryResult, error) {
	return nil, errStubFailure
}

// TestPlanEvictHonorsUsefulnessConfig pins bughunt config-tools F-2: the
// tier-policy eviction lane must honor the resolved [memory.usefulness]
// knob set instead of hardcoding Enabled+defaults.
//
//	Discrimination: candidate source order (oldest-first wiring) is
//	[A(10d, 0 votes), B..D(30d), E(50d)]. Ranked by usefulness, E scores
//	lowest (age penalty Ws/day) and leads the floor; the age-only lane
//	preserves source order, so A leads. Enabled=true -> E first;
//	Enabled=false -> A first.
func TestPlanEvictHonorsUsefulnessConfig(t *testing.T) {
	stub := map[string][]memory.MemoryResult{"episodic": {
		mem("A", "x", 10), mem("B", "x", 30), mem("C", "x", 30),
		mem("D", "x", 30), mem("E", "x", 50),
	}}
	votes := stubVotes{}
	st := TierState{Tier: "episodic", Tokens: 95, Items: 5, Ceiling: 100}

	firstEvict := func(t *testing.T, cfg Config, want string) {
		t.Helper()
		e := New(cfg, Deps{Candidates: &stubCandidates{byTier: stub}, Votes: votes})
		act, err := e.Evaluate(context.Background(), []TierState{st})
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		for i := range act {
			if act[i].Kind == KindEvict {
				if act[i].TargetIDs[0] != want {
					t.Errorf("evict first id = %q, want %q (full=%v)", act[i].TargetIDs[0], want, act[i].TargetIDs)
				}
				return
			}
		}
		t.Fatalf("no evict action in hard band (actions=%d)", len(act))
	}

	enabled := enabledCfg()
	enabled.Usefulness = memory.UsefulEvictionConfig{Enabled: true, FloorPct: 0.2, Weights: memory.DefaultWeights()}
	firstEvict(t, enabled, "E")

	dis := enabledCfg()
	dis.Usefulness = memory.UsefulEvictionConfig{Enabled: false, FloorPct: 0.05, Weights: memory.DefaultWeights()}
	firstEvict(t, dis, "A")

	// Zero-valued Usefulness (legacy unit engines + pre-F-2 field): the
	// documented ranked-default posture, byte-identical to the old code.
	firstEvict(t, enabledCfg(), "E")
}
