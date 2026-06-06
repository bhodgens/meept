# Distributed Cluster Configuration

This guide covers configuring Meept's distributed cluster feature, which allows multiple `meept-daemon` instances to form a peer-to-peer mesh network for distributed task execution.

## Overview

The cluster feature enables:

- **Peer-to-peer mesh network** via WireGuard
- **Distributed task queue** with SQLite-backed replication
- **Gossip protocol** for event propagation between nodes
- **Git-based membership registry** for cluster node discovery
- **Ed25519 signatures** for event authentication

## Quick Start

### Initialize a New Cluster

```bash
# Create a new cluster (first node)
meept cluster init

# This will:
# 1. Generate ed25519 signing keys
# 2. Generate WireGuard keys
# 3. Create cluster config at ~/.meept/cluster/config.json5
# 4. Optionally set up a git remote for membership registry
```

### Join an Existing Cluster

```bash
# Join with an invite key from the cluster creator
meept cluster join <invite-key>
```

### Check Cluster Status

```bash
# View cluster members and their status
meept cluster status
```

## Configuration Options

Cluster configuration is stored in `~/.meept/cluster/config.json5`:

```json5
{
  // Cluster identity
  cluster_id: "my-cluster",
  cluster_name: "Production Cluster",
  node_id: "node-001",
  node_name: "Primary Node",

  // WireGuard mesh network settings
  network: {
    wireguard_subnet: "10.200.0.0/24",
    wireguard_port: 51820,
    mesh_interface: "wg0",
  },

  // Gossip protocol settings
  gossip: {
    heartbeat_interval: "30s",
    peer_timeout: "2m",
    event_retention: "1h",
    max_retry_attempts: 3,
  },

  // Distributed queue settings
  queue: {
    default_claim_timeout: "5m",
    node_reachability_timeout: "2m",
    full_payload_replication: false,
  },

  // Git sync settings
  git: {
    sync_interval: "5m",
    heartbeat_commit: true,
    remote_url: "git@github.com:org/cluster-registry.git",
  },

  // Security settings
  security: {
    require_node_signatures: true,
    ed25519_key_rotation_days: 90,
  },
}
```

### Configuration Fields

#### Identity

| Field | Description | Default |
|-------|-------------|---------|
| `cluster_id` | Unique identifier for the cluster | Required |
| `cluster_name` | Human-readable cluster name | Required |
| `node_id` | This node's unique ID | Auto-generated |
| `node_name` | Human-readable node name | Auto-generated |

#### Network

| Field | Description | Default |
|-------|-------------|---------|
| `wireguard_subnet` | Subnet for the mesh network | `10.200.0.0/24` |
| `wireguard_port` | WireGuard listening port | `51820` |
| `mesh_interface` | WireGuard interface name | `wg0` |

#### Gossip

| Field | Description | Default |
|-------|-------------|---------|
| `heartbeat_interval` | How often to send heartbeats | `30s` |
| `peer_timeout` | Time before peer is unreachable | `2m` |
| `event_retention` | How long to keep events | `1h` |
| `max_retry_attempts` | Max retries for failed sends | `3` |

#### Queue

| Field | Description | Default |
|-------|-------------|---------|
| `default_claim_timeout` | Timeout for claimed jobs | `5m` |
| `node_reachability_timeout` | Time before node unreachable | `2m` |
| `full_payload_replication` | Replicate full payloads | `false` |

#### Git

| Field | Description | Default |
|-------|-------------|---------|
| `sync_interval` | How often to sync with remote | `5m` |
| `heartbeat_commit` | Enable heartbeat commits | `true` |
| `remote_url` | Git remote URL for registry | Required |

#### Security

| Field | Description | Default |
|-------|-------------|---------|
| `require_node_signatures` | Require signed messages | `true` |
| `ed25519_key_rotation_days` | Key rotation interval | `90` |

## CLI Commands

### `meept cluster init`

Initialize a new cluster. Creates keys and configuration.

```bash
meept cluster init --name "My Cluster" --git-remote git@github.com:org/cluster.git
```

### `meept cluster join`

Join an existing cluster with an invite key.

```bash
meept cluster join <invite-key>
```

### `meept cluster start`

Start the cluster coordination protocol.

```bash
meept cluster start
```

### `meept cluster status`

Show cluster status and member information.

```bash
meept cluster status
meept cluster status --json
```

### `meept cluster leave`

Gracefully leave the cluster.

```bash
meept cluster leave
meept cluster leave --force  # Force leave without cleanup
```

### `meept cluster keygen`

Generate new key pairs without initializing a cluster.

```bash
meept cluster keygen
```

### `meept cluster remote`

Manage git remotes for the cluster registry.

```bash
meept cluster remote add origin git@github.com:org/cluster.git
meept cluster remote remove origin
meept cluster remote list
```

## Architecture

### Membership Registry

Cluster membership is stored in a git repository with each node's information in `nodes/<node-id>.json5`:

```json5
{
  node_id: "node-001",
  node_name: "Primary Node",
  wireguard_pubkey: "XyZabc...",
  signing_pubkey: [0x01, 0x02, ...],
  endpoint: "192.168.1.42:51820",
  capabilities: ["coder", "analyst"],
  cluster_ip: "10.200.0.1",
  joined_at: "2026-06-06T12:00:00Z",
  last_heartbeat: "2026-06-06T12:30:00Z",
  status: "active",
}
```

### WireGuard Mesh

Each node gets a unique IP in the `10.200.0.0/24` subnet. The WireGuard configuration is automatically generated and applied via `wg syncconf`.

### Gossip Protocol

Events are propagated using a flood protocol:

1. Node creates event and signs with ed25519
2. Event is stored locally and sent to all peers
3. Peers verify signature, deduplicate, and forward
4. ACKs are sent back to the originating node
5. Failed sends are retried up to 3 times

### Task Queue

The distributed queue uses SQLite with the following cluster-specific columns:

- `managing_node` - Node managing this task
- `claimed_by_node` - Node that claimed the task
- `timeout_at` - When the claim expires
- `payload_full` - Full task payload for replication
- `cluster_task_id` - Cross-reference for cluster events

## Troubleshooting

### Node Cannot Connect to Peers

1. Check WireGuard interface is up: `wg show wg0`
2. Verify firewall allows UDP port 51820
3. Check peer endpoints are reachable

### Git Sync Failing

1. Verify git remote is configured: `meept cluster remote list`
2. Check git credentials are valid
3. Manually pull remote: `cd ~/.meept/cluster && git pull`

### Events Not Replicating

1. Check gossip engine is running in daemon logs
2. Verify peer timeout hasn't been reached
3. Check cluster_events table for pending events

## Security Considerations

- Ed25519 keys should be rotated every 90 days
- Git repository containing membership should be private
- WireGuard keys provide encryption at the network layer
- All cluster events are signed and verified

## Configuration via TOML

Cluster settings can also be configured in `meept.toml`:

```toml
[cluster]
enabled = true
cluster_id = "my-cluster"
cluster_name = "Production Cluster"
node_id = "node-001"

[cluster.network]
wireguard_subnet = "10.200.0.0/24"
wireguard_port = 51820
interface = "wg0"

[cluster.gossip]
heartbeat_interval = "30s"
peer_timeout = "2m"

[cluster.git]
sync_interval = "5m"
heartbeat_commit = true
remote_url = "git@github.com:org/cluster-registry.git"
```
