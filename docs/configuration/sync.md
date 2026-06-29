# Peer Sync Configuration

Multi-machine synchronization for personal devices. Peer sync periodically pulls backup repositories from other machines and merges their data into a local read-only `sync-gossip.db`, making sessions and memories created on one device visible on all others.

## Overview

Peer sync (configured under the `peer_sync` key) works alongside the backup scheduler. While backup pushes this node's data to git, peer sync pulls other nodes' data from git and merges it locally.

The flow per pull cycle:

1. **Fetch** — pull the latest from the shared backup repository
2. **Locate** — for each configured peer, find their latest backup database
3. **Attach** — open the peer's compressed `.db.zst` file and attach it to the local SQLite connection as `peer`
4. **Merge** — copy sessions, turns, and memories using `INSERT OR IGNORE` (deduplication by primary key)
5. **Detach** — close the peer database
6. **Record** — store sync metadata (last sync time, row counts, errors) in `sync-gossip.db`

Merged data lands in `~/.meept/sync-gossip.db` (the gossip database), not `local.db`. This keeps each node's unique data isolated from replicated data. The agent loop reads from both databases transparently via the dual-store memory layer.

## Configuration Reference

Peer sync is configured under the `peer_sync` key in `~/.meept/meept.json5`:

```json5
// ~/.meept/meept.json5
{
  peer_sync: {
    // Master switch. When false, no peer sync runs.
    enabled: true,

    // List of peer node IDs to sync with.
    // Each ID must match the backup.node_id configured on the peer machine.
    peers: ["laptop", "desktop"],

    // Interval between automatic pull cycles. Go duration string.
    // Default: 1h (hourly).
    // Set to 0 to disable scheduled pulls (manual `meept sync pull` only).
    pull_schedule: "1h",

    // Maximum time allowed for a single merge operation, in minutes.
    // Prevents a slow or corrupt peer DB from blocking the sync loop.
    // Default: 10.
    max_merge_minutes: 10,

    // Git repo URL for backup synchronization.
    // When empty, the daemon inherits this from backup.repo_url at wiring time.
    repo_url: "",
  },
}
```

### Field Reference

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | bool | `false` | No | Enable peer sync puller |
| `peers` | []string | `[]` | Yes (if enabled) | Peer node IDs to sync with |
| `pull_schedule` | duration | `1h` | No | Interval between pulls |
| `max_merge_minutes` | int | `10` | No | Merge operation timeout |
| `repo_url` | string | inherited from `backup.repo_url` | No | Override backup repo URL |

### Validation

Config validation fails if:
- `enabled: true` but `peers` is empty
- `pull_schedule` is zero or negative (when enabled)
- `max_merge_minutes` is zero or negative (defaults to 10 if so)

## Multi-Machine Setup

### Prerequisites

- Each machine has backup enabled and pushing to the **same** git repository
- Each machine has a unique `backup.node_id`
- Each machine lists the others in `peer_sync.peers`

### Machine A (laptop)

```json5
// ~/.meept/meept.json5
{
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    node_id: "laptop",
    schedule: "24h",
  },
  peer_sync: {
    enabled: true,
    peers: ["desktop"],
    pull_schedule: "1h",
  },
}
```

### Machine B (desktop)

```json5
// ~/.meept/meept.json5
{
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    node_id: "desktop",
    schedule: "24h",
  },
  peer_sync: {
    enabled: true,
    peers: ["laptop"],
    pull_schedule: "1h",
  },
}
```

### Verification

After both machines have pushed at least one backup:

```bash
# On either machine, trigger immediate sync
meept sync pull

# Check sync status
meept sync status
```

Sessions created on the laptop will appear on the desktop (and vice versa) within one pull cycle, or immediately after `meept sync pull`.

## CLI Commands

### `meept sync pull`

Trigger an immediate peer sync, bypassing the schedule.

```bash
meept sync pull
```

This command runs locally (does not require the daemon to be running). It:
1. Loads config from `~/.meept/meept.json5`
2. Opens `~/.meept/local.db`
3. Constructs a `SyncPuller` with the configured peers
4. Pulls the backup repo and merges each peer's latest backup
5. Reports success or failure

Output on success:
```
starting sync pull...
sync pull completed successfully.
```

### `meept sync status`

Display the current sync state and per-peer synchronization history.

```bash
meept sync status
meept sync status --json
```

Output (text format):
```
sync status
=========================================
this node: laptop
sync enabled: true
known peers: [desktop]
pull schedule: 1h0m0s

peer synchronization:
  desktop:
    last sync: 23m ago
    rows received: 14 sessions, 187 turns, 12 memories
    errors: none
```

When no sync history exists yet:
```
sync status
=========================================
this node: laptop
sync enabled: true
known peers: [desktop]
pull schedule: 1h0m0s

no sync history found. run 'meept sync pull' to sync.
```

Use `--json` for machine-readable output including raw timestamps.

## How Merge Works

The merge operation uses SQLite's `ATTACH DATABASE` mechanism:

```sql
-- 1. Attach peer database
ATTACH '/path/to/peer-local.db' AS peer;

-- 2. Merge sessions (skip duplicates by primary key)
INSERT OR IGNORE INTO sessions
SELECT * FROM peer.sessions;

-- 3. Merge turns
INSERT OR IGNORE INTO turns
SELECT * FROM peer.turns;

-- 4. Merge memories
INSERT OR IGNORE INTO memories
SELECT * FROM peer.memories;

-- 5. Detach
DETACH peer;
```

**Key properties:**

- **INSERT OR IGNORE**: Rows with existing primary keys are silently skipped. No data is overwritten.
- **Single transaction**: All merges for one peer run in a single transaction. If any table fails, the entire peer merge is rolled back.
- **Target database**: Merged data goes into `sync-gossip.db`, not `local.db`. The dual-store memory layer reads from both.
- **Error isolation**: A failure merging one peer does not abort merges for other peers. Each peer's error is recorded separately in sync metadata.

The `MergeStats` returned per peer:
```go
type MergeStats struct {
    SessionsMerged int  // rows inserted
    TurnsMerged    int
    MemoriesMerged int
    Skipped        int  // duplicate IDs skipped
    Errors         int  // per-table error count
}
```

## Troubleshooting

### "sync is not enabled"

**Symptoms**: `meept sync pull` prints "sync is not enabled."

**Fix**: Set `peer_sync.enabled: true` in `~/.meept/meept.json5` and ensure `peers` is non-empty.

### Peer not found

**Symptoms**: Sync status shows "never" for a peer, or merge reports zero rows.

**Cause**: The peer has not pushed a backup yet, or its `node_id` does not match what you configured in `peers`.

**Diagnosis**:
```bash
# Check what node IDs exist in the backup repo
git clone git@github.com:caimlas/meept-backups.git /tmp/backups
ls /tmp/backups/backups/*/   # Lists node ID directories
```

**Fix**: Ensure the `peers` list matches the actual `backup.node_id` values used by each machine. Node IDs are case-sensitive.

### Corrupt peer database

**Symptoms**: Merge fails with "peer database is not a valid SQLite database" or "database disk image is malformed".

**Cause**: The peer's backup was interrupted or the git repo contains a corrupt file.

**Fix**:
1. On the peer machine, trigger a fresh backup: `meept backup push --force`
2. On this machine, pull again: `meept sync pull`
3. If the corrupt backup persists in git, manually delete the bad backup directory from the repo and push.

### Schema mismatch

**Symptoms**: Merge fails with "no such table: sessions" or column count mismatch.

**Cause**: The peer is running a different version of meept with an incompatible database schema.

**Fix**: Upgrade both machines to the same meept version. Schema migrations run automatically on daemon startup.

### Sync takes too long

**Symptoms**: Merge exceeds `max_merge_minutes` and is aborted.

**Cause**: The peer database is very large, or the machine is under heavy I/O load.

**Fix**: Increase `max_merge_minutes` in config, or reduce the sync frequency to avoid overlapping merges:

```json5
{
  peer_sync: {
    max_merge_minutes: 30,
    pull_schedule: "6h",
  },
}
```
