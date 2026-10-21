package memory

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingConsolidationBackend counts consolidation passes by counting
// GetOldMemories calls (every Run pass calls it exactly once). All
// methods succeed with empty results so a pass is fast and side-effect
// free.
type countingConsolidationBackend struct {
	calls atomic.Int64
}

func (b *countingConsolidationBackend) GetOldMemories(_ context.Context, _ time.Time, _ int) ([]MemoryResult, error) {
	b.calls.Add(1)
	return nil, nil
}

func (b *countingConsolidationBackend) GetExpiredMemories(_ context.Context, _ int) ([]Memory, error) {
	return nil, nil
}

func (b *countingConsolidationBackend) StoreSummary(_ context.Context, _, _ string, _ map[string]any) (string, error) {
	return "id", nil
}

func (b *countingConsolidationBackend) DeleteByIDs(_ context.Context, ids []string) (int, error) {
	return len(ids), nil
}

func (b *countingConsolidationBackend) FindDuplicates(_ context.Context, _ int) ([][]string, error) {
	return nil, nil
}

func (b *countingConsolidationBackend) StoreExpiredSummary(_ context.Context, _ Memory, _ string) (string, error) {
	return "id", nil
}

func (b *countingConsolidationBackend) DeleteSingle(_ context.Context, _ string) error {
	return nil
}

// periodicFailedHandler counts the "Periodic consolidation failed" error
// records the ticker goroutine logs when Run rejects a concurrent pass.
// With exactly ONE ticker goroutine blocked inside Run, that message can
// never fire; every occurrence proves a duplicate ticker goroutine.
type periodicFailedHandler struct {
	count atomic.Int64
}

func (h *periodicFailedHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *periodicFailedHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError && r.Message == "Periodic consolidation failed" {
		h.count.Add(1)
	}
	return nil
}

func (h *periodicFailedHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *periodicFailedHandler) WithGroup(string) slog.Handler      { return h }

// blockingConsolidationBackend blocks the FIRST Run pass inside
// GetOldMemories until release is closed, signaling entry on entered.
type blockingConsolidationBackend struct {
	entered chan struct{} // buffered cap 1
	release chan struct{}
}

func (b *blockingConsolidationBackend) GetOldMemories(_ context.Context, _ time.Time, _ int) ([]MemoryResult, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return nil, nil
}

func (b *blockingConsolidationBackend) GetExpiredMemories(_ context.Context, _ int) ([]Memory, error) {
	return nil, nil
}

func (b *blockingConsolidationBackend) StoreSummary(_ context.Context, _, _ string, _ map[string]any) (string, error) {
	return "id", nil
}

func (b *blockingConsolidationBackend) DeleteByIDs(_ context.Context, ids []string) (int, error) {
	return len(ids), nil
}

func (b *blockingConsolidationBackend) FindDuplicates(_ context.Context, _ int) ([][]string, error) {
	return nil, nil
}

func (b *blockingConsolidationBackend) StoreExpiredSummary(_ context.Context, _ Memory, _ string) (string, error) {
	return "id", nil
}

func (b *blockingConsolidationBackend) DeleteSingle(_ context.Context, _ string) error {
	return nil
}

func discardConsolidationLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestConsolidator_RestartAfterStop pins the Start→Stop→Start restart
// path: with the old startOnce+stopOnce pair the second Start was a
// silent no-op and the ticker never ran again (permanently single-use
// Consolidator). After Stop the ticker must also stop ticking.
func TestConsolidator_RestartAfterStop(t *testing.T) {
	backend := &countingConsolidationBackend{}
	c := NewConsolidator(ConsolidatorConfig{
		Backend: backend,
		Logger:  discardConsolidationLogger(),
	})
	ctx := context.Background()

	c.StartPeriodicConsolidation(ctx, 5*time.Millisecond, 24)
	waitForConsolidationCount(t, backend, 1, 2*time.Second, "first Start: ticker never ran")

	c.Stop()
	c.Stop() // idempotent: must not panic on a double Stop

	// Ticks must stop shortly after Stop.
	deadline := time.Now().Add(2 * time.Second)
	for {
		before := backend.calls.Load()
		time.Sleep(30 * time.Millisecond)
		if backend.calls.Load() == before {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ticker kept running after Stop")
		}
	}
	afterStop := backend.calls.Load()

	// Restart: the same Consolidator must tick again.
	c.StartPeriodicConsolidation(ctx, 5*time.Millisecond, 24)
	defer c.Stop()
	waitForConsolidationCountAbove(t, backend, afterStop, 2*time.Second,
		"second Start after Stop: ticker never restarted")
}

// TestConsolidator_ConcurrentStartSingleTicker keeps the 185b4d77 guard
// green: a concurrent double-Start must launch exactly ONE ticker
// goroutine. The first pass blocks inside Run, so any duplicate ticker
// goroutine ticks into Run's "already in progress" rejection and logs
// "Periodic consolidation failed" — zero such records proves singleness.
func TestConsolidator_ConcurrentStartSingleTicker(t *testing.T) {
	backend := &blockingConsolidationBackend{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	handler := &periodicFailedHandler{}
	c := NewConsolidator(ConsolidatorConfig{
		Backend: backend,
		Logger:  slog.New(handler),
	})
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.StartPeriodicConsolidation(ctx, time.Millisecond, 24)
		}()
	}
	wg.Wait()

	select {
	case <-backend.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("no consolidation pass started")
	}

	// One goroutine is parked inside Run; a second ticker goroutine would
	// log ~1 error per millisecond here.
	time.Sleep(100 * time.Millisecond)
	close(backend.release)
	c.Stop()

	if n := handler.count.Load(); n != 0 {
		t.Errorf("concurrent Start launched a duplicate ticker goroutine: "+
			"%d 'Periodic consolidation failed' records while one pass was in progress", n)
	}
}

// TestConsolidator_StopWithoutStart pins Stop's safety on a fresh
// Consolidator: idempotent, no panic, and a later Start still works.
func TestConsolidator_StopWithoutStart(t *testing.T) {
	backend := &countingConsolidationBackend{}
	c := NewConsolidator(ConsolidatorConfig{
		Backend: backend,
		Logger:  discardConsolidationLogger(),
	})
	c.Stop()
	c.Stop()

	c.StartPeriodicConsolidation(context.Background(), 5*time.Millisecond, 24)
	defer c.Stop()
	waitForConsolidationCount(t, backend, 1, 2*time.Second,
		"Start after Stop-without-Start: ticker never ran")
}

func waitForConsolidationCount(t *testing.T, b *countingConsolidationBackend, want int64, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for b.calls.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("%s (calls=%d, want>=%d)", msg, b.calls.Load(), want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitForConsolidationCountAbove(t *testing.T, b *countingConsolidationBackend, base int64, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for b.calls.Load() <= base {
		if time.Now().After(deadline) {
			t.Fatalf("%s (calls=%d, base=%d)", msg, b.calls.Load(), base)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
