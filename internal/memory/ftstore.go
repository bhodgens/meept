package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/caimlas/meept/pkg/sqlite"
)

// FTSConfig holds configuration for SQLite FTS5 storage.
type FTSConfig struct {
	// TableName is the SQLite table name
	TableName string
	// FTS5Table is the FTS5 virtual table name
	FTS5Table string
	// CategoryField is the field name ("category" or "domain")
	CategoryField string
	// DataDir is the directory for the database file
	DataDir string
	// Schema is the CREATE TABLE statement
	Schema []string
	// Triggers are the FTS sync trigger statements
	Triggers []string
}

// SQLiteFTSStore provides shared SQLite + FTS5 functionality.
// Both EpisodicMemory and TaskMemory embed this to eliminate duplication.
type SQLiteFTSStore struct {
	pool        *sqlite.Pool
	config      FTSConfig
	dataDir     string
	initialized bool
	hasFTS5     bool
	mu          sync.RWMutex
	logger      *slog.Logger
}

// NewSQLiteFTSStore creates a new FTS store.
func NewSQLiteFTSStore(config FTSConfig, logger *slog.Logger) (*SQLiteFTSStore, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &SQLiteFTSStore{
		config:  config,
		dataDir: config.DataDir,
		logger:  logger,
	}, nil
}

// Initialize sets up the database schema and connections.
func (s *SQLiteFTSStore) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil
	}

	dbPath := filepath.Join(s.dataDir, s.config.TableName+".db")

	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     dbPath,
		PoolSize: 5,
		WALMode:  true,
		Logger:   s.logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}
	s.pool = pool

	// Initialize schema
	if err := s.initSchema(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	s.initialized = true
	s.logger.Info("FTS store initialized",
		"table", s.config.TableName,
		"path", dbPath,
		"fts5", s.hasFTS5,
	)
	return nil
}

// HasFTS5 returns true if FTS5 full-text search is available.
func (s *SQLiteFTSStore) HasFTS5() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasFTS5
}

// initSchema creates the database tables and indexes.
func (s *SQLiteFTSStore) initSchema(ctx context.Context) error {
	return s.pool.WithConn(ctx, func(db *sql.DB) error {
		// Create main table
		if len(s.config.Schema) > 0 {
			if _, err := db.ExecContext(ctx, s.config.Schema[0]); err != nil {
				return fmt.Errorf("failed to create main table: %w", err)
			}
		}

		// Try to create FTS5 virtual table
		if len(s.config.Schema) > 1 {
			_, err := db.ExecContext(ctx, s.config.Schema[1])
			if err != nil {
				// FTS5 not available
				s.logger.Warn("FTS5 not available, using LIKE-based search (slower)",
					"error", err,
					"hint", "Install SQLite with FTS5 support for better search performance",
				)
				s.hasFTS5 = false
				return nil
			}

			// FTS5 is available, create triggers
			s.hasFTS5 = true
			for _, trigger := range s.config.Triggers {
				if _, err := db.ExecContext(ctx, trigger); err != nil {
					return fmt.Errorf("failed to create FTS trigger: %w", err)
				}
			}
		} else {
			// No FTS5 schema
			s.hasFTS5 = false
		}

		return nil
	})
}

// GetPool returns the connection pool for custom queries.
func (s *SQLiteFTSStore) GetPool() *sqlite.Pool {
	return s.pool
}

// HasFTS5Public returns whether FTS5 is available.
func (s *SQLiteFTSStore) HasFTS5Public() bool {
	return s.hasFTS5
}

// Store executes a store operation.
func (s *SQLiteFTSStore) Store(ctx context.Context, query string, args ...any) error {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return errors.New("FTS store not initialized")
	}
	s.mu.RUnlock()

	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

// Delete executes a delete operation.
func (s *SQLiteFTSStore) Delete(ctx context.Context, query string, args ...any) error {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return errors.New("FTS store not initialized")
	}
	s.mu.RUnlock()

	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

// DeleteByIDs removes multiple items by ID.
func (s *SQLiteFTSStore) DeleteByIDs(ctx context.Context, tableName string, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return 0, errors.New("FTS store not initialized")
	}
	s.mu.RUnlock()

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE id IN (%s)", tableName, joinStrings(placeholders, ","))
	result, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete items: %w", err)
	}

	deleted, _ := result.RowsAffected()
	return int(deleted), nil
}

// Count returns the total number of items.
func (s *SQLiteFTSStore) Count(ctx context.Context, tableName string) (int, error) {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return 0, errors.New("FTS store not initialized")
	}
	s.mu.RUnlock()

	var count int
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tableName).Scan(&count)
	})
	return count, err
}

// GetOldestTimestamp returns the created_at of the oldest item.
func (s *SQLiteFTSStore) GetOldestTimestamp(ctx context.Context, tableName string) (*time.Time, error) {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return nil, errors.New("FTS store not initialized")
	}
	s.mu.RUnlock()

	var ts sql.NullString
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT MIN(created_at) FROM "+tableName).Scan(&ts)
	})
	if err != nil {
		return nil, err
	}

	if !ts.Valid || ts.String == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339Nano, ts.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetNewestTimestamp returns the created_at of the newest item.
func (s *SQLiteFTSStore) GetNewestTimestamp(ctx context.Context, tableName string) (*time.Time, error) {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return nil, errors.New("FTS store not initialized")
	}
	s.mu.RUnlock()

	var ts sql.NullString
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, "SELECT MAX(created_at) FROM "+tableName).Scan(&ts)
	})
	if err != nil {
		return nil, err
	}

	if !ts.Valid || ts.String == "" {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339Nano, ts.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Close releases all resources.
func (s *SQLiteFTSStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return nil
	}

	s.initialized = false
	if s.pool != nil {
		return s.pool.Close()
	}
	return nil
}

// Ensure SQLiteFTSStore implements io.Closer
var _ io.Closer = (*SQLiteFTSStore)(nil)

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

// FindDuplicateGroups finds groups of items with identical content exceeding the threshold.
func (s *SQLiteFTSStore) FindDuplicateGroups(ctx context.Context, tableName string, thresholdChars int) ([][]string, error) {
	s.mu.RLock()
	if !s.initialized {
		s.mu.RUnlock()
		return nil, errors.New("FTS store not initialized")
	}
	s.mu.RUnlock()

	db, err := s.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer s.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT GROUP_CONCAT(id, ','), content, COUNT(*) as cnt
		FROM `+tableName+`
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

		ids := strings.Split(idsStr, ",")
		groups = append(groups, ids)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

// generateUUID creates a new UUID v4 string.
func generateUUID() string {
	return uuid.New().String()
}

// Initialized returns whether the store has been initialized.
func (s *SQLiteFTSStore) Initialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initialized
}

// ScanRowConfig holds parameters that vary between EpisodicMemory and TaskMemory
// when scanning SQL rows into MemoryResult slices.
type ScanRowConfig struct {
	// MemoryType is the type label (MemoryTypeEpisodic or MemoryTypeTask).
	MemoryType MemoryType
	// SourceFmt is the source label. Use a fixed string like "episodic" or
	// a format string with one %s for the category/domain value.
	SourceFmt string
}

// ScanResults scans database rows into a MemoryResult slice using the shared
// logic previously duplicated across EpisodicMemory and TaskMemory. Each row
// must produce columns: id, content, category_or_domain, metadata_json,
// created_at[, rank].  When hasRank is true an additional float64 rank column
// is expected.
func (s *SQLiteFTSStore) ScanResults(rows *sql.Rows, hasRank bool, cfg ScanRowConfig) ([]MemoryResult, error) {
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

		// Build the source label
		source := cfg.SourceFmt
		if strings.Contains(source, "%") {
			source = fmt.Sprintf(source, category)
		}

		results = append(results, MemoryResult{
			Memory: Memory{
				ID:        id,
				Content:   content,
				Type:      cfg.MemoryType,
				Category:  category,
				Metadata:  ParseMetadata(metaJSON),
				CreatedAt: createdAt,
			},
			RelevanceScore: sqlite.NormalizeRank(rank),
			Source:         source,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
