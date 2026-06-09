# Cluster Architecture

Meept's distributed cluster feature enables multiple `meept-daemon` instances to form a peer-to-peer mesh network, sharing a distributed task queue and coordinating work without a central management server.

## Design Principles

| Principle | Description |
|-----------|-------------|
| **No central server** | Cluster coordination via git repo + peer-to-peer gossip |
| **Offline-capable** | Nodes operate independently, sync when reconnected |
| **Single-claim guarantee** | Tasks never processed by multiple nodes simultaneously |
| **Graceful failover** | Tasks automatically reclaimed when nodes go offline |
| **Full payload replication** | All nodes have complete task data, no on-demand fetching |

## Component Overview

```
                            PER-NODE DAEMON

  +-----------+  +-----------+  +-----------+  +-----------+
  |   Agent   |  |  Cluster  |  |  Gossip   |  |   Git     |
  |   Loop    |  |  Queue    |  |  Engine   |  |  Sync     |
  +-----+-----+  +-----+-----+  +-----+-----+  +-----+-----+
        |               |               |               |
        +---------------+-------+-------+---------------+
                               |
                    +----------v-----------+
                    |   Local SQLite Store  |
                    | (queue.db + cluster  |
                    |  tables)             |
                    +----------+-----------+
                               |
                    +----------v-----------+
                    |    WireGuard Mesh    |
                    |   (peer-to-peer)     |
                    +----------+-----------+
                               |
              +----------------+----------------+
              |                                 |
  +-----------v-----------+         +---------v-----------+
  |       Node B           |         |       Node C        |
  +-------------------------+         +--------------------+

              +----------v-----------+
              |     Git Repo         |
              |   (cluster config)   |
              |  - cluster.json5     |
              |  - nodes/<id>.json5  |
              +----------------------+
```

## Core Components

### Gossip Engine (`internal/cluster/gossip.go`)

Peer-to-peer event dissemination protocol. Responsible for:

- Publishing signed events to all connected peers
- Verifying ed25519 signatures on incoming events
- Deduplicating events by event ID (INSERT OR IGNORE into SQLite)
- Forwarding new events to all other peers (flood protocol)
- Maintaining a retry queue for failed broadcasts (up to 3 retries, 5s timeout each)
- Periodic heartbeats to signal liveness
- Vector clocks for causal ordering

**Event flow:**

1. Node creates a `ClusterEvent`
2. Signs with ed25519 private key
3. Persists to local `cluster_events` SQLite table
4. Floods to all known peers via TCP transport
5. Waits for ACK from each peer (bus-based)
6. Retries up to 3 times on failure

### Git Sync (`internal/cluster/git_sync.go`)

Membership synchronization via a shared git repository. Responsible for:

- Pulling remote state on startup
- Periodically polling for changes (configurable interval, default 5m)
- Committing local heartbeats and membership updates
- Providing the `MembersProvider` interface for the gossip engine to discover peer addresses
- Handling push conflicts via rebase

**Stores in git:**

| File | Purpose |
|------|---------|
| `cluster.json5` | Global cluster configuration (subnet, gossip settings, queue config) |
| `nodes/<node-id>.json5` | Per-node registry entry (public keys, endpoint, capabilities, status) |

### WireGuard Sync (`internal/cluster/wireguard_sync.go`)

Manages WireGuard interface configuration from git state. Responsible for:

- Generating `wg0.conf` from the member registry
- Applying configuration via `wg syncconf`
- Removing departed peers
- Generating WireGuard keypairs during `cluster init`

Uses the standard `wg` binary. Templates are rendered in Go using the `text/template` package.

### Cluster Queue (`internal/queue/cluster_queue.go`)

Wraps the standard queue with cluster-aware claim/timeout/reclaim logic. Responsible for:

- Claiming tasks with timeout tracking
- Reclaiming tasks from unreachable managing nodes
- Node reachability checks
- Publishing `TASK_CLAIM`, `TASK_COMPLETE`, `TASK_FAIL`, `TASK_RECLAIM` events
- Recording claim events in SQLite for audit

### Engine (`internal/cluster/engine.go`)

Central orchestrator that starts and manages all cluster components. Responsible for:

- Loading or generating ed25519 signing keys
- Deriving node ID from public key (xxhash)
- Starting git sync, gossip engine, and WireGuard manager in the correct order
- Graceful shutdown in reverse order
- Providing accessors for each sub-component

## Data Model

### ClusterEvent

Events flow through the gossip protocol and are persisted in SQLite.

| Field | Type | Description |
|-------|------|-------------|
| `event_id` | string | UUID, unique per event |
| `node_id` | string | Originating node |
| `event_type` | enum | TASK_CREATE, TASK_CLAIM, TASK_COMPLETE, TASK_FAIL, TASK_RECLAIM, TASK_PAUSE, TASK_RESUME, NODE_JOIN, NODE_LEAVE, NODE_HEARTBEAT |
| `timestamp` | int64 | Unix timestamp |
| `vector_clock` | map | Causal ordering (node_id to counter) |
| `payload` | JSON | Event-specific data |
| `signature` | bytes | ed25519 signature |

### SQLite Schema Extensions

The standard queue schema is extended with cluster columns:

```sql
-- New columns on jobs table
ALTER TABLE jobs ADD COLUMN cluster_task_id TEXT;
ALTER TABLE jobs ADD COLUMN managing_node TEXT;
ALTER TABLE jobs ADD COLUMN claimed_by_node TEXT;
ALTER TABLE jobs ADD COLUMN timeout_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN last_heartbeat_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN payload_full BLOB;
ALTER TABLE jobs ADD COLUMN is_replica INTEGER DEFAULT 0;

-- Cluster event log
CREATE TABLE IF NOT EXISTS cluster_events (
    event_id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    vector_clock TEXT NOT NULL,
    payload BLOB NOT NULL,
    signature BLOB NOT NULL,
    received_at INTEGER NOT NULL,
    synced INTEGER DEFAULT 0
);
```

## Task Lifecycle State Machine

```
PENDING
  | TASK_CLAIM
  v
CLAIMED
  | TASK_COMPLETE
  v
COMPLETED

  | TASK_PAUSE (manager unreachable)
  v
PAUSED
  | manager returns  |  timeout expires
  v                 v
CLAIMED           TASK_RECLAIM -> PENDING
```

Key rules:
- The **managing node** is authoritative for conflict resolution
- If the managing node is unreachable, claiming nodes pause work
- After the claim timeout (default 5m), tasks are reclaimed via gossip
- With `full_payload_replication: true`, every node has the complete task payload

## Bus Topics

| Topic | Direction | Purpose |
|-------|-----------|---------|
| `cluster.events` | All nodes | Gossip event dissemination |
| `cluster.heartbeat` | All nodes | Periodic liveness signals |

## RPC Handler (`internal/rpc/cluster_handler.go`)

CLI-facing cluster commands are served through the RPC handler:

| Method | Purpose |
|--------|---------|
| `cluster_init` | Initialize a new cluster |
| `cluster_join` | Join an existing cluster |
| `cluster_start` | Start cluster coordination |
| `cluster_status` | Show membership and health |
| `cluster_leave` | Gracefully leave the cluster |
| `cluster_debug_events` | Show raw event log |
| `cluster_debug_peers` | Show peer connectivity |

## Security Model

| Layer | Mechanism | Purpose |
|-------|-----------|---------|
| **Network** | WireGuard Curve25519 | Encrypted tunnel between nodes |
| **Event Signing** | ed25519 signatures | Verify event authenticity |
| **Git Access** | SSH keys | Authenticate to cluster repository |

All keys are stored in `~/.meept/cluster/keys/` with `0600` permissions. ed25519 keys can be rotated every 90 days (configurable).

## Error Handling

### Network Partitions

- Nodes continue operating with last-known state
- Gossip events are buffered and delivered on reconnection
- Managing node is authoritative for conflict resolution

### Git Conflicts

- Automatic rebase on pull
- "Last write wins" for heartbeats
- Manual intervention only for corrupted commits

### Task Conflicts

- Double-claim: managing node rejects second claim
- Failed claim: managing node reclaims after timeout
- Result conflict: managing node's result wins

## Configuration Reference

See [Cluster Configuration](../configuration/cluster.md) for the user guide and full configuration reference.

## Package Layout

```
internal/cluster/
  cluster.go          # Member model, load/save
  config.go           # Cluster configuration types
  engine.go           # Central cluster orchestrator
  gossip.go           # Gossip flood protocol
  gossip_transport.go # TCP transport for peer delivery
  git_sync.go         # Git-based membership sync
  wireguard_sync.go   # WireGuard config management

internal/queue/
  cluster_queue.go    # Cluster-aware queue wrapper
  cluster_schema_test.go  # Schema migration tests

pkg/models/
  cluster.go          # ClusterEvent types, signing, verification
```

## Testing

The cluster subsystem has comprehensive test coverage:

- **Unit tests**: Gossip engine signing/verification, git sync, WireGuard config generation, cluster queue claim/reclaim
- **Integration tests**: Multi-node event propagation, cluster bootstrap, event persistence, queue reclaim, member registry, vector clocks
- **Schema tests**: SQLite migration verification for cluster columns and tables

Run all cluster tests:

```bash
go test ./internal/cluster/... -v
go test ./internal/queue/... -v
go test ./pkg/models/... -run TestCluster -v
```
