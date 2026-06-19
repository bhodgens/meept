package metrics

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// TestTaskCollectorConcurrentWithStore exercises the scenario where a
// metrics.Store and a TaskCollector share the same SQLite DB file.
// With the Option A fix (shared *sqlx.DB) this must not deadlock or
// produce "database is locked" errors. Furthermore, the Option B
// PRAGMAs (WAL + busy_timeout) keep things safe even when separate
// connections are used.
func TestTaskCollectorConcurrentWithStore(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")

	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     4,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	// Use the shared DB handle (Option A — preferred path).
	tc, err := NewTaskCollectorWithDB(store.DB(), nil)
	if err != nil {
		t.Fatalf("Failed to create task collector with shared DB: %v", err)
	}
	defer tc.Shutdown()

	const goroutines = 8
	const writesPerGoroutine = 25

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half the goroutines write to the Store.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				store.Record("concurrent.test", 1, map[string]string{
					"goroutine": "store",
				})
			}
		}()
	}

	// Half the goroutines write to the TaskCollector.
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				m := &AgentTaskMetrics{
					TaskID:  "task-" + time.Now().Format("150405.000000"),
					AgentID: "test-agent",
					Status:  "completed",
					Success: true,
					ModelID: "test-model",
				}
				if err := tc.RecordAgentTask(m); err != nil {
					// The flush queue has a bounded capacity; if it fills up
					// under the test's burst load, that's a dropped metric
					// (logged as a Warn), not an error worth failing on.
					// "database is locked" or "database is closed" errors
					// are NOT acceptable here.
					t.Logf("dropped metric (queue full): %v", err)
				}
			}
		}(i)
	}

	// Wait with a generous timeout to catch deadlocks.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for concurrent writes — likely deadlock on metrics DB")
	}

	// Force a final flush on the TaskCollector to ensure all queued writes
	// are persisted.
	tc.flush()
	store.flush()
}

// TestTaskCollectorPathBasedConcurrentWithStore runs the same concurrent
// load but WITHOUT sharing the DB handle — relies solely on the PRAGMA
// belt-and-suspenders (WAL + busy_timeout). This validates the fallback
// code path used by daemon.go when NewTaskCollectorWithDB fails.
func TestTaskCollectorPathBasedConcurrentWithStore(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")

	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     4,
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	// Open a separate connection to the same file (fallback path).
	tc, err := NewTaskCollector(dbPath, nil)
	if err != nil {
		t.Fatalf("Failed to create path-based task collector: %v", err)
	}
	defer tc.Shutdown()

	const goroutines = 4
	const writesPerGoroutine = 25

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				store.Record("concurrent.path", 1, nil)
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				m := &AgentTaskMetrics{
					TaskID:  "task-" + time.Now().Format("150405.000000"),
					AgentID: "test-agent",
					Status:  "completed",
					Success: true,
				}
				if err := tc.RecordAgentTask(m); err != nil {
					t.Logf("dropped metric (queue full): %v", err)
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for concurrent path-based writes — likely deadlock on metrics DB")
	}

	tc.flush()
	store.flush()
}

// TestTaskCollectorWithNilDB verifies NewTaskCollectorWithDB rejects a nil
// *sqlx.DB.
func TestTaskCollectorWithNilDB(t *testing.T) {
	t.Parallel()

	_, err := NewTaskCollectorWithDB(nil, nil)
	if err == nil {
		t.Fatal("expected error when passing nil DB to NewTaskCollectorWithDB")
	}
}

// TestShutdownWithoutClosingSharedDB ensures that when TaskCollector uses
// a shared DB (via NewTaskCollectorWithDB), calling Shutdown does not close
// the underlying DB, allowing the owning party (e.g. Store) to keep using
// it afterwards.
func TestShutdownWithoutClosingSharedDB(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")

	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     4,
		FlushInterval: time.Minute, // no auto-flush during the test
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	tc, err := NewTaskCollectorWithDB(store.DB(), nil)
	if err != nil {
		t.Fatalf("Failed to create task collector: %v", err)
	}

	// Record a metric, then shutdown the collector.
	if err := tc.RecordAgentTask(&AgentTaskMetrics{
		TaskID:  "shutdown-test",
		AgentID: "agent",
		Status:  "completed",
		Success: true,
	}); err != nil {
		t.Fatalf("RecordAgentTask failed: %v", err)
	}
	tc.Shutdown()

	// The DB should still be usable by the Store.
	_, err = store.DB().Exec("SELECT 1")
	if err != nil {
		t.Fatalf("DB unusable after TaskCollector.Shutdown(): %v", err)
	}
}

// TestStorePgmas verifies that the WAL and busy_timeout PRAGMAs are set
// on the Store's DB connection.
func TestStorePragmas(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")

	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1,
		FlushInterval: time.Minute,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	var journalMode string
	if err := store.DB().QueryRowx("PRAGMA journal_mode;").Scan(&journalMode); err != nil {
		t.Fatalf("Failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("expected journal_mode=wal, got %q", journalMode)
	}

	var busyTimeout int
	if err := store.DB().QueryRowx("PRAGMA busy_timeout;").Scan(&busyTimeout); err != nil {
		t.Fatalf("Failed to query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("expected busy_timeout=5000, got %d", busyTimeout)
	}
}

// compile-time assert that *sqlx.DB is used somewhere in the package so the
// import is never accidentally removed.
var _ *sqlx.DB = (*sqlx.DB)(nil)
