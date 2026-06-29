# Backup Configuration

Git-backed SQLite backups for single-node and multi-machine deployments. The backup scheduler compresses local databases with zstd, writes a SHA256-verified manifest, and pushes everything to a git repository on a configurable schedule.

## Overview

When enabled, the backup scheduler runs an immediate backup on daemon startup and then on a recurring ticker. Each backup run:

1. Snapshots the local SQLite databases (`local.db`, and any other configured DBs)
2. Compresses each file with zstd (`.zst` extension)
3. Computes a SHA256 checksum for every database file
4. Writes a `manifest.json` describing the backup set (node ID, timestamp, per-DB sizes and hashes)
5. Commits and pushes to the configured git repository
6. Prunes local backup directories older than `retention_days`
7. On push conflict: rebases and retries up to 3 times

The backup repository is a regular git repo. Each node writes under a `backups/<date>/<node_id>/` prefix, so multiple nodes can share the same repository without colliding.

## Configuration Reference

Backup is configured under the `backup` key in `~/.meept/meept.json5`:

```json5
// ~/.meept/meept.json5
{
  backup: {
    // Master switch. When false, no backups run.
    enabled: true,

    // Git repository URL (SSH or HTTPS). Required when enabled.
    // Use a private repo — backups contain session and memory data.
    repo_url: "git@github.com:caimlas/meept-backups.git",

    // Local checkout directory for the backup repo.
    // Defaults to ~/.meept/backups-git when empty.
    checkout_dir: "",

    // Interval between scheduled backups. Go duration string.
    // Default: 24h (daily).
    schedule: "24h",

    // Number of days to retain local backup directories.
    // Directories older than this are pruned on each run.
    // Default: 12.
    retention_days: 12,

    // Unique identifier for this machine in the backup repo.
    // When empty, a default is derived from the hostname.
    // Set this explicitly in multi-machine setups to avoid collisions.
    node_id: "laptop",
  },
}
```

### Field Reference

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `enabled` | bool | `false` | No | Enable the backup scheduler |
| `repo_url` | string | — | Yes (if enabled) | Git remote URL for backups |
| `checkout_dir` | string | `~/.meept/backups-git` | No | Local git checkout path |
| `schedule` | duration | `24h` | No | Time between scheduled backups |
| `retention_days` | int | `12` | No | Days to keep local backup dirs |
| `node_id` | string | hostname-derived | No | Unique node identifier |

### Validation

The scheduler validates config at construction time. The daemon will fail to start the backup scheduler (logged as an error, does not crash the daemon) if:

- `enabled: true` but `repo_url` is empty
- `schedule` is zero or negative
- `retention_days` is zero or negative

## Setup Guide (Single-Node)

### 1. Create a private git repository

Backups contain session data, memory contents, and embedded API references. Use a private repository.

```bash
# GitHub
gh repo create meept-backups --private

# GitLab
glab repo create meept-backups --private

# Or initialize a bare repo on your own server
ssh backup-server "git init --bare /srv/git/meept-backups.git"
```

### 2. Configure SSH access

The daemon runs as your user and uses your system's git/SSH configuration. Verify the deploy key or SSH key has push access:

```bash
ssh-keygen -t ed25519 -C "meept-backup"
cat ~/.ssh/id_ed25519.pub
# Add the public key as a deploy key in GitHub/GitLab repo settings
```

For HTTPS URLs, configure a credential helper or personal access token:

```bash
# Store credentials (plaintext, macOS keychain on mac)
git config --global credential.helper store
```

### 3. Enable backup in config

Edit `~/.meept/meept.json5`:

```json5
{
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    schedule: "24h",
    retention_days: 12,
    node_id: "laptop",
  },
}
```

### 4. Restart the daemon

```bash
meept daemon restart
```

### 5. Verify

```bash
# Trigger an immediate backup
meept backup push

# List available backups
meept backup list
```

## CLI Commands

### `meept backup list`

List available backups for this node.

```bash
meept backup list
meept backup list --json
```

Output (table format):

```
DATE                NODE     DATABASE      COMPRESSED   UNCOMPRESSED   SHA256
2026-06-26          local    local.db      2.4 MB       8.1 MB         a1b2c3d4
2026-06-25          local    local.db      2.1 MB       7.9 MB         e5f6a7b8
```

When the daemon is reachable, the list is fetched via RPC (`backup.list`). When the daemon is unreachable, the command falls back to scanning the local `~/.meept/backups/` directory and reading each `manifest.json`.

Use `--json` for machine-readable output.

### `meept backup push`

Trigger an immediate backup push, bypassing the schedule.

```bash
meept backup push
meept backup push --force
```

The `--force` flag pushes even if no database changes are detected since the last backup. Without `--force`, the scheduler may skip the push if the compressed output is identical to the previous run.

This command dispatches via RPC (`backup.push`) to the running daemon.

## How Backups Work

### Compression

Each database file is compressed with [zstd](https://github.com/klauspost/compress/zstd) to a `.zst` file. SQLite databases typically compress well (3-5x ratio) due to page alignment and repetitive internal structures.

### Manifest

Each backup directory contains a `manifest.json`:

```json
{
  "node_id": "laptop",
  "timestamp": "2026-06-26T03:00:00Z",
  "databases": [
    {
      "name": "local.db",
      "compressed_size": 2516582,
      "uncompressed_size": 8493465,
      "sha256": "a1b2c3d4e5f6...",
      "compressed_path": "backups/2026-06-26/laptop/local.db.zst"
    }
  ],
  "sync_metadata": {
    "peers_synced": [],
    "gossip_events_sent_24h": 0,
    "gossip_events_recv_24h": 0
  }
}
```

The SHA256 checksum lets you verify integrity after restore. The `sync_metadata` block tracks cluster sync state for diagnostic purposes.

### Retention Pruning

After each successful backup, the scheduler scans the local `~/.meept/backups/` directory and removes subdirectories older than `retention_days`. Pruning is local-only — it does not delete commits from the git repository.

### Git Push with Rebase + Retry

Pushes use go-git and follow this protocol:

1. Stage and commit all changed files in the local checkout
2. Attempt push to `origin`
3. On rejection (non-fast-forward), fetch and rebase onto remote HEAD
4. Retry push (up to 3 attempts total)
5. If all retries fail, log the error and wait for the next scheduled run

This means concurrent pushes from multiple nodes are handled automatically — the losing node rebases and retries. No manual intervention is needed for the common case.

## Recovery

To restore from a backup:

### From the same machine

```bash
# 1. Stop the daemon
meept daemon stop

# 2. Locate the backup to restore
meept backup list

# 3. Decompress the database
zstd -d ~/.meept/backups/2026-06-26/laptop/local.db.zst -o ~/.meept/local.db

# 4. Verify integrity
shasum -a 256 ~/.meept/local.db
# Compare against the sha256 in manifest.json

# 5. Restart the daemon
meept daemon start
```

### From a different machine (disaster recovery)

```bash
# 1. Clone the backup repo
git clone git@github.com:caimlas/meept-backups.git /tmp/backups

# 2. Find the backup manifest
cat /tmp/backups/backups/2026-06-26/laptop/manifest.json

# 3. Decompress
zstd -d /tmp/backups/backups/2026-06-26/laptop/local.db.zst -o ~/.meept/local.db

# 4. Verify checksum
shasum -a 256 ~/.meept/local.db

# 5. Start the daemon
meept daemon start
```

## Troubleshooting

### "backup config is invalid" on daemon startup

**Cause**: Config validation failed. Check daemon logs for the specific reason.

**Fixes**:
- `enabled: true` requires `repo_url` to be non-empty
- `schedule` must be a positive duration (e.g. `"24h"`, not `"0"`)
- `retention_days` must be a positive integer

### Git push fails with conflict

**Symptoms**: Daemon logs show "push rejected" or "non-fast-forward".

**Cause**: Another node pushed to the same backup repo between your last fetch and push.

**Fix**: The scheduler auto-rebases and retries up to 3 times. If it still fails, the next scheduled run will succeed. For immediate resolution:

```bash
cd ~/.meept/backups-git
git pull --rebase origin main
git push
```

If the repo is in a bad state, delete the local checkout and let the scheduler re-clone:

```bash
rm -rf ~/.meept/backups-git
meept daemon restart
```

### Repository unreachable

**Symptoms**: "dial tcp: connection refused" or "permission denied (publickey)".

**Diagnosis**:
```bash
# Test SSH access
ssh -T git@github.com

# Test HTTPS access
git ls-remote git@github.com:caimlas/meept-backups.git
```

**Fixes**:
- Verify SSH key is added to the deploy keys
- Verify `repo_url` matches the actual repository URL
- Check network connectivity and firewall rules

### Disk full

**Symptoms**: Backup fails with "no space left on device".

**Fix**: Reduce `retention_days` to free space, or move the backup checkout to a larger volume:

```json5
{
  backup: {
    checkout_dir: "/Volumes/External/meept-backups",
    retention_days: 7,
  },
}
```

### Backup runs but nothing is pushed

**Cause**: The scheduler may detect no changes (identical SHA256 to previous backup) and skip the push. This is expected behavior.

**Fix**: Use `--force` to push regardless:

```bash
meept backup push --force
```
