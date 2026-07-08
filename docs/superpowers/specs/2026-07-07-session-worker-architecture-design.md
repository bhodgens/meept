# Session Worker Pool Architecture Design

**Date:** 2026-07-07
**Author:** Claude Code (with user collaboration)
**Version:** 1.0.0
**Status:** Approved for Implementation

---

## Problem

The current Meept architecture has a **singleton AgentLoop** shared across all sessions. This creates several critical issues:

1. **No project isolation:** When Session A binds to project `/foo` and Session B binds to `/bar`, both sessions share the same `workingDir`. Switching sessions doesn't properly isolate agent execution contexts.

2. **Cannot work on multiple projects concurrently:** Users cannot have Session A working on project A while Session B works on project B. The last `project.set` call wins for all sessions.

3. **Silent project inheritance:** When resuming a session with a bound `ProjectPath`, the AgentLoop may still be running with the daemon's startup directory, causing agents to execute in the wrong project context.

4. **No graceful degradation:** Sessions without a project bound may cause agents to "spin" waiting for a working directory that never gets set.

## Root Cause Analysis

The current flow (as of 2026-07-07):

```
Daemon Startup → Create ONE AgentLoop → workingDir = os.Getwd()
                      ↓
              All sessions share this loop
                      ↓
         Session A sets project → workingDir = /foo
         Session B sets project → workingDir = /bar (overwrites A!)
```

Key files examined:
- `internal/daemon/components.go:989` — Single `c.AgentLoop = agent.NewAgentLoop(...)`
- `internal/agent/loop.go:471` — `workingDir string` field on AgentLoop
- `internal/agent/loop.go:4444-4493` — `SetWorkingDir()` and `StartProjectSub()` for bus-based updates
- `internal/agent/tactical.go:55-150` — Two-level semaphore (global:10, per-agent:3) for job concurrency

The `project.set` RPC publishes a bus event that `StartProjectSub()` listens to, but this only fires on **explicit** project binding—not on session load/resume.

---

## Goal

Enable **per-session project isolation** with efficient resource pooling:

```
Daemon → Worker Pool (configurable, default: 100 goroutines)
           ↓ multiplexes (up to 5 loops per worker)
    AgentLoop A (session: 1, workingDir: /foo)
    AgentLoop B (session: 2, workingDir: /bar)
    AgentLoop C (session: 3, workingDir: CWD)
           ↓ all route through →
    TacticalScheduler (global semaphore: 10, per-agent: 3)
```

**Key properties:**
- Each session has its own AgentLoop with isolated `workingDir`
- Workers are lazy: only run when session has active work
- Multiplexing: 1 worker goroutine can serve up to 5 idle/active AgentLoops
- Global agent concurrency stays as-is (shared pool across all sessions)

---

## Design

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         MEEPT DAEMON                             │
├─────────────────────────────────────────────────────────────────┤
│  ┌────────────────────────────────────────────────────────┐     │
│  │         Worker Pool Manager                             │     │
│  │  • maxWorkers: int (default: 100)                       │     │
│  │  • worker:agentLoop ratio (default: 1:5)                │     │
│  │  • map[workerID]*WorkerGoroutine                        │     │
│  └────────────────────────────────────────────────────────┘     │
│                            │                                      │
│                            ▼                                      │
│  ┌────────────────────────────────────────────────────────┐     │
│  │         AgentLoop Manager                               │     │
│  │  • map[sessionID]*AgentLoop                             │     │
│  │  • Assign loops to workers (round-robin, max 5:1)       │     │
│  │  • Lazy activation on message/task                      │     │
│  └────────────────────────────────────────────────────────┘     │
│                            │                                      │
│         ┌──────────────────┼──────────────────┐                   │
│         ▼                  ▼                  ▼                   │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐           │
│  │ AgentLoop A │    │ AgentLoop B │    │ AgentLoop C │           │
│  │ session: 1  │    │ session: 2  │    │ session: 3  │           │
│  │ workingDir: │    │ workingDir: │    │ workingDir: │           │
│  │ /proj/foo   │    │ /proj/bar   │    │ CWD         │           │
│  │ worker: 1   │    │ worker: 1   │    │ worker: 2   │           │
│  └─────────────┘    └─────────────┘    └─────────────┘           │
│         │                  │                  │                   │
│         └──────────────────┼──────────────────┘                   │
│                            ▼                                      │
│  ┌────────────────────────────────────────────────────────┐     │
│  │         TacticalScheduler (UNCHANGED)                   │     │
│  │  • globalSemaphore: maxConcurrentJobs (default: 10)    │     │
│  │  • agentSemaphore[agent]: maxPerAgent (default: 3)     │     │
│  │  • Routes ALL jobs from ALL sessions through pool      │     │
│  └────────────────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────────────────┘
```

### Component Changes

#### 1. Worker Pool Manager (`internal/daemon/worker_pool.go` — NEW)

```go
// WorkerPool manages a pool of worker goroutines that can execute agent work.
// Workers are lazy: they only run when assigned sessions have pending work.
type WorkerPool struct {
    mu              sync.RWMutex
    workers         []*Worker
    maxWorkers      int
    maxLoopsPerWorker int  // Default: 5
    roundRobinIndex int
}

// Worker represents a goroutine that can process agent loop work.
type Worker struct {
    id           int
    ctx          context.Context
    cancel       context.CancelFunc
    assignedLoops map[*AgentLoop]bool  // Up to maxLoopsPerWorker
    workQueue    chan WorkItem
    idleTimer    *time.Timer
}

// WorkItem represents a unit of work for a worker.
type WorkItem struct {
    Loop    *AgentLoop
    Trigger WorkTrigger  // UserMessage, TaskQueued, Timer, etc.
}
```

**Configuration:**
```json5
{
  "agent": {
    "worker_pool": {
      "max_workers": 100,        // Max worker goroutines
      "max_loops_per_worker": 5, // Multiplex ratio
      "idle_timeout_seconds": 300 // Kill idle workers after 5 min
    }
  }
}
```

#### 2. AgentLoop Manager (`internal/agent/manager.go` — NEW)

```go
// Manager manages per-session AgentLoop instances.
type Manager struct {
    mu            sync.RWMutex
    loops         map[string]*AgentLoop  // sessionID → loop
    workerPool    *WorkerPool
    sessionStore  session.Store
    projectMgr    *project.ProjectManager
}

// GetOrCreate returns existing loop or creates new one for session.
func (m *Manager) GetOrCreate(sessionID string, opts LoopInitOpts) (*AgentLoop, error)

// LoopInitOpts specifies initialization parameters.
type LoopInitOpts struct {
    WorkingDir      string  // From project path or detection context
    DetectionContext *DetectionContext
    ProjectID       string
}

// DetectionContext captures client-side context for session creation.
type DetectionContext struct {
    CWD         string `json:"cwd"`          // Where TUI/client was run
    DetectedProjectID string `json:"detected_project_id,omitempty"`
    CLIArgs     []string `json:"cli_args,omitempty"`
}
```

#### 3. AgentLoop Modifications (`internal/agent/loop.go`)

**Added fields:**
```go
type AgentLoop struct {
    // ... existing fields ...
    sessionID        string
    workingDir       string
    detectionContext *DetectionContext  // NEW: client context
    projectID        string             // NEW: bound project
    workerID         int                // NEW: assigned worker
    isActive         atomic.Bool        // NEW: has pending work
}
```

**Modified constructor:**
```go
// NewAgentLoop creates a loop with explicit workingDir (no longer defaults to os.Getwd()).
func NewAgentLoop(sessionID string, workingDir string, opts ...LoopOption) *AgentLoop
```

**Removed:**
```go
// REMOVED: StartProjectSub() — no longer needed, workingDir is set at creation
// The project.set bus event subscription is replaced by direct initialization.
```

#### 4. Session Store Schema (`internal/session/session.go`)

**Added fields:**
```go
type Session struct {
    // ... existing fields ...
    ProjectID        string                   `json:"project_id,omitempty"`
    ProjectPath      string                   `json:"project_path,omitempty"`
    DetectionContext *DetectionContext        `json:"detection_context,omitempty"`  // NEW
}
```

#### 5. Session RPC Handler (`internal/rpc/session.go`)

**Modified `session.create`:**
```go
func (h *SessionHandler) handleCreate(ctx context.Context, params json.RawMessage) (any, error) {
    var req struct {
        Name             string            `json:"name"`
        DetectionContext *DetectionContext `json:"detection_context,omitempty"`  // NEW
    }
    // ...

    // Determine workingDir:
    // 1. If detection_context.detected_project_id is set, use that project's path
    // 2. If detection_context.cwd is set, use that
    // 3. Otherwise, leave empty (will prompt on TUI load)
    workingDir := ""
    if req.DetectionContext != nil {
        if req.DetectionContext.DetectedProjectID != "" {
            proj, err := h.projectMgr.Get(ctx, req.DetectionContext.DetectedProjectID)
            if err == nil {
                workingDir = proj.LocalPath
            }
        } else if req.DetectionContext.CWD != "" {
            workingDir = req.DetectionContext.CWD
        }
    }

    session := &session.Session{
        // ...
        ProjectPath: workingDir,
        DetectionContext: req.DetectionContext,
    }
    // ...
}
```

#### 6. TUI Changes (`internal/tui/app.go`)

**On startup, send CWD:**
```go
// When creating/resuming session, include detection context
func (a *App) createOrResumeSession() tea.Msg {
    cwd, _ := os.Getwd()
    params := map[string]any{
        "name": "default",
        "detection_context": map[string]string{
            "cwd": cwd,
        },
    }
    // ... RPC call ...
}
```

**On session load with no project: prompt user**
```go
case SessionLoadedMsg:
    if msg.Session.ProjectPath == "" && msg.Session.DetectionContext == nil {
        // No project bound, no detection context → prompt user
        return a.showProjectPrompt(msg.Session)
    }
    // ...
}

// ProjectPromptMsg triggers the confirmation dialog
type ProjectPromptMsg struct {
    SessionID   string
    DefaultPath string  // From CWD if available
}
```

**Confirmation prompt modal:**
```
Session "fix-bug" has no project bound.

Use current directory for agent execution?
  /Users/caimlas/git/meept

[y] Yes  [n] No (agents run without project)  [p] Pick project
[ ] Don't ask again for this session
```

#### 7. CLI Changes (`cmd/meept/main.go`)

**Support `meept [path]` syntax:**
```go
// If first arg is a path, use as detection context
if len(os.Args) > 1 && filepath.IsAbs(os.Args[1]) {
    detectionContext.CWD = os.Args[1]
    // Auto-detect project in that path
    if proj, err := projectMgr.DetectFromPath(ctx, os.Args[1]); err == nil {
        detectionContext.DetectedProjectID = proj.ID
    }
}
```

#### 8. Flutter GUI Parity (`ui/flutter_ui/`)

**Session model update (`lib/models/api_models.dart`):**
```dart
@freezed
class Session with _$Session {
  const factory Session({
    // ... existing fields ...
    @JsonKey(name: 'project_id') String? projectId,
    @JsonKey(name: 'project_path') String? projectPath,
    @JsonKey(name: 'detection_context') DetectionContext? detectionContext,
  }) = _Session;
}

@freezed
class DetectionContext with _$DetectionContext {
  const factory DetectionContext({
    @JsonKey(name: 'cwd') String? cwd,
    @JsonKey(name: 'detected_project_id') String? detectedProjectId,
  }) = _DetectionContext;
}
```

**Project provider update (`lib/providers/project_provider.dart`):**
- Add logic to show prompt when session loads with no project
- Wire "Pick Project" dialog that calls `setProject` RPC

**Session creation (`lib/providers/session_provider.dart`):**
- Send CWD on session create (from platform)
- Handle detection context in response

---

## Implementation Phases

### Phase 1: Worker Pool Infrastructure
- Create `internal/daemon/worker_pool.go` with `WorkerPool` and `Worker` types
- Add config schema for `agent.worker_pool.*`
- Implement lazy worker activation (worker spins up on first work item)
- Implement multiplexing (1 worker serves up to N loops)

### Phase 2: AgentLoop Manager
- Create `internal/agent/manager.go` with per-session loop tracking
- Modify `internal/agent/loop.go`:
  - Add `sessionID`, `detectionContext`, `projectID` fields
  - Change `NewAgentLoop` to require explicit `workingDir`
  - Remove `StartProjectSub()` goroutine (no longer needed)
- Update `internal/daemon/components.go`:
  - Replace singleton `c.AgentLoop` with `c.AgentLoopManager`
  - Wire manager to worker pool

### Phase 3: Session Schema + Detection Context
- Update `internal/session/session.go` with `DetectionContext` struct
- Modify `internal/rpc/session.go`:
  - `handleCreate` accepts `detection_context` param
  - `handleGetMostRecent` returns session with project path
- Add project binding on session load (re-publish `project.set` event if session has project)

### Phase 4: TUI Integration
- Send CWD on session create in `internal/tui/app.go`
- Add `ProjectPromptMsg` and modal for "no project" case
- Add CLI arg parsing for `meept /path/to/dir` syntax
- Wire "Clear Project" RPC call for declining prompt

### Phase 5: Flutter GUI Parity
- Update `api_models.dart` with `projectId`, `projectPath`, `detectionContext`
- Add project prompt dialog in `lib/dialogs/project_prompt_dialog.dart`
- Update session provider to send/receive detection context
- Ensure project switcher works per-session (not global)

### Phase 6: Legacy Session Migration
- On first load of session with no `detection_context` and no `project_path`:
  - Show modal: "This session has no project bound. Choose action:"
  - Options: Use CWD, Pick Project, Continue Without Project
- Store user's choice in session's `detection_context`

---

## Testing Strategy

### Unit Tests
- `worker_pool_test.go`: Test worker lazy activation, multiplexing, idle timeout
- `manager_test.go`: Test GetOrCreate, loop assignment, session isolation
- `loop_test.go`: Test workingDir initialization, no default fallback

### Integration Tests
- Two sessions, different projects: verify agents run in correct dirs
- Session resume with bound project: verify workingDir restored
- Session without project: verify prompt appears, all 3 responses work
- Worker pool exhaustion: verify queuing behavior when >100 workers busy

### E2E Tests
- TUI: Open 3 sessions in different projects, run agents in each
- CLI: `meept /path/to/foo` starts session with correct workingDir
- Flutter: Same scenarios as TUI

---

## Migration Guide

### For Existing Deployments

1. **Backward compatible:** Existing sessions without `detection_context` work as before
2. **First load prompt:** Users see prompt once per legacy session, then choice is persisted
3. **Daemon config:** New `agent.worker_pool` section is optional with sensible defaults

### Breaking Changes

- `AgentLoop` constructor signature changes (requires `sessionID` and `workingDir`)
- `StartProjectSub()` removed (callers must be updated)
- `project.set` bus event still fires, but only for explicit project switches (not session loads)

---

## Rollback Plan

If issues arise:

1. **Immediate rollback:** Revert to singleton AgentLoop (5-line change in `components.go`)
2. **Partial rollback:** Keep AgentLoop Manager but disable multiplexing (1:1 worker:loop)
3. **Config override:** Add `agent.use_singleton: true` to bypass new code path

---

## Risk Assessment

| Risk                           | Likelihood | Impact | Mitigation |
|--------------------------------|------------|--------|------------|
| Worker pool deadlocks          | Medium     | High   | Extensive testing + timeout watchdog |
| Memory bloat (many idle loops) | Low        | Medium | Idle timeout kills unused workers |
| Flutter/TUI parity drift       | High       | Medium | Implement both in same PR, shared spec |
| Legacy session prompt fatigue  | Medium     | Low    | "Don't ask again" option, persists choice |
| Multiplexing race conditions   | Medium     | High   | Mutex-protected loop assignment, tests |

---

## Success Criteria

- [ ] Can run 3 concurrent sessions in 3 different projects without cross-contamination
- [ ] Resuming a session with bound project restores correct workingDir automatically
- [ ] Sessions without projects prompt user (TUI + Flutter)
- [ ] CLI `meept /path` syntax works
- [ ] Worker pool stays under configured limits under load
- [ ] Multiplexing ratio is configurable (1:1 to 1:10 range tested)
- [ ] No regression in agent concurrency limits (global: 10, per-agent: 3)

---

## References

- `internal/agent/tactical.go` — Current two-level semaphore implementation
- `internal/daemon/components.go:989` — Current singleton AgentLoop creation
- `internal/agent/loop.go:4444-4493` — Current `StartProjectSub()` project subscription
- `internal/session/session.go` — Session schema
- User brainstorming session 2026-07-07 — Design collaboration
