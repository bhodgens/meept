package llm

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/pkg/sqlite"
)

// L2CacheConfig holds configuration for the L2 SQLite-backed cache.
type L2CacheConfig struct {
	// DBPath is the path to the SQLite database file.
	DBPath string
	// DefaultTTL is the default time-to-live for cache entries.
	DefaultTTL time.Duration
	// CleanupFreq is the frequency of background TTL cleanup.
	CleanupFreq time.Duration
}

// l2SchemaSQL is the DDL for the token_cache table.
const l2SchemaSQL = `
CREATE TABLE IF NOT EXISTS token_cache (
	cache_key        TEXT PRIMARY KEY,
	prompt_hash      TEXT NOT NULL,
	model_id         TEXT NOT NULL,
	file_hashes_json TEXT NOT NULL DEFAULT '{}',
	response_json    TEXT NOT NULL,
	created_at       TEXT NOT NULL,
	expires_at       TEXT NOT NULL,
	hit_count        INTEGER NOT NULL DEFAULT 0,
	last_accessed    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_token_cache_prompt_model ON token_cache(prompt_hash, model_id);
CREATE INDEX IF NOT EXISTS idx_token_cache_expires ON token_cache(expires_at);
`

// L2Cache is an SQLite-backed content-hash cache for LLM responses.
// It provides durable, process-spanning cache storage with file-aware
// invalidation and TTL-based eviction.
type L2Cache struct {
	pool         *sqlite.Pool
	config       L2CacheConfig
	mu           sync.RWMutex
	logger       *slog.Logger
	stopCh       chan struct{}
	metricsStore *metrics.Store
}

// NewL2Cache creates a new L2 SQLite-backed cache.
// The database is opened lazily on the first operation or explicitly via Start.
func NewL2Cache(config L2CacheConfig) (*L2Cache, error) {
	if config.DefaultTTL <= 0 {
		config.DefaultTTL = 30 * time.Minute
	}
	if config.CleanupFreq <= 0 {
		config.CleanupFreq = 2 * time.Minute
	}
	if config.DBPath == "" {
		return nil, errors.New("llm: L2Cache requires DBPath")
	}

	logger := slog.Default().With("component", "token_cache_l2")

	c := &L2Cache{
		config: config,
		logger: logger,
		stopCh: make(chan struct{}),
	}

	if err := c.initDB(context.Background()); err != nil {
		return nil, fmt.Errorf("llm: L2Cache init: %w", err)
	}

	return c, nil
}

// expandPath resolves ~ and env vars in the database path, mirroring the
// pattern from internal/memory/ftstore.go initialization.
func expandPath(path string) string {
	if len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path)
}

// initDB opens the SQLite connection pool and creates the schema.
func (c *L2Cache) initDB(ctx context.Context) error {
	dbPath := expandPath(c.config.DBPath)

	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     dbPath,
		PoolSize: 3,
		WALMode:  true,
		Logger:   c.logger,
	})
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}
	c.pool = pool

	return c.pool.WithConn(ctx, func(db *sql.DB) error {
		if _, err := db.ExecContext(ctx, l2SchemaSQL); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
		return nil
	})
}

// buildL2Key derives a deterministic primary key from a CacheKey.
// The key is the SHA256 of (model_id + prompt_hash + sorted file hashes JSON).
func buildL2Key(key CacheKey) string {
	h := sha256.New()
	h.Write([]byte(key.ModelID))
	h.Write([]byte{0})
	h.Write([]byte(key.PromptHash))
	h.Write([]byte{0})

	if len(key.FileHashes) > 0 {
		sorted := make([]string, 0, len(key.FileHashes))
		for k := range key.FileHashes {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)

		for _, k := range sorted {
			h.Write([]byte(k))
			h.Write([]byte{0})
			h.Write([]byte(key.FileHashes[k]))
			h.Write([]byte{0})
		}
	}

	return hex.EncodeToString(h.Sum(nil))
}

// Get looks up a cache entry by prompt_hash + model_id. It checks file hashes
// for staleness: if the stored file hashes differ from the requested ones the
// entry is considered a miss (and is silently removed).
func (c *L2Cache) Get(ctx context.Context, key CacheKey) (*CacheEntry, bool) {
	cacheKey := buildL2Key(key)

	var (
		fileHashesJSON string
		responseJSON   string
		createdAtStr   string
		expiresAtStr   string
		hitCount       int
	)

	err := c.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, `
			SELECT file_hashes_json, response_json, created_at, expires_at, hit_count
			FROM token_cache
			WHERE cache_key = ?`,
			cacheKey,
		).Scan(&fileHashesJSON, &responseJSON, &createdAtStr, &expiresAtStr, &hitCount)
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			c.logger.Error("L2 get query failed", "error", err)
		}
		return nil, false
	}

	// Parse stored file hashes and check for staleness
	var storedHashes map[string]string
	if err := json.Unmarshal([]byte(fileHashesJSON), &storedHashes); err != nil {
		c.logger.Warn("L2 corrupt file_hashes_json, evicting", "key", cacheKey[:16])
		c.doInvalidate(ctx, cacheKey)
		return nil, false
	}

	if !fileHashesMatch(storedHashes, key.FileHashes) {
		c.logger.Debug("L2 stale file hashes, evicting", "key", cacheKey[:16])
		c.doInvalidate(ctx, cacheKey)
		return nil, false
	}

	// Check TTL
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtStr)
	if err != nil {
		c.logger.Warn("L2 corrupt expires_at, evicting", "key", cacheKey[:16])
		c.doInvalidate(ctx, cacheKey)
		return nil, false
	}
	if time.Now().After(expiresAt) {
		c.doInvalidate(ctx, cacheKey)
		return nil, false
	}

	// Deserialize response
	var response Response
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		c.logger.Warn("L2 corrupt response_json, evicting", "key", cacheKey[:16])
		c.doInvalidate(ctx, cacheKey)
		return nil, false
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)

	// Update hit_count and last_accessed
	now := time.Now().Format(time.RFC3339Nano)
	if _, err := c.pool.Exec(ctx, `
		UPDATE token_cache
		SET hit_count = hit_count + 1, last_accessed = ?
		WHERE cache_key = ?`,
		now, cacheKey,
	); err != nil {
		c.logger.Warn("L2 failed to update hit_count", "error", err)
	}

	c.logger.Debug("L2 cache hit", "key", cacheKey[:16], "hit_count", hitCount+1)

	return &CacheEntry{
		Response:   &response,
		CreatedAt:  createdAt,
		ExpiresAt:  expiresAt,
		HitCount:   hitCount + 1,
		FileHashes: storedHashes,
	}, true
}

// Put inserts or updates a cache entry.
func (c *L2Cache) Put(ctx context.Context, key CacheKey, entry *CacheEntry) {
	cacheKey := buildL2Key(key)

	responseJSON, err := json.Marshal(entry.Response)
	if err != nil {
		c.logger.Error("L2 failed to marshal response", "error", err)
		return
	}

	fileHashesJSON, err := json.Marshal(key.FileHashes)
	if err != nil {
		c.logger.Error("L2 failed to marshal file hashes", "error", err)
		return
	}

	now := time.Now()
	createdAt := now
	if !entry.CreatedAt.IsZero() {
		createdAt = entry.CreatedAt
	}
	expiresAt := now.Add(c.config.DefaultTTL)
	if !entry.ExpiresAt.IsZero() {
		expiresAt = entry.ExpiresAt
	}

	_, err = c.pool.Exec(ctx, `
		INSERT INTO token_cache
			(cache_key, prompt_hash, model_id, file_hashes_json, response_json, created_at, expires_at, hit_count, last_accessed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cache_key) DO UPDATE SET
			file_hashes_json = excluded.file_hashes_json,
			response_json    = excluded.response_json,
			expires_at       = excluded.expires_at,
			hit_count        = excluded.hit_count,
			last_accessed    = excluded.last_accessed`,
		cacheKey,
		key.PromptHash,
		key.ModelID,
		string(fileHashesJSON),
		string(responseJSON),
		createdAt.Format(time.RFC3339Nano),
		expiresAt.Format(time.RFC3339Nano),
		entry.HitCount,
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		c.logger.Error("L2 put failed", "error", err)
		return
	}

	c.logger.Debug("L2 cache stored",
		"key", cacheKey[:16],
		"model", key.ModelID,
		"expires_at", expiresAt,
	)
}

// Invalidate removes a specific cache entry identified by key.
func (c *L2Cache) Invalidate(ctx context.Context, key CacheKey) {
	cacheKey := buildL2Key(key)
	c.doInvalidate(ctx, cacheKey)
}

// doInvalidate removes a cache entry by primary key.
func (c *L2Cache) doInvalidate(ctx context.Context, cacheKey string) {
	if _, err := c.pool.Exec(ctx, `DELETE FROM token_cache WHERE cache_key = ?`, cacheKey); err != nil {
		c.logger.Error("L2 invalidate failed", "key", cacheKey[:16], "error", err)
	}
}

// InvalidateByFile removes all cache entries that reference the given file path
// in their file_hashes_json blob. It uses a LIKE pattern on the JSON to find
// matching entries.
func (c *L2Cache) InvalidateByFile(ctx context.Context, filePath string) {
	// Use a LIKE pattern to match entries that contain the file path as a key
	// in the JSON object. This works because JSON keys are quoted with double
	// quotes, so we search for "filePath" as a key indicator.
	// Escape LIKE metacharacters to prevent incorrect matching on paths with % or _
	escaped := strings.ReplaceAll(filePath, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `%`, `\%`)
	escaped = strings.ReplaceAll(escaped, `_`, `\_`)
	pattern := `%"` + escaped + `"%`

	result, err := c.pool.Exec(ctx, `
		DELETE FROM token_cache WHERE file_hashes_json LIKE ? ESCAPE '\'`,
		pattern,
	)
	if err != nil {
		c.logger.Error("L2 invalidate-by-file failed", "file", filePath, "error", err)
		return
	}

	removed, _ := result.RowsAffected()
	if removed > 0 {
		c.recordEvictionMetric(removed, "file_invalidation")
		c.recordEntryCountMetric()
		c.logger.Debug("L2 invalidated by file", "file", filePath, "removed", removed)
	}
}

// Clear removes all entries from the cache.
func (c *L2Cache) Clear() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := c.pool.Exec(ctx, `DELETE FROM token_cache`); err != nil {
		c.logger.Error("L2 clear failed", "error", err)
		return
	}
	c.logger.Debug("L2 cache cleared")
}

// ClearByModelPrefix removes all cache entries whose model_id starts with the
// given prefix. Returns the number of entries removed.
func (c *L2Cache) ClearByModelPrefix(prefix string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use LIKE with a trailing wildcard to match model_id prefix.
	// S3-4 FIX: escape LIKE metacharacters in the user-provided prefix to
	// prevent wildcard injection (e.g. model IDs containing % or _).
	escaped := strings.ReplaceAll(prefix, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `%`, `\%`)
	escaped = strings.ReplaceAll(escaped, `_`, `\_`)
	result, err := c.pool.Exec(ctx, `DELETE FROM token_cache WHERE model_id LIKE ? ESCAPE '\'`, escaped+"%")
	if err != nil {
		c.logger.Error("L2 clear by model prefix failed", "prefix", prefix, "error", err)
		return 0
	}

	removed, _ := result.RowsAffected()
	if removed > 0 {
		c.recordEvictionMetric(removed, "model_prefix_clear")
		c.recordEntryCountMetric()
		c.logger.Debug("L2 cleared by model prefix", "prefix", prefix, "removed", removed)
	}
	return int(removed)
}

// Count returns the number of entries in the cache.
func (c *L2Cache) Count() int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var count int
	err := c.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM token_cache`).Scan(&count)
	})
	if err != nil {
		c.logger.Error("L2 count failed", "error", err)
		return 0
	}
	return count
}

// L2InspectEntry is a lightweight result for inspection.
type L2InspectEntry struct {
	ModelID    string
	Response   *Response
	CreatedAt  time.Time
	ExpiresAt  time.Time
	HitCount   int
	FileHashes map[string]string
}

// Inspect searches for entries matching the given prompt hash in the L2 cache.
func (c *L2Cache) Inspect(promptHash string) []L2InspectEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := c.pool.Query(ctx, `
		SELECT model_id, response_json, created_at, expires_at, hit_count, file_hashes_json
		FROM token_cache
		WHERE prompt_hash = ?`, promptHash)
	if err != nil {
		c.logger.Error("L2 inspect query failed", "error", err)
		return nil
	}
	defer rows.Close()

	var results []L2InspectEntry
	for rows.Next() {
		var (
			modelID        string
			responseJSON   string
			createdAtStr   string
			expiresAtStr   string
			hitCount       int
			fileHashesJSON string
		)
		if err := rows.Scan(&modelID, &responseJSON, &createdAtStr, &expiresAtStr, &hitCount, &fileHashesJSON); err != nil {
			c.logger.Warn("L2 inspect scan failed", "error", err)
			continue
		}

		var response Response
		if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
			c.logger.Warn("L2 inspect corrupt response_json", "error", err)
			continue
		}

		createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)
		expiresAt, _ := time.Parse(time.RFC3339Nano, expiresAtStr)

		var fileHashes map[string]string
		if fileHashesJSON != "" && fileHashesJSON != "{}" {
			_ = json.Unmarshal([]byte(fileHashesJSON), &fileHashes)
		}

		results = append(results, L2InspectEntry{
			ModelID:    modelID,
			Response:   &response,
			CreatedAt:  createdAt,
			ExpiresAt:  expiresAt,
			HitCount:   hitCount,
			FileHashes: fileHashes,
		})
	}

	return results
}

// Start begins the background TTL cleanup goroutine.
func (c *L2Cache) Start() {
	go func() {
		ticker := time.NewTicker(c.config.CleanupFreq)
		defer ticker.Stop()

		c.logger.Debug("L2 background cleanup started", "frequency", c.config.CleanupFreq)

		for {
			select {
			case <-ticker.C:
				c.cleanupExpired()
			case <-c.stopCh:
				c.logger.Debug("L2 background cleanup stopped")
				return
			}
		}
	}()
}

// SetMetricsStore sets the metrics store for recording eviction metrics.
func (c *L2Cache) SetMetricsStore(store *metrics.Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metricsStore = store
}

// recordEvictionMetric records a cache eviction metric if metrics store is available.
func (c *L2Cache) recordEvictionMetric(count int64, reason string) {
	c.mu.RLock()
	store := c.metricsStore
	c.mu.RUnlock()
	if store == nil {
		return
	}
	store.Record("cache.eviction", float64(count), map[string]string{
		KeyLevel: "l2",
		"reason": reason,
	})
}

// recordEntryCountMetric records the current L2 entry count as a metric.
func (c *L2Cache) recordEntryCountMetric() {
	c.mu.RLock()
	store := c.metricsStore
	c.mu.RUnlock()
	if store == nil {
		return
	}
	store.Record("cache.entry_count", float64(c.Count()), map[string]string{
		KeyLevel: "l2",
	})
}

// Close stops the background cleanup and releases the connection pool.
func (c *L2Cache) Close() error {
	// Signal cleanup goroutine to stop
	select {
	case <-c.stopCh:
		// Already closed
	default:
		close(c.stopCh)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pool != nil {
		return c.pool.Close() //nolint:mutexio // one-time teardown; closing requires exclusive access
	}
	return nil
}

// cleanupExpired removes all entries past their expires_at timestamp.
func (c *L2Cache) cleanupExpired() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now().Format(time.RFC3339Nano)
	result, err := c.pool.Exec(ctx, `DELETE FROM token_cache WHERE expires_at < ?`, now)
	if err != nil {
		c.logger.Error("L2 cleanup failed", "error", err)
		return
	}

	removed, _ := result.RowsAffected()
	if removed > 0 {
		c.recordEvictionMetric(removed, "ttl_expired")
		c.recordEntryCountMetric()
		c.logger.Debug("L2 cleanup removed expired entries", "count", removed)
	}
}

// fileHashesMatch compares two file hash maps for equality.
// Both nil maps and empty maps are treated as equivalent.
func fileHashesMatch(a, b map[string]string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bV, ok := b[k]; !ok || bV != v {
			return false
		}
	}
	return true
}

// Ensure L2Cache implements io.Closer.
var _ io.Closer = (*L2Cache)(nil)
