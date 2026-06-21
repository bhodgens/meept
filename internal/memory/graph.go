// Package memory provides memory storage and retrieval for meept.
package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/sqlite"
)

// EdgeType represents the type of relationship between memories.
type EdgeType string

const (
	// EdgeTypeReference indicates one memory references another.
	EdgeTypeReference EdgeType = "reference"
	// EdgeTypeSimilar indicates semantic similarity.
	EdgeTypeSimilar EdgeType = "similar"
	// EdgeTypeTemporal indicates temporal proximity (same session/time).
	EdgeTypeTemporal EdgeType = "temporal"
	// EdgeTypeCoAccessed indicates memories were accessed together.
	EdgeTypeCoAccessed EdgeType = "co_accessed"
	// EdgeTypeCausal indicates causal relationship (one led to another).
	EdgeTypeCausal EdgeType = "causal"
	// Epistemic edges (Plan 1: epistemic memory platform)
	EdgeTypeContradicts     EdgeType = "contradicts"
	EdgeTypeSuperseded      EdgeType = "superseded"
	EdgeTypeEvidenceFor     EdgeType = "evidence_for"
	EdgeTypeEvidenceAgainst EdgeType = "evidence_against"
	EdgeTypeDerivesFrom     EdgeType = "derives_from"
	EdgeTypeSupports        EdgeType = "supports"
	// EdgeTypePotentialContradicts is a low-confidence contradiction candidate
	// surfaced for review. Does not propagate to search ranking or destructive
	// actions.
	EdgeTypePotentialContradicts EdgeType = "potential_contradicts"
)

// MemoryEdge represents a directed edge between two memories.
//
//nolint:revive // stutter with package name is intentional for API clarity
type MemoryEdge struct {
	ID         string         `json:"id"`
	SourceID   string         `json:"source_id"`
	TargetID   string         `json:"target_id"`
	EdgeType   EdgeType       `json:"edge_type"`
	Weight     float64        `json:"weight"`     // 0.0-1.0, higher = stronger relationship
	Confidence float64        `json:"confidence"` // 0.0-1.0, how confident we are in this edge
	CreatedAt  time.Time      `json:"created_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// MemoryNode represents a memory with its graph properties.
//
//nolint:revive // stutter with package name is intentional for API clarity
type MemoryNode struct {
	Memory      Memory  `json:"memory"`
	PageRank    float64 `json:"page_rank"`    // Importance score [0, 1]
	InDegree    int     `json:"in_degree"`    // Number of incoming edges
	OutDegree   int     `json:"out_degree"`   // Number of outgoing edges
	CommunityID string  `json:"community_id"` // Cluster/community membership
}

// GraphStats holds statistics about the knowledge graph.
type GraphStats struct {
	NodeCount      int       `json:"node_count"`
	EdgeCount      int       `json:"edge_count"`
	AvgDegree      float64   `json:"avg_degree"`
	CommunityCount int       `json:"community_count"`
	LastUpdated    time.Time `json:"last_updated"`
}

// KnowledgeGraph manages relationships between memories.
// It provides PageRank scoring, community detection, and graph-aware search.
type KnowledgeGraph struct {
	pool        *sqlite.Pool
	dataDir     string
	initialized bool
	mu          sync.RWMutex
	logger      *slog.Logger

	// PageRank parameters
	dampingFactor float64
	maxIterations int
	tolerance     float64

	// Caches
	pageRankCache    map[string]float64
	communityCache   map[string]string
	cacheLastUpdated time.Time
	cacheTTL         time.Duration
}

// KnowledgeGraphConfig holds configuration for the knowledge graph.
type KnowledgeGraphConfig struct {
	DataDir       string
	Logger        *slog.Logger
	DampingFactor float64       // PageRank damping (default: 0.85)
	MaxIterations int           // Max PageRank iterations (default: 100)
	Tolerance     float64       // PageRank convergence (default: 1e-6)
	CacheTTL      time.Duration // Cache validity (default: 5m)
}

const (
	createEdgesTableSQL = `
CREATE TABLE IF NOT EXISTS memory_edges (
    id          TEXT PRIMARY KEY,
    source_id   TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    edge_type   TEXT NOT NULL,
    weight      REAL NOT NULL DEFAULT 0.5,
    confidence  REAL NOT NULL DEFAULT 1.0,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL,
    UNIQUE(source_id, target_id, edge_type)
)`

	createEdgesIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_edges_source ON memory_edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON memory_edges(target_id);
CREATE INDEX IF NOT EXISTS idx_edges_type ON memory_edges(edge_type)`

	createPageRankTableSQL = `
CREATE TABLE IF NOT EXISTS memory_pagerank (
    memory_id   TEXT PRIMARY KEY,
    score       REAL NOT NULL DEFAULT 0.0,
    in_degree   INTEGER NOT NULL DEFAULT 0,
    out_degree  INTEGER NOT NULL DEFAULT 0,
    community_id TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL
)`

	createStatsTableSQL = `
CREATE TABLE IF NOT EXISTS graph_stats (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    node_count  INTEGER NOT NULL DEFAULT 0,
    edge_count  INTEGER NOT NULL DEFAULT 0,
    updated_at  TEXT NOT NULL
)`
)

// generateEdgeID creates a unique edge ID using full source/target IDs plus a random
// suffix, avoiding collisions from truncated UUID prefixes.
func generateEdgeID(sourceID, targetID string, edgeType EdgeType) string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%s-%s-%s-%x", sourceID, targetID, edgeType, buf)
}

// NewKnowledgeGraph creates a new knowledge graph instance.
func NewKnowledgeGraph(cfg KnowledgeGraphConfig) *KnowledgeGraph {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.DampingFactor == 0 {
		cfg.DampingFactor = 0.85
	}
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 100
	}
	if cfg.Tolerance == 0 {
		cfg.Tolerance = 1e-6
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}

	return &KnowledgeGraph{
		dataDir:        cfg.DataDir,
		logger:         cfg.Logger,
		dampingFactor:  cfg.DampingFactor,
		maxIterations:  cfg.MaxIterations,
		tolerance:      cfg.Tolerance,
		cacheTTL:       cfg.CacheTTL,
		pageRankCache:  make(map[string]float64),
		communityCache: make(map[string]string),
	}
}

// Initialize sets up the database schema.
func (g *KnowledgeGraph) Initialize(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.initialized {
		return nil
	}

	dbPath := filepath.Join(g.dataDir, "graph.db")

	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     dbPath,
		PoolSize: 3,
		WALMode:  true,
		Logger:   g.logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}
	g.pool = pool

	// Initialize schema
	if err := g.initSchema(ctx); err != nil {
		pool.Close() //nolint:mutexio // one-time init cleanup path
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	g.initialized = true
	g.logger.Info("Knowledge graph initialized", "path", dbPath)
	return nil
}

func (g *KnowledgeGraph) initSchema(ctx context.Context) error {
	return g.pool.WithConn(ctx, func(db *sql.DB) error {
		statements := []string{
			createEdgesTableSQL,
			createEdgesIndexSQL,
			createPageRankTableSQL,
			createStatsTableSQL,
		}

		for _, stmt := range statements {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("failed to execute schema statement: %w", err)
			}
		}
		return nil
	})
}

// AddEdge creates a relationship between two memories.
func (g *KnowledgeGraph) AddEdge(ctx context.Context, edge MemoryEdge) error {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	if edge.ID == "" {
		edge.ID = generateEdgeID(edge.SourceID, edge.TargetID, edge.EdgeType)
	}
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = time.Now()
	}
	if edge.Weight == 0 {
		edge.Weight = 0.5
	}
	if edge.Confidence == 0 {
		edge.Confidence = 1.0
	}

	metaJSON := (&Memory{Metadata: edge.Metadata}).MetadataJSON()

	_, err := g.pool.Exec(ctx,
		`INSERT OR REPLACE INTO memory_edges
		(id, source_id, target_id, edge_type, weight, confidence, metadata_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		edge.ID, edge.SourceID, edge.TargetID, string(edge.EdgeType),
		edge.Weight, edge.Confidence, metaJSON, edge.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to add edge: %w", err)
	}

	// Invalidate cache
	g.invalidateCache()

	return nil
}

// AddEdges adds multiple edges efficiently.
func (g *KnowledgeGraph) AddEdges(ctx context.Context, edges []MemoryEdge) error {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	return g.pool.WithConn(ctx, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		stmt, err := tx.PrepareContext(ctx,
			`INSERT OR REPLACE INTO memory_edges
			(id, source_id, target_id, edge_type, weight, confidence, metadata_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, edge := range edges {
			if edge.ID == "" {
				edge.ID = generateEdgeID(edge.SourceID, edge.TargetID, edge.EdgeType)
			}
			if edge.CreatedAt.IsZero() {
				edge.CreatedAt = time.Now()
			}
			if edge.Weight == 0 {
				edge.Weight = 0.5
			}
			if edge.Confidence == 0 {
				edge.Confidence = 1.0
			}

			metaJSON := (&Memory{Metadata: edge.Metadata}).MetadataJSON()

			_, err := stmt.ExecContext(ctx,
				edge.ID, edge.SourceID, edge.TargetID, string(edge.EdgeType),
				edge.Weight, edge.Confidence, metaJSON, edge.CreatedAt.Format(time.RFC3339),
			)
			if err != nil {
				return err
			}
		}

		g.invalidateCache()
		return tx.Commit()
	})
}

// GetEdges retrieves all edges for a memory.
func (g *KnowledgeGraph) GetEdges(ctx context.Context, memoryID string) ([]MemoryEdge, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	db, err := g.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer g.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, source_id, target_id, edge_type, weight, confidence, metadata_json, created_at
		FROM memory_edges
		WHERE source_id = ? OR target_id = ?
		ORDER BY weight DESC
	`, memoryID, memoryID)
	if err != nil {
		return nil, fmt.Errorf("failed to get edges: %w", err)
	}
	defer rows.Close()

	var edges []MemoryEdge
	for rows.Next() {
		var edge MemoryEdge
		var edgeType, metaJSON, createdAtStr string

		if err := rows.Scan(&edge.ID, &edge.SourceID, &edge.TargetID,
			&edgeType, &edge.Weight, &edge.Confidence, &metaJSON, &createdAtStr); err != nil {
			return nil, err
		}

		edge.EdgeType = EdgeType(edgeType)
		edge.Metadata = ParseMetadata(metaJSON)
		edge.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		edges = append(edges, edge)
	}

	return edges, rows.Err()
}

// EdgeCountForMemory returns the number of edges that reference the given
// memory as either source or target. Used by the mark_superseded preview to
// report how many edges will be redirected.
func (g *KnowledgeGraph) EdgeCountForMemory(ctx context.Context, memoryID string) (int, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return 0, errors.New("knowledge graph not initialized")
	}
	pool := g.pool
	g.mu.RUnlock()

	if pool == nil {
		return 0, errors.New("database pool unavailable")
	}

	db, err := pool.Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire connection: %w", err)
	}
	defer pool.Put(db)

	var count int
	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_edges WHERE source_id = ? OR target_id = ?`,
		memoryID, memoryID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count edges: %w", err)
	}
	return count, nil
}

// GetRelatedMemoryIDs returns IDs of memories related to the given memory.
func (g *KnowledgeGraph) GetRelatedMemoryIDs(ctx context.Context, memoryID string, limit int) ([]string, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	db, err := g.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer g.pool.Put(db)

	// Get related IDs weighted by edge weight and confidence
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT
			CASE WHEN source_id = ? THEN target_id ELSE source_id END as related_id,
			weight * confidence as score
		FROM memory_edges
		WHERE source_id = ? OR target_id = ?
		ORDER BY score DESC
		LIMIT ?
	`, memoryID, memoryID, memoryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		var score float64
		if err := rows.Scan(&id, &score); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// ComputePageRank runs the PageRank algorithm on all memories.
func (g *KnowledgeGraph) ComputePageRank(ctx context.Context) error {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	db, err := g.pool.Get(ctx)
	if err != nil {
		return err
	}
	defer g.pool.Put(db)

	// Get all unique node IDs
	nodeRows, err := db.QueryContext(ctx, `
		SELECT DISTINCT id FROM (
			SELECT source_id as id FROM memory_edges
			UNION
			SELECT target_id as id FROM memory_edges
		)
	`)
	if err != nil {
		return err
	}
	defer nodeRows.Close()

	var nodes []string
	nodeIndex := make(map[string]int)
	for nodeRows.Next() {
		var id string
		if err := nodeRows.Scan(&id); err != nil {
			return err
		}
		nodeIndex[id] = len(nodes)
		nodes = append(nodes, id)
	}
	if err := nodeRows.Err(); err != nil {
		return err
	}

	n := len(nodes)
	if n == 0 {
		return nil
	}

	// Build adjacency lists
	outLinks := make(map[int][]int)
	inLinks := make(map[int][]int)

	edgeRows, err := db.QueryContext(ctx, `
		SELECT source_id, target_id, weight FROM memory_edges
	`)
	if err != nil {
		return err
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var srcID, tgtID string
		var weight float64
		if err := edgeRows.Scan(&srcID, &tgtID, &weight); err != nil {
			return err
		}

		srcIdx, tgtIdx := nodeIndex[srcID], nodeIndex[tgtID]
		outLinks[srcIdx] = append(outLinks[srcIdx], tgtIdx)
		inLinks[tgtIdx] = append(inLinks[tgtIdx], srcIdx)
	}
	if err := edgeRows.Err(); err != nil {
		return err
	}

	// Initialize PageRank scores
	scores := make([]float64, n)
	for i := range scores {
		scores[i] = 1.0 / float64(n)
	}

	// Iterate PageRank
	d := g.dampingFactor
	for iter := range g.maxIterations {
		_ = iter
		newScores := make([]float64, n)

		// Base score for all nodes
		for i := range newScores {
			newScores[i] = (1 - d) / float64(n)
		}

		// Distribute scores through links
		for i := range n {
			if outDegree := len(outLinks[i]); outDegree > 0 {
				contribution := d * scores[i] / float64(outDegree)
				for _, j := range outLinks[i] {
					newScores[j] += contribution
				}
			} else {
				// Dangling node: distribute to all
				contribution := d * scores[i] / float64(n)
				for j := range n {
					newScores[j] += contribution
				}
			}
		}

		// Check convergence
		diff := 0.0
		for i := range n {
			diff += math.Abs(newScores[i] - scores[i])
		}
		scores = newScores

		if diff < g.tolerance {
			g.logger.Debug("PageRank converged", "iterations", iter+1, "diff", diff)
			break
		}
	}

	// Store results
	g.mu.Lock()
	g.pageRankCache = make(map[string]float64)
	for i, id := range nodes {
		g.pageRankCache[id] = scores[i]
	}
	g.cacheLastUpdated = time.Now()
	g.mu.Unlock()

	// Persist to database
	return g.persistPageRank(ctx, nodes, scores, outLinks, inLinks)
}

func (g *KnowledgeGraph) persistPageRank(ctx context.Context, nodes []string, scores []float64,
	outLinks, inLinks map[int][]int) error {
	return g.pool.WithConn(ctx, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		stmt, err := tx.PrepareContext(ctx,
			`INSERT OR REPLACE INTO memory_pagerank
			(memory_id, score, in_degree, out_degree, community_id, updated_at)
			VALUES (?, ?, ?, ?, '', ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		now := time.Now().Format(time.RFC3339)
		for i, id := range nodes {
			inDegree := len(inLinks[i])
			outDegree := len(outLinks[i])

			_, err := stmt.ExecContext(ctx, id, scores[i], inDegree, outDegree, now)
			if err != nil {
				return err
			}
		}

		return tx.Commit()
	})
}

// GetPageRank returns the PageRank score for a memory.
func (g *KnowledgeGraph) GetPageRank(ctx context.Context, memoryID string) (float64, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return 0, errors.New("knowledge graph not initialized")
	}

	// Check cache first
	if time.Since(g.cacheLastUpdated) < g.cacheTTL {
		if score, ok := g.pageRankCache[memoryID]; ok {
			g.mu.RUnlock()
			return score, nil
		}
	}
	g.mu.RUnlock()

	// Fetch from database (outside lock to avoid I/O under mutex)
	var score float64
	err := g.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx,
			`SELECT score FROM memory_pagerank WHERE memory_id = ?`,
			memoryID).Scan(&score)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err == nil {
		// Update cache under lock
		g.mu.Lock()
		g.pageRankCache[memoryID] = score
		g.mu.Unlock()
	}
	return score, err
}

// DetectCommunities performs community detection using label propagation.
// Returns a map of memory_id -> community_id.
func (g *KnowledgeGraph) DetectCommunities(ctx context.Context) (map[string]string, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	db, err := g.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer g.pool.Put(db)

	// Get all nodes
	nodeRows, err := db.QueryContext(ctx, `
		SELECT DISTINCT id FROM (
			SELECT source_id as id FROM memory_edges
			UNION
			SELECT target_id as id FROM memory_edges
		)
	`)
	if err != nil {
		return nil, err
	}
	defer nodeRows.Close()

	var nodes []string
	nodeIndex := make(map[string]int)
	for nodeRows.Next() {
		var id string
		if err := nodeRows.Scan(&id); err != nil {
			return nil, err
		}
		nodeIndex[id] = len(nodes)
		nodes = append(nodes, id)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, err
	}

	if len(nodes) == 0 {
		return make(map[string]string), nil
	}

	// Build neighbor lists with weights
	neighbors := make(map[int]map[int]float64)
	edgeRows, err := db.QueryContext(ctx, `
		SELECT source_id, target_id, weight FROM memory_edges
	`)
	if err != nil {
		return nil, err
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var srcID, tgtID string
		var weight float64
		if err := edgeRows.Scan(&srcID, &tgtID, &weight); err != nil {
			return nil, err
		}

		srcIdx, tgtIdx := nodeIndex[srcID], nodeIndex[tgtID]

		if neighbors[srcIdx] == nil {
			neighbors[srcIdx] = make(map[int]float64)
		}
		if neighbors[tgtIdx] == nil {
			neighbors[tgtIdx] = make(map[int]float64)
		}
		neighbors[srcIdx][tgtIdx] = weight
		neighbors[tgtIdx][srcIdx] = weight
	}
	if err := edgeRows.Err(); err != nil {
		return nil, err
	}

	// Initialize: each node is its own community
	labels := make([]int, len(nodes))
	for i := range labels {
		labels[i] = i
	}

	// Label propagation iterations
	maxIter := 10
	for iter := range maxIter {
		changed := false

		for i := range nodes {
			if len(neighbors[i]) == 0 {
				continue
			}

			// Count weighted votes for each label
			votes := make(map[int]float64)
			for j, weight := range neighbors[i] {
				votes[labels[j]] += weight
			}

			// Find label with max votes
			maxVote := 0.0
			maxLabel := labels[i]
			for label, vote := range votes {
				if vote > maxVote {
					maxVote = vote
					maxLabel = label
				}
			}

			if maxLabel != labels[i] {
				labels[i] = maxLabel
				changed = true
			}
		}

		if !changed {
			g.logger.Debug("Community detection converged", "iterations", iter+1)
			break
		}
	}

	// Convert to result map
	result := make(map[string]string)
	for i, id := range nodes {
		result[id] = fmt.Sprintf("c%d", labels[i])
	}

	// Update cache
	g.mu.Lock()
	g.communityCache = result
	g.cacheLastUpdated = time.Now()
	g.mu.Unlock()

	// Persist
	return result, g.persistCommunities(ctx, result)
}

func (g *KnowledgeGraph) persistCommunities(ctx context.Context, communities map[string]string) error {
	return g.pool.WithConn(ctx, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		stmt, err := tx.PrepareContext(ctx,
			`UPDATE memory_pagerank SET community_id = ? WHERE memory_id = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for id, communityID := range communities {
			result, err := stmt.ExecContext(ctx, communityID, id)
			if err != nil {
				return fmt.Errorf("failed to update community for memory %s: %w", id, err)
			}
			// Check that at least one row was affected (memory exists in pagerank table)
			if affected, _ := result.RowsAffected(); affected == 0 {
				// Node might not exist in pagerank table yet - log but continue
				g.logger.Debug("Community update affected 0 rows", "memory_id", id)
			}
		}

		return tx.Commit()
	})
}

// GetCommunity returns the community ID for a memory.
func (g *KnowledgeGraph) GetCommunity(ctx context.Context, memoryID string) (string, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return "", errors.New("knowledge graph not initialized")
	}

	// Check cache first
	if time.Since(g.cacheLastUpdated) < g.cacheTTL {
		if community, ok := g.communityCache[memoryID]; ok {
			g.mu.RUnlock()
			return community, nil
		}
	}
	g.mu.RUnlock()

	// Fetch from database (outside lock to avoid I/O under mutex)
	var community string
	err := g.pool.WithConn(ctx, func(db *sql.DB) error {
		return db.QueryRowContext(ctx,
			`SELECT community_id FROM memory_pagerank WHERE memory_id = ?`,
			memoryID).Scan(&community)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err == nil {
		// Update cache under lock
		g.mu.Lock()
		g.communityCache[memoryID] = community
		g.mu.Unlock()
	}
	return community, err
}

// GetCommunitySiblings returns other memories in the same community.
func (g *KnowledgeGraph) GetCommunitySiblings(ctx context.Context, memoryID string, limit int) ([]string, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	community, err := g.GetCommunity(ctx, memoryID)
	if err != nil || community == "" {
		return nil, err
	}

	db, err := g.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer g.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT memory_id FROM memory_pagerank
		WHERE community_id = ? AND memory_id != ?
		ORDER BY score DESC
		LIMIT ?
	`, community, memoryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// RankResults re-ranks search results using PageRank scores.
func (g *KnowledgeGraph) RankResults(ctx context.Context, results []MemoryResult, alpha float64) ([]MemoryResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	// alpha controls the blend: 0 = pure relevance, 1 = pure PageRank
	if alpha < 0 || alpha > 1 {
		alpha = 0.3 // Default: 30% PageRank influence
	}

	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return results, nil // Return unranked if graph not available
	}
	g.mu.RUnlock()

	// Get PageRank scores
	for i := range results {
		pr, err := g.GetPageRank(ctx, results[i].Memory.ID)
		if err != nil {
			continue
		}

		// Blend relevance and PageRank
		relevance := results[i].RelevanceScore
		results[i].RelevanceScore = (1-alpha)*relevance + alpha*pr
	}

	// Re-sort by combined score
	sort.Slice(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	return results, nil
}

// ExpandResults adds related memories from the graph.
func (g *KnowledgeGraph) ExpandResults(ctx context.Context, results []MemoryResult, expansionLimit int) ([]string, error) {
	if len(results) == 0 {
		return nil, nil
	}

	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, nil
	}
	g.mu.RUnlock()

	// Collect IDs of top results
	seen := make(map[string]bool)
	for _, r := range results {
		seen[r.Memory.ID] = true
	}

	// Get related memories for top results
	var relatedIDs []string
	for _, r := range results[:min(len(results), 5)] {
		related, err := g.GetRelatedMemoryIDs(ctx, r.Memory.ID, expansionLimit)
		if err != nil {
			continue
		}

		for _, id := range related {
			if !seen[id] {
				relatedIDs = append(relatedIDs, id)
				seen[id] = true
			}
		}
	}

	return relatedIDs, nil
}

// CreateTemporalEdges creates temporal edges between memories in the same session.
func (g *KnowledgeGraph) CreateTemporalEdges(ctx context.Context, sessionID string, memoryIDs []string) error {
	if len(memoryIDs) < 2 {
		return nil
	}

	var edges []MemoryEdge
	for i := range len(memoryIDs) - 1 {
		edges = append(edges, MemoryEdge{
			SourceID: memoryIDs[i],
			TargetID: memoryIDs[i+1],
			EdgeType: EdgeTypeTemporal,
			Weight:   0.7,
			Metadata: map[string]any{"session_id": sessionID},
		})
	}

	return g.AddEdges(ctx, edges)
}

// CreateSimilarityEdges creates edges based on content similarity.
// This is a placeholder - real implementation would use embeddings.
func (g *KnowledgeGraph) CreateSimilarityEdges(ctx context.Context, memories []Memory, threshold float64) error {
	// Simple Jaccard similarity based on word overlap
	wordSets := make([]map[string]bool, len(memories))
	for i, m := range memories {
		wordSets[i] = make(map[string]bool)
		for word := range strings.FieldsSeq(strings.ToLower(m.Content)) {
			if len(word) > 3 {
				wordSets[i][word] = true
			}
		}
	}

	var edges []MemoryEdge
	for i := range memories {
		for j := i + 1; j < len(memories); j++ {
			sim := jaccardSimilarity(wordSets[i], wordSets[j])
			if sim >= threshold {
				edges = append(edges, MemoryEdge{
					SourceID: memories[i].ID,
					TargetID: memories[j].ID,
					EdgeType: EdgeTypeSimilar,
					Weight:   sim,
				})
			}
		}
	}

	if len(edges) == 0 {
		return nil
	}

	return g.AddEdges(ctx, edges)
}

func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	intersection := 0
	for word := range a {
		if b[word] {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

// GetStats returns statistics about the knowledge graph.
func (g *KnowledgeGraph) GetStats(ctx context.Context) (*GraphStats, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	stats := &GraphStats{}

	err := g.pool.WithConn(ctx, func(db *sql.DB) error {
		// Node count
		err := db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT id) FROM (
				SELECT source_id as id FROM memory_edges
				UNION
				SELECT target_id as id FROM memory_edges
			)
		`).Scan(&stats.NodeCount)
		if err != nil {
			return err
		}

		// Edge count
		err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_edges`).Scan(&stats.EdgeCount)
		if err != nil {
			return err
		}

		// Community count
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT community_id) FROM memory_pagerank WHERE community_id != ''
		`).Scan(&stats.CommunityCount)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if stats.NodeCount > 0 {
		stats.AvgDegree = float64(stats.EdgeCount*2) / float64(stats.NodeCount)
	}
	g.mu.RLock()
	stats.LastUpdated = g.cacheLastUpdated
	g.mu.RUnlock()

	return stats, nil
}

// DeleteMemoryEdges removes all edges involving a memory.
func (g *KnowledgeGraph) DeleteMemoryEdges(ctx context.Context, memoryID string) error {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	_, err := g.pool.Exec(ctx,
		`DELETE FROM memory_edges WHERE source_id = ? OR target_id = ?`,
		memoryID, memoryID)
	if err != nil {
		return err
	}

	_, err = g.pool.Exec(ctx,
		`DELETE FROM memory_pagerank WHERE memory_id = ?`,
		memoryID)

	g.invalidateCache()
	return err
}

func (g *KnowledgeGraph) invalidateCache() {
	g.mu.Lock()
	g.cacheLastUpdated = time.Time{}
	g.mu.Unlock()
}

// GetEdgesForMemory retrieves all edges for a memory, split into outgoing and incoming.
// This is useful for sync operations where edges need to be serialized with the memory.
func (g *KnowledgeGraph) GetEdgesForMemory(ctx context.Context, memoryID string) (out, in []MemoryEdge, err error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, nil, errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	db, err := g.pool.Get(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer g.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT id, source_id, target_id, edge_type, weight, confidence, metadata_json, created_at
		FROM memory_edges
		WHERE source_id = ? OR target_id = ?
		ORDER BY weight DESC
	`, memoryID, memoryID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get edges: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var edge MemoryEdge
		var edgeType, metaJSON, createdAtStr string

		if err := rows.Scan(&edge.ID, &edge.SourceID, &edge.TargetID,
			&edgeType, &edge.Weight, &edge.Confidence, &metaJSON, &createdAtStr); err != nil {
			return nil, nil, err
		}

		edge.EdgeType = EdgeType(edgeType)
		edge.Metadata = ParseMetadata(metaJSON)
		edge.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)

		if edge.SourceID == memoryID {
			out = append(out, edge)
		} else {
			in = append(in, edge)
		}
	}

	return out, in, rows.Err()
}

// ImportEdges bulk imports edges, typically used when hydrating from shared storage.
// This is more efficient than AddEdges for hydration as it skips cache invalidation
// until all edges are imported.
func (g *KnowledgeGraph) ImportEdges(ctx context.Context, edges []MemoryEdge) error {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	if len(edges) == 0 {
		return nil
	}

	return g.pool.WithConn(ctx, func(db *sql.DB) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		// Use INSERT OR IGNORE to handle duplicate edges gracefully
		stmt, err := tx.PrepareContext(ctx,
			`INSERT OR IGNORE INTO memory_edges
			(id, source_id, target_id, edge_type, weight, confidence, metadata_json, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, edge := range edges {
			if edge.ID == "" {
				edge.ID = fmt.Sprintf("%s-%s-%s", edge.SourceID[:min(8, len(edge.SourceID))], edge.TargetID[:min(8, len(edge.TargetID))], edge.EdgeType)
			}
			if edge.CreatedAt.IsZero() {
				edge.CreatedAt = time.Now()
			}
			if edge.Weight == 0 {
				edge.Weight = 0.5
			}
			if edge.Confidence == 0 {
				edge.Confidence = 1.0
			}

			metaJSON := (&Memory{Metadata: edge.Metadata}).MetadataJSON()

			_, err := stmt.ExecContext(ctx,
				edge.ID, edge.SourceID, edge.TargetID, string(edge.EdgeType),
				edge.Weight, edge.Confidence, metaJSON, edge.CreatedAt.Format(time.RFC3339),
			)
			if err != nil {
				return err
			}
		}

		g.invalidateCache()
		return tx.Commit()
	})
}

// GetMemoriesWithHighPageRank returns memory IDs with PageRank above the threshold.
// Used by distillation policy to find important memories for promotion.
func (g *KnowledgeGraph) GetMemoriesWithHighPageRank(ctx context.Context, threshold float64, limit int) ([]string, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	db, err := g.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer g.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT memory_id FROM memory_pagerank
		WHERE score >= ?
		ORDER BY score DESC
		LIMIT ?
	`, threshold, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// GetHighDegreeNodes returns memory IDs that are hub nodes (high connectivity).
// Used by distillation policy to identify structurally important memories.
func (g *KnowledgeGraph) GetHighDegreeNodes(ctx context.Context, minDegree, limit int) ([]string, error) {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return nil, errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	db, err := g.pool.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer g.pool.Put(db)

	rows, err := db.QueryContext(ctx, `
		SELECT memory_id, (in_degree + out_degree) as total_degree
		FROM memory_pagerank
		WHERE (in_degree + out_degree) >= ?
		ORDER BY total_degree DESC
		LIMIT ?
	`, minDegree, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		var degree int
		if err := rows.Scan(&id, &degree); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// EnsureNode ensures a node exists in the PageRank table.
// Creates a placeholder entry if the node doesn't exist.
// Useful during hydration when edges reference memories not yet imported.
func (g *KnowledgeGraph) EnsureNode(ctx context.Context, memoryID string) error {
	g.mu.RLock()
	if !g.initialized {
		g.mu.RUnlock()
		return errors.New("knowledge graph not initialized")
	}
	g.mu.RUnlock()

	_, err := g.pool.Exec(ctx,
		`INSERT OR IGNORE INTO memory_pagerank
		(memory_id, score, in_degree, out_degree, community_id, updated_at)
		VALUES (?, 0, 0, 0, '', ?)`,
		memoryID, time.Now().Format(time.RFC3339))
	return err
}

// Close releases resources.
func (g *KnowledgeGraph) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.initialized {
		return nil
	}

	g.initialized = false
	if g.pool != nil {
		return g.pool.Close() //nolint:mutexio // one-time teardown; initialized flag prevents re-entry
	}
	return nil
}

// Ensure KnowledgeGraph implements io.Closer
var _ io.Closer = (*KnowledgeGraph)(nil)
