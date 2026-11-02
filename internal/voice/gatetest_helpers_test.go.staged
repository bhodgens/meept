package voice

// Shared test helpers for the gate tests (leaf 00T). Lives in the SAME
// package as the parked tests, so activation order does not matter and the
// implementation leaves never need to define these. No production code.

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/effects"
)

// testLogger is a discard logger for gate tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// newTestLedger builds a memory effects ledger over the repo's real
// internal/effects MemoryLedger.
func newTestLedger(t *testing.T) *effects.MemoryLedger {
	t.Helper()
	return effects.NewMemoryLedger()
}

// captureAudit records audit rows in memory so tests assert the C9 ordering
// (audit row written BEFORE DialOut).
type captureAudit struct {
	mu   sync.Mutex
	rows []map[string]any
}

// AuditWriter is the narrow seam leaf 11 injects (internal/voice AuditWriter).
// The fake satisfies it structurally.
func (a *captureAudit) WriteAudit(_ context.Context, row map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rows = append(a.rows, row)
	return nil
}

func (a *captureAudit) rowsCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.rows)
}

// captureLedger wraps the memory ledger so tests can assert no leaked
// reservation after a failed dial (C9: refused/failed dial completes the
// effect with its outcome).
type captureLedger struct {
	inner *effects.MemoryLedger
	mu    sync.Mutex
	keys  []string
}

func newCaptureLedger(t *testing.T) *captureLedger {
	t.Helper()
	return &captureLedger{inner: effects.NewMemoryLedger()}
}

func (c *captureLedger) Claim(ctx context.Context, key string, meta effects.EffectMeta) (bool, *effects.EffectRecord, error) {
	c.mu.Lock()
	c.keys = append(c.keys, key)
	c.mu.Unlock()
	return c.inner.Claim(ctx, key, meta)
}

func (c *captureLedger) RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error {
	return c.inner.RecordReceipt(ctx, key, receipt)
}

func (c *captureLedger) Complete(ctx context.Context, key string) error {
	return c.inner.Complete(ctx, key)
}

func (c *captureLedger) Abandon(ctx context.Context, key string, reason string) error {
	return c.inner.Abandon(ctx, key, reason)
}

func (c *captureLedger) ReconcilePending(ctx context.Context) ([]effects.EffectRecord, error) {
	return c.inner.ReconcilePending(ctx)
}

func (c *captureLedger) Get(ctx context.Context, key string) (*effects.EffectRecord, error) {
	return c.inner.Get(ctx, key)
}

func (c *captureLedger) Close() error { return c.inner.Close() }

// pending returns the count of reservations still in a non-terminal state
// (leaked if the dial failed but the effect was never completed).
func (c *captureLedger) pending() int {
	recs, err := c.inner.ReconcilePending(context.Background())
	if err != nil {
		return -1
	}
	return len(recs)
}
