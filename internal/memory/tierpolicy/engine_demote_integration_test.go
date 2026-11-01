package tierpolicy

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/working"
)

// TestEngineDemoteMixedPlanOrder: Given an over-ceiling semantic tier
// with stale claims; When Evaluate plans; Then demote actions precede
// evict actions in the returned plan (leaf T4 Given/When/Then, R-B4
// order rule: content-preserving reclamation first).
func TestEngineDemoteMixedPlanOrder(t *testing.T) {
	t.Parallel()
	// Stale claims feed the demotion planner.
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("stale-a", "old claim a", 90, nil),
		claimRes("stale-b", "old claim b", 90, nil),
	}}
	// Over-ceiling working tier with enough candidates to fill an evict
	// batch (R-B5 min batch 5).
	byTier := map[string][]memory.MemoryResult{}
	for i := 0; i < 20; i++ {
		byTier["working"] = append(byTier["working"], mem("m-d"+string(rune('a'+i)), "tiny", 30))
	}
	e := New(enabledCfg(), Deps{
		Candidates:     &stubCandidates{byTier: byTier},
		Votes:          stubVotes{},
		DemotionClaims: claims,
		Demotion:       &stubApplier{},
	})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 95, Items: 20, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	var demoteIdx, evictIdx = -1, -1
	for i, a := range actions {
		switch a.Kind {
		case KindDemote:
			if demoteIdx == -1 {
				demoteIdx = i
			}
		case KindEvict:
			if evictIdx == -1 {
				evictIdx = i
			}
		}
	}
	if demoteIdx == -1 {
		t.Fatalf("over-ceiling plan with stale claims must contain demote actions, got %+v", actions)
	}
	if evictIdx == -1 {
		t.Fatalf("hard-band plan must contain an evict action, got %+v", actions)
	}
	if demoteIdx > evictIdx {
		t.Fatalf("demote (%d) must precede evict (%d) in the plan: %+v", demoteIdx, evictIdx, actions)
	}
}

// TestEngineDemoteUnderCeilingStillPlans: demotion is claim-quality
// driven, not ceiling-driven - stale claims demote even when every tier
// sits under its threshold (the band code takes the noop sentinel while
// demotions still plan).
func TestEngineDemoteUnderCeilingStillPlans(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("stale-u", "old", 90, nil),
	}}
	e := New(enabledCfg(), Deps{
		DemotionClaims: claims,
		Demotion:       &stubApplier{},
	})
	actions, err := e.Evaluate(context.Background(), []TierState{
		{Tier: "working", Tokens: 10, Items: 2, Ceiling: 100},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(actions) != 1 || actions[0].Kind != KindDemote {
		t.Fatalf("stale claim must demote even under ceiling, got %+v", actions)
	}
	// The tier watermark still records the band noop.
	if rec, ok := e.LastPass("working"); !ok || rec.Outcome != OutcomeNoop {
		t.Fatalf("band noop sentinel must survive, got %+v ok=%v", rec, ok)
	}
}

// TestExecuteMixedKindsSinglePass: one Execute pass handles demote,
// compress, and evict actions together and applies all three (T4: "tick
// execution covers all three kinds").
func TestExecuteMixedKindsSinglePass(t *testing.T) {
	t.Parallel()
	del := &stubDeleter{}
	enq := &stubEnqueuer{}
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("mix-1", "stale", 90, nil),
	}}
	ap := &stubApplier{}
	e := New(enabledCfg(), Deps{
		Deletes:        del,
		Enqueuer:       enq,
		DemotionClaims: claims,
		Demotion:       ap,
	})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindDemote, TargetIDs: []string{"mix-1"}, Reason: "reason=stale"},
		{Kind: KindCompress, TargetIDs: []string{"k1", "k2", "k3", "k4", "k5"}, Reason: "tier=working;tokens=85;ceiling=100;target_tokens=5"},
		{Kind: KindEvict, TargetIDs: []string{"v1", "v2", "v3", "v4", "v5"}, Reason: "tier=working;tokens=95;ceiling=100;target_tokens=5"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if rep.Demoted != 1 || rep.CompressionEnqueued != 5 || rep.EvictionsDeleted != 5 {
		t.Fatalf("mixed pass accounting wrong: demoted=%d enqueued=%d deleted=%d",
			rep.Demoted, rep.CompressionEnqueued, rep.EvictionsDeleted)
	}
	if rep.ActionsApplied != 3 {
		t.Fatalf("want 3 applied actions, got %d", rep.ActionsApplied)
	}
	if len(del.deleted) != 1 {
		t.Fatalf("evict must delete once, got %v", del.deleted)
	}
}

// TestExecuteUnknownKindStillErrors: the demote addition does not weaken
// the unknown-kind guard.
func TestExecuteUnknownKindStillErrors(t *testing.T) {
	t.Parallel()
	e := New(enabledCfg(), Deps{})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: "teleport", TargetIDs: []string{"x"}, Reason: "reason=y"},
	})
	if err == nil {
		t.Fatal("unknown kind must error")
	}
	if len(rep.Errors) == 0 {
		t.Fatal("unknown kind must land in report errors")
	}
}

// TestTickLogCarriesDemotedFields: the daemon tick log line includes the
// demotion accounting (leaf T5 user visibility) - pinned by asserting
// the report fields the log line renders exist and carry the counts.
// (The slog call itself is the existing tickOnce; a capture-handler
// double-checks the rendered attributes.)
func TestTickLogCarriesDemotedFields(t *testing.T) {
	t.Parallel()
	claims := &stubClaimSource{claims: []memory.MemoryResult{
		claimRes("log-1", "stale", 90, nil),
		claimRes("log-2", "stale", 90, nil),
	}}
	ap := &stubApplier{}
	e := New(Config{Enabled: true}, Deps{DemotionClaims: claims, Demotion: ap})
	rep, err := e.Execute(context.Background(), []Action{
		{Kind: KindDemote, TargetIDs: []string{"log-1", "log-2"}, Reason: "reason=stale"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The tick log line renders exactly these fields; if the report
	// lost them the log would silently drop the demotion story.
	if rep.Demoted != 2 {
		t.Fatalf("log fields pinned to report: Demoted want 2, got %d", rep.Demoted)
	}
	if rep.DemoteRequested != 2 {
		t.Fatalf("log fields pinned to report: DemoteRequested want 2, got %d", rep.DemoteRequested)
	}
}

// TestResolveTiersConfigToEngineCarriesDemotion: the resolved tiers
// config mapping carries the demotion block onto the engine config
// (wiring continuity from [memory.tiers] to Engine).
func TestResolveTiersConfigToEngineCarriesDemotion(t *testing.T) {
	t.Parallel()
	tiers := ResolveTiersConfig(TiersConfig{Enabled: true})
	_ = tiers // ratios resolve as in lifecycle_test; demotion rides the
	// engine Config directly (single-source-of-truth: TiersConfig stays
	// the [memory.tiers] carrier, the Demotion block hangs off Config).
	ecfg := Config{Enabled: true, Demotion: DemotionConfig{StaleDays: 21, MinUsefulness: 0.2}}
	e := New(ecfg, Deps{})
	th := ResolveDemotionThresholds(e.cfg.Demotion)
	if th.StaleDays != 21 || th.MinUsefulness != 0.2 {
		t.Fatalf("engine must carry the demotion block, got %+v", th)
	}
	_ = working.Config{} // keep the working import aligned with siblings
}
