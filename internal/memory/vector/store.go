// Package vector provides semantic memory search using vector embeddings.
package vector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// SearchResult represents a vector similarity search result.
type SearchResult struct {
	MemoryID         string
	Content          string
	Metadata         map[string]any
	RelevanceScore   float64
	VectorSimilarity float64
}

// Store stores and retrieves embeddings for memories.
type Store struct {
	db            *sql.DB
	provider      Provider
	embeddingCache sync.Map // map[string][]float32
	mu            sync.RWMutex
}

// StoreConfig holds configuration for the vector store.
type StoreConfig struct {
	DBPath   string
	Provider Provider
}

// NewStore creates a new vector store.
func NewStore(cfg StoreConfig) (*Store, error) {
	// Expand home directory
	dbPath := cfg.DBPath
	if len(dbPath) > 0 && dbPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		dbPath = filepath.Join(home, dbPath[1:])
	}

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	s := &Store{
		db:       db,
		provider: cfg.Provider,
	}

	if err := s.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// initialize creates the database schema.
func (s *Store) initialize() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS embeddings (
			id         TEXT PRIMARY KEY,
			vector     BLOB NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS metadata (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			memory_id  TEXT NOT NULL,
			key        TEXT NOT NULL,
			value      TEXT,
			UNIQUE(memory_id, key)
		);

		CREATE INDEX IF NOT EXISTS idx_metadata_memory_id ON metadata(memory_id);
		CREATE INDEX IF NOT EXISTS idx_metadata_key ON metadata(key);
	`)
	return err
}

// Store stores an embedding for a memory.
func (s *Store) Store(ctx context.Context, memoryID, content string, metadata map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate embedding
	embedding, err := s.provider.GenerateEmbedding(ctx, content)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Serialize vector to blob
	vectorBlob, err := serializeVector(embedding)
	if err != nil {
		return fmt.Errorf("failed to serialize vector: %w", err)
	}

	// Store embedding
	_, err = s.db.Exec(`
		INSERT OR REPLACE INTO embeddings (id, vector)
		VALUES (?, ?)
	`, memoryID, vectorBlob)
	if err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}

	// Store metadata
	for key, value := range metadata {
		valueJSON, _ := json.Marshal(value)
		_, err = s.db.Exec(`
			INSERT OR REPLACE INTO metadata (memory_id, key, value)
			VALUES (?, ?, ?)
		`, memoryID, key, string(valueJSON))
		if err != nil {
			return fmt.Errorf("failed to store metadata: %w", err)
		}
	}

	// Cache the embedding
	s.embeddingCache.Store(memoryID, embedding)

	return nil
}

// Search finds memories similar to the query using vector similarity.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	// Generate query embedding
	queryEmbedding, err := s.provider.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Fetch all embeddings
	rows, err := s.db.Query(`
		SELECT id, vector FROM embeddings
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query embeddings: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var memoryID string
		var vectorBlob []byte

		if err := rows.Scan(&memoryID, &vectorBlob); err != nil {
			continue
		}

		// Deserialize vector
		vector, err := deserializeVector(vectorBlob)
		if err != nil {
			continue
		}

		// Calculate cosine similarity
		similarity := cosineSimilarity(queryEmbedding, vector)

		results = append(results, SearchResult{
			MemoryID:         memoryID,
			VectorSimilarity: float64(similarity),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating embeddings: %w", err)
	}

	// Sort by similarity descending
	slices.SortFunc(results, func(a, b SearchResult) int {
		if b.VectorSimilarity > a.VectorSimilarity {
			return 1
		}
		if b.VectorSimilarity < a.VectorSimilarity {
			return -1
		}
		return 0
	})

	// Apply limit
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	// Fetch metadata for results
	for i := range results {
		metadata, content, err := s.getMemoryMetadata(results[i].MemoryID)
		if err == nil {
			results[i].Metadata = metadata
			results[i].Content = content
		}
		results[i].RelevanceScore = results[i].VectorSimilarity
	}

	return results, nil
}

// getMemoryMetadata retrieves metadata and content for a memory.
func (s *Store) getMemoryMetadata(memoryID string) (map[string]any, string, error) {
	rows, err := s.db.Query(`
		SELECT key, value FROM metadata WHERE memory_id = ?
	`, memoryID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	metadata := make(map[string]any)
	var content string
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		if key == "content" {
			content = value
		} else {
			var v any
			json.Unmarshal([]byte(value), &v)
			metadata[key] = v
		}
	}

	return metadata, content, nil
}

// GetEmbedding retrieves the cached embedding for a memory.
func (s *Store) GetEmbedding(memoryID string) ([]float32, bool) {
	if emb, ok := s.embeddingCache.Load(memoryID); ok {
		return emb.([]float32), true
	}

	// Load from database
	s.mu.RLock()
	defer s.mu.RUnlock()

	var vectorBlob []byte
	err := s.db.QueryRow(`
		SELECT vector FROM embeddings WHERE id = ?
	`, memoryID).Scan(&vectorBlob)
	if err != nil {
		return nil, false
	}

	vector, err := deserializeVector(vectorBlob)
	if err != nil {
		return nil, false
	}

	s.embeddingCache.Store(memoryID, vector)
	return vector, true
}

// Delete removes an embedding from the store.
func (s *Store) Delete(memoryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.embeddingCache.Delete(memoryID)

	_, err := s.db.Exec(`
		DELETE FROM embeddings WHERE id = ?
	`, memoryID)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		DELETE FROM metadata WHERE memory_id = ?
	`, memoryID)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// serializeVector converts a vector to a byte array.
func serializeVector(vector []float32) ([]byte, error) {
	// Each float32 is 4 bytes
	data := make([]byte, len(vector)*4)
	for i, v := range vector {
		// Convert float32 to uint32 bytes
		bits := math.Float32bits(v)
		data[i*4] = byte(bits >> 24)
		data[i*4+1] = byte(bits >> 16)
		data[i*4+2] = byte(bits >> 8)
		data[i*4+3] = byte(bits)
	}
	return data, nil
}

// deserializeVector converts a byte array to a vector.
func deserializeVector(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("invalid vector data length")
	}

	vector := make([]float32, len(data)/4)
	for i := 0; i < len(vector); i++ {
		bits := uint32(data[i*4])<<24 | uint32(data[i*4+1])<<16 | uint32(data[i*4+2])<<8 | uint32(data[i*4+3])
		vector[i] = math.Float32frombits(bits)
	}
	return vector, nil
}

// cosineSimilarity calculates the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct float32
	var normA float32
	var normB float32

	for i := 0; i < len(a); i++ {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
