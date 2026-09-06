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
// concurrent claims of the same key yield exactly one grant.
func (m *MemoryLedger) Claim(ctx context.Context, key string, meta EffectMeta) (bool, *EffectRecord, error) {
	m.mu.Lock()
	if prior, ok := m.records[key]; ok {
		priorCopy := copyRecord(prior)
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
	}
	m.mu.Unlock()
	return true, nil, nil
}

// RecordReceipt implements Ledger: claimed -> receipted.
func (m *MemoryLedger) RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error {
	m.mu.Lock()
	rec, ok := m.records[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("effects: key %s: %w", key, ErrUnknownKey)
	}
	if err := validateTransition(rec.State, "record_receipt"); err != nil {
		m.mu.Unlock()
		return err
	}
	now := time.Now().UTC()
	rec.State = StateReceipted
	rec.ExecutedAt = &now
	rec.Receipt = copyRaw(receipt)
	m.records[key] = rec
	m.mu.Unlock()
	return nil
}

// Complete implements Ledger: claimed/receipted -> completed; idempotent
// on completed.
func (m *MemoryLedger) Complete(ctx context.Context, key string) error {
	m.mu.Lock()
	rec, ok := m.records[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("effects: key %s: %w", key, ErrUnknownKey)
	}
	if err := validateTransition(rec.State, "complete"); err != nil {
		m.mu.Unlock()
		return err
	}
	if rec.State != StateCompleted {
		now := time.Now().UTC()
		rec.State = StateCompleted
		rec.CompletedAt = &now
		m.records[key] = rec
	}
	m.mu.Unlock()
	return nil
}

// Abandon implements Ledger: claimed/receipted -> abandoned.
func (m *MemoryLedger) Abandon(ctx context.Context, key string, reason string) error {
	m.mu.Lock()
	rec, ok := m.records[key]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("effects: key %s: %w", key, ErrUnknownKey)
	}
	if err := validateTransition(rec.State, "abandon"); err != nil {
		m.mu.Unlock()
		return err
	}
	rec.State = StateAbandoned
	rec.AbandonReason = reason
	m.records[key] = rec
	m.mu.Unlock()
	return nil
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
