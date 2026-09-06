package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/effects"
)

// EffectReExecutor re-runs a pending effect for a tool that declared
// provider-side idempotency. It receives the durable record (with the
// stored Payload) and rebuilds the effect request from it, returning the
// provider receipt on success.
type EffectReExecutor func(ctx context.Context, rec effects.EffectRecord) (json.RawMessage, error)

// EffectsReconciler owns reconcile behavior over the ledger: auto-retry
// ONLY for records whose EffectMeta.ProviderIdempotent is true AND whose
// tool registered an executor; everything else is surfaced via slog Error
// for human reconciliation (CLI visibility comes in leaf 03).
type EffectsReconciler struct {
	ledger    effects.Ledger
	executors map[string]EffectReExecutor
	logger    *slog.Logger
}

// NewEffectsReconciler constructs a reconciler over the ledger.
func NewEffectsReconciler(l effects.Ledger, logger *slog.Logger) *EffectsReconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &EffectsReconciler{
		ledger:    l,
		executors: make(map[string]EffectReExecutor),
		logger:    logger,
	}
}

// RegisterExecutor registers a re-executor for a tool name. Nil-guarded
// (setter convention); only meaningful for tools whose EffectMeta declared
// ProviderIdempotent=true.
func (r *EffectsReconciler) RegisterExecutor(tool string, fn EffectReExecutor) {
	if r == nil || tool == "" || fn == nil {
		return
	}
	r.executors[tool] = fn
}

// ReconcileCounts summarizes one reconcile pass.
type ReconcileCounts struct {
	Retried  int
	Surfaced int
	Failed   int
}

// Reconcile runs the pinned policy over ReconcilePending:
//   - executor registered AND rec.ProviderIdempotent -> re-execute via
//     effects.Run + Complete on success (state -> receipted -> completed);
//     on failure: slog.Warn, record stays pending.
//   - otherwise: slog.Error("effect pending reconciliation", ...) and count
//     it as surfaced.
//
// Errors never propagate to the caller beyond ReconcilePending itself;
// the reconciler is best-effort by contract (startup + resume hooks).
func (r *EffectsReconciler) Reconcile(ctx context.Context) ReconcileCounts {
	var counts ReconcileCounts

	pending, err := r.ledger.ReconcilePending(ctx)
	if err != nil {
		r.logger.Error("effects reconcile pending failed", "error", err)
		return counts
	}

	for _, rec := range pending {
		executor, hasExecutor := r.executors[rec.Tool]
		if hasExecutor && rec.ProviderIdempotent {
			if r.retryOne(ctx, rec, executor) {
				counts.Retried++
			} else {
				counts.Failed++
			}
			continue
		}

		counts.Surfaced++
		r.logger.Error("effect pending reconciliation",
			"key", rec.Key,
			"tool", rec.Tool,
			"claimed_at", rec.ClaimedAt,
			"state", rec.State,
			"provider_idempotent", rec.ProviderIdempotent,
		)
	}
	return counts
}

// retryOne re-executes one pending record. The reconciler is the
// DESIGNATED ADOPTER of pending records: effects.Run cannot be used here
// because its Claim is INSERT-only — re-claiming an existing key is
// refused as "owned by another caller" (leaf 01 single-owner invariant).
// The pinned state machine is driven directly instead:
//
//	claimed  -> execute -> RecordReceipt(new receipt) -> Complete
//	receipted-> execute (re-verify) ----------------------> Complete
//	(the receipt is already stored; RecordReceipt claimed->receipted
//	 does not apply)
//
// Returns true on success (record completed), false on failure (record
// stays pending for the next pass).
func (r *EffectsReconciler) retryOne(ctx context.Context, rec effects.EffectRecord, executor EffectReExecutor) bool {
	newReceipt, execErr := executor(ctx, rec)
	if execErr != nil {
		r.logger.Warn("effect reconcile retry failed",
			"key", rec.Key,
			"tool", rec.Tool,
			"error", execErr,
		)
		return false
	}
	if newReceipt == nil {
		// Keep the pre-existing receipt when the re-executor has nothing
		// new (e.g. it replayed verification only).
		newReceipt = rec.Receipt
	}

	switch rec.State {
	case effects.StateClaimed:
		if err := r.ledger.RecordReceipt(ctx, rec.Key, newReceipt); err != nil {
			r.logger.Warn("effect reconcile record-receipt failed",
				"key", rec.Key,
				"tool", rec.Tool,
				"error", err,
			)
			return false
		}
	case effects.StateReceipted:
		// Receipt already durable; the executor re-verified the effect.
	default:
		r.logger.Warn("effect reconcile: unexpected state for retry",
			"key", rec.Key,
			"tool", rec.Tool,
			"state", rec.State,
		)
		return false
	}

	if err := r.ledger.Complete(ctx, rec.Key); err != nil {
		r.logger.Warn("effect reconcile complete failed",
			"key", rec.Key,
			"tool", rec.Tool,
			"error", err,
		)
		return false
	}
	r.logger.Info("effect reconcile retried",
		"key", rec.Key,
		"tool", rec.Tool,
	)
	return true
}

// runEffectsStartupReconcile runs one reconcile pass with a summary log —
// the exact call-shape the daemon startup hook performs. Never fatal.
func runEffectsStartupReconcile(ctx context.Context, r *EffectsReconciler, logger *slog.Logger) ReconcileCounts {
	if r == nil {
		return ReconcileCounts{}
	}
	counts := r.Reconcile(ctx)
	logger.Info("effects startup reconcile",
		"retried", counts.Retried,
		"surfaced", counts.Surfaced,
		"failed", counts.Failed,
	)
	return counts
}

// effectsResumeHook adapts the reconciler to agent.EffectsResumeHook (a
// plain func type in package agent — dependency direction daemon -> agent,
// never inverted). Best-effort: errors are logged by the reconciler; a
// panicking hook is recovered so resume always proceeds.
func effectsResumeHook(r *EffectsReconciler, logger *slog.Logger) func(ctx context.Context) {
	return func(ctx context.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("effects resume hook panicked", "panic", fmt.Sprint(rec))
			}
		}()
		counts := runEffectsStartupReconcile(ctx, r, logger)
		_ = counts // summary already logged by runEffectsStartupReconcile
	}
}

// effectsStartupTimeout bounds the synchronous startup reconcile.
const effectsStartupTimeout = 30 * time.Second
