package session

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// newEmbeddingDimStore constructs a SQLiteStore in a temp dir with the given
// options, mirroring testHelper in store_sqlite_test.go but allowing options.
func newEmbeddingDimStore(t *testing.T, opts ...Option) *SQLiteStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_embeddings.db")
	store, err := NewSQLiteStore(dbPath, slog.Default(), opts...)
	if err != nil {
		t.Fatalf("failed to create SQLiteStore: %v", err)
	}
	return store
}

// TestSQLiteStore_WithEmbeddingDim_DefaultIs768 verifies that a store
// constructed with no options uses defaultEmbeddingDim (768), and that a
// 768-dimensional vector can be stored without error.
func TestSQLiteStore_WithEmbeddingDim_DefaultIs768(t *testing.T) {
	t.Parallel()
	store := newEmbeddingDimStore(t)
	defer store.Close()

	if store.embeddingDim != defaultEmbeddingDim {
		t.Fatalf("embeddingDim = %d, want %d", store.embeddingDim, defaultEmbeddingDim)
	}
	if store.embeddingDim != 768 {
		t.Fatalf("expected literal 768, got %d", store.embeddingDim)
	}

	// Storing a 768-dim vector should succeed when the vec0 module is
	// available. We need a valid messageID, so insert a message first.
	msgID := seedMessageForEmbedding(t, store)

	emb := make([]float32, 768)
	for i := range emb {
		emb[i] = float32(i%100) * 0.001
	}
	if err := store.StoreEmbedding(context.Background(), msgID, emb); err != nil {
		// vec0 may be unavailable on some sqlite builds; only dimension
		// validation errors are fatal here. Other errors are skipped.
		if isVec0Unavailable(err) {
			t.Skipf("vec0 module unavailable: %v", err)
		}
		t.Fatalf("StoreEmbedding(768-dim): %v", err)
	}
}

// TestSQLiteStore_WithEmbeddingDim_OverrideTo1536 verifies that
// WithEmbeddingDim(1536) is applied, a 1536-dim vector succeeds, and a 768-dim
// vector fails with "dimension mismatch" (validation occurs before vec0 I/O,
// so this works regardless of vec0 availability).
func TestSQLiteStore_WithEmbeddingDim_OverrideTo1536(t *testing.T) {
	t.Parallel()
	store := newEmbeddingDimStore(t, WithEmbeddingDim(1536))
	defer store.Close()

	if store.embeddingDim != 1536 {
		t.Fatalf("embeddingDim = %d, want 1536", store.embeddingDim)
	}

	msgID := seedMessageForEmbedding(t, store)

	// 1536-dim succeeds (if vec0 is available).
	emb1536 := make([]float32, 1536)
	for i := range emb1536 {
		emb1536[i] = float32(i%100) * 0.001
	}
	if err := store.StoreEmbedding(context.Background(), msgID, emb1536); err != nil {
		if !isVec0Unavailable(err) {
			t.Fatalf("StoreEmbedding(1536-dim): %v", err)
		}
	}

	// 768-dim fails with dimension mismatch. This is validated before the
	// vec0 INSERT, so it always fails regardless of vec0 availability.
	emb768 := make([]float32, 768)
	err := store.StoreEmbedding(context.Background(), msgID, emb768)
	if err == nil {
		t.Fatal("expected dimension mismatch error for 768-dim on a 1536-dim store, got nil")
	}
	if !strings.Contains(err.Error(), "dimension mismatch") {
		t.Errorf("error %q does not contain 'dimension mismatch'", err.Error())
	}
	if !strings.Contains(err.Error(), "got 768") {
		t.Errorf("error %q does not contain 'got 768'", err.Error())
	}
	if !strings.Contains(err.Error(), "want 1536") {
		t.Errorf("error %q does not contain 'want 1536'", err.Error())
	}
}

// TestSQLiteStore_WithEmbeddingDim_NonPositiveIgnored verifies that
// WithEmbeddingDim(0) and WithEmbeddingDim(-1) leave the default unchanged.
func TestSQLiteStore_WithEmbeddingDim_NonPositiveIgnored(t *testing.T) {
	t.Parallel()

	s0 := newEmbeddingDimStore(t, WithEmbeddingDim(0))
	defer s0.Close()
	if s0.embeddingDim != defaultEmbeddingDim {
		t.Errorf("WithEmbeddingDim(0): embeddingDim = %d, want %d", s0.embeddingDim, defaultEmbeddingDim)
	}

	sNeg := newEmbeddingDimStore(t, WithEmbeddingDim(-1))
	defer sNeg.Close()
	if sNeg.embeddingDim != defaultEmbeddingDim {
		t.Errorf("WithEmbeddingDim(-1): embeddingDim = %d, want %d", sNeg.embeddingDim, defaultEmbeddingDim)
	}
}

// TestSQLiteStore_StoreEmbedding_DimensionMismatch verifies the exact error
// message when StoreEmbedding receives a vector of the wrong dimension.
// Dimension validation runs before vec0 I/O so this works without vec0.
func TestSQLiteStore_StoreEmbedding_DimensionMismatch(t *testing.T) {
	t.Parallel()
	store := newEmbeddingDimStore(t)
	defer store.Close()

	msgID := seedMessageForEmbedding(t, store)

	emb := make([]float32, 100)
	err := store.StoreEmbedding(context.Background(), msgID, emb)
	if err == nil {
		t.Fatal("expected dimension mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "dimension mismatch") {
		t.Errorf("error %q does not contain 'dimension mismatch'", err.Error())
	}
	if !strings.Contains(err.Error(), "got 100") {
		t.Errorf("error %q does not contain 'got 100'", err.Error())
	}
	if !strings.Contains(err.Error(), "want 768") {
		t.Errorf("error %q does not contain 'want 768'", err.Error())
	}
}

// TestSQLiteStore_StoreEmbedding_Empty verifies that an empty embedding is
// rejected with an "empty embedding" error.
func TestSQLiteStore_StoreEmbedding_Empty(t *testing.T) {
	t.Parallel()
	store := newEmbeddingDimStore(t)
	defer store.Close()

	// Empty slice must be rejected before any messageID lookup.
	err := store.StoreEmbedding(context.Background(), 1, []float32{})
	if err == nil {
		t.Fatal("expected empty embedding error, got nil")
	}
	if !strings.Contains(err.Error(), "empty embedding") {
		t.Errorf("error %q does not contain 'empty embedding'", err.Error())
	}

	// Nil slice is also treated as empty.
	if err := store.StoreEmbedding(context.Background(), 1, nil); err == nil {
		t.Fatal("expected empty embedding error for nil slice, got nil")
	} else if !strings.Contains(err.Error(), "empty embedding") {
		t.Errorf("nil-slice error %q does not contain 'empty embedding'", err.Error())
	}
}

// seedMessageForEmbedding inserts a session + message and returns the message
// ID. The message ID is needed because StoreEmbedding validates dimension
// before touching the vec0 table, but successful inserts still require a real
// row (foreign-key-style reference is via the vec0 PK, not enforced by vec0,
// but we use a real ID to avoid special-casing).
func seedMessageForEmbedding(t *testing.T, store *SQLiteStore) int64 {
	t.Helper()
	msgs := []Message{
		{Role: "user", Content: "embedding dim test", EntryType: "message", BranchID: "main"},
	}
	saveTestMessages(t, store, msgs)
	saved, err := store.GetMessages(store.GetMostRecent().ID, 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(saved) != 1 {
		t.Fatalf("expected 1 saved message, got %d", len(saved))
	}
	return saved[0].ID
}

// isVec0Unavailable returns true if the error originates from the vec0 virtual
// table being unavailable (e.g. the sqlite-vec extension is not registered in
// this build). This lets tests skip the I/O portion while still exercising the
// dimension validation path, which runs before vec0 is touched.
func isVec0Unavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "session_message_vectors") ||
		strings.Contains(msg, "vec0") ||
		strings.Contains(msg, "no such module") ||
		strings.Contains(msg, "no such table")
}
