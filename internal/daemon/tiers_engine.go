package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/tierpolicy"
	"github.com/caimlas/meept/internal/memory/working"
	"github.com/caimlas/meept/internal/queue"
)

// Daemon wiring for the memory-tier policy engine (tree 02 leaf 02,
// contract C5): constructs the engine AFTER the memory manager and the
// persistent queue exist and owns ONLY the manager->Deps plumbing. All
// behavioral guarantees (bands, batching, watermarks, single-start,
// dry-run, Execute) live in the tierpolicy package - this file is
// reuse, never a fork, of the PlanUsefulEviction inputs
// (consolidation.go:549) and the distill enqueue targets
// (distill_async.go:31-33). It never constructs a Consolidator and
// never imports internal/agent.
//
// TiersConfigAdapter is the resolved [memory.tiers] config carrier. It
// mirrors config.MemoryTiersConfig (leaf T1: the schema knob lands in
// internal/config/schema.go MemoryConfig.Tiers and normalizes at the
// load boundary via NormalizeMemoryTiersDefaults) plus the resolved
// retention window the candidate cutoff consumes (leaf T2). The plain-
// value shape keeps this file independent of the schema package's
// struct identity: NewTiersConfigAdapter is the single mapping point.
type TiersConfigAdapter struct {
	// Enabled is the opt-in gate (default false - the ONLY posture this
	// leaf ships; flipping it is a documented product decision
	// proposal, docs/agents/opt-in-defaults.md).
	Enabled bool
	// EvictOverRatio resolved (0.9 default via tierpolicy.Resolve...).
	EvictOverRatio float64
	// CompressOverRatio resolved (0.8 default).
	CompressOverRatio float64
	// IntervalSeconds resolved (300 default).
	IntervalSeconds int
	// DryRun posture (default false).
	DryRun bool
	// RetentionDays is the resolved candidate cutoff window (default
	// 30; leaf T2): candidates are episodic rows older than now minus
	// retention. <= 0 keeps the engine from passing a zero-time cutoff
	// (the T2 defect) - retention <= 0 resolves to the default before
	// this struct is built.
	RetentionDays int
	// Demotion carries the resolved [memory.tiers.demotion] thresholds
	// (contract C6): staleness window and usefulness floor.
	Demotion tierpolicy.DemotionConfig
}

// NewTiersConfigAdapter maps the normalized schema knob onto the
// adapter. Only the enabled path resolves knobs: a disabled config
// normalizes to the inert zero-value adapter (BuildTierPolicyEngine
// returns nil before reading any knob).
func NewTiersConfigAdapter(mt config.MemoryTiersConfig) TiersConfigAdapter {
	if !mt.Enabled {
		return TiersConfigAdapter{}
	}
	return TiersConfigAdapter{
		Enabled:           true,
		EvictOverRatio:    mt.EvictOverRatio,
		CompressOverRatio: mt.CompressOverRatio,
		IntervalSeconds:   mt.IntervalSeconds,
		DryRun:            mt.DryRun,
		RetentionDays:     mt.RetentionDays,
		Demotion: tierpolicy.DemotionConfig{
			StaleDays:     mt.Demotion.StaleDays,
			MinUsefulness: mt.Demotion.MinUsefulness,
		},
	}
}

// ToEngineConfig maps the adapter onto the engine config.
func (a TiersConfigAdapter) ToEngineConfig(workCfg working.Config) tierpolicy.Config {
	return tierpolicy.Config{
		WorkingConfig:     workCfg,
		EvictOverRatio:    a.EvictOverRatio,
		CompressOverRatio: a.CompressOverRatio,
		Interval:          time.Duration(a.IntervalSeconds) * time.Second,
		Enabled:           a.Enabled,
		DryRun:            a.DryRun,
		Demotion:          a.Demotion,
	}
}

// managerCandidates adapts memory.Manager to the candidate source:
// oldest episodic records first (R-B5 oldest-first batching) via
// GetOldMemories - the same population the consolidation pass ranks
// (consolidation.go:528). The cutoff is REAL: now minus the retention
// window (leaf T2) - the previous time.Time{} zero value can never
// match a row (created_at < 0001-01-01 is always false), so the tick
// saw an empty candidate pool forever and every band action silently
// planned nothing.
type managerCandidates struct {
	mgr       *memory.Manager
	retention time.Duration
}

func (m *managerCandidates) Candidates(ctx context.Context, _ string, limit int) ([]memory.MemoryResult, error) {
	if m == nil || m.mgr == nil {
		return nil, errors.New("tierpolicy wiring: nil memory manager")
	}
	ep := m.mgr.Episodic()
	if ep == nil {
		return nil, nil
	}
	return ep.GetOldMemories(ctx, time.Now().UTC().Add(-m.retention), limit)
}

// managerVotes adapts memory.Manager.NetVotes - the batched vote lookup
// the consolidation pass already uses (consolidation.go:549).
type managerVotes struct{ mgr *memory.Manager }

func (m *managerVotes) NetVotes(ctx context.Context, ids []string) (map[string]int, error) {
	if m == nil || m.mgr == nil {
		return nil, errors.New("tierpolicy wiring: nil memory manager")
	}
	return m.mgr.NetVotes(ctx, ids)
}

// episodicDeleter adapts EpisodicMemory.DeleteByIDs
// (consolidation.go:554 shape).
type episodicDeleter struct{ episodic *memory.EpisodicMemory }

func (d *episodicDeleter) DeleteByIDs(ctx context.Context, ids []string) (int, error) {
	if d == nil || d.episodic == nil {
		return 0, errors.New("tierpolicy wiring: nil episodic store")
	}
	return d.episodic.DeleteByIDs(ctx, ids)
}

// managerDemotionClaims adapts the manager's epistemic claim lookups
// (epistemic.go StoreClaim family: claims are typed memories with
// Type == claim stored in the episodic table) onto the tierpolicy
// ClaimSource seam. Without this adapter the demotion planner fails
// closed at demote.go:129 (nil DemotionClaims -> PlanDemotions returns
// nil) - stale claims never demoted, live. Oldest-first ordering
// matches the ClaimSource contract ("oldest first for deterministic
// planning") via the same broad-search + post-filter pattern
// ListAutoClaims uses; demoted rows are excluded so the R-B7
// idempotency read stays consistent when the planner re-pools.
type managerDemotionClaims struct {
	mgr *memory.Manager
}

func (m *managerDemotionClaims) Claims(ctx context.Context, limit int) ([]memory.MemoryResult, error) {
	if m == nil || m.mgr == nil {
		return nil, errors.New("tierpolicy wiring: nil memory manager")
	}
	if limit <= 0 {
		limit = 500
	}
	// Broad search (Type == claim) then post-filter: the FTS5 index
	// handles free text but metadata filtering happens post-query -
	// exactly the ListAutoClaims idiom (epistemic.go:414). A query with
	// no match terms returns the unranked store population, oldest
	// first per the created_at ASC scan.
	results, err := m.mgr.Search(ctx, memory.MemoryQuery{
		Type:  memory.MemoryTypeClaim,
		Limit: limit * 4,
	})
	if err != nil {
		return nil, fmt.Errorf("tierpolicy wiring: claim pool: %w", err)
	}
	out := make([]memory.MemoryResult, 0, len(results))
	for _, r := range results {
		if r.Memory.Type != memory.MemoryTypeClaim {
			continue
		}
		if memory.ClaimIsDemoted(r.Memory.Metadata) {
			continue // R-B7: demoted rows leave the live pool
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	// Oldest first (ClaimSource contract). Stable sort keeps the
	// search's own relevance order for equal timestamps.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Memory.CreatedAt.Before(out[j].Memory.CreatedAt)
	})
	return out, nil
}

// managerDemotion is the tierpolicy.DemotionApplier over the manager's
// claim-demotion primitives (claim_demote.go): CopyToEpisodic is the
// lossless episodic copy (step 1, content preserved with demoted_from
// provenance so a crash-between-steps re-run dedupes); MarkDemoted is
// the in-place flag stamp (step 2, no version row, claim ID stable).
// The applier doubles as the DemotedDetector (flag read via GetByID),
// which the engine prefers over the metadata-shape path.
type managerDemotion struct {
	mgr *memory.Manager
}

func (d *managerDemotion) CopyToEpisodic(ctx context.Context, claim memory.Memory) (string, error) {
	if d == nil || d.mgr == nil {
		return "", errors.New("tierpolicy wiring: nil memory manager")
	}
	ep := d.mgr.Episodic()
	if ep == nil {
		return "", errors.New("tierpolicy wiring: episodic store unavailable")
	}
	meta := map[string]any{
		"demoted_from": claim.ID,
		"source_type":  string(claim.Type),
	}
	if claim.Metadata != nil {
		if src, ok := claim.Metadata["source"].(string); ok && src != "" {
			meta["source"] = src
		}
	}
	return ep.Store(ctx, claim.Content, "claim", meta)
}

func (d *managerDemotion) MarkDemoted(ctx context.Context, claimID, reason string, at time.Time) error {
	if d == nil || d.mgr == nil {
		return errors.New("tierpolicy wiring: nil memory manager")
	}
	return d.mgr.DemoteClaim(ctx, claimID, reason, at)
}

// IsDemoted satisfies tierpolicy.DemotedDetector (R-B7) through the
// same GetByID read DemoteClaim uses, so the detector and the flag
// writer can never disagree about a claim's state.
func (d *managerDemotion) IsDemoted(ctx context.Context, claimID string) (bool, error) {
	if d == nil || d.mgr == nil {
		return false, errors.New("tierpolicy wiring: nil memory manager")
	}
	mem, err := d.mgr.GetByID(ctx, claimID)
	if err != nil {
		return false, err
	}
	if mem == nil {
		return false, nil
	}
	return memory.ClaimIsDemoted(mem.Metadata), nil
}

// compile-time proofs the manager adapters satisfy the tierpolicy seams.
var (
	_ tierpolicy.ClaimSource     = (*managerDemotionClaims)(nil)
	_ tierpolicy.DemotionApplier = (*managerDemotion)(nil)
	_ tierpolicy.DemotedDetector = (*managerDemotion)(nil)
)

// avgTokensPerMemory estimates per-record token cost for the episodic
// tier's Tokens figure (typical content at chars/4). Leaf 03 owns
// richer per-tier accounting.
const avgTokensPerMemory = 128

// DefaultTiersEpisodicCeiling is the episodic recall-feed ceiling in
// records (R-B3: Items is the trigger unit for semantic sets; the
// working tier overrides with its own token ceiling). Pending per-tier
// ceilings from leaf 03.
const DefaultTiersEpisodicCeiling = 50000

// managerStates adapts the manager into per-tick TierStates (R-B3: the
// working tier triggers on Tokens, episodic on Items).
type managerStates struct {
	mgr     *memory.Manager
	ceiling int
}

func (m *managerStates) TierStates(ctx context.Context) ([]tierpolicy.TierState, error) {
	if m == nil || m.mgr == nil {
		return nil, errors.New("tierpolicy wiring: nil memory manager")
	}
	stats, err := m.mgr.GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("tierpolicy wiring: memory stats: %w", err)
	}
	return []tierpolicy.TierState{
		{
			Tier:    "episodic",
			Tokens:  stats.EpisodicCount * avgTokensPerMemory,
			Items:   stats.EpisodicCount,
			Ceiling: m.ceiling,
		},
	}, nil
}

// queueAsEnqueuer narrows the daemon queue to the enqueue seam
// (*queue.PersistentQueue satisfies memory.JobEnqueuer /
// tierpolicy.JobEnqueuer via Queue.Enqueue, distill_async.go:58). A nil
// queue yields nil: Evaluate keeps working and Execute reports an
// honest per-action error (same posture as the L0 enqueuer wiring,
// components.go:2895).
func queueAsEnqueuer(q *queue.PersistentQueue) tierpolicy.JobEnqueuer {
	if q == nil {
		return nil
	}
	var seam memory.JobEnqueuer = q
	return seam
}

// BuildTierPolicyEngine constructs the memory-tier policy engine from
// daemon components (leaf T4).
//
// GATING + PLACEMENT: call this AFTER the memory manager and the
// persistent queue exist (the same AFTER-queue rule the L0 distill
// wiring follows - a nil enqueuer means compression jobs are never
// enqueued, observed live 2026-10-13 for the L0 twin). With the tiers
// config at its zero value (Enabled=false, the shipped default) this
// returns (nil, false, nil): NO goroutine, zero cost.
//
// The engine is returned STOPPED; the caller owns the lifecycle:
//
//	eng, wired, err := BuildTierPolicyEngine(adapter, workCfg, c.MemoryManager, pq, logger)
//	if err != nil { ... }
//	if wired {
//	    if err := eng.Start(daemonCtx); err != nil { ... }
//	    defer eng.Stop()  // clean ctx-cancel + Stop on shutdown
//	}
//
// Typed-nil guard (internal/daemon/AGENTS.md): an enabled config with a
// nil manager is a wiring error, never a ticking engine that would
// deref nil on its first pass.
//
// Activation note for the orchestrator (single-line call site): the
// call belongs in Components immediately after the DistillWorker wiring
// (~components.go:2916), where both c.MemoryManager (initialized) and
// c.Queue are already live. components.go is outside this leaf's
// ownership, so the call-site insertion is left to the orchestrator
// merge; TiersWiringTest pins the seam contract this function provides.
func BuildTierPolicyEngine(tiers TiersConfigAdapter, workCfg working.Config, mmgr *memory.Manager, q *queue.PersistentQueue, logger *slog.Logger) (*tierpolicy.Engine, bool, error) {
	if !tiers.Enabled {
		return nil, false, nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	if mmgr == nil {
		return nil, false, ErrTiersNoManager
	}
	// Leaf T2: a non-positive retention must never reach the cutoff
	// arithmetic (a zero-time cutoff matches nothing - the live defect
	// this leaf closes). Resolve to the documented default here so the
	// adapter is safe even when built by hand in tests.
	retentionDays := tiers.RetentionDays
	if retentionDays <= 0 {
		retentionDays = DefaultTiersRetentionDays
	}
	staleDays := tiers.Demotion.StaleDays
	if staleDays <= 0 {
		staleDays = tierpolicy.DefaultDemotionStaleDays
	}
	minUsefulness := tiers.Demotion.MinUsefulness
	if minUsefulness <= 0 || minUsefulness >= 1 {
		minUsefulness = tierpolicy.DefaultDemotionMinUsefulness
	}
	deps := tierpolicy.Deps{
		Candidates: &managerCandidates{mgr: mmgr, retention: time.Duration(retentionDays) * 24 * time.Hour},
		Votes:      &managerVotes{mgr: mmgr},
		Deletes:    &episodicDeleter{episodic: mmgr.Episodic()},
		Enqueuer:   queueAsEnqueuer(q),
		States:     &managerStates{mgr: mmgr, ceiling: DefaultTiersEpisodicCeiling},
		// Leaf T3: the epistemic claim pool + demotion applier. A nil
		// DemotionClaims here is what made demotion fail closed
		// forever (demote.go:129); both adapters are always wired when
		// the engine exists.
		DemotionClaims: &managerDemotionClaims{mgr: mmgr},
		Demotion:       &managerDemotion{mgr: mmgr},
		Logger:         logger,
	}
	cfg := tiers.ToEngineConfig(workCfg)
	cfg.Demotion = tierpolicy.DemotionConfig{StaleDays: staleDays, MinUsefulness: minUsefulness}
	eng := tierpolicy.New(cfg, deps)
	return eng, true, nil
}

// DefaultTiersRetentionDays mirrors the schema default (30) so the
// engine-side resolve stays consistent with NormalizeMemoryTiersDefaults
// even for hand-built adapters.
const DefaultTiersRetentionDays = 30

// ErrTiersNoManager is returned when memory.tiers is enabled but the
// memory manager is missing (typed-nil/wiring guard rule).
var ErrTiersNoManager = errors.New("memory.tiers enabled but memory manager is nil")

// EngineHandle is the daemon-facing lifecycle handle over the engine:
// it carries Start/Stop/Running only, keeping the daemon from touching
// Evaluate/Execute directly (the background tick is their ONLY owner -
// never chat-path code).
type EngineHandle struct {
	eng *tierpolicy.Engine
}

// WrapEngine adapts a constructed engine into the handle.
func WrapEngine(eng *tierpolicy.Engine) *EngineHandle {
	return &EngineHandle{eng: eng}
}

// Start launches the background tick. A second Start while running
// returns tierpolicy.ErrAlreadyStarted (single-start guarantee).
func (h *EngineHandle) Start(ctx context.Context) error {
	if h == nil || h.eng == nil {
		return errors.New("tierpolicy wiring: nil engine handle")
	}
	return h.eng.Start(ctx)
}

// Stop stops the background tick. Idempotent; safe on a nil handle.
func (h *EngineHandle) Stop() {
	if h == nil || h.eng == nil {
		return
	}
	h.eng.Stop()
}

// Running reports whether the background tick is active.
func (h *EngineHandle) Running() bool {
	return h != nil && h.eng != nil && h.eng.Running()
}
