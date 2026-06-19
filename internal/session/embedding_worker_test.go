package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"log/slog"
)

// fakeEmbedder is a test double for EmbeddingProvider. It returns fixed-dim
// vectors and records call counts.
type fakeEmbedder struct {
	dim       int
	failNext  atomic.Bool
	callCount atomic.Int64
}

func (f *fakeEmbedder) GenerateEmbeddings(_ context.Context, texts []string) ([][]float32, error) {
	f.callCount.Add(int64(len(texts)))
	if f.failNext.Load() {
		return nil, errors.New("fake embedder failure")
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, f.dim)
		for j := range vec {
			vec[j] = float32(i + 1)
		}
		out[i] = vec
	}
	return out, nil
}

func TestEmbeddingWorker_NilDeps(t *testing.T) {
	t.Parallel()

	// Nil store or embedder should yield nil worker.
	w := NewEmbeddingWorker(context.Background(), nil, nil, nil, EmbeddingWorkerConfig{})
	if w != nil {
		t.Error("expected nil worker when deps are nil")
	}

	// Nil embedder alone.
	w = NewEmbeddingWorker(context.Background(), &SQLiteStore{}, nil, nil, EmbeddingWorkerConfig{})
	if w != nil {
		t.Error("expected nil worker when embedder is nil")
	}

	// Fake embedder but nil store.
	fe := &fakeEmbedder{dim: 8}
	w = NewEmbeddingWorker(context.Background(), nil, fe, nil, EmbeddingWorkerConfig{})
	if w != nil {
		t.Error("expected nil worker when store is nil")
	}
}

func TestEmbeddingWorker_Tick(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available; tick requires session_message_vectors table")
	}

	msgs := []Message{
		{Role: "user", Content: "hello world", EntryType: "message", BranchID: "main"},
		{Role: "assistant", Content: "hi there", EntryType: "message", BranchID: "main"},
	}
	saveTestMessages(t, store, msgs)

	fe := &fakeEmbedder{dim: 768}
	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{Batch: 10, Interval: time.Hour})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	// Call tick directly (not exported via Start, so we invoke the unexported method).
	ctx := context.Background()
	w.tick(ctx)

	if fe.callCount.Load() != 2 {
		t.Errorf("expected embedder called for 2 texts, got %d", fe.callCount.Load())
	}

	// Verify embeddings were stored.
	unembedded, err := store.UnembeddedMessages(ctx, 10)
	if err != nil {
		t.Fatalf("UnembeddedMessages: %v", err)
	}
	if len(unembedded) != 0 {
		t.Errorf("expected 0 unembedded after tick, got %d", len(unembedded))
	}
}

func TestEmbeddingWorker_EmbedderFailure(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	msgs := []Message{
		{Role: "user", Content: "will fail first", EntryType: "message", BranchID: "main"},
	}
	saveTestMessages(t, store, msgs)

	fe := &fakeEmbedder{dim: 768}
	fe.failNext.Store(true) // force failure on first call

	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{Batch: 10, Interval: time.Hour})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	ctx := context.Background()
	// First tick fails; should log but not crash.
	w.tick(ctx)

	// No embeddings stored after failure.
	unembedded, err := store.UnembeddedMessages(ctx, 10)
	if err != nil {
		t.Fatalf("UnembeddedMessages: %v", err)
	}
	if len(unembedded) != 1 {
		t.Errorf("expected 1 unembedded after failed tick, got %d", len(unembedded))
	}

	// Now fix the embedder and tick again; embedding should succeed.
	fe.failNext.Store(false)
	w.tick(ctx)

	unembedded, err = store.UnembeddedMessages(ctx, 10)
	if err != nil {
		t.Fatalf("UnembeddedMessages (second): %v", err)
	}
	if len(unembedded) != 0 {
		t.Errorf("expected 0 unembedded after successful tick, got %d", len(unembedded))
	}
}

func TestEmbeddingWorker_StopCleanly(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	fe := &fakeEmbedder{dim: 768}
	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{
		Batch:    10,
		Interval: 100 * time.Millisecond,
	})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	w.Start()

	// Stop should return promptly (well under 2 seconds).
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop within 2 seconds")
	}
}

// TestEmbeddingWorker_TickNoMessages verifies tick is a no-op when no unembedded
// messages exist (does not call embedder).
func TestEmbeddingWorker_TickNoMessages(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	fe := &fakeEmbedder{dim: 768}
	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{Batch: 10, Interval: time.Hour})

	w.tick(context.Background())

	if fe.callCount.Load() != 0 {
		t.Errorf("expected 0 embedder calls with no messages, got %d", fe.callCount.Load())
	}
}

// TestEmbeddingWorker_StopWithoutStart verifies that calling Stop() without
// Start() returns immediately without blocking (no deadlock waiting on
// w.stopped that will never be closed).
func TestEmbeddingWorker_StopWithoutStart(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	fe := &fakeEmbedder{dim: 768}
	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{
		Batch:    10,
		Interval: time.Hour,
	})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() blocked when Start() was never called")
	}
}

// TestEmbeddingWorker_DoubleStop verifies that calling Stop() twice does not
// panic (double close on stopChan).
func TestEmbeddingWorker_DoubleStop(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	fe := &fakeEmbedder{dim: 768}
	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{
		Batch:    10,
		Interval: time.Hour,
	})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	w.Start()

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Stop()
		// Second call must not panic.
		w.Stop()
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("double Stop() blocked or panicked")
	}
}

// TestEmbeddingWorker_ParentCtxCancellation verifies that the worker's internal
// context is cancelled when the parent context is cancelled, allowing in-flight
// embedding calls to be interrupted on daemon shutdown.
func TestEmbeddingWorker_ParentCtxCancellation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	parentCtx, parentCancel := context.WithCancel(context.Background())
	fe := &fakeEmbedder{dim: 768}
	w := NewEmbeddingWorker(parentCtx, store, fe, slog.Default(), EmbeddingWorkerConfig{
		Batch:    10,
		Interval: 100 * time.Millisecond,
	})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	w.Start()

	// Cancel the parent context — worker should observe and stop the run loop.
	parentCancel()

	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after parent context cancellation")
	}
}

// TestEmbeddingWorker_CatchUpMode verifies that when the unembedded queue is
// large enough to saturate the catch-up batch, tickWithBatch reports catch-up
// mode, and once the queue drains the worker returns to maintenance mode.
func TestEmbeddingWorker_CatchUpMode(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	// Seed 250 messages — more than the default catch-up batch (200) so the
	// first tick should saturate and switch to catch-up mode.
	const total = 250
	msgs := make([]Message, total)
	for i := range msgs {
		msgs[i] = Message{
			Role:      "user",
			Content:   "catch-up payload",
			EntryType: "message",
			BranchID:  "main",
		}
	}
	saveTestMessages(t, store, msgs)

	fe := &fakeEmbedder{dim: 768}
	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{
		Batch:           20,                   // maintenance batch
		Interval:        time.Hour,            // not relevant; we call tick directly
		CatchUpBatch:    200,                  // catch-up batch
		CatchUpInterval: 10 * time.Millisecond, // not relevant for direct tick calls
	})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	ctx := context.Background()

	// First tick processes maintenance batch (20) — not enough to trip catch-up.
	processed := w.tick(ctx)
	if processed != 20 {
		t.Fatalf("first tick: expected 20 processed, got %d", processed)
	}
	if w.mode() != "maintenance" {
		t.Fatalf("after first tick (20 < catchUpBatch=200): expected maintenance mode, got %q", w.mode())
	}

	// Now call tickWithBatch with the catch-up batch (200). After this the
	// remaining queue (230) still has >= 200 entries, so mode should flip.
	processed = w.tickWithBatch(ctx, 200)
	if processed != 200 {
		t.Fatalf("catch-up tick: expected 200 processed, got %d", processed)
	}
	if w.mode() != "catch_up" {
		t.Fatalf("after catch-up tick saturated: expected catch_up mode, got %q", w.mode())
	}

	// Drain: only 30 left (250 - 20 - 200). A catch-up-sized batch returns 30
	// results, which is below catchUpBatch, so mode returns to maintenance.
	processed = w.tickWithBatch(ctx, 200)
	if processed != 30 {
		t.Fatalf("drain tick: expected 30 processed, got %d", processed)
	}
	if w.mode() != "maintenance" {
		t.Fatalf("after drain (30 < catchUpBatch=200): expected maintenance mode, got %q", w.mode())
	}

	// Queue empty: returns 0, stays maintenance.
	processed = w.tickWithBatch(ctx, 200)
	if processed != 0 {
		t.Fatalf("empty tick: expected 0 processed, got %d", processed)
	}
	if w.mode() != "maintenance" {
		t.Fatalf("empty queue: expected maintenance mode, got %q", w.mode())
	}

	// All 250 messages should now be embedded.
	unembedded, err := store.UnembeddedMessages(ctx, 10)
	if err != nil {
		t.Fatalf("UnembeddedMessages: %v", err)
	}
	if len(unembedded) != 0 {
		t.Errorf("expected 0 unembedded remaining, got %d", len(unembedded))
	}
	if fe.callCount.Load() != int64(total) {
		t.Errorf("embedder call count: expected %d, got %d", total, fe.callCount.Load())
	}
}

// TestEmbeddingWorker_MaintenanceMode verifies the worker stays in maintenance
// mode when the queue is consistently smaller than the catch-up batch.
func TestEmbeddingWorker_MaintenanceMode(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	// Seed 5 messages — well below catch-up batch.
	msgs := make([]Message, 5)
	for i := range msgs {
		msgs[i] = Message{
			Role:      "user",
			Content:   "maintenance payload",
			EntryType: "message",
			BranchID:  "main",
		}
	}
	saveTestMessages(t, store, msgs)

	fe := &fakeEmbedder{dim: 768}
	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{
		Batch:           20,
		Interval:        time.Hour,
		CatchUpBatch:    200,
		CatchUpInterval: 10 * time.Millisecond,
	})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	ctx := context.Background()

	processed := w.tick(ctx)
	if processed != 5 {
		t.Fatalf("expected 5 processed, got %d", processed)
	}
	if w.mode() != "maintenance" {
		t.Fatalf("small queue: expected maintenance mode, got %q", w.mode())
	}

	// Subsequent tick on empty queue stays in maintenance.
	processed = w.tick(ctx)
	if processed != 0 {
		t.Fatalf("empty tick: expected 0, got %d", processed)
	}
	if w.mode() != "maintenance" {
		t.Fatalf("empty queue: expected maintenance mode, got %q", w.mode())
	}
}

// TestEmbeddingWorker_DefaultsApplied verifies that zero-valued catch-up fields
// fall back to documented defaults.
func TestEmbeddingWorker_DefaultsApplied(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	defer store.Close()

	if !vec0Available(t, store) {
		t.Skip("sqlite-vec not available")
	}

	fe := &fakeEmbedder{dim: 8}
	w := NewEmbeddingWorker(context.Background(), store, fe, slog.Default(), EmbeddingWorkerConfig{})
	if w == nil {
		t.Fatal("expected non-nil worker")
	}

	if w.batch != 20 {
		t.Errorf("default Batch: expected 20, got %d", w.batch)
	}
	if w.interval != 60*time.Second {
		t.Errorf("default Interval: expected 60s, got %v", w.interval)
	}
	if w.catchUpBatch != 200 {
		t.Errorf("default CatchUpBatch: expected 200, got %d", w.catchUpBatch)
	}
	if w.catchUpInterval != 5*time.Second {
		t.Errorf("default CatchUpInterval: expected 5s, got %v", w.catchUpInterval)
	}
	if w.maintenanceThreshold != 50 {
		t.Errorf("default MaintenanceThreshold: expected 50, got %d", w.maintenanceThreshold)
	}
}
