// Package vector provides semantic memory search using vector embeddings.
package vector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	vec.Auto()
}

// ShardStats holds statistics about a vector shard.
type ShardStats struct {
	Dimension      int    `json:"dimension"`
	M              int    `json:"M"`
	EFConstruction int    `json:"ef_construction"`
	EFSearch       int    `json:"ef_search"`
	VectorCount    int64  `json:"vector_count"`
	DatabaseSize   int64  `json:"database_size_bytes"`
	ShardID        string `json:"shard_id"`
}

// VectorShard is an HNSW-powered vector index backed by sqlite-vec.
type VectorShard struct {
	db             *sql.DB
	path           string
	dimension      int
	M              int
	efConstruction int
	efSearch       int
	provider       Provider
	shardID        string
	mu             sync.RWMutex
}

// VectorShardConfig holds configuration for a vector shard.
type VectorShardConfig struct {
	DBPath         string
	Dimension      int
	M              int
	EFConstruction int
	EFSearch       int
	Provider       Provider
	ShardID        string
}

const (
	DefaultM              = 16
	DefaultEFConstruction = 200
	DefaultEFSearch       = 50
)

// NewVectorShard creates a new vector shard.
func NewVectorShard(path string, dimension, M, efConstruction int) (*VectorShard, error) {
	cfg := VectorShardConfig{
		DBPath:         path,
		Dimension:      dimension,
		M:              M,
		EFConstruction: efConstruction,
		EFSearch:       DefaultEFSearch,
		ShardID:        filepath.Base(path),
	}
	return NewVectorShardFromConfig(cfg)
}

// NewVectorShardFromConfig creates a new vector shard from configuration.
func NewVectorShardFromConfig(cfg VectorShardConfig) (*VectorShard, error) {
	if cfg.Dimension <= 0 {
		return nil, fmt.Errorf("dimension must be positive: %d", cfg.Dimension)
	}
	if cfg.M <= 0 {
		cfg.M = DefaultM
	}
	if cfg.EFConstruction <= 0 {
		cfg.EFConstruction = DefaultEFConstruction
	}
	if cfg.EFSearch <= 0 {
		cfg.EFSearch = DefaultEFSearch
	}

	dbPath := cfg.DBPath
	if dbPath != "" && len(dbPath) > 1 && dbPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[1:])
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", filepath.Dir(dbPath), err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer func() {
		if err != nil {
			db.Close()
		}
	}()

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	shard := &VectorShard{
		db:             db,
		path:           dbPath,
		dimension:      cfg.Dimension,
		M:              cfg.M,
		efConstruction: cfg.EFConstruction,
		efSearch:       cfg.EFSearch,
		provider:       cfg.Provider,
		shardID:        cfg.ShardID,
	}

	if err := shard.createIndex(); err != nil {
		return nil, fmt.Errorf("failed to create index: %w", err)
	}

	return shard, nil
}

// OpenExisting opens an existing vector shard at the given path.
func OpenExisting(path string) (*VectorShard, error) {
	dbPath := path
	if len(path) > 1 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, path[1:])
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	var dimension int
	err = db.QueryRow(
		`SELECT dimension FROM vec_info WHERE name = 'embeddings' AND column = 'embedding'`,
	).Scan(&dimension)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to read shard metadata: %w", err)
	}

	shard := &VectorShard{
		db:        db,
		path:      dbPath,
		dimension: dimension,
		M:         DefaultM,
		efSearch:  DefaultEFSearch,
		shardID:   filepath.Base(path),
	}

	return shard, nil
}

// createIndex creates the vec0 virtual table and support tables.
func (s *VectorShard) createIndex() error {
	hnswSQL := fmt.Sprintf("CREATE VIRTUAL TABLE IF NOT EXISTS embeddings USING vec0(embedding float[%d])", s.dimension)
	if _, err := s.db.Exec(hnswSQL); err != nil {
		return fmt.Errorf("failed to create vec0 virtual table: %w", err)
	}

	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS metadata (
			memory_id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			shard_id TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		)
	`); err != nil {
		return fmt.Errorf("failed to create metadata table: %w", err)
	}

	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS embedding_rowids (
			memory_id TEXT PRIMARY KEY,
			rowid     INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("failed to create rowid mapping table: %w", err)
	}

	return nil
}

// Insert adds a vector to the shard with metadata.
func (s *VectorShard) Insert(ctx context.Context, memoryID string, embedding []float32, content string) error {
	if len(embedding) != s.dimension {
		return fmt.Errorf("embedding dimension mismatch: expected %d, got %d", s.dimension, len(embedding))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	vecJSON := JSONFloat32Slice(embedding)

	result, err := tx.ExecContext(ctx, `INSERT INTO embeddings (embedding) VALUES (?)`, vecJSON)
	if err != nil {
		return fmt.Errorf("failed to insert vector: %w", err)
	}

	rowID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get rowid: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO metadata (memory_id, content, shard_id, created_at)
		VALUES (?, ?, ?, datetime('now'))
	`, memoryID, content, s.shardID)
	if err != nil {
		return fmt.Errorf("failed to insert metadata: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO embedding_rowids (memory_id, rowid) VALUES (?, ?)
	`, memoryID, rowID)
	if err != nil {
		return fmt.Errorf("failed to track rowid: %w", err)
	}

	return tx.Commit()
}

// InsertBatch adds multiple vectors in a single transaction.
func (s *VectorShard) InsertBatch(ctx context.Context, memoryIDs []string, embeddings [][]float32, contents []string) error {
	if len(memoryIDs) != len(embeddings) || len(embeddings) != len(contents) {
		return fmt.Errorf("input slice length mismatch: %d, %d, %d", len(memoryIDs), len(embeddings), len(contents))
	}

	if len(embeddings) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	vecStmt, err := tx.PrepareContext(ctx, `INSERT INTO embeddings (embedding) VALUES (?)`)
	if err != nil {
		return fmt.Errorf("prepare vector stmt: %w", err)
	}
	defer vecStmt.Close()

	rowidStmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO embedding_rowids (memory_id, rowid) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare rowid stmt: %w", err)
	}
	defer rowidStmt.Close()

	metaStmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO metadata (memory_id, content, shard_id, created_at) VALUES (?, ?, ?, datetime('now'))`)
	if err != nil {
		return fmt.Errorf("prepare metadata stmt: %w", err)
	}
	defer metaStmt.Close()

	for i := range embeddings {
		if len(embeddings[i]) != s.dimension {
			return fmt.Errorf("embedding dimension mismatch at index %d: expected %d, got %d", i, s.dimension, len(embeddings[i]))
		}

		if _, err := vecStmt.ExecContext(ctx, JSONFloat32Slice(embeddings[i])); err != nil {
			return fmt.Errorf("insert vector at %d: %w", i, err)
		}

		var rowID int64
		err = tx.QueryRow("SELECT last_insert_rowid()").Scan(&rowID)
		if err != nil {
			return fmt.Errorf("get rowid at %d: %w", i, err)
		}

		if _, err := rowidStmt.ExecContext(ctx, memoryIDs[i], rowID); err != nil {
			return fmt.Errorf("track rowid at %d: %w", i, err)
		}

		if _, err := metaStmt.ExecContext(ctx, memoryIDs[i], contents[i], s.shardID); err != nil {
			return fmt.Errorf("insert metadata at %d: %w", i, err)
		}
	}

	return tx.Commit()
}

// Search performs KNN search using the HNSW index.
func (s *VectorShard) Search(ctx context.Context, queryEmbedding []float32, K int, efSearch int) ([]SearchResult, error) {
	if len(queryEmbedding) != s.dimension {
		return nil, fmt.Errorf("query dimension mismatch: expected %d, got %d", s.dimension, len(queryEmbedding))
	}
	if K <= 0 {
		K = 10
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	queryJSON := JSONFloat32Slice(queryEmbedding)

	query := `
		SELECT e.rowid, e.distance, r.memory_id, m.content
		FROM embeddings e
		JOIN embedding_rowids r ON e.rowid = r.rowid
		LEFT JOIN metadata m ON r.memory_id = m.memory_id
		WHERE e.embedding MATCH ? AND k = ?
		ORDER BY e.distance
	`

	rows, err := s.db.QueryContext(ctx, query, queryJSON, K)
	if err != nil {
		return nil, fmt.Errorf("KNN query failed: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var rowID int64
		var distance float32
		var memoryID string
		var content sql.NullString

		if err := rows.Scan(&rowID, &distance, &memoryID, &content); err != nil {
			continue
		}

		results = append(results, SearchResult{
			RawRowID:         rowID,
			MemoryID:         memoryID,
			Content:          content.String,
			VectorSimilarity: cosineSim(float64(distance)),
			RelevanceScore:   cosineSim(float64(distance)),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating KNN results: %w", err)
	}

	if results == nil {
		results = make([]SearchResult, 0)
	}

	return results, nil
}

// Delete removes a vector from the shard by memory_id.
func (s *VectorShard) Delete(ctx context.Context, memoryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var rowID int64
	err = tx.QueryRowContext(ctx, `SELECT rowid FROM embedding_rowids WHERE memory_id = ?`, memoryID).Scan(&rowID)
	if err != nil {
		_ = tx.Commit()
		return nil
	}

	_, _ = tx.ExecContext(ctx, `DELETE FROM embeddings WHERE rowid = ?`, rowID)
	_, _ = tx.ExecContext(ctx, `DELETE FROM embedding_rowids WHERE memory_id = ?`, memoryID)
	_, _ = tx.ExecContext(ctx, `DELETE FROM metadata WHERE memory_id = ?`, memoryID)

	return tx.Commit()
}

// Close closes the underlying database connection.
func (s *VectorShard) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// Stats returns statistics about the vector shard.
func (s *VectorShard) Stats() ShardStats {
	stats := ShardStats{
		Dimension:      s.dimension,
		M:              s.M,
		EFConstruction: s.efConstruction,
		EFSearch:       s.efSearch,
		ShardID:        s.shardID,
	}

	var count int64
	if err := s.db.QueryRow("SELECT count(*) FROM embeddings").Scan(&count); err == nil {
		stats.VectorCount = count
	}

	if info, err := os.Stat(s.path); err == nil {
		stats.DatabaseSize = info.Size()
	}

	return stats
}

func (s *VectorShard) Dimension() int                       { return s.dimension }
func (s *VectorShard) ShardID() string                      { return s.shardID }
func (s *VectorShard) EFSearch() int                        { return s.efSearch }
func (s *VectorShard) SetEFSearch(ef int)                   { s.efSearch = ef }
func (s *VectorShard) WithProvider(p Provider) *VectorShard { s.provider = p; return s }

func JSONFloat32Slice(v []float32) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func cosineSim(distance float64) float64 {
	s := 1.0 - distance
	if s < 0 {
		s = 0
	}
	if s > 1 {
		s = 1
	}
	return s
}
