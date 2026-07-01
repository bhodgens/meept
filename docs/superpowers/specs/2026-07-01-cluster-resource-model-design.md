# Cluster Resource Model: gRPC Mesh + CAS + Cross-Daemon Dispatch

**Status:** Approved (brainstorm 2026-07-01)
**Author:** caimlas
**Related issues:** #17 (gRPC client migration revisit), #18 (cross-daemon pair-programming)
**Predecessors:**
- `docs/superpowers/specs/2026-06-06-distributed-cluster-design.md` (cluster topology, gossip, WireGuard)
- `docs/superpowers/specs/2026-06-30-project-command-design.md` (project registry, `fs_namespace_mismatch` flag for remote daemons)

## 1. Problem Statement

A remote daemon working on a task currently has no way to access the files its task requires. The cluster mesh can transport small JSON event records (task lifecycle, session turns, memory items) but cannot move file content. Existing scaffolding (`TASK_*` event types, `ManagingNode`/`ClaimedByNode` columns, `FullPayloadReplication` flag) is dormant: `internal/cluster/gossip_handler.go:83` silently drops all `TASK_*` events into the `default` case, and no production caller emits `TASK_CREATE`.

The goal: when daemon A dispatches a task to daemon B, B's agent loop opens files exactly as if the work were local — same file context, same workspace state. Mechanism must remain invisible to the agent layer.

## 2. Design Decisions (Locked)

### 2.1 gRPC migration scope: option C

gRPC replaces the cluster mesh transport only. The local client→daemon Unix-socket JSON-RPC stays unchanged.

**Justification:** The only protocol-level features unique to gRPC that this codebase exercises are HTTP/2 per-stream flow control and bidirectional streaming, and only the CAS `FetchResource` call genuinely needs them. The local client→daemon path never streams and never crosses WireGuard; gRPC's wins there are marginal. Full migration (option B) is tracked in issue #17.

### 2.2 Mutability model: hybrid CAS + WorkspaceManager

- **CAS (immutable blobs).** `blake3:` / `sha256:` content-addressed files: models, datasets, prompts, build artifacts, and uncommitted-state diff patches.
- **WorkspaceManager (mutable source trees).** Dispatch carries a `WorkspaceRef` containing `{RepoURL, CommitSHA, DiffBlobHash, Dirty}`. Receiver fetches commit via shared git origin or peer-to-peer gRPC `GitFetch`, applies the diff-patch blob if the tree was dirty, returns a local working-tree path.

### 2.3 Dispatch trigger surfaces: all three, no deferrals

- **(γ) CLI manual:** `meept dispatch <node> <agent> <task>` — first wire for proving the transport.
- **(α) Agent-facing:** `team.assign` with `node:` prefix on `agentID` — first agent-callable surface.
- **(β) Scheduler-driven:** `internal/placement/` (new package) — capacity-aware, locality-aware placement. Absorbs the cross-node placement logic that is currently dormant in `internal/scheduler/` (local single-daemon scheduling stays in `internal/scheduler/`; cross-node placement is the new module's responsibility).

All three ship as core components in the implementation plan. No "deferred" follow-ups within this feature.

### 2.4 CAS semantics: transit cache, not persistent store

- Files enter CAS only when referenced by an in-flight dispatched task. No speculative hashing, no background sweeps.
- Hashing happens at dispatch-prepare time on the sender.
- Files leave when no active task references them (refcount-driven eviction).
- Local-only files (anything never crossing the mesh) never enter CAS.
- Optional explicit pinning for expensive resources (e.g. a 10 GB model). Default is unpinned; pinning is a manual operator choice.

### 2.5 Failure-mode defaults

- `peer_fallback_policy: if_capacity` — `team.assign` with unreachable peer falls back to local execution only if local capacity exists.
- `scheduler_no_capacity_policy: queue` — scheduler queues for later retry when no suitable peer is found.

## 3. Architecture Overview

### 3.1 New components

```
internal/resources/        # CAS store + ResourceManager
internal/workspace/        # WorkspaceManager (git + diff patches)
internal/placement/        # Cluster-aware scheduler (replaces dormant scheduler logic)
internal/cluster/
    grpc_transport.go      # gRPC server/client (replaces gossip_transport.go)
    executor_bridge.go     # TASK_CREATE handler → materialize → run agent → emit result
proto/cluster.proto        # gRPC service definitions
```

### 3.2 Replaced / extended components

| File | Change |
|------|--------|
| `internal/cluster/gossip_transport.go` | Superseded by `grpc_transport.go`. Existing engine (`gossip.go`) stays; only the wire transport swaps. |
| `internal/cluster/gossip_handler.go:72-85` | Add `case models.EventTaskCreate: return b.executorBridge.HandleTaskCreate(event)`. Currently hits `default` and drops. |
| `internal/daemon/components.go` | New fields `ResourceManager`, `WorkspaceManager`, `ExecutorBridge`, `GRPCTransport`, `PlacementScheduler`. New `wireClusterResources()` function. |
| `internal/daemon/components.go:4192` | `AssignTask` callback detects `node:` prefix on `agentID`, routes to `peerDispatch()`. |
| `internal/rpc/` | New `dispatch.go` with `dispatch.submit`, `dispatch.status`, `dispatch.results` RPC methods. |
| `cmd/meept/` | New `dispatch.go` with `meept dispatch <node> <agent> <task>`, `status`, `results` subcommands. |
| `internal/tui/command_handler.go` | New `/dispatch` slash command (parity with CLI). |
| `internal/comm/http/api_handlers.go` | `POST /api/v1/dispatch`, `GET /api/v1/dispatch/{id}/status`, `GET /api/v1/dispatch/{id}/results`. HTTP→RPC bridge pattern. |

### 3.3 Module boundaries

- `internal/resources/` — CAS only, no network.
- `internal/workspace/` — git operations + CAS-backed diffs. No agent-layer knowledge.
- `internal/cluster/grpc_transport.go` — gRPC server/client for all four services.
- `internal/cluster/executor_bridge.go` — the only piece that knows about both cluster and agent layers.
- `internal/placement/` — placement policy only. No resource/workspace/git knowledge.

## 4. Component Interfaces

### 4.1 ResourceManager (`internal/resources/`)

```go
// ResourceRef is the union type for resource identifiers.
// ResourceManager.Ensure sniffs the prefix to route.
type ResourceRef struct {
    Raw string  // "blake3:...", "sha256:...", "gitcommit:...", "workspace:..."
}

type ResourceManager interface {
    // Ensure resolves a ref to a local filesystem path. Fetches over the
    // cluster mesh if absent locally. Refcount-aware: increment on fetch,
    // caller must call Release when done.
    Ensure(ctx context.Context, ref ResourceRef) (path string, err error)
    Release(ref ResourceRef)

    // Add registers a local file in the CAS store, returning its hash.
    // Called by the dispatcher at send-time; never speculatively.
    Add(ctx context.Context, srcPath string) (hash string, err error)

    // Has returns true if the blob is currently in the local store.
    // Backs the ResourceService.Has gRPC method.
    Has(hash string) bool
}
```

CASStore layout:

```
~/.meept/resources/
├── ab/
│   └── cd/
│       └── <full-blake3-hash>/
│           ├── data          # actual file bytes
│           └── meta.json     # {"original_name": "...", "size": N, "added_at": "...",
│                            #  "refcount": N, "pinned": false, "source": "local|peer-xxx"}
```

Index persisted via bbolt (`~/.meept/resources/index.db`) for refcounts, hash → path, added_at, source node. In-memory cache for hot lookups.

Hash algorithm: **blake3** canonical (`github.com/zeebo/blake3`). SHA-256 accepted as alternative ref syntax but blake3 is the default for new Adds.

### 4.2 WorkspaceManager (`internal/workspace/`)

```go
// WorkspaceRef captures source-tree state at dispatch time.
type WorkspaceRef struct {
    RepoURL      string // git remote URL or "peer:<nodeID>" for P2P fetch
    CommitSHA    string
    DiffBlobHash string // CAS ref to uncommitted-edit diff patch; empty if clean
    Dirty        bool
}

type WorkspaceManager interface {
    // Ensure materializes the workspace: clone or fetch, checkout, apply patch.
    // Returns the local working-tree path. Agent cwd will be set to this.
    Ensure(ctx context.Context, ref WorkspaceRef) (worktreePath string, err error)

    // Snapshot captures current state of a local repo as a WorkspaceRef.
    // Called by the dispatcher on the sending side before dispatch.
    Snapshot(ctx context.Context, repoPath string) (WorkspaceRef, error)

    // Close removes ephemeral worktrees after task completion.
    // Commit/promotion is orchestrator policy, not the manager's.
    Close(worktreePath string) error
}
```

Working trees live at `~/.meept/worktrees/<jobID>/`. Each job gets an ephemeral branch (`meept-job-<jobID>`) on checkout; the receiver's own working state is never touched.

### 4.3 gRPC services (`proto/cluster.proto`)

```
service EventService {
  rpc Publish(ClusterEvent) returns (Ack);
  rpc Broadcast(stream ClusterEvent) returns (stream Ack);
}

service ResourceService {
  rpc Has(HasRequest) returns (HasResponse);
  rpc Fetch(FetchRequest) returns (stream FetchChunk);
  rpc Stat(StatRequest) returns (StatResponse);
}

service WorkspaceService {
  rpc Prepare(WorkspaceRef) returns (WorkspaceReady);
  rpc GitFetch(stream GitObjectRequest) returns (stream GitObject);
}

service DispatchService {
  rpc Submit(DispatchJob) returns (DispatchJobAck);
  rpc Status(JobID) returns (JobStatus);
  rpc Results(JobID) returns (stream DispatchResult);
}
```

`FetchRequest.offset` enables resumable transfers. Chunk size 1–4 MiB. mTLS over WireGuard for transport security (WireGuard provides confidentiality; mTLS provides node identity).

### 4.4 ExecutorBridge (`internal/cluster/executor_bridge.go`)

```go
type ExecutorBridge struct {
    queue       queue.Queue
    resources   resources.ResourceManager
    workspaces  workspace.WorkspaceManager
    agentLoop   *agent.AgentLoop
    localNodeID string
    bus         *bus.MessageBus
}

func (b *ExecutorBridge) HandleTaskCreate(event *models.ClusterEvent) error
func (b *ExecutorBridge) executeJob(ctx context.Context, job DispatchJob) error
func (b *ExecutorBridge) completeJob(jobID, outputRef, workspaceRef string)
func (b *ExecutorBridge) failJob(jobID string, err error)
```

Subscribes to `TASK_CREATE` events from the local gossip handler. Materializes resources + workspace, invokes the agent loop, emits `TASK_COMPLETE` / `TASK_FAIL` back into gossip.

### 4.5 PlacementScheduler (`internal/placement/`)

Cluster-aware scheduler. Consumes heartbeat metadata (node capacity, cached hashes from bloom-filter advertisement), emits placement decisions. Honors `preferred_node` hints. Applies `scheduler_no_capacity_policy` (`queue` default) when no peer is suitable.

## 5. Dispatch Lifecycle (End-to-End)

### Phase 1 — Prepare (sender, daemon A)

1. Dispatcher resolves target daemon + agent role.
2. Walks `required_resources`:
   - Loose file → blake3-hash on demand, add to local CAS with refcount=1.
   - Source tree → `WorkspaceManager.Snapshot(repoPath)` produces `WorkspaceRef` (commit SHA + optional diff blob).
3. Emits `DispatchJob` via gRPC `DispatchService.Submit`. Envelope: task description, target agent, `required_resources` (hashes/refs, never contents), `WorkspaceRef`, origin node ID, ed25519 signature.

### Phase 2 — Receive (target, daemon B)

1. `DispatchService.Submit` handler validates signature.
2. Records job in local queue (`ClaimedByNode = local`).
3. Returns `DispatchJobAck{JobID, Accepted}`.

### Phase 3 — Materialize (daemon B)

`ExecutorBridge.executeJob` calls, in parallel:

1. `ResourceManager.Ensure` for each `required_resources` entry:
   - Hit → use local path.
   - Miss → broadcast `ResourceService.Has(hash)`, pick responder, stream via `Fetch`, verify hash, add to CAS with refcount=1.
2. `WorkspaceManager.Ensure(WorkspaceRef)`:
   - Clone or fetch commit (shared origin first, peer `GitFetch` second).
   - Check out ephemeral branch.
   - Apply diff patch if Dirty.
3. Set agent's working directory to the materialized path.

### Phase 4 — Execute (daemon B)

Agent loop runs as if local. Files on local disk, tools work normally. No "remote mode" awareness at the agent layer.

### Phase 5 — Complete (daemon B → daemon A)

1. If workspace modified, commit (or capture diff from original). Produce new `WorkspaceRef`.
2. Hash any output artifacts referenced by completion report; add to CAS with refcount=1.
3. Emit `DispatchResult{JobID, OutputRef, WorkspaceRef}` via gRPC.
4. Decrement refcounts on this job's resources. Zero → eligible for eviction.

### Phase 6 — Resolve (daemon A)

1. If A wants outputs locally, `ResourceManager.Ensure(resultHash)` — same fetch path as Phase 3.
2. Apply workspace changes (fast-forward / open PR / patch). **Policy decision owned by orchestrator**, not WorkspaceManager.
3. Decrement refcounts on this dispatch's resources.

## 6. Failure Modes and Recovery

### Transport-layer

| Failure | Recovery |
|---------|----------|
| gRPC stream dies mid-fetch | Retry from `FetchRequest.offset`. 3 failures → try different peer. None available → `ResourceUnavailable`. |
| mTLS handshake fails | Drop peer from rotation for `peer_drop_cooldown` (default 30s). |
| Dead peer (keepalive PING fails 3x) | Mark inactive. `ReclaimIfStale` (`internal/queue/cluster_queue.go:225`) reclaims the job. |

### Resource-layer

| Failure | Recovery |
|---------|----------|
| Hash mismatch on receive | Discard, retry same peer once, then different peer. Persistent mismatch → `ResourceCorrupt{hash, sourceNode}`. |
| CAS at capacity on add | Trigger eviction sweep (lowest-refcount-eligible first). Retry. Pinned entries never evicted. Persistent → `CacheFull`. |
| Concurrent fetch of same hash | Both write `*.part`; last `os.Rename` wins (POSIX atomic). Loser's bytes unlinked. No correctness issue. |

### Workspace-layer

| Failure | Recovery |
|---------|----------|
| `git fetch` from shared origin fails | Fall back to peer-to-peer `WorkspaceService.GitFetch`. No peer has commit → `WorkspaceUnavailable{commit}`. |
| Diff patch fails to apply | Fail job with `PatchConflict`. Dispatcher may retry with fresh materialization. |
| Receiver working tree dirty | Never an issue — each job uses ephemeral worktree under `~/.meept/worktrees/<jobID>/`. |

### Executor-bridge

| Failure | Recovery |
|---------|----------|
| Agent loop panics | Recover, log stack, emit `TASK_FAIL` with panic info. |
| Job context cancelled | Kill agent, clean worktree, decrement refcounts. Idempotent. |
| Result emission fails (sender offline) | Queue locally for `result_delivery_timeout` (default 1 hour). Sender doesn't reconnect → drop. Job stays `completed` locally. |

### Dispatch-trigger-specific

| Trigger | Failure path |
|---------|--------------|
| CLI `dispatch` unknown node | Fail with `UnknownNode` immediately. No network call. |
| `team.assign` peer unreachable | `peer_fallback_policy: if_capacity` default — fall back to local execution if local capacity exists. Configurable: `always` / `never` / `if_capacity`. |
| Scheduler no peer suitable | `scheduler_no_capacity_policy: queue` default — queue for retry. Alternative: `run_local`. |

### Security boundaries

- Peer offering blob that doesn't match requested hash → `resource_corruption` metric. After N incidents (default 3) from same peer in a window, peer quarantined for `quarantine_period` (default 1 hour). Operator notified.
- `WorkspaceService.GitFetch` only from registry-listed peers; unsigned node IDs rejected. mTLS + ed25519 already wired in `internal/cluster/gossip.go`.
- CAS path canonicalization strict: `<algo>/<first2hex>/<next2hex>/<fullhash>/data`. Non-canonical paths rejected. No user-controlled components.

## 7. Refcount and Eviction Semantics

- Each CAS entry has `refcount int`. Incremented when a dispatch references it (Phase 1 sender-side, Phase 5 outputs). Decremented when the dispatching task completes or fails on either side.
- **Zero refcount = eligible for eviction.** Eviction sweep runs periodically (default every 5 min) or when total store size exceeds configured cap (default 10 GB).
- Under cap pressure: lowest-refcount-eligible entries evicted first; ties broken by oldest `added_at`.
- Pinned entries (explicit user config) exempt regardless of refcount.
- Local-only files never enter CAS — no refcount, no eviction logic applies.

## 8. Telemetry

All metrics emitted via existing `internal/cluster/metrics.go`:

- `dispatch_jobs_sent`, `dispatch_jobs_received`, `dispatch_jobs_completed`, `dispatch_jobs_failed`, `dispatch_jobs_reclaimed`
- `cas_hits`, `cas_misses`, `cas_bytes_fetched`, `cas_bytes_evicted`, `cas_refcount_zero_eligible`
- `workspace_materialize_ms`, `workspace_patch_conflicts`
- `peer_unreachable`, `peer_corruption_incidents`, `peer_quarantined`

## 9. Configuration

New keys in `~/.meept/meept.json5` under `cluster.resources`:

```json5
{
  cluster: {
    resources: {
      cas_store_dir: "~/.meept/resources",
      cas_capacity_bytes: 10737418240,   // 10 GB default
      eviction_sweep_interval: "5m",
      pinned_hashes: [],                 // explicit operator pins
      hash_algorithm: "blake3",          // canonical
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

## 10. Testing Strategy

### Unit (per package, every commit)

- `internal/resources/cas_store_test.go` — Add/Has/Ensure/Release/evict cycle. Hash mismatch. Refcount semantics. Concurrent Add. Cap-driven eviction order.
- `internal/resources/manager_test.go` — Ref-prefix routing. `ResourceUnavailable` propagation. Telemetry counters.
- `internal/workspace/manager_test.go` — Snapshot clean vs. dirty. Ensure with patch. PatchConflict. Close removes worktree. Idempotency.
- `internal/cluster/grpc_transport_test.go` — All four services. Mock peers. Streaming backpressure. mTLS handshake.
- `internal/cluster/executor_bridge_test.go` — HandleTaskCreate lifecycle. Failure paths. Refcount decrement.
- `internal/placement/scheduler_test.go` — Cache-locality preference. Capacity awareness. Hint honoring. Policy fallbacks.
- `internal/rpc/dispatch_test.go` — Submit/status/results. Error paths.

### Integration (`tests/integration/`)

- `dispatch_round_trip_test.go` — Two-daemon in-process setup. CLI dispatch. End-to-end including refcount cleanup.
- `cas_fetch_streaming_test.go` — 100 MB blob. Hash verification. Resume from offset.
- `workspace_dirty_round_trip_test.go` — Dirty snapshot, patch materialization, edit, ship-back.
- `failover_test.go` — Three daemons. Kill mid-job. Reclaim. Redispatch.
- `cache_eviction_under_pressure_test.go` — Small cap. Multi-job. Eviction order. Pinned preservation.

### Cluster test fixtures

`tests/integration/cluster_helpers.go` (new) — N in-process daemons with wired gRPC transports, ResourceManagers, WorkspaceManagers. In-memory cluster registry. Real git repos in temp dirs. Real files. Only network is in-process gRPC.

### Manual / smoke

Two real daemon processes (or two `~/.meept/` homes). Manual `meept dispatch`. Verify with `meept dispatch status`. Tail gossip logs. Human P0 before ship.

### Out of scope

- Geo-distributed latency tuning (single-region cluster assumed).
- Scale beyond ~10 nodes (small-cluster broadcast for `HasResource` is fine; bloom filters / DHT future work).
- Concurrent-editing collaboration — tracked in issue #18.

## 11. Explicit Non-Goals

- **No shared filesystem layer** (NFS/Ceph/sshfs/CRDT sync). Rejected in brainstorm; violates offline autonomy and "files are just files" principle.
- **No central object store** (MinIO/S3). Violates the "no secondary storage system" constraint.
- **No client→daemon gRPC migration.** Out of scope for this design; tracked in issue #17.
- **No continuous cross-daemon workspace sync.** Dispatch is transactional (snapshot → materialize → execute → result). Live collaboration is issue #18.
- **No blockchain or distributed ledger.** Cooperative cluster; trust is established via mTLS + ed25519 node signatures.

## 12. Open Items (None at Spec Time)

All decisions locked during brainstorm. Implementation plan will phase the work but will not defer any of the three trigger surfaces, the executor bridge, or the scheduler.

## 13. Traceability

| Decision | Source |
|----------|--------|
| gRPC cluster-only (option C) | Brainstorm Q1; protocol-level analysis of HTTP/2 wins |
| Hybrid CAS + WorkspaceManager (iii) | Brainstorm Q2; mutability discussion |
| All three triggers (γ → α → β), no deferrals | Brainstorm Q3′; user directive 2026-07-01 |
| CAS as transit cache, not persistent store | User directive 2026-07-01 |
| blake3 canonical hash | Grok doc recommendation; performance on large files |
| 10 GB default cap, refcount eviction | Brainstorm Section 2 |
| `peer_fallback_policy: if_capacity` | Brainstorm Section 4 |
| `scheduler_no_capacity_policy: queue` | Brainstorm Section 4 |
| `internal/placement/` for scheduler | User decision; cluster-aware signals new module |
| `executor_bridge.go` stays in `internal/cluster/` | User decision; only glue layer with agent knowledge |
