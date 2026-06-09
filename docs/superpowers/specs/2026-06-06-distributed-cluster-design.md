# Distributed Meept Cluster Design Specification

**Date:** 2026-06-06
**Status:** Draft
**Author:** Meept Team

---

## 1. Overview

This spec defines a decentralized cluster architecture allowing multiple `meept-daemon` instances to form a peer-to-peer mesh network, share a distributed task queue, and coordinate work without requiring a central management server.

### 1.1 Design Goals

| Goal | Description |
|------|-------------|
| **No central server** | Cluster coordination via git repo + peer-to-peer gossip |
| **Offline-capable** | Nodes operate independently, sync when reconnected |
| **Single-claim guarantee** | Tasks never processed by multiple nodes simultaneously |
| **Graceful failover** | Tasks automatically reclaimed when nodes go offline |
| **Full payload replication** | All nodes have complete task data |
| **Simple operations** | WireGuard managed via standard `wg` tooling |

### 1.2 Non-Goals (Explicitly Out of Scope)

| Non-Goal | Rationale |
|----------|-----------|
| Blockchain/cryptocurrency | Trusted cluster members; no adversarial threat model |
| Support for untrusted nodes | All nodes authenticated via WireGuard + ed25519 |
| Horizontal scaling beyond ~20 nodes | Gossip flood protocol sufficient for small clusters |
| Real-time sync | Eventual consistency acceptable for task queues |

---

## 2. Architecture

### 2.1 High-Level Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         MEEPT CLUSTER ARCHITECTURE                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                         PER-NODE DAEMON                               │  │
│  │                                                                       │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐ │  │
│  │  │   Agent     │  │   Cluster   │  │   Gossip    │  │   Git       │ │  │
│  │  │   Loop      │  │   Queue     │  │   Engine    │  │   Sync      │ │  │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘ │  │
│  │         │                │                │                │         │  │
│  │         └────────────────┼────────────────┼────────────────┘         │  │
│  │                          │                │                          │  │
│  │              ┌───────────▼────────────────▼────────────┐            │  │
│  │              │           Local SQLite Store            │            │  │
│  │              │    (queue.db + cluster tables)          │            │  │
│  │              └─────────────────────────────────────────┘            │  │
│  │                                                                       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                   │                                         │
│                        ┌──────────▼──────────┐                             │
│                        │    WireGuard Mesh   │                             │
│                        │   (peer-to-peer)    │                             │
│                        └──────────┬──────────┘                             │
│                                   │                                         │
│         ┌─────────────────────────┼─────────────────────────┐              │
│         │              ┌──────────▼──────────┐              │              │
│         │              │     Git Repo        │              │              │
│         │              │   (cluster config)  │              │              │
│         │              │  - cluster.json5    │              │              │
│         │              │  - nodes/<id>.json5 │              │              │
│         │              └─────────────────────┘              │              │
│         │                                                   │              │
│  ┌──────▼──────┐                                     ┌─────▼──────┐       │
│  │   Node B    │                                     │   Node C   │       │
│  └─────────────┘                                     └────────────┘       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Component Summary

| Component | Package | Purpose |
|-----------|---------|---------|
| **Gossip Engine** | `internal/cluster/gossip.go` | Peer-to-peer event dissemination |
| **Cluster Queue** | `internal/queue/cluster_queue.go` | Queue with cluster synchronization |
| **Git Sync** | `internal/cluster/git_sync.go` | Membership sync via git |
| **WireGuard Sync** | `internal/cluster/wireguard_sync.go` | WireGuard config via `wg` binary |
| **Cluster RPC Handlers** | `internal/rpc/cluster_handler.go` | CLI-facing cluster commands |

---

## 3. Data Structures

### 3.1 Cluster Configuration (Global, in Git)

Stored as `cluster.json5` in the git repo root:

```json5
{
  cluster_id: "meept-prod-cluster",
  cluster_name: "Production Meept Cluster",
  created_at: "2026-06-06T00:00:00Z",

  // Network configuration
  network: {
    wireguard_subnet: "10.200.0.0/24",
    wireguard_port: 51820,
    mesh_interface: "wg0",
  },

  // Gossip configuration
  gossip: {
    heartbeat_interval: "30s",
    peer_timeout: "2m",
    event_retention: "24h",
    max_retry_attempts: 3,
  },

  // Task queue configuration
  queue: {
    default_claim_timeout: "5m",
    node_reachability_timeout: "2m",
    full_payload_replication: true,
  },

  // Git sync configuration
  git: {
    sync_interval: "5m",
    heartbeat_commit: true,
  },

  // Security
  security: {
    require_node_signatures: true,
    ed25519_key_rotation_days: 90,
  },
}
```

### 3.2 Node Registry (Per-Node, in Git)

Stored as `nodes/<node-id>.json5`:

```json5
{
  node_id: "meept-node-01",
  node_name: "Home Lab - Node 1",

  // Cryptographic keys
  wireguard_pubkey: "XyZabc123...",
  signing_pubkey: "ed25519:abc456...",

  // Network endpoint
  endpoint: "192.168.1.42:51820",  // IP:port for WireGuard

  //Capabilities (agents this node can run)
  capabilities: ["coder", "analyst", "planner", "local-llm-qwen"],

  // Assigned IP in cluster subnet
  cluster_ip: "10.200.0.1",

  // Lifecycle
  joined_at: "2026-06-06T10:00:00Z",
  last_heartbeat: "2026-06-06T12:30:00Z",
  status: "active",  // active | inactive | leaving
}
```

### 3.3 Cluster Event Schema

```go
type ClusterEvent struct {
    EventID     string            `json:"event_id"`     // UUID
    NodeID      string            `json:"node_id"`      // Originating node
    EventType   ClusterEventType  `json:"event_type"`   // TASK_CREATE, etc.
    Timestamp   time.Time         `json:"timestamp"`    // Unix timestamp
    VectorClock map[string]int64  `json:"vector_clock"` // node_id -> counter
    Payload     json.RawMessage   `json:"payload"`      // Event-specific data
    Signature   []byte            `json:"signature"`    // ed25519 signature
}
```

### 3.4 Task Schema (SQLite Table)

Extends existing queue table with cluster fields:

```sql
ALTER TABLE jobs ADD COLUMN cluster_task_id TEXT UNIQUE;
ALTER TABLE jobs ADD COLUMN managing_node TEXT;
ALTER TABLE jobs ADD COLUMN claimed_by_node TEXT;
ALTER TABLE jobs ADD COLUMN timeout_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN last_heartbeat_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN payload_full BLOB;  -- Full task payload (JSON)
ALTER TABLE jobs ADD COLUMN is_replica INTEGER DEFAULT 0;  -- 1 if from remote node
```

### 3.5 Cluster Event Log (SQLite Table)

```sql
CREATE TABLE cluster_events (
    event_id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    timestamp INTEGER NOT NULL,  -- Unix timestamp
    vector_clock TEXT NOT NULL,  -- JSON map
    payload BLOB NOT NULL,       -- JSON
    signature BLOB NOT NULL,     -- ed25519 signature
    received_at INTEGER NOT NULL, -- When this node received it
    synced INTEGER DEFAULT 0     -- 1 if forwarded to peers
);

CREATE INDEX idx_events_type ON cluster_events(event_type);
CREATE INDEX idx_events_node ON cluster_events(node_id);
CREATE INDEX idx_events_time ON cluster_events(timestamp);
```

---

## 4. Protocol Specifications

### 4.1 Event Types

| Event Type | Payload Schema | Description |
|------------|----------------|-------------|
| `TASK_CREATE` | `{task_id, agent_id, description, input, constraints, priority, created_by}` | New task created |
| `TASK_CLAIM` | `{task_id, claimed_by, timeout_at}` | Node claims task for execution |
| `TASK_COMPLETE` | `{task_id, result, duration_ms}` | Task completed successfully |
| `TASK_FAIL` | `{task_id, error, retry_count}` | Task failed |
| `TASK_RECLAIM` | `{task_id, reason, reclaimed_by}` | Task reclaimed (timeout/crash) |
| `TASK_PAUSE` | `{task_id, reason}` | Task paused (manager unreachable) |
| `TASK_RESUME` | `{task_id}` | Task resumed (manager reconnected) |
| `NODE_JOIN` | `{node_id, endpoint, capabilities}` | New node joined cluster |
| `NODE_LEAVE` | `{node_id, reason}` | Node gracefully leaving |
| `NODE_HEARTBEAT` | `{node_id, load_avg, active_tasks}` | Periodic liveness signal |

### 4.2 Task Lifecycle State Machine

```
┌─────────────┐
│   PENDING   │ ◄──────────────────────────────────────┐
└──────┬──────┘                                        │
       │ TASK_CLAIM                                    │
       ▼                                               │
┌─────────────┐                                        │
│  CLAIMED    │ ◄─────────────┐                        │
└──────┬──────┘               │                        │
       │                      │ HEARTBEAT              │
       │                      │ (reset timeout)        │
       │                      └────────────────────────┘
       │
       ├─────────────────────────────────────┐
       │ TASK_COMPLETE                       │
       ▼                                     │
┌─────────────┐                              │
│ COMPLETED   │                              │
└─────────────┘                              │
                                             │
       ┌─────────────────────────────────────┘
       │ TASK_PAUSE (manager unreachable)
       ▼
┌─────────────┐
│   PAUSED    │
└──────┬──────┘
       │
       │ MANAGER_REACHABLE + work_needed
       ▼
(return to CLAIMED)

       │ MANAGER_UNREACHABLE + timeout_expiry
       ▼
┌─────────────┐     ┌─────────────┐
│ TASK_RECLAIM│ ──▶ │  PENDING    │ (another node can claim)
└─────────────┘     └─────────────┘
```

### 4.3 Gossip Protocol

**Event Publication:**
```
1. Node creates ClusterEvent
2. Signs event with ed25519 private key
3. Appends to local cluster_events table
4. Floods to all known peers (direct send)
5. Waits for ACK from each peer
6. Retries up to 3x on failure (5s timeout each)
7. Marks event as synced after all ACKs received
```

**Event Reception:**
```
1. Node receives event from peer
2. Verifies ed25519 signature
3. Checks for duplicate (event_id exists in local DB)
4. If new: append to local cluster_events table
5. Forward to all other peers (flood)
6. Send ACK back to sender
7. Trigger local callbacks (queue sync, etc.)
```

### 4.4 Node Reachability Protocol

**Periodic Check (every 30s):**
```
1. For each claimed task where managing_node != local_node:
   a. Send PING to managing_node over WireGuard
   b. If ACK within 10s: manager reachable, continue task
   c. If no ACK after 3 retries: manager unreachable
      i. Pause task locally
      ii. Log warning with task_id, manager node_id
      iii. Wait for manager return OR timeout expiry
```

**Timeout Expiry (5m no contact):**
```
1. Check: taskTimeoutAt < now
2. If yes: manager considered failed
3. Create TASK_RECLAIM event
4. Return task to PENDING state
5. Gossip reclaim event to cluster
```

---

## 5. CLI Commands

### 5.1 `meept cluster init`

Interactive initialization of a new cluster:

```bash
$ meept cluster init

🚀 Meept Cluster Initialization

This will set up a new distributed cluster.

Step 1: Cluster Identity
────────────────────────────────────────────────────────────
? Cluster name: [prod-meept-cluster]
? Cluster ID: [prod-cluster-01]

Step 2: Git Repository
────────────────────────────────────────────────────────────
? Git remote URL: [git@github.com:org/meept-cluster.git]
? Git branch: [main]

  This repo will store:
  - cluster.json5 (global config)
  - nodes/*.json5 (member registry)
  - README.md (cluster documentation)

? Continue? Yes

Step 3: Network Configuration
────────────────────────────────────────────────────────────
? WireGuard subnet: [10.200.0.0/24]
? WireGuard port: [51820]
? Interface name: [wg0]

Step 4: Generate Keys
────────────────────────────────────────────────────────────
✓ Generated WireGuard keypair
✓ Generated ed25519 signing keypair
✓ Keys saved to ~/.meept/cluster/keys/

Step 5: Node Registration
────────────────────────────────────────────────────────────
? Node ID: [meept-home-01]
? Node name: [Home Lab Node 1]
? Capabilities: [coder, analyst, planner, local-llm-qwen]
? Public endpoint: [203.0.113.42:51820]

Step 6: Write Configuration
────────────────────────────────────────────────────────────
✓ Created ~/.meept/cluster/config.json5
✓ Wrote cluster.json5 to git working tree
✓ Wrote nodes/meept-home-01.json5 to git working tree

Step 7: Initial Commit & Push
────────────────────────────────────────────────────────────
? Commit message: [Initialize cluster: prod-meept-cluster]
✓ Git commit created
✓ Pushing to remote...

🎉 Cluster initialized successfully!

Next steps:
  1. Share join command with other nodes
  2. Run 'meept cluster start' on this node
  3. Other nodes can join with 'meept cluster join <key>'

Join command for other nodes:
  meept cluster join --remote=git@github.com:org/meept-cluster.git \\
                     --cluster-id=prod-cluster-01 \\
                     --join-key=<generated-join-key>
```

### 5.2 `meept cluster join`

Join an existing cluster:

```bash
$ meept cluster join --remote=git@github.com:org/meept-cluster.git \
                     --cluster-id=prod-cluster-01 \
                     --join-key=CLUSTER_KEY_...

🔗 Joining Meept Cluster

Cluster: prod-meept-cluster (prod-cluster-01)
Git remote: git@github.com:org/meept-cluster.git

Step 1: Verify Cluster Identity
────────────────────────────────────────────────────────────
✓ Cluster signature verified
✓ Downloading cluster configuration...

Step 2: Generate Node Keys
────────────────────────────────────────────────────────────
✓ Generated WireGuard keypair
✓ Generated ed25519 signing keypair
✓ Keys saved to ~/.meept/cluster/keys/

Step 3: Node Configuration
────────────────────────────────────────────────────────────
? Node ID: [meept-home-02]
? Node name: [Home Lab Node 2]
? Capabilities: [coder, debugger]
? Private endpoint: [192.168.1.43]  (for internal mesh)

Step 4: Request Cluster IP Assignment
────────────────────────────────────────────────────────────
✓ Cluster admin notified of join request
⏳ Waiting for approval... (or auto-approve if enabled)

Step 5: WireGuard Configuration
────────────────────────────────────────────────────────────
✓ Writing WireGuard config to ~/.meept/cluster/wg0.conf
✓ Adding peers: meept-home-01 (10.200.0.1)
✓ Applying config via 'wg syncconf'...

Step 6: Register Node in Git
────────────────────────────────────────────────────────────
✓ Created nodes/meept-home-02.json5
✓ Committing registration...
✓ Pushing to remote...

Step 7: Sync Cluster State
────────────────────────────────────────────────────────────
✓ Downloaded cluster event log (N events)
✓ Synced task queue state (M active tasks)

🎉 Successfully joined cluster!

Cluster members:
  - meept-home-01 (active) ← You
  - meept-home-02 (active, joining)

Next steps:
  - Run 'meept cluster start' to begin processing
  - View cluster status with 'meept cluster status'
```

### 5.3 `meept cluster start`

Start cluster coordination on this node:

```bash
$ meept cluster start

✅ Starting cluster coordination...

Cluster: prod-meept-cluster
Node: meept-home-01

✓ WireGuard interface wg0 configured
✓ Gossip engine started (listening on 10.200.0.1:51821)
✓ Git sync loop started (interval: 5m)
✓ Cluster queue sync enabled

Cluster status:
  Members: 2 active
  Active tasks: 3
  This node's role: managing (2), standby (1)

🎉 Cluster coordination active
```

### 5.4 `meept cluster status`

Show cluster membership and health:

```bash
$ meept cluster status

Cluster: prod-meept-cluster
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Members:
┌──────────────────┬──────────┬───────────────────┬─────────────┐
│ Node ID          │ Status   │ Endpoint          │ Capabilities│
├──────────────────┼──────────┼───────────────────┼─────────────┤
│ meept-home-01    │ active   │ 192.168.1.42:51820│ coder,      │
│                  │          │                   │ analyst     │
│ meept-home-02    │ active   │ 192.168.1.43:51820│ coder,      │
│                  │          │                   │ debugger    │
└──────────────────┴──────────┴───────────────────┴─────────────┘

Active Tasks:
┌───────────────┬──────────────┬───────────────┬──────────────┐
│ Task ID       │ Managing Node│ Claimed By    │ Status       │
├───────────────┼──────────────┼───────────────┼──────────────┤
│ task-001      │ meept-home-01│ meept-home-01 │ CLAIMED      │
│ task-002      │ meept-home-01│ meept-home-02 │ CLAIMED      │
│ task-003      │ meept-home-02│ -             │ PENDING      │
└───────────────┴──────────────┴───────────────┴──────────────┘

Recent Events:
  - [12:30:00] TASK_CLAIM: task-002 claimed by meept-home-02
  - [12:28:00] TASK_CREATE: task-003 created by meept-home-02
  - [12:25:00] NODE_HEARTBEAT: all nodes healthy

Sync Status:
  Last git sync: 2m ago
  Last gossip sync: 5s ago
  Pending outbound events: 0
  Pending inbound events: 0
```

### 5.5 `meept cluster leave`

Gracefully leave the cluster:

```bash
$ meept cluster leave

⚠️  Leaving cluster...

This will:
  - Mark this node as 'leaving' in git registry
  - Reclaim all tasks managed by this node
  - Stop gossip and WireGuard interfaces

? Continue? Yes

✓ Notified peers of departure
✓ Reclaimed 2 managed tasks
✓ Stopped gossip engine
✓ Removed WireGuard config
✓ Updated git registry
✓ Committing changes...

👋 Successfully left cluster
```

---

## 6. Configuration Files

### 6.1 `~/.meept/cluster/config.json5` (Local Node Config)

```json5
{
  // Cluster identity
  cluster_id: "prod-cluster-01",
  cluster_name: "Production Meept Cluster",

  // Git configuration
  git: {
    remote_url: "git@github.com:org/meept-cluster.git",
    branch: "main",
    sync_interval: "5m",
    checkout_path: "~/.meept/cluster/git",
  },

  // Network configuration
  network: {
    wireguard_interface: "wg0",
    wireguard_port: 51820,
    listen_address: "0.0.0.0",
  },

  // Gossip configuration
  gossip: {
    listen_port: 51821,  // TCP port for gossip
    heartbeat_interval: "30s",
    peer_timeout: "2m",
    max_retry_attempts: 3,
  },

  // Queue configuration
  queue: {
    claim_timeout: "5m",
    reachability_timeout: "2m",
    heartbeat_interval: "30s",
  },

  // This node's identity
  node: {
    node_id: "meept-home-01",
    node_name: "Home Lab Node 1",
    capabilities: ["coder", "analyst", "planner"],
  },

  // Key paths
  keys: {
    wireguard_private_key: "~/.meept/cluster/keys/wg_private.key",
    ed25519_private_key: "~/.meept/cluster/keys/ed25519_private.key",
  },
}
```

### 6.2 Generated Files

| File | Purpose |
|------|---------|
| `~/.meept/cluster/config.json5` | Local node configuration |
| `~/.meept/cluster/keys/wg_private.key` | WireGuard private key |
| `~/.meept/cluster/keys/wg_public.key` | WireGuard public key |
| `~/.meept/cluster/keys/ed25519_private.key` | Signing private key |
| `~/.meept/cluster/keys/ed25519_public.key` | Signing public key |
| `~/.meept/cluster/git/` | Git checkout of cluster repo |
| `~/.meept/cluster/git/cluster.json5` | Global cluster config |
| `~/.meept/cluster/git/nodes/*.json5` | Node registry |
| `~/.meept/cluster/wg0.conf` | WireGuard interface config |

---

## 7. Security Considerations

### 7.1 Authentication

| Layer | Mechanism | Purpose |
|-------|-----------|---------|
| **Network** | WireGuard pre-shared keys + Curve25519 | Encrypted tunnel between nodes |
| **Event Signing** | ed25519 signatures | Verify event authenticity |
| **Git Access** | SSH keys | Authenticate to git remote |

### 7.2 Trust Model

- All cluster members are **trusted** (authenticated via WireGuard + git SSH)
- No protection against malicious insiders (out of scope)
- Future blockledger enhancement could add Byzantine fault tolerance

### 7.3 Key Management

- Keys generated during `cluster init` / `cluster join`
- Stored in `~/.meept/cluster/keys/` with 0600 permissions
- ed25519 keys should be rotated every 90 days (configurable)
- WireGuard keys persist for node lifetime

---

## 8. Error Handling

### 8.1 Network Partitions

| Scenario | Behavior |
|----------|----------|
| Node loses connectivity to git | Continues operating with last-known state; retries git sync |
| Node loses connectivity to peers | Continues processing local tasks; gossips when reconnected |
| Managing node isolated | Non-managing nodes pause tasks; reclaim after timeout |
| Split-brain (two partitions) | Each partition operates independently; merge on reconnect (managing node authoritative) |

### 8.2 Git Conflicts

| Scenario | Resolution |
|----------|------------|
| Two nodes update same file | Git rebase on pull; "last write wins" for heartbeats |
| Push rejected | Rebases local commits, retries push |
| Corrupted commit | Manual intervention required; restore from peer |

### 8.3 Task Conflicts

| Scenario | Resolution |
|----------|------------|
| Double-claim (race condition) | Managing node Rejects second claim; gossip informs claiming node |
| Claimed node goes offline | Managing node reclaims after timeout; gossips TASK_RECLAIM |
| Result conflict (partial execution) | Managing node's result is authoritative |

---

## 9. Observability

### 9.1 Logging

All cluster events logged via `slog`:

```go
logger.Info("cluster: node joined", "node_id", nodeID, "capabilities", caps)
logger.Warn("cluster: node unreachable", "node_id", nodeID, "timeout", timeout)
logger.Debug("cluster: event gossiped", "event_id", eid, "peers_notified", n)
```

### 9.2 Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `cluster.members.count` | Gauge | Current number of active members |
| `cluster.events.sent` | Counter | Events published by this node |
| `cluster.events.received` | Counter | Events received from peers |
| `cluster.tasks.managed` | Gauge | Tasks where this node is managing_node |
| `cluster.tasks.claimed` | Gauge | Tasks claimed by this node |
| `cluster.gossip.latency_ms` | Histogram | Time to gossip event to all peers |
| `cluster.git.sync_duration_ms` | Histogram | Git pull/push duration |

### 9.3 Diagnostics

```bash
# Debug: show raw cluster event log
$ meept cluster debug events --limit=50

# Debug: show peer connectivity
$ meept cluster debug peers

# Debug: simulate node failure (testing only)
$ meept cluster debug fail-node --node=meept-home-02
```

---

## 10. Future Enhancements

### 10.1 Blockledger Integration (GitHub Issue)

See §11 for detailed GitHub issue template. Summary:

- Replace gossip flood with blockchain-style append-only log
- Blocks contain batched events, linked via hash chain
- Adds: stronger audit trail, deterministic ordering, smart contract support
- Optional feature gate; backward compatible with gossip

### 10.2 Capacity-Aware Task Routing

- Nodes advertise available capacity (CPU, memory, active tasks)
- Task scheduler routes to nodes with available capacity
- Prevents overloading single nodes

### 10.3 Geographic Awareness

- Nodes tagged with region/zone metadata
- Tasks routed to nearest available node
- Compliance with data residency requirements

---

## 11. Appendix: GitHub Issue Template for Blockledger

```markdown
## Feature: Blockledger-Based Cluster Coordination

### Motivation

The current gossip protocol provides eventual consistency for cluster task coordination.
A blockledger (distributed append-only log with cryptographic ordering) would enable:

- **Stronger audit trail**: Cryptographic hash chain ensures tamper-evident event log
- **Deterministic event ordering**: No reliance on managing node authority for conflict resolution
- **Support for untrusted nodes**: Byzantine fault tolerance via consensus
- **Smart contract automation**: Task auto-reclaim, escrow-style payment, conditional execution

### Proposed Enhancement

Replace or augment the gossip replicated log with a lightweight blockledger:

- **Block structure**: Batch of cluster events (10-100 events per block)
- **Block linking**: Each block includes hash of previous block (hash chain, no PoW)
- **Consensus**: Known-node signatures (no mining; consensus via authenticated append)
- **Verification**: Nodes verify chain integrity on sync; reject invalid chains

### Implementation Considerations

1. **Backward compatibility**: Gossip protocol remains default; blockledger as feature gate
2. **Migration path**: Snapshot current event log state, start fresh blockledger from checkpoint
3. **Performance**: Block interval tuning (target: 1-10 blocks per minute)
4. **Storage**: SQLite table for block headers + event payloads

### Not Blockers for Initial Implementation

This is a future enhancement. Initial implementation uses simpler gossip flood protocol.
File this under "Cluster 2.0" or similar milestone.

### References

- [Hash chain fundamentals](https://en.wikipedia.org/wiki/Hash_chain)
- [Distributed log patterns](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html)
- [Practical Byzantine Fault Tolerance](https://www.microsoft.com/en-us/research/publication/practical-byzantine-fault-tolerance-and-proactive-recovery/)
```

---

## 12. Implementation Checklist

- [x] **Phase 1: Foundation**
  - [x] Create `internal/cluster/` package structure
  - [x] Add cluster event types to `pkg/models/cluster.go`
  - [x] Extend SQLite schema with cluster tables
  - [x] Implement Git sync (pull, push, heartbeat commits)

- [x] **Phase 2: WireGuard Integration**
  - [x] Implement `wireguard_sync.go` (Option A: `wg` binary)
  - [x] Generate WireGuard configs from git state
  - [x] Test peer connectivity

- [x] **Phase 3: Gossip Engine**
  - [x] Implement gossip flood protocol
  - [x] Event signing/verification (ed25519)
  - [x] ACK/retry logic
  - [x] Event persistence

- [x] **Phase 4: Cluster Queue**
  - [x] Implement `cluster_queue.go`
  - [x] Task claim/timeout logic
  - [x] Manager reachability checks
  - [x] Failover/reclaim logic

- [x] **Phase 5: CLI Commands**
  - [x] `meept cluster init`
  - [x] `meept cluster join`
  - [x] `meept cluster start`
  - [x] `meept cluster status`
  - [x] `meept cluster leave`
  - [x] `meept cluster debug` (diagnostics)

- [x] **Phase 6: Documentation**
  - [x] Write `docs/configuration/cluster.md` (user guide)
  - [x] Write `docs/concepts/cluster-architecture.md` (technical overview)
  - [x] Update `mkdocs.yml` navigation

- [x] **Phase 7: Testing**
  - [x] Unit tests for gossip engine
  - [x] Integration tests (multi-node cluster in Docker)
  - [x] Failover tests (simulate node crash)

---

## 13. Approval

**Spec author:** [Your name]
**Review date:** [Date]
**Approved by:** [Approver name]

---

*This document is located at `docs/superpowers/specs/2026-06-06-distributed-cluster-design.md`*
