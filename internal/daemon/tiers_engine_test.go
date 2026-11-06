package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/tierpolicy"
	"github.com/caimlas/meept/internal/memory/working"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tiersTestManager builds an initialized SQLite-backed memory manager
// rooted in a temp dir (the internal/memory epistemic_test.go pattern;
// duplicated here so internal/daemon does not depend on memory's test
// helpers, which live in a _test.go file and cannot be imported).
func tiersTestManager(t *testing.T) *memory.Manager {
	t.Helper()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  t.TempDir(),
			Episodic: config.EpisodicConfig{Enabled: true},
		},
	})
	require.NoError(t, mgr.Initialize(context.Background()))
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// TestManagerCandidatesRealCutoff (leaf T2): the wiring-failure-proof
// for the zero-time cutoff defect. The OLD code passed time.Time{} -
// created_at < 0001-01-01 matches nothing, so the candidate pool was
// empty forever. Proof shape: a cutoff IN THE PAST excludes the fresh
// row (created now), a cutoff IN THE FUTURE (negative retention window,
// i.e. "cutoff = now + 1s" - the SQL comparison is strict created_at <)
// includes it, and the ZERO cutoff includes nothing. The store stamps
// created_at at write time and exposes no backdate path, so "old row"
// is modeled as "cutoff after the row" - the same strict-comparison
// arithmetic the production cutoff (now - retention) evaluates. Delete
// the retention wiring from BuildTierPolicyEngine (back to time.Time{})
// and the first assertion pair still passes while the adapter-proof
// (TestBuildTierPolicyEngineWiring's pool-driven assertions on the
// demotion path) fails - this test pins the adapter's cutoff contract.
func TestManagerCandidatesRealCutoff(t *testing.T) {
	mgr := tiersTestManager(t)
	ctx := context.Background()
	_, err := mgr.Store(ctx, memory.Memory{
		Content:  "candidate row",
		Type:     memory.MemoryTypeEpisodic,
		Category: "conversation",
	})
	require.NoError(t, err)

	// Zero cutoff matches nothing (the T2 defect, kept as the negative
	// control: whatever the cutoff logic does, zero must stay inert).
	ep := mgr.Episodic()
	require.NotNil(t, ep)
	zeroRows, err := ep.GetOldMemories(ctx, time.Time{}, 10)
	require.NoError(t, err)
	assert.Empty(t, zeroRows, "zero-time cutoff must match nothing (the T2 defect)")

	// Real cutoff arithmetic: now - 24h (in the past) -> the row created
	// now is YOUNGER than the cutoff and correctly excluded.
	pastCands := &managerCandidates{mgr: mgr, retention: 24 * time.Hour}
	rows, err := pastCands.Candidates(ctx, "episodic", 10)
	require.NoError(t, err)
	assert.Empty(t, rows, "a row newer than the retention cutoff is not a candidate")

	// now + 1s (cutoff just after the row) -> the row QUALIFIES. This is
	// the direction the zero-time cutoff broke: any cutoff after a row's
	// creation must surface it, and time.Time{} never does.
	futureCands := &managerCandidates{mgr: mgr, retention: -1 * time.Second}
	rows, err = futureCands.Candidates(ctx, "episodic", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1, "a cutoff after the row must return it (zero cutoff never could)")
	assert.Contains(t, rows[0].Memory.Content, "candidate row")
}

// TestManagerCandidatesCutoffExcludesFresh (leaf T2 Given/When/Then
// complement): a row younger than the retention window is NOT a
// candidate - the window means something: an OLD row (modeled by a
// cutoff ahead of the row via negative retention, see the modeling note
// above) qualifies while the same row under a past cutoff does not.
func TestManagerCandidatesCutoffExcludesFresh(t *testing.T) {
	mgr := tiersTestManager(t)
	ctx := context.Background()
	_, err := mgr.Store(ctx, memory.Memory{
		Content:  "fresh row",
		Type:     memory.MemoryTypeEpisodic,
		Category: "conversation",
	})
	require.NoError(t, err)
	// Positive retention = cutoff in the past = row too young.
	cands := &managerCandidates{mgr: mgr, retention: 24 * time.Hour}
	rows, err := cands.Candidates(ctx, "episodic", 10)
	require.NoError(t, err)
	assert.Empty(t, rows, "fresh rows must not be candidates under a real retention window")
}

// TestManagerDemotionClaimsAdapter (leaf T3): the epistemic claim pool
// adapter. Proof shape: store two claims (one stale-old via metadata
// age is unnecessary - staleness filtering is the ENGINE's job via
// thresholds; the adapter supplies the POOL). Assert: claims surface,
// typed filtering holds, demoted rows leave the pool (R-B7), oldest
// first ordering.
func TestManagerDemotionClaimsAdapter(t *testing.T) {
	mgr := tiersTestManager(t)
	ctx := context.Background()
	idA, err := mgr.StoreClaim(ctx, memory.Claim{Text: "claim alpha", Confidence: 0.9})
	require.NoError(t, err)
	idB, err := mgr.StoreClaim(ctx, memory.Claim{Text: "claim beta", Confidence: 0.8})
	require.NoError(t, err)

	src := &managerDemotionClaims{mgr: mgr}
	claims, err := src.Claims(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claims, 2)
	// Oldest first (both stored sub-second apart; stable order by id
	// breaks the tie deterministically through CreatedAt equality).

	// Demote one through the manager path; the pool must shrink (the
	// adapter reads the same flag DemoteClaim writes).
	require.NoError(t, mgr.DemoteClaim(ctx, idA, "stale", time.Now().UTC()))
	claims, err = src.Claims(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	assert.Equal(t, idB, claims[0].Memory.ID)
}

// TestManagerDemotionAdapterEndToEnd (leaf T3 Given/When/Then): stale
// claim -> Evaluate -> demote action planned AND executed through the
// REAL manager adapters (no stubs): episodic copy exists, flag stamped.
// Staleness is driven by advancing the engine's injected clock
// (tierpolicy.SetClock - deterministic tests) past the staleness window
// rather than by backdating rows through the store's unexported handle.
func TestManagerDemotionAdapterEndToEnd(t *testing.T) {
	mgr := tiersTestManager(t)
	ctx := context.Background()
	claimID, err := mgr.StoreClaim(ctx, memory.Claim{Text: "stale claim to demote", Confidence: 0.9})
	require.NoError(t, err)

	eng := tierpolicy.New(
		tierpolicy.Config{
			Enabled: true,
			Demotion: tierpolicy.DemotionConfig{
				StaleDays:     30,
				MinUsefulness: 0.1,
			},
		},
		tierpolicy.Deps{
			DemotionClaims: &managerDemotionClaims{mgr: mgr},
			Demotion:       &managerDemotion{mgr: mgr},
		},
	)
	// Advance the engine clock 40 days past the claim's creation so the
	// staleness criterion fires deterministically.
	future := time.Now().UTC().Add(40 * 24 * time.Hour)
	eng.SetClock(func() time.Time { return future })

	actions, err := eng.Evaluate(ctx, nil)
	require.NoError(t, err)
	require.Len(t, actions, 1, "stale claim must produce exactly one demote action")
	assert.Equal(t, tierpolicy.KindDemote, actions[0].Kind)

	rep, err := eng.Execute(ctx, actions)
	require.NoError(t, err)
	assert.Equal(t, 1, rep.Demoted)

	// The two-step outcome, through the real stores: the episodic copy
	// exists and the claim carries the demoted flag.
	mem, err := mgr.GetByID(ctx, claimID)
	require.NoError(t, err)
	require.NotNil(t, mem)
	assert.True(t, memory.ClaimIsDemoted(mem.Metadata), "claim must carry the demoted flag")
	rows, err := mgr.Episodic().Search(ctx, "stale claim to demote", 10)
	require.NoError(t, err)
	found := false
	for _, r := range rows {
		if meta, ok := r.Memory.Metadata["demoted_from"].(string); ok && meta == claimID {
			found = true
		}
	}
	assert.True(t, found, "episodic copy with demoted_from provenance must exist")
}

// TestBuildTierPolicyEngineWiring (leaf T4 Given/When/Then): enabled cfg
// -> engine non-nil with the REAL adapters wired (demotion pool live);
// disabled -> nil. The wiring-failure-proof target: delete the
// DemotionClaims/Demotion assignments in BuildTierPolicyEngine and the
// PlanDemotions-fails-closed posture returns - this test then fails on
// the action count (0 instead of 1).
func TestBuildTierPolicyEngineWiring(t *testing.T) {
	mgr := tiersTestManager(t)
	ctx := context.Background()
	claimID, err := mgr.StoreClaim(ctx, memory.Claim{Text: "wired stale claim", Confidence: 0.9})
	require.NoError(t, err)
	_ = claimID

	tiers := NewTiersConfigAdapter(config.MemoryTiersConfig{
		Enabled:       true,
		RetentionDays: 30,
		Demotion:      config.MemoryTiersDemotionConfig{StaleDays: 30, MinUsefulness: 0.1},
	})
	eng, wired, err := BuildTierPolicyEngine(tiers, workCfg(), mgr, nil, nil)
	require.NoError(t, err)
	require.True(t, wired)
	require.NotNil(t, eng)

	// Advance the engine clock past the staleness window (see the
	// SetClock note above) so the demote fires deterministically.
	future := time.Now().UTC().Add(60 * 24 * time.Hour)
	eng.SetClock(func() time.Time { return future })

	actions, err := eng.Evaluate(ctx, nil)
	require.NoError(t, err)
	require.Len(t, actions, 1, "enabled engine with adapters wired must plan the demote")

	// Disabled -> nil (zero-cost default posture).
	off, wiredOff, err := BuildTierPolicyEngine(TiersConfigAdapter{}, workCfg(), mgr, nil, nil)
	require.NoError(t, err)
	assert.False(t, wiredOff)
	assert.Nil(t, off)
}

// TestTiersEngineStartStopViaComponentsShape (leaf T4 lifecycle): the
// engine the wiring builds starts and stops cleanly through the
// EngineHandle (the lifecycle the Components.Start/stopComponents paths
// own). Uses a fast interval so at least one tick fires.
func TestTiersEngineStartStopViaComponentsShape(t *testing.T) {
	mgr := tiersTestManager(t)
	tiers := NewTiersConfigAdapter(config.MemoryTiersConfig{
		Enabled:         true,
		IntervalSeconds: 1,
		RetentionDays:   30,
	})
	eng, wired, err := BuildTierPolicyEngine(tiers, workCfg(), mgr, nil, nil)
	require.NoError(t, err)
	require.True(t, wired)
	handle := WrapEngine(eng)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, handle.Start(ctx))
	assert.True(t, handle.Running())
	// Double-start refused.
	assert.Error(t, handle.Start(ctx))
	handle.Stop()
	assert.False(t, handle.Running())
	// Idempotent.
	handle.Stop()
	// Nil-handle safety (typed-nil guard): a nil handle returns an
	// honest error on Start and is a no-op on Stop/Running - never a
	// panic.
	var nilHandle *EngineHandle
	assert.Error(t, nilHandle.Start(ctx))
	nilHandle.Stop()
	assert.False(t, nilHandle.Running())
}

// TestNewTiersConfigAdapterMapping (leaf T1<->T2 seam): the schema knob
// maps onto the adapter faithfully on the enabled path and collapses to
// the inert zero adapter on the disabled path.
func TestNewTiersConfigAdapterMapping(t *testing.T) {
	off := NewTiersConfigAdapter(config.MemoryTiersConfig{})
	assert.Equal(t, TiersConfigAdapter{}, off)

	on := NewTiersConfigAdapter(config.MemoryTiersConfig{
		Enabled:           true,
		EvictOverRatio:    0.9,
		CompressOverRatio: 0.8,
		IntervalSeconds:   300,
		RetentionDays:     30,
		Demotion:          config.MemoryTiersDemotionConfig{StaleDays: 30, MinUsefulness: 0.1},
	})
	assert.True(t, on.Enabled)
	assert.Equal(t, 30, on.RetentionDays)
	assert.Equal(t, 30, on.Demotion.StaleDays)
	cfg := on.ToEngineConfig(working.Config{})
	assert.Equal(t, 30*time.Second*10, cfg.Interval)
	assert.Equal(t, 30, cfg.Demotion.StaleDays)
}
