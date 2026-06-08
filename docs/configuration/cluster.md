# Distributed Cluster Configuration

Meept's distributed cluster feature lets multiple `meept-daemon` instances form a peer-to-peer mesh network, share a distributed task queue, and coordinate work without a central management server.

## Overview

Instead of relying on a single daemon to handle all tasks, you can join several machines into a cluster. Tasks land in a shared queue, any node can claim a task, and if one node goes offline the others pick up the work.

### How It Works

```
┌─────────────────────────────────────────────────────────────────────┐
│                         MEEPT CLUSTER ARCHITECTURE                    │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐│
│  │  Agent  Loop│  │  Cluster   │  │  Gossip     │  │  Git Sync   ││
│  │   ──────────│─▶│   Queue    │  │   Engine    │─▶│             ││
│  │             │  └─────────────┘  └─────────────┘  └──────┬──────┘│
│  └─────────────────────────────────────────────────────────┘       │
│                              │                                      │
│                    ┌─────────▼──────────┐                          │
│                    │    WireGuard Mesh   │                          │
│                    └─────────┬──────────┘                          │
│                              │                                      │
│              ┌───────────────┼───────────────┐                     │
│              │   Git Repo    │                │                     │
│              │   ──          │                │                     │
│              │ config +      │                │                     │
│              │ node registry │                │                     │
│              └───────────────┴────────────────┘                     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

Each node keeps its own local queue and SQLite store. Nodes stay in sync through two channels:

| Channel | Purpose |
|---------|---------|
| **WireGuard mesh** | Low-latency peer-to-peer gossip for events and heartbeats |
| **Git remote** | Durable membership registry and fallback sync path |

### Design Goals

- **No central server** -- coordination happens through git + peer-to-peer gossip
- **Offline-capable** -- nodes work independently and sync when reconnected
- **Single-claim guarantee** -- a task is never processed by more than one node
- **Graceful failover** -- tasks automatically reclaimed when nodes go offline
- **Full payload replication** -- every node has all task data, no fetching on demand
- **Simple operations** -- WireGuard managed via the standard `wg` binary

## Prerequisites

Before initializing a cluster, make sure each machine meets these requirements.

### WireGuard

WireGuard must be installed and loaded on every node:

```bash
# macOS (Homebrew)
brew install wireguard-tools
sudo ifconfig wg0 create

# Linux (Debian/Ubuntu)
sudo apt install wireguard-tools
sudo modprobe wireguard

# Linux (RHEL/Fedora)
sudo dnf install wireguard-tools
sudo modprobe wireguard
```

Verify it is working:

```bash
wg show
```

If the command returns `bash: wg: command not found`, install WireGuard first.

### Git SSH Access

Every node needs SSH access to the Git remote that will store the cluster registry. Test connectivity:

```bash
ssh -T git@github.com
# or
ssh -T git@gitlab.com
```

You should see a successful authentication message. If not, configure your SSH keys before proceeding.

### File Permissions

Keys are stored under `~/.meept/cluster/keys/` with `0600` permissions. Ensure your home directory is not world-readable if you handle sensitive data, and do not share these keys.

## Quick Start: `cluster init`

The `cluster init` command walks you through creating a new cluster step by step.

### Step 1: Run `cluster init`

```bash
meept cluster init
```

### Step 2: Provide Cluster Identity

You will be prompted for a name and ID. The defaults are generated from the hostname:

```
Step 1: Cluster Identity
────────────────────────────────────────────────────────────
? Cluster name: [prod-meept-cluster]
? Cluster ID: [prod-cluster-01]
```

### Step 3: Provide Git Remote URL

Enter the SSH URL of a repository that will hold the cluster state. Meept creates `cluster.json5`, `nodes/*.json5`, and a `README.md` in this repository:

```
Step 2: Git Repository
────────────────────────────────────────────────────────────
? Git remote URL: [git@github.com:org/meept-cluster.git]
? Git branch: [main]
```

The repo must already exist (or be creatable) on the remote. Meept does not create the repository itself.

### Step 4: Configure the WireGuard Network

Choose a private subnet for the cluster mesh. The default `10.200.0.0/24` gives room for 254 nodes:

```
Step 3: Network Configuration
────────────────────────────────────────────────────────────
? WireGuard subnet: [10.200.0.0/24]
? WireGuard port: [51820]
? Interface name: [wg0]
```

### Step 5: Generate Cryptographic Keys

Meept creates a WireGuard key pair and an ed25519 signing key pair, storing them in `~/.meept/cluster/keys/`:

```
Step 4: Generate Keys
────────────────────────────────────────────────────────────
✓ Generated WireGuard keypair
✓ Generated ed25519 signing keypair
✓ Keys saved to ~/.meept/cluster/keys/
```

### Step 6: Register This Node

Tell the cluster about yourself -- your node ID, display name, capabilities, and the public endpoint peers will use to reach you:

```
Step 5: Node Registration
────────────────────────────────────────────────────────────
? Node ID: [meept-home-01]
? Node name: [Home Lab Node 1]
? Capabilities: [coder, analyst, planner]
? Public endpoint: [203.0.113.42:51820]
```

Capabilities list which agent IDs this node can run (e.g. `coder`, `analyst`, `debugger`, or a model alias like `local-llm-qwen`).

### Step 7: Commit and Push

Meept writes the cluster configuration and node registry to your working tree, then commits and pushes to the remote:

```
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
```

### Step 8: Share the Join Command

After initialization completes, Meept prints a `cluster join` command that others can use:

```
🎉 Cluster initialized successfully!

Next steps:
  1. Share join command with other nodes
  2. Run 'meept cluster start' on this node
  3. Other nodes can join with 'meept cluster join <key>'

Join command for other nodes:
  meept cluster join --remote=git@github.com:org/meept-cluster.git \
                     --cluster-id=prod-cluster-01 \
                     --join-key=CLUSTER_KEY_...
```

## Joining a Cluster: `cluster join`

Once a cluster exists, add nodes by running `cluster join` on each machine.

### Step 1: Run `cluster join` with the Join Command

The join command comes from whoever initialized the cluster:

```bash
meept cluster join --remote=git@github.com:org/meept-cluster.git \
                   --cluster-id=prod-cluster-01 \
                   --join-key=CLUSTER_KEY_...
```

### Step 2: Verify and Download Cluster State

Meept connects to the Git remote, verifies the cluster signature, and downloads the current configuration:

```
Step 1: Verify Cluster Identity
────────────────────────────────────────────────────────────
✓ Cluster signature verified
✓ Downloading cluster configuration...
```

### Step 3: Generate Keys

Just like init, you get a fresh WireGuard and ed25519 key pair:

```
Step 2: Generate Node Keys
────────────────────────────────────────────────────────────
✓ Generated WireGuard keypair
✓ Generated ed25519 signing keypair
✓ Keys saved to ~/.meept/cluster/keys/
```

### Step 4: Configure Your Node

You will be prompted for your node's identity and capabilities:

```
Step 3: Node Configuration
────────────────────────────────────────────────────────────
? Node ID: [meept-home-02]
? Node name: [Home Lab Node 2]
? Capabilities: [coder, debugger]
? Private endpoint: [192.168.1.43]
```

### Step 5: WireGuard Setup

Meept writes a `wg0.conf` file with entries for every existing peer and applies it:

```
Step 4: WireGuard Configuration
────────────────────────────────────────────────────────────
✓ Writing WireGuard config to ~/.meept/cluster/wg0.conf
✓ Adding peers: meept-home-01 (10.200.0.1)
✓ Applying config via 'wg syncconf'...
```

### Step 6: Register in Git

Your node is written to `nodes/<node-id>.json5`, committed, and pushed:

```
Step 5: Register Node in Git
────────────────────────────────────────────────────────────
✓ Created nodes/meept-home-02.json5
✓ Committing registration...
✓ Pushing to remote...
```

### Step 7: Sync Cluster State

Meept downloads the event log and merges active tasks into the local queue:

```
Step 6: Sync Cluster State
────────────────────────────────────────────────────────────
✓ Downloaded cluster event log (N events)
✓ Synced task queue state (M active tasks)

🎉 Successfully joined cluster!

Cluster members:
  - meept-home-01 (active)
  - meept-home-02 (active, joining) ← You
```

## Starting Cluster Coordination: `cluster start`

After init or join, start the background coordination services:

```bash
meept cluster start
```

```
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

This starts the gossip engine, the periodic git sync loop, and enables cluster-wide queue synchronization. The daemon itself does not need a separate flag -- cluster services run alongside the normal agent loop once started.

## Managing Clusters

### `cluster status`

View the current state of members, tasks, events, and sync health:

```bash
meept cluster status
```

```
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

### `cluster leave`

Gracefully remove a node from the cluster:

```bash
meept cluster leave
```

```
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

Leaving reclaims any tasks this node was managing so other nodes can pick them up. The node's status changes to `leaving` in the git registry.

### Debug Commands

Low-level diagnostics are available under `cluster debug`:

```bash
# Show the raw cluster event log
meept cluster debug events --limit=50

# Show peer connectivity status
meept cluster debug peers

# Simulate node failure (testing only)
meept cluster debug fail-node --node=meept-home-02
```

## Configuration Reference

Meept produces two JSON5 configuration files during init or join.

### Local Node Config: `~/.meept/cluster/config.json5`

```json5
{
  // Cluster identity
  cluster_id: "prod-cluster-01",
  cluster_name: "Production Meept Cluster",

  // Git configuration
  git: {
    remote_url: "git@github.com:org/meept-cluster.git",
    branch: "main",
    sync_interval: "5m",              // How often to pull/push git state
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
    listen_port: 51821,               // TCP port for gossip traffic
    heartbeat_interval: "30s",        // Node-to-node heartbeats
    peer_timeout: "2m",               // Mark peer as unreachable after this
    max_retry_attempts: 3,            // Gossip send retries
  },

  // Queue configuration
  queue: {
    claim_timeout: "5m",              // Task reclaim timeout
    reachability_timeout: "2m",       // Manager ping timeout
    heartbeat_interval: "30s",        // Task-level heartbeats
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

### Cluster Config in Git: `cluster.json5`

This file lives in the git repository and is shared by all nodes:

```json5
{
  cluster_id: "meept-prod-cluster",
  cluster_name: "Production Meept Cluster",
  created_at: "2026-06-06T00:00:00Z",

  // Network
  network: {
    wireguard_subnet: "10.200.0.0/24",
    wireguard_port: 51820,
    mesh_interface: "wg0",
  },

  // Gossip
  gossip: {
    heartbeat_interval: "30s",
    peer_timeout: "2m",
    event_retention: "24h",           // How long to keep events in log
    max_retry_attempts: 3,
  },

  // Task queue
  queue: {
    default_claim_timeout: "5m",
    node_reachability_timeout: "2m",
    full_payload_replication: true,   // All nodes get full task payload
  },

  // Git
  git: {
    sync_interval: "5m",
    heartbeat_commit: true,           // Commit heartbeats to git
  },

  // Security
  security: {
    require_node_signatures: true,
    ed25519_key_rotation_days: 90,
  },
}
```

### Node Registry in Git: `nodes/<node-id>.json5`

Each member is registered in its own file inside the git repo:

```json5
{
  node_id: "meept-node-01",
  node_name: "Home Lab - Node 1",

  // Cryptographic keys
  wireguard_pubkey: "XyZabc123...",
  signing_pubkey: "ed25519:abc456...",

  // Network endpoint
  endpoint: "192.168.1.42:51820",

  // Capabilities (agents this node can run)
  capabilities: ["coder", "analyst", "planner", "local-llm-qwen"],

  // Cluster-subnet IP assignment
  cluster_ip: "10.200.0.1",

  // Lifecycle
  joined_at: "2026-06-06T10:00:00Z",
  last_heartbeat: "2026-06-06T12:30:00Z",
  status: "active",    // active | inactive | leaving
}
```

### Generated Files

| File | Purpose |
|------|---------|
| `~/.meept/cluster/config.json5` | Local node configuration |
| `~/.meept/cluster/keys/wg_private.key` | WireGuard private key (0600) |
| `~/.meept/cluster/keys/wg_public.key` | WireGuard public key |
| `~/.meept/cluster/keys/ed25519_private.key` | Signing private key (0600) |
| `~/.meept/cluster/keys/ed25519_public.key` | Signing public key |
| `~/.meept/cluster/git/` | Git checkout of cluster repo |
| `~/.meept/cluster/git/cluster.json5` | Global cluster config (also on remote) |
| `~/.meept/cluster/git/nodes/*.json5` | Node registry (also on remote) |
| `~/.meept/cluster/wg0.conf` | WireGuard interface config |

## Task Lifecycle

When a task enters the cluster queue, it moves through these states:

```
PENDING --> CLAIMED --> COMPLETED
              |
              v
            PAUSED          (manager temporarily unreachable)
              |
      +-------+-------+
      v               v
(manager returns)  (timeout expired)
      v               v
  CLAIMED        PENDING (someone else can claim)
```

Key behaviors:

- The **managing node** -- the node that first created or last coordinated a task -- is the authority for conflict resolution.
- If the managing node becomes unreachable, claiming nodes pause their work and wait. After the claim timeout elapses, they create a `TASK_RECLAIM` event and return the task to `PENDING`.
- With `full_payload_replication: true`, every node has the complete task payload locally. No node needs to fetch data from another node at execution time.

## Security

| Layer | Mechanism | Purpose |
|-------|-----------|---------|
| **Network** | WireGuard Curve25519 | Encrypted tunnel between nodes |
| **Event Signing** | ed25519 signatures | Verify event authenticity |
| **Git Access** | SSH keys | Authenticate to the cluster repo |

All keys are stored with `0600` permissions. ed25519 keys can be rotated automatically every 90 days (configurable via `security.ed25519_key_rotation_days`).

## Troubleshooting

### `wg: command not found`

WireGuard is not installed. Install it using the instructions in the **Prerequisites** section above, then run `sudo modprobe wireguard` (Linux) or `sudo ifconfig wg0 create` (macOS).

### Git push rejected during join

This usually means another node committed to the repo at the same time. Meept automatically rebases and retries. If it keeps failing:

1. Pull the latest state manually: `git pull --rebase`
2. Ensure no one is editing `cluster.json5` or `nodes/*.json5` in the repo simultaneously
3. Re-run `meept cluster join` with the same join command

### Node shows as `inactive` in `cluster status`

The node is not sending heartbeats or the WireGuard link is down. Check:

```bash
# Verify WireGuard interface is up
wg show

# Check that gossip is still running
# Review logs for the gossip engine
tail -100 ~/.meept/meept.log | grep "cluster"
```

If the machine is temporarily offline, other nodes will mark it inactive after the `peer_timeout` period. It will automatically return to `active` once it reconnects and syncs.

### Task stuck in `CLAIMED` state

If a node went offline while owning a task, another node should claim it after the timeout. If it remains `CLAIMED` longer than `default_claim_timeout` (default 5 minutes):

1. Run `meept cluster status` to see which node is the managing node
2. Check if that node is still online: `meept cluster debug peers`
3. If the managing node is permanently gone, manually trigger reclaim by leaving and re-joining the cluster, or contact a cluster administrator

### Gossip events backed up

If `Pending inbound events` or `Pending outbound events` shows a high count:

- Check WireGuard connectivity between affected pairs: `ping <peer-cluster-ip>`
- Review `meept cluster debug events --limit=50` to see unforwarded events
- The gossip protocol retries up to `max_retry_attempts` times (default 3) with a 5-second timeout per attempt

### Split-brain after network interruption

If the cluster experienced a network partition, the node that was the **managing node** for any disputed task is authoritative on the result. When connectivity returns, Meept reconciles via the gossip protocol and the managing node's state wins. Check `cluster status` after reconnect to verify both sides agree:

```bash
# On each node, compare status output
meept cluster status
```

If they differ, wait a few minutes for git sync and gossip to converge. Manual intervention is rarely needed.

### Corrupted git commit

This is an uncommon situation. To recover:

1. Identify a healthy peer node
2. Pull the latest cluster state from that node's git checkout: `git clone <remote-url> ~/.meept/cluster/git`
3. Re-run `meept cluster start` to reinitialize the local event log against the git state

### Key Management

If you need to rotate an ed25519 signing key:

1. Generate a new key pair: `cd ~/.meept/cluster/keys && wg gen | tee ed25519_private.key.new | wg pubkey > ed25519_public.key.new`
2. Update `~/.meept/cluster/config.json5` to point to the new private key
3. Push the updated `nodes/<node-id>.json5` with the new signing public key
4. Remove the old keys after other nodes have synced

WireGuard keys persist for the lifetime of the node and do not need rotation.
