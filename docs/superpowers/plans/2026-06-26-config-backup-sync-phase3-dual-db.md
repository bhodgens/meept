# Phase 3: Dual-DB Router - Implementation Plan

**Spec:** `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`
**Date:** 2026-06-26
**Status:** Ready for implementation
**Prerequisites:** Phase 1 (Git Backup Scheduler) complete

---

## Overview

This plan implements the Dual-DB routing system that splits storage between `local.db` (unique data, backed up) and `sync-gossip.db` (replicated data from peers, recovered via sync/gossip). This is the architectural foundation that enables storage-efficient backups.

### Scope

| In Scope | Out of Scope |
|----------|--------------|
| Dual-DB initialization | Gossip event schema (Phase 4) |
| Write routing (local vs gossip) | Config sync (Phase 5) |
| Merged read queries | Migration CLI tool |
| Schema creation for both DBs | |

---

## Phase 3 Checklist

### Task 3.1: Dual-DB Store Structure

**File:** `internal/memory/dual_store.go` (new)

```go
package memory

type DualStore struct {
    localDB      *sql.DB    // Unique data owned by this node
    gossipDB     *sql.DB    // Replicated data from peers
    localNodeID  string
    logger       *slog.Logger
    mu           sync.RWMutex
}

// NewDualStore creates both databases and initializes schemas
func NewDualStore(dataDir string, nodeID string, logger *slog.Logger) (*DualStore, error)

// Close closes both database connections
func (s *DualStore) Close() error

// getReadDBs returns both DBs for reading (local first, then gossip)
func (s *DualStore) getReadDBs() [2]*sql.DB
```

**Implementation steps:**
1. Create `internal/memory/dual_store.go`
2. Implement `NewDualStore`:
   - Create/open `local.db`
   - Create/open `sync-gossip.db`
   - Initialize schemas on both
3. Implement connection pooling (share same pool size)
4. Add logging for DB operations

**Schema files:**
- `internal/memory/schema_local.sql` - local.db schema
- `internal/memory/schema_gossip.sql` - sync-gossip.db schema (includes source_node)

**Tests:** `internal/memory/dual_store_test.go`

---

### Task 3.2: Write Routing

**File:** `internal/memory/dual_store.go`

```go
// StoreSession writes a session to local.db (this node owns it)
func (s *DualStore) StoreSession(ctx context.Context, session *Session) error

// StoreTurn writes a turn to local.db (this node created it)
func (s *DualStore) StoreTurn(ctx context.Context, turn *Turn) error

// StoreMemory writes a memory to local.db (this node created it)
func (s *DualStore) StoreMemory(ctx context.Context, memory *Memory) error

// StoreRemoteTurn writes a turn from a peer to gossip.db
func (s *DualStore) StoreRemoteTurn(ctx context.Context, turn *Turn, sourceNode string) error

// StoreRemoteMemory writes a memory from a peer to gossip.db
func (s *DualStore) StoreRemoteMemory(ctx context.Context, memory *Memory, sourceNode string) error
```

**Routing logic:**
```go
func (s *DualStore) StoreTurn(ctx context.Context, turn *Turn) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Determine source node from context or turn metadata
    sourceNode := getSourceNodeFromContext(ctx)
    if sourceNode == "" {
        sourceNode = s.localNodeID
    }

    if sourceNode == s.localNodeID {
        // Write to local.db
        _, err := s.localDB.ExecContext(ctx,
            `INSERT INTO turns (turn_id, session_id, role, content, timestamp) VALUES (?, ?, ?, ?, ?)`,
            turn.TurnID, turn.SessionID, turn.Role, turn.Content, turn.Timestamp)
        return err
    } else {
        // Write to gossip.db
        _, err := s.gossipDB.ExecContext(ctx,
            `INSERT OR IGNORE INTO turns (turn_id, session_id, role, content, timestamp, source_node) VALUES (?, ?, ?, ?, ?, ?)`,
            turn.TurnID, turn.SessionID, turn.Role, turn.Content, turn.Timestamp, sourceNode)
        return err
    }
}
```

**Tests:** `internal/memory/dual_store_routing_test.go`
- Test local write goes to local.db
- Test remote write goes to gossip.db
- Test concurrent writes don't deadlock

---

### Task 3.3: Merged Read Queries

**File:** `internal/memory/dual_store.go`

```go
// GetSession retrieves a session (searches local first, then gossip)
func (s *DualStore) GetSession(ctx context.Context, sessionID string) (*Session, error)

// GetSessions retrieves all sessions (merged from both DBs)
func (s *DualStore) GetSessions(ctx context.Context) ([]*Session, error)

// GetTurnsForSession retrieves all turns for a session (merged)
func (s *DualStore) GetTurnsForSession(ctx context.Context, sessionID string) ([]*Turn, error)

// GetMemories retrieves memories with optional filtering (merged)
func (s *DualStore) GetMemories(ctx context.Context, query *MemoryQuery) ([]*Memory, error)
```

**Merge pattern:**
```go
func (s *DualStore) GetSessions(ctx context.Context) ([]*Session, error) {
    var sessions []*Session

    // Query local.db first
    rows, err := s.localDB.QueryContext(ctx,
        `SELECT id, created_at, updated_at, metadata FROM sessions`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    for rows.Next() {
        sess := &Session{}
        // Scan row...
        sessions = append(sessions, sess)
    }

    // Query gossip.db
    rows2, err := s.gossipDB.QueryContext(ctx,
        `SELECT id, created_at, updated_at, metadata, source_node FROM sessions`)
    if err != nil {
        return nil, err
    }
    defer rows2.Close()

    for rows2.Next() {
        sess := &Session{}
        // Scan row...
        sessions = append(sessions, sess)
    }

    return sessions, nil
}
```

**Optimization:** Use UNION ALL for combined queries where appropriate

**Tests:** `internal/memory/dual_store_read_test.go`
- Test merged reads return data from both DBs
- Test ordering (local first, then gossip)
- Test filtering by source_node

---

### Task 3.4: Schema Definitions

**File:** `internal/memory/schema_local.sql`

```sql
-- local.db schema (data owned by this node)

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    metadata BLOB
);

CREATE TABLE IF NOT EXISTS turns (
    turn_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    category TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    agent_id TEXT,
    session_id TEXT
);

CREATE TABLE IF NOT EXISTS sync_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id);
CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
CREATE INDEX IF NOT EXISTS idx_memories_created ON memories(created_at);
```

**File:** `internal/memory/schema_gossip.sql`

```sql
-- sync-gossip.db schema (data replicated from peers)

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    metadata BLOB,
    source_node TEXT NOT NULL  -- Which node created this
);

CREATE TABLE IF NOT EXISTS turns (
    turn_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    source_node TEXT NOT NULL,  -- Which node created this
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    category TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    agent_id TEXT,
    session_id TEXT,
    source_node TEXT NOT NULL  -- Which node created this
);

-- Indexes for efficient merge queries
CREATE INDEX IF NOT EXISTS idx_turns_source ON turns(source_node);
CREATE INDEX IF NOT EXISTS idx_memories_source ON memories(source_node);
CREATE INDEX IF NOT EXISTS idx_sessions_source ON sessions(source_node);
```

---

### Task 3.5: Migration from Legacy Single-DB

**File:** `internal/memory/migrate.go`

```go
// MigrateToDualDB migrates from legacy single-DB to dual-DB structure
func MigrateToDualDB(dataDir string, nodeID string, logger *slog.Logger) error

// Steps:
// 1. Backup existing sessions.db/memory.db to migration-backup/
// 2. Rename sessions.db -> local.db
// 3. Merge memory.db into local.db.memories table
// 4. Create empty sync-gossip.db with schema
// 5. Update any references
```

**Implementation:**
```go
func MigrateToDualDB(dataDir string, nodeID string, logger *slog.Logger) error {
    sessionsPath := filepath.Join(dataDir, "sessions.db")
    memoryPath := filepath.Join(dataDir, "memory.db")
    localPath := filepath.Join(dataDir, "local.db")
    gossipPath := filepath.Join(dataDir, "sync-gossip.db")
    backupDir := filepath.Join(dataDir, "migration-backup")

    // Step 1: Create backup
    if err := os.MkdirAll(backupDir, 0700); err != nil {
        return err
    }
    if _, err := os.Stat(sessionsPath); err == nil {
        copyFile(sessionsPath, filepath.Join(backupDir, "sessions.db.pre-migration"))
    }

    // Step 2: Rename sessions.db -> local.db
    if _, err := os.Stat(sessionsPath); err == nil {
        if err := os.Rename(sessionsPath, localPath); err != nil {
            return fmt.Errorf("failed to rename sessions.db: %w", err)
        }
        logger.Info("migrated sessions.db to local.db")
    }

    // Step 3: Merge memory.db into local.db
    if _, err := os.Stat(memoryPath); err == nil {
        localDB, err := sql.Open("sqlite", localPath)
        if err != nil {
            return err
        }
        defer localDB.Close()

        _, err = localDB.Exec(`
            ATTACH ? AS src;
            INSERT OR IGNORE INTO memories SELECT * FROM src.memories;
            DETACH src;
        `, memoryPath)
        if err != nil {
            return fmt.Errorf("failed to merge memory.db: %w", err)
        }
        logger.Info("merged memory.db into local.db")
    }

    // Step 4: Create sync-gossip.db
    gossipDB, err := sql.Open("sqlite", gossipPath)
    if err != nil {
        return err
    }
    defer gossipDB.Close()

    // Run schema_gossip.sql
    schema, err := ioutil.ReadFile("internal/memory/schema_gossip.sql")
    if err != nil {
        return err
    }
    _, err = gossipDB.Exec(string(schema))
    if err != nil {
        return fmt.Errorf("failed to create gossip schema: %w", err)
    }

    logger.Info("dual-DB migration complete")
    return nil
}
```

**Tests:** `internal/memory/migrate_test.go`

---

### Task 3.6: Update Existing Memory Manager

**File:** `internal/memory/manager.go`

Update to use DualStore:
```go
type Manager struct {
    // ... existing fields ...

    // New: dual-store backend
    dualStore *DualStore
    useDual   bool  // true if using dual-store vs legacy single DB
}

// Update Store methods to use dual-store routing
func (m *Manager) Store(ctx context.Context, memory *Memory) error {
    if m.useDual && m.dualStore != nil {
        // Route based on ownership
        return m.dualStore.StoreMemory(ctx, memory)
    }
    // Fall back to legacy single-DB store
    return m.legacyStore(ctx, memory)
}
```

**Tests:** `internal/memory/manager_dual_test.go`

---

### Task 3.7: Session Store Updates

**File:** `internal/session/store_sqlite.go`

Update to write to dual-store:
```go
type SQLiteStore struct {
    // ... existing fields ...
    dualStore *memory.DualStore  // Optional: if using dual-store
}

// AppendTurn writes to appropriate DB based on ownership
func (s *SQLiteStore) AppendTurn(ctx context.Context, sessionID, role, content string) error {
    if s.dualStore != nil {
        // Use dual-store routing
        turn := &Turn{
            TurnID:    id.Generate("turn-"),
            SessionID: sessionID,
            Role:      role,
            Content:   content,
            Timestamp: time.Now().UnixNano(),
        }
        return s.dualStore.StoreTurn(ctx, turn)
    }
    // Fall back to legacy single-DB
    return s.legacyAppendTurn(ctx, sessionID, role, content)
}
```

---

### Task 3.8: CLI Migration Tool

**File:** `cmd/meept/migrate_cmd.go`

```go
type MigrateCommand struct {
    DryRun bool `help:"Show what would be migrated without making changes"`
}

func (c *MigrateCommand) Run(cfg *Config) error {
    if c.DryRun {
        // Show what files would be affected
        fmt.Println("Would migrate:")
        fmt.Println("  sessions.db -> local.db")
        fmt.Println("  memory.db -> merged into local.db")
        fmt.Println("  Creates: sync-gossip.db (empty)")
        return nil
    }

    err := memory.MigrateToDualDB(cfg.DataDir, cfg.NodeID, logger)
    if err != nil {
        return fmt.Errorf("migration failed: %w", err)
    }
    fmt.Println("Migration complete!")
    return nil
}
```

**Usage:**
```bash
# Dry run - see what would happen
meept migrate --dry-run

# Run migration
meept migrate
```

---

### Task 3.9: Unit Tests

**Files:**
- `internal/memory/dual_store_test.go`
- `internal/memory/dual_store_routing_test.go`
- `internal/memory/dual_store_read_test.go`
- `internal/memory/migrate_test.go`

**Coverage targets:**
- Write routing: 95%+ (critical for data ownership)
- Merged reads: 90%+
- Migration: 95%+ (data safety critical)

---

## Acceptance Criteria

- [ ] DualStore created with local.db + sync-gossip.db
- [ ] Writes routed to correct DB based on ownership
- [ ] Reads merged from both DBs (local first)
- [ ] Migration tool works without data loss
- [ ] Existing code updated to use dual-store
- [ ] All unit tests pass with >90% coverage
- [ ] Documentation updated with dual-DB architecture

---

## Configuration Example

```json5
// ~/.meept/meept.json5
{
  // No special config needed for dual-DB
  // Enabled automatically when backup or sync is enabled
}
```

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `modernc.org/sqlite` | SQLite driver |
| `internal/memory` | Existing memory types |

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Migration corrupts data | Full backup before migration, reversible until first sync |
| Write goes to wrong DB | Test routing logic thoroughly, add ownership validation |
| Merge duplicates data | INSERT OR IGNORE with proper unique constraints |
| Performance regression | Benchmark merged reads vs single-DB |

---

## Estimated Effort

**Total tasks:** 9
**Estimated time:** 12-16 hours
**Complexity:** High (data routing critical, migration safety)

---

*This plan implements Phase 3 of 7 from the backup/sync design spec.*
