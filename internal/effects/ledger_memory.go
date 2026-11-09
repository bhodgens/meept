package effects

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemoryLedger is the in-memory Ledger implementation, for tests. It
// satisfies the same Ledger interface and passes the same conformance
// suite as SQLiteLedger. The mutex guards ONLY map access; it is never
// held across any I/O or caller callback (mutexio rule).
type MemoryLedger struct {
	mu      sync.Mutex
	records map[string]EffectRecord
}

// NewMemoryLedger returns an empty in-memory ledger.
func NewMemoryLedger() *MemoryLedger {
	return &MemoryLedger{records: make(map[string]EffectRecord)}
}

// Claim implements Ledger. Insert-under-lock is the atomicity mechanism:
// the map membership check and insert happen in one critical section, so
// concurrent claims of the same key yield exactly one grant. A legacy row
// (no claimant) is adopted by the first non-zero claimant and returned
// as a denied prior — the effect must not be re-executed.
func (m *MemoryLedger) Claim(ctx context.Context, key string, meta EffectMeta, who Claimant) (bool, *EffectRecord, error) {
	if who.Zero() {
		return false, nil, fmt.Errorf("effects: claim %s: %w", key, ErrClaimantRequired)
	}
	m.mu.Lock()
	if prior, ok := m.records[key]; ok {
		if !prior.ClaimantSet {
			prior.ClaimantAgentID = who.AgentID
			prior.ClaimantSessionID = who.SessionID
			prior.ClaimantSet = true
			m.records[key] = prior
		}
		priorCopy := copyRecord(m.records[key])
		m.mu.Unlock()
		return false, &priorCopy, nil
	}
	now := time.Now().UTC()
	m.records[key] = EffectRecord{
		Key:                key,
		State:              StateClaimed,
		TaskID:             meta.TaskID,
		StepID:             meta.StepID,
		SessionID:          meta.SessionID,
		Tool:               meta.Tool,
		ProviderIdempotent: meta.ProviderIdempotent,
		ClaimedAt:          now,
		Payload:            copyRaw(meta.Payload),
		ClaimantAgentID:    who.AgentID,
		ClaimantSessionID:  who.SessionID,
		ClaimantSet:        true,
	}
	m.mu.Unlock()
	return true, nil, nil
}

// RecordReceipt implements Ledger: claimed -> receipted, claimant must match.
func (m *MemoryLedger) RecordReceipt(ctx context.Context, key string, who Claimant, receipt json.RawMessage) error {
	return m.recordReceipt(key, who, false, receipt)
}

// RecordReceiptAdmin implements Ledger. Operator override; skips the claimant check.
func (m *MemoryLedger) RecordReceiptAdmin(ctx context.Context, key string, receipt json.RawMessage) error {
	return m.recordReceipt(key, Claimant{}, true, receipt)
}

func (m *MemoryLedger) recordReceipt(key string, who Claimant, admin bool, receipt json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[key]
	if !ok {
		return fmt.Errorf("effects: key %s: %w", key, ErrUnknownKey)
	}
	if err := authorizeUnlessAdmin(&rec, who, admin); err != nil {
		return err
	}
	if err := validateTransition(rec.State, "record_receipt"); err != nil {
		return err
	}
	now := time.Now().UTC()
	rec.State = StateReceipted
	rec.ExecutedAt = &now
	rec.Receipt = copyRaw(receipt)
	m.records[key] = rec
	return nil
}

// Complete implements Ledger: claimed/receipted -> completed; idempotent
// on completed for the owning claimant.
func (m *MemoryLedger) Complete(ctx context.Context, key string, who Claimant) error {
	return m.complete(key, who, false)
}

// CompleteAdmin implements Ledger. Operator override; skips the claimant check.
func (m *MemoryLedger) CompleteAdmin(ctx context.Context, key string) error {
	return m.complete(key, Claimant{}, true)
}

func (m *MemoryLedger) complete(key string, who Claimant, admin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[key]
	if !ok {
		return fmt.Errorf("effects: key %s: %w", key, ErrUnknownKey)
	}
	if err := authorizeUnlessAdmin(&rec, who, admin); err != nil {
		return err
	}
	if err := validateTransition(rec.State, "complete"); err != nil {
		return err
	}
	if rec.State != StateCompleted {
		now := time.Now().UTC()
		rec.State = StateCompleted
		rec.CompletedAt = &now
		m.records[key] = rec
	}
	return nil
}

// Abandon implements Ledger: claimed/receipted -> abandoned.
func (m *MemoryLedger) Abandon(ctx context.Context, key string, who Claimant, reason string) error {
	return m.abandon(key, who, false, reason)
}

// AbandonAdmin implements Ledger. Operator override; skips the claimant check.
func (m *MemoryLedger) AbandonAdmin(ctx context.Context, key string, reason string) error {
	return m.abandon(key, Claimant{}, true, reason)
}

func (m *MemoryLedger) abandon(key string, who Claimant, admin bool, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.records[key]
	if !ok {
		return fmt.Errorf("effects: key %s: %w", key, ErrUnknownKey)
	}
	if err := authorizeUnlessAdmin(&rec, who, admin); err != nil {
		return err
	}
	if err := validateTransition(rec.State, "abandon"); err != nil {
		return err
	}
	rec.State = StateAbandoned
	rec.AbandonReason = reason
	m.records[key] = rec
	return nil
}

func authorizeUnlessAdmin(rec *EffectRecord, who Claimant, admin bool) error {
	if admin {
		return nil
	}
	return authorizeResolve(rec, who)
}

// ReconcilePending implements Ledger: records in state claimed or
// receipted, oldest claimed_at first.
func (m *MemoryLedger) ReconcilePending(ctx context.Context) ([]EffectRecord, error) {
	m.mu.Lock()
	pending := make([]EffectRecord, 0, len(m.records))
	for _, rec := range m.records {
		if rec.State == StateClaimed || rec.State == StateReceipted {
			pending = append(pending, copyRecord(rec))
		}
	}
	m.mu.Unlock()
	sort.SliceStable(pending, func(i, j int) bool {
		return pending[i].ClaimedAt.Before(pending[j].ClaimedAt)
	})
	return pending, nil
}

// Get implements Ledger.
func (m *MemoryLedger) Get(ctx context.Context, key string) (*EffectRecord, error) {
	m.mu.Lock()
	rec, ok := m.records[key]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("effects: key %s: %w", key, ErrUnknownKey)
	}
	out := copyRecord(rec)
	return &out, nil
}

// Close implements Ledger. A no-op for the in-memory store.
func (m *MemoryLedger) Close() error { return nil }

// copyRecord returns a deep copy: scalars by value, json.RawMessage
// fields as fresh byte slices, so callers mutating a returned record
// cannot corrupt the store.
func copyRecord(rec EffectRecord) EffectRecord {
	out := rec
	out.Payload = copyRaw(rec.Payload)
	out.Receipt = copyRaw(rec.Receipt)
	return out
}

func copyRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	out := make(json.RawMessage, len(raw))
	copy(out, raw)
	return out
}
