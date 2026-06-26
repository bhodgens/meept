# Config Backup and Sync - Master Implementation Plan

**Spec:** `docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`
**Date:** 2026-06-26
**Status:** Ready for implementation

---

## Overview

This master plan coordinates the 7-phase implementation of the Config Backup and Sync system. Each phase is independently testable and builds on previous phases.

### Phase Dependencies

```
Phase 1: Git Backup Scheduler ─┬─> Phase 2: Async Pull/Sync ─┬─> Phase 3: Dual-DB Router ─┐
                               │                             │                             │
                               └─────────────────────────────┴─────────────────────────────┘
                                                                                 │
                              ┌──────────────────────────────────────────────────┘
                              │
Phase 5: Config Sync ─────────┴──> Phase 4: Gossip Event Schema ──> Phase 6: Documentation
                                                                   │
                                                                   ▼
                                                          Phase 7: Testing + Integration
```

**Critical path:** Phase 1 → Phase 3 → Phase 4 → Phase 7

### Phase Summary

| Phase | Package | Effort | Dependencies |
|-------|---------|--------|--------------|
| **Phase 1** | `internal/backup/git_backup.go` | 8-12h | None |
| **Phase 2** | `internal/backup/sync_puller.go` | 10-14h | Phase 1 |
| **Phase 3** | `internal/memory/dual_store.go` | 12-16h | None (parallel with P1) |
| **Phase 4** | `internal/cluster/gossip_handler.go` | 10-14h | Phase 3 |
| **Phase 5** | `internal/config/sync.go` | 8-12h | Phase 1 |
| **Phase 6** | Documentation | 4-6h | All implementation phases |
| **Phase 7** | Testing + Integration | 8-12h | Phases 1-5 |

**Total estimated effort:** 60-86 hours

---

## Phase 1: Git Backup Scheduler

**Plan:** `docs/superpowers/plans/2026-06-26-config-backup-sync-phase1-backup.md`

### Deliverables

- [ ] `internal/backup/git_backup.go` - Core scheduler
- [ ] `internal/backup/manifest.go` - Manifest schema
- [ ] `internal/backup/compress.go` - Zstd compression
- [ ] `internal/backup/git_ops.go` - Git operations
- [ ] `internal/config/backup_config.go` - Config schema
- [ ] `cmd/meept/backup_cmd.go` - CLI commands
- [ ] `internal/daemon/daemon.go` - Daemon wiring

### CLI Commands

```bash
meept backup list
meept backup push
meept backup push --force
```

### Acceptance Criteria

- [ ] Daily scheduled backups run automatically
- [ ] Backups compressed with zstd
- [ ] Manifest contains accurate metadata
- [ ] Retention pruning works correctly
- [ ] Git conflicts handled with rebase + retry
- [ ] All unit tests pass

---

## Phase 2: Async Pull/Sync

**Plan:** `docs/superpowers/plans/2026-06-26-config-backup-sync-phase2-sync.md`

### Deliverables

- [ ] `internal/backup/sync_puller.go` - Core puller
- [ ] `internal/backup/merge.go` - Merge logic
- [ ] `internal/backup/sync_metadata.go` - Metadata tracking
- [ ] `internal/config/sync_config.go` - Sync config schema
- [ ] `cmd/meept/sync_cmd.go` - CLI commands
- [ ] `internal/backup/temp.go` - Temp file management

### CLI Commands

```bash
meept sync pull
meept sync status
```

### Acceptance Criteria

- [ ] Hourly peer sync pulls work
- [ ] Peer data merged with INSERT OR IGNORE
- [ ] Sync metadata persisted
- [ ] Temp files cleaned up
- [ ] Corrupt backups skipped gracefully

---

## Phase 3: Dual-DB Router

**Plan:** `docs/superpowers/plans/2026-06-26-config-backup-sync-phase3-dual-db.md`

### Deliverables

- [ ] `internal/memory/dual_store.go` - Dual-store structure
- [ ] `internal/memory/schema_local.sql` - Local DB schema
- [ ] `internal/memory/schema_gossip.sql` - Gossip DB schema
- [ ] `internal/memory/migrate.go` - Migration tool
- [ ] `cmd/meept/migrate_cmd.go` - Migration CLI

### CLI Commands

```bash
meept migrate --dry-run
meept migrate
```

### Acceptance Criteria

- [ ] Dual-store created (local.db + sync-gossip.db)
- [ ] Writes routed to correct DB
- [ ] Reads merged from both DBs
- [ ] Migration works without data loss
- [ ] Existing code updated to use dual-store

---

## Phase 4: Gossip Event Schema

**Plan:** `docs/superpowers/plans/2026-06-26-config-backup-sync-phase4-gossip.md`

### Deliverables

- [ ] `pkg/models/cluster.go` - Event type extensions
- [ ] `internal/cluster/gossip_handler.go` - Receive handlers
- [ ] `internal/cluster/conflict.go` - Conflict resolution
- [ ] `internal/memory/dual_store.go` - Publish integration
- [ ] `internal/daemon/components.go` - Wiring

### Acceptance Criteria

- [ ] SESSION_TURN and MEMORY_STORED events defined
- [ ] Gossip publish integration working
- [ ] Receive handlers merge into gossip.db
- [ ] Event deduplication works
- [ ] Vector clocks maintained
- [ ] Conflict resolution implemented

---

## Phase 5: Config Sync

**Plan:** `docs/superpowers/plans/2026-06-26-config-backup-sync-phase5-config.md`

### Deliverables

- [ ] `internal/config/sync.go` - Config syncer
- [ ] `internal/config/merger.go` - Merge logic
- [ ] `internal/config/reload_hooks.go` - Hot-reload hooks
- [ ] `internal/config/git_checkout.go` - Git checkout
- [ ] `cmd/meept/config_sync_cmd.go` - CLI commands

### CLI Commands

```bash
meept config sync status
meept config sync pull
```

### Acceptance Criteria

- [ ] 5-minute scheduled pulls work
- [ ] Shared configs applied
- [ ] Per-node overrides work
- [ ] Hot-reload hooks trigger
- [ ] Git conflicts handled gracefully

---

## Phase 6: Documentation

### Deliverables

- [ ] `docs/configuration/backup-sync.md` - User guide
- [ ] `docs/concepts/backup-sync-architecture.md` - Architecture overview
- [ ] `docs/reference/cli.md` - CLI reference updates
- [ ] `docs/migration/existing-deployments.md` - Migration guide

### Documentation Outline

**User Guide (`docs/configuration/backup-sync.md`):**
- Overview of backup and sync features
- Configuration reference (all config options)
- Single-node setup guide
- Multi-machine setup guide
- Cluster deployment guide
- Troubleshooting

**Architecture (`docs/concepts/backup-sync-architecture.md`):**
- Dual-DB architecture rationale
- Git vs gossip channels
- Data ownership model
- Merge semantics
- Failure modes and recovery

**Migration Guide (`docs/migration/existing-deployments.md`):**
- Pre-migration checklist
- Step-by-step migration
- Rollback procedure
- Post-migration verification

---

## Phase 7: Testing + Integration

### Deliverables

- [ ] `tests/integration/backup_single_node_test.go`
- [ ] `tests/integration/backup_multi_machine_test.go`
- [ ] `tests/integration/backup_cluster_test.go`
- [ ] `tests/integration/sync_pull_test.go`
- [ ] `tests/integration/dual_db_test.go`
- [ ] `tests/integration/gossip_session_sync_test.go`
- [ ] `tests/integration/config_sync_test.go`

### Test Scenarios

**Single-node backup:**
1. Start daemon with backup enabled
2. Create sessions via CLI
3. Trigger `backup push`
4. Verify backup in git repo
5. Restore to fresh directory
6. Verify restored data matches original

**Multi-machine sync:**
1. Start two daemons with same backup repo
2. Create session on machine A
3. Push backup from A
4. Pull sync on machine B
5. Verify session appears on B's gossip.db
6. Create session on B, verify A receives it

**Cluster gossip:**
1. Start 3-node cluster
2. Create session on node A
3. Verify session gossips to B and C in real-time
4. Simulate network partition
5. Create sessions on both sides
6. Reconnect, verify merge succeeds

**Config sync:**
1. Start daemon with config sync enabled
2. Modify shared config in git repo
3. Wait for pull interval (or trigger manually)
4. Verify config applied without restart
5. Verify hot-reload hook triggered

---

## Implementation Order Recommendation

### Wave 1: Foundation (Week 1)
- **Phase 1** (Git Backup Scheduler) - 8-12h
- **Phase 3** (Dual-DB Router) - 12-16h (can run parallel with P1)

**Why:** These are the foundation. Phase 1 gives you working backups immediately. Phase 3 sets up the storage architecture.

### Wave 2: Sync (Week 2)
- **Phase 2** (Async Pull/Sync) - 10-14h
- **Phase 4** (Gossip Event Schema) - 10-14h

**Why:** Builds on Wave 1. Phase 2 enables multi-machine sync. Phase 4 enables real-time cluster gossip.

### Wave 3: Polish (Week 3)
- **Phase 5** (Config Sync) - 8-12h
- **Phase 6** (Documentation) - 4-6h (runs parallel)
- **Phase 7** (Testing + Integration) - 8-12h

**Why:** Config sync is valuable but not critical for initial rollout. Documentation and testing ensure quality.

---

## Configuration Matrix

| Deployment Mode | Phases Required | Features |
|-----------------|-----------------|----------|
| **Single-node + backup** | Phase 1 | Daily git backups of local state |
| **Multi-machine sync** | P1 + P2 + P3 | Backup + peer sync via git |
| **Cluster (real-time)** | P1 + P3 + P4 | Backup + real-time gossip |
| **Full deployment** | All phases | Backup + sync + gossip + config sync |

---

## Risk Register

| Risk | Severity | Mitigation |
|------|----------|------------|
| Migration corrupts user data | High | Extensive testing, dry-run mode, backup-before-migrate |
| Git conflicts during sync | Medium | Automatic rebase, clear error messages, manual resolution guide |
| Gossip flood overwhelms network | Medium | Rate limiting, batching, configurable intervals |
| Dual-DB merge logic has bugs | High | Comprehensive tests, INSERT OR IGNORE idempotency |
| Hot-reload leaves partial state | Medium | Transactional config swap, rollback on failure |

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Backup success rate | >99% (failed pushes retry successfully) |
| Sync latency (multi-machine) | <1 hour (pull interval) |
| Gossip latency (cluster) | <5 seconds (real-time) |
| Merge conflict rate | <1% of sync operations |
| Config sync latency | <10 minutes (5min pull + reload) |
| Test coverage | >90% across all phases |

---

## Post-Implementation Roadmap

### Future Enhancements (Post-Phase 7)

1. **Incremental backups** - Track changed rows, backup only deltas
2. **Cloud storage backend** - S3/GCS/restic support via abstraction layer
3. **Compression improvements** - Dictionary training, columnar format
4. **Conflict resolution UI** - Web UI for manual merge decisions
5. **Encrypted backups** - Per-node encryption keys
6. **Backup verification** - Periodic restore tests to verify backup integrity

---

## Quick Reference

### Config Examples

**Single-node with backup:**
```json5
{
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-backups.git",
    schedule: "24h",
    retention_days: 12,
  }
}
```

**Multi-machine personal sync:**
```json5
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
  }
}
```

**Cluster deployment:**
```json5
{
  cluster: {
    enabled: true,
    cluster_id: "prod-cluster-01",
    // ... cluster config
  },
  backup: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-cluster-backups.git",
    schedule: "24h",
    retention_days: 12,
  },
  config_sync: {
    enabled: true,
    repo_url: "git@github.com:caimlas/meept-cluster-config.git",
    pull_schedule: "5m",
  }
}
```

---

*This master plan coordinates 7 phases from the backup/sync design spec (`docs/superpowers/specs/2026-06-26-config-backup-sync-design.md`).*
