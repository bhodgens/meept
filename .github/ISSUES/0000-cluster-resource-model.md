# Cluster Resource Model: Cross-Daemon Task Context Gap

## Summary

When a task migrates between daemons via the gossip protocol, it loses access to its project context (working directory, project files, session bindings). This issue tracks the gap and proposes a cluster resource model to solve it.

## Problem Statement

### Current Behavior

1. **Tasks are filesystem-agnostic**: The `TaskPayload` (`pkg/models/cluster.go:125-133`) contains only:
   - `TaskID`, `AgentID`, `Description`, `Input`, `Constraints`, `Priority`, `CreatedBy`
   - **No `ProjectID`, `ProjectPath`, or `SessionID`**

2. **Each daemon has isolated state**:
   - `AgentLoop.workingDir` is set once at daemon startup from `os.Getwd()` (never updated per-task)
   - `session.ProjectPath` exists in SQLite but is daemon-local (not replicated)
   - No cross-daemon session or project replication

3. **Cross-daemon task migration breaks filesystem context**:
   - Task created on daemon-A with project `/home/user/app`
   - Task migrates to daemon-B (different machine, different `cwd`)
   - Task executes in daemon-B's startup directory, NOT `/home/user/app`

### Root Causes

1. **`workingDir` gap** (local too): `AgentLoop.workingDir` is never updated when a session switches projects via `/project set`. Artifact scanning and `AGENTS.md` loading continue to use the daemon's startup directory.

2. **No project/session in task payload**: Cluster task replication doesn't include project context.

3. **Daemon-local SQLite**: Each daemon has its own `projects.db` and `sessions.db` with no cross-replication.

## Proposed Solution: Cluster Resource Model

### Core Concept

Treat **projects**, **sessions**, and **workspaces** as **cluster-scoped resources** that can be:
- **Registered** on any daemon
- **Referenced** by tasks regardless of where they execute
- **Synced** across daemons (via git-sync gossip or optional shared storage)

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Cluster Resource Registry (replicated via git-sync)            │
│  - project.id, project.path, project.git_root                   │
│  - session.id, session.project_id, session.designation          │
│  - workspace.id, workspace.path, workspace.session_id           │
└─────────────────────────────────────────────────────────────────┘
                              │
         syncs via git + gossip
                              │
┌─────────────────────┐     ┌─────────────────────┐
│  Daemon-A           │     │  Daemon-B           │
│  - local project    │◀───▶│  - local project    │
│    cache            │     │    cache            │
│  - workingDir pool  │     │  - workingDir pool  │
│    (per-task)       │     │    (per-task)       │
└─────────────────────┘     └─────────────────────┘
```

### Key Changes

#### 1. Per-task `workingDir` (fix the local gap first)

- `AgentLoop` gains a `map[taskID]string workingDirs` field
- `TaskPayload` gains `ProjectPath string` field
- On task claim, daemon sets `workingDirs[taskID] = task.ProjectPath`
- Artifact context builder reads from per-task map, not loop-level field

#### 2. Cluster resource types

New `pkg/models/cluster_resource.go`:
```go
type ResourceType string
const (
    ResourceProject   = "project"
    ResourceSession   = "session"
    ResourceWorkspace = "workspace"
)

type ClusterResource struct {
    ID          string            `json:"id"`
    Type        ResourceType      `json:"type"`
    OwnerNodeID string            `json:"owner_node_id"`  // where it was created
    Data        json.RawMessage   `json:"data"`           // type-specific payload
    CreatedAt   int64             `json:"created_at"`
    Version     int64             `json:"version"`        // for conflict resolution
}
```

#### 3. Resource registration protocol

- When a user runs `/project <path>` on daemon-A:
  - Daemon-A upserts local `projects.db`
  - Daemon-A publishes `EventResourceRegister` via gossip
  - Other daemons receive, verify, and cache the resource metadata

- When a task is created with a project:
  - `TaskPayload.ProjectPath` is populated from the session's bound project
  - Receiving daemons check if they have the project cached
  - If not, they can lazily fetch via git-sync or return "resource unavailable"

#### 4. Resource availability semantics

- **Optimistic**: Assume all daemons can reach all project paths (shared NFS, same cloud storage)
- **Pessimistic**: Daemon rejects tasks for resources it doesn't have locally (explicit handoff required)

Default to **optimistic** for MVP, add pessimistic mode via config flag.

### Implementation Phases

#### Phase 1: Local-only `/project` command (separate issue)
- Single daemon, local recents, path typeahead
- Fix `AgentLoop.workingDir` gap (update on project switch)
- **No cluster changes**

#### Phase 2: Per-task workingDir
- `TaskPayload` gains `ProjectPath`
- `AgentLoop` uses per-task map
- Cluster tasks carry their filesystem context

#### Phase 3: Resource registry + gossip
- `ClusterResource` struct + event types
- Gossip handlers for register/update/unregister
- SQLite table `cluster_resources`

#### Phase 4: Resource-aware task scheduling
- Daemon checks "do I have this project?" before claiming
- Optional: shared storage detection, mount helpers

## Risks and Considerations

1. **Path portability**: `/home/user/app` on daemon-A may not exist on daemon-B
   - Mitigation: config file with path remapping rules (`{from, to}` pairs)

2. **Secret proliferation**: Projects may contain `.env`, credentials
   - Mitigation: resource registration includes `sensitive: bool` flag; sensitive resources never gossip, always require handoff

3. **Git-sync latency**: Gossip is fast; git-sync is slow (seconds, not milliseconds)
   - Mitigation: two-tier cache (gossip metadata + lazy git fetch on first use)

4. **Conflict resolution**: Two daemons register same project ID with different paths
   - Mitigation: vector clocks + "owner wins" rule (OwnerNodeID comparison)

## Acceptance Criteria

- [ ] Task created on daemon-A with project P executes with correct `workingDir` on daemon-B
- [ ] `/project <path>` on any daemon makes project visible to all daemons (within gossip delay)
- [ ] Daemon can reject a task claim with "resource unavailable" if it lacks the project
- [ ] No regression: local-only tasks (no project) continue to work as before

## Related Issues

- #0000 (this issue) — Cluster resource model
- #0001 — `/project` command (local-only, Phase 1)

## References

- Existing gossip: `internal/cluster/gossip.go`, `pkg/models/cluster.go`
- Existing queue: `internal/queue/cluster_queue.go`
- Session store: `internal/session/store_sqlite.go` (has `project_id`/`project_path` columns)
- AgentLoop workingDir: `internal/agent/loop.go:471` (single field, never updated per-task)
