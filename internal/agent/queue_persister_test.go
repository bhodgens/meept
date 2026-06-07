package agent

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// newTestDB creates an in-memory SQLite database for testing.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newTestDBFile creates a file-based SQLite database with WAL mode for
// tests that need concurrent access.
func newTestDBFile(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.Remove(dbPath)
		os.Remove(dbPath + "-wal")
		os.Remove(dbPath + "-shm")
	})
	return db
}

// newTestPersister creates a QueuePersister with an in-memory DB and short flush delay.
func newTestPersister(t *testing.T, conversationID string, flushDelay time.Duration) *QueuePersister {
	t.Helper()
	db := newTestDB(t)
	p, err := NewQueuePersister(db, conversationID, slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister failed: %v", err)
	}
	t.Cleanup(func() { p.Stop() })
	// Override the flush delay for faster tests.
	if flushDelay > 0 {
		p.flushDelay = flushDelay
	}
	return p
}

func TestQueuePersister_New(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	p, err := NewQueuePersister(db, "conv-123", nil)
	if err != nil {
		t.Fatalf("NewQueuePersister failed: %v", err)
	}
	defer p.Stop()

	if p == nil {
		t.Fatal("NewQueuePersister returned nil")
	}
	if p.conversationID != "conv-123" {
		t.Errorf("conversationID = %q, want %q", p.conversationID, "conv-123")
	}
	if p.flushDelay != defaultFlushDelay {
		t.Errorf("flushDelay = %v, want %v", p.flushDelay, defaultFlushDelay)
	}
	if p.pending == nil {
		t.Error("pending slice should be initialized")
	}
	if len(p.pending) != 0 {
		t.Errorf("pending should be empty, got %d items", len(p.pending))
	}

	// Verify schema was created by checking the table exists.
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='queued_followups'").Scan(&name)
	if err != nil {
		t.Fatalf("queued_followups table not found: %v", err)
	}

	// Verify index was created.
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name='idx_queued_followups_conversation'").Scan(&name)
	if err != nil {
		t.Fatalf("index idx_queued_followups_conversation not found: %v", err)
	}
}

func TestQueuePersister_NewWithNilLogger(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	p, err := NewQueuePersister(db, "conv-nil", nil)
	if err != nil {
		t.Fatalf("NewQueuePersister with nil logger failed: %v", err)
	}
	defer p.Stop()

	if p.logger == nil {
		t.Error("logger should default to slog.Default() when nil is passed")
	}
}

func TestQueuePersister_PersistSync(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-abc", 0)

	msg := QueuedMessage{
		ID:        "msg-1",
		Content:   "hello world",
		QueueType: QueueTypeFollowUp,
		Timestamp: time.Now().UTC(),
		Source:    "user",
	}

	if err := p.PersistSync(msg); err != nil {
		t.Fatalf("PersistSync failed: %v", err)
	}

	// Verify the message is in the database.
	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].ID != "msg-1" {
		t.Errorf("ID = %q, want %q", msgs[0].ID, "msg-1")
	}
	if msgs[0].Content != "hello world" {
		t.Errorf("Content = %q, want %q", msgs[0].Content, "hello world")
	}
	if msgs[0].QueueType != QueueTypeFollowUp {
		t.Errorf("QueueType = %q, want %q", msgs[0].QueueType, QueueTypeFollowUp)
	}
	if msgs[0].Source != "user" {
		t.Errorf("Source = %q, want %q", msgs[0].Source, "user")
	}
}

func TestQueuePersister_PersistSync_Upsert(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-upsert", 0)

	msg := QueuedMessage{
		ID:        "msg-dup",
		Content:   "original",
		QueueType: QueueTypeFollowUp,
		Timestamp: time.Now().UTC(),
		Source:    "user",
	}

	if err := p.PersistSync(msg); err != nil {
		t.Fatalf("first PersistSync failed: %v", err)
	}

	// Same ID, different content (INSERT OR REPLACE).
	msg.Content = "updated"
	if err := p.PersistSync(msg); err != nil {
		t.Fatalf("second PersistSync failed: %v", err)
	}

	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after upsert, got %d", len(msgs))
	}
	if msgs[0].Content != "updated" {
		t.Errorf("Content = %q, want %q (should be updated)", msgs[0].Content, "updated")
	}
}

func TestQueuePersister_PersistSync_ConversationIsolation(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	p1, err := NewQueuePersister(db, "conv-1", nil)
	if err != nil {
		t.Fatalf("NewQueuePersister conv-1 failed: %v", err)
	}
	defer p1.Stop()

	p2, err := NewQueuePersister(db, "conv-2", nil)
	if err != nil {
		t.Fatalf("NewQueuePersister conv-2 failed: %v", err)
	}
	defer p2.Stop()

	msg1 := QueuedMessage{ID: "m1", Content: "a", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"}
	msg2 := QueuedMessage{ID: "m2", Content: "b", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"}

	if err := p1.PersistSync(msg1); err != nil {
		t.Fatalf("p1 PersistSync failed: %v", err)
	}
	if err := p2.PersistSync(msg2); err != nil {
		t.Fatalf("p2 PersistSync failed: %v", err)
	}

	msgs1, _ := p1.LoadPending()
	msgs2, _ := p2.LoadPending()

	if len(msgs1) != 1 || msgs1[0].ID != "m1" {
		t.Errorf("p1 should only see m1, got %v", msgs1)
	}
	if len(msgs2) != 1 || msgs2[0].ID != "m2" {
		t.Errorf("p2 should only see m2, got %v", msgs2)
	}
}

func TestQueuePersister_EnqueueAsync_Buffering(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-buffer", 10*time.Second) // long delay so timer won't fire

	msg := QueuedMessage{
		ID:        "msg-buf-1",
		Content:   "buffered message",
		QueueType: QueueTypeFollowUp,
		Timestamp: time.Now().UTC(),
		Source:    "user",
	}

	p.EnqueueAsync(msg)

	// Message should be buffered, not yet in the database.
	p.mu.Lock()
	pendingCount := len(p.pending)
	p.mu.Unlock()

	if pendingCount != 1 {
		t.Fatalf("expected 1 pending message, got %d", pendingCount)
	}

	// Verify it is NOT in the database yet.
	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 persisted messages (buffered only), got %d", len(msgs))
	}
}

func TestQueuePersister_EnqueueAsync_Multiple(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-multi", 10*time.Second)

	for i := range 5 {
		p.EnqueueAsync(QueuedMessage{
			ID:        fmt.Sprintf("msg-%d", i),
			Content:   fmt.Sprintf("content %d", i),
			QueueType: QueueTypeFollowUp,
			Timestamp: time.Now().UTC(),
			Source:    "user",
		})
	}

	p.mu.Lock()
	count := len(p.pending)
	p.mu.Unlock()

	if count != 5 {
		t.Errorf("expected 5 pending messages, got %d", count)
	}
}

func TestQueuePersister_Flush(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-flush", 10*time.Second)

	messages := []QueuedMessage{
		{ID: "f1", Content: "first", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"},
		{ID: "f2", Content: "second", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"},
		{ID: "f3", Content: "third", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"},
	}

	for _, msg := range messages {
		p.EnqueueAsync(msg)
	}

	// Flush should write all buffered messages.
	p.Flush()

	// Pending buffer should be empty.
	p.mu.Lock()
	count := len(p.pending)
	p.mu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 pending after flush, got %d", count)
	}

	// All messages should now be in the database.
	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 persisted messages, got %d", len(msgs))
	}
	if msgs[0].ID != "f1" || msgs[1].ID != "f2" || msgs[2].ID != "f3" {
		t.Errorf("message order mismatch: got IDs %v", []string{msgs[0].ID, msgs[1].ID, msgs[2].ID})
	}
}

func TestQueuePersister_LoadPending(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-load", 0)

	// Persist some messages directly.
	expected := []QueuedMessage{
		{ID: "l1", Content: "alpha", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "cli"},
		{ID: "l2", Content: "beta", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "web"},
		{ID: "l3", Content: "gamma", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "telegram"},
	}

	for _, msg := range expected {
		if err := p.PersistSync(msg); err != nil {
			t.Fatalf("PersistSync(%s) failed: %v", msg.ID, err)
		}
	}

	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Verify ordering is by created_at ASC.
	for i, msg := range msgs {
		if msg.ID != expected[i].ID {
			t.Errorf("msgs[%d].ID = %q, want %q", i, msg.ID, expected[i].ID)
		}
		if msg.Content != expected[i].Content {
			t.Errorf("msgs[%d].Content = %q, want %q", i, msg.Content, expected[i].Content)
		}
		if msg.Source != expected[i].Source {
			t.Errorf("msgs[%d].Source = %q, want %q", i, msg.Source, expected[i].Source)
		}
		if msg.QueueType != QueueTypeFollowUp {
			t.Errorf("msgs[%d].QueueType = %q, want %q", i, msg.QueueType, QueueTypeFollowUp)
		}
	}
}

func TestQueuePersister_LoadPending_Empty(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-empty-load", 0)

	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending on empty failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestQueuePersister_ClearPending(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-clear", 0)

	// Persist some messages.
	for i := range 3 {
		msg := QueuedMessage{
			ID:        fmt.Sprintf("clr-%d", i),
			Content:   "to be cleared",
			QueueType: QueueTypeFollowUp,
			Timestamp: time.Now().UTC(),
			Source:    "user",
		}
		if err := p.PersistSync(msg); err != nil {
			t.Fatalf("PersistSync failed: %v", err)
		}
	}

	// Verify they exist.
	msgs, _ := p.LoadPending()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages before clear, got %d", len(msgs))
	}

	// Clear them.
	if err := p.ClearPending(); err != nil {
		t.Fatalf("ClearPending failed: %v", err)
	}

	// Verify they are gone.
	msgs, _ = p.LoadPending()
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(msgs))
	}
}

func TestQueuePersister_ClearPending_Empty(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-clear-empty", 0)

	// Clearing when nothing exists should not error.
	if err := p.ClearPending(); err != nil {
		t.Fatalf("ClearPending on empty failed: %v", err)
	}
}

func TestQueuePersister_ClearPending_ConversationIsolation(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)

	p1, _ := NewQueuePersister(db, "conv-x", nil)
	defer p1.Stop()
	p2, _ := NewQueuePersister(db, "conv-y", nil)
	defer p2.Stop()

	_ = p1.PersistSync(QueuedMessage{ID: "x1", Content: "x data", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"})
	_ = p2.PersistSync(QueuedMessage{ID: "y1", Content: "y data", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"})

	// Clear only conv-x.
	if err := p1.ClearPending(); err != nil {
		t.Fatalf("ClearPending failed: %v", err)
	}

	// conv-x should be empty.
	msgs1, _ := p1.LoadPending()
	if len(msgs1) != 0 {
		t.Errorf("conv-x should be empty after clear, got %d", len(msgs1))
	}

	// conv-y should still have its data.
	msgs2, _ := p2.LoadPending()
	if len(msgs2) != 1 {
		t.Errorf("conv-y should still have 1 message, got %d", len(msgs2))
	}
}

func TestQueuePersister_FlushOnTimer(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-timer", 50*time.Millisecond)

	msg := QueuedMessage{
		ID:        "timer-1",
		Content:   "timed flush",
		QueueType: QueueTypeFollowUp,
		Timestamp: time.Now().UTC(),
		Source:    "user",
	}

	p.EnqueueAsync(msg)

	// Wait for the timer to fire and flush.
	time.Sleep(150 * time.Millisecond)

	// The message should now be persisted.
	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after timer flush, got %d", len(msgs))
	}
	if msgs[0].ID != "timer-1" {
		t.Errorf("ID = %q, want %q", msgs[0].ID, "timer-1")
	}
}

func TestQueuePersister_FlushOnTimer_Debounce(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-debounce", 80*time.Millisecond)

	// Enqueue multiple messages rapidly.
	for i := range 5 {
		p.EnqueueAsync(QueuedMessage{
			ID:        fmt.Sprintf("deb-%d", i),
			Content:   "debounce msg",
			QueueType: QueueTypeFollowUp,
			Timestamp: time.Now().UTC(),
			Source:    "user",
		})
		time.Sleep(10 * time.Millisecond) // small gap, but less than flush delay
	}

	// Wait for the final timer to fire.
	time.Sleep(200 * time.Millisecond)

	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected all 5 messages after debounce flush, got %d", len(msgs))
	}
}

func TestQueuePersister_Stop(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	p, err := NewQueuePersister(db, "conv-stop", slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister failed: %v", err)
	}
	// Set long flush delay so it won't auto-flush before we call Stop.
	p.flushDelay = 10 * time.Second

	msg := QueuedMessage{
		ID:        "stop-1",
		Content:   "flushed on stop",
		QueueType: QueueTypeFollowUp,
		Timestamp: time.Now().UTC(),
		Source:    "user",
	}

	p.EnqueueAsync(msg)

	// Stop should flush pending messages.
	p.Stop()

	// Verify the message was persisted.
	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after Stop(), got %d", len(msgs))
	}
	if msgs[0].ID != "stop-1" {
		t.Errorf("ID = %q, want %q", msgs[0].ID, "stop-1")
	}
}

func TestQueuePersister_Stop_Idempotent(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	p, err := NewQueuePersister(db, "conv-stop-idem", slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister failed: %v", err)
	}

	// Multiple stops should not panic.
	p.Stop()
	p.Stop()
	p.Stop()
}

func TestQueuePersister_Recovery(t *testing.T) {
	t.Parallel()

	db := newTestDB(t)
	convID := "conv-recovery"

	// Phase 1: Create persister, persist messages, then simulate shutdown.
	p1, err := NewQueuePersister(db, convID, slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister p1 failed: %v", err)
	}

	original := []QueuedMessage{
		{ID: "rec-1", Content: "recover me", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"},
		{ID: "rec-2", Content: "and me too", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "cli"},
	}

	for _, msg := range original {
		if err := p1.PersistSync(msg); err != nil {
			t.Fatalf("PersistSync failed: %v", err)
		}
	}
	p1.Stop()

	// Phase 2: Simulate daemon restart by creating a new persister with the same DB.
	p2, err := NewQueuePersister(db, convID, slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister p2 failed: %v", err)
	}
	defer p2.Stop()

	// Load the messages that were persisted before the "restart".
	recovered, err := p2.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending failed: %v", err)
	}
	if len(recovered) != 2 {
		t.Fatalf("expected 2 recovered messages, got %d", len(recovered))
	}
	if recovered[0].ID != "rec-1" || recovered[1].ID != "rec-2" {
		t.Errorf("recovery IDs mismatch: got %v", []string{recovered[0].ID, recovered[1].ID})
	}

	// Clear after recovery (simulating the agent consuming the messages).
	if err := p2.ClearPending(); err != nil {
		t.Fatalf("ClearPending failed: %v", err)
	}

	final, _ := p2.LoadPending()
	if len(final) != 0 {
		t.Errorf("expected 0 messages after clear, got %d", len(final))
	}
}

func TestQueuePersister_EmptyOperations(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-empty", 0)

	// Flush when nothing is pending should not error or panic.
	p.Flush()

	// Load when nothing is persisted should return empty (nil or empty slice).
	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("LoadPending on empty failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestQueuePersister_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	// Use a file-based DB with WAL mode for concurrent access.
	db := newTestDBFile(t)
	p, err := NewQueuePersister(db, "conv-concurrent", slog.Default())
	if err != nil {
		t.Fatalf("NewQueuePersister failed: %v", err)
	}
	defer p.Stop()

	var wg sync.WaitGroup
	const writers = 10
	const messagesPerWriter = 20
	var errCount atomic.Int64

	// Concurrent PersistSync calls.
	for w := range writers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := range messagesPerWriter {
				msg := QueuedMessage{
					ID:        fmt.Sprintf("cw-%d-%d", workerID, i),
					Content:   "concurrent msg",
					QueueType: QueueTypeFollowUp,
					Timestamp: time.Now().UTC(),
					Source:    "worker",
				}
				if err := p.PersistSync(msg); err != nil {
					errCount.Add(1)
				}
			}
		}(w)
	}

	// Concurrent EnqueueAsync calls.
	for w := range writers {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := range messagesPerWriter {
				p.EnqueueAsync(QueuedMessage{
					ID:        fmt.Sprintf("ce-%d-%d", workerID, i),
					Content:   "async concurrent msg",
					QueueType: QueueTypeFollowUp,
					Timestamp: time.Now().UTC(),
					Source:    "worker",
				})
			}
		}(w)
	}

	// Concurrent Flush calls.
	for range 5 {
		wg.Go(func() {
			p.Flush()
		})
	}

	// Concurrent LoadPending calls.
	for range 5 {
		wg.Go(func() {
			_, err := p.LoadPending()
			if err != nil {
				errCount.Add(1)
			}
		})
	}

	wg.Wait()

	// Flush any remaining buffered messages (race between Flush goroutines
	// and EnqueueAsync goroutines may leave some in the buffer).
	p.Flush()

	if errs := errCount.Load(); errs > 0 {
		t.Errorf("concurrent operations had %d errors", errs)
	}

	// All messages should be persisted (sync + flushed async).
	msgs, err := p.LoadPending()
	if err != nil {
		t.Fatalf("final LoadPending failed: %v", err)
	}

	expectedCount := writers*messagesPerWriter + writers*messagesPerWriter // sync + async
	if len(msgs) != expectedCount {
		t.Errorf("expected %d total messages, got %d", expectedCount, len(msgs))
	}
}

func TestQueuePersister_Flush_RetryOnFailure(t *testing.T) {
	t.Parallel()

	// This test verifies that Flush with duplicate IDs still works correctly.
	// INSERT OR REPLACE means the second write succeeds and overwrites.
	p := newTestPersister(t, "conv-retry", 0)

	p.EnqueueAsync(QueuedMessage{ID: "dup", Content: "first", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"})
	p.EnqueueAsync(QueuedMessage{ID: "dup", Content: "second", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"})

	p.Flush()

	msgs, _ := p.LoadPending()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (upserted), got %d", len(msgs))
	}
	if msgs[0].Content != "second" {
		t.Errorf("Content = %q, want %q (last write wins)", msgs[0].Content, "second")
	}
}

func TestQueuePersister_WithBus(t *testing.T) {
	t.Parallel()

	p := newTestPersister(t, "conv-bus", 0)

	// WithBus should set the bus field without panicking.
	// We pass nil bus to verify no nil-pointer dereference on publish.
	p.WithBus(nil)

	msg := QueuedMessage{ID: "bus-1", Content: "bus test", QueueType: QueueTypeFollowUp, Timestamp: time.Now().UTC(), Source: "user"}
	if err := p.PersistSync(msg); err != nil {
		t.Fatalf("PersistSync with nil bus failed: %v", err)
	}

	// Should succeed silently (publishPersistedEvent checks for nil bus).
	msgs, _ := p.LoadPending()
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}
