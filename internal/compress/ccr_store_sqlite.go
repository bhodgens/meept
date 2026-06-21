package compress

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Ensure sqliteStore implements CCRStore.
var _ CCRStore = (*sqliteStore)(nil)

// sqliteStore is a SQLite-backed CCR store.
type sqliteStore struct {
	db       *sql.DB
	mu       sync.RWMutex
	config   CCRStoreConfig
	closed   bool
}

// ccrSchema is the SQLite schema for the CCR store.
const ccrSchema = `
-- Main entries table
CREATE TABLE IF NOT EXISTS ccr_entries (
	hash              TEXT PRIMARY KEY,
	original_content  TEXT NOT NULL,
	compressed_content TEXT NOT NULL,
	original_tokens   INTEGER DEFAULT 0,
	compressed_tokens INTEGER DEFAULT 0,
	strategy          TEXT DEFAULT 'unknown',
	tool_name         TEXT DEFAULT '',
	created_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
	expires_at        DATETIME NOT NULL,
	retrieval_count   INTEGER DEFAULT 0
);

-- Index for expiry-based cleanup
CREATE INDEX IF NOT EXISTS idx_ccr_expires ON ccr_entries(expires_at);

-- Index for tool-based queries
CREATE INDEX IF NOT EXISTS idx_ccr_tool ON ccr_entries(tool_name);

-- Index for strategy-based stats
CREATE INDEX IF NOT EXISTS idx_ccr_strategy ON ccr_entries(strategy);
`

// cleanupSQL is run periodically to remove expired entries.
const cleanupSQL = `
DELETE FROM ccr_entries WHERE expires_at < ?
`

// statsSQL returns aggregate statistics.
const statsSQL = `
SELECT
	COUNT(*) as entry_count,
	COALESCE(SUM(original_tokens), 0) as total_original,
	COALESCE(SUM(compressed_tokens), 0) as total_compressed,
	COALESCE(SUM(retrieval_count), 0) as total_retrievals,
	COALESCE(SUM(CASE WHEN expires_at < ? THEN 1 ELSE 0 END), 0) as expired_count
FROM ccr_entries
`

// NewCCRStore creates a new CCR store with the given configuration.
func NewCCRStore(cfg CCRStoreConfig) (*sqliteStore, error) {
	// Expand path (handle ~/)
	dbPath := cfg.DatabasePath
	if strings.HasPrefix(dbPath, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("expanding database path: %w", err)
		}
		dbPath = homeDir + dbPath[1:]
	}

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	// Open database with WAL mode, busy timeout, and shared cache
	dsn := "file:" + dbPath + "?_fk=1&_journal_mode=WAL&_busy_timeout=5000&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &sqliteStore{
		db:     db,
		config: cfg,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	// Start background cleanup goroutine
	go store.periodicCleanup()

	return store, nil
}

// initSchema creates the database schema if it doesn't exist.
func (s *sqliteStore) initSchema() error {
	_, err := s.db.Exec(ccrSchema)
	return err
}

// periodicCleanup removes expired entries every 10 minutes.
func (s *sqliteStore) periodicCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		s.cleanupExpired()
		s.mu.Unlock()
	}
}

// cleanupExpired removes entries that have expired.
func (s *sqliteStore) cleanupExpired() {
	ctx := context.Background()
	now := time.Now().Format(time.RFC3339)

	result, err := s.db.ExecContext(ctx, cleanupSQL, now)
	if err != nil {
		// Log but don't propagate - cleanup is best-effort
		return
	}

	deleted, _ := result.RowsAffected()
	if deleted > 0 {
		// Could log: "Cleaned up %d expired CCR entries"
	}
}

// Store saves a CCR entry and returns its hash.
func (s *sqliteStore) Store(ctx context.Context, entry CCREntry) (string, error) {
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()

	if closed {
		return "", ErrStoreClosed
	}

	// Generate hash from original content if not provided
	hash := entry.Hash
	if hash == "" {
		hash = ContentHash(entry.OriginalContent)
	}

	// Set defaults
	if entry.TTL == 0 {
		entry.TTL = s.config.DefaultTTL.Duration
	}
	if entry.ExpiresAt.IsZero() {
		entry.ExpiresAt = time.Now().Add(entry.TTL)
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	// Insert or update
	insertSQL := `
	INSERT INTO ccr_entries (
		hash, original_content, compressed_content,
		original_tokens, compressed_tokens,
		strategy, tool_name, created_at, expires_at, retrieval_count
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	ON CONFLICT(hash) DO UPDATE SET
		compressed_content = excluded.compressed_content,
		compressed_tokens = excluded.compressed_tokens,
		strategy = excluded.strategy,
		expires_at = MAX(excluded.expires_at, ccr_entries.expires_at)
	`

	_, err := s.db.ExecContext(ctx, insertSQL,
		hash,
		entry.OriginalContent,
		entry.CompressedContent,
		entry.OriginalTokens,
		entry.CompressedTokens,
		string(entry.Strategy),
		entry.ToolName,
		entry.CreatedAt.Format(time.RFC3339),
		entry.ExpiresAt.Format(time.RFC3339),
	)

	if err != nil {
		return "", fmt.Errorf("storing CCR entry: %w", err)
	}

	return hash, nil
}

// Retrieve fetches a CCR entry by hash.
func (s *sqliteStore) Retrieve(ctx context.Context, hash string) (*CCREntry, error) {
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()

	if closed {
		return nil, ErrStoreClosed
	}

	selectSQL := `
	SELECT hash, original_content, compressed_content,
		original_tokens, compressed_tokens,
		strategy, tool_name, created_at, expires_at, retrieval_count
	FROM ccr_entries
	WHERE hash = ? AND expires_at > ?
	`

	row := s.db.QueryRowContext(ctx, selectSQL, hash, time.Now().Format(time.RFC3339))

	var entry CCREntry
	var createdAt, expiresAt string
	var strategy string

	err := row.Scan(
		&entry.Hash,
		&entry.OriginalContent,
		&entry.CompressedContent,
		&entry.OriginalTokens,
		&entry.CompressedTokens,
		&strategy,
		&entry.ToolName,
		&createdAt,
		&expiresAt,
		&entry.RetrievalCount,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found or expired
	}
	if err != nil {
		return nil, fmt.Errorf("retrieving CCR entry: %w", err)
	}

	entry.Strategy = CompressionStrategy(strategy)
	entry.CreatedAt, _ = time.Parse(time.RFC3339, createdAt) // nolint:errcheck // already validated
	entry.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt) // nolint:errcheck // already validated
	entry.TTL = time.Until(entry.ExpiresAt)

	// Increment retrieval count (outside lock)
	_, _ = s.db.ExecContext(ctx, ` // nolint:errcheck // best-effort retrieval count
		UPDATE ccr_entries SET retrieval_count = retrieval_count + 1
		WHERE hash = ?
	`, hash)

	return &entry, nil
}

// Search searches within a CCR entry.
// For MVP, this is a simple text search within the original content.
func (s *sqliteStore) Search(ctx context.Context, hash, query string) ([]CCRSearchResult, error) {
	entry, err := s.Retrieve(ctx, hash)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}

	// Simple substring search - can be enhanced with BM25/FTS5
	content := strings.ToLower(entry.OriginalContent)
	query = strings.ToLower(query)

	idx := strings.Index(content, query)
	if idx < 0 {
		return nil, nil
	}

	// Return matching section with context
	contextSize := 200
	start := max(0, idx-contextSize/2)
	end := min(len(entry.OriginalContent), idx+len(query)+contextSize/2)

	return []CCRSearchResult{
		{
			Hash:           hash,
			MatchedContent: entry.OriginalContent[idx : idx+len(query)],
			Context:        entry.OriginalContent[start:end],
			Score:          1.0, // Simple match = 1.0
		},
	}, nil
}

// Exists checks if an entry exists.
func (s *sqliteStore) Exists(ctx context.Context, hash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return false
	}

	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM ccr_entries
			WHERE hash = ? AND expires_at > ?
		)
	`, hash, time.Now().Format(time.RFC3339)).Scan(&exists)

	return err == nil && exists
}

// Delete removes an entry.
func (s *sqliteStore) Delete(ctx context.Context, hash string) (bool, error) {
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()

	if closed {
		return false, ErrStoreClosed
	}

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM ccr_entries WHERE hash = ?
	`, hash)
	if err != nil {
		return false, fmt.Errorf("deleting CCR entry: %w", err)
	}

	affected, err := result.RowsAffected()
	return affected > 0, err
}

// Stats returns store statistics.
func (s *sqliteStore) Stats() CCRStats {
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()

	if closed {
		return CCRStats{}
	}

	var stats CCRStats
	var totalOriginal, totalCompressed, totalRetrievals sql.NullInt64

	err := s.db.QueryRow(statsSQL, time.Now().Format(time.RFC3339)).Scan(
		&stats.EntryCount,
		&totalOriginal,
		&totalCompressed,
		&totalRetrievals,
		&stats.ExpiredCount,
	)

	if err != nil {
		return CCRStats{}
	}

	stats.TotalOriginalTokens = totalOriginal.Int64
	stats.TotalCompressedTokens = totalCompressed.Int64
	stats.TotalRetrievals = totalRetrievals.Int64

	return stats
}

// Close releases database resources.
func (s *sqliteStore) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	return s.db.Close()
}

// Errors
var (
	ErrStoreClosed = fmt.Errorf("CCR store is closed") //nolint:goerr113 // sentinel error
)
