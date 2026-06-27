# Backup and Sync Architecture

This document describes the architecture of Meept's backup and synchronization system, covering design decisions, data flow, and failure modes.

## Overview

The backup/sync system enables Meept deployments to:
1. **Preserve state** via daily git backups
2. **Share state** across machines via git sync pulls
3. **Replicate state** in real-time via cluster gossip

## Dual-DB Architecture

### Motivation

The dual-DB architecture (`local.db` + `sync-gossip.db`) solves two key problems:

1. **Storage efficiency**: Git repos don't bloat with duplicate peer data
2. **Clear ownership**: Each node owns its `local.db`; peer data is explicitly marked

```
┌──────────────────────────────────────────────────────┐
│                 Single-DB (Legacy)                    │
│  brain.db                                             │
│  ├─ sessions (local + peer mixed)                    │
│  ├─ turns (local + peer mixed)                       │
│  └─ memories (local + peer mixed)                    │
│                                                       │
│  Problem: Git backup contains peer data (bloat)      │
└──────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────┐
│                 Dual-DB (Current)                     │
│  local.db                sync-gossip.db              │
│  ├─ sessions (local)     ├─ sessions (peer)          │
│  ├─ turns (local)        ├─ turns (peer)             │
│  └─ memories (local)     └─ memories (peer)          │
│                            + source_node tracking    │
│                                                      │
│  Solution: Git backup only local.db (efficient)      │
└──────────────────────────────────────────────────────┘
```

### Data Routing

**Writes** (from agent loop):
```
AgentLoop.StoreTurn(turn)
    ↓
DualStore.StoreTurn()
    ↓
if turn.source_node == local_node_id:
    write to local.db          # Backed up daily
else:
    write to sync-gossip.db    # Recovered via gossip/pull
    publish gossip event       # If cluster enabled
```

**Reads** (merged view):
```
DualStore.GetSession(id)
    ↓
Try local.db first
If not found OR merged view requested:
    UNION local.db + sync-gossip.db
    ↓
Return merged result
```

### Migration from Single-DB

The `meept migrate` command performs:
1. Create `local.db` and `sync-gossip.db` schemas
2. Read existing `brain.db`
3. Route rows by `source_node` (or current `node_id` if unknown)
4. Verify counts match
5. Rename `brain.db` to `brain.db.bak` (rollback safe)

## Git Channel (Async)

### Backup Scheduler

**Purpose**: Daily compressed backups of `local.db` to git.

**Flow**:
```
┌─────────────────────────────────────────────────────────┐
│ Backup Scheduler (internal/backup/git_backup.go)        │
│                                                         │
│  1. Wait for schedule trigger (e.g., 24h)              │
│  2. Compress local.db → backup-<timestamp>.db.zst      │
│  3. Compute SHA256 → manifest.json5                    │
│  4. Git add, commit, push                              │
│  5. On conflict: git rebase, retry                     │
│  6. Prune old backups (>retention_days)                │
└─────────────────────────────────────────────────────────┘
```

**Backup format**:
```
backups-repo/
├── 2026/
│   ├── 06/
│   │   ├── 26/
│   │   │   ├── backup-20260626-030000.db.zst
│   │   │   └── manifest.json5
│   │   └── ...
│   └── ...
└── ...
```

**manifest.json5**:
```json5
{
  timestamp: "2026-06-26T03:00:00Z",
  node_id: "machine-a",
  database: {
    name: "local.db",
    size_bytes: 2516582,
    sha256: "abc123...",
    rows: {
      sessions: 142,
      turns: 1847,
      memories: 89,
    }
  }
}
```

### Sync Puller

**Purpose**: Hourly pull of peer backups from git, merge into `sync-gossip.db`.

**Flow**:
```
┌─────────────────────────────────────────────────────────┐
│ Sync Puller (internal/backup/sync_puller.go)            │
│                                                         │
│  1. Wait for schedule trigger (e.g., 1h)               │
│  2. Git pull backup repo                               │
│  3. For each peer backup (NOT this node's):            │
│     a. Decompress → temp file                          │
│     b. MergePeerDB() via INSERT OR IGNORE              │
│     c. Tag with source_node = peer_id                  │
│     d. Clean up temp file                              │
│  4. Update sync_metadata (last_pull timestamp)         │
│  5. On error: skip peer, continue with others          │
└─────────────────────────────────────────────────────────┘
```

**Merge semantics** (`INSERT OR IGNORE`):
- Idempotent: Same peer backup merged twice = no duplicates
- Append-only: New rows from peer are added
- No overwrites: Existing local data preserved

**Example merge SQL**:
```sql
ATTACH ? AS peer_db;

INSERT OR IGNORE INTO sessions (id, created_at, updated_at, metadata, source_node)
SELECT id, created_at, updated_at, metadata, ? FROM peer_db.sessions;

INSERT OR IGNORE INTO turns (turn_id, session_id, role, content, timestamp, source_node)
SELECT turn_id, session_id, role, content, timestamp, ? FROM peer_db.turns;

INSERT OR IGNORE INTO memories (id, type, category, content, created_at, agent_id, session_id, source_node)
SELECT id, type, category, content, created_at, agent_id, session_id, ? FROM peer_db.memories;

DETACH peer_db;
```

## Gossip Channel (Real-Time)

### Gossip Engine

**Purpose**: Real-time bilateral sync for cluster deployments.

**Protocol**:
1. **Heartbeat** (every 30s): Nodes broadcast presence
2. **Event publish** (on write): New sessions/memories gossiped immediately
3. **Event receive** (via TCP/WireGuard): Peers merge events into `sync-gossip.db`

**Event types** (Phase 4):
| Event | Payload | Use Case |
|-------|---------|----------|
| `SESSION_CREATED` | Session metadata | New chat session |
| `SESSION_TURN` | Turn content + role | User/assistant messages |
| `MEMORY_STORED` | Memory content + type | New memory created |
| `MEMORY_EXPIRED` | Memory ID | Memory TTL expired |
| `MEMORY_EDGE` | From/To IDs + edge type | Epistemic relationships |

**Gossip message format**:
```json
{
  "event_id": "gossip-abc123",
  "node_id": "node-a",
  "event_type": "SESSION_TURN",
  "timestamp": 1719403200000000000,
  "vector_clock": {"node-a": 42, "node-b": 38, "node-c": 40},
  "payload": {
    "session_id": "sess-xyz",
    "turn_id": "turn-123",
    "role": "user",
    "content": "Hello!"
  },
  "signature": "ed25519:abc123..."
}
```

### Vector Clocks

**Purpose**: Causal ordering of events across nodes.

**Structure**:
```
vector_clock = {
  "node-a": 42,  // Last event sequence from node-a
  "node-b": 38,  // Last event sequence from node-b
  "node-c": 40,  // Last event sequence from node-c
}
```

**Operations**:
1. **Increment** (on local event): `vc[node_id]++`
2. **Update** (on receive): `vc[k] = max(vc[k], received_vc[k])` for all k
3. **Compare**: Determine if event happened-before, after, or concurrent

**Use cases**:
- Deduplication: Skip events already processed
- Ordering: Process events in causal order
- Conflict detection: Concurrent events need resolution

### Conflict Resolution

**Last-write-wins** (default):
```go
func Resolve(event1, event2 *ClusterEvent) *ClusterEvent {
    if event1.Timestamp.After(event2.Timestamp) {
        return event1
    }
    // Tiebreaker: lower node_id wins
    if event1.Timestamp.Equal(event2.Timestamp) {
        if event1.NodeID < event2.NodeID {
            return event1
        }
        return event2
    }
    return event2
}
```

**For concurrent events** (vector clock incomparable):
- Sessions/memories: Both accepted (append-only)
- Config updates: Based on `conflict_mode` setting

## Config Sync (Git Distribution)

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│ Config Repo (git)                                       │
│                                                         │
│ config/                                                 │
│ ├── shared/                                             │
│ │   ├── meept.json5        # Cluster-wide config       │
│ │   ├── models.json5       # LLM model resolution      │
│ │   └── mcp_servers.json5  # MCP server catalog        │
│ └── nodes/                                              │
│     ├── node-a/                                         │
│     │   └── meept.json5    # Node A overrides          │
│     ├── node-b/                                         │
│     │   └── meept.json5    # Node B overrides          │
│     └── ...                                             │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ Config Syncer (per node)                                │
│                                                         │
│  1. Git pull (shallow clone, --depth 1)                │
│  2. Copy config/shared/* → ~/.meept/                   │
│  3. Copy config/nodes/<node_id>/* → ~/.meept/          │
│     (overrides shared config)                          │
│  4. Trigger reload hooks                               │
│  5. Notify components of changes                       │
└─────────────────────────────────────────────────────────┘
```

### Deep Merge Semantics

Override values replace shared values; nested objects merge deeply:

```json5
// config/shared/meept.json5
{
  cluster: {
    enabled: true,
    cluster_id: "prod",
    gossip_port: 51820,
  },
  backup: {
    enabled: true,
    schedule: "24h",
  }
}

// config/nodes/node-a/meept.json5
{
  cluster: {
    node_id: "node-a",  // Override
  },
  backup: {
    retention_days: 30,  // New field
  }
}

// Result (~/.meept/meept.json5)
{
  cluster: {
    enabled: true,       // From shared
    cluster_id: "prod",  // From shared
    gossip_port: 51820,  // From shared
    node_id: "node-a",   // From node-a override
  },
  backup: {
    enabled: true,       // From shared
    schedule: "24h",     // From shared
    retention_days: 30,  // From node-a override
  }
}
```

### Hot-Reload Hooks

Components register callbacks for config changes:

```go
// Daemon wiring
configSyncer.RegisterReloadHook("meept.json5", func(oldCfg, newCfg *config.Config) error {
    // Reload backup scheduler config
    d.backupScheduler.UpdateConfig(newCfg.Backup)

    // Reload cluster engine config
    d.clusterEngine.UpdateConfig(newCfg.Cluster)

    return nil
})

configSyncer.RegisterReloadHook("models.json5", func(oldCfg, newCfg *config.Config) error {
    // Reload LLM resolver
    return d.llmResolver.Reload(newCfg.LLM)
})
```

**Reload flow**:
```
Config file changed (git pull)
    ↓
Validate new config (JSON5 syntax)
    ↓
Atomic swap: temp file → ~/.meept/<file>
    ↓
Trigger reload hooks (parallel)
    ↓
If any hook fails: log error, continue (config still valid)
```

## Failure Modes and Recovery

### Git Push Conflicts

**Scenario**: Two nodes push backups simultaneously.

**Detection**: `git push` fails with "non-fast-forward" error.

**Recovery** (automatic):
1. Git rebase: `git pull --rebase`
2. Re-commit with new timestamp
3. Retry push (up to 3 times)
4. If still fails: log warning, retry next schedule

**Manual intervention** (rare):
```bash
cd ~/.meept/.backup-cache
git pull --rebase
git push
```

### Corrupt Backup

**Scenario**: Backup file corrupted during compression or git push.

**Detection**: SHA256 mismatch in manifest.

**Recovery** (automatic):
1. Verify SHA256 before push
2. Skip corrupt backup, retry next schedule
3. Log error with details

**Manual intervention**:
```bash
# Force new backup
meept backup push --force
```

### Merge Failure

**Scenario**: Peer DB corrupt or schema mismatch.

**Detection**: SQLite error during `ATTACH` or `INSERT`.

**Recovery** (automatic):
1. Log error with peer ID
2. Skip peer, continue with others
3. Retry next pull cycle

**Manual intervention**:
```bash
# Manually pull and inspect
cd ~/.meept/.config-sync
git pull
sqlite3 path/to/peer-backup.db ".schema"
```

### Gossip Network Partition

**Scenario**: Cluster node isolated from peers (network failure).

**Detection**: No heartbeats received for >3 intervals.

**Recovery** (automatic):
1. Mark peer as "inactive" in `ClusterEngine.peers`
2. Continue local operation (degraded mode)
3. On reconnect: process backlog via vector clock sync

**Data consistency**:
- Concurrent events on both sides: Both accepted (append-only)
- Conflicts: Resolved via last-write-wins + node ID tiebreaker

### Config Sync Invalid Config

**Scenario**: Invalid JSON5 syntax in pushed config.

**Detection**: JSON5 parse error on pull.

**Recovery** (automatic):
1. Skip invalid file, log error
2. Retain previous valid config
3. Continue operation with old config

**Manual intervention**:
```bash
# Fix config in git repo
git revert <bad-commit>
git push
meept config sync pull  # Force refresh
```

## Performance Characteristics

| Operation | Latency | Throughput | Notes |
|-----------|---------|------------|-------|
| Backup push (daily) | ~5-30s | 1/day | Depends on DB size, network |
| Sync pull (hourly) | ~10-60s | 1/hour | Depends on peer count |
| Gossip event | <100ms | 1000/s | TCP within LAN |
| Gossip event (WAN) | <5s | 100/s | WireGuard across regions |
| Config pull | ~2-10s | 1/5min | Shallow clone, small files |
| Migration (1GB DB) | ~30-60s | N/A | One-time operation |

## Security Considerations

### Git Authentication

**SSH keys** (recommended):
```bash
ssh-keygen -t ed25519 -C "meept-backup"
# Add public key to GitHub/GitLab deploy keys
```

**HTTPS with PAT** (alternative):
```
repo_url: "https://<token>@github.com/caimlas/meept-backups.git"
```

### Node Signatures

When `cluster.require_node_signatures` is enabled:
- Every gossip event signed with ed25519
- Signature verified on receipt
- Unsigned events rejected (logged)

**Key generation** (automatic on first start):
```go
pub, priv, err := ed25519.GenerateKey(rand.Reader)
```

**Key distribution** (manual for MVP):
```json5
// ~/.meept/meept.json5
{
  cluster: {
    node_signing_key: "ed25519:abc123...",  // Private key (base64)
  }
}
```

Future: Automatic key exchange via gossip protocol.

## Observability

### Metrics (Prometheus)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `backup_pushes_total` | Counter | `success` | Total backup pushes |
| `sync_pulls_total` | Counter | `peer_id`, `success` | Total sync pulls |
| `gossip_events_published_total` | Counter | `event_type` | Events published |
| `gossip_events_received_total` | Counter | `event_type`, `source_node` | Events received |
| `config_sync_pulls_total` | Counter | `success` | Config sync pulls |
| `merge_conflicts_total` | Counter | `event_type` | Merge conflicts resolved |

### Logs

Key log messages:
- `"backup scheduler started"` - Backup initialized
- `"backup push succeeded"` / `"backup push failed"` - Push result
- `"sync pull completed"` / `"sync pull failed"` - Pull result
- `"gossip event published"` / `"gossip event received"` - Gossip flow
- `"config reloaded"` - Config hot-reload success

### Health Checks

```bash
# Check backup status
meept backup list

# Check sync status
meept sync status

# Check cluster status
meept cluster status
meept cluster peers

# Check config sync status
meept config sync status
```

## Related Documents

- **User guide**: `docs/configuration/backup-sync.md`
- **Migration guide**: `docs/migration/existing-deployments.md`
- **Cluster configuration**: `docs/configuration/cluster.md`
- **Design spec**: `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`
