# Configuration Backup and Sync

This guide covers Meept's backup and synchronization features for preserving configuration and session data across deployments.

## Overview

Meept provides a unified backup/sync system supporting three deployment modes:

| Mode | Description | Components |
|------|-------------|------------|
| **Single-node** | Daily git backups of local state | Backup Scheduler |
| **Multi-machine** | Backup + peer sync via git | Backup Scheduler + Sync Puller |
| **Cluster** | Real-time gossip + git backups | Backup Scheduler + Gossip Engine |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Meept Daemon                          │
│  ┌──────────────────┐        ┌────────────────────────┐ │
│  │   local.db       │        │   sync-gossip.db       │ │
│  │   (unique data)  │        │   (peer data)          │ │
│  │   BACKED UP      │        │   NOT backed up        │ │
│  └────────┬─────────┘        └───────────┬────────────┘ │
│           │                               │              │
│           ▼                               ▼              │
│  ┌─────────────────┐             ┌─────────────────┐    │
│  │ Git Backup Repo │             │ Gossip (Cluster)│    │
│  │ (daily pushes)  │             │ (real-time)     │    │
│  └─────────────────┘             └─────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

**Key Design Decisions:**

1. **Dual-DB Architecture**: `local.db` (unique, backed up) + `sync-gossip.db` (replicated, recovered via sync)
2. **Git for async**: Daily backups + hourly peer sync pulls
3. **Gossip for real-time**: Cluster peers exchange sessions/memories instantly via WireGuard

## Configuration Reference

### Backup Configuration

```json5
// ~/.meept/meept.json5
{
  backup: {
    // Enable/disable backup scheduler
    enabled: true,

    // Git repo URL (SSH or HTTPS)
    repo_url: "git@github.com:caimlas/meept-backups.git",

    // Backup schedule (Go duration format)
    schedule: "24h",

    // Days to retain backups (pruned on each run)
    retention_days: 12,

    // Unique node identifier (auto-generated if empty)
    node_id: "machine-a",
  }
}
```

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | bool | false | No | Enable backup scheduler |
| `repo_url` | string | - | Yes (if enabled) | Git remote URL |
| `schedule` | duration | 24h | No | Backup interval |
| `retention_days` | int | 12 | No | Backup retention |
| `node_id` | string | auto | No | Unique machine ID |

### Sync Configuration (Multi-Machine)

```json5
{
  sync: {
    // Enable peer sync via git
    enabled: true,

    // Known peer node IDs
    peers: ["laptop", "desktop"],

    // Pull interval from backup repo
    pull_schedule: "1h",

    // Max time allowed for merge operation
    max_merge_minutes: 10,
  }
}
```

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | bool | false | No | Enable sync puller |
| `peers` | []string | [] | Yes (if enabled) | Peer node IDs |
| `pull_schedule` | duration | 1h | No | Pull interval |
| `max_merge_minutes` | int | 10 | No | Merge timeout |

### Config Sync (Shared Configuration)

```json5
{
  config_sync: {
    // Enable config distribution
    enabled: true,

    // Git repo containing shared configs
    repo_url: "git@github.com:caimlas/meept-config.git",

    // Pull interval for config updates
    pull_schedule: "5m",

    // Conflict resolution: "local-wins", "remote-wins", "manual"
    conflict_mode: "local-wins",
  }
}
```

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | bool | false | No | Enable config sync |
| `repo_url` | string | - | Yes (if enabled) | Config repo URL |
| `pull_schedule` | duration | 5m | No | Config pull interval |
| `conflict_mode` | string | local-wins | No | Conflict strategy |

### Cluster Configuration (Real-Time Gossip)

```json5
{
  cluster: {
    // Enable cluster mode
    enabled: true,

    // Cluster identifier (shared by all nodes)
    cluster_id: "prod-cluster",

    // This node's ID
    node_id: "node-a",

    // WireGuard port for gossip
    gossip_port: 51820,

    // Require ed25519 signatures on events
    require_node_signatures: true,
  }
}
```

See `docs/configuration/cluster.md` for full cluster configuration options.

## Setup Guides

### Single-Node Setup (Backup Only)

**Use case**: Personal deployment with daily backups to git.

1. **Generate SSH key** (if needed):
   ```bash
   ssh-keygen -t ed25519 -C "meept-backup"
   cat ~/.ssh/id_ed25519.pub  # Add to GitHub/GitLab deploy keys
   ```

2. **Create backup repo**:
   ```bash
   gh repo create meeft-backups --private
   ```

3. **Configure backup**:
   ```json5
   // ~/.meept/meept.json5
   {
     backup: {
       enabled: true,
       repo_url: "git@github.com:caimlas/meept-backups.git",
       schedule: "24h",
       retention_days: 12,
     }
   }
   ```

4. **Restart daemon**:
   ```bash
   meept daemon restart
   ```

5. **Verify**:
   ```bash
   meept backup list
   ```

### Multi-Machine Setup (Backup + Peer Sync)

**Use case**: Personal sessions synced across laptop + desktop.

1. **On Machine A (laptop)**:
   ```json5
   {
     backup: {
       enabled: true,
       repo_url: "git@github.com:caimlas/meept-backups.git",
       node_id: "laptop",
     },
     sync: {
       enabled: true,
       peers: ["desktop"],
       pull_schedule: "1h",
     }
   }
   ```

2. **On Machine B (desktop)**:
   ```json5
   {
     backup: {
       enabled: true,
       repo_url: "git@github.com:caimlas/meept-backups.git",
       node_id: "desktop",
     },
     sync: {
       enabled: true,
       peers: ["laptop"],
       pull_schedule: "1h",
     }
   }
   ```

3. **Verify sync**:
   ```bash
   # On either machine
   meept sync status
   ```

**How it works**:
- Each machine backs up its `local.db` daily
- Hourly, each machine pulls from git and merges peer data into `sync-gossip.db`
- Sessions created on laptop appear on desktop within 1 hour (or immediately via `meept sync pull`)

### Cluster Deployment (Real-Time Gossip)

**Use case**: Production cluster with 3+ nodes sharing state in real-time.

1. **Create config repo**:
   ```bash
   gh repo create meeft-cluster-config --private
   mkdir meeft-config && cd meeft-config
   mkdir -p config/shared config/nodes/{node-a,node-b,node-c}
   ```

2. **Create shared config** (`config/shared/meept.json5`):
   ```json5
   {
     cluster: {
       enabled: true,
       cluster_id: "prod-cluster",
       gossip_port: 51820,
     },
     backup: {
       enabled: true,
       repo_url: "git@github.com:caimlas/meept-cluster-backups.git",
       schedule: "24h",
     },
     config_sync: {
       enabled: true,
       repo_url: "git@github.com:caimlas/meept-cluster-config.git",
       pull_schedule: "5m",
     }
   }
   ```

3. **Create node-specific overrides** (`config/nodes/node-a/meept.json5`):
   ```json5
   {
     cluster: {
       node_id: "node-a",
     }
   }
   ```

4. **Push config**:
   ```bash
   git add .
   git commit -m "Initial cluster config"
   git push -u origin main
   ```

5. **On each node**, configure base path:
   ```json5
   {
     config_sync: {
       enabled: true,
       repo_url: "git@github.com:caimlas/meept-cluster-config.git",
       node_id: "node-a",  // Unique per node
     }
   }
   ```

6. **Verify cluster**:
   ```bash
   meept cluster status
   ```

**How it works**:
- Gossip exchanges sessions/memories in real-time (<5s latency)
- Daily git backups provide durability
- Config sync distributes shared config + per-node overrides every 5 minutes

## CLI Reference

### Backup Commands

```bash
# List backups in git repo
meept backup list

# Push backup to git (manual trigger)
meept backup push

# Force push (rebase + push even if conflicts)
meept backup push --force
```

**Example output** (`meept backup list`):
```
Backups in git@github.com:caimlas/meept-backups.git
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Date                 Size      Commit
2026-06-26 03:00     2.4 MB    abc1234
2026-06-25 03:00     2.1 MB    def5678
2026-06-24 03:00     1.9 MB    9012xyz
```

### Sync Commands

```bash
# Show sync status
meept sync status

# Force pull from backup repo
meept sync pull
```

**Example output** (`meept sync status`):
```
Sync Status
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Enabled        true
Peers          laptop, desktop
Last pull      23 minutes ago
Last commit    abc1234 (2026-06-26)
```

### Config Sync Commands

```bash
# Show config sync status
meept config sync status

# Force pull and reload config
meept config sync pull
```

**Example output** (`meept config sync status`):
```
Config Sync Status
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Repo           git@github.com:caimlas/meept-config.git
Node           node-a
Pull rate      5m0s
Last commit    abc1234 (2026-06-26)

Shared configs applied:
  ✓ meept.json5
  ✓ models.json5
  ✓ mcp_servers.json5

Node overrides (node-a):
  ✓ meept.json5 (overridden)
```

### Migrate Command

```bash
# Dry-run migration (shows what would happen)
meept migrate --dry-run

# Migrate single DB to dual-DB architecture
meept migrate
```

**Example output** (`meept migrate --dry-run`):
```
Migration Dry-Run
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Source:        ~/.meept/brain.db
Local DB:      ~/.meept/local.db
Gossip DB:     ~/.meept/sync-gossip.db

Sessions to migrate:     142
Turns to migrate:        1,847
Memories to migrate:     89

No changes will be made until --dry-run is removed.
```

## Troubleshooting

### Backup Fails with "Git Conflict"

**Symptoms**: `backup push` fails with "conflict" error.

**Causes**:
- Another machine pushed to the same backup repo
- Manual edits to backup repo

**Solutions**:
1. **Automatic retry** (default): Wait for next scheduled run, automatic rebase
2. **Manual pull + rebase**:
   ```bash
   cd ~/.meept/.backup-cache
   git pull --rebase
   git push
   ```
3. **Force push** (last resort):
   ```bash
   meept backup push --force
   ```

### Sync Pull Fails with "No Peers"

**Symptoms**: `sync status` shows empty peers list.

**Solution**: Ensure `sync.peers` is populated in config:
```json5
{
  sync: {
    enabled: true,
    peers: ["laptop", "desktop"],  // Must match peer node_ids
  }
}
```

### Config Sync Not Applying Changes

**Symptoms**: Config changes in git repo don't apply locally.

**Diagnosis**:
1. Check sync status: `meept config sync status`
2. Verify repo URL matches: `meept config get config_sync.repo_url`
3. Check daemon logs: `journalctl -u meeft -f`

**Solutions**:
1. **Manual trigger**: `meept config sync pull`
2. **Check conflict mode**: If `manual`, conflicts must be resolved first
3. **Restart daemon**: `meept daemon restart` (if hot-reload failed)

### Gossip Events Not Replicating

**Symptoms**: Sessions/memories not appearing on cluster peers.

**Diagnosis**:
1. Check cluster status: `meept cluster status`
2. Verify peers see each other: `meept cluster peers`
3. Check network connectivity (WireGuard port 51820)

**Solutions**:
1. **Restart gossip**: `meept daemon restart`
2. **Check firewall**: Ensure UDP 51820 is open between peers
3. **Verify node signatures**: If enabled, all nodes must have keys configured

### Migration Fails

**Symptoms**: `meept migrate` fails with error.

**Common errors**:
- `source DB not found`: Ensure `brain.db` exists at expected path
- `disk full`: Free space before migrating
- `corrupt DB`: Run `sqlite3 ~/.meept/brain.db "PRAGMA integrity_check"`

**Solutions**:
1. **Backup first**: `cp ~/.meept/brain.db ~/.meept/brain.db.bak`
2. **Dry-run**: Always use `--dry-run` first
3. **Check integrity**: `sqlite3 ~/.meept/brain.db "PRAGMA integrity_check"`

## Next Steps

- **Architecture deep dive**: `docs/concepts/backup-sync-architecture.md`
- **Migration guide**: `docs/migration/existing-deployments.md`
- **Cluster configuration**: `docs/configuration/cluster.md`
