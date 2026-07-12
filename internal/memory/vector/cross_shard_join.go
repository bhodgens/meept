package vector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

// CrossShardJoin enables queries across multiple shard databases
// using SQLite's ATTACH DATABASE mechanism.
//
// Because SQLite ATTACH is connection-scoped, CrossShardJoin does not ATTACH
// at registration time. Instead, AttachDatabase registers alias→path mappings.
// Query methods pin a single sql.Conn from the pool, ATTACH all registered
// shards on that connection, run the query, DETACH, and return the connection
// — all under the mutex so no other goroutine can mutate the attach scope
// mid-query.
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

// AttachDatabase registers a shard's SQLite database file with the given alias.
// The actual ATTACH happens on a pinned connection during query execution.
func (c *CrossShardJoin) AttachDatabase(alias, path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate alias to prevent SQL injection — only allow alphanumeric + underscore.
	if !isSafeIdentifier(alias) {
		return fmt.Errorf("attach database: invalid alias %q (must be alphanumeric/underscore)", alias)
	}

	c.attached[alias] = path
	return nil
}

// isSafeIdentifier returns true if s contains only [A-Za-z0-9_].
func isSafeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

// DetachDatabase removes a shard database registration by alias.
func (c *CrossShardJoin) DetachDatabase(alias string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.attached, alias)
	return nil
}

// DetachAll removes all shard database registrations.
func (c *CrossShardJoin) DetachAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.attached = make(map[string]string)
	return nil
}

// attachedCount returns the number of registered shards (caller must hold lock).
func (c *CrossShardJoin) attachedCount() int {
	return len(c.attached)
}

// AttachedAliases returns a copy of the registered alias set.
func (c *CrossShardJoin) AttachedAliases() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	aliases := make([]string, 0, len(c.attached))
	for a := range c.attached {
		aliases = append(aliases, a)
	}
	return aliases
}

// sortedAliases returns registered aliases sorted alphabetically (caller must hold lock).
func (c *CrossShardJoin) sortedAliases() []string {
	aliases := make([]string, 0, len(c.attached))
	for a := range c.attached {
		aliases = append(aliases, a)
	}
	slices.Sort(aliases)
	return aliases
}

// attachAllLocked ATTACHes all registered shards on the given pinned connection.
// Returns the list of aliases in attach order for later DETACH.
// Caller MUST hold c.mu.
func (c *CrossShardJoin) attachAllLocked(ctx context.Context, conn *sql.Conn) ([]string, error) {
	aliases := c.sortedAliases()
	attached := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		path := c.attached[alias]
		escapedPath := strings.ReplaceAll(path, "'", "''")
		attachSQL := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", escapedPath, alias)
		if _, err := conn.ExecContext(ctx, attachSQL); err != nil { //nolint:mutexio // mutex serializes cross-shard ATTACH/DETACH/query; conn is pinned to single connection
			return attached, fmt.Errorf("attach database %s at %s: %w", path, alias, err)
		}
		attached = append(attached, alias)
	}
	return attached, nil
}

// detachAllLocked DETACHes the given aliases in reverse order (LIFO).
// Caller MUST hold c.mu. Best-effort: errors are ignored.
func (c *CrossShardJoin) detachAllLocked(ctx context.Context, conn *sql.Conn, aliases []string) {
	for i := len(aliases) - 1; i >= 0; i-- {
		alias := aliases[i]
		if !isSafeIdentifier(alias) {
			continue
		}
		_, _ = conn.ExecContext(ctx, fmt.Sprintf("DETACH DATABASE %s", alias)) //nolint:mutexio // mutex serializes cross-shard ATTACH/DETACH/query; conn is pinned to single connection
	}
}

// QueryAllShards executes a UNION query across all attached shard databases.
// The query must reference tables using the attached aliases and should include
// columns: memory_id, content, vector_similarity, metadata_json in that order.
func (c *CrossShardJoin) QueryAllShards(ctx context.Context, query string, args ...any) ([]SearchResult, error) {
	c.mu.Lock() //nolint:mutexio // mutex held across ATTACH+query+DETACH to ensure connection-stable attach scope
	defer c.mu.Unlock()

	if c.attachedCount() == 0 {
		return nil, fmt.Errorf("no shards attached")
	}

	// Pin a single connection so ATTACH and query land on the same connection.
	conn, err := c.baseDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection for cross-shard query: %w", err)
	}
	defer conn.Close()

	// ATTACH all registered shards on this pinned connection.
	attachedAliases, err := c.attachAllLocked(ctx, conn)
	// Always DETACH what was successfully attached, even on error.
	defer func() {
		c.detachAllLocked(ctx, conn, attachedAliases)
	}()
	if err != nil {
		return nil, err
	}

	rows, err := conn.QueryContext(ctx, query, args...) //nolint:mutexio // mutex serializes cross-shard query; conn is pinned to single connection
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
// The alias must already be registered before calling this method.
func (c *CrossShardJoin) QueryShards(ctx context.Context, queries map[string]string) (*ShardResults, error) {
	if len(queries) == 0 {
		return &ShardResults{}, nil
	}

	c.mu.Lock() //nolint:mutexio // mutex held across ATTACH+query+DETACH to ensure connection-stable attach scope
	defer c.mu.Unlock()

	// Pin a single connection so ATTACH and query land on the same connection.
	conn, err := c.baseDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection for cross-shard query: %w", err)
	}
	defer conn.Close()

	// ATTACH all registered shards on this pinned connection.
	attachedAliases, err := c.attachAllLocked(ctx, conn)
	// Always DETACH what was successfully attached, even on error.
	defer func() {
		c.detachAllLocked(ctx, conn, attachedAliases)
	}()
	if err != nil {
		return nil, err
	}

	shardResultMap := make(map[string][]SearchResult)
	var missing []string

	for alias, q := range queries {
		rows, err := conn.QueryContext(ctx, q) //nolint:mutexio // mutex serializes cross-shard query; conn is pinned to single connection
		if err != nil {
			missing = append(missing, alias)
			continue
		}

		var results []SearchResult
		// D-09 FIX: track scan errors so we can abort this shard on fatal
		// errors (e.g., column count mismatch) rather than silently skipping.
		var scanErr error
		for rows.Next() {
			var sr SearchResult
			var metaStr string
			if err := rows.Scan(&sr.MemoryID, &sr.Content, &sr.VectorSimilarity, &metaStr); err != nil {
				// D-09 FIX: log the scan error with alias context instead of
				// silently continuing. A scan error usually indicates a schema
				// mismatch or corrupt row — abort this shard's query.
				slog.Warn("cross_shard_join: scan error, aborting shard query",
					"shard_alias", alias, "error", err)
				scanErr = err
				break
			}
			sr.RelevanceScore = sr.VectorSimilarity
			sr.Metadata = parseMetadataString(metaStr)
			results = append(results, sr)
		}
		// D-09 FIX: check rows.Err() after the loop to detect fatal cursor
		// errors mid-iteration (e.g., context cancellation, I/O failure).
		// Previously this was never checked, silently dropping fatal errors
		// and returning partial/garbage results.
		if cursorErr := rows.Err(); cursorErr != nil {
			slog.Warn("cross_shard_join: cursor error after row iteration",
				"shard_alias", alias, "error", cursorErr)
			scanErr = cursorErr
		}
		rows.Close() //nolint:mutexio // same locked-query scope as QueryContext above

		if scanErr != nil {
			// D-09 FIX: abort this shard — don't return partial results from
			// a cursor that errored mid-iteration.
			missing = append(missing, alias)
			continue
		}

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
