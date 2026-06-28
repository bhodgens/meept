package memory

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/models"
	"github.com/caimlas/meept/pkg/sqlite"

	_ "modernc.org/sqlite"
)

//go:embed schema_local.sql
var localSchema embed.FS

//go:embed schema_gossip.sql
var gossipSchema embed.FS

const (
	// localDBName is the filename for the local data store.
	localDBName = "local.db"
	// gossipDBName is the filename for the gossip (replicated) data store.
	gossipDBName = "sync-gossip.db"

	// syncMetaVersion tracks the schema version for migration detection.
	syncMetaVersion = "1"

	// defaultMemoryLimit is used when the caller does not specify a limit.
	defaultMemoryLimit = 100
)

// GossipPublisher is the interface for publishing cluster gossip events.
// Implemented by cluster.GossipEngine to avoid importing internal/cluster
// from the memory package.
type GossipPublisher interface {
	PublishClusterEvent(eventType models.ClusterEventType, payload any) error
}

// DualStore routes memory operations between local.db (own data) and
// sync-gossip.db (replicated data from peers). Local reads take precedence;
// gossip data fills in gaps for merged queries.
type DualStore struct {
	localDB     *sql.DB
	gossipDB    *sql.DB
	localNodeID string
	gossipPub   GossipPublisher
	logger      *slog.Logger
	mu          sync.RWMutex
}
// runs their schemas, and returns a DualStore. The caller should call Close()
// when done.
func NewDualStore(dataDir string, nodeID string, logger *slog.Logger) (*DualStore, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if nodeID == "" {
		return nil, fmt.Errorf("dual store: nodeID must not be empty")
	}

	ds := &DualStore{
		localNodeID: nodeID,
		logger:      logger,
	}

	// Open/create local.db.
	localPath := filepath.Join(dataDir, localDBName)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("dual store: create data dir %s: %w", dataDir, err)
	}

	localDB, err := openWithRetries(localPath, logger, 3)
	if err != nil {
		return nil, fmt.Errorf("dual store: open local.db: %w", err)
	}
	ds.localDB = localDB

	// Open/create gossip.db.
	gossipPath := filepath.Join(dataDir, gossipDBName)
	gossipDB, err := openWithRetries(gossipPath, logger, 3)
	if err != nil {
		localDB.Close()
		return nil, fmt.Errorf("dual store: open sync-gossip.db: %w", err)
	}
	ds.gossipDB = gossipDB

	// Initialize schemas.
	if err := ds.initLocalSchema(context.Background()); err != nil {
		localDB.Close()
		gossipDB.Close()
		return nil, fmt.Errorf("dual store: init local schema: %w", err)
	}
	if err := ds.initGossipSchema(context.Background()); err != nil {
		localDB.Close()
		gossipDB.Close()
		return nil, fmt.Errorf("dual store: init gossip schema: %w", err)
	}

	logger.Debug("dual store initialized", "local", localPath, "gossip", gossipPath)
	return ds, nil
}


// openWithRetries opens a SQLite database with a couple of retry attempts.
func openWithRetries(path string, logger *slog.Logger, retries int) (*sql.DB, error) {
	var lastErr error
	for i := 0; i < retries; i++ {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			lastErr = err
			continue
		}
		if err := db.Ping(); err != nil {
			db.Close()
			lastErr = err
			continue
		}
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(2)
		return db, nil
	}
	return nil, fmt.Errorf("open %q after %d retries: %w", path, retries, lastErr)
}

// Close closes both database connections.
func (s *DualStore) Close() error {
	var errs []error
	if s.localDB != nil {
		if err := s.localDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close local.db: %w", err))
		}
	}
	if s.gossipDB != nil {
		if err := s.gossipDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close gossip.db: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("dual store close errors (%d): %v", len(errs), errs)
	}
	return nil
}

// SetGossipPublisher configures the gossip publisher for cluster sync
// so that local memory writes are automatically broadcast to peers.
func (s *DualStore) SetGossipPublisher(pub GossipPublisher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gossipPub = pub
}

// IsLocal returns true if nodeID matches the local node's ID.
func (s *DualStore) IsLocal(nodeID string) bool {
	return nodeID == s.localNodeID
}

// LocalDB returns the local database handle (for advanced use).
func (s *DualStore) LocalDB() *sql.DB {
	return s.localDB
}

// GossipDB returns the gossip database handle (for advanced use).
func (s *DualStore) GossipDB() *sql.DB {
	return s.gossipDB
}

// ---------- schema initialization ----------

// initLocalSchema runs the local schema on local.db.
func (s *DualStore) initLocalSchema(ctx context.Context) error {
	schema, err := localSchema.ReadFile("schema_local.sql")
	if err != nil {
		return fmt.Errorf("read local schema: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.localDB.ExecContext(ctx, string(schema)); err != nil {
		return fmt.Errorf("exec local schema: %w", err)
	}

	if err := s.setSyncMetaLocal(ctx, "schema_version", syncMetaVersion); err != nil {
		return fmt.Errorf("set local schema_version: %w", err)
	}
	return nil
}

// initGossipSchema runs the gossip schema on gossip.db.
func (s *DualStore) initGossipSchema(ctx context.Context) error {
	schema, err := gossipSchema.ReadFile("schema_gossip.sql")
	if err != nil {
		return fmt.Errorf("read gossip schema: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.gossipDB.ExecContext(ctx, string(schema)); err != nil {
		return fmt.Errorf("exec gossip schema: %w", err)
	}

	if err := s.setSyncMetaGossip(ctx, "schema_version", syncMetaVersion); err != nil {
		return fmt.Errorf("set gossip schema_version: %w", err)
	}
	if err := s.setSyncMetaGossip(ctx, "local_node_id", s.localNodeID); err != nil {
		return fmt.Errorf("set gossip local_node_id: %w", err)
	}
	return nil
}

// setSyncMetaLocal writes a key/value pair to the local sync_metadata table.
func (s *DualStore) setSyncMetaLocal(ctx context.Context, key, value string) error {
	_, err := s.localDB.ExecContext(ctx,
		`INSERT OR REPLACE INTO sync_metadata (key, value) VALUES (?, ?)`,
		key, value)
	return err
}

// setSyncMetaGossip writes a key/value pair to the gossip sync_metadata table.
func (s *DualStore) setSyncMetaGossip(ctx context.Context, key, value string) error {
	_, err := s.gossipDB.ExecContext(ctx,
		`INSERT OR REPLACE INTO sync_metadata (key, value) VALUES (?, ?)`,
		key, value)
	return err
}

// ---------- write routing ----------

// StoreMemory persists a memory record to the appropriate database based on
// ownership. If the memory is from this node (source_node absent or equal to
// localNodeID) it goes to local.db; otherwise to gossip.db.
// Local writes are also broadcast to gossip peers (non-blocking).
func (s *DualStore) StoreMemory(ctx context.Context, mem *Memory) error {
	sourceNode := memorySourceNode(mem)
	if sourceNode == s.localNodeID || sourceNode == "" {
		s.mu.Lock()
		if err := s.storeMemoryLocal(ctx, mem); err != nil {
			s.mu.Unlock()
			return err
		}
		// Snapshot publisher under lock, release, then publish outside.
		pub := s.gossipPub
		s.mu.Unlock()

		if pub != nil {
			s.publishMemoryGossip(pub, mem)
		}
		return nil
	}

	s.mu.Lock()
	err := s.storeMemoryGossip(ctx, mem, sourceNode)
	s.mu.Unlock()
	return err
}

// publishMemoryGossip broadcasts a locally-written memory to gossip peers
// non-blocking. The publish happens outside the DualStore mutex to avoid
// holding the lock during I/O (CLAUDE.md mutex-scope rule).
func (s *DualStore) publishMemoryGossip(pub GossipPublisher, mem *Memory) {
	payload := models.MemoryStoredPayload{
		ID:        mem.ID,
		Type:      string(mem.Type),
		Category:  mem.Category,
		Content:   mem.Content,
		CreatedAt: mem.CreatedAt.UnixNano(),
		AgentID:   mem.AgentID,
		SessionID: mem.SessionID,
		Metadata:  mem.Metadata,
	}
	go func() {
		if err := pub.PublishClusterEvent(models.EventTypeMemoryStored, payload); err != nil {
			s.logger.Warn("dual store: gossip publish failed", "mem_id", mem.ID, "error", err)
		}
	}()
}

// StoreRemoteMemory writes a memory from a peer node to gossip.db.
func (s *DualStore) StoreRemoteMemory(ctx context.Context, mem *Memory, sourceNode string) error {
	if sourceNode == "" {
		return fmt.Errorf("dual store: StoreRemoteMemory requires non-empty sourceNode")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storeMemoryGossip(ctx, mem, sourceNode)
}

// storeMemoryLocal writes a memory to local.db.
func (s *DualStore) storeMemoryLocal(ctx context.Context, mem *Memory) error {
	metaJSON := mem.MetadataJSON()
	createdAt := mem.CreatedAt.UTC().Format(time.RFC3339Nano)

	updatedAt := ""
	if mem.UpdatedAt != nil {
		updatedAt = mem.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}

	_, err := s.localDB.ExecContext(ctx,
		`INSERT OR REPLACE INTO memories
		 (id, type, category, content, metadata_json, created_at, updated_at, agent_id, session_id, bot_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID,
		string(mem.Type),
		mem.Category,
		mem.Content,
		metaJSON,
		createdAt,
		updatedAt,
		mem.AgentID,
		mem.SessionID,
		mem.BotID,
	)
	return err
}

// storeMemoryGossip writes a memory to gossip.db with source_node.
func (s *DualStore) storeMemoryGossip(ctx context.Context, mem *Memory, sourceNode string) error {
	metaJSON := mem.MetadataJSON()
	createdAt := mem.CreatedAt.UTC().Format(time.RFC3339Nano)

	updatedAt := ""
	if mem.UpdatedAt != nil {
		updatedAt = mem.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}

	_, err := s.gossipDB.ExecContext(ctx,
		`INSERT OR REPLACE INTO memories
		 (id, type, category, content, metadata_json, created_at, updated_at, agent_id, session_id, bot_id, source_node)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		mem.ID,
		string(mem.Type),
		mem.Category,
		mem.Content,
		metaJSON,
		createdAt,
		updatedAt,
		mem.AgentID,
		mem.SessionID,
		mem.BotID,
		sourceNode,
	)
	return err
}

// ---------- merged reads ----------

// GetMemories retrieves memories with optional filtering, merging local and
// gossip data. Local results appear first, then gossip. Duplicate IDs are
// deduplicated (local wins).
func (s *DualStore) GetMemories(ctx context.Context, query *MemoryQuery) ([]MemoryResult, error) {
	if query == nil {
		query = &MemoryQuery{}
	}

	var localResults []MemoryResult
	if s.localDB != nil {
		if ok, e := s.tableExists(ctx, s.localDB, "memories"); e == nil && ok {
			var err error
			localResults, err = s.queryMemoriesLocal(ctx, query)
			if err != nil {
				s.logger.Debug("dual store: query memories local failed", "error", err)
			}
		}
	}

	var gossipResults []MemoryResult
	if s.gossipDB != nil {
		if ok, e := s.tableExists(ctx, s.gossipDB, "memories"); e == nil && ok {
			var err error
			gossipResults, err = s.queryMemoriesGossip(ctx, query)
			if err != nil {
				s.logger.Debug("dual store: query memories gossip failed", "error", err)
			}
		}
	}

	if len(localResults) == 0 && len(gossipResults) == 0 {
		return nil, nil
	}

	// Merge with local dedup (local wins on conflict).
	seen := make(map[string]bool, len(localResults))
	for _, r := range localResults {
		seen[r.Memory.ID] = true
	}
	for _, r := range gossipResults {
		if !seen[r.Memory.ID] {
			localResults = append(localResults, r)
		}
	}

	return localResults, nil
}

// GetMemoriesByType retrieves memories filtered by type, merged from both DBs.
func (s *DualStore) GetMemoriesByType(ctx context.Context, memType MemoryType, limit int) ([]MemoryResult, error) {
	return s.GetMemories(ctx, &MemoryQuery{Type: memType, Limit: limit})
}

// GetRecentMemories retrieves the most recent memories up to limit, merged.
func (s *DualStore) GetRecentMemories(ctx context.Context, limit int) ([]MemoryResult, error) {
	return s.GetMemories(ctx, &MemoryQuery{Limit: limit})
}

func (s *DualStore) queryMemoriesLocal(ctx context.Context, query *MemoryQuery) ([]MemoryResult, error) {
	return s.scanMemoriesFromDB(ctx, s.localDB, nil, query)
}

func (s *DualStore) queryMemoriesGossip(ctx context.Context, query *MemoryQuery) ([]MemoryResult, error) {
	if s.gossipDB == nil {
		return nil, nil
	}
	// Count rows in gossip.
	var c int
	_ = s.gossipDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM memories").Scan(&c)
	s.logger.Debug("dual store: gossip memories count", "count", c)
	sn := sql.NullString{Valid: true}
	res, err := s.scanMemoriesFromDB(ctx, s.gossipDB, &sn, query)
	s.logger.Debug("dual store: gossip memories scan done", "count", len(res), "error", err)
	return res, err
}

const (
	memColsLocal   = "id, type, category, content, metadata_json, created_at, updated_at, agent_id, session_id, bot_id"
	memColsGossip  = "id, type, category, content, metadata_json, created_at, updated_at, agent_id, session_id, bot_id, source_node"
)

// scanMemoriesFromDB builds and runs a query against the given DB for the
// memories table, applying the query's filters. When sourceNode != nil the
// gossip schema (with source_node) is used.
func (s *DualStore) scanMemoriesFromDB(ctx context.Context, db *sql.DB, sourceNode *sql.NullString, query *MemoryQuery) ([]MemoryResult, error) {
	var cols []any
	var selectExpr string

	if sourceNode != nil && sourceNode.Valid {
		cols = []any{
			new(string), new(string), new(string), new(string), new(string),
			new(string), new(string), new(string), new(string), new(string),
			new(string),
		}
		selectExpr = memColsGossip
	} else {
		cols = []any{
			new(string), new(string), new(string), new(string), new(string),
			new(string), new(string), new(string), new(string), new(string),
		}
		selectExpr = memColsLocal
	}

	// Build WHERE clause with parameter markers.
	var whereClauses []string
	var args []any

	if query.Type != "" {
		whereClauses = append(whereClauses, "type = ?")
		args = append(args, string(query.Type))
	}
	if query.Category != "" {
		whereClauses = append(whereClauses, "category = ?")
		args = append(args, query.Category)
	}
	// Only add source_node filter when sourceNode is non-nil with a
	// non-empty string (i.e. caller explicitly wants a per-node query).
	// When Valid is true but String is empty, it's a signal to use
	// the gossip schema without filtering.
	if sourceNode != nil && sourceNode.Valid && sourceNode.String != "" {
		whereClauses = append(whereClauses, "source_node = ?")
		args = append(args, sourceNode.String)
	}
	if query.Query != "" {
		safeQ := sqlite.SanitizeQuery(query.Query)
		if safeQ != "" {
			whereClauses = append(whereClauses, "content LIKE ?")
			args = append(args, "%"+safeQ+"%")
		}
	}

	limit := query.Limit
	if limit <= 0 {
		limit = defaultMemoryLimit
	}
	args = append(args, limit)

	var queryStr string
	if len(whereClauses) > 0 {
		queryStr = fmt.Sprintf("SELECT %s FROM memories WHERE %s ORDER BY created_at DESC LIMIT ?",
			selectExpr, strings.Join(whereClauses, " AND "))
	} else {
		queryStr = fmt.Sprintf("SELECT %s FROM memories ORDER BY created_at DESC LIMIT ?", selectExpr)
	}

	rows, err := db.QueryContext(ctx, queryStr, args...)
	if err != nil {
		return nil, fmt.Errorf("dual store query memories: %w", err)
	}
	defer rows.Close()

	var results []MemoryResult
	for rows.Next() {
		if err := rows.Scan(cols...); err != nil {
			return nil, fmt.Errorf("dual store scan memory row: %w", err)
		}

		strs := make([]string, len(cols))
		for i, c := range cols {
			strs[i] = *(c.(*string))
		}

		mem := Memory{
			ID:        strs[0],
			Type:      MemoryType(strs[1]),
			Category:  strs[2],
			Content:   strs[3],
			Metadata:  ParseMetadata(strs[4]),
			CreatedAt: parseTimeRFC(strs[5]),
		}
		if t := parseTimeRFC(strs[6]); !t.IsZero() {
			mem.UpdatedAt = &t
		}
		mem.AgentID = strs[7]
		mem.SessionID = strs[8]
		mem.BotID = strs[9]

		if sourceNode != nil && sourceNode.Valid {
			if mem.Metadata == nil {
				mem.Metadata = make(map[string]any)
			}
			mem.Metadata["source_node"] = strs[10]
		}

		source := "memory"
		if mem.Type != "" {
			source = string(mem.Type)
		}
		if sourceNode != nil && sourceNode.Valid {
			source = fmt.Sprintf("gossip:%s", strs[10])
		}

		score := 1.0
		if query.MinRelevance > 0 && score < query.MinRelevance {
			continue
		}

		results = append(results, MemoryResult{
			Memory:         mem,
			RelevanceScore: score,
			Source:         source,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dual store rows iteration error: %w", err)
	}

	return results, nil
}

// parseTimeRFC is a safe wrapper around time.Parse for RFC3339Nano.
func parseTimeRFC(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// tableExists reports whether the given table exists in the database.
func (s *DualStore) tableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
		table).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetMemoryCountByOwner returns how many memories are stored locally vs in gossip.
func (s *DualStore) GetMemoryCountByOwner(ctx context.Context) (local int, gossip int, err error) {
	if s.localDB != nil {
		if exists, e := s.tableExists(ctx, s.localDB, "memories"); e == nil && exists {
			s.localDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM memories").Scan(&local)
		}
	}
	if s.gossipDB != nil {
		if exists, e := s.tableExists(ctx, s.gossipDB, "memories"); e == nil && exists {
			s.gossipDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM memories").Scan(&gossip)
		}
	}
	return local, gossip, nil
}

// memorySourceNode extracts the source node from a memory's metadata (if set).
// Returns "" when the memory appears to originate from this node.
func memorySourceNode(mem *Memory) string {
	if mem.Metadata != nil {
		if sn, ok := mem.Metadata["source_node"].(string); ok && sn != "" {
			return sn
		}
	}
	return ""
}

// ---------- session / turn routing (T3.2 / T3.3) ----------

// Session is the dual-store's session representation. It mirrors the
// columns of the sessions table in schema_local.sql / schema_gossip.sql.
// The fields are intentionally a subset of internal/session.Session that
// are meaningful for cross-node replication; presentation-layer concerns
// (workers, designation, etc.) stay in the session package.
type Session struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	ConversationID string                 `json:"conversation_id"`
	CreatedAt      time.Time              `json:"created_at"`
	LastActivity   time.Time              `json:"last_activity"`
	Description    string                 `json:"description,omitempty"`
	ProjectID      string                 `json:"project_id,omitempty"`
	ProjectPath    string                 `json:"project_path,omitempty"`
	NoFence        bool                   `json:"no_fence,omitempty"`
	Metadata       map[string]any         `json:"metadata,omitempty"`
	SourceNode     string                 `json:"source_node,omitempty"` // only set for gossip-sourced sessions
}

// Turn is the dual-store's turn representation, mirroring the turns table.
type Turn struct {
	TurnID      string         `json:"turn_id"`
	SessionID   string         `json:"session_id"`
	Role        string         `json:"role"`
	Content     string         `json:"content"`
	Timestamp   time.Time      `json:"timestamp"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	SourceNode  string         `json:"source_node,omitempty"` // only set for gossip-sourced turns
}

// sessionMetadataJSON serializes a session's metadata for SQL storage.
func sessionMetadataJSON(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// StoreSession writes a session to local.db (this node owns it) and
// publishes a SESSION_CREATED gossip event when a publisher is configured.
// The caller must populate Session.ID; if it is empty the write is rejected
// rather than silently generating one (sessions are created upstream by the
// session store, which has its own ID generation contract).
func (s *DualStore) StoreSession(ctx context.Context, sess *Session) error {
	if sess == nil || sess.ID == "" {
		return fmt.Errorf("dual store: StoreSession requires a non-empty session ID")
	}

	s.mu.Lock()
	if err := s.storeSessionLocal(ctx, sess); err != nil {
		s.mu.Unlock()
		return err
	}
	pub := s.gossipPub
	s.mu.Unlock()

	if pub != nil {
		s.publishSessionCreatedGossip(pub, sess)
	}
	return nil
}

// publishSessionCreatedGossip non-blocking broadcasts a SESSION_CREATED
// event. Runs outside the DualStore mutex per CLAUDE.md mutex-scope rule.
func (s *DualStore) publishSessionCreatedGossip(pub GossipPublisher, sess *Session) {
	payload := models.SessionCreatedPayload{
		SessionID: sess.ID,
		Title:     sess.Name,
		CreatedAt: sess.CreatedAt.UnixNano(),
		Metadata:  sess.Metadata,
	}
	go func() {
		if err := pub.PublishClusterEvent(models.EventTypeSessionCreated, payload); err != nil {
			s.logger.Warn("dual store: session gossip publish failed", "session_id", sess.ID, "error", err)
		}
	}()
}

// StoreRemoteSession writes a session received from a peer to gossip.db.
// Used by the gossip handler to record sessions observed from other nodes.
func (s *DualStore) StoreRemoteSession(ctx context.Context, sess *Session, sourceNode string) error {
	if sourceNode == "" {
		return fmt.Errorf("dual store: StoreRemoteSession requires non-empty sourceNode")
	}
	if sess == nil || sess.ID == "" {
		return fmt.Errorf("dual store: StoreRemoteSession requires a non-empty session")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storeSessionGossip(ctx, sess, sourceNode)
}

func (s *DualStore) storeSessionLocal(ctx context.Context, sess *Session) error {
	_, err := s.localDB.ExecContext(ctx,
		`INSERT OR REPLACE INTO sessions
		 (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id, project_id, project_path, no_fence, metadata_json)
		 VALUES (?, ?, ?, ?, ?, '[]', '[]', ?, NULL, ?, ?, ?, ?)`,
		sess.ID, sess.Name, sess.ConversationID,
		sess.CreatedAt.UTC().Format(time.RFC3339Nano),
		sess.LastActivity.UTC().Format(time.RFC3339Nano),
		sess.Description,
		sess.ProjectID, sess.ProjectPath,
		boolToInt(sess.NoFence),
		sessionMetadataJSON(sess.Metadata),
	)
	return err
}

func (s *DualStore) storeSessionGossip(ctx context.Context, sess *Session, sourceNode string) error {
	_, err := s.gossipDB.ExecContext(ctx,
		`INSERT OR REPLACE INTO sessions
		 (id, name, conversation_id, created_at, last_activity, attached_clients, worker_ids, description, leaf_message_id, project_id, project_path, no_fence, metadata_json, source_node)
		 VALUES (?, ?, ?, ?, ?, '[]', '[]', ?, NULL, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Name, sess.ConversationID,
		sess.CreatedAt.UTC().Format(time.RFC3339Nano),
		sess.LastActivity.UTC().Format(time.RFC3339Nano),
		sess.Description,
		sess.ProjectID, sess.ProjectPath,
		boolToInt(sess.NoFence),
		sessionMetadataJSON(sess.Metadata),
		sourceNode,
	)
	return err
}

// boolToInt converts a bool to the integer representation used by SQLite.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// StoreTurn writes a turn to local.db (this node created it) and publishes
// a SESSION_TURN gossip event when a publisher is configured. Caller must
// populate Turn.TurnID; the production path (session SQLiteStore) generates
// IDs via pkg/id and passes them in.
func (s *DualStore) StoreTurn(ctx context.Context, turn *Turn) error {
	if turn == nil || turn.TurnID == "" {
		return fmt.Errorf("dual store: StoreTurn requires a non-empty turn ID")
	}

	s.mu.Lock()
	if err := s.storeTurnLocal(ctx, turn); err != nil {
		s.mu.Unlock()
		return err
	}
	pub := s.gossipPub
	s.mu.Unlock()

	if pub != nil {
		s.publishTurnGossip(pub, turn)
	}
	return nil
}

// publishTurnGossip non-blocking broadcasts a SESSION_TURN event.
// Runs outside the DualStore mutex per CLAUDE.md mutex-scope rule.
func (s *DualStore) publishTurnGossip(pub GossipPublisher, turn *Turn) {
	payload := models.SessionTurnPayload{
		SessionID: turn.SessionID,
		TurnID:    turn.TurnID,
		Role:      turn.Role,
		Content:   turn.Content,
		Timestamp: turn.Timestamp.UnixNano(),
	}
	go func() {
		if err := pub.PublishClusterEvent(models.EventTypeSessionTurn, payload); err != nil {
			s.logger.Warn("dual store: turn gossip publish failed", "turn_id", turn.TurnID, "error", err)
		}
	}()
}

// StoreRemoteTurn writes a turn received from a peer to gossip.db. Used by
// the gossip handler for SESSION_TURN events from other nodes.
func (s *DualStore) StoreRemoteTurn(ctx context.Context, turn *Turn, sourceNode string) error {
	if sourceNode == "" {
		return fmt.Errorf("dual store: StoreRemoteTurn requires non-empty sourceNode")
	}
	if turn == nil || turn.TurnID == "" {
		return fmt.Errorf("dual store: StoreRemoteTurn requires a non-empty turn")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storeTurnGossip(ctx, turn, sourceNode)
}

func (s *DualStore) storeTurnLocal(ctx context.Context, turn *Turn) error {
	_, err := s.localDB.ExecContext(ctx,
		`INSERT OR REPLACE INTO turns
		 (turn_id, session_id, role, content, timestamp, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		turn.TurnID, turn.SessionID, turn.Role, turn.Content,
		turn.Timestamp.UTC().UnixNano(),
		sessionMetadataJSON(turn.Metadata),
	)
	return err
}

func (s *DualStore) storeTurnGossip(ctx context.Context, turn *Turn, sourceNode string) error {
	_, err := s.gossipDB.ExecContext(ctx,
		`INSERT OR REPLACE INTO turns
		 (turn_id, session_id, role, content, timestamp, metadata_json, source_node)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		turn.TurnID, turn.SessionID, turn.Role, turn.Content,
		turn.Timestamp.UTC().UnixNano(),
		sessionMetadataJSON(turn.Metadata),
		sourceNode,
	)
	return err
}

// ---------- session / turn merged reads ----------

// GetSession retrieves a session by ID, checking local.db first and then
// gossip.db. Returns nil, nil when the session does not exist in either DB.
func (s *DualStore) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, nil
	}

	if s.localDB != nil {
		if ok, _ := s.tableExists(ctx, s.localDB, "sessions"); ok {
			sess, err := scanSessionRow(s.localDB.QueryRowContext(ctx,
				selectSessionColsLocal+" FROM sessions WHERE id = ?", sessionID), false)
			if err != nil {
				return nil, fmt.Errorf("dual store: query local session: %w", err)
			}
			if sess != nil {
				return sess, nil
			}
		}
	}

	if s.gossipDB != nil {
		if ok, _ := s.tableExists(ctx, s.gossipDB, "sessions"); ok {
			sess, err := scanSessionRow(s.gossipDB.QueryRowContext(ctx,
				selectSessionColsGossip+" FROM sessions WHERE id = ?", sessionID), true)
			if err != nil {
				return nil, fmt.Errorf("dual store: query gossip session: %w", err)
			}
			return sess, nil
		}
	}
	return nil, nil
}

// GetSessions returns every session across both DBs (local first, then
// gossip). Duplicate IDs are deduplicated: local wins.
func (s *DualStore) GetSessions(ctx context.Context) ([]*Session, error) {
	var out []*Session
	seen := make(map[string]bool)

	if s.localDB != nil {
		if ok, _ := s.tableExists(ctx, s.localDB, "sessions"); ok {
			rows, err := s.localDB.QueryContext(ctx, selectSessionColsLocal+" FROM sessions ORDER BY last_activity DESC")
			if err != nil {
				return nil, fmt.Errorf("dual store: query local sessions: %w", err)
			}
			sessions, err := scanSessionRows(rows, false)
			rows.Close()
			if err != nil {
				return nil, err
			}
			for _, sess := range sessions {
				seen[sess.ID] = true
				out = append(out, sess)
			}
		}
	}

	if s.gossipDB != nil {
		if ok, _ := s.tableExists(ctx, s.gossipDB, "sessions"); ok {
			rows, err := s.gossipDB.QueryContext(ctx, selectSessionColsGossip+" FROM sessions ORDER BY last_activity DESC")
			if err != nil {
				return nil, fmt.Errorf("dual store: query gossip sessions: %w", err)
			}
			sessions, err := scanSessionRows(rows, true)
			rows.Close()
			if err != nil {
				return nil, err
			}
			for _, sess := range sessions {
				if !seen[sess.ID] {
					out = append(out, sess)
				}
			}
		}
	}

	return out, nil
}

// GetTurnsForSession returns all turns for a session (local first, then
// gossip), ordered by timestamp ascending within each shard.
func (s *DualStore) GetTurnsForSession(ctx context.Context, sessionID string) ([]*Turn, error) {
	if sessionID == "" {
		return nil, nil
	}
	var out []*Turn

	if s.localDB != nil {
		if ok, _ := s.tableExists(ctx, s.localDB, "turns"); ok {
			rows, err := s.localDB.QueryContext(ctx,
				selectTurnColsLocal+" FROM turns WHERE session_id = ? ORDER BY timestamp ASC", sessionID)
			if err != nil {
				return nil, fmt.Errorf("dual store: query local turns: %w", err)
			}
			turns, err := scanTurnRows(rows, false)
			rows.Close()
			if err != nil {
				return nil, err
			}
			out = append(out, turns...)
		}
	}

	if s.gossipDB != nil {
		if ok, _ := s.tableExists(ctx, s.gossipDB, "turns"); ok {
			rows, err := s.gossipDB.QueryContext(ctx,
				selectTurnColsGossip+" FROM turns WHERE session_id = ? ORDER BY timestamp ASC", sessionID)
			if err != nil {
				return nil, fmt.Errorf("dual store: query gossip turns: %w", err)
			}
			turns, err := scanTurnRows(rows, true)
			rows.Close()
			if err != nil {
				return nil, err
			}
			out = append(out, turns...)
		}
	}
	return out, nil
}

// Column lists kept in one place so local/gossip reads stay in sync.
const (
	selectSessionColsLocal  = "SELECT id, name, conversation_id, created_at, last_activity, description, project_id, project_path, no_fence, metadata_json"
	selectSessionColsGossip = selectSessionColsLocal + ", source_node"
	selectTurnColsLocal     = "SELECT turn_id, session_id, role, content, timestamp, metadata_json"
	selectTurnColsGossip    = selectTurnColsLocal + ", source_node"
)

// scanSessionRow scans a single session row. When fromGossip is true the
// row is expected to include a trailing source_node column.
func scanSessionRow(row *sql.Row, fromGossip bool) (*Session, error) {
	var (
		sess                Session
		createdAt, lastAct  string
		desc                sql.NullString
		projID, projPath    sql.NullString
		noFence             int
		metaJSON            string
		sourceNode          sql.NullString
	)
	var err error
	if fromGossip {
		err = row.Scan(&sess.ID, &sess.Name, &sess.ConversationID,
			&createdAt, &lastAct, &desc, &projID, &projPath,
			&noFence, &metaJSON, &sourceNode)
	} else {
		err = row.Scan(&sess.ID, &sess.Name, &sess.ConversationID,
			&createdAt, &lastAct, &desc, &projID, &projPath,
			&noFence, &metaJSON)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	sess.Description = desc.String
	sess.ProjectID = projID.String
	sess.ProjectPath = projPath.String
	sess.NoFence = noFence != 0
	if sourceNode.Valid {
		sess.SourceNode = sourceNode.String
	}
	if t := parseTimeRFC(createdAt); !t.IsZero() {
		sess.CreatedAt = t
	}
	if t := parseTimeRFC(lastAct); !t.IsZero() {
		sess.LastActivity = t
	}
	sess.Metadata = ParseMetadata(metaJSON)
	return &sess, nil
}

// scanSessionRows scans a session rows iterator.
func scanSessionRows(rows *sql.Rows, fromGossip bool) ([]*Session, error) {
	var out []*Session
	for rows.Next() {
		var (
			sess                Session
			createdAt, lastAct  string
			desc                sql.NullString
			projID, projPath    sql.NullString
			noFence             int
			metaJSON            string
			sourceNode          sql.NullString
		)
		var err error
		if fromGossip {
			err = rows.Scan(&sess.ID, &sess.Name, &sess.ConversationID,
				&createdAt, &lastAct, &desc, &projID, &projPath,
				&noFence, &metaJSON, &sourceNode)
		} else {
			err = rows.Scan(&sess.ID, &sess.Name, &sess.ConversationID,
				&createdAt, &lastAct, &desc, &projID, &projPath,
				&noFence, &metaJSON)
		}
		if err != nil {
			return nil, fmt.Errorf("dual store: scan session row: %w", err)
		}
		sess.Description = desc.String
		sess.ProjectID = projID.String
		sess.ProjectPath = projPath.String
		sess.NoFence = noFence != 0
		if sourceNode.Valid {
			sess.SourceNode = sourceNode.String
		}
		if t := parseTimeRFC(createdAt); !t.IsZero() {
			sess.CreatedAt = t
		}
		if t := parseTimeRFC(lastAct); !t.IsZero() {
			sess.LastActivity = t
		}
		sess.Metadata = ParseMetadata(metaJSON)
		out = append(out, &sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dual store: session rows iteration: %w", err)
	}
	return out, nil
}

// scanTurnRows scans a turn rows iterator. When fromGossip is true the
// row includes a trailing source_node column.
func scanTurnRows(rows *sql.Rows, fromGossip bool) ([]*Turn, error) {
	var out []*Turn
	for rows.Next() {
		var (
			turn       Turn
			tsUnix     int64
			metaJSON   string
			sourceNode sql.NullString
		)
		var err error
		if fromGossip {
			err = rows.Scan(&turn.TurnID, &turn.SessionID, &turn.Role,
				&turn.Content, &tsUnix, &metaJSON, &sourceNode)
		} else {
			err = rows.Scan(&turn.TurnID, &turn.SessionID, &turn.Role,
				&turn.Content, &tsUnix, &metaJSON)
		}
		if err != nil {
			return nil, fmt.Errorf("dual store: scan turn row: %w", err)
		}
		turn.Timestamp = time.Unix(0, tsUnix).UTC()
		if sourceNode.Valid {
			turn.SourceNode = sourceNode.String
		}
		turn.Metadata = ParseMetadata(metaJSON)
		out = append(out, &turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dual store: turn rows iteration: %w", err)
	}
	return out, nil
}

// GetSessionTurnCountByOwner returns how many turns are stored locally vs
// gossip. Useful for diagnostics and tests.
func (s *DualStore) GetSessionTurnCountByOwner(ctx context.Context) (local int, gossip int, err error) {
	if s.localDB != nil {
		if exists, e := s.tableExists(ctx, s.localDB, "turns"); e == nil && exists {
			s.localDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM turns").Scan(&local)
		}
	}
	if s.gossipDB != nil {
		if exists, e := s.tableExists(ctx, s.gossipDB, "turns"); e == nil && exists {
			s.gossipDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM turns").Scan(&gossip)
		}
	}
	return local, gossip, nil
}

// PublishTurn adapts the DualStore to the session package's
// TurnGossipPublisher interface. It writes the turn to local.db and then
// publishes a SESSION_TURN gossip event. The session package calls this
// method (via the interface) after a successful SaveMessages commit so
// that peers see the turn via gossip.
//
// The method is non-blocking: gossip publication runs in a goroutine.
// The TurnID is caller-supplied and used as the primary key for
// idempotency (INSERT OR REPLACE).
func (s *DualStore) PublishTurn(sessionID, turnID, role, content string, ts time.Time) error {
	turn := &Turn{
		TurnID:    turnID,
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Timestamp: ts,
	}
	return s.StoreTurn(context.Background(), turn)
}
