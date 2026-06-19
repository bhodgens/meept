# Cluster

## Overview

Decentralized cluster coordination between meept daemon instances (`internal/cluster/`). Nodes form a peer-to-peer mesh that shares task queue state, agent availability, and membership via gossip plus a git-backed membership registry. Optional WireGuard tunnel provides authenticated transport between nodes.

## Problem

A single meept daemon handles one machine. Multi-node deployments need:
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

---

*Documents the `internal/cluster/` package.*
