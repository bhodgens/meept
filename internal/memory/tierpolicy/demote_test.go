package tierpolicy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// claimRes builds a MemoryResult shaped like a semantic claim.
func claimRes(id string, content string, ageDays int, meta map[string]any) memory.MemoryResult {
	return memory.MemoryResult{
		Memory: memory.Memory{
			ID:        id,
			Content:   content,
			Type:      memory.MemoryTypeClaim,
			Category:  "claim",
			CreatedAt: time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour),
			Metadata:  meta,
		},
		Source: "claim",
	}
}

// stubClaimSource serves a fixed claim candidate list.
type stubClaimSource struct {
	claims []memory.MemoryResult
	err    error
}

func (s *stubClaimSource) Claims(_ context.Context, limit int) ([]memory.MemoryResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := s.claims
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

// stubApplier records the two-step demotion in exact call order (T2: the
// episodic copy must be logged BEFORE the flag write; no delete seam
// exists on DemotionApplier by construction).
type stubApplier struct {
	order   []string
	copied  []memory.Memory
	flags   []string
	copyErr error
	flagErr error
}

func (a *stubApplier) CopyToEpisodic(_ context.Context, claim memory.Memory) (string, error) {
	if a.copyErr != nil {
		return "", a.copyErr
	}
	a.order = append(a.order, "copy:"+claim.ID)
	a.copied = append(a.copied, claim)
	return "epi-" + claim.ID, nil
}

func (a *stubApplier) MarkDemoted(_ context.Context, claimID, reason string, _ time.Time) error {
	if a.flagErr != nil {
		return a.flagErr
	}
	a.order = append(a.order, "flag:"+claimID)
	a.flags = append(a.flags, claimID+":"+reason)
	return nil
}

// demoteTestEngine builds an enabled engine with the demotion seams wired.
func demoteTestEngine(claims *stubClaimSource, votes stubVotes, applier *stubApplier) *Engine {
	return New(Config{Enabled: true}, Deps{
		DemotionClaims: claims,
		Votes:          votes,
		Demotion:       applier,
	})
}

// TestPlanDemotionsStaleOnly: Given 3 claims (stale, fresh, already
// demoted); When PlanDemotions; Then exactly 1 action targeting the
// stale claim (leaf T1 Given/When/Then; R-B6 criterion 2; R-B7
// idempotent omission of the demoted one).
func TestPlanDemotionsStaleOnly(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("stale-1", "old claim", 90, nil),
		claimRes("fresh-1", "new claim", 1, nil),
		claimRes("demoted-1", "flagged claim", 90, map[string]any{MetaDemoted: true}),
	}}
	e := demoteTestEngine(claims, stubVotes{}, &stubApplier{})
	actions, err := e.PlanDemotions(context.Background(), nil)
	if err != nil {
		t.Fatalf("PlanDemotions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("want exactly 1 action, got %d: %+v", len(actions), actions)
	}
	a := actions[0]
	if a.Kind != KindDemote {
		t.Fatalf("want kind demote, got %q", a.Kind)
	}
	if len(a.TargetIDs) != 1 || a.TargetIDs[0] != "stale-1" {
		t.Fatalf("want target stale-1, got %v", a.TargetIDs)
	}
	if !strings.Contains(a.Reason, "reason="+DemoteReasonStale) {
		t.Fatalf("want stale reason, got %q", a.Reason)
	}
}

// TestPlanDemotionsLowUsefulness: a fresh claim with negative net votes
// scores below the usefulness floor and demotes with the
// low_usefulness reason (R-B6 criterion 3, scored by the SAME
// memory.Usefulness function the eviction ranking uses).
func TestPlanDemotionsLowUsefulness(t *testing.T) {
	t.Parallel()
	// ScoreCandidate: base 0.5 + Wv(-4 votes = -0.32) + Wa(0) - Ws(age).
	// age 1d:  0.5 - 0.32 - 0.005 = 0.175 (above the 0.1 floor).
	// age 5d:  0.5 - 0.32 - 0.025 = 0.155... also above; use 10d:
	// age 10d: 0.5 - 0.32 - 0.05  = 0.13 (still above).
	// Young ages keep staleness (R-B6 priority 2, >30d) out of the way
	// so this test isolates the usefulness criterion (priority 3).
	// A deeply voted-down old claim: -6 votes, age 40d also demotes but
	// via staleness first - not this test's concern.
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("voted-mid", "kept", 1, nil),
		claimRes("voted-down", "demote me", 10, nil),
	}}
	e := demoteTestEngine(claims, stubVotes{"voted-mid": -2, "voted-down": -5}, &stubApplier{})
	actions, err := e.PlanDemotions(context.Background(), nil)
	if err != nil {
		t.Fatalf("PlanDemotions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("want 1 low-usefulness action, got %d: %+v", len(actions), actions)
	}
	if actions[0].TargetIDs[0] != "voted-down" {
		t.Fatalf("want voted-down, got %v", actions[0].TargetIDs)
	}
	if !strings.Contains(actions[0].Reason, DemoteReasonLowUsefulness) {
		t.Fatalf("want low_usefulness reason, got %q", actions[0].Reason)
	}
}

// TestPlanDemotionsHarmfulYoungStays: a YOUNG claim with only mildly
// negative votes stays above the usefulness floor and is not planned -
// demotion is criteria-driven, not a catch-all for unpopular claims.
// (Genuinely harmful claims are eviction business via
// PlanUsefulEviction's harmful set; a stale harmful claim still demotes
// first because staleness is R-B6 priority 2 and demotion preserves
// content the eviction ranking may yet want to see.)
func TestPlanDemotionsHarmfulYoungStays(t *testing.T) {
	t.Parallel()
	// age 1d, -2 votes: score = 0.5 + 0.08*(-2) - 0.005*1 = 0.335, above floor.
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("young-neg", "mildly disliked", 1, nil),
	}}
	e := demoteTestEngine(claims, stubVotes{"young-neg": memory.HarmfulVoteThreshold}, &stubApplier{})
	actions, err := e.PlanDemotions(context.Background(), nil)
	if err != nil {
		t.Fatalf("PlanDemotions: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("young mildly-negative claim must not demote, got %+v", actions)
	}
}

// TestPlanDemotionsIDFilter: an explicit semanticIDs list restricts the
// pool; IDs outside it are never planned.
func TestPlanDemotionsIDFilter(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("in-list", "stale", 90, nil),
		claimRes("out-list", "stale too", 90, nil),
	}}
	e := demoteTestEngine(claims, stubVotes{}, &stubApplier{})
	actions, err := e.PlanDemotions(context.Background(), []string{"in-list"})
	if err != nil {
		t.Fatalf("PlanDemotions: %v", err)
	}
	if len(actions) != 1 || actions[0].TargetIDs[0] != "in-list" {
		t.Fatalf("want only in-list planned, got %+v", actions)
	}
}

// TestPlanDemotionsUnwiredIsNoop: nil demotion seams plan nothing and
// error nothing (honest degradation, C5 posture).
func TestPlanDemotionsUnwiredIsNoop(t *testing.T) {
	t.Parallel()
	e := New(Config{Enabled: true}, Deps{})
	actions, err := e.PlanDemotions(context.Background(), nil)
	if err != nil {
		t.Fatalf("unwired PlanDemotions must not error: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("unwired PlanDemotions must plan nothing, got %+v", actions)
	}
	// Disabled engines likewise.
	claims := &stubClaimSource{claims: []memory.MemoryResult{claimRes("x", "y", 90, nil)}}
	e2 := New(Config{}, Deps{DemotionClaims: claims, Demotion: &stubApplier{}})
	actions, err = e2.PlanDemotions(context.Background(), nil)
	if err != nil || len(actions) != 0 {
		t.Fatalf("disabled engine must plan nothing: %d actions, err %v", len(actions), err)
	}
}

// TestPlanDemotionsNonClaimsSkipped: non-claim memories in the pool are
// never planned (the demotion direction is semantic CLAIM -> episodic).
func TestPlanDemotionsNonClaimsSkipped(t *testing.T) {
	t.Parallel()
	old := claimRes("epi-1", "plain episodic row", 90, nil)
	old.Memory.Type = memory.MemoryTypeEpisodic
	claims := &stubClaimSource{claims: []memory.MemoryResult{old}}
	e := demoteTestEngine(claims, stubVotes{}, &stubApplier{})
	actions, err := e.PlanDemotions(context.Background(), nil)
	if err != nil {
		t.Fatalf("PlanDemotions: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("non-claims must not demote, got %+v", actions)
	}
}

// TestDemotionThresholdDefaults: zero-value config resolves to the
// documented defaults (leaf T3: 30d + the 0.1 usefulness floor).
func TestDemotionThresholdDefaults(t *testing.T) {
	t.Parallel()
	th := ResolveDemotionThresholds(DemotionConfig{})
	if th.StaleDays != DefaultDemotionStaleDays {
		t.Fatalf("stale days default: want %d, got %d", DefaultDemotionStaleDays, th.StaleDays)
	}
	if th.StaleDays != 30 {
		t.Fatalf("stale days must be the documented 30, got %d", th.StaleDays)
	}
	if th.MinUsefulness != DefaultDemotionMinUsefulness {
		t.Fatalf("min usefulness default: want %v, got %v", DefaultDemotionMinUsefulness, th.MinUsefulness)
	}
	// Negative values resolve to defaults too ("negative rejected at
	// resolve").
	th = ResolveDemotionThresholds(DemotionConfig{StaleDays: -5, MinUsefulness: -1})
	if th.StaleDays != 30 || th.MinUsefulness != DefaultDemotionMinUsefulness {
		t.Fatalf("negative config must resolve to defaults, got %+v", th)
	}
	// Explicit values win.
	th = ResolveDemotionThresholds(DemotionConfig{StaleDays: 7, MinUsefulness: 0.3})
	if th.StaleDays != 7 || th.MinUsefulness != 0.3 {
		t.Fatalf("explicit thresholds must win, got %+v", th)
	}
	// Out-of-range usefulness clamps to the default.
	th = ResolveDemotionThresholds(DemotionConfig{MinUsefulness: 1.5})
	if th.MinUsefulness != DefaultDemotionMinUsefulness {
		t.Fatalf("out-of-range usefulness must resolve to default, got %v", th.MinUsefulness)
	}
}

// TestEngineDemotionConfigResolved: New resolves the Demotion block from
// the engine Config (defaults applied at construction).
func TestEngineDemotionConfigResolved(t *testing.T) {
	t.Parallel()
	e := New(Config{Enabled: true, Demotion: DemotionConfig{StaleDays: 14}}, Deps{})
	th := ResolveDemotionThresholds(e.cfg.Demotion)
	if th.StaleDays != 14 {
		t.Fatalf("explicit stale days must survive, got %d", th.StaleDays)
	}
	if th.MinUsefulness != DefaultDemotionMinUsefulness {
		t.Fatalf("unspecified usefulness must default, got %v", th.MinUsefulness)
	}
}
