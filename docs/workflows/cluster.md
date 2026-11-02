# Cluster

## Overview

Decentralized cluster coordination between meept platform instances (`internal/cluster/`). Nodes form a peer-to-peer mesh that shares task queue state, agent availability, and membership via gossip plus a git-backed membership registry. Optional WireGuard tunnel provides authenticated transport between nodes.

## Problem

A single meept platform instance handles one machine. Multi-node deployments need:
- Shared task queue so any node can claim work
- Membership awareness so nodes know their peers' capabilities
- Conflict-free membership changes (nodes can join/leave without coordinator)
- Optional encrypted transport when nodes span untrusted networks

The cluster package implements all four without a central server — git acts as the source of truth for membership, gossip propagates realtime events, and the queue store shares work.

## Behavior

### Membership (`cluster.go`, `git_sync.go`)

- Each node writes its identity to `nodes/<nodeID>.json5` in a shared git repo. The `Member` struct carries: NodeID, NodeName, WireGuardPub, SigningPub (ed25519), Endpoint, Capabilities, ClusterIP, JoinedAt, LastHeartbeat, Status.
- `GitSync` commits and pulls membership changes on a configurable interval. Conflicts resolve via last-writer-wins on `LastHeartbeat` timestamp.
- `SaveMember` writes with mode 0600; the directory hierarchy is created on demand.
- Status values: `"active"`, `"inactive"`, `"leaving"`.

### Gossip (`gossip.go`, `gossip_transport.go`)

- `GossipEngine` runs periodic heartbeats and publishes events to connected peers via the message bus (`internal/bus`).
- Events are signed with the node's ed25519 private key (`SigningPub` in the member record verifies).
- Each event has a monotonic `eventID` (generated via `pkg/id`). Duplicate event IDs are dropped.
- Peer registry (`peers map[string]*PeerInfo`) tracks last-seen timestamps; stale peers are garbage-collected.
- Background goroutines: `run()` (heartbeat loop) and `retryLoop()` (re-establish failed peer connections). Both are tracked by a `sync.WaitGroup` so `Stop()` can drain cleanly.

### Transport — WireGuard (`wireguard_sync.go`)

- `WireGuardManager` provisions a WireGuard interface and adds peers from the membership registry.
- Disabled by default (`enableWireGuard=false`). When enabled, peers communicate over the encrypted tunnel; otherwise direct TCP.
- Public keys are exchanged via the git-backed membership records (no separate key exchange protocol).

### Queue Integration

- `Engine` holds a reference to `queue.Store` so cluster events can mutate the local queue view (e.g., remove a job that another node claimed).
- The gossip protocol emits "job claimed" events that other nodes apply to their local stores.

### Dispatch Signature Verification (`executor_bridge.go`)

Cross-node task injection by bare `agent_id` ends at the ExecutorBridge. Before a
received `DispatchJob` is allowed to execute on the local agent loop, the bridge
verifies its ed25519 signature against the ORIGIN NODE's signing key — the same
key registry the gossip engine uses for event signatures (no second crypto path):

- **Fail-closed by default**: a job that is unsigned, was tampered with after
  signing, or whose origin node has no registered signing key is rejected with a
  Warn (carrying node + job id) and NEVER executed. Rejected jobs also emit
  `TASK_FAIL` so the origin learns the dispatch was refused.
- **Default ON with the cluster**: `cluster.security.require_node_signatures`
  defaults to `true`. A cluster-enabled boot with no explicit security section
  requires signatures; an explicit `require_node_signatures: false` in the
  config is the only way to disable verification (opt-out preserved).
- **Coverage**: the signature covers every identity and execution-affecting job
  field (`job_id`, `origin_node`, `target_node`, `agent_id`,
  `task_description`, `required_resources`, `workspace`, `priority`,
  `created_at`) via a canonical JSON signing struct, mirroring the
  `ClusterEvent` signing shape in `pkg/models/cluster.go`.
- Non-cluster boots are untouched: verification lives inside the ExecutorBridge,
  which is only wired when `cluster.enabled=true`.

## Configuration

`Config` struct (JSON5 via `internal/config`). Key fields:
- `node_id` — local node identifier (auto-generated if empty).
- `node_name` — human-readable name.
- `endpoint` — `host:port` other nodes use to reach this one.
- `capabilities` — labels this node advertises (e.g., `"gpu"`, `"large-memory"`).
- `enable_wireguard` — toggle encrypted transport.
- `git_repo_path` — local checkout of the shared membership repo.
- `gossip_interval` — heartbeat period (default 5s).
- `git_sync_interval` — membership pull/push period (default 60s).

## Edge Cases

- **Split-brain on git sync**: two nodes update the same member record concurrently. Resolution: `LastHeartbeat` wins; older write is discarded on next pull. Members are single-writer per nodeID so this only happens if a node's clock is wrong.
- **Signing key rotation**: changing `SigningPub` invalidates events still in flight. Gossip consumers must re-fetch the member record before verifying.
- **Stale peers**: peers that miss heartbeats for `peerTimeout` (default 30s) are removed from the local registry but their membership record persists in git until they explicitly set `Status="leaving"`.
- **Goroutine shutdown**: `Engine.Stop()` cancels `stopCh`, waits for `run()` and `retryLoop()` via WaitGroup. In-flight gossip publishes complete (best-effort) before return.

## Live verification (two nodes, verified 2026-11-02)

The store-driven claim/reclaim path is covered hermetically by
`e2e/suites/cluster-reclaim` (shared queue.db, no networking). Gossip and
WireGuard need real network separation; the verified procedure:

1. **Two hosts.** Same-host loopback aliases (127.0.0.2/.3 on macOS) carry
   gossip TCP but CANNOT carry WireGuard inner traffic: both tunnel IPs are
   local to the one host, so the kernel short-circuits inner packets to the
   local stack instead of encrypting them into the tunnel. Use two machines.
2. **WireGuard.** `wg-quick up` on both ends. Pitfall: raw `wg set` (instead
   of wg-quick) installs NO routes — with /32 inner addresses the receiving
   node decrypts fine but replies leave via the default gateway and die.
   Fix: `ip route add <peer-inner>/32 dev wg0` (wg-quick does this for you).
   Verify with `ping <peer-inner>` BOTH directions and a TCP payload
   (`nc -l` on the inner IP) before starting daemons.
3. **Cluster config per node** (`$MEEPT_HOME/cluster/config.json5` — the
   path honors MEEPT_HOME via `DefaultClusterConfigPath`): distinct
   `node_id`, `gossip_listen_addr` set to the node's WireGuard inner IP
   (`10.77.0.x:<wgport+1>`), `wireguard_port` matching on both nodes (the
   transport dials `member.ClusterIP:<wireguard_port+1>`), and
   `require_node_signatures=false` OR pre-distributed signing keys.
4. **Membership**: seed both nodes' git registry repos
   (`<state_dir>/cluster/nodes/<id>.json5`) with each member's
   `cluster_ip` = WireGuard inner IP and a fresh `last_heartbeat`. Keep
   `gossip.peer_timeout` long (e.g. 168h) for static test memberships —
   `GetActiveMembers` filters on heartbeat age, and expired members are
   never dialed.
5. **Boot both daemons.** Success looks like: `gossip_transport: listening
   for peer events address=10.77.0.x:<port>`, then `event delivered
   peer=<other>` on both sides, `received event from peer ... remote=10.77.0.<y>`
   (inner IP as remote = traffic crossed the tunnel), and `cluster.status` /
   `cluster.peers` RPC showing peer_count=1 with Status=active on both.
6. **Queue claim path over the tunnel** additionally requires a SHARED
   queue store (both daemons' `queue.db_path` at one path — NFS or similar);
   without it each node sweeps only its own claims. The cross-node reclaim
   contract itself is proven hermetically in `e2e/suites/cluster-reclaim`.

---

*Documents the `internal/cluster/` package.*
