package tierpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/queue"
)

// ExecutionReport summarizes one Execute pass (contract C5, T2).
type ExecutionReport struct {
	// ActionsApplied counts actions actually applied (or, in dry-run,
	// would-be applied). Failed actions are not counted.
	ActionsApplied int
	// EvictRequested / EvictionsDeleted: eviction accounting. In
	// dry-run, EvictionsDeleted stays 0 while EvictRequested counts.
	EvictRequested   int
	EvictionsDeleted int
	// CompressRequested / CompressionEnqueued: compression accounting
	// (memory.l1_extract enqueues through the REAL distill job type).
	CompressRequested   int
	CompressionEnqueued int
	// DemoteRequested / Demoted: demotion accounting (leaf 03). Demoted
	// counts claims actually demoted (copy + flag); in dry-run only
	// DemoteRequested counts. Idempotent re-runs of already-demoted IDs
	// apply without incrementing Demoted (R-B7 noop semantics).
	DemoteRequested int
	Demoted         int
	// DryRun mirrors the engine's dry-run posture for this pass.
	DryRun bool
	// Errors carries per-action failures (never fatal to the batch).
	Errors []string
}

// tierJobPrefix marks jobs the tier policy enqueued so the distill worker
// and operators can tell policy-driven distills from capture-driven ones
// (AgentID is free-form target routing on queue.Job).
const tierJobPrefix = "tierpolicy"

// Execute performs the actions against Deps: evictions via Deleter,
// compression via JobEnqueuer using the REAL memory.MemoryJobL1Extract
// job type with an L1ExtractPayload per target (compression REUSES the
// existing distill pipeline - never inline LLM work, never blocking the
// tick on it). Called ONLY from the background tick owner, never from
// chat-path code.
//
// Dry-run mode (Config.DryRun) applies nothing but reports what would
// happen. Per-action failures are recorded in the report and never abort
// the batch.
func (e *Engine) Execute(ctx context.Context, actions []Action) (ExecutionReport, error) {
	var rep ExecutionReport
	if e == nil {
		return rep, errors.New("tierpolicy: nil engine")
	}
	if !e.cfg.Enabled {
		// A disabled engine executes nothing: the enabled gate wraps
		// BOTH halves of the pipeline.
		rep.DryRun = true
		return rep, nil
	}
	rep.DryRun = e.cfg.DryRun
	var errs []error
	for _, act := range actions {
		switch act.Kind {
		case KindEvict:
			rep.EvictRequested += len(act.TargetIDs)
			if e.cfg.DryRun {
				rep.ActionsApplied++
				continue
			}
			if e.deps.Deletes == nil {
				errs = append(errs, fmt.Errorf("evict %d ids: no Deleter wired", len(act.TargetIDs)))
				continue
			}
			n, err := e.deps.Deletes.DeleteByIDs(ctx, act.TargetIDs)
			if err != nil {
				errs = append(errs, fmt.Errorf("evict %d ids: %w", len(act.TargetIDs), err))
				continue
			}
			rep.EvictionsDeleted += n
			rep.ActionsApplied++
		case KindCompress:
			rep.CompressRequested += len(act.TargetIDs)
			if e.cfg.DryRun {
				rep.ActionsApplied++
				continue
			}
			n, err := e.enqueueCompress(ctx, act)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			rep.CompressionEnqueued += n
			rep.ActionsApplied++
		case KindDemote:
			// Contract C6 (leaf 03): semantic -> episodic demotion,
			// lossless copy + demoted flag, never a delete. Idempotent
			// per ID (R-B7).
			rep.DemoteRequested += len(act.TargetIDs)
			if e.cfg.DryRun {
				rep.ActionsApplied++
				continue
			}
			n, err := e.executeDemote(ctx, act)
			if err != nil {
				errs = append(errs, err)
			}
			// Partial progress still counts: a crash between the
			// episodic copy and the flag is recoverable by re-run.
			rep.Demoted += n
			if n == len(act.TargetIDs) {
				rep.ActionsApplied++
			}
		default:
			errs = append(errs, fmt.Errorf("unknown action kind %q", act.Kind))
		}
	}
	for _, err := range errs {
		rep.Errors = append(rep.Errors, err.Error())
	}
	return rep, errors.Join(errs...)
}

// enqueueCompress enqueues one memory.l1_extract job per compression
// batch through the REAL distill pipeline. The payload reuses
// memory.L1ExtractPayload verbatim (SHARED-CONVENTIONS 2.2): TierPolicy
// compression IS extraction-to-claims, not free-text re-summarization
// (R-B4). The L0Path carries the tier-scoped selection (populated by the
// daemon's tier adapter); SessionID scopes the job. A nil Enqueuer is an
// honest wiring error, reported per action.
func (e *Engine) enqueueCompress(ctx context.Context, act Action) (int, error) {
	if e.deps.Enqueuer == nil {
		return 0, fmt.Errorf("compress %d ids: no queue Enqueuer wired", len(act.TargetIDs))
	}
	payload := memory.L1ExtractPayload{
		SessionID: tierJobPrefix + ":" + sessionFromReason(act.Reason),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("compress payload marshal: %w", err)
	}
	job, err := queue.NewJob(memory.MemoryJobL1Extract, raw)
	if err != nil {
		return 0, fmt.Errorf("compress job create: %w", err)
	}
	job.AgentID = tierJobPrefix
	// Carry the full target selection in the job so the distill worker
	// adapter can process exactly these IDs without a second lookup.
	job.Payload = raw
	if len(act.TargetIDs) > 0 {
		job.TaskID = tierJobPrefix + ":" + act.TargetIDs[0]
	}
	if err := e.deps.Enqueuer.Enqueue(ctx, job); err != nil {
		return 0, fmt.Errorf("compress enqueue (%s): %w", truncate(act.Reason, 80), err)
	}
	return len(act.TargetIDs), nil
}

// sessionFromReason extracts the tier name from the machine-readable
// reason ("tier=<name>;...") for job scoping. Unknown reasons map to
// "unknown".
func sessionFromReason(reason string) string {
	for _, part := range strings.Split(reason, ";") {
		if v, ok := strings.CutPrefix(part, "tier="); ok {
			return v
		}
	}
	return "unknown"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
