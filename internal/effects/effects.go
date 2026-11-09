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
//
// Claim records a Claimant. RecordReceipt, Complete, and Abandon are
// resolves: they succeed only for that same claimant. A second principal
// who knows the key cannot finish another agent's pending effect.
// Operator reconciliation and the daemon's designated adopter use the
// explicit Admin methods; those are not a claimant and must not be used
// by agent tool paths.
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

// Claimant is the principal that owns a pending effect. AgentID and
// SessionID are recorded together at Claim and must both match on every
// later resolve. A zero Claimant is not an identity: Claim rejects it so
// two unattributed callers cannot share a row. Callers pass the identity
// they actually have. Do not substitute a process-wide constant — that
// makes the match a no-op.
type Claimant struct {
	AgentID   string `json:"agent_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// Zero reports whether both identity fields are empty.
func (c Claimant) Zero() bool {
	return c == (Claimant{})
}

// Matches reports whether c and other are the same principal.
func (c Claimant) Matches(other Claimant) bool {
	return c == other
}

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
	// ClaimantAgentID and ClaimantSessionID are the principal recorded at
	// Claim. ClaimantSet distinguishes that record from a pre-claimant
	// legacy row (both id fields empty and ClaimantSet false).
	ClaimantAgentID   string `json:"claimant_agent_id,omitempty"`
	ClaimantSessionID string `json:"claimant_session_id,omitempty"`
	ClaimantSet       bool   `json:"claimant_set,omitempty"`
}

// ClaimantOf returns the principal stored on rec. The bool is false for
// a legacy row that has not been adopted.
func (rec EffectRecord) ClaimantOf() (Claimant, bool) {
	if !rec.ClaimantSet {
		return Claimant{}, false
	}
	return Claimant{AgentID: rec.ClaimantAgentID, SessionID: rec.ClaimantSessionID}, true
}

// Ledger is the effect-idempotency store. Implementations: SQLiteLedger
// (durable, over effects.db) and MemoryLedger (in-memory, for tests).
type Ledger interface {
	// Claim atomically records the intent to execute the effect identified
	// by key, owned by who. granted=true means this caller owns the effect
	// and must execute it. granted=false means the key already exists:
	// prior carries the existing record (a completed prior => return its
	// receipt as an idempotent no-op). A legacy row with no claimant is
	// adopted by the first non-zero claimant (first claim wins) and still
	// returns granted=false — the effect was already claimed under the old
	// protocol and must not be re-executed. INSERT PK conflict is the
	// atomicity mechanism — no separate check-then-insert. A zero claimant
	// is rejected.
	Claim(ctx context.Context, key string, meta EffectMeta, who Claimant) (granted bool, prior *EffectRecord, err error)
	// RecordReceipt stores the provider outcome and moves claimed ->
	// receipted (executed_at stamped). who must match the claimant that
	// claimed the key. Unknown key, claimant mismatch, or non-claimed
	// state is an error.
	RecordReceipt(ctx context.Context, key string, who Claimant, receipt json.RawMessage) error
	// Complete writes the completion marker: receipted (or claimed) ->
	// completed (completed_at stamped). Idempotent when already completed
	// by the same claimant. who must match the claimant that claimed the key.
	Complete(ctx context.Context, key string, who Claimant) error
	// Abandon marks the effect dead (claimed/receipted -> abandoned).
	// Completed keys cannot be abandoned. who must match the claimant
	// that claimed the key.
	Abandon(ctx context.Context, key string, who Claimant, reason string) error
	// RecordReceiptAdmin, CompleteAdmin, and AbandonAdmin are the explicit
	// operator override. They skip the claimant check. The effects RPC
	// reconciler and the daemon's designated adopter are the only
	// production callers. Agent tool paths must not call them.
	RecordReceiptAdmin(ctx context.Context, key string, receipt json.RawMessage) error
	CompleteAdmin(ctx context.Context, key string) error
	AbandonAdmin(ctx context.Context, key string, reason string) error
	// ReconcilePending returns every record in state claimed or receipted,
	// oldest claimed_at first. Startup and parked-turn resume call this.
	ReconcilePending(ctx context.Context) ([]EffectRecord, error)
	// Get returns one record or (nil, ErrUnknownKey).
	Get(ctx context.Context, key string) (*EffectRecord, error)
	Close() error
}

var ErrUnknownKey = errors.New("effects: unknown effect key")
var ErrInvalidTransition = errors.New("effects: invalid state transition")
var ErrClaimantMismatch = errors.New("effects: claimant mismatch")
var ErrClaimantRequired = errors.New("effects: claimant required")

// Run wraps one external effect in the pinned protocol:
// Claim -> execute -> RecordReceipt. Returns (receipt, reused, err);
// reused=true means a completed prior existed, execute was NOT invoked,
// and receipt is the prior completed receipt (idempotent no-op path).
// Callers then invoke Complete themselves, passing the same claimant.
// On execute error the record stays claimed (reconcile finds it); Run does
// NOT abandon automatically.
func Run(ctx context.Context, l Ledger, key string, meta EffectMeta, who Claimant,
	execute func(ctx context.Context) (json.RawMessage, error)) (receipt json.RawMessage, reused bool, err error) {
	granted, prior, err := l.Claim(ctx, key, meta, who)
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
	if err := l.RecordReceipt(ctx, key, who, out); err != nil {
		return nil, false, fmt.Errorf("effects: record receipt %s: %w", key, err)
	}
	return out, false, nil
}

// authorizeResolve rejects a resolve whose claimant is not the one
// recorded at claim time. Legacy rows (ClaimantSet false) are not owned
// by anyone until the first Claim adopts them, so a resolve against them
// is also a mismatch — the operator override is the admin methods.
func authorizeResolve(rec *EffectRecord, who Claimant) error {
	if rec == nil || !rec.ClaimantSet {
		return fmt.Errorf("effects: effect has no claimant: %w", ErrClaimantMismatch)
	}
	stored := Claimant{AgentID: rec.ClaimantAgentID, SessionID: rec.ClaimantSessionID}
	if !who.Matches(stored) {
		return fmt.Errorf("effects: resolve claimant does not match claim: %w", ErrClaimantMismatch)
	}
	return nil
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
