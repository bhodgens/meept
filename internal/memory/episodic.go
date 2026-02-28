package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/caimlas/meept/pkg/sqlite"
)

const (
	// SQL for creating the episodic memories table
	createEpisodicTableSQL = `
CREATE TABLE IF NOT EXISTS episodic_memories (
    id            TEXT PRIMARY KEY,
    content       TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT 'conversation',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL,
    embedding_text TEXT NOT NULL DEFAULT ''
)`

	// SQL for creating the FTS5 virtual table
	createEpisodicFTSSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS episodic_fts
USING fts5(content, category, embedding_text, content='episodic_memories', content_rowid='rowid')`

	// Triggers to keep FTS index in sync
	triggerEpisodicInsert = `
CREATE TRIGGER IF NOT EXISTS episodic_fts_ai AFTER INSERT ON episodic_memories BEGIN
    INSERT INTO episodic_fts(rowid, content, category, embedding_text)
    VALUES (new.rowid, new.content, new.category, new.embedding_text);
END`

	triggerEpisodicDelete = `
CREATE TRIGGER IF NOT EXISTS episodic_fts_ad AFTER DELETE ON episodic_memories BEGIN
    INSERT INTO episodic_fts(episodic_fts, rowid, content, category, embedding_text)
    VALUES ('delete', old.rowid, old.content, old.category, old.embedding_text);
END`

	triggerEpisodicUpdate = `
CREATE TRIGGER IF NOT EXISTS episodic_fts_au AFTER UPDATE ON episodic_memories BEGIN
    INSERT INTO episodic_fts(episodic_fts, rowid, content, category, embedding_text)
    VALUES ('delete', old.rowid, old.content, old.category, old.embedding_text);
    INSERT INTO episodic_fts(rowid, content, category, embedding_text)
    VALUES (new.rowid, new.content, new.category, new.embedding_text);
END`
)

// EpisodicMemory stores and retrieves conversation and interaction history.
// It uses SQLite with FTS5 for full-text search when available, falling back
// to LIKE-based queries when FTS5 is not compiled into SQLite.
type EpisodicMemory struct {
	pool        *sqlite.Pool
	dataDir     string
	initialized bool
	hasFTS5     bool // true if FTS5 is available
	mu          sync.RWMutex
	logger      *slog.Logger
}

// EpisodicConfig holds configuration for episodic memory.
type EpisodicConfig struct {
	// DataDir is the directory for database files.
	DataDir string
	// PoolSize is the number of database connections. Default: 5.
	PoolSize int
	// Logger for operations.
	Logger *slog.Logger
}

// NewEpisodicMemory creates a new episodic memory instance.
func NewEpisodicMemory(cfg EpisodicConfig) *EpisodicMemory {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &EpisodicMemory{
		dataDir: cfg.DataDir,
		logger:  cfg.Logger,
	}
}

// Initialize sets up the database schema and connections.
func (e *EpisodicMemory) Initialize(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.initialized {
		return nil
	}

	dbPath := filepath.Join(e.dataDir, "episodic.db")

	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     dbPath,
		PoolSize: 5,
		WALMode:  true,
		Logger:   e.logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}
	e.pool = pool

	// Initialize schema
	if err := e.initSchema(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	e.initialized = true
	e.logger.Info("Episodic memory initialized", "path", dbPath, "fts5", e.hasFTS5)
	return nil
}

// HasFTS5 returns true if FTS5 full-text search is available.
// When false, search falls back to slower LIKE-based queries.
func (e *EpisodicMemory) HasFTS5() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.hasFTS5
}

// initSchema creates the database tables and indexes.
func (e *EpisodicMemory) initSchema(ctx context.Context) error {
	return e.pool.WithConn(ctx, func(db *sql.DB) error {
		// Create main table first (always required)
		if _, err := db.ExecContext(ctx, createEpisodicTableSQL); err != nil {
			return fmt.Errorf("failed to create episodic table: %w", err)
		}

		// Check if FTS5 is available by attempting to create the virtual table
		_, err := db.ExecContext(ctx, createEpisodicFTSSQL)
		if err != nil {
			// FTS5 not available - log warning and continue without it
			e.logger.Warn("FTS5 not available, using LIKE-based search (slower)",
				"error", err,
				"hint", "Install SQLite with FTS5 support for better search performance",
			)
			e.hasFTS5 = false
			return nil
		}

		// FTS5 is available, create triggers to keep it in sync
		e.hasFTS5 = true
		ftsStatements := []string{
			triggerEpisodicInsert,
			triggerEpisodicDelete,
			triggerEpisodicUpdate,
		}

		for _, stmt := range ftsStatements {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("failed to create FTS trigger: %w", err)
			}
		}
		return nil
	})
}

// Store persists a new episodic memory.
// Returns the unique ID of the stored item.
func (e *EpisodicMemory) Store(ctx context.Context, content string, category string, metadata map[string]any) (string, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return "", errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	id := uuid.New().String()
	nowISO := time.Now().UTC().Format(time.RFC3339)
	metaJSON := (&Memory{Metadata: metadata}).MetadataJSON()
	embeddingText := fmt.Sprintf("%s: %s", category, content)

	_, err := e.pool.Exec(ctx,
		`INSERT INTO episodic_memories (id, content, category, metadata_json, created_at, embedding_text)
         VALUES (?, ?, ?, ?, ?, ?)`,
		id, content, category, metaJSON, nowISO, embeddingText,
	)
	if err != nil {
		return "", fmt.Errorf("failed to store memory: %w", err)
	}

	e.logger.Debug("Stored episodic memory", "id", id, "category", category)
	return id, nil
}

// Search finds episodic memories matching the query.
// Uses FTS5 when available, falls back to LIKE-based queries otherwise.
func (e *EpisodicMemory) Search(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return nil, errors.New("episodic memory not initialized")
	}
	hasFTS5 := e.hasFTS5
	e.mu.RUnlock()

	safeQuery := sqlite.SanitizeQuery(query)
	if safeQuery == "" {
		return e.GetRecent(ctx, limit)
	}

	db, err := e.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer e.pool.Put(db)

	var rows *sql.Rows

	if hasFTS5 {
		// Use FTS5 for efficient full-text search
		rows, err = db.QueryContext(ctx, `
			SELECT
				m.id, m.content, m.category, m.metadata_json, m.created_at,
				f.rank
			FROM episodic_fts f
			JOIN episodic_memories m ON m.rowid = f.rowid
			WHERE episodic_fts MATCH ?
			ORDER BY f.rank
			LIMIT ?
		`, safeQuery, limit)
	} else {
		// Fallback to LIKE-based search (slower but works without FTS5)
		likePattern := "%" + query + "%"
		rows, err = db.QueryContext(ctx, `
			SELECT id, content, category, metadata_json, created_at
			FROM episodic_memories
			WHERE content LIKE ? OR category LIKE ?
			ORDER BY created_at DESC
			LIMIT ?
		`, likePattern, likePattern, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer rows.Close()

	return e.scanResults(rows, hasFTS5)
}

// GetRecent retrieves the most recent episodic memories.
func (e *EpisodicMemory) GetRecent(ctx context.Context, limit int) ([]MemoryResult, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return nil, errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	db, err := e.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer e.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at
		FROM episodic_memories
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent: %w", err)
	}
	defer rows.Close()

	return e.scanResults(rows, false)
}

// GetByCategory retrieves memories filtered to a specific category.
func (e *EpisodicMemory) GetByCategory(ctx context.Context, category string, limit int) ([]MemoryResult, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return nil, errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	db, err := e.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer e.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at
		FROM episodic_memories
		WHERE category = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, category, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get by category: %w", err)
	}
	defer rows.Close()

	return e.scanResults(rows, false)
}

// GetByTimeRange retrieves memories within a time range.
func (e *EpisodicMemory) GetByTimeRange(ctx context.Context, start, end time.Time, limit int) ([]MemoryResult, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return nil, errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	db, err := e.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer e.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at
		FROM episodic_memories
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC
		LIMIT ?
	`, start.Format(time.RFC3339), end.Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get by time range: %w", err)
	}
	defer rows.Close()

	return e.scanResults(rows, false)
}

// GetOldMemories retrieves memories older than the given time.
func (e *EpisodicMemory) GetOldMemories(ctx context.Context, olderThan time.Time, limit int) ([]MemoryResult, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return nil, errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	db, err := e.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer e.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at
		FROM episodic_memories
		WHERE created_at < ?
		ORDER BY created_at ASC
		LIMIT ?
	`, olderThan.Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get old memories: %w", err)
	}
	defer rows.Close()

	return e.scanResults(rows, false)
}

// Delete removes a memory by ID.
func (e *EpisodicMemory) Delete(ctx context.Context, id string) error {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	_, err := e.pool.Exec(ctx, "DELETE FROM episodic_memories WHERE id = ?", id)
	return err
}

// DeleteByIDs removes multiple memories by ID.
func (e *EpisodicMemory) DeleteByIDs(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return 0, errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	// Build query with placeholders
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM episodic_memories WHERE id IN (%s)",
		joinStrings(placeholders, ","))

	result, err := e.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete memories: %w", err)
	}

	deleted, _ := result.RowsAffected()
	return int(deleted), nil
}

// Count returns the total number of episodic memories.
func (e *EpisodicMemory) Count(ctx context.Context) (int, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return 0, errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	var count int
	err := e.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT COUNT(*) FROM episodic_memories").Scan(&count)
	})
	return count, err
}

// GetOldestTimestamp returns the created_at of the oldest memory.
func (e *EpisodicMemory) GetOldestTimestamp(ctx context.Context) (*time.Time, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return nil, errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	var ts sql.NullString
	err := e.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT MIN(created_at) FROM episodic_memories").Scan(&ts)
	})
	if err != nil {
		return nil, err
	}

	if !ts.Valid || ts.String == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, ts.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetNewestTimestamp returns the created_at of the newest memory.
func (e *EpisodicMemory) GetNewestTimestamp(ctx context.Context) (*time.Time, error) {
	e.mu.RLock()
	if !e.initialized {
		e.mu.RUnlock()
		return nil, errors.New("episodic memory not initialized")
	}
	e.mu.RUnlock()

	var ts sql.NullString
	err := e.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT MAX(created_at) FROM episodic_memories").Scan(&ts)
	})
	if err != nil {
		return nil, err
	}

	if !ts.Valid || ts.String == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, ts.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Close releases all resources.
func (e *EpisodicMemory) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		return nil
	}

	e.initialized = false
	if e.pool != nil {
		return e.pool.Close()
	}
	return nil
}

// scanResults scans database rows into MemoryResult slice.
func (e *EpisodicMemory) scanResults(rows *sql.Rows, hasRank bool) ([]MemoryResult, error) {
	var results []MemoryResult

	for rows.Next() {
		var id, content, category, metaJSON, createdAtStr string
		var rank float64

		var err error
		if hasRank {
			err = rows.Scan(&id, &content, &category, &metaJSON, &createdAtStr, &rank)
		} else {
			err = rows.Scan(&id, &content, &category, &metaJSON, &createdAtStr)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		createdAt, _ := time.Parse(time.RFC3339, createdAtStr)

		results = append(results, MemoryResult{
			Memory: Memory{
				ID:        id,
				Content:   content,
				Type:      MemoryTypeEpisodic,
				Category:  category,
				Metadata:  ParseMetadata(metaJSON),
				CreatedAt: createdAt,
			},
			RelevanceScore: sqlite.NormalizeRank(rank),
			Source:         "episodic",
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// joinStrings joins strings with a separator.
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
