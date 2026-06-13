package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/pkg/sqlite"
)

const (
	// SQL for creating the episodic memories table
	createEpisodicTableSQL = `
CREATE TABLE IF NOT EXISTS episodic_memories (
    id               TEXT PRIMARY KEY,
    content          TEXT NOT NULL,
    category         TEXT NOT NULL DEFAULT 'conversation',
    metadata_json    TEXT NOT NULL DEFAULT '{}',
    created_at       TEXT NOT NULL,
    last_accessed_at TEXT NOT NULL DEFAULT '',
    version          INTEGER DEFAULT 1,
    parent_id        TEXT REFERENCES episodic_memories(id),
    is_current       INTEGER DEFAULT 1
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
	// Logger for operations.
	Logger *slog.Logger
}

// NewEpisodicMemory creates a new episodic memory instance.
func NewEpisodicMemory(cfg EpisodicConfig) (*EpisodicMemory, error) {
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
		return nil, fmt.Errorf("failed to create episodic store: %w", err)
	}

	return &EpisodicMemory{
		store:  store,
		logger: cfg.Logger,
	}, nil
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
func (e *EpisodicMemory) Store(ctx context.Context, content, category string, metadata map[string]any) (string, error) {
	if !e.store.HasFTS5Public() {
		e.logger.Debug("Storing without FTS5 (slower search)")
	}

	id := generateUUID()
	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	metaJSON := (&Memory{Metadata: metadata}).MetadataJSON()

	// Extract versioning fields from metadata to populate SQL columns
	var parentID sql.NullString
	var version sql.NullInt64
	var isCurrent sql.NullInt64

	if metadata != nil {
		if pid, ok := metadata["parent_id"].(string); ok && pid != "" {
			parentID = sql.NullString{String: pid, Valid: true}
		}
		if v, ok := metadata["version"]; ok {
			switch n := v.(type) {
			case int:
				version = sql.NullInt64{Int64: int64(n), Valid: true}
			case float64:
				version = sql.NullInt64{Int64: int64(n), Valid: true}
			}
		}
		if ic, ok := metadata["is_current"]; ok {
			switch n := ic.(type) {
			case int:
				isCurrent = sql.NullInt64{Int64: int64(n), Valid: true}
			case float64:
				isCurrent = sql.NullInt64{Int64: int64(n), Valid: true}
			}
		}
	}

	// Default is_current to 1 when not explicitly set
	if !isCurrent.Valid {
		isCurrent = sql.NullInt64{Int64: 1, Valid: true}
	}
	// Default version to 1 when not explicitly set
	if !version.Valid {
		version = sql.NullInt64{Int64: 1, Valid: true}
	}

	err := e.store.Store(ctx,
		`INSERT INTO episodic_memories (id, content, category, metadata_json, created_at, last_accessed_at, version, parent_id, is_current)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, content, category, metaJSON, nowISO, nowISO, version, parentID, isCurrent,
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
	db := e.store.GetDB()

	var rows *sql.Rows
	var err error

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
		escapedQuery := escapeLikeWildcards(query)
		likePattern := "%" + escapedQuery + "%"
		rows, err = db.QueryContext(ctx, `
			SELECT id, content, category, metadata_json, created_at
			FROM episodic_memories
			WHERE content LIKE ? ESCAPE '\' OR category LIKE ? ESCAPE '\'
			ORDER BY created_at DESC
			LIMIT ?
		`, likePattern, likePattern, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}

	results, err := e.scanResults(rows, hasFTS5)
	rows.Close()

	if err != nil {
		return nil, err
	}

	// Update last_accessed_at asynchronously to avoid blocking.
	//nolint:gosec // goroutine outlives request context
	go func() {
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := e.updateLastAccessed(updateCtx, results); err != nil {
			e.logger.Warn("Failed to update last_accessed_at", "error", err)
		}
	}()

	return results, nil
}

// updateLastAccessed updates the last_accessed_at timestamp for retrieved memories.
func (e *EpisodicMemory) updateLastAccessed(ctx context.Context, results []MemoryResult) error {
	if len(results) == 0 {
		return nil
	}

	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.Memory.ID
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids)+1)
	args[0] = nowISO
	for i, id := range ids {
		placeholders[i] = "?"
		args[i+1] = id
	}

	// #nosec G201 -- all values use ? placeholders; IN clause is safe
	//nolint:gosec // parameterized query
	query := fmt.Sprintf("UPDATE episodic_memories SET last_accessed_at = ? WHERE id IN (%s)", strings.Join(placeholders, ","))

	_, err := e.store.GetDB().ExecContext(ctx, query, args...)
	return err
}

// GetRecent retrieves the most recent episodic memories.
func (e *EpisodicMemory) GetRecent(ctx context.Context, limit int) ([]MemoryResult, error) {
	if !e.store.Initialized() {
		return nil, errors.New("episodic memory not initialized")
	}

	rows, err := e.store.GetDB().QueryContext(ctx, `
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
	rows, err := e.store.GetDB().QueryContext(ctx, `
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
	rows, err := e.store.GetDB().QueryContext(ctx, `
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
	rows, err := e.store.GetDB().QueryContext(ctx, `
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

// GetByID retrieves a single memory by its ID.
// Returns nil, nil if the memory is not found.
func (e *EpisodicMemory) GetByID(ctx context.Context, id string) (*MemoryResult, error) {
	if !e.store.Initialized() {
		return nil, errors.New("episodic memory not initialized")
	}

	row := e.store.GetDB().QueryRowxContext(ctx, `
		SELECT id, content, category, metadata_json, created_at
		FROM episodic_memories
		WHERE id = ?
	`, id)

	var memID, content, category, metaJSON, createdAtStr string
	err := row.Scan(&memID, &content, &category, &metaJSON, &createdAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get memory by ID: %w", err)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)
	mem := Memory{
		ID:        memID,
		Content:   content,
		Type:      MemoryTypeEpisodic,
		Category:  category,
		Metadata:  ParseMetadata(metaJSON),
		CreatedAt: createdAt,
	}

	return &MemoryResult{
		Memory: mem,
		Source: "episodic",
	}, nil
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
// Delegates to the shared SQLiteFTSStore.ScanResults implementation.
func (e *EpisodicMemory) scanResults(rows *sql.Rows, hasRank bool) ([]MemoryResult, error) {
	return e.store.ScanResults(rows, hasRank, ScanRowConfig{
		MemoryType: MemoryTypeEpisodic,
		SourceFmt:  "episodic",
	})
}

// Ensure EpisodicMemory implements io.Closer
var _ io.Closer = (*EpisodicMemory)(nil)
