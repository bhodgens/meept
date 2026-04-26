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
	// SQL for creating the task memories table
	createTaskTableSQL = `
CREATE TABLE IF NOT EXISTS task_memories (
    id            TEXT PRIMARY KEY,
    content       TEXT NOT NULL,
    domain        TEXT NOT NULL DEFAULT 'general',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL
)`

	// SQL for creating the FTS5 virtual table
	createTaskFTSSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS task_fts
USING fts5(content, domain, content='task_memories', content_rowid='rowid')`

	// Triggers to keep FTS index in sync
	triggerTaskInsert = `
CREATE TRIGGER IF NOT EXISTS task_fts_ai AFTER INSERT ON task_memories BEGIN
    INSERT INTO task_fts(rowid, content, domain)
    VALUES (new.rowid, new.content, new.domain);
END`

	triggerTaskDelete = `
CREATE TRIGGER IF NOT EXISTS task_fts_ad AFTER DELETE ON task_memories BEGIN
    INSERT INTO task_fts(task_fts, rowid, content, domain)
    VALUES ('delete', old.rowid, old.content, old.domain);
END`

	triggerTaskUpdate = `
CREATE TRIGGER IF NOT EXISTS task_fts_au AFTER UPDATE ON task_memories BEGIN
    INSERT INTO task_fts(task_fts, rowid, content, domain)
    VALUES ('delete', old.rowid, old.content, old.domain);
    INSERT INTO task_fts(rowid, content, domain)
    VALUES (new.rowid, new.content, new.domain);
END`
)

// TaskMemory stores and retrieves domain-specific technical knowledge.
// It supports multiple domains (e.g., "general", "code", "commands") and
// uses SQLite with FTS5 for full-text search when available, falling back
// to LIKE-based queries when FTS5 is not compiled into SQLite.
type TaskMemory struct {
	store   *SQLiteFTSStore
	domains []string
	logger  *slog.Logger
}

// TaskMemoryConfig holds configuration for task memory.
type TaskMemoryConfig struct {
	// DataDir is the directory for database files.
	DataDir string
	// Domains is the list of knowledge domains to track.
	// Defaults to ["general", "code", "commands"].
	Domains []string
	// PoolSize is the number of database connections. Default: 5.
	PoolSize int
	// Logger for operations.
	Logger *slog.Logger
}

// DefaultTaskMemoryConfig returns configuration with sensible defaults.
func DefaultTaskMemoryConfig(dataDir string) TaskMemoryConfig {
	return TaskMemoryConfig{
		DataDir:  dataDir,
		Domains:  []string{"general", "code", "commands"},
		PoolSize: 5,
		Logger:   slog.Default(),
	}
}

// NewTaskMemory creates a new task memory instance.
func NewTaskMemory(cfg TaskMemoryConfig) (*TaskMemory, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if len(cfg.Domains) == 0 {
		cfg.Domains = []string{"general"}
	}

	// Create the shared FTS store with task-specific config
	storeCfg := FTSConfig{
		TableName:     "task_memories",
		FTS5Table:     "task_fts",
		CategoryField: "domain",
		DataDir:       cfg.DataDir,
		Schema:        []string{createTaskTableSQL, createTaskFTSSQL},
		Triggers:      []string{triggerTaskInsert, triggerTaskDelete, triggerTaskUpdate},
	}

	store, err := NewSQLiteFTSStore(storeCfg, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create task store: %w", err)
	}

	return &TaskMemory{
		store:   store,
		domains: cfg.Domains,
		logger:  cfg.Logger,
	}, nil
}

// Initialize sets up the database schema and connections.
func (t *TaskMemory) Initialize(ctx context.Context) error {
	return t.store.Initialize(ctx)
}

// HasFTS5 returns true if FTS5 full-text search is available.
// When false, search falls back to slower LIKE-based queries.
func (t *TaskMemory) HasFTS5() bool {
	return t.store.HasFTS5()
}

// Store persists a new task memory.
// Returns the unique ID of the stored item.
func (t *TaskMemory) Store(ctx context.Context, content string, domain string, metadata map[string]any) (string, error) {
	if domain == "" {
		domain = "general"
	}

	id := generateUUID()
	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	metaJSON := (&Memory{Metadata: metadata}).MetadataJSON()

	err := t.store.Store(ctx,
		`INSERT INTO task_memories (id, content, domain, metadata_json, created_at)
         VALUES (?, ?, ?, ?, ?)`,
		id, content, domain, metaJSON, nowISO,
	)
	if err != nil {
		return "", fmt.Errorf("failed to store memory: %w", err)
	}

	t.logger.Debug("Stored task memory", "id", id, "domain", domain)
	return id, nil
}

// Search finds task memories matching the query.
// Uses FTS5 when available, falls back to LIKE-based queries otherwise.
// If domain is specified, results are limited to that domain.
func (t *TaskMemory) Search(ctx context.Context, query string, domain string, limit int) ([]MemoryResult, error) {
	if !t.store.Initialized() {
		return nil, errors.New("task memory not initialized")
	}

	safeQuery := sqlite.SanitizeQuery(query)
	if safeQuery == "" {
		return t.GetRecent(ctx, domain, limit)
	}

	hasFTS5 := t.store.HasFTS5Public()
	pool := t.store.GetPool()

	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

	var rows *sql.Rows

	if hasFTS5 {
		// Use FTS5 for efficient full-text search
		if domain != "" {
			rows, err = db.QueryContext(ctx, `
				SELECT
					m.id, m.content, m.domain, m.metadata_json, m.created_at,
					f.rank
				FROM task_fts f
				JOIN task_memories m ON m.rowid = f.rowid
				WHERE task_fts MATCH ? AND m.domain = ?
				ORDER BY f.rank
				LIMIT ?
			`, safeQuery, domain, limit)
		} else {
			rows, err = db.QueryContext(ctx, `
				SELECT
					m.id, m.content, m.domain, m.metadata_json, m.created_at,
					f.rank
				FROM task_fts f
				JOIN task_memories m ON m.rowid = f.rowid
				WHERE task_fts MATCH ?
				ORDER BY f.rank
				LIMIT ?
			`, safeQuery, limit)
		}
	} else {
		// Fallback to LIKE-based search
		likePattern := "%" + query + "%"
		if domain != "" {
			rows, err = db.QueryContext(ctx, `
				SELECT id, content, domain, metadata_json, created_at
				FROM task_memories
				WHERE (content LIKE ? OR domain LIKE ?) AND domain = ?
				ORDER BY created_at DESC
				LIMIT ?
			`, likePattern, likePattern, domain, limit)
		} else {
			rows, err = db.QueryContext(ctx, `
				SELECT id, content, domain, metadata_json, created_at
				FROM task_memories
				WHERE content LIKE ? OR domain LIKE ?
				ORDER BY created_at DESC
				LIMIT ?
			`, likePattern, likePattern, limit)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer rows.Close()

	return t.scanResults(rows, hasFTS5)
}

// GetRecent retrieves the most recent task memories.
// If domain is specified, results are limited to that domain.
func (t *TaskMemory) GetRecent(ctx context.Context, domain string, limit int) ([]MemoryResult, error) {
	if !t.store.Initialized() {
		return nil, errors.New("task memory not initialized")
	}

	pool := t.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

	var rows *sql.Rows
	if domain != "" {
		rows, err = db.QueryContext(ctx, `
			SELECT id, content, domain, metadata_json, created_at
			FROM task_memories
			WHERE domain = ?
			ORDER BY created_at DESC
			LIMIT ?
		`, domain, limit)
	} else {
		rows, err = db.QueryContext(ctx, `
			SELECT id, content, domain, metadata_json, created_at
			FROM task_memories
			ORDER BY created_at DESC
			LIMIT ?
		`, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get recent: %w", err)
	}
	defer rows.Close()

	return t.scanResults(rows, false)
}

// FindDuplicates finds groups of memories with identical content.
// Returns a list of ID groups where each group contains two or more IDs
// sharing the same content. Only groups where content exceeds thresholdChars
// are returned.
func (t *TaskMemory) FindDuplicates(ctx context.Context, thresholdChars int) ([][]string, error) {
	return t.store.FindDuplicateGroups(ctx, "task_memories", thresholdChars)
}

// Delete removes a memory by ID.
func (t *TaskMemory) Delete(ctx context.Context, id string) error {
	return t.store.Delete(ctx, "DELETE FROM task_memories WHERE id = ?", id)
}

// GetByID retrieves a single memory by its ID.
// Returns nil, nil if the memory is not found.
func (t *TaskMemory) GetByID(ctx context.Context, id string) (*MemoryResult, error) {
	if !t.store.Initialized() {
		return nil, errors.New("task memory not initialized")
	}

	pool := t.store.GetPool()
	db, err := pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Put(db)

	row := db.QueryRowContext(ctx, `
		SELECT id, content, domain, metadata_json, created_at
		FROM task_memories
		WHERE id = ?
	`, id)

	var memID, content, domain, metaJSON, createdAtStr string
	err = row.Scan(&memID, &content, &domain, &metaJSON, &createdAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get memory by ID: %w", err)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)
	mem := Memory{
		ID:        memID,
		Content:   content,
		Type:      MemoryTypeTask,
		Category:  domain,
		Metadata:  ParseMetadata(metaJSON),
		CreatedAt: createdAt,
	}

	return &MemoryResult{
		Memory: mem,
		Source: fmt.Sprintf("task:%s", domain),
	}, nil
}

// DeleteByIDs removes multiple memories by ID.
func (t *TaskMemory) DeleteByIDs(ctx context.Context, ids []string) (int, error) {
	return t.store.DeleteByIDs(ctx, "task_memories", ids)
}

// Count returns the total number of task memories.
func (t *TaskMemory) Count(ctx context.Context) (int, error) {
	return t.store.Count(ctx, "task_memories")
}

// GetOldestTimestamp returns the created_at of the oldest memory.
func (t *TaskMemory) GetOldestTimestamp(ctx context.Context) (*time.Time, error) {
	return t.store.GetOldestTimestamp(ctx, "task_memories")
}

// GetNewestTimestamp returns the created_at of the newest memory.
func (t *TaskMemory) GetNewestTimestamp(ctx context.Context) (*time.Time, error) {
	return t.store.GetNewestTimestamp(ctx, "task_memories")
}

// Domains returns the configured domains.
func (t *TaskMemory) Domains() []string {
	return t.domains
}

// Close releases all resources.
func (t *TaskMemory) Close() error {
	return t.store.Close()
}

// scanResults scans database rows into MemoryResult slice.
// Delegates to the shared SQLiteFTSStore.ScanResults implementation.
func (t *TaskMemory) scanResults(rows *sql.Rows, hasRank bool) ([]MemoryResult, error) {
	return t.store.ScanResults(rows, hasRank, ScanRowConfig{
		MemoryType: MemoryTypeTask,
		SourceFmt:  "task:%s",
	})
}

// Ensure TaskMemory implements io.Closer
var _ io.Closer = (*TaskMemory)(nil)
