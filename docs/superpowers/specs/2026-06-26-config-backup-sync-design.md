# Configuration Backup and Sync Design Specification

**Date:** 2026-06-26
**Status:** Approved
**Author:** Meept Team

---

## 1. Overview

This spec defines a unified backup and synchronization system for Meept configuration and state data, supporting:

1. **Single-node deployments** - Daily git backups of local state
2. **Multi-machine personal sync** - Bidirectional sync across personal devices via git
3. **Cluster deployments** - Real-time bilateral gossip + daily git backups

### 1.1 Design Goals

| Goal | Description |
|------|-------------|
| **Unified architecture** | Same system handles single-node, multi-machine, and cluster |
| **Dual-channel sync** | Git for async/config, gossip for real-time state |
| **Storage efficiency** | Split local vs. gossip data to minimize backup size |
| **Recoverability** | Clear restoration semantics for all deployment modes |
| **Config distribution** | Shared + per-node configuration via git |

### 1.2 Non-Goals (Explicitly Out of Scope)

| Non-Goal | Rationale |
|----------|-----------|
| Real-time sync without cluster | Git polling latency acceptable for personal sync |
| Cross-platform conflict UI | Manual resolution via CLI initially |
| Cloud storage backends | Git-only for MVP; S3/restic can be added later |
| Encrypted backups | Git repo access control sufficient for MVP |

---

## 2. Architecture

### 2.1 High-Level Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         PER-MACHINE DAEMON                               │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐ │
│  │                      SQLite Databases                               │ │
│  │  ┌──────────────────┐              ┌─────────────────────────────┐ │ │
│  │  │  local.db        │              │  sync-gossip.db             │ │ │
│  │  │  (unique data)   │              │  (from peers)               │ │ │
│  │  │                  │              │                             │ │ │
│  │  │  BACKED UP       │              │  NOT BACKED UP              │ │ │
│  │  │  - Daily git     │              │  - Recover via pull/gossip  │ │ │
│  │  └────────┬─────────┘              └──────────────┬──────────────┘ │ │
│  │           │                                        │                │ │
│  │           │        ┌────────────────────────────┐  │                │ │
│  │           │        │  Backup/Sync Scheduler     │  │                │ │
│  │           │        │  - Daily push to git       │  │                │ │
│  │           │        │  - Periodic peer pull      │  │                │ │
│  │           │        └──────────────┬─────────────┘  │                │ │
│  │           │                       │                │                │ │
│  └───────────┼───────────────────────┼────────────────┘                │ │
│              │                       │                                  │ │
│              ▼                       ▼                                  │ │
│     ┌─────────────────┐   ┌─────────────────┐                          │ │
│     │  Git Remote     │   │  WireGuard Mesh │                          │ │
│     │  (async sync)   │   │  (real-time)    │                          │ │
│     └─────────────────┘   └─────────────────┘                          │ │
│              │                       │                                  │ │
│              │                       └────────────────┐                 │ │
│              ▼                                        ▼                 │ │
│     ┌─────────────────┐                     ┌─────────────────┐        │ │
│     │  Machine B      │                     │  Cluster Peer   │        │ │
│     │  (pull + merge) │                     │  (gossip)       │        │ │
│     └─────────────────┘                     └─────────────────┘        │ │
└─────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Component Summary

| Component | Package | Purpose |
|-----------|---------|---------|
| **Backup Scheduler** | `internal/backup/git_backup.go` | Daily git push of local.db |
| **Sync Puller** | `internal/backup/sync_puller.go` | Periodic pull + merge from peers |
| **Gossip Engine** | `internal/cluster/gossip.go` | Real-time bilateral sync (cluster mode) |
| **Dual-DB Router** | `internal/memory/manager.go` | Route reads/writes to local vs. gossip DB |
| **Config Sync** | `internal/config/sync.go` | Pull shared + per-node config from git |

---

## 3. Data Structures

### 3.1 Database Schema

**local.db** - Unique data owned by this machine:

```sql
-- Sessions started on this machine
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    metadata BLOB
);

-- Conversation turns (append-only)
CREATE TABLE turns (
    turn_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

-- Memories created on this machine
CREATE TABLE memories (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    category TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    agent_id TEXT,
    session_id TEXT
);

-- Sync tracking: last pull timestamp per peer
CREATE TABLE sync_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

**sync-gossip.db** - Replicated data from peers:

```sql
-- Same schema as local.db, PLUS:
ALTER TABLE sessions ADD COLUMN source_node TEXT NOT NULL;
ALTER TABLE turns ADD COLUMN source_node TEXT NOT NULL;
ALTER TABLE memories ADD COLUMN source_node TEXT NOT NULL;

-- Index for efficient merge queries
CREATE INDEX idx_turns_source ON turns(source_node);
CREATE INDEX idx_memories_source ON memories(source_node);
```

### 3.2 Backup Repository Structure

```
backups-repo/
├── backups/
│   ├── 2026-06-25/
│   │   ├── machine-a/
│   │   │   ├── local.db.zst
│   │   │   └── manifest.json
│   │   ├── machine-b/
│   │   │   ├── local.db.zst
│   │   │   └── manifest.json
│   │   └── cluster-archive/        # Optional: full cluster backup
│   │       ├── full-cluster.db.zst
│   │       └── manifest.json
│   ├── 2026-06-26/
│   │   └── ...
│   └── config/                      # Shared configuration
│       ├── shared/
│       │   ├── meept.json5
│       │   ├── models.json5
│       │   └── mcp_servers.json5
│       └── nodes/
│           ├── machine-a/
│           └── machine-b/
└── README.md                        # Restore instructions
```

### 3.3 Manifest Schema

```json5
{
  "node_id": "machine-a",
  "timestamp": "2026-06-25T00:00:00Z",
  "databases": [
    {
      "name": "local.db",
      "compressed_size": 15234567,
      "uncompressed_size": 52428800,
      "sha256": "abc123..."
    }
  ],
  "sync_metadata": {
    "last_peer_pull": "2026-06-25T23:00:00Z",
    "peers_synced": ["machine-b", "machine-c"],
    "gossip_events_sent_24h": 1523,
    "gossip_events_recv_24h": 2891
  }
}
```

### 3.4 Configuration Schema

```json5
// ~/.meept/meept.json5
{
  // Cluster configuration (real-time gossip via WireGuard)
  cluster: {
    enabled: false,
    cluster_id: "",
    cluster_name: "",
    node_id: "machine-a",
    // ... cluster settings from existing schema
  },

  // Backup configuration (git-based)
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    schedule: "24h",           // How often to push backups
    retention_days: 12,        // Days of backups to retain
  },

  // Sync configuration (async multi-machine via git)
  sync: {
    enabled: true,
    peers: ["machine-a", "machine-b"],  // Known peer IDs
    pull_schedule: "1h",                // How often to pull peers' data
  },

  // Config sync configuration
  config_sync: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-config.git",  // Can same as backup
    pull_schedule: "5m",       // How often to check for config changes
  }
}
```

---

## 4. Protocol Specifications

### 4.1 Backup Push Protocol

**Schedule:** Daily (configurable via `backup.schedule`)

```
1. Checkpoint all open SQLite connections
   PRAGMA wal_checkpoint(TRUNCATE)

2. Compress local.db with zstd
   - Output: local.db.zst
   - Store SHA256 hash for integrity verification

3. Create manifest.json with metadata
   - Timestamp, sizes, peer sync stats

4. Write to backup repo working tree
   Path: backups/YYYY-MM-DD/<node_id>/local.db.zst

5. Git add, commit, push
   - Commit message: "Backup: <node_id> at <timestamp>"
   - Rebase on conflict, retry up to 3x

6. Prune old backups beyond retention_days
```

### 4.2 Async Pull Protocol (Multi-Machine Sync)

**Schedule:** Hourly (configurable via `sync.pull_schedule`)

```
1. Pull latest from backup/sync repo

2. For each peer's backup in backups/YYYY-MM-DD/<peer_id>/:
   a. Find most recent backup newer than last_sync timestamp
   b. Decompress peer's local.db.zst to temp file
   c. Open peer DB as attached database
   d. Merge rows with INSERT OR IGNORE:

      INSERT OR IGNORE INTO sessions (id, created_at, updated_at, metadata, source_node)
      SELECT id, created_at, updated_at, metadata, ? FROM attached.sessions
      WHERE source_node NOT IN (SELECT source_node FROM sync_gossip.sessions);

   e. Repeat for turns, memories tables
   f. Update sync_metadata with new last_sync timestamp

3. Clean up temp files
```

### 4.3 Real-Time Gossip Protocol (Cluster Mode)

**Transport:** WireGuard mesh (TCP)

```
1. On local write to local.db:
   a. Commit to local SQLite
   b. Create ClusterEvent with payload
   c. Flood to all peers via WireGuard
   d. Peer sends ACK

2. On gossip event receive:
   a. Verify ed25519 signature
   b. Check for duplicate (event_id exists)
   c. Insert into sync-gossip.db (INSERT OR IGNORE)
   d. Forward to all other peers
   e. Send ACK

3. On peer timeout (no heartbeat > peer_timeout):
   a. Mark peer as unreachable
   b. Queue events for retry on reconnect
```

### 4.4 Config Sync Protocol

**Schedule:** Every 5 minutes (configurable)

```
1. Pull config repo (shallow clone for efficiency)

2. Check for changes since last pull:
   - Compare git commit hash
   - If no change, skip

3. Merge shared config:
   - Copy config/shared/*.json5 to ~/.meept/
   - Per-node overrides: config/nodes/<node_id>/*.json5

4. Apply config changes:
   - Hot-reload where supported
   - Queue restart for changes requiring daemon reload

5. Push local config changes (if any):
   - User-initiated config edits committed to repo
   - Rebase on conflict
```

---

## 5. Deployment Modes

### 5.1 Single-Node Deployment

```json5
{
  cluster: { enabled: false },
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    schedule: "24h",
    retention_days: 12,
  },
  sync: { enabled: false },
  config_sync: { enabled: false }
}
```

**Behavior:**
- Only local.db exists (no sync-gossip.db)
- Daily git backups
- No peer sync, no gossip

### 5.2 Multi-Machine Personal Sync

```json5
{
  cluster: { enabled: false },
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
  config_sync: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-config.git",
    pull_schedule: "5m",
  }
}
```

**Behavior:**
- local.db + sync-gossip.db
- Daily push of local.db
- Hourly pull + merge from peers
- Config synced from shared repo

### 5.3 Cluster Deployment

```json5
{
  cluster: {
    enabled: true,
    cluster_id: "prod-cluster-01",
    cluster_name: "Production Cluster",
    node_id: "node-a",
    // ... existing cluster config
  },
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-cluster-backups.git",
    schedule: "24h",
    retention_days: 12,
  },
  sync: { enabled: false },  // Not needed with real-time gossip
  config_sync: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-cluster-config.git",
    pull_schedule: "5m",
  }
}
```

**Behavior:**
- local.db + sync-gossip.db (or cluster-gossip.db)
- Real-time gossip via WireGuard
- Daily git backups (local.db only)
- Shared config via git

---

## 6. CLI Commands

### 6.1 `meept backup list`

```bash
$ meept backup list

Available backups for this node:
  2026-06-25  local.db.zst (15MB, SHA: abc123...)
  2026-06-24  local.db.zst (14MB, SHA: def456...)
  2026-06-23  local.db.zst (16MB, SHA: 789xyz...)

Peer backups available:
  machine-b: 2026-06-25 (12MB)
  machine-c: 2026-06-25 (18MB)
```

### 6.2 `meept backup restore`

```bash
# Restore from specific backup
$ meept backup restore 2026-06-25 --target=local.db

# Restore all databases
$ meept backup restore 2026-06-25 --all

# Restore from peer backup (multi-machine mode)
$ meept backup restore 2026-06-25 --peer=machine-b --target=local.db
```

### 6.3 `meept backup push`

```bash
# Trigger immediate backup push
$ meept backup push

# Force push even if no changes
$ meept backup push --force
```

### 6.4 `meept sync pull`

```bash
# Trigger immediate peer sync
$ meept sync pull

# Show sync status
$ meept sync status

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

### 6.5 `meept config sync`

```bash
# Show config sync status
$ meept config sync status

# Force config refresh
$ meept config sync pull
```

---

## 7. Security Considerations

### 7.1 Authentication

| Layer | Mechanism | Purpose |
|-------|-----------|---------|
| **Git Access** | SSH keys | Authenticate to backup/config repos |
| **Backup Integrity** | SHA256 in manifest | Detect corruption/tampering |
| **Gossip (cluster)** | ed25519 signatures | Verify event authenticity |
| **Network (cluster)** | WireGuard Curve25519 | Encrypted peer-to-peer tunnel |

### 7.2 Key Management

- SSH keys for git: `~/.ssh/id_ed25519` (standard location)
- WireGuard keys: `~/.meept/cluster/keys/` (existing)
- Gossip ed25519: `~/.meept/cluster/keys/` (existing)

### 7.3 Access Control

| Deployment | Who Can Read Backups | Who Can Write |
|------------|---------------------|---------------|
| Single-node | Repo owner | Single machine |
| Multi-machine | All synced machines | All synced machines |
| Cluster | Cluster members | Cluster members only |

---

## 8. Error Handling

### 8.1 Backup Push Failures

| Error | Recovery |
|-------|----------|
| Git push rejected (conflict) | Automatic rebase + retry (up to 3x) |
| Git auth failure | Log error, retry next scheduled run |
| Disk full (local) | Abort backup, alert user |
| Compression failure | Retry with lower compression level |

### 8.2 Sync Pull Failures

| Error | Recovery |
|-------|----------|
| Git pull fails | Retry on next schedule, log warning |
| Peer DB corrupt | Skip peer, log error, continue with other peers |
| Merge conflict (duplicate ID) | INSERT OR IGNORE - skip duplicate |
| Schema mismatch | Log error, require manual intervention |

### 8.3 Gossip Failures (Cluster)

| Error | Recovery |
|-------|----------|
| Peer unreachable | Queue events, retry on reconnect |
| Signature verification fails | Drop event, log security warning |
| Vector clock conflict | Last-write-wins, log warning |

---

## 9. Observability

### 9.1 Logging

```go
logger.Info("backup: push completed",
    "node_id", nodeID,
    "backup_path", backupPath,
    "compressed_size", size,
    "duration_ms", duration)

logger.Info("sync: peer pull completed",
    "peer_id", peerID,
    "sessions_merged", sessions,
    "turns_merged", turns,
    "memories_merged", memories)
```

### 9.2 Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `backup.push_duration_ms` | Histogram | Time to compress + push backup |
| `backup.size_bytes` | Gauge | Compressed backup size |
| `sync.pull_duration_ms` | Histogram | Time to pull + merge from peers |
| `sync.rows_merged_total` | Counter | Rows merged from all peers |
| `sync.peer_last_sync_timestamp` | Gauge | Last successful sync per peer |
| `gossip.events_sent_total` | Counter | Events published via gossip |
| `gossip.events_received_total` | Counter | Events received from peers |

---

## 10. Future Enhancements

### 10.1 Incremental Backups

Instead of full DB backup daily, track changed rows since last backup:

```sql
-- Add tracking columns
ALTER TABLE sessions ADD COLUMN _modified_at INTEGER DEFAULT (strftime('%s', 'now'));
ALTER TABLE turns ADD COLUMN _modified_at INTEGER DEFAULT (strftime('%s', 'now'));

-- Export only changed rows
SELECT * FROM turns WHERE _modified_at > ?;
```

**Benefit:** Smaller backups, faster push.

### 10.2 Compression Improvements

| Option | Trade-off |
|--------|-----------|
| **zstd --ultra** | Better ratio, slower compression |
| **Dictionary training** | Train on typical DB patterns for better compression |
| **Columnar format** | Convert to Parquet before compression (better for analytics) |

### 10.3 Cloud Storage Backend

Abstract backup destination to support:
- S3/GCS buckets
- Restic repositories
- IPFS for decentralized storage

### 10.4 Conflict Resolution UI

For multi-machine scenarios with frequent conflicts:
- Web UI showing divergent sessions
- Interactive merge tool
- "Accept theirs" / "Accept mine" / "Merge both"

---

## 11. Implementation Checklist

- [ ] **Phase 1: Git Backup Scheduler**
  - [ ] Create `internal/backup/git_backup.go`
  - [ ] Implement daily push with zstd compression
  - [ ] Add manifest generation
  - [ ] Wire into daemon startup
  - [ ] Add CLI: `backup list`, `backup push`

- [ ] **Phase 2: Async Pull/Sync**
  - [ ] Create `internal/backup/sync_puller.go`
  - [ ] Implement peer merge logic (INSERT OR IGNORE)
  - [ ] Add sync metadata tracking
  - [ ] Add CLI: `sync pull`, `sync status`

- [ ] **Phase 3: Dual-DB Router**
  - [ ] Modify `internal/memory/manager.go` to support two DBs
  - [ ] Implement routing logic (local vs. gossip writes)
  - [ ] Implement merged reads (query both, combine results)
  - [ ] Add migration path for existing single-DB deployments

- [ ] **Phase 4: Gossip Event Schema**
  - [ ] Extend `ClusterEvent.EventType` with SESSION_TURN, MEMORY_STORED
  - [ ] Define payload schemas
  - [ ] Implement idempotent merge handlers
  - [ ] Add vector clock support for ordering

- [ ] **Phase 5: Config Sync**
  - [ ] Create `internal/config/sync.go`
  - [ ] Implement shared + per-node config pulling
  - [ ] Add hot-reload hooks for config changes
  - [ ] Add CLI: `config sync status`, `config sync pull`

- [ ] **Phase 6: Documentation**
  - [ ] User guide: `docs/configuration/backup-sync.md`
  - [ ] Architecture doc: `docs/concepts/backup-sync-architecture.md`
  - [ ] CLI reference: `docs/reference/cli.md` updates
  - [ ] Migration guide for existing deployments

- [ ] **Phase 7: Testing**
  - [ ] Unit tests for backup scheduler
  - [ ] Unit tests for sync puller merge logic
  - [ ] Integration test: multi-machine sync via git
  - [ ] Integration test: cluster gossip + backup
  - [ ] Restore test: full recovery from backup

---

## 11.5. Migration Path (Existing Deployments)

For deployments with existing `sessions.db`, `memory.db`, and `queue.db`:

```bash
# Automatic migration on first daemon startup with new config
# ~/.meept/meept.json5 with backup.enabled: true triggers migration

# Step 1: Backup existing databases
cp ~/.meept/sessions.db ~/.meept/migration-backup/sessions.db.pre-migration
cp ~/.meept/memory.db ~/.meept/migration-backup/memory.db.pre-migration

# Step 2: Rename to local.db (new canonical name)
mv ~/.meept/sessions.db ~/.meept/local.db
# Merge memory.db into local.db.memories table
sqlite3 ~/.meept/local.db "ATTACH '~/.meept/memory.db' AS src; INSERT OR IGNORE INTO memories SELECT * FROM src.memories;"

# Step 3: Create empty sync-gossip.db
sqlite3 ~/.meept/sync-gossip.db ".read schema.sql"

# Step 4: Update in-memory references
# Daemon reloads with new dual-DB routing
```

**Post-migration:**
- Old table names (`sessions`, `memories`) preserved in `local.db`
- Existing data treated as "local" (will be backed up + synced to peers)
- No data loss; migration is reversible until first sync occurs

---

## 12. Approval

**Spec author:** Meept Team
**Review date:** 2026-06-26
**Approved by:** [Pending user review]

---

*This document is located at `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`*
