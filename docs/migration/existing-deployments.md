# Migrating Existing Deployments

This guide covers migration procedures for existing Meept deployments to the new backup/sync architecture with dual-DB support.

## Overview

The migration involves:
1. Splitting single `brain.db` into `local.db` + `sync-gossip.db`
2. Enabling git backup scheduler
3. (Optional) Configuring peer sync for multi-machine setups
4. (Optional) Enabling cluster gossip for real-time sync

**Migration time**: 5-15 minutes for typical deployments (<1GB DB)

**Risk level**: Low (dry-run mode available, rollback safe)

## Pre-Migration Checklist

Before migrating, verify:

- [ ] **Backup existing data**
  ```bash
  cp -r ~/.meept ~/.meept.bak
  ```

- [ ] **Check disk space** (need 2x current DB size)
  ```bash
  du -sh ~/.meept/
  # Should have at least 2x available:
  df -h ~/.meept/
  ```

- [ ] **Verify DB integrity**
  ```bash
  sqlite3 ~/.meept/brain.db "PRAGMA integrity_check"
  # Expected: "ok"
  ```

- [ ] **Record current state**
  ```bash
  sqlite3 ~/.meept/brain.db "SELECT COUNT(*) FROM sessions;"
  sqlite3 ~/.meept/brain.db "SELECT COUNT(*) FROM turns;"
  sqlite3 ~/.meept/brain.db "SELECT COUNT(*) FROM memories;"
  ```

- [ ] **Stop daemon**
  ```bash
  meept daemon stop
  # Or: systemctl stop meeft
  ```

- [ ] **Generate node ID** (if not already set)
  ```bash
  # Use hostname or generate unique ID
  hostname  # e.g., "laptop", "desktop", "prod-node-a"
  ```

## Migration Steps

### Step 1: Dry Run

First, verify migration would succeed without making changes:

```bash
meept migrate --dry-run
```

**Expected output**:
```
Migration Dry-Run
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Source:        ~/.meept/brain.db
Local DB:      ~/.meept/local.db
Gossip DB:     ~/.meept/sync-gossip.db

Sessions to migrate:     142
Turns to migrate:        1,847
Memories to migrate:     89

Estimated disk space:    4.2 MB
Estimated time:          ~30 seconds

No changes will be made until --dry-run is removed.
```

**If errors occur**:
- `source DB not found`: Verify `brain.db` path
- `disk full`: Free up space
- `corrupt DB`: See "Troubleshooting" below

### Step 2: Configure Backup

Edit `~/.meept/meept.json5`:

```json5
{
  // Add backup configuration
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    schedule: "24h",
    retention_days: 12,
    node_id: "laptop",  // Unique per machine
  },

  // Optional: Enable peer sync (multi-machine)
  sync: {
    enabled: false,  // Set true for multi-machine
    peers: ["desktop"],  // Other machine node_ids
    pull_schedule: "1h",
  },

  // Optional: Enable config sync
  config_sync: {
    enabled: false,  // Set true for shared config
    repo_url: "git@github.com:caimlas/meept-config.git",
    pull_schedule: "5m",
  },
}
```

### Step 3: Run Migration

Execute the migration:

```bash
meept migrate
```

**Expected output**:
```
Migrating to dual-DB architecture...
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Source:        ~/.meept/brain.db
Local DB:      ~/.meept/local.db
Gossip DB:     ~/.meept/sync-gossip.db

Creating schemas... OK
Migrating sessions... 142 rows
Migrating turns... 1,847 rows
Migrating memories... 89 rows
Updating sync_metadata... OK

Backing up source DB to brain.db.bak... OK
Verifying migration... OK

Migration complete!
You can now safely start the daemon.
```

**What happens**:
1. Creates `local.db` and `sync-gossip.db` with appropriate schemas
2. Routes all existing data to `local.db` (tagged with current `node_id`)
3. Renames `brain.db` to `brain.db.bak` (preserved for rollback)
4. Verifies row counts match

### Step 4: Verify Migration

Before starting daemon, verify:

```bash
# Check files exist
ls -la ~/.meept/*.db
# Expected: local.db, sync-gossip.db, brain.db.bak

# Verify row counts match pre-migration
sqlite3 ~/.meept/local.db "SELECT COUNT(*) FROM sessions;"
sqlite3 ~/.meept/local.db "SELECT COUNT(*) FROM turns;"
sqlite3 ~/.meept/local.db "SELECT COUNT(*) FROM memories;"

# Verify source_node is set
sqlite3 ~/.meept/local.db "SELECT DISTINCT source_node FROM sessions;"
# Expected: Your node_id (e.g., "laptop")
```

### Step 5: Start Daemon

```bash
meept daemon start
# Or: systemctl start meeft
```

**Verify startup**:
```bash
# Check daemon status
meept status

# Check backup scheduler (should start after ~30s)
tail -f ~/.meept/daemon.log | grep "backup scheduler"
```

### Step 6: First Backup (Manual Trigger)

Trigger initial backup manually to verify git integration:

```bash
meept backup push
```

**Expected output**:
```
Pushing backup to git@github.com:caimlas/meept-backups.git...
Compressing local.db... 2.4 MB → 0.8 MB
Computing SHA256... abc123...
Git add, commit, push... OK

Backup complete!
```

**Verify in git repo**:
```bash
# If you have access to the backup repo:
git clone git@github.com:caimlas/meept-backups.git
ls -la backups/2026/06/
# Expected: backup-20260626-<timestamp>.db.zst, manifest.json5
```

### Step 7: Post-Migration Verification

After 24 hours, verify:

```bash
# Check backup was created (should see 2 entries)
meept backup list

# Verify sync working (multi-machine only)
meept sync status

# Verify daemon healthy
meept status
```

## Rollback Procedure

If migration fails or causes issues, rollback:

### Immediate Rollback (Before Starting Daemon)

```bash
cd ~/.meept/

# Remove new DBs
rm local.db sync-gossip.db

# Restore original DB
mv brain.db.bak brain.db

# Restore old config (if backup exists)
# cp ~/.meept.bak/meept.json5 ~/.meept/meept.json5

echo "Rollback complete. Original state restored."
```

### Rollback After Starting Daemon

If you already started the daemon:

```bash
# 1. Stop daemon
meept daemon stop

# 2. Remove new DBs
cd ~/.meept/
rm local.db sync-gossip.db

# 3. Restore original DB
mv brain.db.bak brain.db

# 4. Remove backup config
# Edit ~/.meept/meept.json5, remove backup/sync blocks

# 5. Start daemon
meept daemon start

echo "Rollback complete."
```

### Data Loss Recovery

If data was lost during migration (unlikely):

```bash
# 1. Stop daemon
meept daemon stop

# 2. Restore full backup
rm -rf ~/.meept/
cp -r ~/.meept.bak ~/.meept/

# 3. Start daemon
meept daemon start

echo "Full restoration complete."
```

## Multi-Machine Migration

When migrating multiple machines to shared backup/sync:

### Machine A (First)

1. Follow Steps 1-7 above
2. Note your `node_id` (e.g., "laptop")
3. Verify backup pushed to git

### Machine B (Second)

1. Configure with **different** `node_id`:
   ```json5
   {
     backup: {
       enabled: true,
       repo_url: "git@github.com:caimlas/meept-backups.git",  // Same repo
       node_id: "desktop",  // MUST be different
     },
     sync: {
       enabled: true,
       peers: ["laptop"],  // Reference Machine A's node_id
       pull_schedule: "1h",
     }
   }
   ```

2. Run migration:
   ```bash
   meept migrate
   ```

3. Pull peer data (optional, may take time):
   ```bash
   meept sync pull
   ```

4. Verify sessions from Machine A appear:
   ```bash
   meept sessions list
   ```

### Additional Machines

Repeat Machine B steps for each additional machine, ensuring:
- Unique `node_id` per machine
- All peer `node_id`s listed in `sync.peers`
- Same `backup.repo_url` for all machines

## Cluster Migration

For cluster deployments with gossip:

### Prerequisites

- All nodes must have WireGuard installed
- UDP port 51820 open between all nodes
- Each node has unique `node_id`

### Migration Steps

1. **On each node**, follow single-node migration (Steps 1-7)

2. **Enable cluster mode** on all nodes:
   ```json5
   {
     cluster: {
       enabled: true,
       cluster_id: "prod-cluster",  // Same on all nodes
       node_id: "node-a",  // Unique per node
       gossip_port: 51820,
       require_node_signatures: true,
     }
   }
   ```

3. **Restart all nodes**:
   ```bash
   meept daemon restart
   ```

4. **Verify cluster formation**:
   ```bash
   meept cluster status
   meept cluster peers
   ```

5. **Test gossip replication**:
   - Create session on node-a
   - Verify appears on node-b within 5 seconds
   ```bash
   # On node-a
   meept chat "test message"

   # On node-b (within 5s)
   meept sessions list
   ```

## Troubleshooting

### "Disk Full" During Migration

**Symptoms**: Migration fails with "no space left on device"

**Solution**:
```bash
# Check space
df -h ~/.meept/

# Free space (remove old logs, caches)
rm -rf ~/.meept/logs/*.log
rm -rf ~/.meept/cache/*

# Or expand disk
```

### "Corrupt Database"

**Symptoms**: Migration fails with "database disk image is malformed"

**Diagnosis**:
```bash
sqlite3 ~/.meept/brain.db "PRAGMA integrity_check"
```

**Solutions**:
1. **Try dump + restore**:
   ```bash
   sqlite3 ~/.meept/brain.db ".dump" | sqlite3 ~/.meept/brain-fixed.db
   # Use brain-fixed.db as source
   ```

2. **Restore from backup** (if available):
   ```bash
   cp ~/.meept/brain.db.bak ~/.meept/brain.db
   ```

### "Git Authentication Failed"

**Symptoms**: `meept backup push` fails with "permission denied"

**Solutions**:
1. **Verify SSH key**:
   ```bash
   ssh -T git@github.com
   # Expected: "Hi caimlas! You've successfully authenticated"
   ```

2. **Add SSH key to agent**:
   ```bash
   ssh-add ~/.ssh/id_ed25519
   ```

3. **Test git push manually**:
   ```bash
   cd ~/.meept/.backup-cache
   git push
   ```

### "Sync Pull Fails"

**Symptoms**: `meept sync pull` fails with "no peers found"

**Solutions**:
1. **Verify peer config**:
   ```json5
   sync: {
     enabled: true,
     peers: ["laptop"],  // Must match other machine's node_id
   }
   ```

2. **Check other machine pushed backup**:
   ```bash
   # On other machine
   meept backup push
   ```

3. **Manual pull**:
   ```bash
   meept sync pull
   ```

### "Config Sync Not Applying"

**Symptoms**: Config changes in git repo don't apply

**Solutions**:
1. **Check status**:
   ```bash
   meept config sync status
   ```

2. **Force pull**:
   ```bash
   meept config sync pull
   ```

3. **Check daemon logs**:
   ```bash
   tail -100 ~/.meept/daemon.log | grep config
   ```

### Migration Takes Too Long

**Expected times**:
- 100 MB DB: ~5 seconds
- 1 GB DB: ~30-60 seconds
- 10 GB DB: ~5-10 minutes

**If slower**:
1. Check disk I/O: `iostat -x 1`
2. Check for locks: `lsof ~/.meept/`
3. Consider upgrading to SSD

## Post-Migration Tasks

After successful migration:

### 1. Clean Up Old Backups

```bash
# After verifying migration successful (>7 days)
rm -rf ~/.meept.bak
```

### 2. Set Up Monitoring

Add to your monitoring system:
```bash
# Backup age check (alert if >2 days old)
meept backup list | tail -1 | awk '{print $1}'

# Sync status check
meept sync status | grep "Last pull"
```

### 3. Document Configuration

Record in your runbook:
- Backup repo URL
- Node IDs for each machine
- Sync pull schedule
- Retention policy

### 4. Test Restore Procedure

Periodically test restore:
```bash
# On test machine or temp directory
meept migrate  # Fresh install
meept sync pull  # Pull from backup repo
meept sessions list  # Verify data present
```

## Related Documents

- **User guide**: `docs/configuration/backup-sync.md`
- **Architecture**: `docs/concepts/backup-sync-architecture.md`
- **Design spec**: `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`
