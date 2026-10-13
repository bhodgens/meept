// Package knowledge implements the opt-in knowledge-asset store (tree 04):
// SQLite pages with an FTS5 full-text index and a link graph between pages.
//
// This store is separate from the evolver wiki (internal/selfimprove):
// separate schema, separate code path, and gated behind
// [memory.knowledge] enabled=false by default. Nothing here reaches the
// inference path except through an explicit knowledge_* tool call.
package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/sqlite"
	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// ErrNotFound is returned when a knowledge page does not exist.
var ErrNotFound = errors.New("knowledge page not found")

const (
	createPagesTableSQL = `
CREATE TABLE IF NOT EXISTS knowledge_pages (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    body         TEXT NOT NULL,
    source       TEXT NOT NULL DEFAULT '',
    ingested_at  TEXT NOT NULL,
    content_hash TEXT NOT NULL DEFAULT '',
    UNIQUE(title, source)
)`

	createLinksTableSQL = `
CREATE TABLE IF NOT EXISTS knowledge_links (
    from_id          TEXT NOT NULL REFERENCES knowledge_pages(id) ON DELETE CASCADE,
    to_id            TEXT NOT NULL DEFAULT '',
    unresolved_title TEXT NOT NULL DEFAULT ''
)`

	createLinksIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_knowledge_links_from ON knowledge_links(from_id)`

	// FTS5 external-content virtual table over title+body, mirroring the
	// episodic/task store pattern (content table + sync triggers).
	createKnowledgeFTSSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts
USING fts5(title, body, content='knowledge_pages', content_rowid='rowid')`

	triggerKnowledgeInsert = `
CREATE TRIGGER IF NOT EXISTS knowledge_fts_ai AFTER INSERT ON knowledge_pages BEGIN
    INSERT INTO knowledge_fts(rowid, title, body)
    VALUES (new.rowid, new.title, new.body);
END`

	triggerKnowledgeDelete = `
CREATE TRIGGER IF NOT EXISTS knowledge_fts_ad AFTER DELETE ON knowledge_pages BEGIN
    INSERT INTO knowledge_fts(knowledge_fts, rowid, title, body)
    VALUES ('delete', old.rowid, old.title, old.body);
END`

	triggerKnowledgeUpdate = `
CREATE TRIGGER IF NOT EXISTS knowledge_fts_au AFTER UPDATE OF title, body ON knowledge_pages BEGIN
    INSERT INTO knowledge_fts(knowledge_fts, rowid, title, body)
    VALUES ('delete', old.rowid, old.title, old.body);
    INSERT INTO knowledge_fts(rowid, title, body)
    VALUES (new.rowid, new.title, new.body);
END`
)

// Store is the SQLite-backed knowledge page store with an FTS5 search index
// and a link graph. Safe for concurrent use.
type Store struct {
	db          *sqlx.DB
	dataDir     string
	maxPages    int
	logger      *slog.Logger
	mu          sync.RWMutex
	initialized bool
}

// New opens (creating if necessary) the knowledge store under dataDir and
// initializes its schema. maxPages bounds the page count at ingest time
// (<= 0 means unbounded). FTS5 absence degrades search to an error-free
// empty result set rather than failing the store.
func New(dataDir string, maxPages int, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil { //nolint:gosec // user config directory permissions
		return nil, fmt.Errorf("create knowledge data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "knowledge.db")
	dsn := "file:" + dbPath + "?_fk=1&_journal_mode=WAL&_busy_timeout=5000"
	rawDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open knowledge database: %w", err)
	}
	db := sqlx.NewDb(rawDB, "sqlite")

	// SQLite writes must be serialized.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	s := &Store{db: db, dataDir: dataDir, maxPages: maxPages, logger: logger}
	if err := s.initSchema(context.Background()); err != nil {
		db.Close() //nolint:errcheck // one-time init cleanup path
		return nil, err
	}

	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()

	logger.Info("knowledge store initialized", "path", dbPath, "max_pages", maxPages)
	return s, nil
}

// initSchema creates the tables, the FTS5 virtual table, and its sync
// triggers. A missing FTS5 module is non-fatal: the store works, search
// returns no hits.
func (s *Store) initSchema(ctx context.Context) error {
	for _, stmt := range []string{
		createPagesTableSQL,
		createLinksTableSQL,
		createLinksIndexSQL,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create knowledge schema: %w", err)
		}
	}

	if _, err := s.db.ExecContext(ctx, createKnowledgeFTSSQL); err != nil {
		s.logger.Warn("fts5 not available, knowledge search disabled",
			"error", err,
			"hint", "install sqlite with fts5 support for knowledge search",
		)
		return nil
	}

	for _, trigger := range []string{
		triggerKnowledgeInsert,
		triggerKnowledgeDelete,
		triggerKnowledgeUpdate,
	} {
		if _, err := s.db.ExecContext(ctx, trigger); err != nil {
			return fmt.Errorf("create knowledge fts trigger: %w", err)
		}
	}
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return nil
	}
	s.initialized = false
	if s.db != nil {
		return s.db.Close() //nolint:mutexio // one-time teardown; initialized flag prevents re-entry
	}
	return nil
}

// checkReady reports whether the store is initialized, releasing the lock
// before any I/O happens (no mutex across I/O).
func (s *Store) checkReady() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.initialized {
		return errors.New("knowledge store not initialized")
	}
	return nil
}

// Count returns the number of stored pages.
func (s *Store) Count(ctx context.Context) (int, error) {
	if err := s.checkReady(); err != nil {
		return 0, err
	}
	var count int
	if err := s.db.QueryRowxContext(ctx, `SELECT COUNT(*) FROM knowledge_pages`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count knowledge pages: %w", err)
	}
	return count, nil
}

// HasFTS5 reports whether the FTS5 index could be created (search works).
func (s *Store) HasFTS5(ctx context.Context) bool {
	if err := s.checkReady(); err != nil {
		return false
	}
	var name string
	err := s.db.QueryRowxContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='knowledge_fts'`).Scan(&name)
	return err == nil
}

// Compile-time guard that pkg/sqlite helpers remain linkable from here
// (snippet/bm25 are used in search.go).
var _ = sqlite.BM25
