package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewPool(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 3,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	if pool.Size() != 3 {
		t.Errorf("Expected pool size 3, got %d", pool.Size())
	}

	if pool.Path() != dbPath {
		t.Errorf("Expected path %s, got %s", dbPath, pool.Path())
	}

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestPoolGetPut(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 2,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Get a connection
	conn1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Failed to get connection: %v", err)
	}

	// Get another connection
	conn2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Failed to get second connection: %v", err)
	}

	// Pool should be empty now
	if pool.Size() != 0 {
		t.Errorf("Expected pool size 0 after getting all connections, got %d", pool.Size())
	}

	// Return connections
	pool.Put(conn1)
	pool.Put(conn2)

	if pool.Size() != 2 {
		t.Errorf("Expected pool size 2 after returning connections, got %d", pool.Size())
	}
}

func TestPoolWithConn(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 2,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Execute a statement
	err = pool.WithConn(ctx, func(db *sql.DB) error {
		_, err := db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
		return err
	})
	if err != nil {
		t.Fatalf("Failed to execute statement: %v", err)
	}

	// Verify table was created
	err = pool.WithConn(ctx, func(db *sql.DB) error {
		_, err := db.Exec("INSERT INTO test (name) VALUES ('hello')")
		return err
	})
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}
}

func TestPoolWithTx(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 2,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Create table
	_, err = pool.Exec(ctx, "CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Successful transaction
	err = pool.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO test (name) VALUES ('row1')")
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO test (name) VALUES ('row2')")
		return err
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}

	// Verify rows were inserted
	var count int
	err = pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRow("SELECT COUNT(*) FROM test").Scan(&count)
	})
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 rows, got %d", count)
	}
}

func TestPoolConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 3,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Create table
	_, err = pool.Exec(ctx, "CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Concurrent inserts
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := pool.Exec(ctx, "INSERT INTO test (name) VALUES (?)", n)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent insert failed: %v", err)
	}

	// Verify all rows were inserted
	var count int
	err = pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRow("SELECT COUNT(*) FROM test").Scan(&count)
	})
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 10 {
		t.Errorf("Expected 10 rows, got %d", count)
	}
}

func TestPoolClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 2,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	// Close the pool
	err = pool.Close()
	if err != nil {
		t.Fatalf("Failed to close pool: %v", err)
	}

	// Try to get a connection after close
	ctx := context.Background()
	_, err = pool.Get(ctx)
	if err == nil {
		t.Error("Expected error getting connection from closed pool")
	}

	// Second close should be safe
	err = pool.Close()
	if err != nil {
		t.Errorf("Second close returned error: %v", err)
	}
}

func TestPoolContextTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 1,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Get the only connection
	conn, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("Failed to get connection: %v", err)
	}
	defer pool.Put(conn)

	// Try to get another with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = pool.Get(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded, got %v", err)
	}
}

func TestPoolExec(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 2,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	ctx := context.Background()

	// Create table using Exec
	result, err := pool.Exec(ctx, "CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// Insert using Exec with args
	result, err = pool.Exec(ctx, "INSERT INTO test (name) VALUES (?)", "hello")
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Errorf("Expected 1 affected row, got %d", affected)
	}
}

func TestPoolPutNil(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	pool, err := NewPool(PoolConfig{
		Path:     dbPath,
		PoolSize: 2,
		WALMode:  true,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	// Putting nil should not panic
	pool.Put(nil)

	// Pool size should be unchanged
	if pool.Size() != 2 {
		t.Errorf("Pool size changed after putting nil")
	}
}

func TestNewPoolErrors(t *testing.T) {
	// Empty path should error
	_, err := NewPool(PoolConfig{
		Path:     "",
		PoolSize: 2,
	})
	if err == nil {
		t.Error("Expected error for empty path")
	}

	// Default pool size should be 5
	tmpDir := t.TempDir()
	pool, err := NewPool(PoolConfig{
		Path:     filepath.Join(tmpDir, "test.db"),
		PoolSize: 0,
	})
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	defer pool.Close()

	if pool.Size() != 5 {
		t.Errorf("Expected default pool size 5, got %d", pool.Size())
	}
}
