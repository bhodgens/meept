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
	pool        *sqlite.Pool
	dataDir     string
	domains     []string
	initialized bool
	hasFTS5     bool // true if FTS5 is available
	mu          sync.RWMutex
	logger      *slog.Logger
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
func NewTaskMemory(cfg TaskMemoryConfig) *TaskMemory {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if len(cfg.Domains) == 0 {
		cfg.Domains = []string{"general"}
	}
	return &TaskMemory{
		dataDir: cfg.DataDir,
		domains: cfg.Domains,
		logger:  cfg.Logger,
	}
}

// Initialize sets up the database schema and connections.
func (t *TaskMemory) Initialize(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.initialized {
		return nil
	}

	dbPath := filepath.Join(t.dataDir, "task.db")

	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     dbPath,
		PoolSize: 5,
		WALMode:  true,
		Logger:   t.logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}
	t.pool = pool

	// Initialize schema
	if err := t.initSchema(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	t.initialized = true
	t.logger.Info("Task memory initialized",
		"path", dbPath,
		"domains", t.domains,
		"fts5", t.hasFTS5,
	)
	return nil
}

// HasFTS5 returns true if FTS5 full-text search is available.
// When false, search falls back to slower LIKE-based queries.
func (t *TaskMemory) HasFTS5() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.hasFTS5
}

// initSchema creates the database tables and indexes.
func (t *TaskMemory) initSchema(ctx context.Context) error {
	return t.pool.WithConn(ctx, func(db *sql.DB) error {
		// Create main table first (always required)
		if _, err := db.ExecContext(ctx, createTaskTableSQL); err != nil {
			return fmt.Errorf("failed to create task table: %w", err)
		}

		// Check if FTS5 is available by attempting to create the virtual table
		_, err := db.ExecContext(ctx, createTaskFTSSQL)
		if err != nil {
			// FTS5 not available - log warning and continue without it
			t.logger.Warn("FTS5 not available, using LIKE-based search (slower)",
				"error", err,
				"hint", "Install SQLite with FTS5 support for better search performance",
			)
			t.hasFTS5 = false
			return nil
		}

		// FTS5 is available, create triggers to keep it in sync
		t.hasFTS5 = true
		ftsStatements := []string{
			triggerTaskInsert,
			triggerTaskDelete,
			triggerTaskUpdate,
		}

		for _, stmt := range ftsStatements {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("failed to create FTS trigger: %w", err)
			}
		}
		return nil
	})
}

// Store persists a new task memory.
// Returns the unique ID of the stored item.
func (t *TaskMemory) Store(ctx context.Context, content string, domain string, metadata map[string]any) (string, error) {
	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return "", errors.New("task memory not initialized")
	}
	t.mu.RUnlock()

	if domain == "" {
		domain = "general"
	}

	id := uuid.New().String()
	nowISO := time.Now().UTC().Format(time.RFC3339)
	metaJSON := (&Memory{Metadata: metadata}).MetadataJSON()

	_, err := t.pool.Exec(ctx,
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
	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return nil, errors.New("task memory not initialized")
	}
	hasFTS5 := t.hasFTS5
	t.mu.RUnlock()

	safeQuery := sqlite.SanitizeQuery(query)
	if safeQuery == "" {
		return t.GetRecent(ctx, domain, limit)
	}

	db, err := t.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer t.pool.Put(db)

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
		// Fallback to LIKE-based search (slower but works without FTS5)
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
	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return nil, errors.New("task memory not initialized")
	}
	t.mu.RUnlock()

	db, err := t.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer t.pool.Put(db)

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
	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return nil, errors.New("task memory not initialized")
	}
	t.mu.RUnlock()

	db, err := t.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer t.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT GROUP_CONCAT(id, ','), content, COUNT(*) as cnt
		FROM task_memories
		WHERE LENGTH(content) > ?
		GROUP BY content
		HAVING cnt > 1
	`, thresholdChars)
	if err != nil {
		return nil, fmt.Errorf("failed to find duplicates: %w", err)
	}
	defer rows.Close()

	var groups [][]string
	for rows.Next() {
		var idsStr, content string
		var count int
		if err := rows.Scan(&idsStr, &content, &count); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		ids := splitString(idsStr, ',')
		groups = append(groups, ids)
	}

	return groups, rows.Err()
}

// Delete removes a memory by ID.
func (t *TaskMemory) Delete(ctx context.Context, id string) error {
	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return errors.New("task memory not initialized")
	}
	t.mu.RUnlock()

	_, err := t.pool.Exec(ctx, "DELETE FROM task_memories WHERE id = ?", id)
	return err
}

// DeleteByIDs removes multiple memories by ID.
func (t *TaskMemory) DeleteByIDs(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return 0, errors.New("task memory not initialized")
	}
	t.mu.RUnlock()

	// Build query with placeholders
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM task_memories WHERE id IN (%s)",
		joinStrings(placeholders, ","))

	result, err := t.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete memories: %w", err)
	}

	deleted, _ := result.RowsAffected()
	return int(deleted), nil
}

// Count returns the total number of task memories.
func (t *TaskMemory) Count(ctx context.Context) (int, error) {
	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return 0, errors.New("task memory not initialized")
	}
	t.mu.RUnlock()

	var count int
	err := t.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT COUNT(*) FROM task_memories").Scan(&count)
	})
	return count, err
}

// GetOldestTimestamp returns the created_at of the oldest memory.
func (t *TaskMemory) GetOldestTimestamp(ctx context.Context) (*time.Time, error) {
	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return nil, errors.New("task memory not initialized")
	}
	t.mu.RUnlock()

	var ts sql.NullString
	err := t.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT MIN(created_at) FROM task_memories").Scan(&ts)
	})
	if err != nil {
		return nil, err
	}

	if !ts.Valid || ts.String == "" {
		return nil, nil
	}

	parsedTime, err := time.Parse(time.RFC3339, ts.String)
	if err != nil {
		return nil, err
	}
	return &parsedTime, nil
}

// GetNewestTimestamp returns the created_at of the newest memory.
func (t *TaskMemory) GetNewestTimestamp(ctx context.Context) (*time.Time, error) {
	t.mu.RLock()
	if !t.initialized {
		t.mu.RUnlock()
		return nil, errors.New("task memory not initialized")
	}
	t.mu.RUnlock()

	var ts sql.NullString
	err := t.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT MAX(created_at) FROM task_memories").Scan(&ts)
	})
	if err != nil {
		return nil, err
	}

	if !ts.Valid || ts.String == "" {
		return nil, nil
	}

	parsedTime, err := time.Parse(time.RFC3339, ts.String)
	if err != nil {
		return nil, err
	}
	return &parsedTime, nil
}

// Domains returns the configured domains.
func (t *TaskMemory) Domains() []string {
	return t.domains
}

// Close releases all resources.
func (t *TaskMemory) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.initialized {
		return nil
	}

	t.initialized = false
	if t.pool != nil {
		return t.pool.Close()
	}
	return nil
}

// scanResults scans database rows into MemoryResult slice.
func (t *TaskMemory) scanResults(rows *sql.Rows, hasRank bool) ([]MemoryResult, error) {
	var results []MemoryResult

	for rows.Next() {
		var id, content, domain, metaJSON, createdAtStr string
		var rank float64

		var err error
		if hasRank {
			err = rows.Scan(&id, &content, &domain, &metaJSON, &createdAtStr, &rank)
		} else {
			err = rows.Scan(&id, &content, &domain, &metaJSON, &createdAtStr)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		createdAt, _ := time.Parse(time.RFC3339, createdAtStr)

		results = append(results, MemoryResult{
			Memory: Memory{
				ID:        id,
				Content:   content,
				Type:      MemoryTypeTask,
				Category:  domain,
				Metadata:  ParseMetadata(metaJSON),
				CreatedAt: createdAt,
			},
			RelevanceScore: sqlite.NormalizeRank(rank),
			Source:         fmt.Sprintf("task:%s", domain),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// splitString splits a string by separator.
func splitString(s string, sep rune) []string {
	var result []string
	current := ""
	for _, r := range s {
		if r == sep {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}
