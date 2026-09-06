// Package effects implements the external-effect idempotency ledger: a
// durable record of irreversible external effects (publish, delete, send,
// push) that is claimed before execution and reconciled after crashes.
//
// The protocol: Claim a stable, content-derived EffectKey before any
// irreversible external effect; execute once; RecordReceipt with the
// provider's outcome; Complete when the effect is confirmed done. On resume,
// ReconcilePending finds every claimed-but-incomplete effect. A completed
// prior makes any later Claim of the same key an idempotent no-op that
// returns the stored receipt.
package effects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// EffectState is the lifecycle state of a ledger record.
type EffectState string

// The four pinned states. claimed means the effect is believed
// not-yet-run; receipted means it ran and the provider outcome is stored
// but the completion marker is not yet written; completed means done —
// subsequent claims of the key are idempotent no-ops; abandoned means the
// effect is dead (tool error path or explicit human reconciliation).
const (
	StateClaimed   EffectState = "claimed"
	StateReceipted EffectState = "receipted"
	StateCompleted EffectState = "completed"
	StateAbandoned EffectState = "abandoned"
)

// EffectMeta is caller-supplied context captured at claim time.
type EffectMeta struct {
	TaskID    string `json:"task_id,omitempty"`
	StepID    string `json:"step_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Tool      string `json:"tool"`
	// ProviderIdempotent: the tool declares the provider accepts the same
	// idempotency key, so reconcile may auto-retry this effect. Tools that
	// cannot safely re-execute leave it false; their stuck records are
	// surfaced for human reconciliation instead.
	ProviderIdempotent bool `json:"provider_idempotent"`
	// Payload is the effect input (e.g. the marshaled request). Stored at
	// claim time and returned in EffectRecord so reconcile re-executors can
	// reconstruct the effect after a restart. Preserved byte-for-byte.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// EffectRecord is a durable ledger row.
type EffectRecord struct {
	Key                string          `json:"key"`
	State              EffectState     `json:"state"`
	TaskID             string          `json:"task_id"`
	StepID             string          `json:"step_id"`
	SessionID          string          `json:"session_id"`
	Tool               string          `json:"tool"`
	ProviderIdempotent bool            `json:"provider_idempotent"`
	ClaimedAt          time.Time       `json:"claimed_at"`
	ExecutedAt         *time.Time      `json:"executed_at,omitempty"`
	CompletedAt        *time.Time      `json:"completed_at,omitempty"`
	Payload            json.RawMessage `json:"payload,omitempty"`
	Receipt            json.RawMessage `json:"receipt,omitempty"`
	// AbandonReason is the reason recorded when the effect was abandoned
	// (CLI `effects reconcile --abandon --reason ...`). Empty otherwise.
	AbandonReason string `json:"abandon_reason,omitempty"`
}

// Ledger is the effect-idempotency store. Implementations: SQLiteLedger
// (durable, over effects.db) and MemoryLedger (in-memory, for tests).
type Ledger interface {
	// Claim atomically records the intent to execute the effect identified
	// by key. granted=true means this caller owns the effect and must
	// execute it. granted=false means the key already exists: prior carries
	// the existing record (a completed prior => return its receipt as an
	// idempotent no-op). INSERT PK conflict is the atomicity mechanism —
	// no separate check-then-insert.
	Claim(ctx context.Context, key string, meta EffectMeta) (granted bool, prior *EffectRecord, err error)
	// RecordReceipt stores the provider outcome and moves claimed ->
	// receipted (executed_at stamped). Unknown key or non-claimed state is
	// an error.
	RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error
	// Complete writes the completion marker: receipted (or claimed) ->
	// completed (completed_at stamped). Idempotent when already completed.
	Complete(ctx context.Context, key string) error
	// Abandon marks the effect dead (claimed/receipted -> abandoned).
	// Completed keys cannot be abandoned.
	Abandon(ctx context.Context, key string, reason string) error
	// ReconcilePending returns every record in state claimed or receipted,
	// oldest claimed_at first. Startup and parked-turn resume call this.
	ReconcilePending(ctx context.Context) ([]EffectRecord, error)
	// Get returns one record or (nil, ErrUnknownKey).
	Get(ctx context.Context, key string) (*EffectRecord, error)
	Close() error
}

var ErrUnknownKey = errors.New("effects: unknown effect key")
var ErrInvalidTransition = errors.New("effects: invalid state transition")

// Run wraps one external effect in the pinned protocol:
// Claim -> execute -> RecordReceipt. Returns (receipt, reused, err);
// reused=true means a completed prior existed, execute was NOT invoked,
// and receipt is the prior completed receipt (idempotent no-op path).
// Callers then invoke Complete themselves.
// On execute error the record stays claimed (reconcile finds it); Run does
// NOT abandon automatically.
func Run(ctx context.Context, l Ledger, key string, meta EffectMeta,
	execute func(ctx context.Context) (json.RawMessage, error)) (receipt json.RawMessage, reused bool, err error) {
	granted, prior, err := l.Claim(ctx, key, meta)
	if err != nil {
		return nil, false, fmt.Errorf("effects: claim %s: %w", key, err)
	}
	if !granted {
		if prior == nil {
			return nil, false, fmt.Errorf("effects: claim %s: denied without prior record", key)
		}
		if prior.State == StateCompleted {
			return prior.Receipt, true, nil
		}
		// A claimed/receipted prior means someone else owns the in-flight
		// effect: never execute, never complete.
		return nil, false, fmt.Errorf("effects: claim %s: prior in state %q owned by another caller: %w", key, prior.State, ErrInvalidTransition)
	}

	out, execErr := execute(ctx)
	if execErr != nil {
		// The record stays claimed on purpose: reconcile finds it. The
		// tool decides whether to Abandon.
		return nil, false, fmt.Errorf("effects: execute %s: %w", key, execErr)
	}
	if out == nil {
		return nil, false, fmt.Errorf("effects: execute %s: execute returned nil receipt", key)
	}
	if err := l.RecordReceipt(ctx, key, out); err != nil {
		return nil, false, fmt.Errorf("effects: record receipt %s: %w", key, err)
	}
	return out, false, nil
}

// validateTransition maps the pinned state machine to nil or
// ErrInvalidTransition. Shared by both Ledger implementations so their
// transition tables cannot drift.
//
//	claimed    --RecordReceipt--> receipted
//	claimed    --Complete-------> completed
//	claimed    --Abandon--------> abandoned
//	receipted  --Complete-------> completed
//	receipted  --Abandon--------> abandoned
//	completed  --Complete-------> completed (idempotent)
//	everything else               -> ErrInvalidTransition
func validateTransition(from EffectState, action string) error {
	switch action {
	case "record_receipt":
		if from == StateClaimed {
			return nil
		}
	case "complete":
		if from == StateClaimed || from == StateReceipted || from == StateCompleted {
			return nil
		}
	case "abandon":
		if from == StateClaimed || from == StateReceipted {
			return nil
		}
	}
	return fmt.Errorf("effects: cannot %s a %s effect: %w", action, from, ErrInvalidTransition)
}
