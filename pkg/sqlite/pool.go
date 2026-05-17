// Package sqlite provides SQLite connection pooling and FTS5 query utilities.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver with FTS5 support
)

// Rows is an interface that both *sql.Rows and *pooledRows implement.
type Rows interface {
	Close() error
	Columns() ([]string, error)
	Err() error
	Next() bool
	Scan(dest ...any) error
}

// Pool manages a pool of SQLite database connections.
type Pool struct {
	dbPath   string
	poolSize int
	conns    chan *sql.DB
	mu       sync.Mutex
	closed   bool
	logger   *slog.Logger
	pragmas  []string
}

// PoolConfig holds configuration for a connection pool.
type PoolConfig struct {
	// Path to the SQLite database file.
	Path string
	// PoolSize is the number of connections to maintain. Default: 5.
	PoolSize int
	// WALMode enables Write-Ahead Logging for better concurrency.
	WALMode bool
	// Logger for pool operations.
	Logger *slog.Logger
}

// DefaultPoolConfig returns a PoolConfig with sensible defaults.
func DefaultPoolConfig(path string) PoolConfig {
	return PoolConfig{
		Path:     path,
		PoolSize: 5,
		WALMode:  true,
		Logger:   slog.Default(),
	}
}

// NewPool creates a new connection pool.
func NewPool(cfg PoolConfig) (*Pool, error) {
	if cfg.Path == "" {
		return nil, errors.New("database path is required")
	}

	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 5
	}

	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Ensure parent directory exists
	dir := filepath.Dir(cfg.Path)
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	pragmas := []string{
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}

	if cfg.WALMode {
		pragmas = append(pragmas, "PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL")
	}

	p := &Pool{
		dbPath:   cfg.Path,
		poolSize: cfg.PoolSize,
		conns:    make(chan *sql.DB, cfg.PoolSize),
		logger:   cfg.Logger,
		pragmas:  pragmas,
	}

	// Pre-create connections
	for range cfg.PoolSize {
		db, err := p.createConn()
		if err != nil {
			// Close any connections we've already created
			p.Close()
			return nil, err
		}
		p.conns <- db
	}

	cfg.Logger.Info("SQLite pool created",
		"path", cfg.Path,
		"pool_size", cfg.PoolSize,
		"wal_mode", cfg.WALMode,
	)

	return p, nil
}

// createConn creates a new database connection with configured pragmas.
func (p *Pool) createConn() (*sql.DB, error) {
	// Use URI mode for more options
	dsn := "file:" + p.dbPath + "?_fk=1&cache=shared"

	db, err := sql.Open("modernc.org/sqlite", dsn)
	if err != nil {
		return nil, err
	}

	// Set connection limits
	db.SetMaxOpenConns(1) // SQLite doesn't support multiple writers
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	// Apply pragmas
	for _, pragma := range p.pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}

	return db, nil
}

// Get retrieves a connection from the pool.
// The caller must return the connection using Put when done.
func (p *Pool) Get(ctx context.Context) (*sql.DB, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("pool is closed")
	}
	p.mu.Unlock()

	select {
	case db := <-p.conns:
		// Test the connection
		if err := db.PingContext(ctx); err != nil {
			// Connection is bad, try to create a new one
			db.Close()
			return p.createConn()
		}
		return db, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Put returns a connection to the pool.
func (p *Pool) Put(db *sql.DB) {
	if db == nil {
		return
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		db.Close()
		return
	}
	p.mu.Unlock()

	select {
	case p.conns <- db:
		// Returned to pool
	default:
		// Pool is full, close the connection
		db.Close()
	}
}

// Close closes all connections in the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	close(p.conns)

	var lastErr error
	for db := range p.conns {
		if err := db.Close(); err != nil {
			lastErr = err
		}
	}

	p.logger.Info("SQLite pool closed", "path", p.dbPath)
	return lastErr
}

// WithConn executes a function with a pooled connection.
// The connection is automatically returned to the pool when done.
func (p *Pool) WithConn(ctx context.Context, fn func(*sql.DB) error) error {
	db, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(db)
	return fn(db)
}

// WithTx executes a function within a transaction.
// The transaction is automatically committed on success or rolled back on error.
func (p *Pool) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	return p.WithConn(ctx, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			return err
		}

		return tx.Commit()
	})
}

// Exec executes a query without returning rows.
func (p *Pool) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	var result sql.Result
	err := p.WithConn(ctx, func(db *sql.DB) error {
		var err error
		result, err = db.ExecContext(ctx, query, args...)
		return err
	})
	return result, err
}

// Query executes a query that returns rows.
func (p *Pool) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	db, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, query, args...) //nolint:rowserrcheck // rows.Err() is checked in pooledRows.Close()
	if err != nil {
		p.Put(db)
		return nil, err
	}

	// Note: caller is responsible for closing rows and returning connection
	// This is a limitation of the pool pattern with sql.Rows
	return &pooledRows{rows: rows, pool: p, db: db}, nil
}

// QueryRow executes a query that returns at most one row.
func (p *Pool) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	db, err := p.Get(ctx)
	if err != nil {
		// Return a row that will error on Scan
		return nil
	}
	defer p.Put(db)
	return db.QueryRowContext(ctx, query, args...)
}

// pooledRows wraps sql.Rows to return the connection when closed.
type pooledRows struct {
	rows *sql.Rows
	pool *Pool
	db   *sql.DB
}

func (r *pooledRows) Close() error {
	closeErr := r.rows.Close()
	if iterErr := r.rows.Err(); iterErr != nil {
		r.pool.Put(r.db)
		return iterErr
	}
	r.pool.Put(r.db)
	return closeErr
}

func (r *pooledRows) Columns() ([]string, error) {
	return r.rows.Columns()
}

func (r *pooledRows) Err() error {
	return r.rows.Err()
}

func (r *pooledRows) Next() bool {
	return r.rows.Next()
}

func (r *pooledRows) Scan(dest ...any) error {
	return r.rows.Scan(dest...)
}

// Size returns the current pool size.
func (p *Pool) Size() int {
	return len(p.conns)
}

// Path returns the database file path.
func (p *Pool) Path() string {
	return p.dbPath
}
