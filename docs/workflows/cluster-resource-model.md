# Cluster Resource Model

## Overview

Cross-daemon dispatch with content-addressable file transport. When daemon A dispatches a task to daemon B, B's agent loop opens files exactly as if the work were local — same file context, same workspace state. The mechanism is invisible to the agent layer.

Spec: `docs/superpowers/specs/2026-07-01-cluster-resource-model-design.md`.

## Problem

A remote daemon working on a task had no way to access the files its task required. The cluster mesh could transport small JSON event records (task lifecycle, session turns, memory items) but could not move file content. Existing scaffolding (`TASK_*` event types, `ManagingNode`/`ClaimedByNode` columns, `FullPayloadReplication` flag) was dormant.

## Components

| Package | Role |
|---------|------|
| `internal/resources/` | CAS store + ResourceManager. Content-addressed file blobs (blake3 canonical). Transit cache, not persistent store. |
| `internal/workspace/` | WorkspaceManager. Ephemeral per-job git worktrees with diff-patch materialization. |
| `internal/placement/` | Cluster-aware scheduler. Capacity + cache-locality-aware placement. |
| `internal/cluster/grpc_transport.go` | gRPC server/client for all four services (Event/Resource/Workspace/Dispatch). |
| `internal/cluster/grpc_handlers.go` | Service handler implementations. |
| `internal/cluster/executor_bridge.go` | The only piece that knows about both cluster and agent layers. Materializes resources + workspace, invokes agent, emits result. |
| `proto/cluster.proto` | gRPC service definitions (documentation / future protoc migration). |

## CAS (Content-Addressable Store)

Layout under `~/.meept/resources/`:

```
ab/cd/<full-blake3-hash>/
├── data          # actual file bytes
└── meta.json     # {"original_name", "size", "added_at", "refcount", "pinned", "source"}
```

Index persisted via bbolt (`~/.meept/resources/index.db`) for refcounts, hash→path, added_at, source node. In-memory cache for hot lookups.

Hash algorithm: **blake3** canonical (`github.com/zeebo/blake3`). SHA-256 accepted as alternative ref syntax.

### Transit-cache semantics

- Files enter CAS only when referenced by an in-flight dispatched task. No speculative hashing, no background sweeps.
- Hashing happens at dispatch-prepare time on the sender.
- Files leave when no active task references them (refcount-driven eviction).
- Local-only files (anything never crossing the mesh) never enter CAS.
- Optional explicit pinning for expensive resources (e.g., a 10 GB model).

### Refcount and eviction

- Each CAS entry has `refcount int`. Incremented when a dispatch references it. Decremented when the dispatching task completes or fails on either side.
- Zero refcount = eligible for eviction. Eviction sweep runs every 5 min (default) OR when total store size exceeds configured cap (default 10 GB).
- Under cap pressure: lowest-refcount-eligible entries evicted first; ties broken by oldest `added_at`.
- Pinned entries (explicit user config) exempt regardless of refcount.

## Workspace Manager

Working trees live at `~/.meept/worktrees/<jobID>/`. Each job gets an ephemeral branch (`meept-job-<jobID>`) on checkout; the receiver's own working state is never touched.

`WorkspaceRef` captures source-tree state at dispatch time:
- `RepoURL` — git remote URL or `"peer:<nodeID>"` for P2P fetch.
- `CommitSHA` — pinned commit.
- `DiffBlobHash` — CAS ref to uncommitted-edit diff patch; empty if clean.
- `Dirty` — whether to apply the diff patch after checkout.

## Dispatch Lifecycle

1. **Prepare (sender):** walk `required_resources`, blake3-hash loose files, snapshot source tree → `WorkspaceRef`.
2. **Receive (target):** `DispatchService.Submit` validates signature, records job in local queue, returns `DispatchJobAck`.
3. **Materialize (target):** `ExecutorBridge.executeJob` calls `ResourceManager.Ensure` and `WorkspaceManager.Ensure` in parallel.
4. **Execute (target):** agent loop runs as if local. Files on local disk, tools work normally. No "remote mode" awareness at the agent layer.
5. **Complete (target → sender):** if workspace modified, capture diff or commit. Hash output artifacts. Emit `DispatchResult{JobID, OutputRef, WorkspaceRef}` via gRPC.
6. **Resolve (sender):** if A wants outputs locally, `ResourceManager.Ensure(resultHash)`. Apply workspace changes (fast-forward / open PR / patch) — policy decision owned by orchestrator.

## Trigger Surfaces

Three trigger surfaces, all shipped as core components:

- **(γ) CLI manual:** `meept dispatch <node> <agent> <task>` — first wire for proving the transport. Subcommands: `submit`, `status`, `results`.
- **(α) Agent-facing:** `team.assign` with `node:<nodeID>:<agentID>` prefix on `agentID` — first agent-callable surface.
- **(β) Scheduler-driven:** `internal/placement/` — capacity-aware, locality-aware placement.

## Configuration

Keys under `cluster` in `~/.meept/meept.json5`:

```json5
{
  cluster: {
    resources: {
      cas_store_dir: "~/.meept/resources",
      cas_capacity_bytes: 10737418240,   // 10 GB default
      eviction_sweep_interval: "5m",
      pinned_hashes: [],
      hash_algorithm: "blake3",
    },
    workspace: {
      worktree_root: "~/.meept/worktrees",
      git_fallback_to_peer: true,
    },
    dispatch: {
      default_claim_timeout: "5m",
      result_delivery_timeout: "1h",
      peer_fallback_policy: "if_capacity",  // always | never | if_capacity
      scheduler_no_capacity_policy: "queue", // queue | run_local
      peer_drop_cooldown: "30s",
      quarantine_period: "1h",
      quarantine_threshold: 3,
    },
  },
}
```

## Failure Modes

### Transport-layer
- gRPC stream dies mid-fetch → retry from `FetchRequest.offset`. 3 failures → try different peer.
- mTLS handshake fails → drop peer for `peer_drop_cooldown` (default 30s).
- Dead peer (keepalive PING fails 3x) → mark inactive. `ReclaimIfStale` reclaims the job.

### Resource-layer
- Hash mismatch → discard, retry same peer once, then different peer. Persistent → `ResourceCorrupt{hash, sourceNode}`.
- CAS at capacity → trigger eviction sweep. Pinned entries never evicted. Persistent → `CacheFull`.
- Concurrent fetch of same hash → both write `*.part`; last `os.Rename` wins (POSIX atomic).

### Workspace-layer
- `git fetch` from shared origin fails → fall back to peer-to-peer `WorkspaceService.GitFetch`.
- Diff patch fails to apply → fail job with `PatchConflict`.
- Receiver working tree dirty → never an issue (ephemeral worktrees).

### Security boundaries
- Peer offering blob that doesn't match requested hash → `resource_corruption` metric. After N incidents (default 3) from same peer in a window, peer quarantined for `quarantine_period` (default 1 hour).
- `WorkspaceService.GitFetch` only from registry-listed peers; unsigned node IDs rejected. mTLS + ed25519 wired in `internal/cluster/gossip.go`.
- CAS path canonicalization strict: `<algo>/<first2hex>/<next2hex>/<fullhash>/data`. Non-canonical paths rejected.

## Telemetry

Metrics emitted via `internal/cluster/metrics.go`:

- `dispatch_jobs_sent`, `dispatch_jobs_received`, `dispatch_jobs_completed`, `dispatch_jobs_failed`, `dispatch_jobs_reclaimed`
- `cas_hits`, `cas_misses`, `cas_bytes_fetched`, `cas_bytes_evicted`, `cas_refcount_zero_eligible`
- `workspace_materialize_ms`, `workspace_patch_conflicts`
- `peer_unreachable`, `peer_corruption_incidents`, `peer_quarantined`

## Module Boundaries

- `internal/resources/` — CAS only, no network.
- `internal/workspace/` — git operations + CAS-backed diffs. No agent-layer knowledge.
- `internal/cluster/grpc_transport.go` — gRPC server/client for all four services.
- `internal/cluster/executor_bridge.go` — the only piece that knows about both cluster and agent layers.
- `internal/placement/` — placement policy only. No resource/workspace/git knowledge.

## RPC Methods

Registered on the daemon's RPC server:

- `dispatch.submit` — submit a job to a target node. Payload: `target_node`, `agent_id`, `task_description`, `required_resources[]`, `workspace_ref?`, `priority?`.
- `dispatch.status` — query job status. Payload: `job_id`.
- `dispatch.results` — fetch job results. Payload: `job_id`.

## HTTP API

- `POST /api/v1/dispatch` — submit. Body mirrors `dispatch.submit` payload.
- `GET /api/v1/dispatch/{id}/status` — query status.
- `GET /api/v1/dispatch/{id}/results` — fetch results.

HTTP routes dispatch through the RPC callback (set via `http.WithRPCCall`) to avoid an invasive struct change. Routes are gated on the submitter being wired; clients get 503 when the dispatch feature is not enabled.

## TUI

`/dispatch` slash command mirrors the CLI:

- `/dispatch <node> <agent> <task>` — submit
- `/dispatch status <jobID>` — status
- `/dispatch results <jobID>` — results

## Testing

Unit tests per package (`internal/resources/`, `internal/workspace/`, `internal/placement/`, `internal/cluster/`).

Integration tests in `tests/integration/`:

- `cluster_helpers.go` — N in-process daemons with wired gRPC transports, ResourceManagers, WorkspaceManagers.
- `dispatch_round_trip_test.go` — two-daemon end-to-end dispatch including refcount cleanup.
- `cas_fetch_streaming_test.go` — blob streaming, hash verification, resume from offset.
- `workspace_dirty_round_trip_test.go` — dirty snapshot, diff blob generation.
- `failover_test.go` — job cancellation during execution, context cancellation handling.
- `cache_eviction_under_pressure_test.go` — capacity-driven eviction, refcount ordering, pinned preservation.

## Non-Goals

- **No shared filesystem layer** (NFS/Ceph/sshfs/CRDT sync). Rejected; violates offline autonomy.
- **No central object store** (MinIO/S3). Violates "no secondary storage system" constraint.
- **No client→daemon gRPC migration.** Out of scope; tracked in issue #17.
- **No continuous cross-daemon workspace sync.** Dispatch is transactional. Live collaboration is issue #18.
