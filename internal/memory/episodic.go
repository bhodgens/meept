package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

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
    created_at    TEXT NOT NULL
)`

	// SQL for creating the FTS5 virtual table
	createEpisodicFTSSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS episodic_fts
USING fts5(content, category, content='episodic_memories', content_rowid='rowid')`

	// Triggers to keep FTS index in sync
	triggerEpisodicInsert = `
CREATE TRIGGER IF NOT EXISTS episodic_fts_ai AFTER INSERT ON episodic_memories BEGIN
    INSERT INTO episodic_fts(rowid, content, category)
    VALUES (new.rowid, new.content, new.category);
END`

	triggerEpisodicDelete = `
CREATE TRIGGER IF NOT EXISTS episodic_fts_ad AFTER DELETE ON episodic_memories BEGIN
    INSERT INTO episodic_fts(episodic_fts, rowid, content, category)
    VALUES ('delete', old.rowid, old.content, old.category);
END`

	triggerEpisodicUpdate = `
CREATE TRIGGER IF NOT EXISTS episodic_fts_au AFTER UPDATE ON episodic_memories BEGIN
    INSERT INTO episodic_fts(episodic_fts, rowid, content, category)
    VALUES ('delete', old.rowid, old.content, old.category);
    INSERT INTO episodic_fts(rowid, content, category)
    VALUES (new.rowid, new.content, new.category);
END`
)

// EpisodicMemory stores and retrieves conversation and interaction history.
// It uses SQLite with FTS5 for full-text search when available, falling back
// to LIKE-based queries when FTS5 is not compiled into SQLite.
type EpisodicMemory struct {
	store  *SQLiteFTSStore
	logger *slog.Logger
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

	// Create the shared FTS store with episodic-specific config
	storeCfg := FTSConfig{
		TableName:     "episodic_memories",
		FTS5Table:     "episodic_fts",
		CategoryField: "category",
		DataDir:       cfg.DataDir,
		Schema:        []string{createEpisodicTableSQL, createEpisodicFTSSQL},
		Triggers:      []string{triggerEpisodicInsert, triggerEpisodicDelete, triggerEpisodicUpdate},
	}

	store, err := NewSQLiteFTSStore(storeCfg, cfg.Logger)
	if err != nil {
		// This shouldn't fail with current implementation
		panic(fmt.Sprintf("failed to create episodic store: %v", err))
	}

	return &EpisodicMemory{
		store:  store,
		logger: cfg.Logger,
	}
}

// Initialize sets up the database schema and connections.
func (e *EpisodicMemory) Initialize(ctx context.Context) error {
	return e.store.Initialize(ctx)
}

// HasFTS5 returns true if FTS5 full-text search is available.
// When false, search falls back to slower LIKE-based queries.
func (e *EpisodicMemory) HasFTS5() bool {
	return e.store.HasFTS5()
}

// Store persists a new episodic memory.
// Returns the unique ID of the stored item.
func (e *EpisodicMemory) Store(ctx context.Context, content string, category string, metadata map[string]any) (string, error) {
	if !e.store.HasFTS5Public() {
		e.logger.Debug("Storing without FTS5 (slower search)")
	}

	id := generateUUID()
	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	metaJSON := (&Memory{Metadata: metadata}).MetadataJSON()

	err := e.store.Store(ctx,
		`INSERT INTO episodic_memories (id, content, category, metadata_json, created_at)
         VALUES (?, ?, ?, ?, ?)`,
		id, content, category, metaJSON, nowISO,
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
	if !e.store.Initialized() {
		return nil, errors.New("episodic memory not initialized")
	}

	safeQuery := sqlite.SanitizeQuery(query)
	if safeQuery == "" {
		return e.GetRecent(ctx, limit)
	}

	hasFTS5 := e.store.HasFTS5Public()
	pool := e.store.GetPool()

	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

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
	if !e.store.Initialized() {
		return nil, errors.New("episodic memory not initialized")
	}

	pool := e.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

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
	pool := e.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

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
	pool := e.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at
		FROM episodic_memories
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC
		LIMIT ?
	`, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get by time range: %w", err)
	}
	defer rows.Close()

	return e.scanResults(rows, false)
}

// GetOldMemories retrieves memories older than the given time.
func (e *EpisodicMemory) GetOldMemories(ctx context.Context, olderThan time.Time, limit int) ([]MemoryResult, error) {
	pool := e.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at
		FROM episodic_memories
		WHERE created_at < ?
		ORDER BY created_at ASC
		LIMIT ?
	`, olderThan.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get old memories: %w", err)
	}
	defer rows.Close()

	return e.scanResults(rows, false)
}

// Delete removes a memory by ID.
func (e *EpisodicMemory) Delete(ctx context.Context, id string) error {
	return e.store.Delete(ctx, "DELETE FROM episodic_memories WHERE id = ?", id)
}

// DeleteByIDs removes multiple memories by ID.
func (e *EpisodicMemory) DeleteByIDs(ctx context.Context, ids []string) (int, error) {
	return e.store.DeleteByIDs(ctx, "episodic_memories", ids)
}

// Count returns the total number of episodic memories.
func (e *EpisodicMemory) Count(ctx context.Context) (int, error) {
	return e.store.Count(ctx, "episodic_memories")
}

// GetOldestTimestamp returns the created_at of the oldest memory.
func (e *EpisodicMemory) GetOldestTimestamp(ctx context.Context) (*time.Time, error) {
	return e.store.GetOldestTimestamp(ctx, "episodic_memories")
}

// GetNewestTimestamp returns the created_at of the newest memory.
func (e *EpisodicMemory) GetNewestTimestamp(ctx context.Context) (*time.Time, error) {
	return e.store.GetNewestTimestamp(ctx, "episodic_memories")
}

// Close releases all resources.
func (e *EpisodicMemory) Close() error {
	return e.store.Close()
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

		createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)

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

// Ensure EpisodicMemory implements io.Closer
var _ io.Closer = (*EpisodicMemory)(nil)
