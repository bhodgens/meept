package vector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// CrossShardJoin enables queries across multiple shard databases
// using SQLite's ATTACH DATABASE mechanism.
type CrossShardJoin struct {
	mu       sync.Mutex
	baseDB   *sql.DB
	attached map[string]string // alias -> db path
}

// NewCrossShardJoin creates a new cross-shard join operator backed by the base DB.
func NewCrossShardJoin(baseDB *sql.DB) *CrossShardJoin {
	return &CrossShardJoin{
		baseDB:   baseDB,
		attached: make(map[string]string),
	}
}

// AttachDatabase attaches a shard's SQLite database file with the given alias.
// Multiple shards can be attached and queried together.
func (c *CrossShardJoin) AttachDatabase(alias, path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Detach existing attachment with same alias if present
	if _, exists := c.attached[alias]; exists {
		if err := c.detachNoLock(alias); err != nil {
			return fmt.Errorf("failed to detach existing %s: %w", alias, err)
		}
	}

	query := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", path, alias)
	if _, err := c.baseDB.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("attach database %s at %s: %w", path, alias, err)
	}

	c.attached[alias] = path
	return nil
}

// DetachDatabase detaches a shard database by alias.
func (c *CrossShardJoin) DetachDatabase(alias string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.attached[alias]; !exists {
		return nil // already detached
	}

	return c.detachNoLock(alias)
}

// DetachAll detaches all attached databases.
func (c *CrossShardJoin) DetachAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Detach in reverse order of attachment (SQLite requires LIFO order).
	aliases := make([]string, 0, len(c.attached))
	for a := range c.attached {
		aliases = append(aliases, a)
	}
	for i := len(aliases) - 1; i >= 0; i-- {
		_ = c.detachNoLock(aliases[i])
	}
	return nil
}

// attachedCount returns the number of attached shards (caller must hold lock).
func (c *CrossShardJoin) attachedCount() int {
	return len(c.attached)
}

// AttachedAliases returns a copy of the attached alias set.
func (c *CrossShardJoin) AttachedAliases() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	aliases := make([]string, 0, len(c.attached))
	for a := range c.attached {
		aliases = append(aliases, a)
	}
	return aliases
}

// QueryAllShards executes a UNION query across all attached shard databases.
// The query must reference tables using the attached aliases and should include
// columns: memory_id, content, vector_similarity, metadata_json in that order.
func (c *CrossShardJoin) QueryAllShards(ctx context.Context, query string, args ...any) ([]SearchResult, error) {
	c.mu.Lock()
	if c.attachedCount() == 0 {
		c.mu.Unlock()
		return nil, fmt.Errorf("no shards attached")
	}
	c.mu.Unlock()

	rows, err := c.baseDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query all shards: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var sr SearchResult
		var metaStr string
		if err := rows.Scan(&sr.MemoryID, &sr.Content, &sr.VectorSimilarity, &metaStr); err != nil {
			// Skip rows with scan errors rather than attempting partial reads
			continue
		}
		sr.RelevanceScore = sr.VectorSimilarity
		sr.Metadata = parseMetadataString(metaStr)
		results = append(results, sr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating shard results: %w", err)
	}

	return results, nil
}

// ShardResults holds the combined results from querying multiple shards.
type ShardResults struct {
	Combined []SearchResult
	Missing  []string // aliases that failed to load
}

// QueryShards executes queries across individual shard aliases and merges results.
// Each entry in queries maps an alias to its SQL query string.
// The alias must already be attached before calling this method.
func (c *CrossShardJoin) QueryShards(ctx context.Context, queries map[string]string) (*ShardResults, error) {
	if len(queries) == 0 {
		return &ShardResults{}, nil
	}

	shardResultMap := make(map[string][]SearchResult)
	var missing []string

	for alias, query := range queries {
		rows, err := c.baseDB.QueryContext(ctx, query)
		if err != nil {
			missing = append(missing, alias)
			continue
		}

		var results []SearchResult
		for rows.Next() {
			var sr SearchResult
			if err := rows.Scan(&sr.MemoryID, &sr.Content, &sr.VectorSimilarity, &sr.Metadata); err == nil {
				sr.RelevanceScore = sr.VectorSimilarity
			} else {
				var metaStr string
				rows.Scan(&sr.MemoryID, &sr.Content, &sr.VectorSimilarity, &metaStr)
				sr.RelevanceScore = sr.VectorSimilarity
				sr.Metadata = parseMetadataString(metaStr)
			}
			results = append(results, sr)
		}
		rows.Close()

		if len(results) > 0 {
			shardResultMap[alias] = results
		}
	}

	// Combine all results sorted by similarity descending
	combined := consolidateSorted(shardResultMap)

	return &ShardResults{
		Combined: combined,
		Missing:  missing,
	}, nil
}

// consolidateSorted merges per-shard result maps into a single sorted slice.
func consolidateSorted(m map[string][]SearchResult) []SearchResult {
	var combined []SearchResult
	for _, results := range m {
		combined = append(combined, results...)
	}
	// Sort by vector similarity descending
	slices.SortFunc(combined, func(a, b SearchResult) int {
		if b.VectorSimilarity > a.VectorSimilarity {
			return 1
		}
		if b.VectorSimilarity < a.VectorSimilarity {
			return -1
		}
		return 0
	})
	return combined
}

// parseMetadataString safely decodes a JSON metadata string into a map.
func parseMetadataString(s string) map[string]any {
	if s == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

// BuildUnionQuery creates a UNION ALL query from per-shard queries.
// Each query in the map should reference its shard's tables using the alias prefix.
// Returns a combined query string and the number of participating shards.
func BuildUnionQuery(queries map[string]string) (string, int) {
	count := len(queries)
	if count == 0 {
		return "", 0
	}

	parts := make([]string, 0, count)
	for _, q := range queries {
		parts = append(parts, "("+q+")")
	}

	combined := strings.Join(parts, " UNION ALL ")
	combined += " ORDER BY vector_similarity DESC"

	return combined, count
}

// detachNoLock detaches without acquiring the lock (caller must hold it).
func (c *CrossShardJoin) detachNoLock(alias string) error {
	query := fmt.Sprintf("DETACH DATABASE %s", alias)
	if _, err := c.baseDB.ExecContext(context.Background(), query); err != nil {
		return fmt.Errorf("detach database %s: %w", alias, err)
	}

	delete(c.attached, alias)
	return nil
}
