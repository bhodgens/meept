# Phase 2: Async Pull/Sync - Implementation Plan

**Spec:** `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`
**Date:** 2026-06-26
**Status:** Ready for implementation
**Prerequisites:** Phase 1 (Git Backup Scheduler) complete

---

## Overview

This plan implements the Async Pull/Sync system for multi-machine synchronization via git. Enables bidirectional sync of local data across personal devices using git as the async transport layer.

### Scope

| In Scope | Out of Scope |
|----------|--------------|
| Git pull from backup repo | Real-time gossip (Phase 4) |
| Peer merge logic (INSERT OR IGNORE) | Config sync (Phase 5) |
| Sync metadata tracking | Dual-DB routing (Phase 3) |
| CLI: `sync pull`, `sync status` | Migration from legacy DB names |

---

## Phase 2 Checklist

### Task 2.1: Sync Puller Core

**File:** `internal/backup/sync_puller.go`

```go
package backup

type SyncPuller struct {
    backupRepo      string
    checkoutDir     string
    localNodeID     string
    peers           []string
    pullSchedule    time.Duration
    localDB         *sql.DB
    gossipDB        *sql.DB
    logger          *slog.Logger
    lastSyncTime    map[string]time.Time  // peer -> last successful sync
}

func NewSyncPuller(cfg SyncConfig, localDB, gossipDB *sql.DB) (*SyncPuller, error)
func (p *SyncPuller) Start(ctx context.Context)
func (p *SyncPuller) pullOnce(ctx context.Context) error
func (p *SyncPuller) mergePeerBackup(ctx context.Context, peerID, peerDBPath string) (MergeStats, error)
```

**Implementation steps:**
1. Create sync puller with config validation
2. Implement `Start()` - ticker loop for scheduled pulls
3. Implement `pullOnce()`:
   - Git pull from backup repo
   - Find each peer's latest backup
   - Call `mergePeerBackup()` for each
4. Implement `mergePeerBackup()`:
   - Decompress peer's `local.db.zst` to temp
   - Open as attached SQLite database
   - Run INSERT OR IGNORE for sessions, turns, memories
   - Return merge statistics

**Dependencies:** Phase 1 types, `modernc.org/sqlite`

**Tests:** `internal/backup/sync_puller_test.go`

---

### Task 2.2: Sync Config Schema

**File:** `internal/config/sync_config.go` (new)

```go
type SyncConfig struct {
    Enabled      bool          `json:"enabled"`
    Peers        []string      `json:"peers"`
    PullSchedule time.Duration `json:"pull_schedule"`  // "1h"
}

func DefaultSyncConfig() SyncConfig
func LoadSyncConfig(data []byte) (*SyncConfig, error)
func (c *SyncConfig) Validate() error  // peers required when enabled
```

**Update:** `internal/config/schema.go` - add `Sync` field

**Tests:** `internal/config/sync_config_test.go`

---

### Task 2.3: Merge Logic

**File:** `internal/backup/merge.go`

```go
type MergeStats struct {
    SessionsMerged int
    TurnsMerged    int
    MemoriesMerged int
    Skipped        int  // Duplicate IDs skipped
    Errors         int
}

// MergePeerDB merges data from peer's backup into local gossip DB
func MergePeerDB(ctx context.Context, gossipDB *sql.DB, peerDBPath, peerID string) (*MergeStats, error)

func mergeSessions(ctx context.Context, tx *sql.Tx, peerDB *sql.DB, peerID string) (int, error)
func mergeTurns(ctx context.Context, tx *sql.Tx, peerDB *sql.DB, peerID string) (int, error)
func mergeMemories(ctx context.Context, tx *sql.Tx, peerDB *sql.DB, peerID string) (int, error)
```

**SQL pattern:**
```sql
-- Attach peer DB
ATTACH ? AS peer;

-- Merge sessions (INSERT OR IGNORE)
INSERT OR IGNORE INTO sessions (id, created_at, updated_at, metadata, source_node)
SELECT id, created_at, updated_at, metadata, ? FROM peer.sessions;

-- Merge turns
INSERT OR IGNORE INTO turns (turn_id, session_id, role, content, timestamp, source_node)
SELECT turn_id, session_id, role, content, timestamp, ? FROM peer.turns;

-- Merge memories
INSERT OR IGNORE INTO memories (id, type, category, content, created_at, agent_id, session_id, source_node)
SELECT id, type, category, content, created_at, agent_id, session_id, ? FROM peer.memories;

DETACH peer;
```

**Tests:** `internal/backup/merge_test.go`
- Test with overlapping data (duplicates should be skipped)
- Test with entirely new data (all merged)
- Test with schema mismatch (should error gracefully)

---

### Task 2.4: Sync Metadata Tracking

**File:** `internal/backup/sync_metadata.go`

```go
type SyncMetadataStore struct {
    db *sql.DB
}

func NewSyncMetadataStore(db *sql.DB) *SyncMetadataStore
func (s *SyncMetadataStore) GetLastSync(peerID string) (time.Time, error)
func (s *SyncMetadataStore) SetLastSync(peerID string, t time.Time) error
func (s *SyncMetadataStore) GetAllSyncStatus() (map[string]SyncStatus, error)

type SyncStatus struct {
    PeerID         string
    LastSync       time.Time
    LastMergeStats *MergeStats
    Error          string
}
```

**Schema:**
```sql
CREATE TABLE IF NOT EXISTS sync_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- Keys: "last_sync:peer_a", "last_sync:peer_b", etc.
```

**Tests:** `internal/backup/sync_metadata_test.go`

---

### Task 2.5: CLI Commands

**File:** `cmd/meept/sync_cmd.go` (new)

```go
type SyncCommand struct {
    Pull   *SyncPullCmd   `cmd:"" help:"Trigger immediate peer sync"`
    Status *SyncStatusCmd `cmd:"" help:"Show sync status"`
}

type SyncPullCmd struct{}
func (c *SyncPullCmd) Run(cfg *Config) error

type SyncStatusCmd struct{}
func (c *SyncStatusCmd) Run(cfg *Config) error
```

**Output format for `sync status`:**
```
Sync Status
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
This node: machine-a (laptop)
Last pull: 5 minutes ago
Last push: 18 hours ago

Peer synchronization:
  machine-b (desktop):
    Last sync: 5m ago
    Rows received: 152 sessions, 1,234 turns, 89 memories
    Errors: none

  machine-c (server):
    Last sync: 1h ago
    Rows received: 45 sessions, 423 turns, 12 memories
    Errors: none

Pending local changes: 3 sessions, 28 turns (will push in 6h)
```

**Tests:** `cmd/meept/sync_cmd_test.go`

---

### Task 2.6: Temp File Management

**File:** `internal/backup/temp.go`

```go
// TempManager manages temporary files during sync operations
type TempManager struct {
    tempDir string
    mu      sync.Mutex
}

func NewTempManager() *TempManager

// ReservePeerDB decompresses peer backup to temp and returns path
func (m *TempManager) ReservePeerDB(peerBackupPath string) (string, error)

// Cleanup removes all temp files
func (m *TempManager) Cleanup() error

// Register temp file for cleanup on process exit
func (m *TempManager) Register(path string)
```

**Cleanup strategy:**
- Create temp dir: `~/.meept/sync-temp/`
- Clean on startup (remove stale temps)
- Remove each peer DB after merge completes

**Tests:** `internal/backup/temp_test.go`

---

### Task 2.7: Error Handling

**File:** `internal/backup/sync_errors.go`

```go
type SyncError struct {
    PeerID string
    Op     string  // "pull", "decompress", "merge"
    Err    error
}

func (e *SyncError) Error() string

var (
    ErrPeerNotFound      = errors.New("sync: peer backup not found")
    ErrPeerDBCorrupt     = errors.New("sync: peer database corrupt")
    ErrSchemaMismatch    = errors.New("sync: schema mismatch (peer version incompatible)")
    ErrMergeTimeout      = errors.New("sync: merge operation timed out")
)

// IsRetryable checks if error can be retried
func IsRetryable(err error) bool
```

**Error recovery:**
- `ErrPeerNotFound`: Skip peer, retry next pull cycle
- `ErrPeerDBCorrupt`: Log, skip peer, alert user
- `ErrSchemaMismatch`: Log, skip peer, require manual intervention

---

### Task 2.8: Peer Discovery (Optional)

**File:** `internal/backup/peer_discovery.go`

```go
// ListAvailablePeers scans backup repo for peer directories
func ListAvailablePeers(repo *git.Repository) ([]string, error)

// GetLatestBackupForPeer returns path to peer's most recent backup
func GetLatestBackupForPeer(repo *git.Repository, peerID string) (string, error)
```

**Integration:**
- Call during `sync status` to show available peers
- Auto-add discovered peers to config (optional, user-confirm)

---

### Task 2.9: Daemon Wiring

**File:** `internal/daemon/daemon.go`

Add to daemon startup:
```go
// In wireBackup() or wireSync()
if cfg.Sync.Enabled {
    // gossipDB created in Phase 3, use stub for now
    gossipDB := openOrCreateGossipDB()

    syncPuller, err := backup.NewSyncPuller(cfg.Sync, localDB, gossipDB)
    if err != nil {
        return fmt.Errorf("failed to create sync puller: %w", err)
    }
    d.syncPuller = syncPuller
    go syncPuller.Start(d.ctx)
}
```

**Tests:** `internal/daemon/sync_wiring_test.go`

---

### Task 2.10: Unit Tests

**Files:**
- `internal/backup/sync_puller_test.go`
- `internal/backup/merge_test.go`
- `internal/backup/sync_metadata_test.go`
- `internal/backup/sync_errors_test.go`

**Coverage targets:**
- Merge logic: 95%+ (critical for data integrity)
- Sync puller: 90%+
- Sync metadata: 90%+

---

## Acceptance Criteria

- [ ] `meept sync pull` triggers immediate peer sync
- [ ] `meept sync status` shows peer sync status with stats
- [ ] Hourly scheduled pulls run automatically when enabled
- [ ] Peer data merged with INSERT OR IGNORE (duplicates skipped)
- [ ] Sync metadata persisted in local DB
- [ ] Temp files cleaned up after merge
- [ ] Errors logged with appropriate severity
- [ ] Corrupt peer backups skipped gracefully
- [ ] All unit tests pass with >90% coverage
- [ ] Documentation updated in `docs/configuration/sync.md`

---

## Configuration Example

```json5
// ~/.meept/meept.json5
{
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    schedule: "24h",
    retention_days: 12,
  },
  sync: {
    enabled: true,
    peers: ["laptop", "desktop"],
    pull_schedule: "1h",
  },
}
```

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/klauspost/compress/zstd` | Decompression (Phase 1) |
| `gopkg.in/src-d/go-git.v4` | Git pull (Phase 1) |
| `modernc.org/sqlite` | SQLite merge operations |

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Merge corrupts local DB | Run in transaction, rollback on error |
| Peer backup very large | Stream decompression, don't load entirely in memory |
| Sync loop hangs | Timeout on merge operation (max 10 minutes) |
| Disk fills from temp files | Aggressive cleanup, fail if temp dir > 1GB |

---

## Estimated Effort

**Total tasks:** 10
**Estimated time:** 10-14 hours
**Complexity:** Medium-High (SQLite merge logic critical)

---

*This plan implements Phase 2 of 7 from the backup/sync design spec.*
