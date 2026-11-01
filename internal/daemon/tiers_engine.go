package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
// TiersConfigAdapter is the resolved [memory.tiers] config carrier. The
// orchestrator's schema.go merge (leaf T3: MemoryConfig.Tiers of type
// config.MemoryTiersConfig) maps onto this adapter; keeping the field
// set as plain values here lets this file compile before that merge and
// keeps the merge mechanical.
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
	}
}

// managerCandidates adapts memory.Manager to the candidate source:
// oldest episodic records first (R-B5 oldest-first batching) via
// GetOldMemories - the same population the consolidation pass ranks
// (consolidation.go:528).
type managerCandidates struct{ mgr *memory.Manager }

func (m *managerCandidates) Candidates(ctx context.Context, _ string, limit int) ([]memory.MemoryResult, error) {
	if m == nil || m.mgr == nil {
		return nil, errors.New("tierpolicy wiring: nil memory manager")
	}
	ep := m.mgr.Episodic()
	if ep == nil {
		return nil, nil
	}
	return ep.GetOldMemories(ctx, time.Time{}, limit)
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
	deps := tierpolicy.Deps{
		Candidates: &managerCandidates{mgr: mmgr},
		Votes:      &managerVotes{mgr: mmgr},
		Deletes:    &episodicDeleter{episodic: mmgr.Episodic()},
		Enqueuer:   queueAsEnqueuer(q),
		States:     &managerStates{mgr: mmgr, ceiling: DefaultTiersEpisodicCeiling},
		Logger:     logger,
	}
	eng := tierpolicy.New(tiers.ToEngineConfig(workCfg), deps)
	return eng, true, nil
}

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
