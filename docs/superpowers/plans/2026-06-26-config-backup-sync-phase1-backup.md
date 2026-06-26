# Phase 1: Git Backup Scheduler - Implementation Plan

**Spec:** `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`
**Date:** 2026-06-26
**Status:** Ready for implementation

---

## Overview

This plan implements the Git Backup Scheduler - the foundation of the backup/sync system. This phase enables daily git-backed backups of local SQLite data with compression and retention management.

### Scope

| In Scope | Out of Scope |
|----------|--------------|
| Git backup push scheduler | Peer sync/pull (Phase 2) |
| zstd compression | Dual-DB routing (Phase 3) |
| Manifest generation | Gossip event schema (Phase 4) |
| CLI: `backup list`, `backup push` | Config sync (Phase 5) |
| Retention pruning | Migration from legacy DB names |

---

## Phase 1 Checklist

### Task 1.1: Core Backup Scheduler

**File:** `internal/backup/git_backup.go`

```go
package backup

type GitBackupScheduler struct {
    dbPaths       []string
    backupRepo    string
    checkoutDir   string
    nodeID        string
    retentionDays int
    schedule      time.Duration
    logger        *slog.Logger
}

func NewGitBackupScheduler(cfg GitBackupConfig) (*GitBackupScheduler, error)
func (s *GitBackupScheduler) Start(ctx context.Context)
func (s *GitBackupScheduler) runBackup(ctx context.Context) error
func (s *GitBackupScheduler) pruneOldBackups(ctx context.Context) error
```

**Implementation steps:**
1. Create `internal/backup/` package directory
2. Implement `NewGitBackupScheduler` with config validation
3. Implement `Start()` - ticker loop for scheduled backups
4. Implement `runBackup()`:
   - Checkpoint SQLite WAL (`PRAGMA wal_checkpoint(TRUNCATE)`)
   - Compress with zstd (use `github.com/klauspost/compress/zstd`)
   - Generate manifest with SHA256, sizes, timestamps
   - Write to `backups/YYYY-MM-DD/<node_id>/local.db.zst`
   - Git add, commit, push (with rebase on conflict)
5. Implement `pruneOldBackups()` - remove directories older than retention

**Dependencies:** `internal/config`, `github.com/klauspost/compress/zstd`, `gopkg.in/src-d/go-git.v4`

**Tests:** `internal/backup/git_backup_test.go`
- Test checkpoint invocation
- Test compression roundtrip
- Test manifest JSON format
- Test prune logic (mock git repo)

---

### Task 1.2: Backup Config Schema

**File:** `internal/config/backup_config.go` (new)

```go
type BackupConfig struct {
    Enabled       bool          `json:"enabled"`
    RepoURL       string        `json:"repo_url"`
    Schedule      time.Duration `json:"schedule"`  // "24h"
    RetentionDays int           `json:"retention_days"`
}

func DefaultBackupConfig() BackupConfig
func LoadBackupConfig(data []byte) (*BackupConfig, error)
```

**Update:** `internal/config/schema.go` - add `Backup` field to main config

**Tests:** `internal/config/backup_config_test.go`
- Test JSON5 parsing
- Test default values (24h schedule, 12 days retention)
- Test validation (repo URL required when enabled)

---

### Task 1.3: Manifest Schema

**File:** `internal/backup/manifest.go`

```go
type BackupManifest struct {
    NodeID        string         `json:"node_id"`
    Timestamp     time.Time      `json:"timestamp"`
    Databases     []DatabaseInfo `json:"databases"`
    SyncMetadata  SyncMetadata   `json:"sync_metadata"`
}

type DatabaseInfo struct {
    Name             string `json:"name"`
    CompressedSize   int64  `json:"compressed_size"`
    UncompressedSize int64  `json:"uncompressed_size"`
    SHA256           string `json:"sha256"`
}

type SyncMetadata struct {
    LastPeerPull        string   `json:"last_peer_pull"`
    PeersSynced         []string `json:"peers_synced"`
    GossipEventsSent24h int      `json:"gossip_events_sent_24h"`
    GossipEventsRecv24h int      `json:"gossip_events_recv_24h"`
}

func GenerateManifest(nodeID string, dbPaths []string) (*BackupManifest, error)
func (m *BackupManifest) Save(path string) error
func LoadManifest(path string) (*BackupManifest, error)
```

**Tests:** `internal/backup/manifest_test.go`

---

### Task 1.4: CLI Commands

**File:** `cmd/meept/backup_cmd.go` (new)

```go
// cmd/meept/backup_cmd.go
type BackupCommand struct {
    // Subcommands
    List    *BackupListCmd    `cmd:"" help:"List available backups"`
    Push    *BackupPushCmd    `cmd:"" help:"Trigger immediate backup"`
    Restore *BackupRestoreCmd `cmd:"" help:"Restore from backup (Phase 2)"`
}

type BackupListCmd struct {
    Peer string `help:"Show peer backups"`
}

type BackupPushCmd struct {
    Force bool `help:"Force push even if no changes"`
}

func (c *BackupListCmd) Run(cfg *Config) error
func (c *BackupPushCmd) Run(cfg *Config) error
```

**Output format for `backup list`:**
```
Available backups for this node:
  2026-06-25  local.db.zst (15MB, SHA: abc123...)
  2026-06-24  local.db.zst (14MB, SHA: def456...)
  2026-06-23  local.db.zst (16MB, SHA: 789xyz...)

Peer backups available:
  machine-b: 2026-06-25 (12MB)
  machine-c: 2026-06-25 (18MB)
```

**Tests:** `cmd/meept/backup_cmd_test.go` (integration with mock git repo)

---

### Task 1.5: Daemon Wiring

**File:** `internal/daemon/daemon.go`

Add to daemon startup:
```go
// In Components.Start() or wireBackup()
if cfg.Backup.Enabled {
    backupScheduler, err := backup.NewGitBackupScheduler(cfg.Backup)
    if err != nil {
        return fmt.Errorf("failed to create backup scheduler: %w", err)
    }
    d.backupScheduler = backupScheduler
    go backupScheduler.Start(d.ctx)
}
```

**Update:** `internal/daemon/components.go` - add backup scheduler component registration

**Tests:** `internal/daemon/backup_wiring_test.go`

---

### Task 1.6: Zstd Compression Wrapper

**File:** `internal/backup/compress.go`

```go
// CompressFile compresses src to dst.zst and returns compressed size
func CompressFile(src, dst string) (compressedSize int64, err error)

// DecompressFile decompresses src.zst to dst
func DecompressFile(src, dst string) (err error)

// ComputeSHA256 returns hex-encoded SHA256 hash of file
func ComputeSHA256(path string) (string, error)
```

**Tests:** `internal/backup/compress_test.go`
- Test compression of typical SQLite DB
- Test decompression roundtrip
- Test SHA256 consistency

---

### Task 1.7: Git Operations Wrapper

**File:** `internal/backup/git_ops.go`

```go
// GitInit initializes or opens a git repo
func GitInit(path string) (*git.Repository, error)

// GitAddCommitPush adds files, commits with message, pushes with rebase
func GitAddCommitPush(repo *git.Repository, files []string, message string) error

// GitPullRebase pulls remote with rebase strategy
func GitPullRebase(repo *git.Repository) error

// GitListBackups returns sorted list of backup directories
func GitListBackups(repo *git.Repository, nodeID string) ([]string, error)
```

**Dependencies:** `gopkg.in/src-d/go-git.v4`

**Tests:** `internal/backup/git_ops_test.go` (integration with test git repo)

---

### Task 1.8: Error Types and Logging

**File:** `internal/backup/errors.go`

```go
type BackupError struct {
    Op       string  // "compress", "git_push", etc.
    Err      error
    Retryable bool
}

func (e *BackupError) Error() string

var (
    ErrGitConflict   = errors.New("backup: git conflict (remote has newer backup)")
    ErrDiskFull      = errors.New("backup: disk full")
    ErrCompression   = errors.New("backup: compression failed")
)
```

**Logging integration:**
```go
logger.Info("backup: push completed",
    "node_id", nodeID,
    "backup_path", backupPath,
    "compressed_size", size,
    "duration_ms", duration.Milliseconds())

logger.Error("backup: push failed",
    "error", err,
    "retryable", isRetryable(err))
```

---

### Task 1.9: SQLite Path Resolution

**File:** `internal/backup/db_paths.go`

```go
// GetLocalDBPaths returns paths to SQLite DBs that should be backed up
func GetLocalDBPaths(dataDir string) ([]string, error)

// Returns:
// - ~/.meept/local.db (or sessions.db for migration)
// - ~/.meept/memory.db (if exists separately)
```

**Migration handling:**
```go
// If local.db doesn't exist but sessions.db does, use sessions.db
// Log warning about migration
```

---

### Task 1.10: Unit Tests

**Files:**
- `internal/backup/git_backup_test.go`
- `internal/backup/manifest_test.go`
- `internal/backup/compress_test.go`
- `internal/backup/git_ops_test.go`
- `internal/config/backup_config_test.go`

**Coverage targets:**
- Backup scheduler: 90%+
- Manifest generation: 95%+
- Compression: 100% (critical path)

---

## Acceptance Criteria

- [ ] `meept backup list` shows available backups with size/SHA
- [ ] `meept backup push --force` triggers immediate backup
- [ ] Daily scheduled backups run automatically when enabled
- [ ] Backups are zstd compressed
- [ ] Manifest contains accurate metadata (sizes, hashes, timestamps)
- [ ] Retention pruning removes backups older than configured days
- [ ] Git conflicts handled with automatic rebase + retry (3x max)
- [ ] Daemon starts backup scheduler on startup when enabled
- [ ] All unit tests pass with >90% coverage
- [ ] Documentation updated in `docs/configuration/backup.md`

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
  // sync, config_sync not yet implemented (Phase 2+)
}
```

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/klauspost/compress/zstd` | Zstandard compression |
| `gopkg.in/src-d/go-git.v4` | Git operations |
| `modernc.org/sqlite` | SQLite (existing dep) |

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Git push fails repeatedly | Retry with exponential backoff, alert user after 3 failures |
| SQLite DB locked during backup | WAL checkpoint before compress, retry if still locked |
| Compression uses too much memory | Use streaming compression with limited buffer |
| Disk fills from backups | Prune runs before new backup, fail gracefully if still full |

---

## Estimated Effort

**Total tasks:** 10
**Estimated time:** 8-12 hours
**Complexity:** Medium (git integration + compression + scheduler)

---

*This plan implements Phase 1 of 7 from the backup/sync design spec.*
