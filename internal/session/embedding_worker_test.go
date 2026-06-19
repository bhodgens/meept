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
	w := NewEmbeddingWorker(nil, nil, nil, EmbeddingWorkerConfig{})
	if w != nil {
		t.Error("expected nil worker when deps are nil")
	}

	// Nil embedder alone.
	w = NewEmbeddingWorker(&SQLiteStore{}, nil, nil, EmbeddingWorkerConfig{})
	if w != nil {
		t.Error("expected nil worker when embedder is nil")
	}

	// Fake embedder but nil store.
	fe := &fakeEmbedder{dim: 8}
	w = NewEmbeddingWorker(nil, fe, nil, EmbeddingWorkerConfig{})
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
	w := NewEmbeddingWorker(store, fe, slog.Default(), EmbeddingWorkerConfig{Batch: 10, Interval: time.Hour})
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

	w := NewEmbeddingWorker(store, fe, slog.Default(), EmbeddingWorkerConfig{Batch: 10, Interval: time.Hour})
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
	w := NewEmbeddingWorker(store, fe, slog.Default(), EmbeddingWorkerConfig{
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
	w := NewEmbeddingWorker(store, fe, slog.Default(), EmbeddingWorkerConfig{Batch: 10, Interval: time.Hour})

	w.tick(context.Background())

	if fe.callCount.Load() != 0 {
		t.Errorf("expected 0 embedder calls with no messages, got %d", fe.callCount.Load())
	}
}
