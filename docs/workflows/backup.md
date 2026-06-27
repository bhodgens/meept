---
title: Backup
---

# Backup

## Overview
The backup system provides automated SQLite database backup with git-based versioning and peer synchronization.

## Problem
Meept needs to preserve session data (sessions, turns, memories) across restarts and synchronize data between multiple nodes in a cluster deployment.

## Behavior
- Daily automated backups of local.db to a git repository
- Zstd compression for efficient storage
- SHA256 manifest for integrity verification
- Peer sync via git pull with INSERT OR IGNORE merge semantics
- Dual-DB architecture: local.db (unique data) + sync-gossip.db (replicated peer data)

## Configuration
See [Backup and Sync Configuration](../configuration/backup-sync.md) for detailed configuration options including:
- `[backup]` - Git backup configuration
- `[peer_sync]` - Peer synchronization settings
- `[config_sync]` - Config file synchronization
- `[cluster]` - Cluster gossip protocol settings

## Architecture
See [Backup and Sync Architecture](../concepts/backup-sync-architecture.md) for:
- Dual-DB design rationale
- Git vs gossip channels
- Merge semantics
- Failure modes and recovery

## Edge Cases
- Concurrent writes are handled by SQLite's internal concurrency
- Merge conflicts in git are resolved using INSERT OR IGNORE (idempotent)
- Peer backup files not found are skipped with logged warnings
